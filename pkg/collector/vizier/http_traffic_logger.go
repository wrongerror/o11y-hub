package vizier

import (
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/wrongerror/o11y-hub/pkg/k8s"
	"github.com/wrongerror/o11y-hub/pkg/logging"
)

// HTTPTrafficLogger manages HTTP traffic logging to files and implements HTTPEventSubscriber
type HTTPTrafficLogger struct {
	logChannel *logging.LogChannel
	k8sManager *k8s.Manager
	logger     *logrus.Logger

	// Control state
	isRunning bool
	runningMu sync.RWMutex
}

// NewHTTPTrafficLogger creates a new HTTP traffic logger
func NewHTTPTrafficLogger(logManager *logging.LogManager, logger *logrus.Logger) *HTTPTrafficLogger {
	logChannel := logging.NewLogChannel(logManager, 1000, logger) // Buffer size of 1000

	return &HTTPTrafficLogger{
		logChannel: logChannel,
		logger:     logger,
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

	// Stop the log channel
	h.logChannel.Stop()

	h.isRunning = false
	h.logger.Info("HTTP traffic logger stopped")
}

// ProcessEvent implements HTTPEventSubscriber interface
func (h *HTTPTrafficLogger) ProcessEvent(event *ProcessedHTTPEvent) error {
	// Create traffic log from processed event
	trafficLog := h.createTrafficLogFromProcessedEvent(event)

	// Log the event directly (deduplication is handled by HTTPEventProcessor)
	if err := h.logChannel.LogEvent(trafficLog); err != nil {
		h.logger.WithError(err).Error("Failed to log HTTP traffic event")
		return err
	}

	return nil
}

// createTrafficLogFromProcessedEvent converts a ProcessedHTTPEvent to HTTPTrafficLog
func (h *HTTPTrafficLogger) createTrafficLogFromProcessedEvent(event *ProcessedHTTPEvent) *logging.HTTPTrafficLog {
	// Convert status code from string to uint16
	var statusCode uint16
	if event.StatusCode != "" {
		if code, err := parseUint16(event.StatusCode); err == nil {
			statusCode = code
		}
	}

	// Extract UPID from raw event if available
	upid := getStringValue(event.Raw, "upid", "")

	// Create traffic log with processed event data
	trafficLog := &logging.HTTPTrafficLog{
		TimestampNS:    event.Timestamp.UnixNano(),
		UPID:           upid,
		TraceRole:      event.TraceRole,
		LocalAddr:      event.LocalAddr,
		LocalPort:      event.LocalPort,
		RemoteAddr:     event.RemoteAddr,
		RemotePort:     event.RemotePort,
		ReqMethod:      event.Method,
		ReqPath:        event.Path,
		RespStatusCode: statusCode,
		ReqBodySize:    event.ReqBodySize,
		RespBodySize:   event.RespBodySize,
		Duration:       event.Duration.Nanoseconds(),
		Encrypted:      event.Encrypted,

		// Source and destination metadata from processed event
		SrcNamespace:   event.Source.Namespace,
		SrcType:        event.Source.Type,
		SrcAddress:     event.Source.Address,
		SrcPodName:     event.Source.PodName,
		SrcServiceName: event.Source.ServiceName,
		SrcNodeName:    event.Source.NodeName,
		SrcOwnerName:   event.Source.OwnerName,
		SrcOwnerType:   event.Source.OwnerType,

		DstNamespace:   event.Destination.Namespace,
		DstType:        event.Destination.Type,
		DstAddress:     event.Destination.Address,
		DstPodName:     event.Destination.PodName,
		DstServiceName: event.Destination.ServiceName,
		DstNodeName:    event.Destination.NodeName,
		DstOwnerName:   event.Destination.OwnerName,
		DstOwnerType:   event.Destination.OwnerType,
	}

	return trafficLog
}

// parseUint16 safely converts a string to uint16
func parseUint16(s string) (uint16, error) {
	if i, err := parseFloat64(s); err == nil && i >= 0 && i <= 65535 {
		return uint16(i), nil
	}
	return 0, fmt.Errorf("invalid uint16 value: %s", s)
}
