package common

import (
"context"
"time"
)

// DataSource represents a generic observability data source
type DataSource interface {
	// Connect establishes connection to the data source
	Connect(ctx context.Context) error
	
	// HealthCheck checks if the data source is healthy
	HealthCheck(ctx context.Context) error
	
	// Query executes a query against the data source
	Query(ctx context.Context, query string) (QueryResult, error)
	
	// Close closes the connection to the data source
	Close() error
}

// QueryResult represents the result of a query
type QueryResult struct {
	Data      []map[string]interface{} `json:"data"`
	Columns   []string                 `json:"columns"`
	RowCount  int                      `json:"row_count"`
	Timestamp time.Time                `json:"timestamp"`
	Metadata  map[string]string        `json:"metadata,omitempty"`
}

// ConnectionConfig represents generic connection configuration
type ConnectionConfig struct {
	Address    string `json:"address"`
	ClusterID  string `json:"cluster_id,omitempty"`
	
	// TLS Configuration
	DisableSSL   bool   `json:"disable_ssl,omitempty"`
	SkipVerify   bool   `json:"skip_verify,omitempty"`
	CACert       string `json:"ca_cert,omitempty"`
	ClientCert   string `json:"client_cert,omitempty"`
	ClientKey    string `json:"client_key,omitempty"`
	ServerName   string `json:"server_name,omitempty"`
	
	// Additional metadata
	Metadata map[string]string `json:"metadata,omitempty"`
}
