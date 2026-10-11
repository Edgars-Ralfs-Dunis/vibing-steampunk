package adt

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGetObjectTextsInLanguage(t *testing.T) {
	mock := &mockTransportClient{
		responses: map[string]*http.Response{
			"/sap/bc/adt/programs/programs/ZTEST/source/main": newTestResponse("REPORT ztest.\nWRITE 'Bonjour'."),
			"discovery": newTestResponse("OK"),
		},
	}

	cfg := NewConfig("https://sap.example.com:44300", "user", "pass")
	transport := NewTransportWithClient(cfg, mock)
	client := NewClientWithTransport(cfg, transport)

	content, err := client.GetObjectTextsInLanguage(context.Background(), "/sap/bc/adt/programs/programs/ZTEST/source/main", "FR")
	if err != nil {
		t.Fatalf("GetObjectTextsInLanguage failed: %v", err)
	}

	if !strings.Contains(content, "Bonjour") {
		t.Errorf("Expected content to contain 'Bonjour', got: %s", content)
	}
}

// Both of these tests used to assert against a response shape nobody had ever
// received. That is not a weak test, it is an inverted one: it proved the
// parser matched the fiction, and it went green for as long as the capability
// was broken. The documents below are the ones a 7.58 system actually serves,
// with the object names replaced.

func TestGetDataElementLabels(t *testing.T) {
	// The labels are child elements of dtel:dataElement. The old fixture had
	// them as attributes of the root — shortDescription, mediumDescription,
	// longDescription, heading — which no release has ever sent.
	xmlResp := `<?xml version="1.0" encoding="utf-8"?><blue:wbobj adtcore:name="ZDEMO_ORDER_ID" adtcore:type="DTEL/DE" adtcore:description="Order" xmlns:blue="http://www.sap.com/wbobj/dictionary/dtel" xmlns:adtcore="http://www.sap.com/adt/core"><dtel:dataElement xmlns:dtel="http://www.sap.com/adt/dictionary/dataelements"><dtel:typeKind>domain</dtel:typeKind><dtel:shortFieldLabel>Court</dtel:shortFieldLabel><dtel:shortFieldLength>10</dtel:shortFieldLength><dtel:mediumFieldLabel>Moyen</dtel:mediumFieldLabel><dtel:longFieldLabel>Long texte</dtel:longFieldLabel><dtel:headingFieldLabel>En-tete</dtel:headingFieldLabel></dtel:dataElement></blue:wbobj>`

	mock := &mockTransportClient{
		responses: map[string]*http.Response{
			"/sap/bc/adt/ddic/dataelements/ZDEMO_ORDER_ID": newTestResponse(xmlResp),
			"discovery": newTestResponse("OK"),
		},
	}

	cfg := NewConfig("https://sap.example.com:44300", "user", "pass")
	transport := NewTransportWithClient(cfg, mock)
	client := NewClientWithTransport(cfg, transport)

	labels, err := client.GetDataElementLabels(context.Background(), "ZDEMO_ORDER_ID", "FR")
	if err != nil {
		t.Fatalf("GetDataElementLabels failed: %v", err)
	}

	for _, c := range []struct{ got, want, field string }{
		{labels.Short, "Court", "Short"},
		{labels.Medium, "Moyen", "Medium"},
		{labels.Long, "Long texte", "Long"},
		{labels.Heading, "En-tete", "Heading"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
}

// The old parser read attributes off the root element, so it would also have
// returned four empty strings for the document above without failing. This
// pins the direction: a document with no labels must not come back looking
// like a data element whose labels are blank.
func TestDataElementWithNoLabelsIsNotMistakenForOne(t *testing.T) {
	xmlResp := `<?xml version="1.0" encoding="utf-8"?><blue:wbobj adtcore:name="ZDEMO_ORDER_ID" xmlns:blue="http://www.sap.com/wbobj/dictionary/dtel"><dtel:dataElement xmlns:dtel="http://www.sap.com/adt/dictionary/dataelements"><dtel:typeKind>domain</dtel:typeKind></dtel:dataElement></blue:wbobj>`

	mock := &mockTransportClient{
		responses: map[string]*http.Response{
			"/sap/bc/adt/ddic/dataelements/ZDEMO_ORDER_ID": newTestResponse(xmlResp),
			"discovery": newTestResponse("OK"),
		},
	}
	cfg := NewConfig("https://sap.example.com:44300", "user", "pass")
	client := NewClientWithTransport(cfg, NewTransportWithClient(cfg, mock))

	labels, err := client.GetDataElementLabels(context.Background(), "ZDEMO_ORDER_ID", "FR")
	if err != nil {
		t.Fatalf("GetDataElementLabels failed: %v", err)
	}
	if labels.Short != "" || labels.Medium != "" || labels.Long != "" || labels.Heading != "" {
		t.Errorf("labels appeared out of a document that has none: %+v", labels)
	}
}

func TestGetTextPoolInLanguage(t *testing.T) {
	// Three plain-text sub-resources, not one XML document at
	// /programs/programs/{name}/textelements — that address answers 404 and
	// the XML the old fixture described does not exist.
	mock := &funcMockClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(req.URL.Path, "discovery"):
				return newTestResponse("OK"), nil
			case strings.HasSuffix(req.URL.Path, "/source/symbols"):
				return newTestResponse("@MaxLength:8\n001=Texte un\n002=Texte deux\n"), nil
			case strings.HasSuffix(req.URL.Path, "/source/selections"):
				return newTestResponse("P_CREA  =Creer\n\nP_DELE  =Supprimer\n"), nil
			case strings.HasSuffix(req.URL.Path, "/source/headings"):
				return newTestResponse("listHeader=\n\ncolumnHeader_1=\n"), nil
			}
			return newTestResponse(""), nil
		},
	}

	cfg := NewConfig("https://sap.example.com:44300", "user", "pass")
	client := NewClientWithTransport(cfg, NewTransportWithClient(cfg, mock))

	entries, err := client.GetTextPoolInLanguage(context.Background(), "ZDEMO_REPORT", "FR")
	if err != nil {
		t.Fatalf("GetTextPoolInLanguage failed: %v", err)
	}

	// Two symbols, two selection texts, two headings. The headings are empty
	// and are still entries: "this heading exists and is untranslated" is the
	// answer a translation report is for.
	if len(entries) != 6 {
		t.Fatalf("got %d entries, want 6: %+v", len(entries), entries)
	}
	want := []TextPoolEntry{
		{ID: "I", Key: "001", Text: "Texte un"},
		{ID: "I", Key: "002", Text: "Texte deux"},
		{ID: "S", Key: "P_CREA", Text: "Creer"},
		{ID: "S", Key: "P_DELE", Text: "Supprimer"},
		{ID: "H", Key: "listHeader", Text: ""},
		{ID: "H", Key: "columnHeader_1", Text: ""},
	}
	for i, w := range want {
		if entries[i] != w {
			t.Errorf("entry %d = %+v, want %+v", i, entries[i], w)
		}
	}
}

// The @MaxLength directive at the top of the symbols document describes the
// document. Reading it as an entry would put a key of "@MaxLength:8" into every
// text pool a report ever showed.
func TestTextPoolDirectivesAreNotEntries(t *testing.T) {
	got := parseTextPoolSource("I", "@MaxLength:8\n  @Other\n001=Texte\n")
	if len(got) != 1 || got[0].Key != "001" {
		t.Errorf("got %+v, want the one real entry", got)
	}
}

func TestCompareObjectLanguages(t *testing.T) {
	// Use a func-based mock that returns different content based on sap-language query param
	mock := &funcMockClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "discovery") {
				return newTestResponse("OK"), nil
			}
			lang := req.URL.Query().Get("sap-language")
			if lang == "FR" {
				return newTestResponse("REPORT ztest.\nWRITE 'Bonjour'."), nil
			}
			return newTestResponse("REPORT ztest.\nWRITE 'Hello'."), nil
		},
	}

	cfg := NewConfig("https://sap.example.com:44300", "user", "pass")
	transport := NewTransportWithClient(cfg, mock)
	client := NewClientWithTransport(cfg, transport)

	comparison, err := client.CompareObjectLanguages(context.Background(),
		"/sap/bc/adt/programs/programs/ZTEST/source/main", "EN", "FR")
	if err != nil {
		t.Fatalf("CompareObjectLanguages failed: %v", err)
	}

	if comparison.SourceLang != "EN" {
		t.Errorf("SourceLang = %v, want EN", comparison.SourceLang)
	}
	if comparison.TargetLang != "FR" {
		t.Errorf("TargetLang = %v, want FR", comparison.TargetLang)
	}

	// Line 2 differs: 'Hello' vs 'Bonjour'
	if len(comparison.Entries) == 0 {
		t.Error("Expected at least 1 diff entry for differing content")
	}
}

// funcMockClient is a mock HTTP client that uses a function for responses.
type funcMockClient struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *funcMockClient) Do(req *http.Request) (*http.Response, error) {
	return m.doFunc(req)
}

func TestOverrideLanguageInRequest(t *testing.T) {
	mock := &mockTransportClient{
		responses: map[string]*http.Response{
			"/sap/bc/adt/programs/programs/ZTEST/source/main": newTestResponse("OK"),
			"discovery": newTestResponse("OK"),
		},
	}

	cfg := NewConfig("https://sap.example.com:44300", "user", "pass")
	cfg.Language = "EN"
	transport := NewTransportWithClient(cfg, mock)
	client := NewClientWithTransport(cfg, transport)

	_, err := client.GetObjectTextsInLanguage(context.Background(), "/sap/bc/adt/programs/programs/ZTEST/source/main", "FR")
	if err != nil {
		t.Fatalf("GetObjectTextsInLanguage failed: %v", err)
	}

	// Verify that the request URL contains sap-language=FR (override)
	if len(mock.requests) < 1 {
		t.Fatal("Expected at least 1 request")
	}

	lastReq := mock.requests[len(mock.requests)-1]
	sapLang := lastReq.URL.Query().Get("sap-language")
	if sapLang != "FR" {
		t.Errorf("Expected sap-language=FR in URL, got sap-language=%s (full URL: %s)", sapLang, lastReq.URL.String())
	}
}

func TestWriteOperationsCheckSafety(t *testing.T) {
	mock := &mockTransportClient{
		responses: map[string]*http.Response{
			"discovery": newTestResponse("OK"),
		},
	}

	cfg := NewConfig("https://sap.example.com:44300", "user", "pass")
	cfg.Safety.ReadOnly = true // Enable read-only mode
	transport := NewTransportWithClient(cfg, mock)
	client := NewClientWithTransport(cfg, transport)

	// WriteMessageClassTexts should be blocked by safety (OpUpdate)
	err := client.WriteMessageClassTexts(context.Background(), "ZTEST_MC", "FR", nil, "lock123", "")
	if err == nil {
		t.Error("WriteMessageClassTexts should fail in read-only mode")
	}

	// WriteDataElementLabels should be blocked by safety (OpUpdate)
	err = client.WriteDataElementLabels(context.Background(), "ZTEST_DTEL", "FR", &DataElementLabels{}, "lock123", "")
	if err == nil {
		t.Error("WriteDataElementLabels should fail in read-only mode")
	}

	// Read operations should still work (will fail on mock but not on safety)
	_, err = client.GetDataElementLabels(context.Background(), "ZTEST_DTEL", "FR")
	// This will fail because we have no mock response for this path, but the error
	// should NOT be a safety error
	if err != nil && strings.Contains(err.Error(), "read-only") {
		t.Error("GetDataElementLabels should not be blocked by read-only mode")
	}
}

// 7.50 has no ADT text-element resource, so all three kinds answer 404, for a
// program with a text pool and for one that does not exist alike. That must
// not read as "no text pool entries".
func TestGetTextPoolInLanguageAll404IsAnError(t *testing.T) {
	mock := &funcMockClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "discovery") {
				return newTestResponse("OK"), nil
			}
			return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{},
				Body: io.NopCloser(strings.NewReader("ExceptionResourceNotFound"))}, nil
		},
	}
	cfg := NewConfig("https://sap.example.com:44300", "user", "pass")
	client := NewClientWithTransport(cfg, NewTransportWithClient(cfg, mock))

	entries, err := client.GetTextPoolInLanguage(context.Background(), "ZRPTEXTPT_SPLIT", "EN")
	if err == nil {
		t.Fatalf("got %d entries and no error; three 404s are not an empty text pool", len(entries))
	}
}
