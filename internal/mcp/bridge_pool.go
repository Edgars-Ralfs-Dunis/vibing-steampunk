package mcp

import (
	"context"
	"fmt"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/oisee/vibing-steampunk/pkg/adt"
)

// newBridge builds an unconnected ZADT_VSP bridge bound to one SAP client.
// Auth is copied from the server config, so a pooled bridge differs from the
// base one only in the client stamped into its WebSocket URL.
func (s *Server) newBridge(client string) *adt.AMDPWebSocketClient {
	b := adt.NewAMDPWebSocketClient(
		s.config.BaseURL, client, s.config.Username, s.config.Password, s.config.InsecureSkipVerify,
	)
	if s.config.ClientCertProvider != nil {
		b.SetClientCertProvider(s.config.ClientCertProvider)
	} else if s.config.ClientCert != nil {
		b.SetClientCert(s.config.ClientCert)
	}
	return b
}

// bridgeFor resolves the ZADT_VSP bridge for a SAP client, building and pooling
// one on first use. It does not connect; ensureWSConnectedFor does that.
//
// Unlike an adt.Client, the bridge takes sap-client as a URL parameter at
// connect time, so reaching another client is a second connection rather than a
// redirect of an existing session.
//
// An empty client means the CONFIGURED client, deliberately not the active one.
// SetClient exists so that reads can roam; letting it also steer where code
// EXECUTES would turn a read-side convenience into action at a distance in the
// one place that can least afford it.
func (s *Server) bridgeFor(client string) (*adt.AMDPWebSocketClient, error) {
	if client == "" {
		client = s.config.Client
	}
	if !clientNumberRE.MatchString(client) {
		return nil, fmt.Errorf("invalid SAP client %q: expected exactly three digits, e.g. \"460\"", client)
	}

	s.bridgePoolMu.Lock()
	defer s.bridgePoolMu.Unlock()

	if client == s.config.Client {
		if s.amdpWSClient == nil {
			s.amdpWSClient = s.newBridge(client)
		}
		return s.amdpWSClient, nil
	}
	if pooled, ok := s.bridgePool[client]; ok {
		return pooled, nil
	}
	pooled := s.newBridge(client)
	s.bridgePool[client] = pooled
	return pooled, nil
}

// replaceBridge swaps a dead bridge for a fresh one in whichever slot holds it.
func (s *Server) replaceBridge(client string) *adt.AMDPWebSocketClient {
	s.bridgePoolMu.Lock()
	defer s.bridgePoolMu.Unlock()

	fresh := s.newBridge(client)
	if client == s.config.Client {
		s.amdpWSClient = fresh
	} else {
		s.bridgePool[client] = fresh
	}
	return fresh
}

// ensureWSConnectedFor ensures the bridge for the given SAP client is connected,
// building and connecting it if needed. An empty client means the configured
// client, which is the pre-existing single-client behaviour.
func (s *Server) ensureWSConnectedFor(ctx context.Context, toolName, client string) *mcp.CallToolResult {
	bridge, err := s.bridgeFor(client)
	if err != nil {
		return newToolResultError(fmt.Sprintf("%s: %v", toolName, err))
	}
	if bridge.IsConnected() {
		return nil
	}

	target := client
	if target == "" {
		target = s.config.Client
	}
	bridge = s.replaceBridge(target)
	if err := bridge.Connect(ctx); err != nil {
		s.dropBridge(target)
		return newToolResultError(fmt.Sprintf("%s: WebSocket connect failed for client %s: %v", toolName, target, err))
	}
	return nil
}

// dropBridge forgets a bridge that failed to connect, so the next call builds a
// new one instead of reusing a dead handle.
func (s *Server) dropBridge(client string) {
	s.bridgePoolMu.Lock()
	defer s.bridgePoolMu.Unlock()

	if client == s.config.Client {
		s.amdpWSClient = nil
		return
	}
	delete(s.bridgePool, client)
}

// bridgeClientArg reads the optional per-call "client" argument for a bridge
// tool. Empty means the configured client.
func bridgeClientArg(request mcp.CallToolRequest) string {
	client, _ := request.GetArguments()["client"].(string)
	return client
}

// newDebugBridge builds an unconnected debug bridge bound to one SAP client.
func (s *Server) newDebugBridge(client string) *adt.DebugWebSocketClient {
	b := adt.NewDebugWebSocketClient(
		s.config.BaseURL, client, s.config.Username, s.config.Password, s.config.InsecureSkipVerify,
	)
	if s.config.ClientCertProvider != nil {
		b.SetClientCertProvider(s.config.ClientCertProvider)
	} else if s.config.ClientCert != nil {
		b.SetClientCert(s.config.ClientCert)
	}
	return b
}

// debugBridgeFor resolves the debug bridge for a SAP client. CallRFC runs over
// this connection rather than the AMDP one. Same rule as bridgeFor: an empty
// client is the configured client, never the active one.
//
// The debugger tools keep using s.debugWSClient with no client argument, so
// their session state stays on the configured connection.
func (s *Server) debugBridgeFor(client string) (*adt.DebugWebSocketClient, error) {
	if client == "" {
		client = s.config.Client
	}
	if !clientNumberRE.MatchString(client) {
		return nil, fmt.Errorf("invalid SAP client %q: expected exactly three digits, e.g. \"460\"", client)
	}

	s.bridgePoolMu.Lock()
	defer s.bridgePoolMu.Unlock()

	if client == s.config.Client {
		if s.debugWSClient == nil {
			s.debugWSClient = s.newDebugBridge(client)
		}
		return s.debugWSClient, nil
	}
	if pooled, ok := s.debugBridgePool[client]; ok {
		return pooled, nil
	}
	pooled := s.newDebugBridge(client)
	s.debugBridgePool[client] = pooled
	return pooled, nil
}

// ensureDebugWSClientFor connects the debug bridge for the given SAP client,
// replacing a dead handle. An empty client keeps the pre-existing behaviour.
func (s *Server) ensureDebugWSClientFor(ctx context.Context, client string) (*adt.DebugWebSocketClient, error) {
	bridge, err := s.debugBridgeFor(client)
	if err != nil {
		return nil, err
	}
	if bridge.IsConnected() {
		return bridge, nil
	}

	target := client
	if target == "" {
		target = s.config.Client
	}

	s.bridgePoolMu.Lock()
	fresh := s.newDebugBridge(target)
	if target == s.config.Client {
		s.debugWSClient = fresh
	} else {
		s.debugBridgePool[target] = fresh
	}
	s.bridgePoolMu.Unlock()

	if err := fresh.Connect(ctx); err != nil {
		s.bridgePoolMu.Lock()
		if target == s.config.Client {
			s.debugWSClient = nil
		} else {
			delete(s.debugBridgePool, target)
		}
		s.bridgePoolMu.Unlock()
		return nil, err
	}
	return fresh, nil
}

// bridgeAttribution renders the host and SAP client a bridge tool ran against.
//
// The client hooks that tried to PREVENT a wrong client were removed because a
// PreToolUse "ask" cannot survive a matching allow rule (see
// docs/dynamic-client-design.md). This is the replacement the doc asks for:
// make the client VISIBLE in every answer, so a wrong one is obvious in the
// transcript rather than silently correct-looking.
func bridgeAttribution(baseURL, client string) string {
	host := baseURL
	if u, err := url.Parse(baseURL); err == nil && u.Host != "" {
		host = u.Hostname()
	}
	return fmt.Sprintf("[%s client %s]", host, client)
}

// bridgeAttributionFor is bridgeAttribution for a per-call client argument,
// resolving "" to the configured client exactly as bridgeFor does.
func (s *Server) bridgeAttributionFor(client string) string {
	if client == "" {
		client = s.config.Client
	}
	return bridgeAttribution(s.config.BaseURL, client)
}
