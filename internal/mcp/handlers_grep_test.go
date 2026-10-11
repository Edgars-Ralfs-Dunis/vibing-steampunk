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

// F1-03: SAP answers a package that does not exist with an empty tree, the
// same as an empty package; GetPackage must say which one it is.
func TestGetPackageSaysAMissingPackageDoesNotExist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-CSRF-Token", "test-token")
		switch r.URL.Path {
		case "/sap/bc/adt/repository/nodestructure":
			w.Write([]byte(`<asx:abap xmlns:asx="http://www.sap.com/abapxml"><asx:values><DATA><TREE_CONTENT/></DATA></asx:values></asx:abap>`))
		case "/sap/bc/adt/packages/ZEMPTY":
			w.Write([]byte(`<pak:package/>`))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()
	s := &Server{config: &Config{}, adtClient: adt.NewClient(srv.URL, "user", "pass")}

	res, err := s.handleGetPackage(context.Background(), newRequest(map[string]any{"package_name": "ZZZ_NO_SUCH_PKG"}))
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(resultText(res), "does not exist") {
		t.Fatalf("want a does-not-exist error, got IsError=%v: %s", res.IsError, resultText(res))
	}

	res, err = s.handleGetPackage(context.Background(), newRequest(map[string]any{"package_name": "ZEMPTY"}))
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("an existing empty package is an answer, got: %s", resultText(res))
	}
}
