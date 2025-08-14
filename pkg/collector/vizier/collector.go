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

	// Metric descriptors
	httpRequestsTotal       *collector.TypedDesc
	httpRequestDuration     *collector.TypedDesc
	httpErrorRate           *collector.TypedDesc
	serviceCPUUsage         *collector.TypedDesc
	serviceMemoryUsage      *collector.TypedDesc
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

		// Define metric descriptors
		httpRequestsTotal: collector.NewTypedDesc(
			"http_requests_total",
			"Total number of HTTP requests",
			prometheus.CounterValue,
			[]string{"service", "method", "status_code"},
		),
		httpRequestDuration: collector.NewTypedDesc(
			"http_request_duration_seconds",
			"HTTP request duration in seconds",
			prometheus.GaugeValue,
			[]string{"service", "method", "quantile"},
		),
		httpErrorRate: collector.NewTypedDesc(
			"http_error_rate",
			"HTTP error rate (percentage)",
			prometheus.GaugeValue,
			[]string{"service"},
		),
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

// collectHTTPMetrics collects HTTP-related metrics
func (c *Collector) collectHTTPMetrics(ctx context.Context, ch chan<- prometheus.Metric) error {
	// Execute service_performance script
	params := map[string]string{
		"start_time": "-5m",
		"namespace":  "",
	}

	result, err := c.scriptExecutor.ExecuteBuiltinScriptForMetrics(ctx, c.config.VizierClusterID, "service_performance", params)
	if err != nil {
		return fmt.Errorf("failed to execute service_performance script: %w", err)
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

		// HTTP request count
		if count, ok := row["http_request_count"]; ok {
			if countVal, err := parseFloat64(count); err == nil {
				ch <- c.httpRequestsTotal.MustNewConstMetric(countVal, service, "GET", "200")
			}
		}

		// HTTP request rate (RPS)
		if rps, ok := row["http_rps"]; ok {
			if rpsVal, err := parseFloat64(rps); err == nil {
				ch <- prometheus.MustNewConstMetric(
					prometheus.NewDesc(
						prometheus.BuildFQName(collector.Namespace, "", "http_requests_per_second"),
						"HTTP requests per second",
						[]string{"service"},
						nil,
					),
					prometheus.GaugeValue,
					rpsVal,
					service,
				)
			}
		}

		// HTTP average latency
		if latency, ok := row["http_avg_latency_ms"]; ok {
			if latencyVal, err := parseFloat64(latency); err == nil {
				// Convert milliseconds to seconds
				ch <- c.httpRequestDuration.MustNewConstMetric(latencyVal/1000.0, service, "GET", "0.5")
			}
		}

		// HTTP error rate
		if errorRate, ok := row["http_error_rate"]; ok {
			if errorRateVal, err := parseFloat64(errorRate); err == nil {
				ch <- c.httpErrorRate.MustNewConstMetric(errorRateVal*100, service)
			}
		}
	}

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

func init() {
	// Register the Vizier collector
	collector.RegisterCollector("vizier", NewCollector)
}
