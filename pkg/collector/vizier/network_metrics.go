package vizier

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/sirupsen/logrus"

	"github.com/wrongerror/o11y-hub/pkg/expire"
	"github.com/wrongerror/o11y-hub/pkg/k8s"
	"github.com/wrongerror/o11y-hub/pkg/metrics"
)

// NetworkMetrics manages Prometheus counter metrics for network flow data
type NetworkMetrics struct {
	cachedClock *expire.CachedClock
	logger      *logrus.Logger
	k8sManager  *k8s.Manager

	// Counter metrics with expiring labels using generic Expirer
	transmitBytes *metrics.Expirer[prometheus.Counter]
	receiveBytes  *metrics.Expirer[prometheus.Counter]
}

// NetworkFlowMetricLabels represents the label set for network flow metrics
type NetworkFlowMetricLabels struct {
	SrcNamespace   string
	SrcAddress     string
	SrcNodeName    string
	SrcOwnerName   string
	SrcOwnerType   string
	SrcPodName     string
	SrcServiceName string
	SrcType        string
	DstNamespace   string
	DstAddress     string
	DstNodeName    string
	DstOwnerName   string
	DstOwnerType   string
	DstPodName     string
	DstServiceName string
	DstType        string
	TraceRole      string
}

// ToSlice converts NetworkFlowMetricLabels to a string slice for use with expiry map
func (l NetworkFlowMetricLabels) ToSlice() []string {
	return []string{
		l.SrcNamespace,
		l.SrcAddress,
		l.SrcNodeName,
		l.SrcOwnerName,
		l.SrcOwnerType,
		l.SrcPodName,
		l.SrcServiceName,
		l.SrcType,
		l.DstNamespace,
		l.DstAddress,
		l.DstNodeName,
		l.DstOwnerName,
		l.DstOwnerType,
		l.DstPodName,
		l.DstServiceName,
		l.DstType,
		l.TraceRole,
	}
}

// NewNetworkMetrics creates a new NetworkMetrics instance
func NewNetworkMetrics(logger *logrus.Logger) *NetworkMetrics {
	clock := expire.NewCachedClock(time.Now)
	clockFunc := clock.ClockFunc()
	ttl := 5 * time.Minute

	labelNames := []string{
		"src_namespace", "src_address", "src_node_name", "src_owner_name", "src_owner_type",
		"src_pod_name", "src_service_name", "src_type",
		"dst_namespace", "dst_address", "dst_node_name", "dst_owner_name", "dst_owner_type",
		"dst_pod_name", "dst_service_name", "dst_type", "trace_role",
	}

	m := &NetworkMetrics{
		cachedClock: clock,
		logger:      logger,

		transmitBytes: metrics.NewExpirer[prometheus.Counter](
			prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "network_flow_transmit_bytes_total",
				Help: "Total bytes transmitted to remote entity",
			}, labelNames),
			clockFunc, ttl,
		),
		receiveBytes: metrics.NewExpirer[prometheus.Counter](
			prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "network_flow_receive_bytes_total",
				Help: "Total bytes received from remote entity",
			}, labelNames),
			clockFunc, ttl,
		),
	}

	return m
}

// SetK8sManager sets the Kubernetes manager for endpoint resolution
func (m *NetworkMetrics) SetK8sManager(k8sManager *k8s.Manager) {
	m.k8sManager = k8sManager
}

// UpdateClock updates the cached clock used by all counters
func (m *NetworkMetrics) UpdateClock() {
	m.cachedClock.Update()
}

// ProcessNetworkEvent processes a single network flow event and updates metrics
func (m *NetworkMetrics) ProcessNetworkEvent(event map[string]interface{}) error {
	// Extract basic flow information
	srcAddress := getStringValueFromEvent(event, "src_address", "")
	dstAddress := getStringValueFromEvent(event, "dst_address", "")

	// Get simple bytes sent/received from the simplified PxL script
	bytesSent := getFloat64ValueFromEvent(event, "bytes_sent", 0)
	bytesRecv := getFloat64ValueFromEvent(event, "bytes_recv", 0)

	// Skip events with no data transfer
	if bytesSent <= 0 && bytesRecv <= 0 {
		return nil
	}

	// Extract metadata
	srcNamespace := getStringValueFromEvent(event, "src_namespace", "")
	srcPodNameRaw := getStringValueFromEvent(event, "src_pod_name", "")
	srcServiceName := getStringValueFromEvent(event, "src_service_name", "")
	srcNodeName := getStringValueFromEvent(event, "src_node_name", "")
	srcType := getStringValueFromEvent(event, "src_type", "ip")
	srcOwnerName := getStringValueFromEvent(event, "src_owner_name", "")
	srcOwnerType := getStringValueFromEvent(event, "src_owner_type", "")

	dstNamespace := getStringValueFromEvent(event, "dst_namespace", "")
	dstPodNameRaw := getStringValueFromEvent(event, "dst_pod_name", "")
	dstServiceName := getStringValueFromEvent(event, "dst_service_name", "")
	dstNodeName := getStringValueFromEvent(event, "dst_node_name", "")
	dstType := getStringValueFromEvent(event, "dst_type", "ip")
	dstOwnerName := getStringValueFromEvent(event, "dst_owner_name", "")
	dstOwnerType := getStringValueFromEvent(event, "dst_owner_type", "")

	// Determine trace role from original trace_role field
	traceRoleRaw := getStringValueFromEvent(event, "trace_role", "1")
	var traceRole string
	if traceRoleRaw == "2" {
		traceRole = "server"
	} else {
		traceRole = "client"
	}

	// Parse pod names (handle namespace/pod format)
	var srcPodName string
	if srcPodNameRaw != "" && strings.Contains(srcPodNameRaw, "/") {
		parts := strings.SplitN(srcPodNameRaw, "/", 2)
		if len(parts) == 2 {
			if srcNamespace == "" {
				srcNamespace = parts[0]
			}
			srcPodName = parts[1]
		} else {
			srcPodName = srcPodNameRaw
		}
	} else {
		srcPodName = srcPodNameRaw
	}

	var dstPodName string
	if dstPodNameRaw != "" && strings.Contains(dstPodNameRaw, "/") {
		parts := strings.SplitN(dstPodNameRaw, "/", 2)
		if len(parts) == 2 {
			if dstNamespace == "" {
				dstNamespace = parts[0]
			}
			dstPodName = parts[1]
		} else {
			dstPodName = dstPodNameRaw
		}
	} else {
		dstPodName = dstPodNameRaw
	}

	// Get additional owner information using K8s manager if available
	if m.k8sManager != nil {
		if srcOwnerName == "" && srcOwnerType == "" && srcNamespace != "" && srcPodName != "" {
			srcOwnerName, srcOwnerType = m.k8sManager.GetPodOwnerInfo(srcNamespace, srcPodName)
		}
		if dstOwnerName == "" && dstOwnerType == "" && dstNamespace != "" && dstPodName != "" {
			dstOwnerName, dstOwnerType = m.k8sManager.GetPodOwnerInfo(dstNamespace, dstPodName)
		}
	}

	// Ensure we have some value for srcAddress
	if srcAddress == "" {
		srcAddress = "unknown"
	}

	// Create labels
	labels := NetworkFlowMetricLabels{
		SrcNamespace:   srcNamespace,
		SrcAddress:     srcAddress,
		SrcNodeName:    srcNodeName,
		SrcOwnerName:   srcOwnerName,
		SrcOwnerType:   srcOwnerType,
		SrcPodName:     srcPodName,
		SrcServiceName: srcServiceName,
		SrcType:        srcType,
		DstNamespace:   dstNamespace,
		DstAddress:     dstAddress,
		DstNodeName:    dstNodeName,
		DstOwnerName:   dstOwnerName,
		DstOwnerType:   dstOwnerType,
		DstPodName:     dstPodName,
		DstServiceName: dstServiceName,
		DstType:        dstType,
		TraceRole:      traceRole,
	}

	// Convert labels to slice for metrics.Expirer
	labelSlice := labels.ToSlice()

	// Get or create counter metrics and calculate increments
	if bytesSent > 0 {
		counterEntry := m.transmitBytes.WithLabelValues(labelSlice...)
		increment := m.calculateIncrement(counterEntry.Metric, bytesSent)
		if increment > 0 {
			counterEntry.Metric.Add(increment)
		}
	}

	if bytesRecv > 0 {
		counterEntry := m.receiveBytes.WithLabelValues(labelSlice...)
		increment := m.calculateIncrement(counterEntry.Metric, bytesRecv)
		if increment > 0 {
			counterEntry.Metric.Add(increment)
		}
	}

	return nil
}

// calculateIncrement calculates the increment needed to update counter from its current value to target value
func (m *NetworkMetrics) calculateIncrement(counter prometheus.Counter, targetValue float64) float64 {
	// Get current counter value
	metric := &dto.Metric{}
	if err := counter.Write(metric); err != nil {
		m.logger.WithError(err).Debug("Failed to read counter value, using target value as increment")
		return targetValue
	}

	currentValue := metric.GetCounter().GetValue()
	increment := targetValue - currentValue

	// Handle counter resets (if target < current, assume counter was reset)
	if increment < 0 {
		m.logger.WithFields(logrus.Fields{
			"current": currentValue,
			"target":  targetValue,
		}).Debug("Counter reset detected, using target value as increment")
		return targetValue
	}

	return increment
}

// Describe implements prometheus.Collector
func (m *NetworkMetrics) Describe(ch chan<- *prometheus.Desc) {
	m.transmitBytes.Describe(ch)
	m.receiveBytes.Describe(ch)
}

// Collect implements prometheus.Collector
func (m *NetworkMetrics) Collect(ch chan<- prometheus.Metric) {
	m.UpdateClock()
	m.transmitBytes.Collect(ch)
	m.receiveBytes.Collect(ch)
}

// getFloat64ValueFromEvent extracts a float64 value from an event map
func getFloat64ValueFromEvent(event map[string]interface{}, key string, defaultValue float64) float64 {
	if value, ok := event[key]; ok {
		switch v := value.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int64:
			return float64(v)
		case string:
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
		}
	}
	return defaultValue
}

// Helper functions with unique names to avoid conflicts
func parseFloat64FromEvent(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		// Try to parse string as number
		if val, err := strconv.ParseFloat(v, 64); err == nil {
			return val, nil
		}
		return 0, fmt.Errorf("cannot convert string %q to float64", v)
	default:
		return 0, fmt.Errorf("unsupported type %T for float64 conversion", value)
	}
}

func getStringValueFromEvent(event map[string]interface{}, key, defaultValue string) string {
	if value, exists := event[key]; exists {
		switch v := value.(type) {
		case string:
			return v
		case []interface{}:
			// Handle array of strings (e.g., service names)
			var parts []string
			for _, item := range v {
				if str, ok := item.(string); ok {
					parts = append(parts, str)
				}
			}
			if len(parts) > 0 {
				return fmt.Sprintf("[%s]", strings.Join(parts, ","))
			}
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	return defaultValue
}
