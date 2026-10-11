package mcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A per-call client the user may not log on to answered "access forbidden
// (403): check user authorizations": no client, no host, and the access gate's
// plain-text reason thrown away with the body.
func TestPooledClientLogonRefusalNamesClientAndKeepsSAPReason(t *testing.T) {
	const reason = "ZADT access gate: user DEVU has no profile ZADT_ACC in client 090"
	sap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sap-client") == "090" {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, reason)
			return
		}
		w.Header().Set("x-csrf-token", "T")
	}))
	defer sap.Close()

	s := NewServer(&Config{BaseURL: sap.URL, Username: "DEVU", Client: "050", Mode: "expert"})
	result, err := s.handleRunQuery(t.Context(), newRequest(map[string]any{"sql_query": "SELECT COUNT( * ) AS cnt FROM t500p", "client": "090"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	for _, want := range []string{"client 090", "127.0.0.1", "403", reason} {
		if !strings.Contains(text, want) {
			t.Errorf("refusal does not carry %q:\n%s", want, text)
		}
	}
}
