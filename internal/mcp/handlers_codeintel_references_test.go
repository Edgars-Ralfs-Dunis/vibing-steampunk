package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oisee/vibing-steampunk/pkg/adt"
)

// The where-used response is a tree flattened into a list, and most of it is
// scaffolding — packages and function groups carrying isResult="false", present
// only so their children have a parent. Returned whole, one real question
// produced 113 entries and 56,000 characters: more than the agent asking it can
// hold, so the tool that answered was the tool that could not be used.

func referencesXML(hits int, scaffolding int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><usageReferences:referenceResult><referencedObjects>`)
	for i := 0; i < scaffolding; i++ {
		b.WriteString(fmt.Sprintf(
			`<referencedObject uri="/sap/bc/adt/packages/zdemo%d" isResult="false" canHaveChildren="true">`+
				`<adtObject adtcore:name="$ZDEMO%d" adtcore:type="DEVC/K"/></referencedObject>`, i, i))
	}
	for i := 0; i < hits; i++ {
		b.WriteString(fmt.Sprintf(
			`<referencedObject uri="/sap/bc/adt/oo/classes/zcl_demo_caller%d" isResult="true" usageInformation="gradeDirect">`+
				`<adtObject adtcore:name="ZCL_DEMO_CALLER%d" adtcore:type="CLAS/OC"/></referencedObject>`, i, i))
	}
	b.WriteString(`</referencedObjects></usageReferences:referenceResult>`)
	return b.String()
}

func referencesServer(t *testing.T, hits, scaffolding int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "test-token")
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(referencesXML(hits, scaffolding)))
	}))
}

func referencesAnswer(t *testing.T, srv *httptest.Server, args map[string]any) map[string]any {
	t.Helper()
	s := &Server{adtClient: adt.NewClient(srv.URL, "user", "pass")}
	var req mcp.CallToolRequest
	req.Params.Arguments = args
	result, err := s.handleFindReferences(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(toolResultText(t, result)), &out); err != nil {
		t.Fatalf("the answer should be JSON: %v", err)
	}
	return out
}

func TestReferencesDropsScaffoldingAndSaysHowMuch(t *testing.T) {
	srv := referencesServer(t, 3, 7)
	defer srv.Close()

	out := referencesAnswer(t, srv, map[string]any{"object_url": "/sap/bc/adt/oo/classes/zcl_demo_order"})
	if out["total"] != float64(3) {
		t.Errorf("three rows are results, got %v", out["total"])
	}
	if out["scaffolding"] != float64(7) {
		t.Errorf("seven rows are containers and should be counted, not silently dropped, got %v", out["scaffolding"])
	}
	refs, _ := out["references"].([]any)
	if len(refs) != 3 {
		t.Errorf("only the results belong in the list, got %d", len(refs))
	}
}

func TestReferencesCapsAndSaysItCapped(t *testing.T) {
	srv := referencesServer(t, 120, 5)
	defer srv.Close()

	out := referencesAnswer(t, srv, map[string]any{"object_url": "/sap/bc/adt/oo/classes/zcl_demo_order"})
	refs, _ := out["references"].([]any)
	if len(refs) != 50 {
		t.Errorf("the default cap is 50, got %d", len(refs))
	}
	if out["total"] != float64(120) {
		t.Errorf("the total must survive the cap: \"50 of 120\" and \"120\" are different answers, got %v", out["total"])
	}
	if out["truncated"] == nil {
		t.Error("a list that was cut is not a list that ended, and must say so")
	}
}

func TestReferencesSaysWhenEveryRowWasAContainer(t *testing.T) {
	srv := referencesServer(t, 0, 4)
	defer srv.Close()

	out := referencesAnswer(t, srv, map[string]any{"object_url": "/sap/bc/adt/oo/classes/zcl_demo_order"})
	if out["total"] != float64(0) {
		t.Fatalf("no row was a result, got %v", out["total"])
	}
	if out["note"] == nil {
		t.Error("containers only is an answer, and not the same as the resource returning nothing")
	}
}

// A table is used by structures as much as by code: on one SAP system T549A had
// 393 references, the first fifty of them all structures. Asking for code got
// none, and the cap was not even in the schema, so no agent could raise it.

func mixedReferencesXML(structures int, classes int) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><usageReferences:referenceResult><referencedObjects>`)
	for i := 0; i < structures; i++ {
		b.WriteString(fmt.Sprintf(
			`<referencedObject uri="/sap/bc/adt/ddic/structures/zs_demo%d" isResult="true">`+
				`<adtObject adtcore:name="ZS_DEMO%d" adtcore:type="TABL/DS"/></referencedObject>`, i, i))
	}
	for i := 0; i < classes; i++ {
		b.WriteString(fmt.Sprintf(
			`<referencedObject uri="/sap/bc/adt/oo/classes/zcl_demo_reader%d" isResult="true">`+
				`<adtObject adtcore:name="ZCL_DEMO_READER%d" adtcore:type="CLAS/OC"/></referencedObject>`, i, i))
	}
	b.WriteString(`</referencedObjects></usageReferences:referenceResult>`)
	return b.String()
}

func TestReferencesSchemaOffersTheCapAndATypeFilter(t *testing.T) {
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
		if tool.Name != "FindReferences" {
			continue
		}
		for _, name := range []string{"max_results", "object_types"} {
			if _, ok := tool.InputSchema.Properties[name]; !ok {
				t.Errorf("an argument the handler reads but the schema hides is an argument no agent will send: %s", name)
			}
		}
		return
	}
	t.Fatal("FindReferences is not registered")
}

func TestReferencesFiltersByTypeBeforeTheCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "test-token")
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(mixedReferencesXML(60, 3)))
		}
	}))
	defer srv.Close()

	for _, filter := range []any{"CLAS", []any{"CLAS/OC"}} {
		out := referencesAnswer(t, srv, map[string]any{
			"object_url": "/sap/bc/adt/vit/wb/object_type/tabldt/object_name/ZT_DEMO", "object_types": filter})
		refs, _ := out["references"].([]any)
		if len(refs) != 3 {
			t.Errorf("filter %v: the three classes sit behind sixty structures and must still come back, got %d", filter, len(refs))
		}
		if out["total"] != float64(3) || out["filtered_out"] != float64(60) {
			t.Errorf("filter %v: total counts what matched, filtered_out what did not; got %v and %v",
				filter, out["total"], out["filtered_out"])
		}
		if out["scaffolding"] != float64(0) {
			t.Errorf("filter %v: a filtered hit is not scaffolding too, got %v", filter, out["scaffolding"])
		}
	}

	out := referencesAnswer(t, srv, map[string]any{
		"object_url": "/sap/bc/adt/vit/wb/object_type/tabldt/object_name/ZT_DEMO", "object_types": "PROG"})
	if out["note"] != nil {
		t.Errorf("a filter that removed every hit must not say nothing uses it: %v", out["note"])
	}
}

// On a 7.50 system the where-used resource answers a DDIC table's own address
// with a 500, "Error while converting object references", and the same table
// under the generic workbench address with the right list.

func TestReferencesRetriesATableUnderTheWorkbenchAddress(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "test-token")
		if r.Method != http.MethodPost {
			return
		}
		uri := r.URL.Query().Get("uri")
		asked = append(asked, uri)
		if strings.HasPrefix(uri, "/sap/bc/adt/ddic/") {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`<exc:exception><message lang="EN">Error while converting object references</message></exc:exception>`))
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(mixedReferencesXML(0, 2)))
	}))
	defer srv.Close()

	out := referencesAnswer(t, srv, map[string]any{"object_url": "/sap/bc/adt/ddic/tables/zt_demo"})
	want := "/sap/bc/adt/vit/wb/object_type/tabldt/object_name/ZT_DEMO"
	if out["object_url_used"] != want {
		t.Errorf("the retry must say which address answered, got %v (asked %v)", out["object_url_used"], asked)
	}
	if refs, _ := out["references"].([]any); len(refs) != 2 {
		t.Errorf("the workbench address knows two callers, got %d", len(refs))
	}
}

// A 7.50 system (ZED) sets isResult on no row of this answer. For
// ZCL_ADT_HTTP_GATE its one caller, a method of ZCL_VSP_APC_HANDLER, and for
// ZALA_CHANGE_LOG the reports filed straight under their package, all arrive as
// isResult="false" rows carrying usageInformation="gradeDirect" — the rows
// GetCallersOf reads. Filtering on isResult alone said "nothing uses it".
const references750XML = `<?xml version="1.0" encoding="utf-8"?><usageReferences:usageReferenceResult><usageReferences:referencedObjects>` +
	`<usageReferences:referencedObject uri="/sap/bc/adt/packages/zadt_vsp" isResult="false" canHaveChildren="true">` +
	`<usageReferences:adtObject adtcore:name="ZADT_VSP" adtcore:type="DEVC/K"/></usageReferences:referencedObject>` +
	`<usageReferences:referencedObject uri="/sap/bc/adt/oo/classes/zcl_vsp_apc_handler" parentUri="/sap/bc/adt/packages/zadt_vsp" isResult="false" canHaveChildren="true">` +
	`<usageReferences:adtObject adtcore:name="ZCL_VSP_APC_HANDLER" adtcore:type="CLAS/OC"/></usageReferences:referencedObject>` +
	`<usageReferences:referencedObject uri="/sap/bc/adt/oo/classes/zcl_vsp_apc_handler/source/main#type=CLAS%2FOM;name=IF_APC_WSP_EXTENSION~ON_ACCEPT" parentUri="/sap/bc/adt/oo/classes/zcl_vsp_apc_handler" isResult="false" usageInformation="gradeDirect,includeProductive">` +
	`<usageReferences:adtObject adtcore:name="IF_APC_WSP_EXTENSION~ON_ACCEPT" adtcore:type="CLAS/OM"/></usageReferences:referencedObject>` +
	`<usageReferences:referencedObject uri="/sap/bc/adt/packages/zala" isResult="false" canHaveChildren="true">` +
	`<usageReferences:adtObject adtcore:name="ZALA" adtcore:type="DEVC/K"/></usageReferences:referencedObject>` +
	`<usageReferences:referencedObject uri="/sap/bc/adt/programs/programs/zala_fulldump_selection" parentUri="/sap/bc/adt/packages/zala" isResult="false" usageInformation="gradeDirect,includeProductive">` +
	`<usageReferences:adtObject adtcore:name="ZALA_FULLDUMP_SELECTION" adtcore:type="PROG/P"/></usageReferences:referencedObject>` +
	`<usageReferences:referencedObject uri="/sap/bc/adt/oo/classes/zcl_adt_http_gate/source/main#type=CLAS%2FOM;name=IS_AUTHORIZED" parentUri="/sap/bc/adt/oo/classes/zcl_adt_http_gate" isResult="false" usageInformation="gradeComponent,includeProductive">` +
	`<usageReferences:adtObject adtcore:name="IS_AUTHORIZED" adtcore:type="CLAS/OM"/></usageReferences:referencedObject>` +
	`</usageReferences:referencedObjects></usageReferences:usageReferenceResult>`

func TestReferencesKeepsGradeDirectRowsWithoutIsResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "test-token")
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(references750XML))
		}
	}))
	defer srv.Close()

	out := referencesAnswer(t, srv, map[string]any{"object_url": "/sap/bc/adt/oo/classes/zcl_adt_http_gate"})
	if out["total"] != float64(2) {
		t.Errorf("the caller's method and the report are references, got total %v", out["total"])
	}
	if out["scaffolding"] != float64(4) {
		t.Errorf("two packages, one class container and the target's own part are not, got scaffolding %v", out["scaffolding"])
	}
	if out["note"] != nil {
		t.Errorf("a where-used list with callers must not say nothing uses it: %v", out["note"])
	}
}

// GetCallersOf reaches the where-used list through WhereUsed, not through the
// FindReferences handler, so the handler's retry never helped it: a
// /ddic/tables/ address answered 500 on 7.50, and TABL could not be named.
func TestCallersOfATableReachTheWorkbenchAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "test-token")
		if r.Method != http.MethodPost {
			return
		}
		if strings.HasPrefix(r.URL.Query().Get("uri"), "/sap/bc/adt/ddic/") {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`<exc:exception><message lang="EN">Error while converting object references</message></exc:exception>`))
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(references750XML))
	}))
	defer srv.Close()

	s := &Server{adtClient: adt.NewClient(srv.URL, "user", "pass")}
	for _, args := range []map[string]any{
		{"object_uri": "/sap/bc/adt/ddic/tables/zala_change_log"},
		{"object_type": "TABL", "object_name": "zala_change_log"},
	} {
		var req mcp.CallToolRequest
		req.Params.Arguments = args
		answer, err := s.callGraphAnswer(context.Background(), req, "callers")
		if err != nil {
			t.Errorf("%v: %v", args, err)
			continue
		}
		if answer["total"] != 1 {
			t.Errorf("%v: the workbench address knows ZCL_VSP_APC_HANDLER, got %v", args, answer["callers"])
		}
	}
}

// The callers graph hangs each caller under the target, and flattening it as
// parent->child said ZCL_ADT_HTTP_GATE calls ZCL_VSP_APC_HANDLER: backwards.
func TestAnalyzeCallGraphCallersPointAtTheTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "test-token")
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/xml")
			w.Write([]byte(references750XML))
		}
	}))
	defer srv.Close()

	s := &Server{adtClient: adt.NewClient(srv.URL, "user", "pass")}
	var req mcp.CallToolRequest
	req.Params.Arguments = map[string]any{"object_type": "CLAS", "object_name": "ZCL_ADT_HTTP_GATE", "direction": "callers"}
	result, err := s.handleAnalyzeCallGraph(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Edges []adt.CallGraphEdge `json:"edges"`
	}
	if err := json.Unmarshal([]byte(toolResultText(t, result)), &out); err != nil {
		t.Fatalf("the answer should be JSON: %v", err)
	}
	if len(out.Edges) != 1 || out.Edges[0].CallerName != "ZCL_VSP_APC_HANDLER" || out.Edges[0].CalleeName != "ZCL_ADT_HTTP_GATE" {
		t.Fatalf("want ZCL_VSP_APC_HANDLER -> ZCL_ADT_HTTP_GATE, got %+v", out.Edges)
	}
	if out.Edges[0].CallerURI != "/sap/bc/adt/oo/classes/zcl_vsp_apc_handler" || out.Edges[0].CalleeKind != "CLAS" {
		t.Errorf("the caller's URI and the target's kind belong on the edge, got %+v", out.Edges[0])
	}
}
