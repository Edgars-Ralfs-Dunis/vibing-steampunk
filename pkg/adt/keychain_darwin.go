//go:build darwin

package adt

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"

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

// LoadKeychainClientCert finds a macOS keychain identity whose leaf certificate
// Common Name equals cn and returns it as a *tls.Certificate. The private key
// is never exported: PrivateKey is an in-place crypto.Signer backed by the
// Security framework (the same way Safari/SLC use the key). The full chain
// (leaf + intermediates) is included — ZED's 7.50 ICM rejects a leaf-only
// presentation. Caller must cap TLS at 1.2 (see Config.tlsClientConfig).
func LoadKeychainClientCert(cn string) (*tls.Certificate, error) {
	store, err := certstore.Open()
	if err != nil {
		return nil, fmt.Errorf("open macOS keychain: %w", err)
	}

	idents, err := store.Identities()
	if err != nil {
		store.Close()
		return nil, fmt.Errorf("list keychain identities: %w", err)
	}

	for _, id := range idents {
		crt, err := id.Certificate()
		if err != nil || crt.Subject.CommonName != cn {
			id.Close()
			continue
		}

		chain, err := id.CertificateChain()
		if err != nil || len(chain) == 0 {
			chain = []*x509.Certificate{crt}
		}
		signer, err := id.Signer()
		if err != nil {
			id.Close()
			store.Close()
			return nil, fmt.Errorf("keychain signer for CN=%q: %w (grant access when prompted)", cn, err)
		}

		cert := &tls.Certificate{PrivateKey: signer, Leaf: crt}
		for _, c := range chain {
			cert.Certificate = append(cert.Certificate, c.Raw)
		}

		// keep the store + matched identity alive for the process lifetime
		keychainKeepAlive.store = store
		keychainKeepAlive.ident = id
		return cert, nil
	}

	store.Close()
	return nil, fmt.Errorf("no keychain identity with certificate CN=%q found", cn)
}
