package vizier

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"

	"github.com/wrongerror/observo-connector/pkg/expire"
	"github.com/wrongerror/observo-connector/pkg/k8s"
	"github.com/wrongerror/observo-connector/pkg/metrics"
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
	clockFunc := clock.ClockFunc()
	ttl := 5 * time.Minute

	// Standard histogram buckets for different metrics
	durationBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	sizeBuckets := []float64{64, 256, 1024, 4096, 16384, 65536, 262144, 1048576, 4194304, 16777216}

	labelNames := []string{
		"client_namespace", "client_type", "client_address", "client_pod_name", "client_service_name", "client_node_name", "client_owner_name", "client_owner_type",
		"server_namespace", "server_type", "server_address", "server_pod_name", "server_service_name", "server_node_name", "server_owner_name", "server_owner_type",
		"req_method", "req_path", "resp_status_code", "trace_role", "encrypted",
	}

	m := &HTTPMetrics{
		cachedClock: clock,
		logger:      logger,
		k8sManager:  nil, // Will be set by SetK8sManager

		clientRequestDuration: metrics.NewExpirer[prometheus.Histogram](
			prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "http_client_request_duration_seconds",
				Help:    "Duration of HTTP service calls from the client side, in seconds",
				Buckets: durationBuckets,
			}, labelNames),
			clockFunc, ttl,
		),
		clientRequestBodySize: metrics.NewExpirer[prometheus.Histogram](
			prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "http_client_request_body_size_bytes",
				Help:    "Size, in bytes, of the HTTP request body as sent from the client side",
				Buckets: sizeBuckets,
			}, labelNames),
			clockFunc, ttl,
		),
		clientResponseBodySize: metrics.NewExpirer[prometheus.Histogram](
			prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "http_client_response_body_size_bytes",
				Help:    "Size, in bytes, of the HTTP response body as received at the client side",
				Buckets: sizeBuckets,
			}, labelNames),
			clockFunc, ttl,
		),
		serverRequestDuration: metrics.NewExpirer[prometheus.Histogram](
			prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "http_server_request_duration_seconds",
				Help:    "Duration of HTTP service calls from the server side, in seconds",
				Buckets: durationBuckets,
			}, labelNames),
			clockFunc, ttl,
		),
		serverRequestBodySize: metrics.NewExpirer[prometheus.Histogram](
			prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "http_server_request_body_size_bytes",
				Help:    "Size, in bytes, of the HTTP request body as received at the server side",
				Buckets: sizeBuckets,
			}, labelNames),
			clockFunc, ttl,
		),
		serverResponseBodySize: metrics.NewExpirer[prometheus.Histogram](
			prometheus.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "http_server_response_body_size_bytes",
				Help:    "Size, in bytes, of the HTTP response body as sent from the server side",
				Buckets: sizeBuckets,
			}, labelNames),
			clockFunc, ttl,
		),

		processedEvents: make(map[string]time.Time), // TTL-based event deduplication
		stopCleanup:     make(chan struct{}),
		eventTTL:        ttl, // Use same TTL as metrics
	}

	// Start background cleanup goroutine
	m.cleanupTicker = time.NewTicker(2 * time.Minute) // Clean every 2 minutes
	go m.backgroundCleanup()

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

// ProcessHTTPEvent processes a single HTTP event and updates metrics
func (m *HTTPMetrics) ProcessHTTPEvent(event map[string]interface{}) error {
	// Create unique event ID for deduplication
	eventID := m.createEventID(event)

	now := time.Now()

	// Check if already processed
	m.processedEventsMu.RLock()
	lastSeen, exists := m.processedEvents[eventID]
	if exists && now.Sub(lastSeen) < m.eventTTL {
		m.processedEventsMu.RUnlock()
		return nil // Skip duplicate (within TTL)
	}
	m.processedEventsMu.RUnlock()

	// Mark as processed
	m.processedEventsMu.Lock()
	m.processedEvents[eventID] = now
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

	// Extract SSL information from Pixie's encrypted field
	encrypted := "false"
	if encryptedVal, ok := event["encrypted"].(bool); ok && encryptedVal {
		encrypted = "true"
	}

	// Extract metadata from ctx fields (local endpoint)
	service := getStringValue(event, "service", "")
	namespace := getStringValue(event, "namespace", "")
	podNameRaw := getStringValue(event, "pod_name", "")
	nodeName := getStringValue(event, "node_name", "")

	// Parse pod name which comes as "namespace/pod-name" format from Pixie
	var podName string
	if podNameRaw != "" && strings.Contains(podNameRaw, "/") {
		parts := strings.SplitN(podNameRaw, "/", 2)
		if len(parts) == 2 {
			// Use namespace from pod_name if local namespace is empty
			if namespace == "" {
				namespace = parts[0]
			}
			podName = parts[1]
		} else {
			podName = podNameRaw
		}
	} else {
		podName = podNameRaw
	}

	// Extract remote/local addresses
	remoteAddr := getStringValue(event, "remote_addr", "")
	localAddr := getStringValue(event, "local_addr", "")

	// Extract resolved remote endpoint information from Pixie's built-in functions
	remotePodNameRaw := getStringValue(event, "remote_pod_name", "")
	remoteServiceName := getStringValue(event, "remote_service_name", "")
	remoteNamespace := getStringValue(event, "remote_namespace", "")
	remoteNodeName := getStringValue(event, "remote_node_name", "")

	// Parse remote pod name which also comes as "namespace/pod-name" format
	var remotePodName string
	if remotePodNameRaw != "" && strings.Contains(remotePodNameRaw, "/") {
		parts := strings.SplitN(remotePodNameRaw, "/", 2)
		if len(parts) == 2 {
			// Use namespace from remote_pod_name if remote namespace is empty
			if remoteNamespace == "" {
				remoteNamespace = parts[0]
			}
			remotePodName = parts[1]
		} else {
			remotePodName = remotePodNameRaw
		}
	} else {
		remotePodName = remotePodNameRaw
	}

	// Create labels based on trace role with resolved remote endpoint info
	labels := m.createLabels(traceRole, namespace, podName, service, nodeName, remoteAddr, localAddr,
		remotePodName, remoteServiceName, remoteNamespace, remoteNodeName, encrypted, reqMethod, reqPath, respStatus) // Update metrics based on trace role
	switch traceRole {
	case "client":
		if duration > 0 {
			m.clientRequestDuration.WithLabelValues(labels.ToSlice()...).Metric.Observe(duration)
		}
		if reqBodySize > 0 {
			m.clientRequestBodySize.WithLabelValues(labels.ToSlice()...).Metric.Observe(reqBodySize)
		}
		if respBodySize > 0 {
			m.clientResponseBodySize.WithLabelValues(labels.ToSlice()...).Metric.Observe(respBodySize)
		}
	case "server":
		if duration > 0 {
			m.serverRequestDuration.WithLabelValues(labels.ToSlice()...).Metric.Observe(duration)
		}
		if reqBodySize > 0 {
			m.serverRequestBodySize.WithLabelValues(labels.ToSlice()...).Metric.Observe(reqBodySize)
		}
		if respBodySize > 0 {
			m.serverResponseBodySize.WithLabelValues(labels.ToSlice()...).Metric.Observe(respBodySize)
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

// determineEndpointType determines the correct endpoint type based on address and metadata
func (m *HTTPMetrics) determineEndpointType(addr, podName, serviceName, nodeName string) k8s.EndpointType {
	// For special IPs (loopback, link-local, etc.), use Pixie metadata to determine type
	if m.isSpecialIP(addr) {
		return m.determineTypeFromPixieData(podName, serviceName, nodeName)
	}

	// Use K8s manager to classify the IP if available
	var ipClassification k8s.EndpointType
	if m.k8sManager != nil {
		endpointInfo := m.k8sManager.GetEndpointInfo(addr)
		if endpointInfo != nil {
			ipClassification = endpointInfo.Type
		} else {
			ipClassification = k8s.EndpointTypeIP
		}
	} else {
		ipClassification = k8s.EndpointTypeIP
	}

	// Apply the correct logic based on requirements:
	// 1. If address is Service IP → service
	// 2. If address is Pod IP → pod
	// 3. If address is Node IP:
	//    - Has Pod info → pod (hostNetwork Pod)
	//    - No Pod info → node (direct Node access)
	// 4. Other cases → ip

	switch ipClassification {
	case k8s.EndpointTypeService:
		return k8s.EndpointTypeService
	case k8s.EndpointTypePod:
		return k8s.EndpointTypePod
	case k8s.EndpointTypeNode:
		if podName != "" {
			return k8s.EndpointTypePod // hostNetwork Pod
		}
		return k8s.EndpointTypeNode // direct Node access
	default:
		// Use Pixie metadata for unknown IPs
		return m.determineTypeFromPixieData(podName, serviceName, nodeName)
	}
}

// isSpecialIP checks if an IP is special (loopback, link-local, etc.)
func (m *HTTPMetrics) isSpecialIP(addr string) bool {
	if addr == "127.0.0.1" || addr == "::1" {
		return true // loopback
	}
	if strings.HasPrefix(addr, "169.254.") {
		return true // link-local (including Calico CNI)
	}
	return false
}

// determineTypeFromPixieData determines type based only on Pixie metadata
func (m *HTTPMetrics) determineTypeFromPixieData(podName, serviceName, nodeName string) k8s.EndpointType {
	// Priority: Service > Pod > Node > IP
	if serviceName != "" {
		return k8s.EndpointTypeService
	}
	if podName != "" {
		return k8s.EndpointTypePod
	}
	if nodeName != "" {
		return k8s.EndpointTypeNode
	}
	return k8s.EndpointTypeIP
}

// createLabels creates HTTPMetricLabels based on trace role and extracted metadata
func (m *HTTPMetrics) createLabels(traceRole, namespace, podName, service, nodeName, remoteAddr, localAddr,
	remotePodName, remoteServiceName, remoteNamespace, remoteNodeName, encrypted, reqMethod, reqPath, respStatus string) HTTPMetricLabels {

	// Get local endpoint information
	localOwnerName, localOwnerType := m.getOwnerInfo(namespace, podName)

	// Get remote endpoint information using K8s manager if available
	var remoteEndpointInfo *k8s.EndpointInfo
	var remoteOwnerName, remoteOwnerType string

	if m.k8sManager != nil && remoteAddr != "" {
		remoteEndpointInfo = m.k8sManager.GetEndpointInfo(remoteAddr)
		if remoteEndpointInfo != nil && remoteEndpointInfo.Type == k8s.EndpointTypePod {
			remoteOwnerName, remoteOwnerType = m.k8sManager.GetPodOwnerInfo(remoteEndpointInfo.Namespace, remoteEndpointInfo.Name)
		}
	}

	// Fill in missing remote info from Pixie data if K8s manager didn't provide it
	if remoteEndpointInfo == nil {
		remoteEndpointInfo = &k8s.EndpointInfo{
			Name:        remotePodName,
			Namespace:   remoteNamespace,
			IP:          remoteAddr,
			NodeName:    remoteNodeName,
			ServiceName: remoteServiceName,
		}

		// Determine remote endpoint type using the improved logic
		remoteEndpointInfo.Type = m.determineEndpointType(remoteAddr, remotePodName, remoteServiceName, remoteNodeName)
	}

	var labels HTTPMetricLabels

	if traceRole == "client" {
		// Local endpoint is client
		labels.ClientNamespace = namespace
		labels.ClientType = string(m.determineEndpointType(localAddr, podName, service, nodeName))
		labels.ClientAddress = localAddr
		labels.ClientPodName = podName
		labels.ClientServiceName = service
		labels.ClientNodeName = nodeName
		labels.ClientOwnerName = localOwnerName
		labels.ClientOwnerType = localOwnerType

		// Remote endpoint is server
		labels.ServerNamespace = remoteEndpointInfo.Namespace
		labels.ServerType = string(remoteEndpointInfo.Type)
		labels.ServerAddress = remoteEndpointInfo.IP
		labels.ServerPodName = remoteEndpointInfo.Name
		labels.ServerServiceName = remoteEndpointInfo.ServiceName
		labels.ServerNodeName = remoteEndpointInfo.NodeName
		labels.ServerOwnerName = remoteOwnerName
		labels.ServerOwnerType = remoteOwnerType
	} else {
		// Remote endpoint is client
		labels.ClientNamespace = remoteEndpointInfo.Namespace
		labels.ClientType = string(remoteEndpointInfo.Type)
		labels.ClientAddress = remoteEndpointInfo.IP
		labels.ClientPodName = remoteEndpointInfo.Name
		labels.ClientServiceName = remoteEndpointInfo.ServiceName
		labels.ClientNodeName = remoteEndpointInfo.NodeName
		labels.ClientOwnerName = remoteOwnerName
		labels.ClientOwnerType = remoteOwnerType

		// Local endpoint is server
		labels.ServerNamespace = namespace
		labels.ServerType = string(m.determineEndpointType(localAddr, podName, service, nodeName))
		labels.ServerAddress = localAddr
		labels.ServerPodName = podName
		labels.ServerServiceName = service
		labels.ServerNodeName = nodeName
		labels.ServerOwnerName = localOwnerName
		labels.ServerOwnerType = localOwnerType
	}

	// Set request/response information
	labels.TraceRole = traceRole
	labels.Encrypted = encrypted
	labels.ReqMethod = reqMethod
	labels.ReqPath = reqPath
	labels.RespStatusCode = respStatus

	return labels
}

// getOwnerInfo gets owner information for a pod
func (m *HTTPMetrics) getOwnerInfo(namespace, podName string) (string, string) {
	if m.k8sManager != nil && namespace != "" && podName != "" {
		ownerName, ownerType := m.k8sManager.GetPodOwnerInfo(namespace, podName)
		logrus.Debugf("Getting owner info for pod %s/%s: owner=%s, type=%s", namespace, podName, ownerName, ownerType)
		return ownerName, ownerType
	}
	logrus.Debugf("No owner info available for pod %s/%s (k8sManager=%v)", namespace, podName, m.k8sManager != nil)
	return "", ""
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

// Stop stops the background cleanup goroutine
func (m *HTTPMetrics) Stop() {
	close(m.stopCleanup)
}

// CleanupOldEvents removes expired events (called during metrics collection)
func (m *HTTPMetrics) CleanupOldEvents() {
	m.processedEventsMu.Lock()
	defer m.processedEventsMu.Unlock()

	now := time.Now()
	expiredCount := 0

	// Clean expired events using TTL mechanism
	for eventID, lastSeen := range m.processedEvents {
		if now.Sub(lastSeen) > m.eventTTL {
			delete(m.processedEvents, eventID)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		m.logger.WithField("expired_count", expiredCount).Debug("Metrics collection cleanup removed expired events")
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
