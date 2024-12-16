package telejob

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// serverTLSConfig creates a TLS configuration for a server with mTLS
// authentication, used in [NewServer] to ensure properly secured connections.
// It requires a server certificate and key file, and a client CA certificate
// file. It enforces TLS version 1.3 and requires and verifies client
// certificates.
func serverTLSConfig(serverCertFile, serverKeyFile, clientCACertFile string) (*tls.Config, error) {
	if clientCACertFile == "" {
		return nil, fmt.Errorf("%w: client CA cert file is required", ErrCASetup)
	}
	certificate, err := tls.LoadX509KeyPair(serverCertFile, serverKeyFile)
	if err != nil {
		return nil, fmt.Errorf("%w: server cert file %q, key file %q: %w", ErrCertLoad, serverCertFile, serverKeyFile, err)
	}
	clientCAs, err := newCertPool(clientCACertFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		ClientCAs:    clientCAs,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// clientTLSConfig creates a TLS configuration for a client with mTLS
// authentication, used in [NewClient] to ensure properly secured connections.
// It requires a client certificate and key file. It optionally uses the
// provided server CA certificate, if it's not available as part of the root
// certificates. It enforces TLS version 1.3.
func clientTLSConfig(clientCertFile, clientKeyFile, serverCACertFile string) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
	if err != nil {
		return nil, fmt.Errorf("%w: client cert file %q, key file %q: %w", ErrCertLoad, clientCertFile, clientKeyFile, err)
	}
	rootCAs, err := newCertPool(serverCACertFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		RootCAs:      rootCAs,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// newCertPool creates a x509.CertPool.
//
// If the provided CA certificate file path is empty, it attempts to load the
// system's certificate pool. If the file path is not empty, it loads the
// certificates from the specified file.
func newCertPool(caCertFile string) (*x509.CertPool, error) {
	if caCertFile == "" {
		certPool, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("%w: cannot get system cert pool: %w", ErrCASetup, err)
		}
		return certPool, nil
	}
	certPool := x509.NewCertPool()
	b, err := os.ReadFile(caCertFile) //nolint:gosec // G304: Potential file inclusion via variable
	if err != nil {
		return nil, fmt.Errorf("%w: cannot read %q: %w", ErrCASetup, caCertFile, err)
	}
	if !certPool.AppendCertsFromPEM(b) {
		return nil, fmt.Errorf("%w: cannot append %q", ErrCASetup, caCertFile)
	}
	return certPool, nil
}
