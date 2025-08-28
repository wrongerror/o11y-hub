package vizier

import (
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/wrongerror/o11y-hub/pkg/k8s"
	"github.com/wrongerror/o11y-hub/pkg/logging"
)

// HTTPTrafficLogger manages HTTP traffic logging to files
type HTTPTrafficLogger struct {
	logChannel *logging.LogChannel
	k8sManager *k8s.Manager
	logger     *logrus.Logger

	// Event deduplication with TTL
	processedEvents   map[string]time.Time
	processedEventsMu sync.RWMutex
	cleanupTicker     *time.Ticker
	stopCleanup       chan struct{}
	eventTTL          time.Duration

	// Control channels
	eventChan chan map[string]interface{}
	stopChan  chan struct{}
	wg        sync.WaitGroup
	isRunning bool
	runningMu sync.RWMutex
}

// NewHTTPTrafficLogger creates a new HTTP traffic logger
func NewHTTPTrafficLogger(logManager *logging.LogManager, logger *logrus.Logger) *HTTPTrafficLogger {
	logChannel := logging.NewLogChannel(logManager, 1000, logger) // Buffer size of 1000

	return &HTTPTrafficLogger{
		logChannel:      logChannel,
		logger:          logger,
		processedEvents: make(map[string]time.Time),
		stopCleanup:     make(chan struct{}),
		eventTTL:        5 * time.Minute, // Same as HTTP metrics
		eventChan:       make(chan map[string]interface{}, 1000),
		stopChan:        make(chan struct{}),
	}
}

// SetK8sManager sets the Kubernetes manager for metadata enrichment
func (h *HTTPTrafficLogger) SetK8sManager(k8sManager *k8s.Manager) {
	h.k8sManager = k8sManager
}

// Start begins the HTTP traffic logging service
func (h *HTTPTrafficLogger) Start() error {
	h.runningMu.Lock()
	defer h.runningMu.Unlock()

	if h.isRunning {
		return nil // Already running
	}

	// Start the log channel
	h.logChannel.Start()

	// Start background cleanup for event deduplication
	h.cleanupTicker = time.NewTicker(2 * time.Minute)
	go h.backgroundCleanup()

	// Start the main processing goroutine
	h.wg.Add(1)
	go h.processEvents()

	h.isRunning = true
	h.logger.Info("HTTP traffic logger started")

	return nil
}

// Stop gracefully stops the HTTP traffic logging service
func (h *HTTPTrafficLogger) Stop() {
	h.runningMu.Lock()
	defer h.runningMu.Unlock()

	if !h.isRunning {
		return // Already stopped
	}

	// Signal stop to all goroutines
	close(h.stopChan)
	close(h.stopCleanup)

	// Stop cleanup ticker
	if h.cleanupTicker != nil {
		h.cleanupTicker.Stop()
	}

	// Wait for processing goroutine to finish
	h.wg.Wait()

	// Stop the log channel
	h.logChannel.Stop()

	h.isRunning = false
	h.logger.Info("HTTP traffic logger stopped")
}

// ProcessHTTPEvent processes an HTTP event for logging (non-blocking)
func (h *HTTPTrafficLogger) ProcessHTTPEvent(event map[string]interface{}) {
	h.runningMu.RLock()
	defer h.runningMu.RUnlock()

	if !h.isRunning {
		return
	}

	select {
	case h.eventChan <- event:
		// Event successfully queued
	default:
		// Channel is full, drop the event
		h.logger.Warn("HTTP traffic event channel full, dropping event")
	}
}

// processEvents is the main goroutine that processes HTTP events
func (h *HTTPTrafficLogger) processEvents() {
	defer h.wg.Done()

	for {
		select {
		case event, ok := <-h.eventChan:
			if !ok {
				return // Channel closed
			}
			h.processEventInternal(event)

		case <-h.stopChan:
			// Drain remaining events
			for {
				select {
				case event := <-h.eventChan:
					h.processEventInternal(event)
				default:
					return
				}
			}
		}
	}
}

// processEventInternal handles the actual processing of an HTTP event
func (h *HTTPTrafficLogger) processEventInternal(event map[string]interface{}) {
	// Create traffic log from event
	trafficLog := logging.NewHTTPTrafficLogFromEvent(event)

	// Check for deduplication
	eventID := trafficLog.CreateEventID()
	now := time.Now()

	h.processedEventsMu.RLock()
	lastSeen, exists := h.processedEvents[eventID]
	if exists && now.Sub(lastSeen) < h.eventTTL {
		h.processedEventsMu.RUnlock()
		return // Skip duplicate
	}
	h.processedEventsMu.RUnlock()

	// Mark as processed
	h.processedEventsMu.Lock()
	h.processedEvents[eventID] = now
	h.processedEventsMu.Unlock()

	// Enrich with metadata using the same logic as HTTP metrics
	h.enrichWithMetadata(trafficLog, event)

	// Log the event
	if err := h.logChannel.LogEvent(trafficLog); err != nil {
		h.logger.WithError(err).Error("Failed to log HTTP traffic event")
	}
}

// enrichWithMetadata enriches the traffic log with Kubernetes metadata
func (h *HTTPTrafficLogger) enrichWithMetadata(trafficLog *logging.HTTPTrafficLog, event map[string]interface{}) {
	// Extract metadata from ctx fields (local endpoint) - similar to HTTP metrics
	service := getStringValue(event, "service", "")
	namespace := getStringValue(event, "namespace", "")
	podNameRaw := getStringValue(event, "pod_name", "")
	nodeName := getStringValue(event, "node_name", "")

	// Parse pod name which comes as "namespace/pod-name" format from Pixie
	var podName string
	if podNameRaw != "" && strings.Contains(podNameRaw, "/") {
		parts := strings.SplitN(podNameRaw, "/", 2)
		if len(parts) == 2 {
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

	// Parse service name which also comes as "namespace/service-name" format from Pixie
	if service != "" && strings.Contains(service, "/") {
		parts := strings.SplitN(service, "/", 2)
		if len(parts) == 2 {
			if namespace == "" {
				namespace = parts[0]
			}
			service = parts[1]
		}
	}

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

	// Parse remote service name which also comes as "namespace/service-name" format from Pixie
	if remoteServiceName != "" && strings.Contains(remoteServiceName, "/") {
		parts := strings.SplitN(remoteServiceName, "/", 2)
		if len(parts) == 2 {
			if remoteNamespace == "" {
				remoteNamespace = parts[0]
			}
			remoteServiceName = parts[1]
		}
	}

	// Get local endpoint information
	var localOwnerName, localOwnerType string
	if h.k8sManager != nil && namespace != "" && podName != "" {
		localOwnerName, localOwnerType = h.k8sManager.GetPodOwnerInfo(namespace, podName)
	}

	// Get remote endpoint information using K8s manager
	var remoteOwnerName, remoteOwnerType string
	if h.k8sManager != nil && remoteNamespace != "" && remotePodName != "" {
		remoteOwnerName, remoteOwnerType = h.k8sManager.GetPodOwnerInfo(remoteNamespace, remotePodName)
	}

	// Fill in the metadata based on trace role
	if trafficLog.TraceRole == "client" {
		// Local endpoint is client
		trafficLog.SrcNamespace = namespace
		trafficLog.SrcAddress = trafficLog.LocalAddr
		trafficLog.SrcPodName = podName
		trafficLog.SrcServiceName = service
		trafficLog.SrcNodeName = nodeName
		trafficLog.SrcOwnerName = localOwnerName
		trafficLog.SrcOwnerType = localOwnerType

		// Remote endpoint is server
		trafficLog.DstNamespace = remoteNamespace
		trafficLog.DstAddress = trafficLog.RemoteAddr
		trafficLog.DstPodName = remotePodName
		trafficLog.DstServiceName = remoteServiceName
		trafficLog.DstNodeName = remoteNodeName
		trafficLog.DstOwnerName = remoteOwnerName
		trafficLog.DstOwnerType = remoteOwnerType
	} else {
		// Remote endpoint is client
		trafficLog.SrcNamespace = remoteNamespace
		trafficLog.SrcAddress = trafficLog.RemoteAddr
		trafficLog.SrcPodName = remotePodName
		trafficLog.SrcServiceName = remoteServiceName
		trafficLog.SrcNodeName = remoteNodeName
		trafficLog.SrcOwnerName = remoteOwnerName
		trafficLog.SrcOwnerType = remoteOwnerType

		// Local endpoint is server
		trafficLog.DstNamespace = namespace
		trafficLog.DstAddress = trafficLog.LocalAddr
		trafficLog.DstPodName = podName
		trafficLog.DstServiceName = service
		trafficLog.DstNodeName = nodeName
		trafficLog.DstOwnerName = localOwnerName
		trafficLog.DstOwnerType = localOwnerType
	}

	// Determine endpoint types using similar logic as HTTP metrics
	if h.k8sManager != nil {
		trafficLog.SrcType = h.determineEndpointType(trafficLog.SrcAddress, trafficLog.SrcPodName, trafficLog.SrcServiceName, trafficLog.SrcNodeName)
		trafficLog.DstType = h.determineEndpointType(trafficLog.DstAddress, trafficLog.DstPodName, trafficLog.DstServiceName, trafficLog.DstNodeName)
	} else {
		// Default fallback
		trafficLog.SrcType = "ip"
		trafficLog.DstType = "ip"
	}
}

// determineEndpointType determines the endpoint type using similar logic as HTTP metrics
func (h *HTTPTrafficLogger) determineEndpointType(addr, podName, serviceName, nodeName string) string {
	// For special IPs (loopback, link-local, etc.), use Pixie metadata to determine type
	if h.isSpecialIP(addr) {
		return h.determineTypeFromPixieData(podName, serviceName, nodeName)
	}

	// Use K8s manager to classify the IP if available
	var ipClassification k8s.EndpointType
	if h.k8sManager != nil {
		endpointInfo := h.k8sManager.GetEndpointInfo(addr)
		if endpointInfo != nil {
			ipClassification = endpointInfo.Type
		} else {
			ipClassification = k8s.EndpointTypeIP
		}
	} else {
		ipClassification = k8s.EndpointTypeIP
	}

	switch ipClassification {
	case k8s.EndpointTypeService:
		return "service"
	case k8s.EndpointTypePod:
		return "pod"
	case k8s.EndpointTypeNode:
		if podName != "" {
			return "pod" // hostNetwork Pod
		}
		return "node" // direct Node access
	default:
		// Use Pixie metadata for unknown IPs
		return h.determineTypeFromPixieData(podName, serviceName, nodeName)
	}
}

// isSpecialIP checks if an IP is special (loopback, link-local, etc.)
func (h *HTTPTrafficLogger) isSpecialIP(addr string) bool {
	if addr == "127.0.0.1" || addr == "::1" {
		return true // loopback
	}
	if strings.HasPrefix(addr, "169.254.") {
		return true // link-local (including Calico CNI)
	}
	return false
}

// determineTypeFromPixieData determines type based only on Pixie metadata
func (h *HTTPTrafficLogger) determineTypeFromPixieData(podName, serviceName, nodeName string) string {
	// Priority: Service > Pod > Node > IP
	if serviceName != "" {
		return "service"
	}
	if podName != "" {
		return "pod"
	}
	if nodeName != "" {
		return "node"
	}
	return "ip"
}

// backgroundCleanup runs in background to periodically clean expired events
func (h *HTTPTrafficLogger) backgroundCleanup() {
	for {
		select {
		case <-h.cleanupTicker.C:
			h.processedEventsMu.Lock()
			now := time.Now()
			expiredCount := 0

			// Remove expired entries
			for eventID, lastSeen := range h.processedEvents {
				if now.Sub(lastSeen) > h.eventTTL {
					delete(h.processedEvents, eventID)
					expiredCount++
				}
			}
			h.processedEventsMu.Unlock()

			if expiredCount > 0 {
				h.logger.WithField("expired_count", expiredCount).Debug("HTTP traffic logger cleanup removed expired events")
			}

		case <-h.stopCleanup:
			return
		}
	}
}
