package vizier

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"github.com/wrongerror/o11y-hub/pkg/collector"
	"github.com/wrongerror/o11y-hub/pkg/k8s"
	"github.com/wrongerror/o11y-hub/pkg/logging"
	"github.com/wrongerror/o11y-hub/pkg/vizier"
)

// Collector implements collector.Collector for Pixie/Vizier
type Collector struct {
	logger       *logrus.Logger
	config       collector.Config
	vizierClient *vizier.Client
	k8sManager   *k8s.Manager

	// Unified HTTP event processing
	httpEventProcessor *HTTPEventProcessor

	// HTTP metrics with expiring histograms
	httpMetrics *HTTPMetrics
	// Network flow metrics with expiring counters
	networkMetrics *NetworkMetrics

	// HTTP traffic logging components
	httpTrafficLogger *HTTPTrafficLogger
	logManager        *logging.LogManager
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

	// Initialize log manager if traffic logging is enabled
	var logManager *logging.LogManager
	var httpTrafficLogger *HTTPTrafficLogger

	if config.EnableHTTPTraffic {
		// Create log configuration
		logConfig := &logging.LogConfig{
			Directory:   config.LogDirectory,
			MaxFiles:    config.LogMaxFiles,
			EnabledLogs: map[string]bool{"http_traffic": true},
			JSONFormat:  config.LogJSONFormat,
			MaxFileSize: config.LogMaxFileSize,
		}

		// Apply defaults if values are not set
		if logConfig.Directory == "" {
			logConfig.Directory = "./logs"
		}
		if logConfig.MaxFiles == 0 {
			logConfig.MaxFiles = 5
		}
		if logConfig.MaxFileSize == 0 {
			logConfig.MaxFileSize = 100 * 1024 * 1024 // 100MB
		}

		// Create and initialize log manager
		logManager = logging.NewLogManager(logConfig, logger)
		if err := logManager.Initialize(); err != nil {
			logger.WithError(err).Warn("Failed to initialize log manager, continuing without traffic logging")
			logManager = nil
		} else {
			// Create HTTP traffic logger
			httpTrafficLogger = NewHTTPTrafficLogger(logManager, logger)
			if k8sManager != nil {
				httpTrafficLogger.SetK8sManager(k8sManager)
			}

			// Start the HTTP traffic logger
			if err := httpTrafficLogger.Start(); err != nil {
				logger.WithError(err).Warn("Failed to start HTTP traffic logger")
				httpTrafficLogger = nil
			} else {
				logger.Info("HTTP traffic logging enabled")
			}
		}
	}

	collector := &Collector{
		logger:       logger,
		config:       config,
		vizierClient: vizierClient,
		k8sManager:   k8sManager,

		// Initialize HTTP metrics with histograms and TTL support
		httpMetrics: NewHTTPMetrics(logger),
		// Initialize network flow metrics with counters and TTL support
		networkMetrics: NewNetworkMetrics(logger),

		// Traffic logging components
		httpTrafficLogger: httpTrafficLogger,
		logManager:        logManager,
	}

	// Create unified HTTP event processor
	collector.httpEventProcessor = NewHTTPEventProcessor(logger)
	if collector.k8sManager != nil {
		collector.httpEventProcessor.SetK8sManager(collector.k8sManager)
	}

	// Register HTTP metrics as subscriber
	collector.httpEventProcessor.AddSubscriber(collector.httpMetrics)

	// Register HTTP traffic logger as subscriber if enabled
	if httpTrafficLogger != nil {
		collector.httpEventProcessor.AddSubscriber(httpTrafficLogger)
	}

	// Start the unified event processor
	collector.httpEventProcessor.Start()

	// Start K8s manager if available
	if collector.k8sManager != nil {
		// Set K8s manager for HTTP metrics (for backward compatibility)
		collector.httpMetrics.SetK8sManager(collector.k8sManager)
		// Set K8s manager for network metrics
		collector.networkMetrics.SetK8sManager(collector.k8sManager)

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

	// Collect network flow metrics
	if err := c.collectNetworkMetrics(ctx, ch); err != nil {
		c.logger.WithError(err).Warn("Failed to collect network flow metrics")
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

	// Process each HTTP event record through the unified event processor
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

		// Process event through the unified event processor (will notify all subscribers)
		if err := c.httpEventProcessor.ProcessEvent(row); err != nil {
			c.logger.WithError(err).Debug("Failed to process HTTP event through unified processor")
		} else {
			processedCount++
		}
	}

	c.logger.Infof("Processed %d/%d HTTP events successfully through unified processor", processedCount, eventCount)

	// Collect all histogram metrics from the HTTP metrics subscriber
	c.httpMetrics.Collect(ch)

	return nil
}

// collectNetworkMetrics collects network flow metrics from Vizier conn_stats table
func (c *Collector) collectNetworkMetrics(ctx context.Context, ch chan<- prometheus.Metric) error {
	// Execute PxL script to get connection stats from conn_stats table
	// Based on connection_throughput_stats from pxviews.pxl and net_flow_graph.pxl patterns
	script := `import px

# Get connection stats from the last 30 seconds
df = px.DataFrame(table='conn_stats', start_time='-30s')

# Extract pod_id and context metadata BEFORE aggregation
df.pod_id = df.ctx['pod_id']
df.pod = df.ctx['pod']  # For filtering non-k8s sources
df.src_namespace = df.ctx['namespace']
df.src_pod_name = df.ctx['pod_name']
df.src_service_name = df.ctx['service']
df.src_node_name = df.ctx['node_name']

# Filter out any non-k8s sources (following net_flow_graph.pxl pattern)
df = df[df.pod != '']

# Resolve destination endpoint information BEFORE aggregation
# This provides the highest priority resolution for cluster-internal IPs
df.dst_pod_id = px.ip_to_pod_id(df.remote_addr)
df.dst_pod_name = px.pod_id_to_pod_name(df.dst_pod_id)
df.dst_service_name = px.pod_id_to_service_name(df.dst_pod_id)
df.dst_namespace = px.pod_id_to_namespace(df.dst_pod_id)
df.dst_node_name = px.pod_id_to_node_name(df.dst_pod_id)

# For IPs that couldn't be resolved by Pixie (external IPs, some node IPs),
# use nslookup as fallback (following net_flow_graph.pxl pattern)
df.dst_nslookup = px.nslookup(df.remote_addr)

# Now perform aggregation with all needed fields
# Group by all identity fields to preserve endpoint information
df = df.groupby(['upid', 'trace_role', 'remote_addr', 'pod_id', 'dst_pod_id',
                'src_namespace', 'src_pod_name', 'src_service_name', 'src_node_name',
                'dst_pod_name', 'dst_service_name', 'dst_namespace', 'dst_node_name',
                'dst_nslookup']).agg(
    bytes_recv=('bytes_recv', px.max),
    bytes_sent=('bytes_sent', px.max),
    time_=('time_', px.max),
)

# Add source and destination addresses
df.src_address = ''  # Will be filled by Go code using K8s manager
df.dst_address = df.remote_addr

# Add type fields based on resolved info
# For source: prefer service > pod > node > ip
df.src_type = px.select(df.src_service_name != '', 'service', 
              px.select(df.src_pod_name != '', 'pod',
              px.select(df.src_node_name != '', 'node', 'ip')))

# For destination: prefer service > pod > node > ip
# Don't distinguish between external/internal IPs at PxL level, let Go code handle it
df.dst_type = px.select(df.dst_service_name != '', 'service',
              px.select(df.dst_pod_name != '', 'pod', 
              px.select(df.dst_node_name != '', 'node', 'ip')))

# Add empty owner fields (will be filled by Go code using K8s manager)
df.src_owner_name = ''
df.src_owner_type = ''
df.dst_owner_name = ''
df.dst_owner_type = ''

# Display final connection flow stats
px.display(df, 'conn_stats')
`

	result, err := c.vizierClient.ExecuteScriptAndExtractData(ctx, c.config.VizierClusterID, script)
	if err != nil {
		return fmt.Errorf("failed to execute conn_stats script: %w", err)
	}

	if result == nil || len(result.Data) == 0 {
		return collector.ErrNoData
	}

	// Log network flow data
	c.logger.Infof("Found %d connection stats events", len(result.Data))

	// Process each network flow event record
	eventCount := 0
	processedCount := 0

	for i, row := range result.Data {
		eventCount++

		if i < 2 { // Only log first 2 records to avoid spam
			c.logger.Infof("=== Connection Stats Event %d ===", i+1)
			c.logger.Infof("Flow: %v:%v -> %v:%v (role=%v)",
				row["src_address"], row["local_port"], row["dst_address"], row["remote_port"], row["trace_role"])
			c.logger.Infof("Bytes: sent=%v, recv=%v", row["bytes_sent"], row["bytes_recv"])
			c.logger.Infof("Connections: open=%v, close=%v, active=%v", row["conn_open"], row["conn_close"], row["conn_active"])
			c.logger.Infof("Source: ns=%v, pod=%v, svc=%v, node=%v",
				row["src_namespace"], row["src_pod_name"], row["src_service_name"], row["src_node_name"])
			c.logger.Infof("Destination: ns=%v, pod=%v, svc=%v, node=%v",
				row["dst_namespace"], row["dst_pod_name"], row["dst_service_name"], row["dst_node_name"])
		}

		// Process event with the network metrics system
		if err := c.networkMetrics.ProcessNetworkEvent(row); err != nil {
			c.logger.WithError(err).Debug("Failed to process connection stats event")
		} else {
			processedCount++
		}
	}

	c.logger.Infof("Processed %d/%d connection stats events successfully", processedCount, eventCount)

	// Collect all counter metrics
	c.networkMetrics.Collect(ch)

	return nil
}

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
		return 0, fmt.Errorf("cannot convert string %q to float64", v)
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

// Stop gracefully stops the collector and cleans up resources
func (c *Collector) Stop() {
	// Stop the unified HTTP event processor first
	if c.httpEventProcessor != nil {
		c.httpEventProcessor.Stop()
		c.logger.Info("HTTP event processor stopped")
	}

	if c.httpTrafficLogger != nil {
		c.httpTrafficLogger.Stop()
		c.logger.Info("HTTP traffic logger stopped")
	}

	if c.logManager != nil {
		if err := c.logManager.Close(); err != nil {
			c.logger.WithError(err).Warn("Failed to close log manager")
		}
		c.logger.Info("Log manager closed")
	}

	c.logger.Info("Vizier collector stopped")
}

func init() {
	// Register the Vizier collector
	collector.RegisterCollector("vizier", NewCollector)
}
