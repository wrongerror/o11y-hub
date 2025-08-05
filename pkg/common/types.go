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

	// Extended fields for observability data
	Query      string        `json:"query"`
	ExecutedAt time.Time     `json:"executed_at"`
	Duration   time.Duration `json:"duration"`
	Metrics    []Metric      `json:"metrics,omitempty"`
	Logs       []LogEntry    `json:"logs,omitempty"`
	Traces     []TraceSpan   `json:"traces,omitempty"`
	RawData    interface{}   `json:"raw_data,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// ConnectionConfig represents generic connection configuration
type ConnectionConfig struct {
	Address   string `json:"address"`
	ClusterID string `json:"cluster_id,omitempty"`

	// TLS Configuration
	DisableSSL bool   `json:"disable_ssl,omitempty"`
	SkipVerify bool   `json:"skip_verify,omitempty"`
	CACert     string `json:"ca_cert,omitempty"`
	ClientCert string `json:"client_cert,omitempty"`
	ClientKey  string `json:"client_key,omitempty"`
	ServerName string `json:"server_name,omitempty"`

	// Additional metadata
	Metadata map[string]string `json:"metadata,omitempty"`
}

// DataPoint 表示一个数据点
type DataPoint struct {
	Timestamp time.Time              `json:"timestamp"`
	Value     interface{}            `json:"value"`
	Labels    map[string]string      `json:"labels"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// MetricType 表示指标类型
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
	MetricTypeSummary   MetricType = "summary"
)

// Metric 表示一个指标
type Metric struct {
	Name        string            `json:"name"`
	Type        MetricType        `json:"type"`
	Description string            `json:"description"`
	Unit        string            `json:"unit,omitempty"`
	Labels      map[string]string `json:"labels"`
	Value       float64           `json:"value"`
	Timestamp   time.Time         `json:"timestamp"`
}

// LogEntry 表示一个日志条目
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Source    string                 `json:"source"`
	Labels    map[string]string      `json:"labels"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
}

// TraceSpan 表示一个trace span
type TraceSpan struct {
	TraceID       string                 `json:"trace_id"`
	SpanID        string                 `json:"span_id"`
	ParentSpanID  string                 `json:"parent_span_id,omitempty"`
	OperationName string                 `json:"operation_name"`
	StartTime     time.Time              `json:"start_time"`
	Duration      time.Duration          `json:"duration"`
	Tags          map[string]interface{} `json:"tags"`
	Logs          []SpanLog              `json:"logs,omitempty"`
}

// SpanLog 表示span内的日志事件
type SpanLog struct {
	Timestamp time.Time              `json:"timestamp"`
	Fields    map[string]interface{} `json:"fields"`
}

// ExportFormat 表示导出格式
type ExportFormat string

const (
	FormatPrometheusText ExportFormat = "prometheus"
	FormatJSON           ExportFormat = "json"
	FormatCSV            ExportFormat = "csv"
	FormatOpenTelemetry  ExportFormat = "otlp"
)

// ExportOptions 导出选项
type ExportOptions struct {
	Format      ExportFormat      `json:"format"`
	Compression bool              `json:"compression"`
	Headers     map[string]string `json:"headers,omitempty"`
	BatchSize   int               `json:"batch_size,omitempty"`
}
