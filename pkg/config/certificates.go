package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CertificateManager 管理证书的生命周期
type CertificateManager struct {
	certDir string
}

// NewCertificateManager 创建新的证书管理器
func NewCertificateManager(certDir string) *CertificateManager {
	return &CertificateManager{
		certDir: certDir,
	}
}

// LoadCACert 加载CA证书
func (cm *CertificateManager) LoadCACert(caPath string) (*x509.CertPool, error) {
	var caCertPath string
	if filepath.IsAbs(caPath) {
		caCertPath = caPath
	} else {
		caCertPath = filepath.Join(cm.certDir, caPath)
	}

	caCert, err := os.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate from %s: %w", caCertPath, err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	return caCertPool, nil
}

// LoadClientCertificate 加载客户端证书
func (cm *CertificateManager) LoadClientCertificate(certPath, keyPath string) (tls.Certificate, error) {
	var clientCertPath, clientKeyPath string

	if filepath.IsAbs(certPath) {
		clientCertPath = certPath
	} else {
		clientCertPath = filepath.Join(cm.certDir, certPath)
	}

	if filepath.IsAbs(keyPath) {
		clientKeyPath = keyPath
	} else {
		clientKeyPath = filepath.Join(cm.certDir, keyPath)
	}

	cert, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to load client certificate pair: %w", err)
	}

	return cert, nil
}

// ValidateCertificate 验证证书的有效性
func (cm *CertificateManager) ValidateCertificate(cert tls.Certificate) error {
	if len(cert.Certificate) == 0 {
		return fmt.Errorf("certificate chain is empty")
	}

	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	if x509Cert.NotAfter.Before(time.Now()) {
		return fmt.Errorf("certificate has expired")
	}

	return nil
}

// EnsureCertDir 确保证书目录存在
func (cm *CertificateManager) EnsureCertDir() error {
	return os.MkdirAll(cm.certDir, 0755)
}
