package adt

import (
	"context"
	"net/http"
	"testing"
)

// Captured from ZED 050 (7.50) on 2026-10-11, cut to 3 entries
// (night-runs/2026-10-11-vsp/captures/item19-components-750.xml).
const components750Feed = `<?xml version="1.0" encoding="utf-8"?><atom:feed xmlns:atom="http://www.w3.org/2005/Atom"><atom:author><atom:name>SAP SE</atom:name></atom:author><atom:title>Installed Components</atom:title><atom:updated>2026-10-11T18:04:02Z</atom:updated><atom:entry><atom:id>SAP_ABA</atom:id><atom:title>750;SAPK-75029INSAPABA;0029;Cross-Application Component</atom:title></atom:entry><atom:entry><atom:id>SAP_BASIS</atom:id><atom:title>750;SAPK-75029INSAPBASIS;0029;SAP Basis Component</atom:title></atom:entry><atom:entry><atom:id>SAP_HR</atom:id><atom:title>608;SAPKE608G1;0161;Human Resources</atom:title></atom:entry></atom:feed>`

func componentsClient(body string) *Client {
	cfg := NewConfig("https://sap.example.com:44300", "user", "pass")
	mock := &mockTransportClient{responses: map[string]*http.Response{
		"/sap/bc/adt/system/components": newTestResponse(body),
		"discovery":                     newTestResponse("OK"),
	}}
	return NewClientWithTransport(cfg, NewTransportWithClient(cfg, mock))
}

func TestGetInstalledComponents_AtomFeed750(t *testing.T) {
	got, err := componentsClient(components750Feed).GetInstalledComponents(context.Background())
	if err != nil {
		t.Fatalf("GetInstalledComponents: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d components, want 3: %+v", len(got), got)
	}
	want := InstalledComponent{Name: "SAP_HR", Release: "608", SupportPack: "0161", Description: "Human Resources"}
	if got[2] != want {
		t.Errorf("SAP_HR = %+v, want %+v", got[2], want)
	}
}

func TestGetInstalledComponents_ComponentsShapeStillParses(t *testing.T) {
	got, err := componentsClient(`<components><component name="SAP_BASIS" release="758" supportPack="0002" description="SAP Basis"/></components>`).GetInstalledComponents(context.Background())
	if err != nil {
		t.Fatalf("GetInstalledComponents: %v", err)
	}
	if len(got) != 1 || got[0].Name != "SAP_BASIS" || got[0].Release != "758" || got[0].SupportPack != "0002" {
		t.Errorf("got %+v", got)
	}
}
