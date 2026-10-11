package adt

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dpRow renders one data-preview row from name/value pairs; no pairs is an
// empty result.
func dpRow(kv ...string) string {
	var b strings.Builder
	b.WriteString(`<dataPreview:tableData xmlns:dataPreview="http://www.sap.com/adt/dataPreview">`)
	for i := 0; i+1 < len(kv); i += 2 {
		b.WriteString(`<dataPreview:columns><dataPreview:metadata dataPreview:name="` + kv[i] + `"/><dataPreview:dataSet><dataPreview:data>` + kv[i+1] + `</dataPreview:data></dataPreview:dataSet></dataPreview:columns>`)
	}
	b.WriteString(`</dataPreview:tableData>`)
	return b.String()
}

// zedSQL answers GetSystemInfo's queries the way ZED 050 does: Suite on HANA,
// so CVERS names neither S4CORE nor anything HDB/HANA.
func zedSQL(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/datapreview/freestyle") {
			w.Header().Set("X-CSRF-Token", "TOKEN")
			return
		}
		q, _ := io.ReadAll(r.Body)
		sql := string(q)
		switch {
		case strings.Contains(sql, "T000"):
			io.WriteString(w, dpRow("MANDT", "050", "LOGSYS", "ZEDCLNT050"))
		case strings.Contains(sql, "'SAP_BASIS'"):
			io.WriteString(w, dpRow("RELEASE", "750", "EXTRELEASE", "0029"))
		case strings.Contains(sql, "'SAP_ABA'"):
			io.WriteString(w, dpRow("RELEASE", "750", "EXTRELEASE", "0029"))
		default:
			io.WriteString(w, dpRow())
		}
	}))
	t.Cleanup(srv.Close)
	cfg := NewConfig(srv.URL, "u", "p")
	cfg.Client = "050"
	return NewClientWithTransport(cfg, NewTransport(cfg))
}

func TestGetSystemInfo_NoKernelFromSAPABA_BasisSP(t *testing.T) {
	info, err := zedSQL(t).GetSystemInfo(context.Background())
	if err != nil {
		t.Fatalf("GetSystemInfo: %v", err)
	}
	if info.KernelRelease != "" {
		t.Errorf("kernelRelease = %q: SAP_ABA's release is not the kernel", info.KernelRelease)
	}
	if info.SupportPack != "0029" {
		t.Errorf("supportPack = %q, want SAP_BASIS EXTRELEASE 0029", info.SupportPack)
	}
	if info.SystemID != "ZED" || info.SAPRelease != "750" {
		t.Errorf("got %+v", info)
	}
}

// An unidentified database is not evidence of a non-HANA one.
func TestProbeHANA_UnknownDatabaseIsNotNonHANA(t *testing.T) {
	ok, msg, err := NewFeatureProber(zedSQL(t), FeatureConfig{}, false).probeHANA(context.Background())
	if err != nil {
		t.Fatalf("probeHANA: %v", err)
	}
	if ok || strings.Contains(msg, "non-HANA") {
		t.Errorf("probeHANA = %v, %q; want unavailable with an 'unknown' message", ok, msg)
	}
}
