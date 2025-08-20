package vizier

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"github.com/wrongerror/observo-connector/pkg/collector"
	"github.com/wrongerror/observo-connector/pkg/scripts"
	"github.com/wrongerror/observo-connector/pkg/vizier"
)

// Collector implements collector.Collector for Pixie/Vizier
type Collector struct {
	logger         *logrus.Logger
	config         collector.Config
	vizierClient   *vizier.Client
	scriptExecutor *scripts.Executor

	// HTTP metrics with expiring histograms
	httpMetrics *HTTPMetrics

	// Resource metric descriptors
	serviceCPUUsage    *collector.TypedDesc
	serviceMemoryUsage *collector.TypedDesc

	// Network metric descriptors
	networkBytesTotal       *collector.TypedDesc
	networkConnectionsTotal *collector.TypedDesc
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

	// Create script executor
	scriptExecutor := scripts.NewExecutor(vizierClient, logger)

	return &Collector{
		logger:         logger,
		config:         config,
		vizierClient:   vizierClient,
		scriptExecutor: scriptExecutor,

		// Initialize HTTP metrics with histograms and TTL support
		httpMetrics: NewHTTPMetrics(logger),

		// Define resource metric descriptors
		serviceCPUUsage: collector.NewTypedDesc(
			"service_cpu_usage_nanoseconds_total",
			"Service CPU usage in nanoseconds",
			prometheus.CounterValue,
			[]string{"service", "namespace", "pod"},
		),
		serviceMemoryUsage: collector.NewTypedDesc(
			"service_memory_usage_bytes",
			"Service memory usage in bytes",
			prometheus.GaugeValue,
			[]string{"service", "namespace", "pod"},
		),

		// Define network metric descriptors
		networkBytesTotal: collector.NewTypedDesc(
			"network_bytes_total",
			"Total network bytes transferred",
			prometheus.CounterValue,
			[]string{"service", "direction", "protocol"},
		),
		networkConnectionsTotal: collector.NewTypedDesc(
			"network_connections_total",
			"Total network connections",
			prometheus.CounterValue,
			[]string{"service", "state", "protocol"},
		),
	}, nil
}

// Update implements collector.Collector interface
func (c *Collector) Update(ch chan<- prometheus.Metric) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Collect HTTP metrics
	if err := c.collectHTTPMetrics(ctx, ch); err != nil {
		c.logger.WithError(err).Warn("Failed to collect HTTP metrics")
	}

	// Collect resource metrics
	if err := c.collectResourceMetrics(ctx, ch); err != nil {
		c.logger.WithError(err).Warn("Failed to collect resource metrics")
	}

	// Collect network metrics
	if err := c.collectNetworkMetrics(ctx, ch); err != nil {
		c.logger.WithError(err).Warn("Failed to collect network metrics")
	}

	return nil
}

// collectHTTPMetrics collects HTTP-related metrics from Vizier http_events table
func (c *Collector) collectHTTPMetrics(ctx context.Context, ch chan<- prometheus.Metric) error {
	// Execute simplified PxL script to get all HTTP events data with ctx metadata
	script := `import px

# Get HTTP events from the last 30 seconds
df = px.DataFrame(table='http_events', start_time='-30s')

# Extract ctx metadata fields
df.service = df.ctx['service']
df.namespace = df.ctx['namespace'] 
df.pod_name = df.ctx['pod_name']
df.container_name = df.ctx['container_name']

# Keep all other fields as-is, display all fields including ctx metadata
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

// collectResourceMetrics collects CPU and memory metrics
func (c *Collector) collectResourceMetrics(ctx context.Context, ch chan<- prometheus.Metric) error {
	// Execute resource_usage script
	params := map[string]string{
		"start_time": "-5m",
		"namespace":  "",
	}

	result, err := c.scriptExecutor.ExecuteBuiltinScriptForMetrics(ctx, c.config.VizierClusterID, "resource_usage", params)
	if err != nil {
		return fmt.Errorf("failed to execute resource_usage script: %w", err)
	}

	if result == nil || len(result.Data) == 0 {
		return collector.ErrNoData
	}

	// Convert data to metrics
	for _, row := range result.Data {
		service, ok := row["service"].(string)
		if !ok {
			continue
		}

		namespace := getStringValue(row, "namespace", "default")
		pod := getStringValue(row, "pod", "unknown")

		// CPU usage
		if cpuUsage, ok := row["avg_cpu_usage"]; ok {
			if cpuVal, err := parseFloat64(cpuUsage); err == nil {
				ch <- c.serviceCPUUsage.MustNewConstMetric(cpuVal, service, namespace, pod)
			}
		}

		// Memory usage in bytes
		if memUsage, ok := row["avg_memory_bytes"]; ok {
			if memVal, err := parseFloat64(memUsage); err == nil {
				ch <- c.serviceMemoryUsage.MustNewConstMetric(memVal, service, namespace, pod)
			}
		}
	}

	return nil
}

// collectNetworkMetrics collects network-related metrics
func (c *Collector) collectNetworkMetrics(ctx context.Context, ch chan<- prometheus.Metric) error {
	// Execute network_stats script
	params := map[string]string{
		"start_time": "-5m",
		"namespace":  "",
	}

	result, err := c.scriptExecutor.ExecuteBuiltinScriptForMetrics(ctx, c.config.VizierClusterID, "network_stats", params)
	if err != nil {
		return fmt.Errorf("failed to execute network_stats script: %w", err)
	}

	if result == nil || len(result.Data) == 0 {
		return collector.ErrNoData
	}

	// Convert data to metrics
	for _, row := range result.Data {
		service, ok := row["service"].(string)
		if !ok {
			continue
		}

		// Network bytes sent/received
		if bytesRx, ok := row["bytes_rx"]; ok {
			if bytesRxVal, err := parseFloat64(bytesRx); err == nil {
				ch <- c.networkBytesTotal.MustNewConstMetric(bytesRxVal, service, "rx", "tcp")
			}
		}

		if bytesTx, ok := row["bytes_tx"]; ok {
			if bytesTxVal, err := parseFloat64(bytesTx); err == nil {
				ch <- c.networkBytesTotal.MustNewConstMetric(bytesTxVal, service, "tx", "tcp")
			}
		}

		// Network connections
		if connections, ok := row["active_connections"]; ok {
			if connVal, err := parseFloat64(connections); err == nil {
				ch <- c.networkConnectionsTotal.MustNewConstMetric(connVal, service, "established", "tcp")
			}
		}
	}

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
