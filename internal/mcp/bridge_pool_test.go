package mcp

import (
	"strings"
	"testing"
)

// The bridge pool mirrors the adt.Client pool, with one deliberate difference:
// an empty client resolves to the CONFIGURED client, never to the sticky active
// client. Reads may follow SetClient; executing code in a customer client must
// be asked for explicitly.

func TestBridgeForConfiguredClientReturnsBaseBridge(t *testing.T) {
	s := NewServer(poolTestConfig())

	got, err := s.bridgeFor("050")
	if err != nil {
		t.Fatalf("bridgeFor(050) returned error: %v", err)
	}
	if got == nil {
		t.Fatal("bridgeFor(050) returned nil")
	}
	if got != s.amdpWSClient {
		t.Error("bridgeFor(configured) should return the base bridge, not a pooled copy")
	}
	if got.SAPClient() != "050" {
		t.Errorf("base bridge SAPClient() = %q, want %q", got.SAPClient(), "050")
	}
}

func TestBridgeForEmptyClientResolvesToConfiguredClientNotActiveClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	if err := s.SetActiveClient("830"); err != nil {
		t.Fatalf("SetActiveClient(830): %v", err)
	}

	got, err := s.bridgeFor("")
	if err != nil {
		t.Fatalf("bridgeFor(\"\") returned error: %v", err)
	}
	if got.SAPClient() != "050" {
		t.Errorf("bridgeFor(\"\") bound to %q, want the configured client %q; "+
			"a read-side SetClient must never redirect execution", got.SAPClient(), "050")
	}
}

func TestBridgeForOtherClientIsDistinctAndBoundToThatClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	base, err := s.bridgeFor("050")
	if err != nil {
		t.Fatalf("bridgeFor(050): %v", err)
	}
	got, err := s.bridgeFor("460")
	if err != nil {
		t.Fatalf("bridgeFor(460) returned error: %v", err)
	}
	if got == base {
		t.Fatal("bridgeFor(460) must not return the base bridge")
	}
	if got.SAPClient() != "460" {
		t.Errorf("pooled bridge SAPClient() = %q, want %q", got.SAPClient(), "460")
	}
	if base.SAPClient() != "050" {
		t.Errorf("base bridge was mutated: SAPClient() = %q, want %q", base.SAPClient(), "050")
	}
}

func TestBridgeForPoolsPerClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	first, err := s.bridgeFor("460")
	if err != nil {
		t.Fatalf("first bridgeFor(460): %v", err)
	}
	second, err := s.bridgeFor("460")
	if err != nil {
		t.Fatalf("second bridgeFor(460): %v", err)
	}
	if first != second {
		t.Error("bridgeFor should reuse the pooled bridge for the same client number")
	}
}

func TestBridgeForDifferentClientsGetDifferentConnections(t *testing.T) {
	s := NewServer(poolTestConfig())

	a, err := s.bridgeFor("460")
	if err != nil {
		t.Fatalf("bridgeFor(460): %v", err)
	}
	b, err := s.bridgeFor("830")
	if err != nil {
		t.Fatalf("bridgeFor(830): %v", err)
	}
	if a == b {
		t.Fatal("different clients must get different bridge instances")
	}
	if a.SAPClient() == b.SAPClient() {
		t.Errorf("pooled bridges share a client binding: %q", a.SAPClient())
	}
}

func TestBridgeForRejectsMalformedClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	for _, bad := range []string{"46", "4600", "4x0", "abc", " 46"} {
		if _, err := s.bridgeFor(bad); err == nil {
			t.Errorf("bridgeFor(%q) should have been rejected", bad)
		}
	}
}

// CallRFC runs over the debug bridge rather than the AMDP one, so it needs its
// own pool with the same guarantees. Debugger tools keep using the configured
// connection untouched.

func TestDebugBridgeForConfiguredClientReturnsBaseBridge(t *testing.T) {
	s := NewServer(poolTestConfig())

	got, err := s.debugBridgeFor("050")
	if err != nil {
		t.Fatalf("debugBridgeFor(050) returned error: %v", err)
	}
	if got != s.debugWSClient {
		t.Error("debugBridgeFor(configured) should return the base debug bridge")
	}
	if got.SAPClient() != "050" {
		t.Errorf("base debug bridge SAPClient() = %q, want %q", got.SAPClient(), "050")
	}
}

func TestDebugBridgeForEmptyClientResolvesToConfiguredClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	if err := s.SetActiveClient("830"); err != nil {
		t.Fatalf("SetActiveClient(830): %v", err)
	}

	got, err := s.debugBridgeFor("")
	if err != nil {
		t.Fatalf("debugBridgeFor(\"\"): %v", err)
	}
	if got.SAPClient() != "050" {
		t.Errorf("debugBridgeFor(\"\") bound to %q, want the configured client %q",
			got.SAPClient(), "050")
	}
}

func TestDebugBridgeForOtherClientIsDistinctAndBound(t *testing.T) {
	s := NewServer(poolTestConfig())

	base, err := s.debugBridgeFor("050")
	if err != nil {
		t.Fatalf("debugBridgeFor(050): %v", err)
	}
	got, err := s.debugBridgeFor("460")
	if err != nil {
		t.Fatalf("debugBridgeFor(460): %v", err)
	}
	if got == base {
		t.Fatal("debugBridgeFor(460) must not return the base debug bridge")
	}
	if got.SAPClient() != "460" {
		t.Errorf("pooled debug bridge SAPClient() = %q, want %q", got.SAPClient(), "460")
	}
}

func TestDebugBridgeForPoolsPerClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	first, err := s.debugBridgeFor("460")
	if err != nil {
		t.Fatalf("first debugBridgeFor(460): %v", err)
	}
	second, err := s.debugBridgeFor("460")
	if err != nil {
		t.Fatalf("second debugBridgeFor(460): %v", err)
	}
	if first != second {
		t.Error("debugBridgeFor should reuse the pooled bridge for the same client")
	}
}

func TestDebugBridgeForRejectsMalformedClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	for _, bad := range []string{"46", "4600", "4x0", "abc", " 46"} {
		if _, err := s.debugBridgeFor(bad); err == nil {
			t.Errorf("debugBridgeFor(%q) should have been rejected", bad)
		}
	}
}

// The removed client hooks tried to PREVENT a wrong client and could not. The
// design doc's replacement is to make it VISIBLE: every bridge tool says which
// system and client answered.

func TestBridgeAttributionNamesClientAndHost(t *testing.T) {
	got := bridgeAttribution("https://vhzaazesci.rise.zalaris.com:44300", "460")
	want := "[vhzaazesci.rise.zalaris.com client 460]"
	if got != want {
		t.Errorf("bridgeAttribution() = %q, want %q", got, want)
	}
}

func TestBridgeAttributionDropsThePort(t *testing.T) {
	got := bridgeAttribution("https://vhzaazedci.rise.zalaris.com:44300", "050")
	if strings.Contains(got, "44300") {
		t.Errorf("bridgeAttribution() = %q, should not carry the port", got)
	}
}

func TestBridgeAttributionFallsBackToRawURLWhenUnparseable(t *testing.T) {
	got := bridgeAttribution("://not a url", "460")
	if !strings.Contains(got, "460") {
		t.Errorf("bridgeAttribution() = %q, must still name the client", got)
	}
}

func TestServerAttributionUsesResolvedClientNotTheArgument(t *testing.T) {
	s := NewServer(poolTestConfig())

	// empty argument means the configured client, and the attribution must say so
	got := s.bridgeAttributionFor("")
	if !strings.Contains(got, "client 050") {
		t.Errorf("bridgeAttributionFor(\"\") = %q, want the configured client 050", got)
	}

	got = s.bridgeAttributionFor("460")
	if !strings.Contains(got, "client 460") {
		t.Errorf("bridgeAttributionFor(460) = %q, want client 460", got)
	}
}
