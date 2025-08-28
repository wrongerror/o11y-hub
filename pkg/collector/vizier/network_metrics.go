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

	// Get nslookup result for fallback resolution
	dstNslookup := getStringValueFromEvent(event, "dst_nslookup", "")

	// Log original PxL data for debugging key IPs
	if dstAddress == "10.233.0.1" || dstAddress == "172.31.17.20" {
		m.logger.WithFields(logrus.Fields{
			"dst_address":       dstAddress,
			"pxl_dst_type":      dstType,
			"pxl_dst_service":   dstServiceName,
			"pxl_dst_pod":       dstPodNameRaw,
			"pxl_dst_node":      dstNodeName,
			"pxl_dst_namespace": dstNamespace,
			"dst_nslookup":      dstNslookup,
		}).Debug("Original PxL data for key IP")
	}

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

	// Use nslookup result for DNS-based service resolution
	// Only for IPs that haven't been resolved by PxL and aren't node IPs
	originalDstType := dstType
	if dstType == "ip" && dstNslookup != "" && dstNslookup != dstAddress {
		m.logger.WithFields(logrus.Fields{
			"dst_address":   dstAddress,
			"dst_nslookup":  dstNslookup,
			"original_type": originalDstType,
		}).Debug("Processing DNS fallback for IP")

		// Parse DNS name to determine if it's K8s internal or external
		parsedNamespace, parsedServiceName := parseKubernetesDNS(dstNslookup)
		if parsedNamespace != "" && parsedServiceName != "" {
			// This is a K8s cluster internal DNS name
			// Use K8s manager to get complete metadata for this service endpoint
			if m.k8sManager != nil {
				// First, try to find the pod using the service information and IP address
				if podInfo := m.k8sManager.GetPodByServiceAndIP(parsedNamespace, parsedServiceName, dstAddress); podInfo != nil {
					// Found the specific pod for this service endpoint
					dstNamespace = podInfo.Namespace
					dstPodName = fmt.Sprintf("%s/%s", podInfo.Namespace, podInfo.Name)
					dstNodeName = podInfo.NodeName
					dstServiceName = fmt.Sprintf("%s/%s", parsedNamespace, parsedServiceName)
					dstType = "service"

					m.logger.WithFields(logrus.Fields{
						"dst_address":   dstAddress,
						"pod_name":      podInfo.Name,
						"pod_namespace": podInfo.Namespace,
						"node_name":     podInfo.NodeName,
						"service_name":  dstServiceName,
					}).Debug("Found pod for K8s service endpoint via DNS and IP")
				} else {
					// Fallback: check if this is a node IP that happens to have a service DNS
					if endpointInfo := m.k8sManager.GetEndpointInfo(dstAddress); endpointInfo != nil && endpointInfo.Type == k8s.EndpointTypeNode {
						dstNodeName = endpointInfo.NodeName
						dstType = "node"
						dstServiceName = fmt.Sprintf("%s/%s", parsedNamespace, parsedServiceName)
						m.logger.WithFields(logrus.Fields{
							"dst_address":      dstAddress,
							"node_name":        endpointInfo.NodeName,
							"service_from_dns": dstServiceName,
						}).Debug("Node IP with K8s DNS - classified as node, service name preserved")
					} else {
						// Default service classification
						if dstNamespace == "" {
							dstNamespace = parsedNamespace
						}
						if dstServiceName == "" {
							dstServiceName = fmt.Sprintf("%s/%s", parsedNamespace, parsedServiceName)
						}
						dstType = "service"
						m.logger.WithFields(logrus.Fields{
							"dst_address":      dstAddress,
							"parsed_namespace": parsedNamespace,
							"parsed_service":   parsedServiceName,
							"new_type":         dstType,
						}).Debug("K8s DNS parsed for IP - classified as service (no specific pod found)")
					}
				}
			} else {
				// No k8s manager, default behavior
				if dstNamespace == "" {
					dstNamespace = parsedNamespace
				}
				if dstServiceName == "" {
					dstServiceName = fmt.Sprintf("%s/%s", parsedNamespace, parsedServiceName)
				}
				dstType = "service"
			}
		} else {
			// This is an external DNS name or simple hostname
			if dstServiceName == "" {
				dstServiceName = dstNslookup
			}
			// Keep as "ip" type for external addresses
			m.logger.WithFields(logrus.Fields{
				"dst_address":  dstAddress,
				"external_dns": dstNslookup,
			}).Debug("External DNS resolved for IP - keeping as ip type")
		}
	}

	// Log final classification for key IPs
	if dstAddress == "10.233.0.1" || dstAddress == "172.31.17.20" {
		m.logger.WithFields(logrus.Fields{
			"dst_address":         dstAddress,
			"final_dst_type":      dstType,
			"final_dst_service":   dstServiceName,
			"final_dst_node":      dstNodeName,
			"final_dst_namespace": dstNamespace,
		}).Debug("Final classification for key IP")
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

// parseKubernetesDNS parses Kubernetes DNS names to extract namespace and service name
// Examples:
// - "kubernetes.default.svc.cluster.local" -> ("default", "kubernetes")
// - "ingester-whizard-0.ingester-whizard-operated.kubesphere-monitoring-system.svc.cluster.local" -> ("kubesphere-monitoring-system", "ingester-whizard-operated")
// - "172-31-17-20.calico-exporter-bgp.kubesphere-monitoring-system.svc.cluster.local" -> ("kubesphere-monitoring-system", "calico-exporter-bgp")
// - "localhost" -> ("", "")
func parseKubernetesDNS(dnsName string) (namespace, serviceName string) {
	// Check if this is a Kubernetes cluster DNS name (ends with .svc.cluster.local)
	if !strings.HasSuffix(dnsName, ".svc.cluster.local") {
		return "", ""
	}

	// Remove the .svc.cluster.local suffix
	name := strings.TrimSuffix(dnsName, ".svc.cluster.local")

	// Split by dots: [pod/service-name].[service-name].[namespace]
	parts := strings.Split(name, ".")
	if len(parts) < 2 {
		return "", ""
	}

	// The namespace is always the second-to-last part
	// The service name is always the last part
	namespace = parts[len(parts)-1]
	serviceName = parts[len(parts)-2]

	return namespace, serviceName
}
