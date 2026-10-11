package mcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A package that holds only types the 7.50 SE80 tree does not list (SICF,
// SUSC) came back as an empty package with no word about it, and every
// answer led with empty XMLName/URI/Type fields.
func TestHandleGetPackageNamesWhatNodestructureLeftOut(t *testing.T) {
	var sql string
	sap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-csrf-token", "T")
		switch {
		case strings.Contains(r.URL.Path, "/repository/nodestructure"):
			io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?><asx:abap xmlns:asx="http://www.sap.com/abapxml" version="1.0"><asx:values><DATA><TREE_CONTENT/></DATA></asx:values></asx:abap>`)
		case strings.Contains(r.URL.Path, "/datapreview/freestyle"):
			b, _ := io.ReadAll(r.Body)
			sql = string(b)
			io.WriteString(w, `<dataPreview:tableData xmlns:dataPreview="http://www.sap.com/adt/dataPreview">`+
				`<dataPreview:columns><dataPreview:metadata dataPreview:name="OBJECT"/><dataPreview:dataSet><dataPreview:data>SICF</dataPreview:data><dataPreview:data>SUSC</dataPreview:data></dataPreview:dataSet></dataPreview:columns>`+
				`<dataPreview:columns><dataPreview:metadata dataPreview:name="OBJ_NAME"/><dataPreview:dataSet><dataPreview:data>ZGW_SRV</dataPreview:data><dataPreview:data>ZGW</dataPreview:data></dataPreview:dataSet></dataPreview:columns>`+
				`</dataPreview:tableData>`)
		}
	}))
	defer sap.Close()

	s := NewServer(&Config{BaseURL: sap.URL, Username: "TESTUSER", Client: "050", Mode: "expert"})
	result, err := s.handleGetPackage(t.Context(), newRequest(map[string]any{"package_name": "zgw_only"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := resultText(result)
	for _, noise := range []string{"XMLName", `"URI"`, `"Type"`} {
		if strings.Contains(text, noise) {
			t.Errorf("answer still carries %s:\n%s", noise, text)
		}
	}
	if !strings.Contains(text, "SICF ZGW_SRV") || !strings.Contains(text, "SUSC ZGW") {
		t.Errorf("answer does not name the TADIR objects the nodestructure left out:\n%s", text)
	}
	if !strings.Contains(sql, "'ZGW_ONLY'") {
		t.Errorf("TADIR query = %q, want the upper-cased package", sql)
	}
}
