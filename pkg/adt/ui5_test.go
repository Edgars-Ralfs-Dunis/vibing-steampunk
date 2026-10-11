package adt

import (
	"context"
	"net/http"
	"testing"
)

// 7.50's filestore ignores name= and maxResults= and answers the whole
// catalogue, so the filter and the cap have to happen on our side.
const ui5CatalogueFeed = `<?xml version="1.0" encoding="utf-8"?>
<atom:feed xmlns:atom="http://www.w3.org/2005/Atom">
<atom:entry><atom:id>/MIBCON/HR_PDF</atom:id><atom:title>/MIBCON/HR_PDF</atom:title><atom:summary>PDF</atom:summary></atom:entry>
<atom:entry><atom:id>HRPY_PCC2_M</atom:id><atom:title>HRPY_PCC2_M</atom:title><atom:summary>PCC 2</atom:summary></atom:entry>
<atom:entry><atom:id>HRPY_PCC_TM_1</atom:id><atom:title>HRPY_PCC_TM_1</atom:title><atom:summary>PCC TM</atom:summary></atom:entry>
<atom:entry><atom:id>hrpy_pcc_x</atom:id><atom:title>hrpy_pcc_x</atom:title><atom:summary>lower</atom:summary></atom:entry>
<atom:entry><atom:id>ZNGOM_CAL</atom:id><atom:title>ZNGOM_CAL</atom:title><atom:summary>NextGenOM payroll calendar</atom:summary></atom:entry>
</atom:feed>`

func newUI5TestClient(responses map[string]*http.Response) *Client {
	cfg := NewConfig("https://sap.example.com:44300", "user", "pass")
	return NewClientWithTransport(cfg, NewTransportWithClient(cfg, &mockTransportClient{responses: responses}))
}

func newUI5CatalogueClient() *Client {
	return newUI5TestClient(map[string]*http.Response{
		"/sap/bc/adt/filestore/ui5-bsp/objects": newTestResponse(ui5CatalogueFeed),
		"discovery":                             newTestResponse("OK"),
	})
}

func TestUI5ListApps_FiltersAndCapsClientSide(t *testing.T) {
	client := newUI5CatalogueClient()

	apps, matched, total, err := client.UI5ListApps(context.Background(), "HRPY_PCC*", 2)
	if err != nil {
		t.Fatalf("UI5ListApps: %v", err)
	}
	if len(apps) != 2 || matched != 3 || total != 5 {
		t.Fatalf("shown/matched/total = %d/%d/%d, want 2/3/5: %+v", len(apps), matched, total, apps)
	}
	for _, a := range apps {
		if a.Name != "HRPY_PCC2_M" && a.Name != "HRPY_PCC_TM_1" {
			t.Errorf("unexpected app %q for HRPY_PCC*", a.Name)
		}
	}

	// * must cross the namespace slash.
	if apps, _, _, _ := newUI5CatalogueClient().UI5ListApps(context.Background(), "*HR_PDF", 10); len(apps) != 1 {
		t.Errorf("*HR_PDF: got %+v, want /MIBCON/HR_PDF", apps)
	}
}
