package mcp

import (
	"testing"

	"github.com/oisee/vibing-steampunk/pkg/adt"
)

// A bridge is one long-lived ABAP internal session, so it keeps every class it
// loaded and the FM interfaces it read. resetBridges drops the pooled sessions
// so the next call reconnects and picks up freshly activated ABAP.

func testServer() *Server {
	s := &Server{config: &Config{}}
	s.config.Client = "050"
	s.config.BaseURL = "https://example.invalid:44300"
	s.bridgePool = make(map[string]*adt.AMDPWebSocketClient)
	s.debugBridgePool = make(map[string]*adt.DebugWebSocketClient)
	return s
}

func TestResetBridgesDropsNamedClient(t *testing.T) {
	s := testServer()
	s.bridgePool["460"] = s.newBridge("460")
	s.debugBridgePool["460"] = s.newDebugBridge("460")
	s.bridgePool["440"] = s.newBridge("440")

	got, err := s.resetBridges("460", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "460" {
		t.Fatalf("got %v, want [460]", got)
	}
	if _, ok := s.bridgePool["460"]; ok {
		t.Error("460 AMDP bridge still pooled")
	}
	if _, ok := s.debugBridgePool["460"]; ok {
		t.Error("460 debug bridge still pooled")
	}
	if _, ok := s.bridgePool["440"]; !ok {
		t.Error("440 must be left alone")
	}
}

func TestResetBridgesEmptyClientMeansConfigured(t *testing.T) {
	s := testServer()
	s.amdpWSClient = s.newBridge("050")
	s.debugWSClient = s.newDebugBridge("050")

	got, err := s.resetBridges("", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "050" {
		t.Fatalf("got %v, want [050]", got)
	}
	if s.amdpWSClient != nil || s.debugWSClient != nil {
		t.Error("configured-client bridges must be cleared")
	}
}

func TestResetBridgesAll(t *testing.T) {
	s := testServer()
	s.amdpWSClient = s.newBridge("050")
	s.bridgePool["460"] = s.newBridge("460")
	s.debugBridgePool["440"] = s.newDebugBridge("440")

	got, err := s.resetBridges("", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %v, want three clients", got)
	}
	if len(s.bridgePool) != 0 || len(s.debugBridgePool) != 0 || s.amdpWSClient != nil {
		t.Error("all pooled bridges must be dropped")
	}
}

func TestResetBridgesRejectsBadClient(t *testing.T) {
	s := testServer()
	if _, err := s.resetBridges("46", false); err == nil {
		t.Error("expected a three-digit client to be required")
	}
}
