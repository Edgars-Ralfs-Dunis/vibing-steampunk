package mcp

import (
	"fmt"
	"regexp"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/oisee/vibing-steampunk/pkg/adt"
)

// clientNumberRE matches a well-formed SAP client: exactly three digits.
var clientNumberRE = regexp.MustCompile(`^[0-9]{3}$`)

// adtOptionsFor builds the adt.Option set for a connection to the given SAP
// client. Everything except the client number comes from cfg, so a pooled
// client is identical to the base one in auth, TLS and safety settings.
func adtOptionsFor(cfg *Config, client string) []adt.Option {
	opts := []adt.Option{
		adt.WithClient(client),
		adt.WithLanguage(cfg.Language),
	}
	if cfg.InsecureSkipVerify {
		opts = append(opts, adt.WithInsecureSkipVerify())
	}
	if cfg.ClientCertProvider != nil {
		opts = append(opts, adt.WithClientCertProvider(cfg.ClientCertProvider))
	} else if cfg.ClientCert != nil {
		opts = append(opts, adt.WithClientCert(cfg.ClientCert))
	}
	if len(cfg.Cookies) > 0 {
		opts = append(opts, adt.WithCookies(cfg.Cookies))
	}
	if cfg.Verbose {
		opts = append(opts, adt.WithVerbose())
	}
	if cfg.ReauthFunc != nil {
		opts = append(opts, adt.WithReauthFunc(cfg.ReauthFunc))
	}

	// Configure safety settings
	safety := adt.UnrestrictedSafetyConfig() // Default: unrestricted for backwards compatibility
	if cfg.ReadOnly {
		safety.ReadOnly = true
	}
	if cfg.BlockFreeSQL {
		safety.BlockFreeSQL = true
	}
	if cfg.AllowedOps != "" {
		safety.AllowedOps = cfg.AllowedOps
	}
	if cfg.DisallowedOps != "" {
		safety.DisallowedOps = cfg.DisallowedOps
	}
	if len(cfg.AllowedPackages) > 0 {
		safety.AllowedPackages = cfg.AllowedPackages
	}
	if cfg.EnableTransports {
		safety.EnableTransports = true
	}
	if cfg.TransportReadOnly {
		safety.TransportReadOnly = true
	}
	if len(cfg.AllowedTransports) > 0 {
		safety.AllowedTransports = cfg.AllowedTransports
	}
	if cfg.AllowTransportableEdits {
		safety.AllowTransportableEdits = true
	}
	opts = append(opts, adt.WithSafety(safety))
	return opts
}

// ActiveClient returns the SAP client that client-dependent reads currently
// resolve to.
func (s *Server) ActiveClient() string {
	s.clientPoolMu.Lock()
	defer s.clientPoolMu.Unlock()
	return s.activeClient
}

// SetActiveClient makes client the target for subsequent client-dependent
// reads. It does not touch the base client, so writes stay on the configured
// client regardless.
func (s *Server) SetActiveClient(client string) error {
	if !clientNumberRE.MatchString(client) {
		return fmt.Errorf("invalid SAP client %q: expected exactly three digits, e.g. \"460\"", client)
	}
	s.clientPoolMu.Lock()
	defer s.clientPoolMu.Unlock()
	s.activeClient = client
	return nil
}

// clientFor resolves the adt.Client for a SAP client number, building and
// pooling one on first use. An empty client means "the active client".
//
// A pooled client is a full second connection: its own Transport, cookie jar,
// CSRF token and SAP session. The client cannot be varied per request because
// the session is bound to it, not just the sap-client query parameter. With
// certificate auth the extra logon is silent, since the macOS keychain grant is
// per (private key x binary) rather than per system or client.
func (s *Server) clientFor(client string) (*adt.Client, error) {
	s.clientPoolMu.Lock()
	defer s.clientPoolMu.Unlock()

	if client == "" {
		client = s.activeClient
	}
	if !clientNumberRE.MatchString(client) {
		return nil, fmt.Errorf("invalid SAP client %q: expected exactly three digits, e.g. \"460\"", client)
	}
	if client == s.config.Client {
		return s.adtClient, nil
	}
	if pooled, ok := s.clientPool[client]; ok {
		return pooled, nil
	}

	pooled := adt.NewClient(s.config.BaseURL, s.config.Username, s.config.Password,
		adtOptionsFor(s.config, client)...)
	s.clientPool[client] = pooled
	return pooled, nil
}

// resolveReadClient picks the connection for a client-dependent read: the
// optional per-call "client" argument when given, otherwise the active client.
// A per-call client does not change the active one, so comparing two customers
// does not disturb the session's default.
//
// Only client-dependent reads call this. Write handlers use s.adtClient
// directly and have no way to reach another client.
func (s *Server) resolveReadClient(request mcp.CallToolRequest) (*adt.Client, error) {
	client, _ := request.GetArguments()["client"].(string)
	return s.clientFor(client)
}
