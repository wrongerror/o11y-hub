package vizier

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"github.com/wrongerror/o11y-hub/pkg/collector"
	"github.com/wrongerror/o11y-hub/pkg/k8s"
	"github.com/wrongerror/o11y-hub/pkg/vizier"
)

// Collector implements collector.Collector for Pixie/Vizier
type Collector struct {
	logger       *logrus.Logger
	config       collector.Config
	vizierClient *vizier.Client
	k8sManager   *k8s.Manager

	// HTTP metrics with expiring histograms
	httpMetrics *HTTPMetrics
}

// NewCollector creates a new Vizier collector
func NewCollector(logger *logrus.Logger, config collector.Config) (collector.Collector, error) {
	// Create Vizier client options
	var opts []vizier.Option

	// Add TLS configuration if enabled
	if config.TLSEnabled {
		opts = append(opts, func(c *vizier.Config) {
			c.TLSEnabled = true
			c.InsecureSkipVerify = config.TLSSkipVerify
			c.CACert = config.TLSCAFile
			c.ClientCert = config.TLSCertFile
			c.ClientKey = config.TLSKeyFile
		})
	}

	// Add JWT authentication
	if config.VizierJWTKey != "" {
		opts = append(opts, func(c *vizier.Config) {
			c.JWTSigningKey = config.VizierJWTKey
			c.JWTServiceName = config.VizierJWTService
		})
	}

	// Create Vizier client
	vizierClient, err := vizier.NewClient(config.VizierAddress, logger, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create vizier client: %w", err)
	}

	// Create K8s manager
	k8sManager, err := k8s.NewManager(logger, config.KubeconfigPath)
	if err != nil {
		logger.WithError(err).Warn("Failed to create Kubernetes manager, continuing without K8s metadata")
		k8sManager = nil
	}

	collector := &Collector{
		logger:       logger,
		config:       config,
		vizierClient: vizierClient,
		k8sManager:   k8sManager,

		// Initialize HTTP metrics with histograms and TTL support
		httpMetrics: NewHTTPMetrics(logger),
	}

	// Start K8s manager if available
	if collector.k8sManager != nil {
		// Set K8s manager for HTTP metrics
		collector.httpMetrics.SetK8sManager(collector.k8sManager)

		go func() {
			if err := collector.k8sManager.Start(context.Background()); err != nil {
				logger.WithError(err).Error("Failed to start Kubernetes manager")
			}
		}()
	}

	return collector, nil
}

// Update implements collector.Collector interface
func (c *Collector) Update(ch chan<- prometheus.Metric) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Collect HTTP metrics
	if err := c.collectHTTPMetrics(ctx, ch); err != nil {
		c.logger.WithError(err).Warn("Failed to collect HTTP metrics")
	}

	return nil
}

// collectHTTPMetrics collects HTTP-related metrics from Vizier http_events table
func (c *Collector) collectHTTPMetrics(ctx context.Context, ch chan<- prometheus.Metric) error {
	// Execute enhanced PxL script to get HTTP events with remote endpoint resolution
	script := `import px

# Get HTTP events from the last 30 seconds
df = px.DataFrame(table='http_events', start_time='-30s')

# Extract ctx metadata fields for local endpoint
df.service = df.ctx['service']
df.namespace = df.ctx['namespace'] 
df.pod_name = df.ctx['pod_name']
df.container_name = df.ctx['container_name']
df.node_name = df.ctx['node_name']

# Resolve remote endpoint information using Pixie's built-in functions
# Based on trace_role, remote_addr represents either client or server
df.remote_pod_id = px.ip_to_pod_id(df.remote_addr)
df.remote_pod_name = px.pod_id_to_pod_name(df.remote_pod_id)
df.remote_service_name = px.pod_id_to_service_name(df.remote_pod_id)
df.remote_namespace = px.pod_id_to_namespace(df.remote_pod_id)
df.remote_node_name = px.pod_id_to_node_name(df.remote_pod_id)

# Keep all other fields as-is, display all fields including resolved remote endpoint info
px.display(df, 'http_events')
`

	result, err := c.vizierClient.ExecuteScriptAndExtractData(ctx, c.config.VizierClusterID, script)
	if err != nil {
		return fmt.Errorf("failed to execute HTTP events script: %w", err)
	}

	if result == nil || len(result.Data) == 0 {
		return collector.ErrNoData
	}

	// Log HTTP events data with all fields
	c.logger.Infof("Found %d HTTP events with full field data", len(result.Data))

	// Process each HTTP event record with the new histogram-based metrics system
	eventCount := 0
	processedCount := 0

	for i, row := range result.Data {
		eventCount++

		if i < 2 { // Only log first 2 records to avoid spam
			c.logger.Infof("=== HTTP Event %d Full Data ===", i+1)
			c.logger.Infof("Basic: trace_role=%v, method=%v, path=%v, status=%v",
				row["trace_role"], row["req_method"], row["req_path"], row["resp_status"])
			c.logger.Infof("Timing: time=%v, latency=%v", row["time_"], row["latency"])
			c.logger.Infof("Network: remote=%v:%v, local=%v:%v",
				row["remote_addr"], row["remote_port"], row["local_addr"], row["local_port"])
			c.logger.Infof("Ctx metadata:")
			c.logger.Infof("  service=%v", row["service"])
			c.logger.Infof("  namespace=%v", row["namespace"])
			c.logger.Infof("  pod_name=%v", row["pod_name"])
			c.logger.Infof("  container_name=%v", row["container_name"])
			c.logger.Infof("Body sizes: req=%v, resp=%v", row["req_body_size"], row["resp_body_size"])

			// Log truncated body content to avoid excessive output
			reqBody := truncateString(getStringValue(row, "req_body", ""), 200)
			respBody := truncateString(getStringValue(row, "resp_body", ""), 200)
			c.logger.Infof("Body content (truncated): req=%q, resp=%q", reqBody, respBody)
		}

		// Process event with the new histogram metrics system
		if err := c.httpMetrics.ProcessHTTPEvent(row); err != nil {
			c.logger.WithError(err).Debug("Failed to process HTTP event")
		} else {
			processedCount++
		}
	}

	c.logger.Infof("Processed %d/%d HTTP events successfully", processedCount, eventCount)

	// Collect all histogram metrics
	c.httpMetrics.Collect(ch)

	return nil
}

// Helper functions
func parseFloat64(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", value)
	}
}

func getStringValue(row map[string]interface{}, key, defaultValue string) string {
	if value, ok := row[key]; ok {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return defaultValue
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func init() {
	// Register the Vizier collector
	collector.RegisterCollector("vizier", NewCollector)
}
