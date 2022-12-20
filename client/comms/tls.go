package comms

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
)

// TLSConfig prepares a *tls.Config struct using the provided cert.
func TLSConfig(URL string, cert []byte) (*tls.Config, error) {
	if len(cert) == 0 {
		return nil, nil
	}

	uri, err := url.Parse(URL)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL: %w", err)
	}

	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	if ok := rootCAs.AppendCertsFromPEM(cert); !ok {
		return nil, ErrInvalidCert
	}

	return &tls.Config{
		RootCAs:    rootCAs,
		MinVersion: tls.VersionTLS12,
		ServerName: uri.Hostname(),
	}, nil
}
