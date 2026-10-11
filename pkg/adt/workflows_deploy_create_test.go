package adt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// createFake answers a CreateFromFile run: the transport check is refused (the
// choice is left to SAP, which records the new object on a request of its
// own), the LOCK reports that request as CORRNR, and the checkrun answers
// checkType ("" for a clean check). It records the corrNr of every PUT.
func createFake(t *testing.T, lockCorrNr, checkType string) (*Client, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var putCorrNr []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		path := r.URL.Path
		switch {
		case strings.Contains(path, "/discovery"):
			w.Header().Set("X-CSRF-Token", "TOKEN")
		case strings.Contains(path, "/transportchecks"):
			w.WriteHeader(http.StatusNotFound)
		case strings.Contains(path, "/checkruns"):
			msg := ""
			if checkType != "" {
				msg = `<chkrun:checkMessageList><chkrun:checkMessage chkrun:uri="/sap/bc/adt/programs/programs/ZNEW/source/main#start=2,0" chkrun:type="` + checkType + `" chkrun:shortText="Field X is unknown"/></chkrun:checkMessageList>`
			}
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><chkrun:checkRunReports xmlns:chkrun="http://www.sap.com/adt/checkrun"><chkrun:checkReport chkrun:reporter="abapCheckRun">` + msg + `</chkrun:checkReport></chkrun:checkRunReports>`))
		case r.Method == http.MethodPost && r.URL.Query().Get("_action") == "LOCK":
			w.Header().Set("Content-Type", "application/vnd.sap.as+xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
<asx:abap xmlns:asx="http://www.sap.com/abapxml" version="1.0"><asx:values><DATA>
<LOCK_HANDLE>HANDLE-1</LOCK_HANDLE><CORRNR>` + lockCorrNr + `</CORRNR><MODIFICATION_SUPPORT>Modification</MODIFICATION_SUPPORT>
</DATA></asx:values></asx:abap>`))
		case r.Method == http.MethodPut && strings.HasSuffix(path, "/source/main"):
			got := r.URL.Query().Get("corrNr")
			putCorrNr = append(putCorrNr, got)
			if got == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte("Parameter corrNr could not be found"))
			} else if got != lockCorrNr {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte("Object ZNEW is already locked in request " + lockCorrNr))
			}
		}
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "TESTER", "secret", WithAllowTransportableEdits()), &putCorrNr
}

func newProgFile(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "znew.prog.abap")
	if err := os.WriteFile(file, []byte("REPORT znew.\nWRITE 'hello'.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// RWF 20: a new object deployed with no transport lands on the request SAP
// generates. The source PUT went out without it and failed with "Parameter
// corrNr could not be found"; it must use the request the LOCK reports, as
// the update path does.
func TestCreateFromFileWritesUnderTheLockRequest(t *testing.T) {
	client, puts := createFake(t, "TRK900001", "")
	res, err := client.CreateFromFile(context.Background(), newProgFile(t), "ZPKG", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(*puts) != 1 || (*puts)[0] != "TRK900001" {
		t.Fatalf("source PUT corrNr = %q, want [TRK900001]", *puts)
	}
	if !res.Success || res.Transport != "TRK900001" {
		t.Fatalf("result success=%v transport=%q: %s %v", res.Success, res.Transport, res.Message, res.Errors)
	}
}

// RWF 22: once CreateObject succeeded the object exists, so a later failure
// must say created, and what was left behind on which request.
func TestCreateFromFileFailureAfterCreateSaysCreated(t *testing.T) {
	client, _ := createFake(t, "TRK900001", "E")
	res, err := client.CreateFromFile(context.Background(), newProgFile(t), "ZPKG", "TRK900002")
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || len(res.SyntaxErrors) == 0 {
		t.Fatalf("want a syntax-error failure, got success=%v %s", res.Success, res.Message)
	}
	if !res.Created || res.Transport != "TRK900002" {
		t.Errorf("created=%v transport=%q, want true and TRK900002", res.Created, res.Transport)
	}
	if !strings.Contains(res.Message, "ZNEW") || !strings.Contains(res.Message, "TRK900002") {
		t.Errorf("the message does not say what was left behind where: %s", res.Message)
	}
}

// RWF 20, second half: a write refused with "already locked in request X"
// returns X, the request the new object actually sits on.
func TestCreateFromFileReturnsTheRequestTheObjectIsLockedIn(t *testing.T) {
	client, _ := createFake(t, "TRK900001", "")
	res, err := client.CreateFromFile(context.Background(), newProgFile(t), "ZPKG", "TRK900002")
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || res.Transport != "TRK900001" {
		t.Fatalf("success=%v transport=%q, want a failure that returns TRK900001: %s", res.Success, res.Transport, res.Message)
	}
}
