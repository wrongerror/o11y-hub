package vizier

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/wrongerror/o11y-hub/pkg/collector"
	"github.com/wrongerror/o11y-hub/pkg/vizier"
)

// HTTPTrafficCollector is responsible for continuously collecting HTTP traffic logs
type HTTPTrafficCollector struct {
	logger        *logrus.Logger
	vizierClient  *vizier.Client
	config        collector.Config
	trafficLogger *HTTPTrafficLogger

	// Control channels
	stopChan  chan struct{}
	wg        sync.WaitGroup
	isRunning bool
	runningMu sync.RWMutex

	// Collection interval
	collectInterval time.Duration
}

// NewHTTPTrafficCollector creates a new HTTP traffic collector
func NewHTTPTrafficCollector(
	logger *logrus.Logger,
	vizierClient *vizier.Client,
	config collector.Config,
	trafficLogger *HTTPTrafficLogger,
) *HTTPTrafficCollector {
	return &HTTPTrafficCollector{
		logger:          logger,
		vizierClient:    vizierClient,
		config:          config,
		trafficLogger:   trafficLogger,
		stopChan:        make(chan struct{}),
		collectInterval: 30 * time.Second, // Collect every 30 seconds
	}
}

// Start begins the continuous HTTP traffic collection
func (h *HTTPTrafficCollector) Start() error {
	h.runningMu.Lock()
	defer h.runningMu.Unlock()

	if h.isRunning {
		return nil // Already running
	}

	h.wg.Add(1)
	go h.collectLoop()

	h.isRunning = true
	h.logger.Info("HTTP traffic collector started")

	return nil
}

// Stop gracefully stops the HTTP traffic collection
func (h *HTTPTrafficCollector) Stop() {
	h.runningMu.Lock()
	defer h.runningMu.Unlock()

	if !h.isRunning {
		return // Already stopped
	}

	close(h.stopChan)
	h.wg.Wait()

	h.isRunning = false
	h.logger.Info("HTTP traffic collector stopped")
}

// collectLoop is the main loop that continuously collects HTTP traffic data
func (h *HTTPTrafficCollector) collectLoop() {
	defer h.wg.Done()

	ticker := time.NewTicker(h.collectInterval)
	defer ticker.Stop()

	// Initial collection
	h.collectHTTPTraffic()

	for {
		select {
		case <-ticker.C:
			h.collectHTTPTraffic()
		case <-h.stopChan:
			return
		}
	}
}

// collectHTTPTraffic collects HTTP traffic data from Vizier and sends to logger
func (h *HTTPTrafficCollector) collectHTTPTraffic() {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// Execute PxL script to get HTTP events with complete data for logging
	script := `import px

# Get HTTP events from the last 30 seconds with all available fields
df = px.DataFrame(table='http_events', start_time='-30s')

# Extract ctx metadata fields for local endpoint
df.service = df.ctx['service']
df.namespace = df.ctx['namespace'] 
df.pod_name = df.ctx['pod_name']
df.container_name = df.ctx['container_name']
df.node_name = df.ctx['node_name']

# Resolve remote endpoint information using Pixie's built-in functions
df.remote_pod_id = px.ip_to_pod_id(df.remote_addr)
df.remote_pod_name = px.pod_id_to_pod_name(df.remote_pod_id)
df.remote_service_name = px.pod_id_to_service_name(df.remote_pod_id)
df.remote_namespace = px.pod_id_to_namespace(df.remote_pod_id)
df.remote_node_name = px.pod_id_to_node_name(df.remote_pod_id)

# Include all fields for comprehensive logging
px.display(df, 'http_events')
`

	result, err := h.vizierClient.ExecuteScriptAndExtractData(ctx, h.config.VizierClusterID, script)
	if err != nil {
		h.logger.WithError(err).Warn("Failed to collect HTTP traffic data")
		return
	}

	if result == nil || len(result.Data) == 0 {
		h.logger.Debug("No HTTP traffic data found")
		return
	}

	h.logger.WithField("event_count", len(result.Data)).Debug("Collected HTTP traffic data")

	// Process each HTTP event for logging
	for _, row := range result.Data {
		if h.trafficLogger != nil {
			h.trafficLogger.ProcessHTTPEvent(row)
		}
	}
}
