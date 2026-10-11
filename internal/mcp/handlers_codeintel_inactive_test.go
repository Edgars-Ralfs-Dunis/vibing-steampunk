package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The 7.50 inactive-objects body, captured 2026-10-11 (user anonymised): no
// parentUri, and an empty <ioc:transport/> even for a $TMP object.
const inactiveObjects750 = `<?xml version="1.0" encoding="utf-8"?><ioc:inactiveObjects xmlns:ioc="http://www.sap.com/abapxml/inactiveCtsObjects"><ioc:entry><ioc:object ioc:user="TESTUSER" ioc:deleted="false"><ioc:ref adtcore:uri="/sap/bc/adt/programs/programs/ztemp_exec_44831992" adtcore:type="PROG/P" adtcore:name="ZTEMP_EXEC_44831992" xmlns:adtcore="http://www.sap.com/adt/core"/></ioc:object><ioc:transport/></ioc:entry></ioc:inactiveObjects>`

func inactiveObjectsText(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	s := NewServer(&Config{BaseURL: srv.URL, Username: "TESTUSER", Client: "001", Mode: "expert"})
	res, err := s.handleGetInactiveObjects(t.Context(), newRequest(nil))
	if err != nil {
		t.Fatal(err)
	}
	return resultText(res)
}

func TestInactiveObjectsTextSkipsAnEmptyTransport(t *testing.T) {
	if text := inactiveObjectsText(t, inactiveObjects750); strings.Contains(text, "Transport: \n") {
		t.Errorf("an empty <transport/> printed an empty Transport line:\n%s", text)
	}
}

func TestInactiveObjectsTextShowsTheParent(t *testing.T) {
	body := strings.Replace(inactiveObjects750, `adtcore:name="ZTEMP_EXEC_44831992"`,
		`adtcore:name="ZTEMP_EXEC_44831992" adtcore:parentUri="/sap/bc/adt/packages/%24tmp"`, 1)
	if text := inactiveObjectsText(t, body); !strings.Contains(text, "Parent: /sap/bc/adt/packages/%24tmp\n") {
		t.Errorf("parentUri is not shown:\n%s", text)
	}
}
