package adt

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The vit/wb TRAN URI has no mapping on 7.50 (500 "No URI-Mapping defined",
// upstream #232); TSTC and TSTCT through the data preview answer everywhere.
func TestGetTransactionReadsTSTC(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-CSRF-Token", "TOKEN")
		switch {
		case strings.Contains(r.URL.Path, "/vit/wb/"):
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, `<exc:exception xmlns:exc="http://www.sap.com/abapxml/types/communicationframework"><type id="uriMappingError"/><localizedMessage lang="EN">No URI-Mapping defined</localizedMessage></exc:exception>`)
		case strings.Contains(r.URL.Path, "/datapreview/"):
			b, _ := io.ReadAll(r.Body)
			q := string(b)
			queries = append(queries, q)
			switch {
			case strings.Contains(q, "TSTCT"):
				io.WriteString(w, dpRow("TTEXT", "ABAP Editor"))
			case strings.Contains(q, "TSTC"):
				io.WriteString(w, dpRow("PGMNA", "RSABAPPROGRAM"))
			}
		}
	}))
	defer srv.Close()
	cfg := NewConfig(srv.URL, "u", "p")
	c := NewClientWithTransport(cfg, NewTransport(cfg))

	tr, err := c.GetTransaction(context.Background(), "se38")
	if err != nil {
		t.Fatalf("GetTransaction: %v", err)
	}
	if tr.Name != "SE38" || tr.Program != "RSABAPPROGRAM" || tr.Description != "ABAP Editor" {
		t.Errorf("got %+v", tr)
	}
	for _, q := range queries {
		for _, line := range strings.Split(q, "\n") {
			if len(line) > 255 {
				t.Errorf("statement line over the 255-char data preview limit: %q", line)
			}
		}
	}
}
