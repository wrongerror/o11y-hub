package vizier

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"github.com/wrongerror/o11y-hub/pkg/expire"
	"github.com/wrongerror/o11y-hub/pkg/k8s"
	"github.com/wrongerror/o11y-hub/pkg/metrics"
)

const (
	// metricsNamespace defines the namespace for HTTP metrics
	// Empty string means metrics will start with subsystem directly (http_*)
	metricsNamespace = ""
)

// HTTPMetrics manages Prometheus histogram metrics for HTTP requests
type HTTPMetrics struct {
	cachedClock *expire.CachedClock
	logger      *logrus.Logger
	k8sManager  *k8s.Manager

	// Histogram metrics with expiring labels using generic Expirer
	clientRequestDuration  *metrics.Expirer[prometheus.Histogram]
	clientRequestBodySize  *metrics.Expirer[prometheus.Histogram]
	clientResponseBodySize *metrics.Expirer[prometheus.Histogram]
	serverRequestDuration  *metrics.Expirer[prometheus.Histogram]
	serverRequestBodySize  *metrics.Expirer[prometheus.Histogram]
	serverResponseBodySize *metrics.Expirer[prometheus.Histogram]

	// Event deduplication with TTL
	processedEvents   map[string]time.Time // Store event ID -> last seen time
	processedEventsMu sync.RWMutex
	cleanupTicker     *time.Ticker
	stopCleanup       chan struct{}
	eventTTL          time.Duration
}

// HTTPMetricLabels represents the label set for HTTP metrics
type HTTPMetricLabels struct {
	ClientNamespace   string
	ClientType        string
	ClientAddress     string
	ClientPodName     string
	ClientServiceName string
	ClientNodeName    string
	ClientOwnerName   string
	ClientOwnerType   string
	ServerNamespace   string
	ServerType        string
	ServerAddress     string
	ServerPodName     string
	ServerServiceName string
	ServerNodeName    string
	ServerOwnerName   string
	ServerOwnerType   string
	ReqMethod         string
	ReqPath           string
	RespStatusCode    string
	TraceRole         string
	Encrypted         string
}

// ToSlice converts HTTPMetricLabels to a string slice for use with expiry map
func (l HTTPMetricLabels) ToSlice() []string {
	return []string{
		l.ClientNamespace,
		l.ClientType,
		l.ClientAddress,
		l.ClientPodName,
		l.ClientServiceName,
		l.ClientNodeName,
		l.ClientOwnerName,
		l.ClientOwnerType,
		l.ServerNamespace,
		l.ServerType,
		l.ServerAddress,
		l.ServerPodName,
		l.ServerServiceName,
		l.ServerNodeName,
		l.ServerOwnerName,
		l.ServerOwnerType,
		l.ReqMethod,
		l.ReqPath,
		l.RespStatusCode,
		l.TraceRole,
		l.Encrypted,
	}
}

// NewHTTPMetrics creates a new HTTPMetrics instance
func NewHTTPMetrics(logger *logrus.Logger) *HTTPMetrics {
	clock := expire.NewCachedClock(time.Now)
	ttl := 5 * time.Minute

	// Standard histogram buckets for different metrics
	durationBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	sizeBuckets := []float64{64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216}

	labelNames := []string{
		"client_namespace", "client_type", "client_address", "client_pod_name",
		"client_service_name", "client_node_name", "client_owner_name", "client_owner_type",
		"server_namespace", "server_type", "server_address", "server_pod_name",
		"server_service_name", "server_node_name", "server_owner_name", "server_owner_type",
		"req_method", "req_path", "resp_status_code", "trace_role", "encrypted",
	}

	// Create metric vectors for use with expiring labels
	clientRequestDurationVec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: "http",
			Name:      "client_request_duration_seconds",
			Help:      "Histogram of client HTTP request durations",
			Buckets:   durationBuckets,
		},
		labelNames,
	)

	clientRequestBodySizeVec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: "http",
			Name:      "client_request_body_size_bytes",
			Help:      "Histogram of client HTTP request body sizes in bytes",
			Buckets:   sizeBuckets,
		},
		labelNames,
	)

	clientResponseBodySizeVec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: "http",
			Name:      "client_response_body_size_bytes",
			Help:      "Histogram of client HTTP response body sizes in bytes",
			Buckets:   sizeBuckets,
		},
		labelNames,
	)

	serverRequestDurationVec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: "http",
			Name:      "server_request_duration_seconds",
			Help:      "Histogram of server HTTP request durations",
			Buckets:   durationBuckets,
		},
		labelNames,
	)

	serverRequestBodySizeVec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: "http",
			Name:      "server_request_body_size_bytes",
			Help:      "Histogram of server HTTP request body sizes in bytes",
			Buckets:   sizeBuckets,
		},
		labelNames,
	)

	serverResponseBodySizeVec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: metricsNamespace,
			Subsystem: "http",
			Name:      "server_response_body_size_bytes",
			Help:      "Histogram of server HTTP response body sizes in bytes",
			Buckets:   sizeBuckets,
		},
		labelNames,
	)

	m := &HTTPMetrics{
		cachedClock:            clock,
		logger:                 logger,
		k8sManager:             nil, // Will be set by SetK8sManager
		processedEvents:        make(map[string]time.Time),
		stopCleanup:            make(chan struct{}),
		eventTTL:               5 * time.Minute,
		clientRequestDuration:  metrics.NewExpirer[prometheus.Histogram](clientRequestDurationVec, clock.ClockFunc(), ttl),
		clientRequestBodySize:  metrics.NewExpirer[prometheus.Histogram](clientRequestBodySizeVec, clock.ClockFunc(), ttl),
		clientResponseBodySize: metrics.NewExpirer[prometheus.Histogram](clientResponseBodySizeVec, clock.ClockFunc(), ttl),
		serverRequestDuration:  metrics.NewExpirer[prometheus.Histogram](serverRequestDurationVec, clock.ClockFunc(), ttl),
		serverRequestBodySize:  metrics.NewExpirer[prometheus.Histogram](serverRequestBodySizeVec, clock.ClockFunc(), ttl),
		serverResponseBodySize: metrics.NewExpirer[prometheus.Histogram](serverResponseBodySizeVec, clock.ClockFunc(), ttl),
	}

	return m
}

// SetK8sManager sets the Kubernetes manager for endpoint resolution
func (m *HTTPMetrics) SetK8sManager(k8sManager *k8s.Manager) {
	m.k8sManager = k8sManager
}

// UpdateClock updates the cached clock used by all histograms
func (m *HTTPMetrics) UpdateClock() {
	m.cachedClock.Update()
}

// ProcessEvent implements HTTPEventSubscriber interface
func (m *HTTPMetrics) ProcessEvent(event *ProcessedHTTPEvent) error {
	// Create labels from the processed event
	labels := m.createLabelsFromEvent(event)

	// Convert duration from nanoseconds to seconds for histograms
	durationSeconds := event.Duration.Seconds()

	// Update histograms based on trace role
	if event.TraceRole == "client" {
		// Client-side metrics
		m.clientRequestDuration.WithLabelValues(labels.ToSlice()...).Metric.Observe(durationSeconds)
		m.clientRequestBodySize.WithLabelValues(labels.ToSlice()...).Metric.Observe(float64(event.ReqBodySize))
		m.clientResponseBodySize.WithLabelValues(labels.ToSlice()...).Metric.Observe(float64(event.RespBodySize))
	} else {
		// Server-side metrics
		m.serverRequestDuration.WithLabelValues(labels.ToSlice()...).Metric.Observe(durationSeconds)
		m.serverRequestBodySize.WithLabelValues(labels.ToSlice()...).Metric.Observe(float64(event.ReqBodySize))
		m.serverResponseBodySize.WithLabelValues(labels.ToSlice()...).Metric.Observe(float64(event.RespBodySize))
	}

	return nil
}

// ProcessHTTPEvent processes a raw HTTP event (for backward compatibility with collector)
func (m *HTTPMetrics) ProcessHTTPEvent(rawEvent map[string]interface{}) error {
	// This method is now deprecated in favor of the unified event processor
	// It's kept for backward compatibility but should not be used directly
	// The unified processor will call ProcessEvent instead
	m.logger.Warn("ProcessHTTPEvent called directly - this should be handled by the unified event processor")
	return nil
}

// Collect implements the prometheus.Collector interface for backward compatibility
func (m *HTTPMetrics) Collect(ch chan<- prometheus.Metric) {
	// Trigger cleanup to remove expired histograms
	m.UpdateClock()

	// Use Expirer to collect all current histograms and send to channel
	m.clientRequestDuration.Collect(ch)
	m.clientRequestBodySize.Collect(ch)
	m.clientResponseBodySize.Collect(ch)
	m.serverRequestDuration.Collect(ch)
	m.serverRequestBodySize.Collect(ch)
	m.serverResponseBodySize.Collect(ch)
}

// createLabelsFromEvent creates metric labels from a processed event
func (m *HTTPMetrics) createLabelsFromEvent(event *ProcessedHTTPEvent) HTTPMetricLabels {
	// Convert boolean to string
	encrypted := "false"
	if event.Encrypted {
		encrypted = "true"
	}

	return HTTPMetricLabels{
		ClientNamespace:   event.Source.Namespace,
		ClientType:        event.Source.Type,
		ClientAddress:     event.Source.Address,
		ClientPodName:     event.Source.PodName,
		ClientServiceName: event.Source.ServiceName,
		ClientNodeName:    event.Source.NodeName,
		ClientOwnerName:   event.Source.OwnerName,
		ClientOwnerType:   event.Source.OwnerType,
		ServerNamespace:   event.Destination.Namespace,
		ServerType:        event.Destination.Type,
		ServerAddress:     event.Destination.Address,
		ServerPodName:     event.Destination.PodName,
		ServerServiceName: event.Destination.ServiceName,
		ServerNodeName:    event.Destination.NodeName,
		ServerOwnerName:   event.Destination.OwnerName,
		ServerOwnerType:   event.Destination.OwnerType,
		ReqMethod:         event.Method,
		ReqPath:           event.Path,
		RespStatusCode:    event.StatusCode,
		TraceRole:         event.TraceRole,
		Encrypted:         encrypted,
	}
}

// Start starts background cleanup for expired histogram labels
func (m *HTTPMetrics) Start() {
	// Start background cleanup for event deduplication
	m.cleanupTicker = time.NewTicker(2 * time.Minute)
	go m.backgroundCleanup()
}

// Stop stops the background cleanup goroutine
func (m *HTTPMetrics) Stop() {
	close(m.stopCleanup)
}

// Update implements the collector.Collector interface
func (m *HTTPMetrics) Update(ch chan<- prometheus.Metric) error {
	// Trigger cleanup to remove expired histograms
	m.UpdateClock()

	// Use Expirer to collect all current histograms and send to channel
	m.clientRequestDuration.Collect(ch)
	m.clientRequestBodySize.Collect(ch)
	m.clientResponseBodySize.Collect(ch)
	m.serverRequestDuration.Collect(ch)
	m.serverRequestBodySize.Collect(ch)
	m.serverResponseBodySize.Collect(ch)

	return nil
}

// backgroundCleanup runs in background to periodically clean expired events
func (m *HTTPMetrics) backgroundCleanup() {
	for {
		select {
		case <-m.cleanupTicker.C:
			m.processedEventsMu.Lock()
			now := time.Now()
			expiredCount := 0

			// Remove expired entries
			for eventID, lastSeen := range m.processedEvents {
				if now.Sub(lastSeen) > m.eventTTL {
					delete(m.processedEvents, eventID)
					expiredCount++
				}
			}
			m.processedEventsMu.Unlock()

			if expiredCount > 0 {
				m.logger.WithField("expired_count", expiredCount).Debug("Background cleanup removed expired events")
			}

		case <-m.stopCleanup:
			m.cleanupTicker.Stop()
			return
		}
	}
}
