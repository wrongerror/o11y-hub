package vizier

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wrongerror/observo-connector/pkg/auth"
	"github.com/wrongerror/observo-connector/pkg/common"
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
			caCert, err := os.ReadFile(config.CACert)
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
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(creds))
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
		if resp.Status.Code != 0 { // 0 is OK status
			c.logger.WithFields(logrus.Fields{
				"code":    resp.Status.Code,
				"message": resp.Status.Message,
			}).Error("Health check failed")
			return fmt.Errorf("health check failed: %s", resp.Status.Message)
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

	// Process all responses from the stream
	return c.processScriptResponses(stream)
}

// processScriptResponses handles all responses from the ExecuteScript stream
func (c *Client) processScriptResponses(stream pb.VizierService_ExecuteScriptClient) error {
	responseCount := 0

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			c.logger.WithField("response_count", responseCount).Info("Script execution completed successfully")
			break
		}
		if err != nil {
			return fmt.Errorf("failed to receive response: %w", err)
		}

		responseCount++

		// Handle first response
		if responseCount == 1 {
			c.logger.WithField("query_id", resp.QueryId).Info("Received first response")
		}

		// Check for errors
		if resp.Status != nil && resp.Status.Code != 0 {
			return fmt.Errorf("script execution failed: %s", resp.Status.Message)
		}

		// Log detailed response in debug mode
		if c.logger.Level >= logrus.DebugLevel {
			c.logResponseDetails(resp, responseCount)
		}
	}

	return nil
}

// logResponseDetails logs detailed information about the response
func (c *Client) logResponseDetails(resp *pb.ExecuteScriptResponse, responseNum int) {
	c.logger.WithFields(logrus.Fields{
		"response_num": responseNum,
		"query_id":     resp.QueryId,
		"has_status":   resp.Status != nil,
	}).Debug("Response details")

	if resp.Status != nil {
		c.logger.WithFields(logrus.Fields{
			"status_code":    resp.Status.Code,
			"status_message": resp.Status.Message,
		}).Debug("Response status")
	}
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

// ExecuteScriptAndExtractData executes a script and returns structured data
func (c *Client) ExecuteScriptAndExtractData(ctx context.Context, clusterID, query string) (*common.QueryResult, error) {
	c.logger.WithFields(logrus.Fields{
		"cluster_id": clusterID,
		"query":      query[:min(100, len(query))],
	}).Info("Executing script for data extraction")

	// Create request
	req := &pb.ExecuteScriptRequest{
		ClusterId: clusterID,
		QueryStr:  query,
		Mutation:  false,
	}

	// Add authentication to context
	authCtx := c.getAuthenticatedContext(ctx)

	// Execute the script
	stream, err := c.client.ExecuteScript(authCtx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute script: %w", err)
	}

	// Extract data from responses
	return c.extractDataFromResponses(stream, query)
}

// extractDataFromResponses extracts structured data from script responses
func (c *Client) extractDataFromResponses(stream pb.VizierService_ExecuteScriptClient, query string) (*common.QueryResult, error) {
	result := &common.QueryResult{
		Data:      make([]map[string]interface{}, 0),
		Columns:   make([]string, 0),
		Timestamp: time.Now(),
		Query:     query,
	}

	responseCount := 0
	var columns []string
	var rows []map[string]interface{}

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to receive response: %w", err)
		}

		responseCount++

		// Check for errors
		if resp.Status != nil && resp.Status.Code != 0 {
			return nil, fmt.Errorf("script execution failed: %s", resp.Status.Message)
		}

		// Parse response data - this is a simplified implementation
		// In a real implementation, you would parse the actual Pixie response format
		if responseCount == 1 {
			// First response typically contains metadata or column information
			c.logger.WithField("query_id", resp.QueryId).Debug("Processing first response")
		}

		// For now, we'll create mock data based on the script type
		// In a real implementation, you would parse the actual response payload
		mockData := c.generateMockDataFromQuery(query)
		if len(mockData) > 0 {
			if len(columns) == 0 {
				// Extract columns from first data row
				for key := range mockData[0] {
					columns = append(columns, key)
				}
			}
			rows = append(rows, mockData...)
		}
	}

	result.Columns = columns
	result.Data = rows
	result.RowCount = len(rows)

	c.logger.WithFields(logrus.Fields{
		"response_count": responseCount,
		"columns":        len(columns),
		"rows":           len(rows),
	}).Info("Data extraction completed")

	return result, nil
}

// generateMockDataFromQuery generates mock data based on the query content
// TODO: Replace this with actual response parsing in production
func (c *Client) generateMockDataFromQuery(query string) []map[string]interface{} {
	queryLower := strings.ToLower(query)
	
	if strings.Contains(queryLower, "cpu") || strings.Contains(queryLower, "memory") {
		// Resource usage data
		return []map[string]interface{}{
			{
				"pod_name":     "app-pod-1",
				"namespace":    "default", 
				"cpu_usage":    0.25,
				"memory_usage": 0.45,
				"timestamp":    time.Now().Unix(),
			},
			{
				"pod_name":     "app-pod-2", 
				"namespace":    "default",
				"cpu_usage":    0.15,
				"memory_usage": 0.32,
				"timestamp":    time.Now().Unix(),
			},
		}
	}
	
	if strings.Contains(queryLower, "http") || strings.Contains(queryLower, "request") {
		// HTTP metrics data
		return []map[string]interface{}{
			{
				"service_name":    "frontend",
				"request_count":   1234,
				"error_count":     12,
				"avg_latency_ms":  45.6,
				"p99_latency_ms":  156.7,
				"timestamp":       time.Now().Unix(),
			},
			{
				"service_name":    "backend",
				"request_count":   5678,
				"error_count":     23,
				"avg_latency_ms":  23.4,
				"p99_latency_ms":  89.1,
				"timestamp":       time.Now().Unix(),
			},
		}
	}
	
	if strings.Contains(queryLower, "network") || strings.Contains(queryLower, "bytes") {
		// Network metrics data
		return []map[string]interface{}{
			{
				"pod_name":           "app-pod-1",
				"namespace":          "default",
				"bytes_sent":         1048576,
				"bytes_received":     2097152,
				"packets_sent":       1024,
				"packets_received":   2048,
				"timestamp":          time.Now().Unix(),
			},
		}
	}
	
	// Default empty data
	return []map[string]interface{}{}
}
