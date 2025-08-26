package vizier

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"github.com/wrongerror/o11y-hub/pkg/k8s"
)

// NetworkMetrics manages Prometheus counter metrics for network flow data
type NetworkMetrics struct {
	logger     *logrus.Logger
	k8sManager *k8s.Manager

	// Counter metrics (simple prometheus counters without expiration for now)
	transmitBytes prometheus.CounterVec
	receiveBytes  prometheus.CounterVec

	// Connection state tracking (instead of event deduplication)
	connectionStates   map[string]ConnectionState // Store connection key -> state
	connectionStatesMu sync.RWMutex
	cleanupTicker      *time.Ticker
	stopCleanup        chan struct{}
	stateTTL           time.Duration // TTL for connection states
}

// ConnectionState tracks the state of a network connection
type ConnectionState struct {
	LastTransmitBytes uint64
	LastReceiveBytes  uint64
	LastSeen          time.Time
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

// NewNetworkMetrics creates a new NetworkMetrics instance
func NewNetworkMetrics(logger *logrus.Logger) *NetworkMetrics {
	labelNames := []string{
		"src_namespace", "src_address", "src_node_name", "src_owner_name", "src_owner_type",
		"src_pod_name", "src_service_name", "src_type",
		"dst_namespace", "dst_address", "dst_node_name", "dst_owner_name", "dst_owner_type",
		"dst_pod_name", "dst_service_name", "dst_type", "trace_role",
	}

	m := &NetworkMetrics{
		logger: logger,

		transmitBytes: *prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "network_flow_transmit_bytes_total",
			Help: "Total bytes transmitted to remote entity",
		}, labelNames),
		receiveBytes: *prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "network_flow_receive_bytes_total",
			Help: "Total bytes received from remote entity",
		}, labelNames),

		connectionStates: make(map[string]ConnectionState),
		stopCleanup:      make(chan struct{}),
		stateTTL:         10 * time.Minute, // TTL for connection states
	}

	// Start background cleanup goroutine
	m.cleanupTicker = time.NewTicker(2 * time.Minute) // Clean every 2 minutes
	go m.backgroundCleanup()

	return m
}

// SetK8sManager sets the Kubernetes manager for endpoint resolution
func (m *NetworkMetrics) SetK8sManager(k8sManager *k8s.Manager) {
	m.k8sManager = k8sManager
}

// ProcessNetworkEvent processes a single network event and updates metrics
func (m *NetworkMetrics) ProcessNetworkEvent(event map[string]interface{}) error {
	// Create unique connection ID (not event ID) based on connection characteristics
	connectionID := m.createConnectionID(event)

	now := time.Now()

	// Extract byte counters from current event
	bytesSent, _ := parseFloat64FromEvent(event["bytes_sent"])
	bytesRecv, _ := parseFloat64FromEvent(event["bytes_recv"])
	currentTransmitBytes := uint64(bytesSent)
	currentReceiveBytes := uint64(bytesRecv)

	// Get or create connection state
	m.connectionStatesMu.Lock()
	connectionState, exists := m.connectionStates[connectionID]

	// Calculate increments
	transmitIncrement := uint64(0)
	receiveIncrement := uint64(0)

	if exists {
		// For existing connections, calculate the increment
		if currentTransmitBytes > connectionState.LastTransmitBytes {
			transmitIncrement = currentTransmitBytes - connectionState.LastTransmitBytes
		}
		if currentReceiveBytes > connectionState.LastReceiveBytes {
			receiveIncrement = currentReceiveBytes - connectionState.LastReceiveBytes
		}
	} else {
		// For new connections, use the full current values
		transmitIncrement = currentTransmitBytes
		receiveIncrement = currentReceiveBytes
	}

	// Update connection state
	m.connectionStates[connectionID] = ConnectionState{
		LastTransmitBytes: currentTransmitBytes,
		LastReceiveBytes:  currentReceiveBytes,
		LastSeen:          now,
	}
	m.connectionStatesMu.Unlock()

	// Only update metrics if there's actual increment (avoid zero-value updates)
	if transmitIncrement == 0 && receiveIncrement == 0 {
		return nil
	}

	// Extract trace role to determine client vs server perspective
	traceRoleRaw := event["trace_role"]
	traceRole := ""
	if traceRoleVal, err := parseFloat64FromEvent(traceRoleRaw); err == nil {
		// trace_role: 1 = client, 2 = server in Pixie
		if traceRoleVal == 1 {
			traceRole = "client"
		} else if traceRoleVal == 2 {
			traceRole = "server"
		}
	}
	if traceRole == "" {
		return fmt.Errorf("invalid or missing trace_role")
	}

	// Extract local endpoint metadata
	srcNamespace := getStringValueFromEvent(event, "src_namespace", "")
	srcPodNameRaw := getStringValueFromEvent(event, "src_pod_name", "")
	srcServiceName := getStringValueFromEvent(event, "src_service_name", "")
	srcNodeName := getStringValueFromEvent(event, "src_node_name", "")
	srcAddress := getStringValueFromEvent(event, "src_address", "")
	srcType := getStringValueFromEvent(event, "src_type", "")
	srcOwnerName := getStringValueFromEvent(event, "src_owner_name", "")
	srcOwnerType := getStringValueFromEvent(event, "src_owner_type", "")

	// Parse src pod name which comes as "namespace/pod-name" format from Pixie
	var srcPodName string
	if srcPodNameRaw != "" && strings.Contains(srcPodNameRaw, "/") {
		parts := strings.SplitN(srcPodNameRaw, "/", 2)
		if len(parts) == 2 {
			// Use namespace from pod_name if local namespace is empty
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

	// Extract destination endpoint metadata (usually remote)
	dstNamespace := getStringValueFromEvent(event, "dst_namespace", "")
	dstPodNameRaw := getStringValueFromEvent(event, "dst_pod_name", "")
	dstServiceName := getStringValueFromEvent(event, "dst_service_name", "")
	dstNodeName := getStringValueFromEvent(event, "dst_node_name", "")
	dstAddress := getStringValueFromEvent(event, "dst_address", "")
	dstType := getStringValueFromEvent(event, "dst_type", "")
	dstOwnerName := getStringValueFromEvent(event, "dst_owner_name", "")
	dstOwnerType := getStringValueFromEvent(event, "dst_owner_type", "")

	// Parse dst pod name
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

	// Update counter metrics with incremental values
	if transmitIncrement > 0 {
		counter := m.transmitBytes.With(labels.ToPrometheusLabels())
		counter.Add(float64(transmitIncrement))
	}

	if receiveIncrement > 0 {
		counter := m.receiveBytes.With(labels.ToPrometheusLabels())
		counter.Add(float64(receiveIncrement))
	}

	return nil
}

// createConnectionID creates a unique identifier for a network connection
// This is different from event ID - it identifies the connection, not the event
func (m *NetworkMetrics) createConnectionID(event map[string]interface{}) string {
	// Use combination of upid, addresses, and ports to identify unique connections
	traceRole := getStringValueFromEvent(event, "trace_role", "")
	upid := getStringValueFromEvent(event, "upid", "")
	srcAddr := getStringValueFromEvent(event, "src_address", "")
	dstAddr := getStringValueFromEvent(event, "dst_address", "")
	remotePort := getStringValueFromEvent(event, "remote_port", "")
	protocol := getStringValueFromEvent(event, "protocol", "")

	return fmt.Sprintf("%s:%s:%s:%s:%s:%s", traceRole, upid, srcAddr, dstAddr, remotePort, protocol)
}

// backgroundCleanup runs background cleanup for expired connection states
func (m *NetworkMetrics) backgroundCleanup() {
	for {
		select {
		case <-m.cleanupTicker.C:
			m.cleanupExpiredStates()
		case <-m.stopCleanup:
			return
		}
	}
}

// cleanupExpiredStates removes expired connection states
func (m *NetworkMetrics) cleanupExpiredStates() {
	now := time.Now()

	m.connectionStatesMu.Lock()
	defer m.connectionStatesMu.Unlock()

	// Clean up expired connection states
	for connectionID, state := range m.connectionStates {
		if now.Sub(state.LastSeen) > m.stateTTL {
			delete(m.connectionStates, connectionID)
		}
	}
}

// Collect collects all metrics and sends them to the channel
func (m *NetworkMetrics) Collect(ch chan<- prometheus.Metric) {
	m.transmitBytes.Collect(ch)
	m.receiveBytes.Collect(ch)
}

// Cleanup stops background tasks and cleans up resources
func (m *NetworkMetrics) Cleanup() {
	if m.cleanupTicker != nil {
		m.cleanupTicker.Stop()
	}

	close(m.stopCleanup)

	m.connectionStatesMu.Lock()
	defer m.connectionStatesMu.Unlock()

	// Clear all connection states
	for connectionID := range m.connectionStates {
		delete(m.connectionStates, connectionID)
	}
}

// ToPrometheusLabels converts NetworkFlowMetricLabels to prometheus.Labels
func (l *NetworkFlowMetricLabels) ToPrometheusLabels() prometheus.Labels {
	return prometheus.Labels{
		"src_namespace":    l.SrcNamespace,
		"src_address":      l.SrcAddress,
		"src_node_name":    l.SrcNodeName,
		"src_owner_name":   l.SrcOwnerName,
		"src_owner_type":   l.SrcOwnerType,
		"src_pod_name":     l.SrcPodName,
		"src_service_name": l.SrcServiceName,
		"src_type":         l.SrcType,
		"dst_namespace":    l.DstNamespace,
		"dst_address":      l.DstAddress,
		"dst_node_name":    l.DstNodeName,
		"dst_owner_name":   l.DstOwnerName,
		"dst_owner_type":   l.DstOwnerType,
		"dst_pod_name":     l.DstPodName,
		"dst_service_name": l.DstServiceName,
		"dst_type":         l.DstType,
		"trace_role":       l.TraceRole,
	}
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
