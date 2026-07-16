//go:build !darwin

package adt

import (
	"crypto/tls"
	"fmt"
)

const errKeychainUnsupported = "keychain client certificate auth is only supported on macOS"

// LoadKeychainClientCert is only implemented on macOS.
func LoadKeychainClientCert(cn string) (*tls.Certificate, error) {
	return nil, fmt.Errorf(errKeychainUnsupported)
}

// LoadKeychainClientCertByIssuer is only implemented on macOS.
func LoadKeychainClientCertByIssuer(issuerCN string) (*tls.Certificate, error) {
	return nil, fmt.Errorf(errKeychainUnsupported)
}
