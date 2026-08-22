package adt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The ADT activation endpoint answers a preauditRequested=true POST with an
// <ioc:inactiveObjects> list of the parts it wants confirmed. That list is a
// question, not a failure. vsp used to stop there, so every object owning child
// includes (CLAS, FUGR) stayed inactive while Activate reported an unexplained
// failure with empty Messages. Verified against ZES on 2026-08-22: a PROG on the
// same package and transport activated, a one-method class did not.

const preauditClassPayload = `<?xml version="1.0" encoding="utf-8"?>
<ioc:inactiveObjects xmlns:ioc="http://www.sap.com/abapxml/inactiveCtsObjects" xmlns:adtcore="http://www.sap.com/adt/core">
  <ioc:entry><ioc:object><ioc:ref/></ioc:object><ioc:transport/></ioc:entry>
  <ioc:entry><ioc:object><ioc:ref adtcore:uri="/sap/bc/adt/oo/classes/zcl_x" adtcore:type="CLAS/OC" adtcore:name="ZCL_X"/></ioc:object></ioc:entry>
  <ioc:entry><ioc:object><ioc:ref adtcore:uri="/sap/bc/adt/oo/classes/zcl_x/includes/definitions" adtcore:type="CLAS/OCN/definitions" adtcore:name="ZCL_X" adtcore:parentUri="/sap/bc/adt/oo/classes/zcl_x"/></ioc:object></ioc:entry>
</ioc:inactiveObjects>`

func newActivationServer(t *testing.T, phase1 string) (*httptest.Server, *[]string, *[]string) {
	t.Helper()
	var seen []string
	var bodies []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sap/bc/adt/core/discovery" {
			w.Header().Set("X-CSRF-Token", "test-token")
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/sap/bc/adt/activation" {
			preaudit := r.URL.Query().Get("preauditRequested")
			seen = append(seen, preaudit)

			buf := new(strings.Builder)
			if r.Body != nil {
				b := make([]byte, 8192)
				n, _ := r.Body.Read(b)
				buf.Write(b[:n])
			}
			bodies = append(bodies, buf.String())

			w.Header().Set("Content-Type", "application/xml")
			if preaudit == "true" {
				w.Write([]byte(phase1))
				return
			}
			// confirm phase: empty body means activated
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	return srv, &seen, &bodies
}

func TestActivateConfirmsPreauditList(t *testing.T) {
	srv, seen, bodies := newActivationServer(t, preauditClassPayload)
	defer srv.Close()

	client := NewClient(srv.URL, "testuser", "testpass", WithClient("001"))
	res, err := client.Activate(context.Background(), "/sap/bc/adt/oo/classes/zcl_x", "ZCL_X")
	if err != nil {
		t.Fatalf("Activate returned error: %v", err)
	}

	if len(*seen) != 2 {
		t.Fatalf("expected two activation POSTs (preaudit + confirm), got %d: %v", len(*seen), *seen)
	}
	if (*seen)[0] != "true" || (*seen)[1] != "false" {
		t.Fatalf("expected preauditRequested true then false, got %v", *seen)
	}
	if !res.Success {
		t.Errorf("expected Success after confirm, got false (messages=%v inactive=%v)", res.Messages, res.Inactive)
	}

	confirm := (*bodies)[1]
	if !strings.Contains(confirm, "/sap/bc/adt/oo/classes/zcl_x/includes/definitions") {
		t.Errorf("confirm body missing child include:\n%s", confirm)
	}
	// The transport group header arrives as an entry with an empty ref; echoing
	// it back makes SAP reject the whole batch.
	if strings.Contains(confirm, `adtcore:uri=""`) {
		t.Errorf("confirm body echoed the blank group-header ref:\n%s", confirm)
	}
}

func TestActivateSkipsConfirmForFlatObject(t *testing.T) {
	// A PROG has no child includes: phase 1 comes back empty and there is
	// nothing to confirm.
	srv, seen, _ := newActivationServer(t, "")
	defer srv.Close()

	client := NewClient(srv.URL, "testuser", "testpass", WithClient("001"))
	res, err := client.Activate(context.Background(), "/sap/bc/adt/programs/programs/ztest", "ZTEST")
	if err != nil {
		t.Fatalf("Activate returned error: %v", err)
	}
	if len(*seen) != 1 {
		t.Fatalf("expected a single POST for a flat object, got %d: %v", len(*seen), *seen)
	}
	if !res.Success {
		t.Errorf("expected Success for empty phase-1 response")
	}
}

func TestActivateDoesNotConfirmRealErrors(t *testing.T) {
	// Genuine syntax errors must not be "confirmed" into activation.
	const errPayload = `<?xml version="1.0" encoding="utf-8"?>
<chkl:messages xmlns:chkl="http://www.sap.com/abapxml/checklist">
  <msg objDescr="ZCL_X" type="E" line="12" href="x"><shortText><txt>Field FOO is unknown</txt></shortText></msg>
</chkl:messages>`

	srv, seen, _ := newActivationServer(t, errPayload)
	defer srv.Close()

	client := NewClient(srv.URL, "testuser", "testpass", WithClient("001"))
	res, err := client.Activate(context.Background(), "/sap/bc/adt/oo/classes/zcl_x", "ZCL_X")
	if err != nil {
		t.Fatalf("Activate returned error: %v", err)
	}
	if len(*seen) != 1 {
		t.Fatalf("expected no confirm POST when phase 1 reports errors, got %v", *seen)
	}
	if res.Success {
		t.Errorf("expected Success=false on a syntax error")
	}
	if len(res.Messages) != 1 || res.Messages[0].ShortText != "Field FOO is unknown" {
		t.Errorf("expected the error message to survive, got %+v", res.Messages)
	}
}

func TestBuildObjectReferencesEscapes(t *testing.T) {
	out := buildObjectReferences([]InactiveObject{
		{URI: "/sap/bc/adt/oo/classes/zcl_x/source/main#type=CLAS%2FOM;name=EXECUTE", Name: "ZCL_X  EXECUTE"},
		{URI: "/a&b", Name: `q"uote`},
	})
	if !strings.Contains(out, "name=EXECUTE") {
		t.Errorf("method URI mangled:\n%s", out)
	}
	if !strings.Contains(out, "/a&amp;b") || !strings.Contains(out, "q&quot;uote") {
		t.Errorf("attribute values not escaped:\n%s", out)
	}
}

func TestActivationFailureReasonPrefersErrorText(t *testing.T) {
	res := &ActivationResult{
		Messages: []ActivationResultMessage{
			{Type: "W", ShortText: "just a warning"},
			{Type: "E", ShortText: "the real problem"},
		},
	}
	if got := activationFailureReason(res); got != "the real problem" {
		t.Errorf("got %q", got)
	}

	res2 := &ActivationResult{Inactive: []InactiveObject{{URI: "/x"}, {URI: "/y"}}}
	if got := activationFailureReason(res2); !strings.Contains(got, "still inactive") {
		t.Errorf("got %q", got)
	}
}
