package server

import (
	"fmt"
	stdlog "log"
	"net/http"
	"sort"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	promcollectors "github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/wrongerror/observo-connector/pkg/collector"
	_ "github.com/wrongerror/observo-connector/pkg/collector/beyla"  // Register beyla collector
	_ "github.com/wrongerror/observo-connector/pkg/collector/vizier" // Register vizier collector
)

// Handler implements the HTTP handler with collector-based metrics
type Handler struct {
	unfilteredHandler       http.Handler
	exporterMetricsRegistry *prometheus.Registry
	includeExporterMetrics  bool
	maxRequests             int
	logger                  *logrus.Logger
	config                  collector.Config
}

// NewHandler creates a new HTTP handler with collector support
func NewHandler(includeExporterMetrics bool, maxRequests int, logger *logrus.Logger, config collector.Config, filters ...string) *Handler {
	h := &Handler{
		exporterMetricsRegistry: prometheus.NewRegistry(),
		includeExporterMetrics:  includeExporterMetrics,
		maxRequests:             maxRequests,
		logger:                  logger,
		config:                  config,
	}

	// Register Go runtime and process metrics if enabled
	if h.includeExporterMetrics {
		h.exporterMetricsRegistry.MustRegister(
			promcollectors.NewProcessCollector(promcollectors.ProcessCollectorOpts{}),
			promcollectors.NewGoCollector(),
		)
	}

	// Create unfiltered handler
	if innerHandler, err := h.innerHandler(filters...); err != nil {
		panic(fmt.Sprintf("Couldn't create metrics handler: %s", err))
	} else {
		h.unfilteredHandler = innerHandler
	}

	return h
}

// ServeHTTP implements http.Handler
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	filters := r.URL.Query()["collect[]"]
	h.logger.WithField("filters", filters).Debug("Collect query")

	if len(filters) == 0 {
		// No filters, use the prepared unfiltered handler
		h.unfilteredHandler.ServeHTTP(w, r)
		return
	}

	// To serve filtered metrics, we create a filtering handler on the fly
	filteredHandler, err := h.innerHandler(filters...)
	if err != nil {
		h.logger.WithError(err).Warn("Couldn't create filtered metrics handler")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Couldn't create filtered metrics handler: %s", err)))
		return
	}
	filteredHandler.ServeHTTP(w, r)
}

// innerHandler creates the prometheus handler with collectors
func (h *Handler) innerHandler(filters ...string) (http.Handler, error) {
	observoCollector, err := collector.NewObservoCollector(h.logger, h.config)
	if err != nil {
		return nil, fmt.Errorf("couldn't create observo collector: %s", err)
	}

	// Only log the creation of an unfiltered handler, which should happen
	// only once upon startup
	if len(filters) == 0 {
		h.logger.Info("Enabled collectors")
		collectors := []string{}
		for name := range observoCollector.Collectors {
			collectors = append(collectors, name)
		}
		sort.Strings(collectors)
		for _, c := range collectors {
			h.logger.WithField("collector", c).Info("Collector enabled")
		}
	}

	r := prometheus.NewRegistry()

	// Register version information
	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: collector.Namespace,
			Name:      "build_info",
			Help:      "Build information",
		},
		[]string{"version"},
	)
	buildInfo.WithLabelValues("dev").Set(1)
	r.MustRegister(buildInfo)

	// Register the observo collector
	if err := r.Register(observoCollector); err != nil {
		return nil, fmt.Errorf("couldn't register observo collector: %s", err)
	}

	handler := promhttp.HandlerFor(
		prometheus.Gatherers{h.exporterMetricsRegistry, r},
		promhttp.HandlerOpts{
			ErrorLog:            stdlog.New(&logrusAdapter{logger: h.logger}, "", 0),
			ErrorHandling:       promhttp.ContinueOnError,
			MaxRequestsInFlight: h.maxRequests,
			Registry:            h.exporterMetricsRegistry,
		},
	)

	if h.includeExporterMetrics {
		// Note that we have to use h.exporterMetricsRegistry here to
		// use the same promhttp metrics for all expositions
		handler = promhttp.InstrumentMetricHandler(
			h.exporterMetricsRegistry, handler,
		)
	}

	return handler, nil
}

// Server represents the HTTP server
type Server struct {
	router  *mux.Router
	logger  *logrus.Logger
	port    int
	handler *Handler
}

// NewServer creates a new HTTP server with collector-based architecture
func NewServer(port int, config collector.Config, logger *logrus.Logger) *Server {
	// Create the collector-based handler
	handler := NewHandler(
		true, // include exporter metrics
		40,   // max requests
		logger,
		config,
	)

	s := &Server{
		router:  mux.NewRouter(),
		logger:  logger,
		port:    port,
		handler: handler,
	}

	s.setupRoutes()
	return s
}

// setupRoutes sets up the HTTP routes
func (s *Server) setupRoutes() {
	// Standard Prometheus metrics endpoint
	s.router.Path("/metrics").Handler(s.handler)

	// Health check endpoint
	s.router.HandleFunc("/health", s.handleHealth).Methods("GET")

	// Landing page
	s.router.HandleFunc("/", s.handleLandingPage).Methods("GET")

	// Add logging middleware
	s.router.Use(s.loggingMiddleware)
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy","timestamp":"` + time.Now().Format(time.RFC3339) + `"}`))
}

// handleLandingPage handles the landing page
func (s *Server) handleLandingPage(w http.ResponseWriter, r *http.Request) {
	landingPage := `<html>
<head><title>Observo Connector</title></head>
<body>
<h1>Observo Connector</h1>
<p><a href="/metrics">Metrics</a></p>
<p><a href="/health">Health</a></p>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(landingPage))
}

// loggingMiddleware provides request logging
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.logger.WithFields(logrus.Fields{
			"method":   r.Method,
			"path":     r.URL.Path,
			"duration": time.Since(start),
			"remote":   r.RemoteAddr,
		}).Info("HTTP request")
	})
}

// Start starts the HTTP server
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	s.logger.WithField("address", addr).Info("Starting HTTP server")
	return http.ListenAndServe(addr, s.router)
}

// StartTLS starts the HTTPS server
func (s *Server) StartTLS(certFile, keyFile string) error {
	addr := fmt.Sprintf(":%d", s.port)
	s.logger.WithField("address", addr).Info("Starting HTTPS server")
	return http.ListenAndServeTLS(addr, certFile, keyFile, s.router)
}

// logrusAdapter adapts logrus to stdlib log interface
type logrusAdapter struct {
	logger *logrus.Logger
}

func (l *logrusAdapter) Write(p []byte) (n int, err error) {
	l.logger.Error(string(p))
	return len(p), nil
}
