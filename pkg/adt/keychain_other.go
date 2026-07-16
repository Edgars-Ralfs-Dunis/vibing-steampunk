//go:build !darwin

package adt

import (
	"crypto/tls"
	"fmt"
)

// LoadKeychainClientCert is only implemented on macOS. On other platforms it
// returns an error so the (Mac-only) keychain client-cert feature fails loudly.
func LoadKeychainClientCert(cn string) (*tls.Certificate, error) {
	return nil, fmt.Errorf("keychain client certificate auth is only supported on macOS")
}
