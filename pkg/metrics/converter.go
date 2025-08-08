package metrics

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wrongerror/observo-connector/pkg/common"
)

// PrometheusConverter converts script results to Prometheus metrics
type PrometheusConverter struct{}

// NewPrometheusConverter creates a new Prometheus converter
func NewPrometheusConverter() *PrometheusConverter {
	return &PrometheusConverter{}
}

// ConvertToMetrics converts query results to Prometheus metrics
func (pc *PrometheusConverter) ConvertToMetrics(results []*common.QueryResult) []common.Metric {
	var metrics []common.Metric

	for _, result := range results {
		if result == nil || len(result.Data) == 0 {
			continue
		}

		scriptName := result.Metadata["script_name"]
		category := result.Metadata["script_category"]

		switch scriptName {
		case "resource_usage":
			metrics = append(metrics, pc.convertResourceUsageMetrics(result, category)...)
		case "http_overview":
			metrics = append(metrics, pc.convertHTTPMetrics(result, category)...)
		case "network_stats":
			metrics = append(metrics, pc.convertNetworkMetrics(result, category)...)
		case "error_analysis":
			metrics = append(metrics, pc.convertErrorMetrics(result, category)...)
		default:
			// Generic conversion for unknown script types
			metrics = append(metrics, pc.convertGenericMetrics(result, scriptName, category)...)
		}
	}

	return metrics
}

// convertResourceUsageMetrics converts resource usage data to metrics
func (pc *PrometheusConverter) convertResourceUsageMetrics(result *common.QueryResult, category string) []common.Metric {
	var metrics []common.Metric

	for _, row := range result.Data {
		labels := make(map[string]string)
		
		// Extract labels
		if podName, ok := row["pod_name"].(string); ok {
			labels["pod"] = podName
		}
		if namespace, ok := row["namespace"].(string); ok {
			labels["namespace"] = namespace
		}

		timestamp := time.Now()
		if ts, ok := pc.extractTimestamp(row); ok {
			timestamp = ts
		}

		// CPU usage metric
		if cpuUsage, ok := pc.extractFloat64(row, "cpu_usage"); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_pod_cpu_usage_ratio",
				Type:        common.MetricTypeGauge,
				Description: "Pod CPU usage ratio (0-1)",
				Unit:        "ratio",
				Labels:      copyMap(labels),
				Value:       cpuUsage,
				Timestamp:   timestamp,
			})
		}

		// Memory usage metric
		if memUsage, ok := pc.extractFloat64(row, "memory_usage"); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_pod_memory_usage_ratio",
				Type:        common.MetricTypeGauge,
				Description: "Pod memory usage ratio (0-1)",
				Unit:        "ratio",
				Labels:      copyMap(labels),
				Value:       memUsage,
				Timestamp:   timestamp,
			})
		}
	}

	return metrics
}

// convertHTTPMetrics converts HTTP metrics data
func (pc *PrometheusConverter) convertHTTPMetrics(result *common.QueryResult, category string) []common.Metric {
	var metrics []common.Metric

	for _, row := range result.Data {
		labels := make(map[string]string)
		
		if serviceName, ok := row["service_name"].(string); ok {
			labels["service"] = serviceName
		}

		timestamp := time.Now()
		if ts, ok := pc.extractTimestamp(row); ok {
			timestamp = ts
		}

		// Request count metric
		if reqCount, ok := pc.extractFloat64(row, "request_count"); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_http_requests_total",
				Type:        common.MetricTypeCounter,
				Description: "Total number of HTTP requests",
				Unit:        "requests",
				Labels:      copyMap(labels),
				Value:       reqCount,
				Timestamp:   timestamp,
			})
		}

		// Error count metric
		if errorCount, ok := pc.extractFloat64(row, "error_count"); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_http_errors_total",
				Type:        common.MetricTypeCounter,
				Description: "Total number of HTTP errors",
				Unit:        "errors",
				Labels:      copyMap(labels),
				Value:       errorCount,
				Timestamp:   timestamp,
			})
		}

		// Average latency metric
		if avgLatency, ok := pc.extractFloat64(row, "avg_latency_ms"); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_http_request_duration_milliseconds",
				Type:        common.MetricTypeGauge,
				Description: "Average HTTP request duration in milliseconds",
				Unit:        "milliseconds",
				Labels:      copyMap(labels),
				Value:       avgLatency,
				Timestamp:   timestamp,
			})
		}

		// P99 latency metric
		if p99Latency, ok := pc.extractFloat64(row, "p99_latency_ms"); ok {
			labels["quantile"] = "0.99"
			metrics = append(metrics, common.Metric{
				Name:        "pixie_http_request_duration_milliseconds_quantile",
				Type:        common.MetricTypeGauge,
				Description: "99th percentile HTTP request duration in milliseconds",
				Unit:        "milliseconds",
				Labels:      copyMap(labels),
				Value:       p99Latency,
				Timestamp:   timestamp,
			})
		}
	}

	return metrics
}

// convertNetworkMetrics converts network metrics data
func (pc *PrometheusConverter) convertNetworkMetrics(result *common.QueryResult, category string) []common.Metric {
	var metrics []common.Metric

	for _, row := range result.Data {
		labels := make(map[string]string)
		
		if podName, ok := row["pod_name"].(string); ok {
			labels["pod"] = podName
		}
		if namespace, ok := row["namespace"].(string); ok {
			labels["namespace"] = namespace
		}

		timestamp := time.Now()
		if ts, ok := pc.extractTimestamp(row); ok {
			timestamp = ts
		}

		// Bytes sent metric
		if bytesSent, ok := pc.extractFloat64(row, "bytes_sent"); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_network_bytes_sent_total",
				Type:        common.MetricTypeCounter,
				Description: "Total bytes sent over network",
				Unit:        "bytes",
				Labels:      copyMap(labels),
				Value:       bytesSent,
				Timestamp:   timestamp,
			})
		}

		// Bytes received metric
		if bytesReceived, ok := pc.extractFloat64(row, "bytes_received"); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_network_bytes_received_total",
				Type:        common.MetricTypeCounter,
				Description: "Total bytes received over network",
				Unit:        "bytes",
				Labels:      copyMap(labels),
				Value:       bytesReceived,
				Timestamp:   timestamp,
			})
		}

		// Packets sent metric
		if packetsSent, ok := pc.extractFloat64(row, "packets_sent"); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_network_packets_sent_total",
				Type:        common.MetricTypeCounter,
				Description: "Total packets sent over network",
				Unit:        "packets",
				Labels:      copyMap(labels),
				Value:       packetsSent,
				Timestamp:   timestamp,
			})
		}

		// Packets received metric
		if packetsReceived, ok := pc.extractFloat64(row, "packets_received"); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_network_packets_received_total",
				Type:        common.MetricTypeCounter,
				Description: "Total packets received over network",
				Unit:        "packets",
				Labels:      copyMap(labels),
				Value:       packetsReceived,
				Timestamp:   timestamp,
			})
		}
	}

	return metrics
}

// convertErrorMetrics converts error analysis data
func (pc *PrometheusConverter) convertErrorMetrics(result *common.QueryResult, category string) []common.Metric {
	var metrics []common.Metric

	for _, row := range result.Data {
		labels := make(map[string]string)
		
		if serviceName, ok := row["service_name"].(string); ok {
			labels["service"] = serviceName
		}
		if errorType, ok := row["error_type"].(string); ok {
			labels["error_type"] = errorType
		}

		timestamp := time.Now()
		if ts, ok := pc.extractTimestamp(row); ok {
			timestamp = ts
		}

		// Error count by type
		if errorCount, ok := pc.extractFloat64(row, "error_count"); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_errors_by_type_total",
				Type:        common.MetricTypeCounter,
				Description: "Total errors by type",
				Unit:        "errors",
				Labels:      copyMap(labels),
				Value:       errorCount,
				Timestamp:   timestamp,
			})
		}
	}

	return metrics
}

// convertGenericMetrics converts unknown script results to generic metrics
func (pc *PrometheusConverter) convertGenericMetrics(result *common.QueryResult, scriptName, category string) []common.Metric {
	var metrics []common.Metric

	for _, row := range result.Data {
		labels := map[string]string{
			"script_name": scriptName,
			"category":    category,
		}

		timestamp := time.Now()
		if ts, ok := pc.extractTimestamp(row); ok {
			timestamp = ts
		}

		// Try to convert each numeric field to a metric
		for key, value := range row {
			if floatValue, ok := pc.extractFloat64FromInterface(value); ok {
				metricName := fmt.Sprintf("pixie_%s_%s", scriptName, pc.sanitizeMetricName(key))
				
				// Add other string fields as labels
				for labelKey, labelValue := range row {
					if labelKey != key && !pc.isNumericField(labelValue) {
						if strValue, ok := labelValue.(string); ok {
							labels[pc.sanitizeMetricName(labelKey)] = strValue
						}
					}
				}

				metrics = append(metrics, common.Metric{
					Name:        metricName,
					Type:        common.MetricTypeGauge,
					Description: fmt.Sprintf("Generic metric from %s script", scriptName),
					Labels:      copyMap(labels),
					Value:       floatValue,
					Timestamp:   timestamp,
				})
			}
		}
	}

	return metrics
}

// Helper functions

func (pc *PrometheusConverter) extractFloat64(row map[string]interface{}, key string) (float64, bool) {
	value, exists := row[key]
	if !exists {
		return 0, false
	}
	return pc.extractFloat64FromInterface(value)
}

func (pc *PrometheusConverter) extractFloat64FromInterface(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func (pc *PrometheusConverter) extractTimestamp(row map[string]interface{}) (time.Time, bool) {
	for _, key := range []string{"timestamp", "time", "ts"} {
		if value, exists := row[key]; exists {
			switch v := value.(type) {
			case time.Time:
				return v, true
			case int64:
				return time.Unix(v, 0), true
			case float64:
				return time.Unix(int64(v), 0), true
			case string:
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					return t, true
				}
			}
		}
	}
	return time.Time{}, false
}

func (pc *PrometheusConverter) isNumericField(value interface{}) bool {
	_, ok := pc.extractFloat64FromInterface(value)
	return ok
}

func (pc *PrometheusConverter) sanitizeMetricName(name string) string {
	// Replace invalid characters with underscores
	result := strings.ReplaceAll(name, " ", "_")
	result = strings.ReplaceAll(result, "-", "_")
	result = strings.ReplaceAll(result, ".", "_")
	result = strings.ToLower(result)
	return result
}

func copyMap(original map[string]string) map[string]string {
	copy := make(map[string]string)
	for k, v := range original {
		copy[k] = v
	}
	return copy
}
