package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A bridge that cannot be reached and a keyword SAP has no documentation for
// used to read the same: the URL and nothing else.
func TestHandleGetAbapHelpSaysWhyThereIsNoSystemDocumentation(t *testing.T) {
	sap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "T")
		w.WriteHeader(http.StatusNotFound)
	}))
	defer sap.Close()

	s := NewServer(&Config{BaseURL: sap.URL, Username: "TESTUSER", Client: "050", Mode: "expert"})
	result, err := s.handleGetAbapHelp(t.Context(), newRequest(map[string]any{"keyword": "LOOP"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text := resultText(result); !strings.Contains(text, "bridge") {
		t.Errorf("answer does not say the ZADT_VSP bridge was not reached:\n%s", text)
	}
}
