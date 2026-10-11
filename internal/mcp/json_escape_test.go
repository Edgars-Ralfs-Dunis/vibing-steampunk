package mcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// encoding/json escapes & < > by default, so a T100 text "error: &1" reached
// the caller as "error: &1".
func TestToolJSONKeepsAmpersandAndAngleBrackets(t *testing.T) {
	if got := resultText(newToolResultJSON(map[string]string{"text": "Data deletion error: &1 <x> ->"})); !strings.Contains(got, "&1 <x> ->") {
		t.Errorf("newToolResultJSON escaped HTML characters: %s", got)
	}

	sap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "T")
		io.WriteString(w, `<dataPreview:tableData xmlns:dataPreview="http://www.sap.com/adt/dataPreview"><dataPreview:columns><dataPreview:metadata dataPreview:name="TEXT"/><dataPreview:dataSet><dataPreview:data>Data deletion error: &amp;1</dataPreview:data></dataPreview:dataSet></dataPreview:columns></dataPreview:tableData>`)
	}))
	defer sap.Close()
	s := NewServer(&Config{BaseURL: sap.URL, Username: "TESTUSER", Client: "050", Mode: "expert"})
	result, err := s.handleRunQuery(t.Context(), newRequest(map[string]any{"sql_query": "SELECT text FROM t100"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resultText(result); !strings.Contains(got, "Data deletion error: &1") {
		t.Errorf("RunQuery escaped the ampersand:\n%s", got)
	}
}
