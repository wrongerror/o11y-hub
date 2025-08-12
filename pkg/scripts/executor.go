package scripts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wrongerror/observo-connector/pkg/common"
	"github.com/wrongerror/observo-connector/pkg/vizier"
)

// Executor manages script execution and result handling
type Executor struct {
	client        *vizier.Client
	logger        *logrus.Logger
	scriptManager *ScriptManager
}

// NewExecutor creates a new script executor
func NewExecutor(client *vizier.Client, logger *logrus.Logger) *Executor {
	executor := &Executor{
		client: client,
		logger: logger,
	}

	// Initialize script manager with default scripts directory
	executor.scriptManager = NewScriptManager("./scripts")

	// Load scripts on startup
	if err := executor.scriptManager.LoadScripts(); err != nil {
		logger.WithError(err).Warn("Failed to load some scripts from directory, falling back to builtin scripts")
	}

	return executor
}

// ExecuteBuiltinScript executes a script by name with parameters
func (e *Executor) ExecuteBuiltinScript(ctx context.Context, clusterID, scriptName string, params map[string]string) error {
	// Try to get script from script manager first
	script, err := e.scriptManager.GetScript(scriptName)
	if err != nil {
		// Fall back to builtin scripts
		return e.executeBuiltinScriptLegacy(ctx, clusterID, scriptName, params)
	}

	e.logger.WithFields(logrus.Fields{
		"script_name": scriptName,
		"description": script.Short,
		"category":    script.Category,
		"parameters":  params,
	}).Info("Executing script from file")

	// Render the script with parameters
	renderedScript, err := e.scriptManager.ExecuteScript(scriptName, params)
	if err != nil {
		return fmt.Errorf("failed to render script: %w", err)
	}

	e.logger.WithField("rendered_script", renderedScript).Debug("Rendered script")

	// Execute the script
	return e.client.ExecuteScript(ctx, clusterID, renderedScript)
}

// executeBuiltinScriptLegacy executes a builtin script using the legacy method
func (e *Executor) executeBuiltinScriptLegacy(ctx context.Context, clusterID, scriptName string, params map[string]string) error {
	// Get the script template from builtin scripts
	script, exists := BuiltinScripts[scriptName]
	if !exists {
		return fmt.Errorf("script '%s' not found in both file system and builtin scripts", scriptName)
	}

	e.logger.WithFields(logrus.Fields{
		"script_name": scriptName,
		"description": script.Description,
		"category":    script.Category,
		"parameters":  params,
	}).Info("Executing builtin script (legacy)")

	// Render the script with parameters
	renderedScript, err := script.Execute(params)
	if err != nil {
		return fmt.Errorf("failed to render script: %w", err)
	}

	e.logger.WithField("rendered_script", renderedScript).Debug("Rendered script")

	// Execute the script
	return e.client.ExecuteScript(ctx, clusterID, renderedScript)
}

// ListBuiltinScripts returns information about all available scripts
func (e *Executor) ListBuiltinScripts() map[string]ScriptInfo {
	result := make(map[string]ScriptInfo)

	// Get scripts from script manager first
	for _, name := range e.scriptManager.ListScripts() {
		script, err := e.scriptManager.GetScript(name)
		if err != nil {
			continue
		}

		var params []Parameter
		for _, p := range script.Parameters {
			params = append(params, Parameter{
				Name:         p.Name,
				Type:         p.Type,
				Description:  p.Description,
				DefaultValue: fmt.Sprintf("%v", p.Default),
				Required:     p.Required,
			})
		}

		result[name] = ScriptInfo{
			Name:        script.Name,
			Description: script.Short,
			Category:    script.Category,
			Parameters:  params,
		}
	}

	// Add builtin scripts as fallback
	for name, script := range BuiltinScripts {
		if _, exists := result[name]; !exists {
			result[name] = ScriptInfo{
				Name:        script.Name,
				Description: script.Description,
				Category:    script.Category,
				Parameters:  script.Parameters,
			}
		}
	}

	return result
}

// ScriptInfo contains information about a script without the template
type ScriptInfo struct {
	Name        string
	Description string
	Category    string
	Parameters  []Parameter
}

// GetScriptHelp returns detailed help for a specific script
func (e *Executor) GetScriptHelp(scriptName string) (*ScriptInfo, error) {
	// Try to get script from script manager first
	script, err := e.scriptManager.GetScript(scriptName)
	if err == nil {
		var params []Parameter
		for _, p := range script.Parameters {
			params = append(params, Parameter{
				Name:         p.Name,
				Type:         p.Type,
				Description:  p.Description,
				DefaultValue: fmt.Sprintf("%v", p.Default),
				Required:     p.Required,
			})
		}

		return &ScriptInfo{
			Name:        script.Name,
			Description: script.Short,
			Category:    script.Category,
			Parameters:  params,
		}, nil
	}

	// Fall back to builtin scripts
	builtinScript, exists := BuiltinScripts[scriptName]
	if !exists {
		return nil, fmt.Errorf("script '%s' not found", scriptName)
	}

	return &ScriptInfo{
		Name:        builtinScript.Name,
		Description: builtinScript.Description,
		Category:    builtinScript.Category,
		Parameters:  builtinScript.Parameters,
	}, nil
}

// ExecuteBuiltinScriptForMetrics executes a script and returns structured data for metrics conversion
func (e *Executor) ExecuteBuiltinScriptForMetrics(ctx context.Context, clusterID, scriptName string, params map[string]string) (*common.QueryResult, error) {
	// Try to get script from script manager first
	script, err := e.scriptManager.GetScript(scriptName)
	var renderedScript string
	var scriptDescription, scriptCategory string

	if err == nil {
		// Use script from file system
		e.logger.WithFields(logrus.Fields{
			"script_name": scriptName,
			"description": script.Short,
			"category":    script.Category,
			"parameters":  params,
		}).Info("Executing script for metrics collection")

		renderedScript, err = e.scriptManager.ExecuteScript(scriptName, params)
		if err != nil {
			return nil, fmt.Errorf("failed to render script: %w", err)
		}

		scriptDescription = script.Short
		scriptCategory = script.Category
	} else {
		// Fall back to builtin scripts
		builtinScript, exists := BuiltinScripts[scriptName]
		if !exists {
			return nil, fmt.Errorf("script '%s' not found", scriptName)
		}

		e.logger.WithFields(logrus.Fields{
			"script_name": scriptName,
			"description": builtinScript.Description,
			"category":    builtinScript.Category,
			"parameters":  params,
		}).Info("Executing builtin script for metrics collection")

		renderedScript, err = builtinScript.Execute(params)
		if err != nil {
			return nil, fmt.Errorf("failed to render script: %w", err)
		}

		scriptDescription = builtinScript.Description
		scriptCategory = builtinScript.Category
	}

	// Execute the script and capture results
	startTime := time.Now()
	result, err := e.client.ExecuteScriptAndExtractData(ctx, clusterID, renderedScript)
	if err != nil {
		return nil, fmt.Errorf("failed to execute script: %w", err)
	}

	// Enhance result with script metadata
	if result != nil {
		result.Query = renderedScript
		result.ExecutedAt = startTime
		result.Duration = time.Since(startTime)

		if result.Metadata == nil {
			result.Metadata = make(map[string]string)
		}
		result.Metadata["script_name"] = scriptName
		result.Metadata["script_category"] = scriptCategory
		result.Metadata["script_description"] = scriptDescription

		// Convert raw data to metrics based on script type
		result.Metrics = e.convertDataToMetrics(result, scriptName)

		e.logger.WithFields(logrus.Fields{
			"script_name":   scriptName,
			"data_rows":     len(result.Data),
			"metrics_count": len(result.Metrics),
		}).Info("Data conversion completed")
	}

	return result, nil
}

// ExecuteAllMetricScripts executes all scripts suitable for metrics collection
func (e *Executor) ExecuteAllMetricScripts(ctx context.Context, clusterID string) ([]*common.QueryResult, error) {
	var results []*common.QueryResult
	var errors []string

	// Define default parameters for automatic execution
	defaultParams := map[string]map[string]string{
		"resource_usage": {
			"start_time": "-5m",
			"namespace":  "",
		},
		"http_overview": {
			"start_time": "-5m",
			"namespace":  "",
		},
		"network_stats": {
			"start_time": "-5m",
			"namespace":  "",
		},
		"error_analysis": {
			"start_time": "-5m",
			"namespace":  "",
		},
	}

	// Get all available scripts
	scriptList := e.scriptManager.ListScripts()

	// Add builtin scripts that are not in file system
	for name := range BuiltinScripts {
		found := false
		for _, scriptName := range scriptList {
			if scriptName == name {
				found = true
				break
			}
		}
		if !found {
			scriptList = append(scriptList, name)
		}
	}

	for _, scriptName := range scriptList {
		// Skip scripts that require specific parameters
		if scriptName == "pod_overview" {
			continue
		}

		params := defaultParams[scriptName]
		if params == nil {
			// Use default parameters for unknown scripts
			params = map[string]string{
				"start_time": "-5m",
				"namespace":  "",
			}
		}

		result, err := e.ExecuteBuiltinScriptForMetrics(ctx, clusterID, scriptName, params)
		if err != nil {
			e.logger.WithError(err).WithField("script_name", scriptName).Warn("Failed to execute script for metrics")
			errors = append(errors, fmt.Sprintf("%s: %v", scriptName, err))
			continue
		}

		if result != nil {
			results = append(results, result)
		}
	}

	// Log any errors but don't fail completely
	if len(errors) > 0 {
		e.logger.WithField("errors", strings.Join(errors, "; ")).Warn("Some scripts failed during metrics collection")
	}

	return results, nil
}

// convertDataToMetrics converts raw query data to Prometheus metrics based on script type
func (e *Executor) convertDataToMetrics(result *common.QueryResult, scriptName string) []common.Metric {
	var metrics []common.Metric
	now := time.Now()

	// Convert based on script name and data structure
	switch scriptName {
	case "http_overview":
		metrics = e.convertHTTPOverviewMetrics(result.Data, now)
	case "resource_usage":
		metrics = e.convertResourceUsageMetrics(result.Data, now)
	case "network_stats":
		metrics = e.convertNetworkStatsMetrics(result.Data, now)
	case "error_analysis":
		metrics = e.convertErrorAnalysisMetrics(result.Data, now)
	default:
		// Generic conversion for unknown scripts
		metrics = e.convertGenericMetrics(result.Data, now, scriptName)
	}

	return metrics
}

// convertHTTPOverviewMetrics converts HTTP overview data to metrics
func (e *Executor) convertHTTPOverviewMetrics(data []map[string]interface{}, timestamp time.Time) []common.Metric {
	var metrics []common.Metric

	for _, row := range data {
		labels := make(map[string]string)

		// Extract common labels
		if namespace, ok := row["namespace"].(string); ok {
			labels["namespace"] = namespace
		}
		if service, ok := row["service"].(string); ok {
			labels["service"] = service
		}

		// Request count metric (Counter) - total累积值
		if requestCount, ok := e.convertToFloat64(row["request_count"]); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_http_requests_total",
				Type:        common.MetricTypeCounter,
				Description: "Total number of HTTP requests",
				Labels:      copyLabels(labels),
				Value:       requestCount,
				Timestamp:   timestamp,
			})
		}

		// Error count metric (Counter) - total累积值
		if errorCount, ok := e.convertToFloat64(row["error_count"]); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_http_errors_total",
				Type:        common.MetricTypeCounter,
				Description: "Total number of HTTP errors",
				Labels:      copyLabels(labels),
				Value:       errorCount,
				Timestamp:   timestamp,
			})
		}

		// Request duration histogram (Histogram) - 将平均延迟转换为histogram桶
		if avgLatency, ok := e.convertToFloat64(row["avg_latency_ms"]); ok {
			// 创建延迟histogram的观察值（将毫秒转换为秒）
			latencySeconds := avgLatency / 1000.0

			// 为histogram生成基本的桶分布
			// 基于平均值估算分布（简化处理）
			requestCount, _ := e.convertToFloat64(row["request_count"])

			// 生成histogram桶（延迟范围：0.001s到10s）
			buckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
			for _, bucket := range buckets {
				bucketLabels := copyLabels(labels)
				bucketLabels["le"] = fmt.Sprintf("%.3f", bucket)

				// 简单估算：如果平均延迟小于桶值，则所有请求都在此桶内
				bucketCount := float64(0)
				if latencySeconds <= bucket {
					bucketCount = requestCount
				}

				metrics = append(metrics, common.Metric{
					Name:        "pixie_http_request_duration_seconds_bucket",
					Type:        common.MetricTypeCounter,
					Description: "HTTP request duration histogram buckets",
					Unit:        "seconds",
					Labels:      bucketLabels,
					Value:       bucketCount,
					Timestamp:   timestamp,
				})
			}

			// +Inf桶（包含所有请求）
			infLabels := copyLabels(labels)
			infLabels["le"] = "+Inf"
			metrics = append(metrics, common.Metric{
				Name:        "pixie_http_request_duration_seconds_bucket",
				Type:        common.MetricTypeCounter,
				Description: "HTTP request duration histogram buckets",
				Unit:        "seconds",
				Labels:      infLabels,
				Value:       requestCount,
				Timestamp:   timestamp,
			})

			// histogram总计数
			sumLabels := copyLabels(labels)
			totalDuration := latencySeconds * requestCount
			metrics = append(metrics, common.Metric{
				Name:        "pixie_http_request_duration_seconds_sum",
				Type:        common.MetricTypeCounter,
				Description: "Total sum of HTTP request durations",
				Unit:        "seconds",
				Labels:      sumLabels,
				Value:       totalDuration,
				Timestamp:   timestamp,
			})

			// histogram计数
			countLabels := copyLabels(labels)
			metrics = append(metrics, common.Metric{
				Name:        "pixie_http_request_duration_seconds_count",
				Type:        common.MetricTypeCounter,
				Description: "Total count of HTTP requests for duration histogram",
				Labels:      countLabels,
				Value:       requestCount,
				Timestamp:   timestamp,
			})
		}
	}

	return metrics
} // convertResourceUsageMetrics converts resource usage data to metrics
func (e *Executor) convertResourceUsageMetrics(data []map[string]interface{}, timestamp time.Time) []common.Metric {
	var metrics []common.Metric

	for _, row := range data {
		labels := make(map[string]string)

		// Extract common labels
		if namespace, ok := row["namespace"].(string); ok {
			labels["namespace"] = namespace
		}
		if pod, ok := row["pod"].(string); ok {
			labels["pod"] = pod
		}

		// CPU usage metric
		if cpuUsage, ok := e.convertToFloat64(row["cpu_usage"]); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_pod_cpu_usage",
				Type:        common.MetricTypeGauge,
				Description: "Pod CPU usage",
				Labels:      copyLabels(labels),
				Value:       cpuUsage,
				Timestamp:   timestamp,
			})
		}

		// Memory usage metric
		if memUsage, ok := e.convertToFloat64(row["memory_usage"]); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_pod_memory_usage_bytes",
				Type:        common.MetricTypeGauge,
				Description: "Pod memory usage in bytes",
				Unit:        "bytes",
				Labels:      copyLabels(labels),
				Value:       memUsage,
				Timestamp:   timestamp,
			})
		}
	}

	return metrics
}

// convertNetworkStatsMetrics converts network stats data to metrics
func (e *Executor) convertNetworkStatsMetrics(data []map[string]interface{}, timestamp time.Time) []common.Metric {
	var metrics []common.Metric

	for _, row := range data {
		labels := make(map[string]string)

		// Extract common labels
		if namespace, ok := row["namespace"].(string); ok {
			labels["namespace"] = namespace
		}
		if pod, ok := row["pod"].(string); ok {
			labels["pod"] = pod
		}

		// Bytes received metric
		if bytesRx, ok := e.convertToFloat64(row["bytes_rx"]); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_network_bytes_received_total",
				Type:        common.MetricTypeCounter,
				Description: "Total network bytes received",
				Unit:        "bytes",
				Labels:      copyLabels(labels),
				Value:       bytesRx,
				Timestamp:   timestamp,
			})
		}

		// Bytes transmitted metric
		if bytesTx, ok := e.convertToFloat64(row["bytes_tx"]); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_network_bytes_transmitted_total",
				Type:        common.MetricTypeCounter,
				Description: "Total network bytes transmitted",
				Unit:        "bytes",
				Labels:      copyLabels(labels),
				Value:       bytesTx,
				Timestamp:   timestamp,
			})
		}
	}

	return metrics
}

// convertErrorAnalysisMetrics converts error analysis data to metrics
func (e *Executor) convertErrorAnalysisMetrics(data []map[string]interface{}, timestamp time.Time) []common.Metric {
	var metrics []common.Metric

	for _, row := range data {
		labels := make(map[string]string)

		// Extract common labels
		if namespace, ok := row["namespace"].(string); ok {
			labels["namespace"] = namespace
		}
		if service, ok := row["service"].(string); ok {
			labels["service"] = service
		}
		if errorCode, ok := row["error_code"].(string); ok {
			labels["error_code"] = errorCode
		}

		// Error count metric
		if errorCount, ok := e.convertToFloat64(row["error_count"]); ok {
			metrics = append(metrics, common.Metric{
				Name:        "pixie_service_errors_total",
				Type:        common.MetricTypeCounter,
				Description: "Total number of service errors",
				Labels:      copyLabels(labels),
				Value:       errorCount,
				Timestamp:   timestamp,
			})
		}
	}

	return metrics
}

// convertGenericMetrics provides a generic conversion for unknown script types
func (e *Executor) convertGenericMetrics(data []map[string]interface{}, timestamp time.Time, scriptName string) []common.Metric {
	var metrics []common.Metric

	for i, row := range data {
		labels := make(map[string]string)
		labels["script"] = scriptName
		labels["row_index"] = fmt.Sprintf("%d", i)

		// Try to extract meaningful metrics from any numeric fields
		for key, value := range row {
			if numValue, ok := e.convertToFloat64(value); ok {
				// Create metric for each numeric field
				metricName := fmt.Sprintf("pixie_%s_%s", scriptName, strings.ToLower(key))

				// Add non-numeric fields as labels
				metricLabels := copyLabels(labels)
				for labelKey, labelValue := range row {
					if labelKey != key {
						if strValue, ok := labelValue.(string); ok {
							metricLabels[labelKey] = strValue
						}
					}
				}

				metrics = append(metrics, common.Metric{
					Name:        metricName,
					Type:        common.MetricTypeGauge,
					Description: fmt.Sprintf("Generic metric from %s script: %s", scriptName, key),
					Labels:      metricLabels,
					Value:       numValue,
					Timestamp:   timestamp,
				})
			}
		}
	}

	return metrics
}

// convertToFloat64 converts various types to float64
func (e *Executor) convertToFloat64(value interface{}) (float64, bool) {
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
		if f, err := fmt.Sscanf(v, "%f", new(float64)); err == nil && f == 1 {
			var result float64
			fmt.Sscanf(v, "%f", &result)
			return result, true
		}
	}
	return 0, false
}

// copyLabels creates a copy of the labels map
func copyLabels(labels map[string]string) map[string]string {
	copy := make(map[string]string)
	for k, v := range labels {
		copy[k] = v
	}
	return copy
}
