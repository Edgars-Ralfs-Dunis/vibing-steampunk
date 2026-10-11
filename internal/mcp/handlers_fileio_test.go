package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// RWF 29: ImportFromFile's schema says package_name is not needed for class
// includes (nor for any object that exists), but the shared handler refused
// every call without it before looking at the file. A package is needed only
// to create, so only the create path asks for it.
func TestDeployFromFileNeedsPackageOnlyToCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/discovery") {
			w.Header().Set("X-CSRF-Token", "TOKEN")
			return
		}
		w.WriteHeader(http.StatusNotFound) // nothing exists
	}))
	defer srv.Close()
	s := NewServer(&Config{BaseURL: srv.URL, Client: "001", Username: "u", Password: "p", Mode: "hyperfocused"})

	dir := t.TempDir()
	call := func(name, src string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := s.handleDeployFromFile(context.Background(), newRequest(map[string]any{"file_path": path}))
		if err != nil {
			t.Fatal(err)
		}
		return res.Content[0].(mcp.TextContent).Text
	}

	if got := call("zcl_x.clas.testclasses.abap", "CLASS ltc_x DEFINITION FOR TESTING.\nENDCLASS.\nCLASS ltc_x IMPLEMENTATION.\nENDCLASS.\n"); strings.Contains(got, "package_name is required") {
		t.Errorf("a class include was refused for want of a package: %s", got)
	}
	if got := call("znew.prog.abap", "REPORT znew.\n"); !strings.Contains(got, "package_name") {
		t.Errorf("creating a new object without a package must ask for one, got: %s", got)
	}
}
