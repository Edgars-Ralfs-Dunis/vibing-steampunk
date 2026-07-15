package adt

import (
	"strings"
	"testing"
)

// A real ADT activation failure: the checklist messages are the ROOT element,
// not a child of a wrapper. If the parser only looks for a nested <messages>,
// it reports success for a failed activation — silent data loss for the caller.
const activationFailedRoot = `<?xml version="1.0" encoding="utf-8"?>
<chkl:messages xmlns:chkl="http://www.sap.com/abapxml/checklist">
  <msg objDescr="Class ZCL_DEMO" type="E" line="12" href="/sap/bc/adt/oo/classes/zcl_demo/source/main#start=12,4" forceSupported="false">
    <shortText><txt>Field "FOO" is unknown</txt></shortText>
  </msg>
</chkl:messages>`

// Some releases wrap the same payload; both shapes must be understood.
const activationFailedWrapped = `<?xml version="1.0" encoding="utf-8"?>
<adtcore:objectReferences xmlns:adtcore="http://www.sap.com/adt/core" xmlns:chkl="http://www.sap.com/abapxml/checklist">
  <chkl:messages>
    <msg objDescr="Class ZCL_DEMO" type="E" line="12" href="x" forceSupported="false">
      <shortText><txt>Field "FOO" is unknown</txt></shortText>
    </msg>
  </chkl:messages>
</adtcore:objectReferences>`

// A warning must not fail the activation.
const activationWarning = `<?xml version="1.0" encoding="utf-8"?>
<chkl:messages xmlns:chkl="http://www.sap.com/abapxml/checklist">
  <msg objDescr="Class ZCL_DEMO" type="W" line="3" href="x" forceSupported="true">
    <shortText><txt>Variable is never used</txt></shortText>
  </msg>
</chkl:messages>`

func TestParseActivationResultFailure(t *testing.T) {
	for name, body := range map[string]string{
		"messages as root": activationFailedRoot,
		"messages wrapped": activationFailedWrapped,
	} {
		t.Run(name, func(t *testing.T) {
			res, err := parseActivationResult([]byte(body))
			if err != nil {
				t.Fatalf("parseActivationResult: %v", err)
			}
			if res.Success {
				t.Fatal("an activation with an error message must not report success")
			}
			if len(res.Messages) != 1 {
				t.Fatalf("messages = %d, want 1", len(res.Messages))
			}
			m := res.Messages[0]
			if m.Type != "E" || m.Line != 12 || m.ShortText != `Field "FOO" is unknown` {
				t.Fatalf("message not parsed: %+v", m)
			}
		})
	}
}

func TestParseActivationResultWarningStillSucceeds(t *testing.T) {
	res, err := parseActivationResult([]byte(activationWarning))
	if err != nil {
		t.Fatalf("parseActivationResult: %v", err)
	}
	if !res.Success {
		t.Fatal("a warning must not fail the activation")
	}
	if len(res.Messages) != 1 || res.Messages[0].Type != "W" {
		t.Fatalf("warning not parsed: %+v", res.Messages)
	}
}

func TestParseActivationResultEmptyIsSuccess(t *testing.T) {
	res, err := parseActivationResult(nil)
	if err != nil {
		t.Fatalf("parseActivationResult: %v", err)
	}
	if !res.Success || len(res.Messages) != 0 {
		t.Fatalf("an empty body means a clean activation, got %+v", res)
	}
}

// --- Carried over from the Zalaris fork (e24cc53): a payload captured on a 7.50 system,
// --- kept as proof that the root-element fallback above covers it.
// Real response captured from POST /sap/bc/adt/activation?method=activate&preauditRequested=true
// for a program containing a syntax error. Note the ROOT element is <chkl:messages>.
const activationSyntaxErrorPayload = `<?xml version="1.0" encoding="utf-8"?><chkl:messages xmlns:chkl="http://www.sap.com/abapxml/checklist"><msg objDescr="Program Z_VSP_ACT_TEST" type="E" line="1" href="/sap/bc/adt/programs/programs/z_vsp_act_test/source/main#start=2,6" forceSupported="true"><shortText><txt>Field "LV_UNDEFINED_VARIABLE" is unknown.</txt></shortText><atom:link href="art.syntax:GTU" rel="http://www.sap.com/adt/categories/quickfixes" xmlns:atom="http://www.w3.org/2005/Atom"/></msg></chkl:messages>`

func TestParseActivationResultSyntaxError(t *testing.T) {
	res, err := parseActivationResult([]byte(activationSyntaxErrorPayload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Fatal("expected Success=false for activation response with type=E message")
	}
	if len(res.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(res.Messages))
	}
	if res.Messages[0].Type != "E" {
		t.Fatalf("expected type E, got %q", res.Messages[0].Type)
	}
	if !strings.Contains(res.Messages[0].ShortText, "LV_UNDEFINED_VARIABLE") {
		t.Fatalf("unexpected message text: %q", res.Messages[0].ShortText)
	}
}

func TestParseActivationResultEmptyBodySuccess(t *testing.T) {
	res, err := parseActivationResult(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Success {
		t.Fatal("expected Success=true for empty body")
	}
}

func TestParseActivationResultInactiveObjects(t *testing.T) {
	payload := `<?xml version="1.0" encoding="utf-8"?><ioc:inactiveObjects xmlns:ioc="http://www.sap.com/abapxml/inactiveCtsObjects" xmlns:adtcore="http://www.sap.com/adt/core"><ioc:entry><ioc:object><ioc:ref adtcore:uri="/sap/bc/adt/programs/programs/ztest" adtcore:type="PROG/P" adtcore:name="ZTEST"/></ioc:object><ioc:transport/></ioc:entry></ioc:inactiveObjects>`
	res, err := parseActivationResult([]byte(payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Success {
		t.Fatal("expected Success=false when inactive objects are returned")
	}
	if len(res.Inactive) != 1 || res.Inactive[0].Name != "ZTEST" {
		t.Fatalf("unexpected inactive list: %+v", res.Inactive)
	}
}
