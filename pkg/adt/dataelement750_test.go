package adt

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Captured from ZED 050 (7.50) on 2026-10-11 with Accept dataelements.v1
// (night-runs/2026-10-11-vsp/captures/i153-dataelement-PERNR_D-v1-750.xml),
// atom links cut. The same GET with Accept v2 answers 406 (upstream #153).
const pernrDV1 = `<?xml version="1.0" encoding="utf-8"?><blue:wbobj adtcore:responsible="SAP" adtcore:masterLanguage="DE" adtcore:masterSystem="SAP" adtcore:name="PERNR_D" adtcore:type="DTEL/DE" adtcore:changedAt="2024-09-08T13:39:09Z" adtcore:version="active" adtcore:changedBy="SAP" adtcore:createdBy="SAP" adtcore:description="Personalnummer" adtcore:language="EN" xmlns:blue="http://www.sap.com/wbobj/dictionary/dtel" xmlns:adtcore="http://www.sap.com/adt/core"><adtcore:packageRef adtcore:uri="/sap/bc/adt/vit/wb/object_type/devck/object_name/BP" adtcore:type="DEVC/K" adtcore:name="BP"/><dtel:dataElement xmlns:dtel="http://www.sap.com/adt/dictionary/dataelements"><dtel:typeKind>domain</dtel:typeKind><dtel:typeName>PERNR</dtel:typeName><dtel:dataType>NUMC</dtel:dataType><dtel:dataTypeLength>000008</dtel:dataTypeLength><dtel:dataTypeDecimals>000000</dtel:dataTypeDecimals><dtel:shortFieldLabel>PersNr</dtel:shortFieldLabel><dtel:shortFieldLength>08</dtel:shortFieldLength><dtel:shortFieldMaxLength>10</dtel:shortFieldMaxLength><dtel:mediumFieldLabel>Personalnr</dtel:mediumFieldLabel><dtel:mediumFieldLength>15</dtel:mediumFieldLength><dtel:mediumFieldMaxLength>20</dtel:mediumFieldMaxLength><dtel:longFieldLabel>Personalnummer</dtel:longFieldLabel><dtel:longFieldLength>20</dtel:longFieldLength><dtel:longFieldMaxLength>40</dtel:longFieldMaxLength><dtel:headingFieldLabel>PersNr</dtel:headingFieldLabel><dtel:headingFieldLength>08</dtel:headingFieldLength><dtel:headingFieldMaxLength>55</dtel:headingFieldMaxLength></dtel:dataElement></blue:wbobj>`

const dtel406 = `<?xml version="1.0" encoding="utf-8"?><exc:exception xmlns:exc="http://www.sap.com/abapxml/types/communicationframework"><namespace id="com.sap.adt"/><type id="ExceptionResourceNotAcceptable"/><message lang="EN">The message content is not acceptable</message><localizedMessage lang="EN">The message content is not acceptable</localizedMessage><properties/></exc:exception>`

// dtel750Client answers like 7.50: v1 only, 406 for anything else.
func dtel750Client() *Client {
	cfg := NewConfig("https://sap.example.com:44300", "user", "pass")
	mock := &funcMockClient{doFunc: func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "discovery") {
			return newTestResponse("OK"), nil
		}
		if strings.Contains(req.Header.Get("Accept"), "dataelements.v1+xml") {
			return newTestResponse(pernrDV1), nil
		}
		return &http.Response{StatusCode: http.StatusNotAcceptable, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(dtel406))}, nil
	}}
	return NewClientWithTransport(cfg, NewTransportWithClient(cfg, mock))
}

func TestGetDataElementLabels750ServesV1Only(t *testing.T) {
	labels, err := dtel750Client().GetDataElementLabels(context.Background(), "PERNR_D", "EN")
	if err != nil {
		t.Fatalf("GetDataElementLabels: %v", err)
	}
	if labels.Short != "PersNr" || labels.Medium != "Personalnr" || labels.Long != "Personalnummer" || labels.Heading != "PersNr" {
		t.Errorf("got %+v", labels)
	}
}

func TestGetTypeInfo750ServesV1Only(t *testing.T) {
	ti, err := dtel750Client().GetTypeInfo(context.Background(), "pernr_d")
	if err != nil {
		t.Fatalf("GetTypeInfo: %v", err)
	}
	want := TypeInfo{Name: "PERNR_D", Type: "NUMC", Description: "Personalnummer", Length: 8, Decimals: 0}
	if *ti != want {
		t.Errorf("got %+v, want %+v", *ti, want)
	}
}
