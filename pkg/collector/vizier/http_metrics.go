package vizier

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"github.com/wrongerror/observo-connector/pkg/expire"
)

// HTTPMetrics manages Prometheus histogram metrics for HTTP requests
type HTTPMetrics struct {
	cachedClock *expire.CachedClock
	logger      *logrus.Logger

	// Histogram metrics with expiring labels
	clientRequestDuration  *ExpiringHistogram
	clientRequestBodySize  *ExpiringHistogram
	clientResponseBodySize *ExpiringHistogram
	serverRequestDuration  *ExpiringHistogram
	serverRequestBodySize  *ExpiringHistogram
	serverResponseBodySize *ExpiringHistogram

	// Last timestamp processed to handle duplicates
	processedEvents   map[string]bool
	processedEventsMu sync.RWMutex
}

// HTTPMetricLabels represents the label set for HTTP metrics
type HTTPMetricLabels struct {
	ClientNamespace   string
	ClientPodName     string
	ClientServiceName string
	ClientOwnerName   string
	ClientOwnerType   string
	ServerNamespace   string
	ServerPodName     string
	ServerServiceName string
	ServerOwnerName   string
	ServerOwnerType   string
	ReqMethod         string
	ReqPath           string
	RespStatusCode    string
}

// ToSlice converts HTTPMetricLabels to a string slice for use with expiry map
func (l HTTPMetricLabels) ToSlice() []string {
	return []string{
		l.ClientNamespace,
		l.ClientPodName,
		l.ClientServiceName,
		l.ClientOwnerName,
		l.ClientOwnerType,
		l.ServerNamespace,
		l.ServerPodName,
		l.ServerServiceName,
		l.ServerOwnerName,
		l.ServerOwnerType,
		l.ReqMethod,
		l.ReqPath,
		l.RespStatusCode,
	}
}

// HistogramEntry represents a single histogram instance with its label values
type HistogramEntry struct {
	observer    prometheus.Observer
	labelValues []string
}

// ExpiringHistogram wraps a Prometheus histogram with an expiry map for labels
type ExpiringHistogram struct {
	wrapped   *prometheus.HistogramVec
	expiryMap *expire.ExpiryMap[*HistogramEntry]
	logger    *logrus.Logger
}

// NewExpiringHistogram creates a new expiring histogram following beyla's pattern
func NewExpiringHistogram(name, help string, labelNames []string, buckets []float64, clock expire.Clock, ttl time.Duration, logger *logrus.Logger) *ExpiringHistogram {
	wrapped := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    name,
		Help:    help,
		Buckets: buckets,
	}, labelNames)

	return &ExpiringHistogram{
		wrapped:   wrapped,
		expiryMap: expire.NewExpiryMap[*HistogramEntry](clock, ttl),
		logger:    logger,
	}
}

// WithLabelValues returns the HistogramEntry for the given slice of label values.
// If accessed for the first time, a new HistogramEntry is created and cached.
func (h *ExpiringHistogram) WithLabelValues(lbls ...string) *HistogramEntry {
	return h.expiryMap.GetOrCreate(lbls, func() *HistogramEntry {
		h.logger.WithField("labelValues", lbls).Debug("Creating new histogram instance")
		observer, err := h.wrapped.GetMetricWithLabelValues(lbls...)
		if err != nil {
			panic(err) // Same behavior as beyla
		}
		return &HistogramEntry{
			observer:    observer,
			labelValues: lbls,
		}
	})
}

// Observe records a value with the given labels
func (h *ExpiringHistogram) Observe(value float64, labels HTTPMetricLabels) {
	labelValues := labels.ToSlice()
	entry := h.WithLabelValues(labelValues...)
	entry.observer.Observe(value)
}

// Describe implements prometheus.Collector
func (h *ExpiringHistogram) Describe(ch chan<- *prometheus.Desc) {
	h.wrapped.Describe(ch)
}

// Collect implements prometheus.Collector following beyla's pattern
func (h *ExpiringHistogram) Collect(ch chan<- prometheus.Metric) {
	// Clean up expired entries
	expired := h.expiryMap.DeleteExpired()
	for _, old := range expired {
		h.wrapped.DeleteLabelValues(old.labelValues...)
		h.logger.WithField("labelValues", old.labelValues).Debug("Deleting expired histogram metric")
	}

	// Collect from the wrapped HistogramVec which will include all active metrics
	h.wrapped.Collect(ch)
} // NewHTTPMetrics creates a new HTTPMetrics instance
func NewHTTPMetrics(logger *logrus.Logger) *HTTPMetrics {
	clock := expire.NewCachedClock(time.Now)
	clockFunc := clock.ClockFunc()
	ttl := 5 * time.Minute

	// Standard histogram buckets for different metrics
	durationBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	sizeBuckets := []float64{64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216}

	labelNames := []string{
		"client_namespace", "client_pod_name", "client_service_name", "client_owner_name", "client_owner_type",
		"server_namespace", "server_pod_name", "server_service_name", "server_owner_name", "server_owner_type",
		"req_method", "req_path", "resp_status_code",
	}

	return &HTTPMetrics{
		cachedClock: clock,
		logger:      logger,

		clientRequestDuration: NewExpiringHistogram(
			"http_client_request_duration_seconds",
			"Duration of HTTP service calls from the client side, in seconds",
			labelNames, durationBuckets, clockFunc, ttl, logger,
		),
		clientRequestBodySize: NewExpiringHistogram(
			"http_client_request_body_size_bytes",
			"Size, in bytes, of the HTTP request body as sent from the client side",
			labelNames, sizeBuckets, clockFunc, ttl, logger,
		),
		clientResponseBodySize: NewExpiringHistogram(
			"http_client_response_body_size_bytes",
			"Size, in bytes, of the HTTP response body as received at the client side",
			labelNames, sizeBuckets, clockFunc, ttl, logger,
		),
		serverRequestDuration: NewExpiringHistogram(
			"http_server_request_duration_seconds",
			"Duration of HTTP service calls from the server side, in seconds",
			labelNames, durationBuckets, clockFunc, ttl, logger,
		),
		serverRequestBodySize: NewExpiringHistogram(
			"http_server_request_body_size_bytes",
			"Size, in bytes, of the HTTP request body as received at the server side",
			labelNames, sizeBuckets, clockFunc, ttl, logger,
		),
		serverResponseBodySize: NewExpiringHistogram(
			"http_server_response_body_size_bytes",
			"Size, in bytes, of the HTTP response body as sent from the server side",
			labelNames, sizeBuckets, clockFunc, ttl, logger,
		),

		processedEvents: make(map[string]bool),
	}
}

// UpdateClock updates the cached clock used by all histograms
func (m *HTTPMetrics) UpdateClock() {
	m.cachedClock.Update()
}

// ProcessHTTPEvent processes a single HTTP event and updates metrics
func (m *HTTPMetrics) ProcessHTTPEvent(event map[string]interface{}) error {
	// Create unique event ID for deduplication
	eventID := m.createEventID(event)

	// Check if already processed
	m.processedEventsMu.RLock()
	if m.processedEvents[eventID] {
		m.processedEventsMu.RUnlock()
		return nil // Skip duplicate
	}
	m.processedEventsMu.RUnlock()

	// Mark as processed
	m.processedEventsMu.Lock()
	m.processedEvents[eventID] = true
	m.processedEventsMu.Unlock()

	// Extract trace role to determine client vs server perspective
	traceRoleRaw := event["trace_role"]
	traceRole := ""
	if traceRoleVal, err := parseFloat64(traceRoleRaw); err == nil {
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

	// Extract common fields
	reqMethod := getStringValue(event, "req_method", "")
	reqPath := cleanPath(getStringValue(event, "req_path", ""))

	// Convert resp_status to string
	respStatusNum, _ := parseFloat64(event["resp_status"])
	respStatus := fmt.Sprintf("%.0f", respStatusNum)

	// Extract latency and convert to seconds
	latencyNS, _ := parseFloat64(event["latency"])
	duration := latencyNS / 1e9 // Convert nanoseconds to seconds

	// Extract body sizes
	reqBodySize, _ := parseFloat64(event["req_body_size"])
	respBodySize, _ := parseFloat64(event["resp_body_size"])

	// Extract metadata from ctx fields
	service := getStringValue(event, "service", "")
	namespace := getStringValue(event, "namespace", "")
	podName := getStringValue(event, "pod_name", "")

	// Create labels based on trace role
	labels := m.createLabels(traceRole, namespace, podName, service, reqMethod, reqPath, respStatus)

	// Update metrics based on trace role
	switch traceRole {
	case "client":
		if duration > 0 {
			m.clientRequestDuration.Observe(duration, labels)
		}
		if reqBodySize > 0 {
			m.clientRequestBodySize.Observe(reqBodySize, labels)
		}
		if respBodySize > 0 {
			m.clientResponseBodySize.Observe(respBodySize, labels)
		}
	case "server":
		if duration > 0 {
			m.serverRequestDuration.Observe(duration, labels)
		}
		if reqBodySize > 0 {
			m.serverRequestBodySize.Observe(reqBodySize, labels)
		}
		if respBodySize > 0 {
			m.serverResponseBodySize.Observe(respBodySize, labels)
		}
	}

	return nil
}

// createEventID creates a unique identifier for an HTTP event to enable deduplication
func (m *HTTPMetrics) createEventID(event map[string]interface{}) string {
	// Use combination of time, trace_role, upid, and response details for uniqueness
	timeVal := getStringValue(event, "time_", "")
	traceRole := getStringValue(event, "trace_role", "")
	upid := getStringValue(event, "upid", "")
	respStatus := getStringValue(event, "resp_status", "")
	latency := getStringValue(event, "latency", "")

	return fmt.Sprintf("%s:%s:%s:%s:%s", timeVal, traceRole, upid, respStatus, latency)
}

// createLabels creates HTTPMetricLabels based on trace role and extracted metadata
func (m *HTTPMetrics) createLabels(traceRole, namespace, podName, service, reqMethod, reqPath, respStatus string) HTTPMetricLabels {
	var clientNamespace, clientPod, clientService string
	var serverNamespace, serverPod, serverService string

	if traceRole == "client" {
		clientNamespace = namespace
		clientPod = podName
		clientService = service
		serverNamespace = "unknown"
		serverPod = "unknown"
		serverService = "unknown"
	} else {
		clientNamespace = "unknown"
		clientPod = "unknown"
		clientService = "unknown"
		serverNamespace = namespace
		serverPod = podName
		serverService = service
	}

	return HTTPMetricLabels{
		ClientNamespace:   clientNamespace,
		ClientPodName:     clientPod,
		ClientServiceName: clientService,
		ClientOwnerName:   "", // Not available in exploration
		ClientOwnerType:   "", // Not available in exploration
		ServerNamespace:   serverNamespace,
		ServerPodName:     serverPod,
		ServerServiceName: serverService,
		ServerOwnerName:   "", // Not available in exploration
		ServerOwnerType:   "", // Not available in exploration
		ReqMethod:         reqMethod,
		ReqPath:           reqPath,
		RespStatusCode:    respStatus,
	}
}

// CleanupOldEvents removes processed events older than a certain threshold
func (m *HTTPMetrics) CleanupOldEvents() {
	m.processedEventsMu.Lock()
	defer m.processedEventsMu.Unlock()

	// Simple cleanup: if we have too many entries, clear half
	if len(m.processedEvents) > 10000 {
		m.logger.WithField("count", len(m.processedEvents)).Debug("Cleaning up processed events cache")

		// Keep only recent half (simple approach)
		newMap := make(map[string]bool, 5000)
		count := 0
		for k, v := range m.processedEvents {
			if count >= 5000 {
				break
			}
			newMap[k] = v
			count++
		}
		m.processedEvents = newMap

		m.logger.WithField("new_count", len(m.processedEvents)).Debug("Processed events cache cleaned")
	}
}

// Describe implements prometheus.Collector for all histograms
func (m *HTTPMetrics) Describe(ch chan<- *prometheus.Desc) {
	m.clientRequestDuration.Describe(ch)
	m.clientRequestBodySize.Describe(ch)
	m.clientResponseBodySize.Describe(ch)
	m.serverRequestDuration.Describe(ch)
	m.serverRequestBodySize.Describe(ch)
	m.serverResponseBodySize.Describe(ch)
}

// Collect implements prometheus.Collector for all histograms
func (m *HTTPMetrics) Collect(ch chan<- prometheus.Metric) {
	m.UpdateClock()

	m.clientRequestDuration.Collect(ch)
	m.clientRequestBodySize.Collect(ch)
	m.clientResponseBodySize.Collect(ch)
	m.serverRequestDuration.Collect(ch)
	m.serverRequestBodySize.Collect(ch)
	m.serverResponseBodySize.Collect(ch)

	// Cleanup old events periodically
	m.CleanupOldEvents()
}

// cleanPath cleans up URL path by removing query parameters
func cleanPath(rawPath string) string {
	if rawPath == "" {
		return ""
	}

	// Remove query parameters
	if idx := strings.Index(rawPath, "?"); idx != -1 {
		return rawPath[:idx]
	}

	return rawPath
}
