package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oisee/vibing-steampunk/pkg/adt"
)

// notFoundServer answers every request with SAP's 404 for a missing object.
func notFoundServer(t *testing.T) *Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-CSRF-Token", "test-token")
		http.Error(w, "No suitable resource found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return &Server{config: &Config{}, adtClient: adt.NewClient(srv.URL, "user", "pass")}
}

// F1-01: a grep that could read nothing is an error, not "no matches".
func TestGrepObjectsThatSearchedNothingIsAnError(t *testing.T) {
	s := notFoundServer(t)
	res, err := s.handleGrepObjects(context.Background(), newRequest(map[string]any{
		"object_urls": []any{"/sap/bc/adt/programs/programs/zz_vsp_no_such_prog"},
		"pattern":     ".",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(resultText(res), `"partial": true`) {
		t.Fatalf("want is_error with partial:true, got IsError=%v:\n%s", res.IsError, resultText(res))
	}
}
