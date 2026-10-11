package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oisee/vibing-steampunk/pkg/adt"
)

// rowNumberCaptureServer answers any datapreview request with a minimal but
// valid table-data XML body, and records the rowNumber query parameter each
// call actually sent — the thing this whole bug was about.
func rowNumberCaptureServer(t *testing.T) (*httptest.Server, *string) {
	t.Helper()
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Any ADT session that works hands out a CSRF token on every response;
		// the transport's HEAD/GET CSRF fetch fails without one before the
		// real POST (the one carrying rowNumber) is ever sent.
		w.Header().Set("x-csrf-token", "AbCdEfGhIjKlMnOpQrStUv==")
		if r.URL.Query().Has("rowNumber") {
			got = r.URL.Query().Get("rowNumber")
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><dataPreview:tableData xmlns:dataPreview="http://www.sap.com/adt/dataPreview"></dataPreview:tableData>`))
	}))
	return srv, &got
}

// probe is the rowNumber sent for want rows: one more, so that an answer
// that fills it is known to be capped.
func probe(want string) string {
	n, _ := strconv.Atoi(want)
	return strconv.Itoa(n + 1)
}

// A real MCP client's max_rows: 0 (and, it turned out, max_rows: -1 too)
// arrived at this server identically to the argument being omitted — see
// pkg/adt/limits.go's ResolveRowLimit doc comment for the live-verified
// evidence. all_rows is the boolean escape hatch that doesn't have a
// zero-like value for anything upstream to normalize away.
func TestHandleGetTableContentsRowLimit(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantRow string
	}{
		{
			name:    "all_rows true wins over max_rows",
			args:    map[string]any{"table_name": "TADIR", "all_rows": true, "max_rows": float64(5)},
			wantRow: fmt.Sprintf("%d", adt.UnlimitedRows),
		},
		{
			name:    "positive max_rows honored",
			args:    map[string]any{"table_name": "TADIR", "max_rows": float64(5000)},
			wantRow: "5000",
		},
		{
			name:    "neither set defaults to 100",
			args:    map[string]any{"table_name": "TADIR"},
			wantRow: "100",
		},
		{
			name:    "max_rows 0 without all_rows still defaults to 100 (documented MCP limitation)",
			args:    map[string]any{"table_name": "TADIR", "max_rows": float64(0)},
			wantRow: "100",
		},
		{
			// Regression (2026-08-31, live): fetchRows used to be
			// maxRows+offset unconditionally, so all_rows:true + offset:900
			// sent rowNumber=UnlimitedRows+900 — enough to cross ADT's real
			// cutoff and silently truncate the result, defeating all_rows
			// entirely. The sentinel is already the maximum; offset must not
			// be added on top of it.
			name:    "all_rows true ignores offset — sentinel already is the max",
			args:    map[string]any{"table_name": "TADIR", "all_rows": true, "offset": float64(900)},
			wantRow: fmt.Sprintf("%d", adt.UnlimitedRows),
		},
		{
			// The bounded path still needs +offset — that's deliberate
			// client-side pagination within an intentionally limited request.
			name:    "positive max_rows still composes with offset",
			args:    map[string]any{"table_name": "TADIR", "max_rows": float64(500), "offset": float64(900)},
			wantRow: "1400",
		},
		{
			// Above about 100,000 ddic silently answers ~100 rows (night
			// 2026-10-11 F2-03), so a larger request is clamped.
			name:    "max_rows above the ddic cutoff is clamped",
			args:    map[string]any{"table_name": "T000", "max_rows": float64(150000)},
			wantRow: fmt.Sprintf("%d", adt.UnlimitedRows),
		},
		{
			name:    "max_rows plus offset above the ddic cutoff is clamped",
			args:    map[string]any{"table_name": "T000", "max_rows": float64(500), "offset": float64(100000)},
			wantRow: fmt.Sprintf("%d", adt.UnlimitedRows),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sap, gotRowNumber := rowNumberCaptureServer(t)
			defer sap.Close()

			s := NewServer(&Config{BaseURL: sap.URL, Username: "TESTUSER", Client: "001", Mode: "expert"})
			result, err := s.handleGetTableContents(t.Context(), newRequest(tt.args))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if *gotRowNumber != probe(tt.wantRow) {
				t.Errorf("rowNumber sent to ADT = %q, want %q\nresult text: %s", *gotRowNumber, probe(tt.wantRow), resultText(result))
			}
		})
	}
}

func TestHandleRunQueryRowLimit(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantRow string
	}{
		{
			name:    "all_rows true wins over max_rows",
			args:    map[string]any{"sql_query": "SELECT * FROM TADIR", "all_rows": true, "max_rows": float64(5)},
			wantRow: fmt.Sprintf("%d", adt.UnlimitedRows),
		},
		{
			name:    "positive max_rows honored",
			args:    map[string]any{"sql_query": "SELECT * FROM TADIR", "max_rows": float64(5000)},
			wantRow: "5000",
		},
		{
			name:    "neither set defaults to 100",
			args:    map[string]any{"sql_query": "SELECT * FROM TADIR"},
			wantRow: "100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sap, gotRowNumber := rowNumberCaptureServer(t)
			defer sap.Close()

			s := NewServer(&Config{BaseURL: sap.URL, Username: "TESTUSER", Client: "001", Mode: "expert"})
			if _, err := s.handleRunQuery(t.Context(), newRequest(tt.args)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if *gotRowNumber != probe(tt.wantRow) {
				t.Errorf("rowNumber sent to ADT = %q, want %q", *gotRowNumber, probe(tt.wantRow))
			}
		})
	}
}

// The universal SAP() single-tool wrapper builds its own args map for these
// same handlers (routeReadAction in handlers_universal.go) and has to
// forward all_rows separately from max_rows — easy to add to one call site
// and forget the other three.
func TestRouteReadActionForwardsAllRows(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantRow string
	}{
		{
			name: "read TABL_CONTENTS forwards all_rows",
			args: map[string]any{
				"action": "read",
				"target": "TABL_CONTENTS TADIR",
				"params": map[string]any{"all_rows": true},
			},
			wantRow: fmt.Sprintf("%d", adt.UnlimitedRows),
		},
		{
			name: "query TABL_CONTENTS forwards all_rows",
			args: map[string]any{
				"action": "query",
				"target": "TABL_CONTENTS TADIR",
				"params": map[string]any{"all_rows": true},
			},
			wantRow: fmt.Sprintf("%d", adt.UnlimitedRows),
		},
		{
			name: "query with sql forwards all_rows",
			args: map[string]any{
				"action": "query",
				"params": map[string]any{"sql": "SELECT * FROM TADIR", "all_rows": true},
			},
			wantRow: fmt.Sprintf("%d", adt.UnlimitedRows),
		},
		{
			// parseTarget (handlers_universal.go) splits target on the first
			// space into objectType/objectName. A target with no space (e.g.
			// "TADIR") becomes objectType="TADIR", objectName="" — it does NOT
			// reach the "bare table name" branch. That branch is
			// `case "SQL", "":` with no sql_query and a non-empty objectName,
			// which requires a target with an (unused) leading word, e.g.
			// "SQL TADIR".
			name: "query with bare table target forwards all_rows",
			args: map[string]any{
				"action": "query",
				"target": "SQL TADIR",
				"params": map[string]any{"all_rows": true},
			},
			wantRow: fmt.Sprintf("%d", adt.UnlimitedRows),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sap, gotRowNumber := rowNumberCaptureServer(t)
			defer sap.Close()

			s := NewServer(&Config{BaseURL: sap.URL, Username: "TESTUSER", Client: "001", Mode: "hyperfocused"})
			if _, err := s.handleUniversalTool(t.Context(), newRequest(tt.args)); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if *gotRowNumber != probe(tt.wantRow) {
				t.Errorf("rowNumber sent to ADT = %q, want %q", *gotRowNumber, probe(tt.wantRow))
			}
		})
	}
}

// rowsServer plays the data preview over a table of total rows numbered 001,
// 002, .... Freestyle returns the rowNumber it was asked for; 7.50's ddic
// returns one row more (night 2026-10-11 F2-02).
func rowsServer(t *testing.T, total int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "AbCdEfGhIjKlMnOpQrStUv==")
		n, _ := strconv.Atoi(r.URL.Query().Get("rowNumber"))
		if strings.Contains(r.URL.Path, "/ddic") {
			n++
		}
		var data strings.Builder
		for i := 1; i <= min(n, total); i++ {
			fmt.Fprintf(&data, "<data>%03d</data>", i)
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<tableData><columns><metadata name="N"/><dataSet>%s</dataSet></columns></tableData>`, data.String())
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A capped answer must say so, and a page must hold max_rows rows, not one
// more that the next page repeats (night 2026-10-11 F2-04, F2-02).
func TestQueryAnswersSayWhenCapped(t *testing.T) {
	s := NewServer(&Config{BaseURL: rowsServer(t, 7).URL, Username: "TESTUSER", Client: "001", Mode: "expert"})
	tests := []struct {
		name      string
		handler   func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args      map[string]any
		want      string
		truncated bool
	}{
		{"RunQuery capped", s.handleRunQuery, map[string]any{"sql_query": "SELECT n FROM t", "max_rows": float64(3)}, "001 002 003", true},
		{"RunQuery complete", s.handleRunQuery, map[string]any{"sql_query": "SELECT n FROM t", "max_rows": float64(10)}, "001 002 003 004 005 006 007", false},
		{"GetTableContents page 1", s.handleGetTableContents, map[string]any{"table_name": "T", "max_rows": float64(3)}, "001 002 003", true},
		{"GetTableContents page 2", s.handleGetTableContents, map[string]any{"table_name": "T", "max_rows": float64(3), "offset": float64(3)}, "004 005 006", true},
		{"GetTableContents last page", s.handleGetTableContents, map[string]any{"table_name": "T", "max_rows": float64(3), "offset": float64(6)}, "007", false},
		{"GetTableContents complete", s.handleGetTableContents, map[string]any{"table_name": "T", "max_rows": float64(10)}, "001 002 003 004 005 006 007", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.handler(t.Context(), newRequest(tt.args))
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Rows      []map[string]string
				Truncated string `json:"truncated"`
			}
			if err := json.Unmarshal([]byte(resultText(result)), &got); err != nil {
				t.Fatalf("%v\n%s", err, resultText(result))
			}
			var rows []string
			for _, r := range got.Rows {
				rows = append(rows, r["N"])
			}
			if strings.Join(rows, " ") != tt.want {
				t.Errorf("rows = %v, want %s", rows, tt.want)
			}
			if (got.Truncated != "") != tt.truncated {
				t.Errorf("truncated = %q, want a note: %v", got.Truncated, tt.truncated)
			}
		})
	}
}
