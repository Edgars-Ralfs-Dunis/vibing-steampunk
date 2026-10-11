package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/oisee/vibing-steampunk/pkg/adt"
)

// dumpFeed750Server answers the dumps feed with a real 7.50 body (one entry,
// captured 2026-10-11, names anonymised) and 404s the detail resource, which
// is what 7.50 does.
func dumpFeed750Server(t *testing.T) *Server {
	t.Helper()
	feed, err := os.ReadFile("testdata/dumps-feed-750.xml")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "TOKEN")
		if r.URL.Path == "/sap/bc/adt/runtime/dumps" {
			w.Header().Set("Content-Type", "application/atom+xml")
			_, _ = w.Write(feed)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return NewServer(&Config{BaseURL: srv.URL, Username: "TESTUSER", Client: "001", Mode: "expert"})
}

// 7.50 serves no /runtime/dump/<id>/formatted, so GetDump answered with the
// feed fields and a note — while the feed entry's own summary already said
// where the program died.
func TestGetDumpReadsTheFeedSummaryWhenTheDetailIsMissing(t *testing.T) {
	s := dumpFeed750Server(t)
	res, err := s.handleGetDump(t.Context(), newRequest(map[string]any{"dump_id": "latest"}))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Detail *adt.DumpDetail `json:"detail"`
		Source string          `json:"source"`
	}
	if err := json.Unmarshal([]byte(resultText(res)), &got); err != nil {
		t.Fatalf("%v: %s", err, resultText(res))
	}
	if got.Detail == nil {
		t.Fatalf("no detail: %s", resultText(res))
	}
	d := got.Detail
	want := adt.DumpDetail{
		ShortText:   `Conversion of type "STRING" to type "DEEP_STRUCTURE" not supported.`,
		ErrorType:   "OBJECTS_MOVE_NOT_SUPPORTED",
		Program:     "ZCL_VSP_RFC_SERVICE===========CP",
		Include:     "ZCL_VSP_RFC_SERVICE===========CM007",
		Line:        46,
		Procedure:   "HANDLE_CALL",
		MainProgram: "SAPMHTTP",
	}
	if d.ShortText != want.ShortText || d.ErrorType != want.ErrorType || d.Program != want.Program ||
		d.Include != want.Include || d.Line != want.Line || d.Procedure != want.Procedure || d.MainProgram != want.MainProgram {
		t.Errorf("detail = %+v\nwant     %+v", *d, want)
	}
	if got.Source != "feed summary" {
		t.Errorf("source = %q, want \"feed summary\"", got.Source)
	}
}
