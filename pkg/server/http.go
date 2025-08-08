package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"

	"github.com/wrongerror/observo-connector/pkg/common"
	"github.com/wrongerror/observo-connector/pkg/export"
	"github.com/wrongerror/observo-connector/pkg/metrics"
	"github.com/wrongerror/observo-connector/pkg/scripts"
	"github.com/wrongerror/observo-connector/pkg/vizier"
)

// Server HTTP服务器
type Server struct {
	router           *mux.Router
	logger           *logrus.Logger
	vizierClient     *vizier.Client
	exportFactory    *export.ExporterFactory
	dataConverter    *export.DataConverter
	scriptExecutor   *scripts.Executor
	metricsConverter *metrics.PrometheusConverter
	port             int
	defaultClusterID string
}

// NewServer 创建新的HTTP服务器
func NewServer(port int, vizierClient *vizier.Client, defaultClusterID string) *Server {
	logger := logrus.New()
	scriptExecutor := scripts.NewExecutor(vizierClient, logger)
	
	s := &Server{
		router:           mux.NewRouter(),
		logger:           logger,
		vizierClient:     vizierClient,
		exportFactory:    export.NewExporterFactory(),
		dataConverter:    export.NewDataConverter(),
		scriptExecutor:   scriptExecutor,
		metricsConverter: metrics.NewPrometheusConverter(),
		port:             port,
		defaultClusterID: defaultClusterID,
	}

	s.setupRoutes()
	return s
}

// setupRoutes 设置路由
func (s *Server) setupRoutes() {
	// API路由
	api := s.router.PathPrefix("/api/v1").Subrouter()

	// 查询接口
	api.HandleFunc("/query", s.handleQuery).Methods("POST")
	api.HandleFunc("/query", s.handleQueryGET).Methods("GET")

	// 健康检查
	api.HandleFunc("/health", s.handleHealth).Methods("GET")

	// Prometheus metrics接口
	api.HandleFunc("/metrics", s.handleMetrics).Methods("GET")

	// 导出接口
	api.HandleFunc("/export", s.handleExport).Methods("POST")

	// 静态文件和文档
	s.router.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))

	// 添加中间件
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.corsMiddleware)
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

// handleMetrics 处理Prometheus metrics请求
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	clusterID := r.URL.Query().Get("cluster_id")
	if clusterID == "" {
		clusterID = s.defaultClusterID
	}
	
	if clusterID == "" {
		s.writeErrorResponse(w, http.StatusBadRequest, "Cluster ID parameter is required")
		return
	}

	scriptName := r.URL.Query().Get("script")
	
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	var results []*common.QueryResult
	var err error

	if scriptName != "" {
		// Execute specific script
		result, execErr := s.scriptExecutor.ExecuteBuiltinScriptForMetrics(ctx, clusterID, scriptName, map[string]string{
			"start_time": r.URL.Query().Get("start_time"),
			"namespace":  r.URL.Query().Get("namespace"),
			"pod_name":   r.URL.Query().Get("pod_name"),
		})
		if execErr != nil {
			s.writeErrorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Failed to execute script '%s': %v", scriptName, execErr))
			return
		}
		if result != nil {
			results = []*common.QueryResult{result}
		}
	} else {
		// Execute all metric scripts
		results, err = s.scriptExecutor.ExecuteAllMetricScripts(ctx, clusterID)
		if err != nil {
			s.writeErrorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Failed to execute scripts: %v", err))
			return
		}
	}

	// Convert to Prometheus metrics
	prometheusMetrics := s.metricsConverter.ConvertToMetrics(results)
	
	// Add server metrics
	prometheusMetrics = append(prometheusMetrics, s.getServerMetrics()...)

	// Create result with metrics
	queryResult := &common.QueryResult{
		Metrics:   prometheusMetrics,
		Timestamp: time.Now(),
	}

	// Export as Prometheus format
	s.handleExportResponse(w, queryResult, common.FormatPrometheusText, common.ExportOptions{})
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

// handleExportResponse 处理导出响应
func (s *Server) handleExportResponse(w http.ResponseWriter, result *common.QueryResult, format common.ExportFormat, opts common.ExportOptions) {
	exporter, err := s.exportFactory.CreateExporter(format)
	if err != nil {
		s.writeErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	// 如果是Prometheus格式，需要先转换数据
	if format == common.FormatPrometheusText {
		result.Metrics = s.dataConverter.ConvertToMetrics(result)
	}

	data, err := exporter.Export(result, opts)
	if err != nil {
		s.writeErrorResponse(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", exporter.ContentType())
	w.WriteHeader(http.StatusOK)
	w.Write(data)
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
