package adt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RWF 31: a .fugr.abap holding only "FUNCTION-POOL zfg." was written over the
// group's main program, replacing its INCLUDE list; the deploy said "activated"
// and every function module of the group became unreachable. A main program
// without its TOP and UXX includes is never a group's source, so it is refused
// before anything is written.
func TestFunctionGroupMainWithoutIncludesIsRefused(t *testing.T) {
	var puts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/discovery"):
			w.Header().Set("X-CSRF-Token", "TOKEN")
		case strings.Contains(r.URL.Path, "/checkruns"):
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><chkrun:checkRunReports xmlns:chkrun="http://www.sap.com/adt/checkrun"/>`))
		case r.Method == http.MethodPost && r.URL.Query().Get("_action") == "LOCK":
			w.Header().Set("Content-Type", "application/vnd.sap.as+xml")
			_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><asx:abap xmlns:asx="http://www.sap.com/abapxml" version="1.0"><asx:values><DATA><LOCK_HANDLE>H</LOCK_HANDLE></DATA></asx:values></asx:abap>`))
		case r.Method == http.MethodPut:
			puts++
		}
	}))
	defer srv.Close()
	client := NewClient(srv.URL, "TESTER", "secret")

	file := filepath.Join(t.TempDir(), "zfg.fugr.abap")
	if err := os.WriteFile(file, []byte("FUNCTION-POOL zfg.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := client.DeployFromFile(context.Background(), file, "$TMP", "")
	if err == nil && res.Success {
		t.Fatalf("the include list of SAPLZFG was replaced: %s", res.Message)
	}
	if puts != 0 {
		t.Fatalf("%d source write(s) went out", puts)
	}

	frame := "  INCLUDE LZFGTOP.                          \" Global Data\n  INCLUDE LZFGUXX.                          \" Function Modules\n"
	if err := client.UpdateSource(context.Background(), "/sap/bc/adt/functions/groups/zfg/source/main", frame, "H", ""); err != nil || puts != 1 {
		t.Fatalf("a whole main program was refused (err %v, %d writes)", err, puts)
	}
	ns := "INCLUDE /ABC/LFGTOP.\nINCLUDE /ABC/LFGUXX.\n"
	if err := client.UpdateSource(context.Background(), "/sap/bc/adt/functions/groups/%2fabc%2ffg/source/main", ns, "H", ""); err != nil || puts != 2 {
		t.Fatalf("a namespaced main program was refused (err %v, %d writes)", err, puts)
	}
}
