package vizier

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"io/ioutil"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wrongerror/observo-connector/pkg/auth"
	pb "github.com/wrongerror/observo-connector/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Client represents a Vizier client
type Client struct {
	conn   *grpc.ClientConn
	client pb.VizierServiceClient
	logger *logrus.Logger
	config *Config
}

// Config represents the client configuration
type Config struct {
	Address            string
	TLSEnabled         bool
	CACert             string
	ClientCert         string
	ClientKey          string
	ServerName         string
	InsecureSkipVerify bool

	// JWT Authentication options
	JWTSigningKey  string
	JWTUserID      string
	JWTOrgID       string
	JWTEmail       string
	JWTServiceName string

	// Direct Vizier options
	DirectVizierKey string
}

// Option represents a configuration option
type Option func(*Config)

// NewClient creates a new Vizier client
func NewClient(address string, logger *logrus.Logger, opts ...Option) (*Client, error) {
	config := &Config{
		Address:    address,
		TLSEnabled: true,
	}

	// Apply options
	for _, opt := range opts {
		opt(config)
	}

	// Setup credentials
	var creds credentials.TransportCredentials
	if config.TLSEnabled {
		tlsConfig := &tls.Config{
			ServerName:         config.ServerName,
			InsecureSkipVerify: config.InsecureSkipVerify,
		}

		// Load CA certificate if provided
		if config.CACert != "" {
			caCert, err := ioutil.ReadFile(config.CACert)
			if err != nil {
				return nil, fmt.Errorf("failed to read CA certificate: %w", err)
			}
			caCertPool := x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caCert)
			tlsConfig.RootCAs = caCertPool
		}

		// Load client certificate if provided
		if config.ClientCert != "" && config.ClientKey != "" {
			cert, err := tls.LoadX509KeyPair(config.ClientCert, config.ClientKey)
			if err != nil {
				return nil, fmt.Errorf("failed to load client certificate: %w", err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}

		creds = credentials.NewTLS(tlsConfig)
	} else {
		creds = insecure.NewCredentials()
	}

	// Create gRPC connection
	conn, err := grpc.Dial(address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", address, err)
	}

	client := &Client{
		conn:   conn,
		client: pb.NewVizierServiceClient(conn),
		logger: logger,
		config: config,
	}

	return client, nil
}

// Close closes the client connection
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// getAuthenticatedContext adds authentication to the context
func (c *Client) getAuthenticatedContext(ctx context.Context) context.Context {
	md := metadata.New(map[string]string{})

	// Add JWT authentication if configured
	if c.config.JWTSigningKey != "" {
		var token string
		var err error

		if c.config.JWTServiceName != "" {
			// Service authentication
			token, err = auth.GenerateServiceJWTToken(
				c.config.JWTSigningKey,
				c.config.JWTServiceName,
				30*time.Minute,
			)
		} else {
			// User authentication
			token, err = auth.GenerateJWTToken(
				c.config.JWTSigningKey,
				c.config.JWTUserID,
				c.config.JWTOrgID,
				c.config.JWTEmail,
				30*time.Minute,
			)
		}

		if err != nil {
			c.logger.WithError(err).Error("Failed to generate JWT token")
		} else {
			md.Set("authorization", "Bearer "+token)
		}
	}

	// Add direct Vizier key if configured
	if c.config.DirectVizierKey != "" {
		md.Set("pixie-api-key", c.config.DirectVizierKey)
	}

	return metadata.NewOutgoingContext(ctx, md)
}

// HealthCheck performs a health check on the Vizier cluster
func (c *Client) HealthCheck(ctx context.Context, clusterID string) error {
	req := &pb.HealthCheckRequest{
		ClusterId: clusterID,
	}

	c.logger.WithField("cluster_id", clusterID).Info("Performing health check")

	// Add authentication to context
	authCtx := c.getAuthenticatedContext(ctx)

	stream, err := c.client.HealthCheck(authCtx, req)
	if err != nil {
		return fmt.Errorf("failed to perform health check: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to receive health check response: %w", err)
	}

	if resp != nil && resp.Status != nil {
		if resp.Status.ErrCode != pb.Code_OK {
			c.logger.WithFields(logrus.Fields{
				"code":    resp.Status.ErrCode,
				"message": resp.Status.Msg,
			}).Error("Health check failed")
			return fmt.Errorf("health check failed: %s", resp.Status.Msg)
		}
	}

	c.logger.Info("Health check passed")
	return nil
}

// ExecuteScript executes a simple PxL script on the Vizier cluster
func (c *Client) ExecuteScript(ctx context.Context, clusterID, query string) error {
	c.logger.WithFields(logrus.Fields{
		"cluster_id": clusterID,
		"query":      query[:min(100, len(query))], // Log first 100 chars
	}).Info("Executing script")

	// Create request
	req := &pb.ExecuteScriptRequest{
		ClusterId: clusterID,
		QueryStr:  query,
		Mutation:  false, // Default to non-mutation
	}

	// Add authentication to context
	authCtx := c.getAuthenticatedContext(ctx)

	// Execute the script
	stream, err := c.client.ExecuteScript(authCtx, req)
	if err != nil {
		return fmt.Errorf("failed to execute script: %w", err)
	}

	c.logger.Info("Script executed successfully, stream created")
	
	// For now, just try to receive one response to see if the basic call works
	resp, err := stream.Recv()
	if err != nil {
		if err == io.EOF {
			c.logger.Info("Received EOF immediately")
			return nil
		}
		return fmt.Errorf("failed to receive first response: %w", err)
	}

	c.logger.WithField("query_id", resp.QueryId).Info("Received first response")
	
	if resp.Status != nil {
		c.logger.WithFields(logrus.Fields{
			"status_code": resp.Status.ErrCode,
			"status_msg":  resp.Status.Msg,
		}).Info("Response status")
		
		if resp.Status.ErrCode != pb.Code_OK {
			return fmt.Errorf("script execution failed: %s", resp.Status.Msg)
		}
	}

	c.logger.Info("Script execution completed successfully")
	return nil
}

// min helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// WithTLS enables TLS
func WithTLS(enabled bool) Option {
	return func(c *Config) {
		c.TLSEnabled = enabled
	}
}

// WithCACert sets the CA certificate
func WithCACert(path string) Option {
	return func(c *Config) {
		c.CACert = path
	}
}

// WithClientCert sets the client certificate
func WithClientCert(certPath, keyPath string) Option {
	return func(c *Config) {
		c.ClientCert = certPath
		c.ClientKey = keyPath
	}
}

// WithServerName sets the server name
func WithServerName(name string) Option {
	return func(c *Config) {
		c.ServerName = name
	}
}

// WithInsecureSkipVerify skips TLS verification
func WithInsecureSkipVerify(skip bool) Option {
	return func(c *Config) {
		c.InsecureSkipVerify = skip
	}
}

// WithJWTAuth configures JWT authentication
func WithJWTAuth(signingKey, userID, orgID, email string) Option {
	return func(c *Config) {
		c.JWTSigningKey = signingKey
		c.JWTUserID = userID
		c.JWTOrgID = orgID
		c.JWTEmail = email
	}
}

// WithJWTServiceAuth configures JWT service authentication
func WithJWTServiceAuth(signingKey, serviceName string) Option {
	return func(c *Config) {
		c.JWTSigningKey = signingKey
		c.JWTServiceName = serviceName
	}
}

// WithDirectVizierKey configures direct Vizier key authentication
func WithDirectVizierKey(key string) Option {
	return func(c *Config) {
		c.DirectVizierKey = key
	}
}
