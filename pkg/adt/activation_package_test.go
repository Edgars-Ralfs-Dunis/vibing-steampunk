package adt

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// The 7.50 inactive-objects entry, captured 2026-10-11 (user anonymised). It
// has no parentUri, which is all objectBelongsToPackage looked at, so on 7.50
// ActivatePackage matched nothing and answered "Activated 0 objects, 0 failed"
// with the package's objects still inactive.
const inactiveEntry750 = `<ioc:entry><ioc:object ioc:user="TESTUSER" ioc:deleted="false"><ioc:ref adtcore:uri="/sap/bc/adt/programs/programs/%s" adtcore:type="PROG/P" adtcore:name="%s" xmlns:adtcore="http://www.sap.com/adt/core"/></ioc:object><ioc:transport/></ioc:entry>`

func inactive750(names ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><ioc:inactiveObjects xmlns:ioc="http://www.sap.com/abapxml/inactiveCtsObjects">`)
	for _, n := range names {
		b.WriteString(strings.NewReplacer("%s", n).Replace(inactiveEntry750))
	}
	b.WriteString(`</ioc:inactiveObjects>`)
	return b.String()
}

// packageServer serves the inactive list, answers TADIR with the names in
// members, and activates whatever it is sent.
func packageServer(t *testing.T, inactive string, members ...string) (*Client, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-CSRF-Token", "TOKEN")
		w.Header().Set("Content-Type", "application/xml")
		switch {
		case strings.Contains(r.URL.Path, "/activation/inactiveobjects"):
			_, _ = w.Write([]byte(inactive))
		case strings.Contains(r.URL.Path, "/datapreview/freestyle"):
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			queries = append(queries, string(body))
			mu.Unlock()
			_, _ = w.Write([]byte(tableXML(col("OBJ_NAME", members...))))
		default:
			w.WriteHeader(http.StatusOK) // activation: an empty answer is a clean one
		}
	}))
	t.Cleanup(srv.Close)
	cfg := NewConfig(srv.URL, "TESTUSER", "secret")
	return NewClientWithTransport(cfg, NewTransport(cfg)), &queries
}

func TestActivatePackageFindsMembersWithoutParentURI(t *testing.T) {
	client, queries := packageServer(t, inactive750("ZDEMO_IN", "ZDEMO_OUT"), "ZDEMO_IN")
	result, err := client.ActivatePackage(context.Background(), "$zdemo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Activated) != 1 || result.Activated[0].Name != "ZDEMO_IN" {
		t.Fatalf("activated %+v, want only ZDEMO_IN (summary %q)", result.Activated, result.Summary)
	}
	if len(*queries) != 1 || !strings.Contains((*queries)[0], "devclass = '$ZDEMO'") {
		t.Errorf("membership was not asked of TADIR once: %q", *queries)
	}
}

func TestActivatePackageWarnsWhenNoInactiveObjectIsInThePackage(t *testing.T) {
	client, _ := packageServer(t, inactive750("ZTEMP_EXEC_44831992"))
	result, err := client.ActivatePackage(context.Background(), "$ZDEMO", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "WARNING") {
		t.Errorf("summary %q reads as success with 1 inactive object outside the package", result.Summary)
	}
}
