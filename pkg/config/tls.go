package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type TLSConfig struct {
	DisableSSL bool
	CACert     string
	ClientCert string
	ClientKey  string
	ServerName string
	SkipVerify bool
}

// GetGRPCDialOpts returns gRPC dial options with TLS configuration
func (c *TLSConfig) GetGRPCDialOpts() ([]grpc.DialOption, error) {
	var dialOpts []grpc.DialOption

	if c.DisableSSL {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		return dialOpts, nil
	}

	// For mutual TLS (when client cert/key are provided)
	if c.ClientCert != "" && c.ClientKey != "" {
		return c.getMutualTLSDialOpts()
	}

	// For server-side TLS only
	return c.getServerSideTLSDialOpts()
}

// getMutualTLSDialOpts configures mutual TLS (client certificate authentication)
func (c *TLSConfig) getMutualTLSDialOpts() ([]grpc.DialOption, error) {
	var dialOpts []grpc.DialOption

	// Load client certificate pair
	cert, err := tls.LoadX509KeyPair(c.ClientCert, c.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificates: %w", err)
	}

	// Load CA certificate if provided
	var certPool *x509.CertPool
	if c.CACert != "" {
		certPool = x509.NewCertPool()
		ca, err := os.ReadFile(c.CACert)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}
		if !certPool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("failed to append CA certificate")
		}
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            certPool,
		ServerName:         c.ServerName,
		InsecureSkipVerify: c.SkipVerify,
	}

	creds := credentials.NewTLS(tlsConfig)
	dialOpts = append(dialOpts, grpc.WithTransportCredentials(creds))

	return dialOpts, nil
}

// getServerSideTLSDialOpts configures server-side TLS only
func (c *TLSConfig) getServerSideTLSDialOpts() ([]grpc.DialOption, error) {
	var dialOpts []grpc.DialOption

	tlsConfig := &tls.Config{
		ServerName:         c.ServerName,
		InsecureSkipVerify: c.SkipVerify,
	}

	// Load CA certificate if provided
	if c.CACert != "" {
		certPool := x509.NewCertPool()
		ca, err := os.ReadFile(c.CACert)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}
		if !certPool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("failed to append CA certificate")
		}
		tlsConfig.RootCAs = certPool
	}

	creds := credentials.NewTLS(tlsConfig)
	dialOpts = append(dialOpts, grpc.WithTransportCredentials(creds))

	return dialOpts, nil
}
