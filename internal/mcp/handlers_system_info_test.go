package mcp

import (
	"strings"
	"testing"
)

// On Suite on HANA, CVERS cannot identify the database. When the operator
// forces --feature-hana on, GetSystemInfo says HDB like GetFeatures does.
func TestHandleGetSystemInfoTakesDatabaseFromForcedHANAFeature(t *testing.T) {
	sap, _ := rowNumberCaptureServer(t)
	defer sap.Close()

	s := NewServer(&Config{BaseURL: sap.URL, Username: "TESTUSER", Client: "050", Mode: "expert", FeatureHANA: "on"})
	result, err := s.handleGetSystemInfo(t.Context(), newRequest(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text := resultText(result); !strings.Contains(text, `"databaseSystem": "HDB"`) {
		t.Errorf("want databaseSystem HDB, got:\n%s", text)
	}
}
