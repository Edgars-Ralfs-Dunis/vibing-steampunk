package adt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// The CTS listing endpoint does not answer with one shape, and encoding/xml
// answers a path that does not match with an empty struct and a nil error.
// Both parsers hardcoded one shape each, so a system that sent either of the
// other two got "no transports found" (#111, #140).

// treeWithTargets is what the endpoint sends when transport targets exist:
// workbench > target > modifiable > request.
const treeWithTargets = `<?xml version="1.0" encoding="utf-8"?>
<tm:root xmlns:tm="http://www.sap.com/cts/adt/tm" xmlns:adtcore="http://www.sap.com/adt/core">
  <tm:workbench tm:desc="Workbench">
    <tm:target tm:name="/ZDEMO/" tm:desc="Demo layer">
      <tm:modifiable tm:desc="Modifiable">
        <tm:request tm:number="TR-EXAMPLE-1" tm:owner="TESTUSER" tm:desc="First"
                    tm:type="K" tm:status="D" tm:status_text="Modifiable"
                    tm:lastchanged_timestamp="20260415195636" tm:source_client="001">
          <tm:task tm:number="TR-EXAMPLE-2" tm:owner="TESTUSER" tm:desc="Task" tm:status="D">
            <tm:abap_object tm:pgmid="R3TR" tm:type="CLAS" tm:name="ZCL_DEMO_ONE"
                            tm:obj_info="Class ZCL_DEMO_ONE"/>
          </tm:task>
        </tm:request>
      </tm:modifiable>
      <tm:released tm:desc="Released">
        <tm:request tm:number="TR-EXAMPLE-9" tm:owner="TESTUSER" tm:desc="Gone"
                    tm:type="K" tm:status="R"/>
      </tm:released>
    </tm:target>
  </tm:workbench>
  <tm:customizing tm:desc="Customizing">
    <tm:target tm:name="/ZDEMO/">
      <tm:modifiable>
        <tm:request tm:number="TR-EXAMPLE-3" tm:owner="TESTUSER" tm:desc="Cust"
                    tm:type="W" tm:status="D"/>
      </tm:modifiable>
    </tm:target>
  </tm:customizing>
</tm:root>`

// treeWithoutTargets is the same listing on a system with no configured
// transport routes: the target level is simply absent.
const treeWithoutTargets = `<?xml version="1.0" encoding="utf-8"?>
<tm:root xmlns:tm="http://www.sap.com/cts/adt/tm">
  <tm:workbench tm:desc="Workbench">
    <tm:modifiable tm:desc="Modifiable">
      <tm:request tm:number="TR-EXAMPLE-1" tm:owner="TESTUSER" tm:desc="First"/>
    </tm:modifiable>
  </tm:workbench>
  <tm:customizing>
    <tm:modifiable>
      <tm:request tm:number="TR-EXAMPLE-3" tm:owner="TESTUSER" tm:desc="Cust"/>
    </tm:modifiable>
  </tm:customizing>
</tm:root>`

// flatTree is the single-request document: tm:request straight under tm:root.
const flatTree = `<?xml version="1.0" encoding="utf-8"?>
<tm:root xmlns:tm="http://www.sap.com/cts/adt/tm">
  <tm:request tm:number="TR-EXAMPLE-1" tm:owner="TESTUSER" tm:desc="First"
              tm:type="K" tm:status="D" tm:status_text="Modifiable"/>
</tm:root>`

func TestParseUserTransportsWithoutTargetLevel(t *testing.T) {
	result, err := parseUserTransports([]byte(treeWithoutTargets))
	if err != nil {
		t.Fatalf("parseUserTransports: %v", err)
	}
	if len(result.Workbench) != 1 {
		t.Fatalf("expected 1 workbench request from a target-less tree, got %d", len(result.Workbench))
	}
	if result.Workbench[0].Number != "TR-EXAMPLE-1" {
		t.Errorf("expected TR-EXAMPLE-1, got %q", result.Workbench[0].Number)
	}
	if result.Workbench[0].Type != "workbench" {
		t.Errorf("expected type 'workbench', got %q", result.Workbench[0].Type)
	}
	if len(result.Customizing) != 1 {
		t.Fatalf("expected 1 customizing request, got %d", len(result.Customizing))
	}
}

func TestParseUserTransportsFlatRoot(t *testing.T) {
	result, err := parseUserTransports([]byte(flatTree))
	if err != nil {
		t.Fatalf("parseUserTransports: %v", err)
	}
	if len(result.Workbench) != 1 {
		t.Fatalf("expected 1 workbench request from a flat tree, got %d", len(result.Workbench))
	}
	if result.Workbench[0].Owner != "TESTUSER" {
		t.Errorf("expected owner TESTUSER, got %q", result.Workbench[0].Owner)
	}
}

func TestParseUserTransportsKeepsTargetAndTasks(t *testing.T) {
	result, err := parseUserTransports([]byte(treeWithTargets))
	if err != nil {
		t.Fatalf("parseUserTransports: %v", err)
	}
	if len(result.Workbench) != 2 {
		t.Fatalf("expected 2 workbench requests (modifiable + released), got %d", len(result.Workbench))
	}
	first := result.Workbench[0]
	if first.Target != "/ZDEMO/" {
		t.Errorf("expected target /ZDEMO/ from the enclosing element, got %q", first.Target)
	}
	if len(first.Tasks) != 1 || len(first.Tasks[0].Objects) != 1 {
		t.Fatalf("expected 1 task with 1 object, got %d tasks", len(first.Tasks))
	}
	if got := first.Tasks[0].Objects[0].Name; got != "ZCL_DEMO_ONE" {
		t.Errorf("expected object ZCL_DEMO_ONE, got %q", got)
	}
	if len(result.Customizing) != 1 {
		t.Fatalf("expected 1 customizing request, got %d", len(result.Customizing))
	}
}

func TestParseTransportListNestedTree(t *testing.T) {
	transports, err := parseTransportList([]byte(treeWithTargets))
	if err != nil {
		t.Fatalf("parseTransportList: %v", err)
	}
	// The released request is not part of a modifiable listing.
	if len(transports) != 2 {
		t.Fatalf("expected 2 modifiable transports from a nested tree, got %d", len(transports))
	}
	got := transports[0]
	if got.Number != "TR-EXAMPLE-1" {
		t.Errorf("expected TR-EXAMPLE-1, got %q", got.Number)
	}
	if got.Target != "/ZDEMO/" {
		t.Errorf("expected target /ZDEMO/, got %q", got.Target)
	}
	if got.Type != "K" || got.Status != "D" || got.StatusText != "Modifiable" {
		t.Errorf("expected K/D/Modifiable, got %q/%q/%q", got.Type, got.Status, got.StatusText)
	}
	if got.ChangedAt != "20260415195636" {
		t.Errorf("expected changedAt 20260415195636, got %q", got.ChangedAt)
	}
	for _, tr := range transports {
		if tr.Number == "TR-EXAMPLE-9" {
			t.Errorf("released request TR-EXAMPLE-9 leaked into the modifiable listing")
		}
	}
}

func TestParseTransportListWithoutTargetLevel(t *testing.T) {
	transports, err := parseTransportList([]byte(treeWithoutTargets))
	if err != nil {
		t.Fatalf("parseTransportList: %v", err)
	}
	if len(transports) != 2 {
		t.Fatalf("expected 2 transports from a target-less tree, got %d", len(transports))
	}
	// Type and status come from the levels when the attributes are absent.
	if transports[0].Type != "K" || transports[0].Status != "D" {
		t.Errorf("expected K/D inferred from workbench+modifiable, got %q/%q",
			transports[0].Type, transports[0].Status)
	}
	if transports[1].Type != "W" {
		t.Errorf("expected W inferred from the customizing section, got %q", transports[1].Type)
	}
}

func TestParseTransportListFlatRoot(t *testing.T) {
	// The one shape that already worked. It must keep working.
	transports, err := parseTransportList([]byte(flatTree))
	if err != nil {
		t.Fatalf("parseTransportList: %v", err)
	}
	if len(transports) != 1 || transports[0].Number != "TR-EXAMPLE-1" {
		t.Fatalf("expected the flat shape to still parse, got %+v", transports)
	}
}

func TestParseUserTransportsUndeclaredPrefix(t *testing.T) {
	// Namespaces are resolved by the decoder, not by string surgery on the
	// document, so an undeclared tm: prefix parses too.
	doc := strings.ReplaceAll(treeWithoutTargets, ` xmlns:tm="http://www.sap.com/cts/adt/tm"`, "")
	result, err := parseUserTransports([]byte(doc))
	if err != nil {
		t.Fatalf("parseUserTransports: %v", err)
	}
	if len(result.Workbench) != 1 {
		t.Fatalf("expected 1 workbench request, got %d", len(result.Workbench))
	}
}

func TestAs4userPredicate(t *testing.T) {
	tests := []struct {
		name    string
		user    string
		want    string
		wantErr bool
	}{
		{"wildcard drops the predicate", "*", "", false},
		{"named user is an equality", "TESTUSER", "e070~AS4USER = 'TESTUSER'", false},
		{"lowercase is upper-cased", "testuser", "e070~AS4USER = 'TESTUSER'", false},
		{"prefix wildcard becomes LIKE", "TEST*", "e070~AS4USER LIKE 'TEST%'", false},
		{"empty user is refused", "", "", true},
		{"blank user is refused", "   ", "", true},
		{"a name that could close the literal is refused", "X' OR '1'='1", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := as4userPredicate(tt.user)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q, got predicate %q", tt.user, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.user, err)
			}
			if got != tt.want {
				t.Errorf("as4userPredicate(%q) = %q, want %q", tt.user, got, tt.want)
			}
		})
	}
}

// --- End-to-end over httptest: no SAP system is contacted. ---

// ctsServer answers the CTS listing with a document the old parsers could not
// navigate, and the E070 fallback query with two rows.
type ctsServer struct {
	mu        sync.Mutex
	listing   string
	e070      string // E070 rows to answer with; e070Rows when empty
	queries   []string
	sqlCalls  int
	treeCalls int
}

func (s *ctsServer) start(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/discovery"):
			w.Header().Set("X-CSRF-Token", "TOKEN")
			w.WriteHeader(http.StatusOK)

		case strings.Contains(r.URL.Path, "/cts/transportrequests"):
			s.mu.Lock()
			s.treeCalls++
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(s.listing))

		case strings.Contains(r.URL.Path, "/datapreview/freestyle"):
			body := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(body)
			s.mu.Lock()
			s.queries = append(s.queries, string(body))
			s.sqlCalls++
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(firstNonEmpty(s.e070, e070Rows)))

		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := NewConfig(srv.URL, "TESTUSER", "secret")
	cfg.Safety.EnableTransports = true
	return NewClientWithTransport(cfg, NewTransport(cfg))
}

func (s *ctsServer) lastQuery() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queries) == 0 {
		return ""
	}
	return s.queries[len(s.queries)-1]
}

const e070Rows = `<?xml version="1.0" encoding="utf-8"?>
<dataPreview:tableData xmlns:dataPreview="http://www.sap.com/adt/dataPreview">
  <dataPreview:columns>
    <dataPreview:metadata dataPreview:name="TRKORR" dataPreview:type="C" dataPreview:length="20"/>
    <dataPreview:dataSet>
      <dataPreview:data>TR-EXAMPLE-1</dataPreview:data>
      <dataPreview:data>TR-EXAMPLE-3</dataPreview:data>
    </dataPreview:dataSet>
  </dataPreview:columns>
  <dataPreview:columns>
    <dataPreview:metadata dataPreview:name="AS4USER" dataPreview:type="C" dataPreview:length="12"/>
    <dataPreview:dataSet>
      <dataPreview:data>TESTUSER</dataPreview:data>
      <dataPreview:data>OTHERUSER</dataPreview:data>
    </dataPreview:dataSet>
  </dataPreview:columns>
  <dataPreview:columns>
    <dataPreview:metadata dataPreview:name="TRFUNCTION" dataPreview:type="C" dataPreview:length="1"/>
    <dataPreview:dataSet>
      <dataPreview:data>K</dataPreview:data>
      <dataPreview:data>W</dataPreview:data>
    </dataPreview:dataSet>
  </dataPreview:columns>
  <dataPreview:columns>
    <dataPreview:metadata dataPreview:name="TRSTATUS" dataPreview:type="C" dataPreview:length="1"/>
    <dataPreview:dataSet>
      <dataPreview:data>D</dataPreview:data>
      <dataPreview:data>D</dataPreview:data>
    </dataPreview:dataSet>
  </dataPreview:columns>
</dataPreview:tableData>`

// The whole of #111: the two tools were asked the same question on the same
// connection and only one of them answered.
func TestGetUserTransportsAgreesWithListTransports(t *testing.T) {
	// A shape neither parser handled, so both have to reach the fallback.
	srv := &ctsServer{listing: `<?xml version="1.0"?><tm:root xmlns:tm="http://www.sap.com/cts/adt/tm"/>`}
	client := srv.start(t)
	ctx := context.Background()

	list, err := client.ListTransports(ctx, "TESTUSER")
	if err != nil {
		t.Fatalf("ListTransports: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 transports from the E070 fallback, got %d", len(list))
	}

	user, err := client.GetUserTransports(ctx, "TESTUSER")
	if err != nil {
		t.Fatalf("GetUserTransports: %v", err)
	}
	total := len(user.Workbench) + len(user.Customizing)
	if total != len(list) {
		t.Fatalf("GetUserTransports returned %d requests where ListTransports returned %d "+
			"on the same connection (workbench=%d customizing=%d)",
			total, len(list), len(user.Workbench), len(user.Customizing))
	}
	if len(user.Workbench) != 1 || user.Workbench[0].Number != "TR-EXAMPLE-1" {
		t.Errorf("expected the K request in workbench, got %+v", user.Workbench)
	}
	if len(user.Customizing) != 1 || user.Customizing[0].Number != "TR-EXAMPLE-3" {
		t.Errorf("expected the W request in customizing, got %+v", user.Customizing)
	}
}

// GetUserTransports must not reach for SQL when the tree answered.
func TestGetUserTransportsPrefersTheTree(t *testing.T) {
	srv := &ctsServer{listing: treeWithoutTargets}
	client := srv.start(t)

	result, err := client.GetUserTransports(context.Background(), "TESTUSER")
	if err != nil {
		t.Fatalf("GetUserTransports: %v", err)
	}
	if len(result.Workbench) != 1 || len(result.Customizing) != 1 {
		t.Fatalf("expected 1 workbench and 1 customizing, got %d/%d",
			len(result.Workbench), len(result.Customizing))
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.sqlCalls != 0 {
		t.Errorf("expected no SQL fallback when the tree answered, got %d queries", srv.sqlCalls)
	}
}

// #140: '*' is what Eclipse ADT means by "every user".
func TestListTransportsWildcardUser(t *testing.T) {
	srv := &ctsServer{listing: `<?xml version="1.0"?><tm:root xmlns:tm="http://www.sap.com/cts/adt/tm"/>`}
	client := srv.start(t)

	list, err := client.ListTransports(context.Background(), "*")
	if err != nil {
		t.Fatalf("ListTransports('*'): %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected every user's transports, got %d", len(list))
	}

	query := srv.lastQuery()
	if strings.Contains(query, "AS4USER = '*'") {
		t.Errorf("the wildcard reached the database as a literal:\n%s", query)
	}
	if strings.Contains(query, "AS4USER =") || strings.Contains(query, "AS4USER LIKE") {
		t.Errorf("expected no AS4USER predicate for the wildcard:\n%s", query)
	}
	owners := map[string]bool{}
	for _, tr := range list {
		owners[tr.Owner] = true
	}
	if !owners["TESTUSER"] || !owners["OTHERUSER"] {
		t.Errorf("expected transports from more than one owner, got %v", owners)
	}
	// F3-06: '*' is the sql source's alone; asking the tree for every user's
	// requests made a 7.50 system build them all until the client timed out.
	if srv.treeCalls != 0 {
		t.Errorf("'*' asked the organizer tree %d times", srv.treeCalls)
	}
}

func TestListTransportsNamedUserStillFilters(t *testing.T) {
	srv := &ctsServer{listing: `<?xml version="1.0"?><tm:root xmlns:tm="http://www.sap.com/cts/adt/tm"/>`}
	client := srv.start(t)

	if _, err := client.ListTransports(context.Background(), "TESTUSER"); err != nil {
		t.Fatalf("ListTransports: %v", err)
	}
	if q := srv.lastQuery(); !strings.Contains(q, "AS4USER = 'TESTUSER'") {
		t.Errorf("expected an AS4USER equality for a named user:\n%s", q)
	}
}

// RWF 8: 7.50 answers GET /cts/transportrequests/<N> with the user's whole
// organizer tree (captured on a 7.50 system), so the request sits at
// tm:root/workbench/modifiable/request, next to the user's other requests.
func TestGetTransportFindsTheRequestInA750Tree(t *testing.T) {
	body, err := os.ReadFile("testdata/gettransport-750.xml")
	if err != nil {
		t.Fatal(err)
	}
	client := (&ctsServer{listing: string(body)}).start(t)

	got, err := client.GetTransport(context.Background(), "dvdk9adspp")
	if err != nil {
		t.Fatalf("GetTransport: %v", err)
	}
	if got.Number != "DVDK9ADSPP" || got.Owner != "DEVU" || got.Description != "Example development request" {
		t.Errorf("picked the wrong request: %+v", got.TransportSummary)
	}
	if got.Type != "K" || got.Status != "D" {
		t.Errorf("type/status = %q/%q, want K/D", got.Type, got.Status)
	}
	if len(got.Tasks) != 1 || got.Tasks[0].Number != "DVDK9ADSPQ" || len(got.Tasks[0].Objects) != 2 {
		t.Fatalf("expected task DVDK9ADSPQ with 2 objects, got %+v", got.Tasks)
	}
	if o := got.Tasks[0].Objects[0]; o.PgmID != "R3TR" || o.Type != "SUSC" || o.Name != "ZADT" {
		t.Errorf("first task object = %+v", o)
	}

	// The other request in the same body carries objects on the request itself.
	other, err := client.GetTransport(context.Background(), "DVDK9ABQUJ")
	if err != nil {
		t.Fatalf("GetTransport DVDK9ABQUJ: %v", err)
	}
	if len(other.Objects) != 2 || len(other.Tasks) != 1 || other.Tasks[0].Number != "DVDK9ABQUK" {
		t.Errorf("DVDK9ABQUJ: %d objects, tasks %+v", len(other.Objects), other.Tasks)
	}

	// A request the tree does not hold names what came back instead.
	_, err = client.GetTransport(context.Background(), "DVDK9XXXXX")
	if err == nil || !strings.Contains(err.Error(), "DVDK9XXXXX") || !strings.Contains(err.Error(), "<root>") ||
		!strings.Contains(err.Error(), `tm:root xmlns:adtcore`) {
		t.Errorf("miss error should name the number, the root element and the body start, got %v", err)
	}
}

// RWF 17: GetUserTransports and GetTransportInfo only read, so they open on
// the same flag ListTransports and GetTransport do; a refusal names the flag.
func TestTransportReadsOpenWithTransportableEdits(t *testing.T) {
	client := (&ctsServer{listing: treeWithoutTargets}).start(t)
	client.config.Safety = SafetyConfig{AllowTransportableEdits: true}
	ctx := context.Background()

	if _, err := client.GetUserTransports(ctx, "TESTUSER"); err != nil {
		t.Errorf("GetUserTransports refused under --allow-transportable-edits: %v", err)
	}
	if _, err := client.GetTransportInfo(ctx, "/sap/bc/adt/programs/programs/zdemo", "ZDEMO"); err != nil && strings.Contains(err.Error(), "blocked") {
		t.Errorf("GetTransportInfo refused under --allow-transportable-edits: %v", err)
	}

	client.config.Safety = SafetyConfig{}
	for name, call := range map[string]func() error{
		"GetUserTransports": func() error { _, err := client.GetUserTransports(ctx, "TESTUSER"); return err },
		"GetTransportInfo":  func() error { _, err := client.GetTransportInfo(ctx, "/x", "ZDEMO"); return err },
		"CreateTransport":   func() error { _, err := client.CreateTransport(ctx, "/x", "d", "ZDEMO"); return err },
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "SAP_ENABLE_TRANSPORTS") {
			t.Errorf("%s: refusal should name the missing flag, got %v", name, err)
		}
	}
}

// F3-05: E070 knows a transport of copies and when a request last changed;
// the flat listing lost both on the way through the grouped form.
func TestListTransportsSQLKeepsTypeTAndChangedAt(t *testing.T) {
	srv := &ctsServer{listing: `<?xml version="1.0"?><tm:root xmlns:tm="http://www.sap.com/cts/adt/tm"/>`, e070: `<?xml version="1.0" encoding="utf-8"?>
<dataPreview:tableData xmlns:dataPreview="http://www.sap.com/adt/dataPreview">
  <dataPreview:columns><dataPreview:metadata dataPreview:name="TRKORR"/><dataPreview:dataSet><dataPreview:data>TR-EXAMPLE-7</dataPreview:data></dataPreview:dataSet></dataPreview:columns>
  <dataPreview:columns><dataPreview:metadata dataPreview:name="TRFUNCTION"/><dataPreview:dataSet><dataPreview:data>T</dataPreview:data></dataPreview:dataSet></dataPreview:columns>
  <dataPreview:columns><dataPreview:metadata dataPreview:name="TRSTATUS"/><dataPreview:dataSet><dataPreview:data>D</dataPreview:data></dataPreview:dataSet></dataPreview:columns>
  <dataPreview:columns><dataPreview:metadata dataPreview:name="AS4DATE"/><dataPreview:dataSet><dataPreview:data>20260315</dataPreview:data></dataPreview:dataSet></dataPreview:columns>
  <dataPreview:columns><dataPreview:metadata dataPreview:name="AS4TIME"/><dataPreview:dataSet><dataPreview:data>171004</dataPreview:data></dataPreview:dataSet></dataPreview:columns>
</dataPreview:tableData>`}
	client := srv.start(t)

	list, err := client.ListTransports(context.Background(), "TESTUSER")
	if err != nil {
		t.Fatalf("ListTransports: %v", err)
	}
	if len(list) != 1 || list[0].Type != "T" || list[0].ChangedAt != "20260315171004" || list[0].TargetDesc == "" {
		t.Errorf("want one T row with changedAt and targetDesc, got %+v", list)
	}
}

// F3-02: 7.50 ignores requestType, requestStatus and the released dates and
// answers with the user's whole tree; the listing echoed the filters as
// applied. Type and status are applied here now, and a released window or a
// request for transports of copies, which the tree cannot answer, goes to E070.
func TestQueryTransportsAppliesFiltersTheTreeIgnored(t *testing.T) {
	client := (&ctsServer{listing: treeWithProjects}).start(t)
	ctx := context.Background()

	res, err := client.QueryTransports(ctx, TransportQuery{User: "DEVELOPER", Source: TransportSourceParams, RequestTypes: "W", RequestStatuses: "D"})
	if err != nil {
		t.Fatalf("QueryTransports: %v", err)
	}
	rows := res.Flat()
	if len(rows) != 1 || rows[0].Number != "TR-CUS-1" {
		t.Errorf("want only the modifiable W request, got %+v", rows)
	}
	if !strings.Contains(strings.Join(res.Notes, "\n"), "dropped here") {
		t.Errorf("dropping rows must be noted, got %v", res.Notes)
	}

	for _, q := range []TransportQuery{
		{User: "DEVELOPER", RequestTypes: "T"},
		{User: "DEVELOPER", RequestStatuses: "R", ReleasedFrom: "20260801", ReleasedTo: "20260831"},
	} {
		res, err := client.QueryTransports(ctx, q)
		if err != nil {
			t.Fatalf("QueryTransports(%+v): %v", q, err)
		}
		if res.Source != TransportSourceSQL {
			t.Errorf("%+v: source = %s, want sql (notes %v)", q, res.Source, res.Notes)
		}
	}
}
