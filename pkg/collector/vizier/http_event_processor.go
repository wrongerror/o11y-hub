package vizier

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/wrongerror/o11y-hub/pkg/k8s"
)

// EndpointMetadata represents metadata for a network endpoint
type EndpointMetadata struct {
	Namespace   string
	Type        string
	Address     string
	PodName     string
	ServiceName string
	NodeName    string
	OwnerName   string
	OwnerType   string
}

// ProcessedHTTPEvent represents a fully processed HTTP event with enriched metadata
type ProcessedHTTPEvent struct {
	// Original raw event data
	Raw map[string]interface{}

	// Basic HTTP information
	TraceRole    string        // "client" or "server"
	Method       string        // HTTP method
	Path         string        // Request path
	StatusCode   string        // Response status code
	Duration     time.Duration // Request duration
	ReqBodySize  int64         // Request body size in bytes
	RespBodySize int64         // Response body size in bytes
	Encrypted    bool          // Whether the connection is encrypted

	// Network information
	LocalAddr  string
	LocalPort  uint16
	RemoteAddr string
	RemotePort uint16

	// Enriched endpoint metadata
	Source      EndpointMetadata // Source endpoint (client in client perspective, server in server perspective)
	Destination EndpointMetadata // Destination endpoint

	// Event metadata
	EventID   string    // Unique event ID for deduplication
	Timestamp time.Time // Event timestamp
}

// HTTPEventSubscriber defines the interface for components that process HTTP events
type HTTPEventSubscriber interface {
	ProcessEvent(event *ProcessedHTTPEvent) error
}

// HTTPEventProcessor processes raw HTTP events from Vizier and enriches them with K8s metadata
type HTTPEventProcessor struct {
	k8sManager *k8s.Manager
	logger     *logrus.Logger

	// Event deduplication with TTL
	processedEvents   map[string]time.Time
	processedEventsMu sync.RWMutex
	cleanupTicker     *time.Ticker
	stopCleanup       chan struct{}
	eventTTL          time.Duration

	// Subscribers
	subscribers   []HTTPEventSubscriber
	subscribersMu sync.RWMutex

	// Control
	isRunning bool
	runningMu sync.RWMutex
	wg        sync.WaitGroup
}

// NewHTTPEventProcessor creates a new HTTP event processor
func NewHTTPEventProcessor(logger *logrus.Logger) *HTTPEventProcessor {
	return &HTTPEventProcessor{
		logger:          logger,
		processedEvents: make(map[string]time.Time),
		stopCleanup:     make(chan struct{}),
		eventTTL:        5 * time.Minute,
		subscribers:     make([]HTTPEventSubscriber, 0),
	}
}

// SetK8sManager sets the Kubernetes manager for metadata enrichment
func (p *HTTPEventProcessor) SetK8sManager(k8sManager *k8s.Manager) {
	p.k8sManager = k8sManager
}

// AddSubscriber adds a new subscriber to receive processed events
func (p *HTTPEventProcessor) AddSubscriber(subscriber HTTPEventSubscriber) {
	p.subscribersMu.Lock()
	defer p.subscribersMu.Unlock()
	p.subscribers = append(p.subscribers, subscriber)
}

// Start starts the event processor
func (p *HTTPEventProcessor) Start() error {
	p.runningMu.Lock()
	defer p.runningMu.Unlock()

	if p.isRunning {
		return nil // Already running
	}

	// Start background cleanup for event deduplication
	p.cleanupTicker = time.NewTicker(2 * time.Minute)
	go p.backgroundCleanup()

	p.isRunning = true
	p.logger.Info("HTTP event processor started")

	return nil
}

// Stop stops the event processor
func (p *HTTPEventProcessor) Stop() {
	p.runningMu.Lock()
	defer p.runningMu.Unlock()

	if !p.isRunning {
		return // Already stopped
	}

	// Signal stop to cleanup goroutine
	close(p.stopCleanup)

	// Stop cleanup ticker
	if p.cleanupTicker != nil {
		p.cleanupTicker.Stop()
	}

	// Wait for any ongoing processing to complete
	p.wg.Wait()

	p.isRunning = false
	p.logger.Info("HTTP event processor stopped")
}

// ProcessEvent processes a raw HTTP event and distributes it to subscribers
func (p *HTTPEventProcessor) ProcessEvent(rawEvent map[string]interface{}) error {
	// Parse and enrich the event
	processedEvent, err := p.parseAndEnrichEvent(rawEvent)
	if err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	// Check for deduplication
	now := time.Now()
	p.processedEventsMu.RLock()
	lastSeen, exists := p.processedEvents[processedEvent.EventID]
	if exists && now.Sub(lastSeen) < p.eventTTL {
		p.processedEventsMu.RUnlock()
		return nil // Skip duplicate
	}
	p.processedEventsMu.RUnlock()

	// Mark as processed
	p.processedEventsMu.Lock()
	p.processedEvents[processedEvent.EventID] = now
	p.processedEventsMu.Unlock()

	// Distribute to all subscribers
	p.subscribersMu.RLock()
	defer p.subscribersMu.RUnlock()

	for _, subscriber := range p.subscribers {
		if err := subscriber.ProcessEvent(processedEvent); err != nil {
			p.logger.WithError(err).Error("Subscriber failed to process event")
		}
	}

	return nil
}

// parseAndEnrichEvent parses raw event data and enriches it with K8s metadata
func (p *HTTPEventProcessor) parseAndEnrichEvent(rawEvent map[string]interface{}) (*ProcessedHTTPEvent, error) {
	event := &ProcessedHTTPEvent{
		Raw:       rawEvent,
		Timestamp: time.Now(),
	}

	// Extract trace role
	traceRoleRaw := rawEvent["trace_role"]
	if traceRoleVal, err := parseFloat64(traceRoleRaw); err == nil {
		// trace_role: 1 = client, 2 = server in Pixie
		if traceRoleVal == 1 {
			event.TraceRole = "client"
		} else if traceRoleVal == 2 {
			event.TraceRole = "server"
		}
	}
	if event.TraceRole == "" {
		return nil, fmt.Errorf("invalid or missing trace_role")
	}

	// Extract basic HTTP information
	event.Method = getStringValue(rawEvent, "req_method", "")
	event.Path = cleanPath(getStringValue(rawEvent, "req_path", ""))

	// Convert resp_status to string
	respStatusNum, _ := parseFloat64(rawEvent["resp_status"])
	event.StatusCode = fmt.Sprintf("%.0f", respStatusNum)

	// Extract latency and convert to duration
	latencyNS, _ := parseFloat64(rawEvent["latency"])
	event.Duration = time.Duration(latencyNS) * time.Nanosecond

	// Extract body sizes
	reqBodySizeVal, _ := parseFloat64(rawEvent["req_body_size"])
	event.ReqBodySize = int64(reqBodySizeVal)
	respBodySizeVal, _ := parseFloat64(rawEvent["resp_body_size"])
	event.RespBodySize = int64(respBodySizeVal)

	// Extract SSL information
	if encryptedVal, ok := rawEvent["encrypted"].(bool); ok {
		event.Encrypted = encryptedVal
	}

	// Extract network information
	event.LocalAddr = getStringValue(rawEvent, "local_addr", "")
	event.RemoteAddr = getStringValue(rawEvent, "remote_addr", "")
	localPortVal, _ := parseFloat64(rawEvent["local_port"])
	event.LocalPort = uint16(localPortVal)
	remotePortVal, _ := parseFloat64(rawEvent["remote_port"])
	event.RemotePort = uint16(remotePortVal)

	// Create event ID for deduplication
	event.EventID = p.createEventID(event)

	// Enrich with K8s metadata
	p.enrichWithK8sMetadata(event, rawEvent)

	return event, nil
}

// enrichWithK8sMetadata enriches the event with Kubernetes metadata
func (p *HTTPEventProcessor) enrichWithK8sMetadata(event *ProcessedHTTPEvent, rawEvent map[string]interface{}) {
	// Extract metadata from ctx fields (local endpoint)
	service := getStringValue(rawEvent, "service", "")
	namespace := getStringValue(rawEvent, "namespace", "")
	podNameRaw := getStringValue(rawEvent, "pod_name", "")
	nodeName := getStringValue(rawEvent, "node_name", "")

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
	remotePodNameRaw := getStringValue(rawEvent, "remote_pod_name", "")
	remoteServiceName := getStringValue(rawEvent, "remote_service_name", "")
	remoteNamespace := getStringValue(rawEvent, "remote_namespace", "")
	remoteNodeName := getStringValue(rawEvent, "remote_node_name", "")

	// Parse remote pod name
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

	// Parse remote service name
	if remoteServiceName != "" && strings.Contains(remoteServiceName, "/") {
		parts := strings.SplitN(remoteServiceName, "/", 2)
		if len(parts) == 2 {
			if remoteNamespace == "" {
				remoteNamespace = parts[0]
			}
			remoteServiceName = parts[1]
		}
	}

	// Get owner information using K8s manager
	var localOwnerName, localOwnerType string
	var remoteOwnerName, remoteOwnerType string

	if p.k8sManager != nil {
		if namespace != "" && podName != "" {
			localOwnerName, localOwnerType = p.k8sManager.GetPodOwnerInfo(namespace, podName)
		}
		if remoteNamespace != "" && remotePodName != "" {
			remoteOwnerName, remoteOwnerType = p.k8sManager.GetPodOwnerInfo(remoteNamespace, remotePodName)
		}
	}

	// Fill in the metadata based on trace role
	if event.TraceRole == "client" {
		// Local endpoint is client (source)
		event.Source = EndpointMetadata{
			Namespace:   namespace,
			Type:        "pod",
			Address:     event.LocalAddr,
			PodName:     podName,
			ServiceName: service,
			NodeName:    nodeName,
			OwnerName:   localOwnerName,
			OwnerType:   localOwnerType,
		}

		// Remote endpoint is server (destination)
		event.Destination = EndpointMetadata{
			Namespace:   remoteNamespace,
			Type:        p.determineEndpointType(event.RemoteAddr, remoteNamespace, remotePodName, remoteServiceName),
			Address:     event.RemoteAddr,
			PodName:     remotePodName,
			ServiceName: remoteServiceName,
			NodeName:    remoteNodeName,
			OwnerName:   remoteOwnerName,
			OwnerType:   remoteOwnerType,
		}
	} else {
		// Local endpoint is server (destination)
		event.Destination = EndpointMetadata{
			Namespace:   namespace,
			Type:        "pod",
			Address:     event.LocalAddr,
			PodName:     podName,
			ServiceName: service,
			NodeName:    nodeName,
			OwnerName:   localOwnerName,
			OwnerType:   localOwnerType,
		}

		// Remote endpoint is client (source)
		event.Source = EndpointMetadata{
			Namespace:   remoteNamespace,
			Type:        p.determineEndpointType(event.RemoteAddr, remoteNamespace, remotePodName, remoteServiceName),
			Address:     event.RemoteAddr,
			PodName:     remotePodName,
			ServiceName: remoteServiceName,
			NodeName:    remoteNodeName,
			OwnerName:   remoteOwnerName,
			OwnerType:   remoteOwnerType,
		}
	}

	// Further enrich with K8s manager information if available
	if p.k8sManager != nil {
		p.enrichEndpointWithK8s(&event.Source)
		p.enrichEndpointWithK8s(&event.Destination)
	}
}

// determineEndpointType determines the type of endpoint based on available information
func (p *HTTPEventProcessor) determineEndpointType(address, namespace, podName, serviceName string) string {
	if podName != "" {
		return "pod"
	}
	if serviceName != "" {
		return "service"
	}
	if p.k8sManager != nil {
		if endpointInfo := p.k8sManager.GetEndpointInfo(address); endpointInfo != nil {
			return string(endpointInfo.Type)
		}
	}
	return "ip"
}

// enrichEndpointWithK8s enriches endpoint metadata using K8s manager
func (p *HTTPEventProcessor) enrichEndpointWithK8s(endpoint *EndpointMetadata) {
	if p.k8sManager == nil || endpoint.Address == "" {
		return
	}

	if endpointInfo := p.k8sManager.GetEndpointInfo(endpoint.Address); endpointInfo != nil {
		// Fill in missing information from K8s manager
		if endpoint.Type == "ip" {
			endpoint.Type = string(endpointInfo.Type)
		}
		if endpoint.Namespace == "" {
			endpoint.Namespace = endpointInfo.Namespace
		}
		if endpoint.PodName == "" {
			endpoint.PodName = endpointInfo.Name
		}
		if endpoint.ServiceName == "" {
			endpoint.ServiceName = endpointInfo.ServiceName
		}
		if endpoint.NodeName == "" {
			endpoint.NodeName = endpointInfo.NodeName
		}
		if endpoint.OwnerName == "" {
			endpoint.OwnerName = endpointInfo.OwnerName
		}
		if endpoint.OwnerType == "" {
			endpoint.OwnerType = endpointInfo.OwnerType
		}
	}
}

// createEventID creates a unique event ID for deduplication
func (p *HTTPEventProcessor) createEventID(event *ProcessedHTTPEvent) string {
	// Create ID based on key fields that should be unique for each distinct event
	return fmt.Sprintf("%s:%s:%s:%s:%s:%d",
		event.TraceRole,
		event.LocalAddr,
		event.RemoteAddr,
		event.Method,
		event.Path,
		event.Timestamp.UnixNano()/1000000, // Millisecond precision
	)
}

// backgroundCleanup runs in background to periodically clean expired events
func (p *HTTPEventProcessor) backgroundCleanup() {
	defer p.wg.Done()
	p.wg.Add(1)

	for {
		select {
		case <-p.cleanupTicker.C:
			p.processedEventsMu.Lock()
			now := time.Now()
			expiredCount := 0

			// Remove expired entries
			for eventID, lastSeen := range p.processedEvents {
				if now.Sub(lastSeen) > p.eventTTL {
					delete(p.processedEvents, eventID)
					expiredCount++
				}
			}
			p.processedEventsMu.Unlock()

			if expiredCount > 0 {
				p.logger.WithField("expired_count", expiredCount).Debug("HTTP event processor cleanup removed expired events")
			}

		case <-p.stopCleanup:
			return
		}
	}
}
