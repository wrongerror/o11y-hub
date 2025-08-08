package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/wrongerror/observo-connector/pkg/common"
	"github.com/wrongerror/observo-connector/pkg/scripts"
	"github.com/wrongerror/observo-connector/pkg/vizier"
)

// Server HTTP服务器
type Server struct {
	router           *mux.Router
	logger           *logrus.Logger
	vizierClient     *vizier.Client
	scriptExecutor   *scripts.Executor
	port             int
	defaultClusterID string

	// Prometheus metrics
	registry        *prometheus.Registry
	upMetric        prometheus.Gauge
	buildInfoMetric *prometheus.GaugeVec
	requestCounter  *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
}

// NewServer 创建新的HTTP服务器
func NewServer(port int, vizierClient *vizier.Client, defaultClusterID string) *Server {
	logger := logrus.New()
	scriptExecutor := scripts.NewExecutor(vizierClient, logger)

	// Create Prometheus registry
	registry := prometheus.NewRegistry()

	// Define metrics
	upMetric := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "observo_connector_up",
		Help: "Whether the connector is up",
	})

	buildInfoMetric := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "observo_connector_build_info",
		Help: "Build information",
	}, []string{"version"})

	requestCounter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "observo_connector_requests_total",
		Help: "Total number of HTTP requests",
	}, []string{"method", "path", "status"})

	requestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "observo_connector_request_duration_seconds",
		Help: "HTTP request duration in seconds",
	}, []string{"method", "path"})

	// Register metrics
	registry.MustRegister(upMetric, buildInfoMetric, requestCounter, requestDuration)

	// Set static metrics
	upMetric.Set(1)
	buildInfoMetric.WithLabelValues("dev").Set(1)

	s := &Server{
		router:           mux.NewRouter(),
		logger:           logger,
		vizierClient:     vizierClient,
		scriptExecutor:   scriptExecutor,
		port:             port,
		defaultClusterID: defaultClusterID,
		registry:         registry,
		upMetric:         upMetric,
		buildInfoMetric:  buildInfoMetric,
		requestCounter:   requestCounter,
		requestDuration:  requestDuration,
	}

	s.setupRoutes()
	return s
}

// setupRoutes 设置路由
func (s *Server) setupRoutes() {
	// Prometheus metrics endpoint - 使用标准的/metrics路径
	s.router.Path("/metrics").Handler(promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))

	// 为了兼容性，也提供/api/v1/metrics路径
	s.router.Path("/api/v1/metrics").Handler(promhttp.HandlerFor(s.registry, promhttp.HandlerOpts{}))

	// API路由
	api := s.router.PathPrefix("/api/v1").Subrouter()

	// 健康检查
	api.HandleFunc("/health", s.handleHealth).Methods("GET")

	// 脚本执行接口
	api.HandleFunc("/execute", s.handleExecuteScript).Methods("POST")
	api.HandleFunc("/scripts", s.handleListScripts).Methods("GET")
	api.HandleFunc("/scripts/{name}", s.handleGetScript).Methods("GET")

	// 添加中间件
	s.router.Use(s.loggingMiddleware)
}

// QueryRequest 查询请求
type QueryRequest struct {
	Query     string               `json:"query"`
	ClusterID string               `json:"cluster_id"`
	Format    common.ExportFormat  `json:"format,omitempty"`
	Options   common.ExportOptions `json:"options,omitempty"`
}

// QueryResponse 查询响应
type QueryResponse struct {
	Success   bool                `json:"success"`
	Data      *common.QueryResult `json:"data,omitempty"`
	Error     string              `json:"error,omitempty"`
	Timestamp time.Time           `json:"timestamp"`
}

// handleQuery 处理POST查询请求
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	if req.Query == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Query is required")
		return
	}

	if req.ClusterID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Cluster ID is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := s.executeQuery(ctx, req.Query, req.ClusterID)
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 如果指定了导出格式，直接返回格式化数据
	if req.Format != "" {
		s.handleExportResponse(w, result, req.Format, req.Options)
		return
	}

	response := QueryResponse{
		Success:   true,
		Data:      result,
		Timestamp: time.Now(),
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// handleQueryGET 处理GET查询请求
func (s *Server) handleQueryGET(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	clusterID := r.URL.Query().Get("cluster_id")
	format := r.URL.Query().Get("format")

	if query == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Query parameter is required")
		return
	}

	if clusterID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Cluster ID parameter is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := s.executeQuery(ctx, query, clusterID)
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 如果指定了导出格式，直接返回格式化数据
	if format != "" {
		exportFormat := common.ExportFormat(format)
		s.handleExportResponse(w, result, exportFormat, common.ExportOptions{})
		return
	}

	response := QueryResponse{
		Success:   true,
		Data:      result,
		Timestamp: time.Now(),
	}

	s.writeJSONResponse(w, http.StatusOK, response)
}

// handleHealth 处理健康检查
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	clusterID := r.URL.Query().Get("cluster_id")
	if clusterID == "" {
		clusterID = "default" // 使用默认cluster ID
	}

	err := s.vizierClient.HealthCheck(ctx, clusterID)
	if err != nil {
		response := map[string]interface{}{
			"status":    "unhealthy",
			"error":     err.Error(),
			"timestamp": time.Now(),
		}
		s.writeJSONResponse(w, http.StatusServiceUnavailable, response)
		return
	}

	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
	}
	s.writeJSONResponse(w, http.StatusOK, response)
}

// handleMetrics 处理Prometheus metrics请求 - 简化版本
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	// 暂时返回静态的Prometheus格式数据
	// 真正的Prometheus metrics由/metrics端点通过SDK提供

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Write([]byte(`# HELP observo_connector_up Whether the connector is up
# TYPE observo_connector_up gauge
observo_connector_up 1
# HELP observo_connector_build_info Build information
# TYPE observo_connector_build_info gauge
observo_connector_build_info{version="dev"} 1
`))
}

// getServerMetrics returns basic server health metrics
func (s *Server) getServerMetrics() []common.Metric {
	now := time.Now()
	return []common.Metric{
		{
			Name:        "observo_connector_up",
			Type:        common.MetricTypeGauge,
			Description: "Whether the connector is up",
			Value:       1,
			Timestamp:   now,
		},
		{
			Name:        "observo_connector_build_info",
			Type:        common.MetricTypeGauge,
			Description: "Build information",
			Labels:      map[string]string{"version": "dev"},
			Value:       1,
			Timestamp:   now,
		},
	}
}

// executeQuery 执行查询
func (s *Server) executeQuery(ctx context.Context, query, clusterID string) (*common.QueryResult, error) {
	s.logger.WithFields(logrus.Fields{
		"query":      query,
		"cluster_id": clusterID,
	}).Info("Executing query")

	// Use the script executor for query execution
	return s.scriptExecutor.ExecuteBuiltinScriptForMetrics(ctx, clusterID, "custom", map[string]string{
		"query": query,
	})
}

// handleExport 处理导出请求
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	var req QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, "Invalid JSON payload")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	result, err := s.executeQuery(ctx, req.Query, req.ClusterID)
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	format := req.Format
	if format == "" {
		format = common.FormatJSON
	}

	s.handleExportResponse(w, result, format, req.Options)
}

// handleExportResponse 处理导出响应 - 简化版本
func (s *Server) handleExportResponse(w http.ResponseWriter, result *common.QueryResult, format common.ExportFormat, opts common.ExportOptions) {
	// Simplified export - just return JSON for now
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":   true,
		"data":      result,
		"timestamp": time.Now(),
	})
}

// writeJSONResponse 写入JSON响应
func (s *Server) writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// writeErrorResponse 写入错误响应
func (s *Server) writeErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	response := map[string]interface{}{
		"success":   false,
		"error":     message,
		"timestamp": time.Now(),
	}
	s.writeJSONResponse(w, statusCode, response)
}

// loggingMiddleware 日志中间件
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

// corsMiddleware CORS中间件
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Start 启动服务器
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	s.logger.WithField("address", addr).Info("Starting HTTP server")
	return http.ListenAndServe(addr, s.router)
}

// StartTLS 启动TLS服务器
func (s *Server) StartTLS(certFile, keyFile string) error {
	addr := fmt.Sprintf(":%d", s.port)
	s.logger.WithField("address", addr).Info("Starting HTTPS server")
	return http.ListenAndServeTLS(addr, certFile, keyFile, s.router)
}

// handleExecuteScript 处理脚本执行请求
func (s *Server) handleExecuteScript(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Script    string            `json:"script"`
		ClusterID string            `json:"cluster_id,omitempty"`
		Params    map[string]string `json:"params,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	clusterID := req.ClusterID
	if clusterID == "" {
		clusterID = s.defaultClusterID
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	err := s.vizierClient.ExecuteScript(ctx, clusterID, req.Script)
	if err != nil {
		s.logger.WithError(err).Error("Failed to execute script")
		http.Error(w, fmt.Sprintf("Script execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Script executed successfully",
	})
}

// handleListScripts 处理列出脚本请求 - 简化版本
func (s *Server) handleListScripts(w http.ResponseWriter, r *http.Request) {
	// Return a basic list of available scripts
	scripts := []string{"http_overview", "resource_usage", "network_stats", "error_analysis"}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"scripts": scripts,
	})
}

// handleGetScript 处理获取脚本详情请求 - 简化版本
func (s *Server) handleGetScript(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	scriptName := vars["name"]

	// Return basic script info
	scriptInfo := map[string]interface{}{
		"name":        scriptName,
		"description": fmt.Sprintf("Built-in script: %s", scriptName),
		"parameters":  []string{"start_time", "namespace"},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"script":  scriptInfo,
	})
}
