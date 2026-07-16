//go:build darwin

package adt

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"strings"
	"time"

	"github.com/github/smimesign/certstore"
)

// keychainKeepAlive holds the certstore store and matched identity for the
// process lifetime. The in-place signer references live Security.framework
// handles; letting the store/identity be garbage-collected (or closed) would
// invalidate the signer. vsp loads the cert once at startup and runs as a
// daemon, so this deliberate, bounded leak is fine.
var keychainKeepAlive struct {
	store certstore.Store
	ident certstore.Identity
}

// LoadKeychainClientCert finds a valid keychain identity whose leaf certificate
// Subject Common Name equals cn. Use this to pin a specific user's cert.
func LoadKeychainClientCert(cn string) (*tls.Certificate, error) {
	return loadKeychainIdentity(
		fmt.Sprintf("Subject CN=%q", cn),
		func(c *x509.Certificate) bool { return c.Subject.CommonName == cn },
	)
}

// LoadKeychainClientCertByIssuer finds a valid keychain identity whose leaf
// certificate was issued by an issuer with the given Common Name (the freshest,
// if several match). This lets a shared config select "the SLC/IAS login cert"
// generically — each user's own cert is picked without a per-user CN.
func LoadKeychainClientCertByIssuer(issuerCN string) (*tls.Certificate, error) {
	return LoadKeychainClientCertByIssuers([]string{issuerCN})
}

// LoadKeychainClientCertByIssuers is LoadKeychainClientCertByIssuer for a set
// of acceptable issuer CNs (org fleets often have more than one Secure Login
// Server / CA in play). The freshest valid identity across all of them wins.
func LoadKeychainClientCertByIssuers(issuerCNs []string) (*tls.Certificate, error) {
	return loadKeychainIdentity(
		fmt.Sprintf("Issuer CN in %q", issuerCNs),
		func(c *x509.Certificate) bool {
			for _, cn := range issuerCNs {
				if c.Issuer.CommonName == cn {
					return true
				}
			}
			return false
		},
	)
}

// loadKeychainIdentity returns the freshest currently-valid keychain identity
// matching pred, as a *tls.Certificate (full chain + in-place signer; the key
// never leaves the keychain). Caller pins TLS 1.2 (see Config.tlsClientConfig).
func loadKeychainIdentity(desc string, pred func(*x509.Certificate) bool) (*tls.Certificate, error) {
	store, err := certstore.Open()
	if err != nil {
		return nil, fmt.Errorf("open macOS keychain: %w", err)
	}
	idents, err := store.Identities()
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("list keychain identities: %w", err)
	}

	now := time.Now()
	var best certstore.Identity
	var bestLeaf *x509.Certificate
	var seen []string // what IS in the keychain, for the no-match error
	for _, id := range idents {
		crt, err := id.Certificate()
		if err == nil && len(seen) < 10 {
			state := "valid"
			if now.After(crt.NotAfter) {
				state = "EXPIRED " + crt.NotAfter.Format("2006-01-02 15:04")
			}
			seen = append(seen, fmt.Sprintf("Subject CN=%q Issuer CN=%q (%s)",
				crt.Subject.CommonName, crt.Issuer.CommonName, state))
		}
		if err != nil || !pred(crt) || now.Before(crt.NotBefore) || now.After(crt.NotAfter) {
			id.Close()
			continue
		}
		if bestLeaf == nil || crt.NotBefore.After(bestLeaf.NotBefore) {
			if best != nil {
				best.Close()
			}
			best, bestLeaf = id, crt
		} else {
			id.Close()
		}
	}

	if best == nil {
		store.Close()
		detail := "keychain has no identities at all"
		if len(seen) > 0 {
			detail = "keychain identities present: " + strings.Join(seen, "; ")
		}
		return nil, fmt.Errorf("no valid (unexpired) keychain identity matching %s found — open SLC and log in (wrong SLS profile? %s)", desc, detail)
	}

	chain, err := best.CertificateChain()
	if err != nil || len(chain) == 0 {
		chain = []*x509.Certificate{bestLeaf}
	}
	signer, err := best.Signer()
	if err != nil {
		best.Close()
		store.Close()
		return nil, fmt.Errorf("keychain signer for %s: %w (grant access when prompted)", desc, err)
	}

	cert := &tls.Certificate{PrivateKey: signer, Leaf: bestLeaf}
	for _, c := range chain {
		cert.Certificate = append(cert.Certificate, c.Raw)
	}

	// keep the store + matched identity alive for the process lifetime
	keychainKeepAlive.store = store
	keychainKeepAlive.ident = best
	return cert, nil
}
