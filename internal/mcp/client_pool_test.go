package mcp

import "testing"

func poolTestConfig() *Config {
	return &Config{
		BaseURL:  "https://sap.example.com:44300",
		Username: "TESTER",
		Client:   "050",
		Language: "EN",
		Mode:     "expert",
	}
}

func TestActiveClientDefaultsToConfiguredClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	if got := s.ActiveClient(); got != "050" {
		t.Errorf("ActiveClient() = %q, want %q", got, "050")
	}
}

func TestClientForConfiguredClientReturnsBaseClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	got, err := s.clientFor("050")
	if err != nil {
		t.Fatalf("clientFor(050) returned error: %v", err)
	}
	if got != s.adtClient {
		t.Error("clientFor(configured) should return the base client, not a pooled copy")
	}
}

func TestClientForEmptyStringReturnsActiveClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	got, err := s.clientFor("")
	if err != nil {
		t.Fatalf("clientFor(\"\") returned error: %v", err)
	}
	if got != s.adtClient {
		t.Error("clientFor(\"\") should resolve to the active client")
	}
}

func TestClientForOtherClientIsDistinctAndBoundToThatClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	got, err := s.clientFor("460")
	if err != nil {
		t.Fatalf("clientFor(460) returned error: %v", err)
	}
	if got == s.adtClient {
		t.Fatal("clientFor(460) must not return the base client")
	}
	if got.SAPClient() != "460" {
		t.Errorf("pooled client SAPClient() = %q, want %q", got.SAPClient(), "460")
	}
	if s.adtClient.SAPClient() != "050" {
		t.Errorf("base client was mutated: SAPClient() = %q, want %q", s.adtClient.SAPClient(), "050")
	}
}

func TestClientForPoolsPerClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	first, err := s.clientFor("460")
	if err != nil {
		t.Fatalf("first clientFor(460): %v", err)
	}
	second, err := s.clientFor("460")
	if err != nil {
		t.Fatalf("second clientFor(460): %v", err)
	}
	if first != second {
		t.Error("clientFor should reuse the pooled client for the same client number")
	}
}

func TestClientForDifferentClientsDoNotShareASession(t *testing.T) {
	s := NewServer(poolTestConfig())

	a, err := s.clientFor("460")
	if err != nil {
		t.Fatalf("clientFor(460): %v", err)
	}
	b, err := s.clientFor("830")
	if err != nil {
		t.Fatalf("clientFor(830): %v", err)
	}
	if a == b {
		t.Fatal("different clients must get different adt.Client instances")
	}
	if a.SameSessionAs(b) {
		t.Error("different clients must not share a transport/cookie jar")
	}
}

func TestClientForRejectsMalformedClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	for _, bad := range []string{"46", "4600", "4x0", "abc", " 46"} {
		if _, err := s.clientFor(bad); err == nil {
			t.Errorf("clientFor(%q) should have been rejected", bad)
		}
	}
}

func TestSetActiveClientChangesResolution(t *testing.T) {
	s := NewServer(poolTestConfig())

	if err := s.SetActiveClient("830"); err != nil {
		t.Fatalf("SetActiveClient(830): %v", err)
	}
	if got := s.ActiveClient(); got != "830" {
		t.Errorf("ActiveClient() = %q, want %q", got, "830")
	}

	got, err := s.clientFor("")
	if err != nil {
		t.Fatalf("clientFor(\"\"): %v", err)
	}
	if got.SAPClient() != "830" {
		t.Errorf("after SetActiveClient, clientFor(\"\") bound to %q, want %q", got.SAPClient(), "830")
	}
}

func TestSetActiveClientRejectsMalformedClient(t *testing.T) {
	s := NewServer(poolTestConfig())

	if err := s.SetActiveClient("nope"); err == nil {
		t.Error("SetActiveClient should reject a malformed client")
	}
	if got := s.ActiveClient(); got != "050" {
		t.Errorf("a rejected SetActiveClient must not change the active client, got %q", got)
	}
}
