package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

type TLSConfig struct {
	DisableSSL bool
	CACert     string
	ClientCert string
	ClientKey  string
	ServerName string
	SkipVerify bool
	CertDir    string // 证书目录
}

// GetGRPCDialOpts returns gRPC dial options with TLS configuration
func (c *TLSConfig) GetGRPCDialOpts() ([]grpc.DialOption, error) {
	var dialOpts []grpc.DialOption

	// 添加keepalive参数
	keepaliveParams := keepalive.ClientParameters{
		Time:                10 * time.Second,
		Timeout:             5 * time.Second,
		PermitWithoutStream: true,
	}
	dialOpts = append(dialOpts, grpc.WithKeepaliveParams(keepaliveParams))

	if c.DisableSSL {
		dialOpts = append(dialOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		return dialOpts, nil
	}

	// For mutual TLS (when client cert/key are provided)
	if c.ClientCert != "" && c.ClientKey != "" {
		return c.getMutualTLSDialOpts(dialOpts)
	}

	// For server-side TLS only
	return c.getServerSideTLSDialOpts(dialOpts)
}

// getMutualTLSDialOpts configures mutual TLS (client certificate authentication)
func (c *TLSConfig) getMutualTLSDialOpts(dialOpts []grpc.DialOption) ([]grpc.DialOption, error) {
	certManager := NewCertificateManager(c.CertDir)

	// Load client certificate pair
	cert, err := certManager.LoadClientCertificate(c.ClientCert, c.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificates: %w", err)
	}

	// Validate certificate
	if err := certManager.ValidateCertificate(cert); err != nil {
		return nil, fmt.Errorf("client certificate validation failed: %w", err)
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
func (c *TLSConfig) getServerSideTLSDialOpts(dialOpts []grpc.DialOption) ([]grpc.DialOption, error) {

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
