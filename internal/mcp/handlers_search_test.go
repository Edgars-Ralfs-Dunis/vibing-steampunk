package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// TestHandleSearchObject_ServerSideTypeFilter verifies the MCP search path goes
// through SearchObjectByType: the short-form type is canonicalized and sent to
// the server as the objectType query param, and maxResults is forwarded — so
// max applies after the type filter (the bug txape10 flagged on PR #126).
// The handler asks for one more than requested (see handleSearchObject) so it
// can tell a full page from a truncated one; that over-fetch applies here too.
func TestHandleSearchObject_ServerSideTypeFilter(t *testing.T) {
	var searchQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "informationsystem/search") {
			searchQuery = r.URL.RawQuery
		}
		w.Header().Set("X-CSRF-Token", "test-token")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` +
			`<adtcore:objectReferences xmlns:adtcore="http://www.sap.com/adt/core"/>`))
	}))
	defer ts.Close()

	cfg := &Config{
		BaseURL:            ts.URL,
		Username:           "u",
		Password:           "p",
		Client:             "001",
		Language:           "EN",
		InsecureSkipVerify: true,
	}
	server := NewServer(cfg)
	if server == nil {
		t.Fatal("NewServer returned nil")
	}

	req := newRequest(map[string]any{
		"query":      "Z*",
		"objectType": "CLAS",
		"maxResults": float64(5),
	})
	res, err := server.handleSearchObject(context.Background(), req)
	if err != nil {
		t.Fatalf("handleSearchObject error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handleSearchObject returned an error result: %+v", res)
	}

	if searchQuery == "" {
		t.Fatal("no search request reached the server")
	}
	q, err := url.ParseQuery(searchQuery)
	if err != nil {
		t.Fatalf("parsing captured query %q: %v", searchQuery, err)
	}
	if got := q.Get("objectType"); got != "CLAS/OC" {
		t.Errorf("objectType = %q, want %q (short form should be canonicalized)", got, "CLAS/OC")
	}
	if got := q.Get("maxResults"); got != "6" {
		t.Errorf("maxResults = %q, want %q (requested 5, plus the truncation-detection over-fetch)", got, "6")
	}
	if got := q.Get("query"); got != "Z*" {
		t.Errorf("query = %q, want %q", got, "Z*")
	}
}

// F1-12: the truncation note tells callers to "raise max_results", so the
// handler must read that name too, not only maxResults.
func TestHandleSearchObjectAcceptsMaxResultsSnakeCase(t *testing.T) {
	var sent string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "informationsystem/search") {
			sent = r.URL.Query().Get("maxResults")
		}
		w.Header().Set("X-CSRF-Token", "test-token")
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<adtcore:objectReferences xmlns:adtcore="http://www.sap.com/adt/core"/>`))
	}))
	defer ts.Close()
	server := NewServer(&Config{BaseURL: ts.URL, Username: "u", Password: "p", Client: "001", Language: "EN"})

	if _, err := server.handleSearchObject(context.Background(), newRequest(map[string]any{
		"query": "ZCL_VSP*", "max_results": float64(2),
	})); err != nil {
		t.Fatal(err)
	}
	if sent != "3" {
		t.Fatalf("max_results 2 should ask SAP for 3 (2 plus the truncation probe), sent %q", sent)
	}
}

// F1-13: the handler filters by objectType; the schema must offer it.
func TestSearchObjectSchemaOffersObjectType(t *testing.T) {
	server := NewServer(&Config{BaseURL: "https://sap.example.com:44300", Username: "u", Password: "p", Client: "001"})
	raw := server.mcpServer.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	response, ok := raw.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("expected JSONRPCResponse, got %T", raw)
	}
	var tools []mcp.Tool
	switch result := response.Result.(type) {
	case mcp.ListToolsResult:
		tools = result.Tools
	case *mcp.ListToolsResult:
		tools = result.Tools
	}
	for _, tool := range tools {
		if tool.Name == "SearchObject" {
			if _, ok := tool.InputSchema.Properties["objectType"]; !ok {
				t.Fatal("SearchObject reads objectType but its schema does not offer it")
			}
			return
		}
	}
	t.Fatal("SearchObject is not registered")
}
