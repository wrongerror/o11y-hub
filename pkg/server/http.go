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
	"github.com/wrongerror/observo-connector/pkg/vizier"
)

// Server HTTP服务器
type Server struct {
	router        *mux.Router
	logger        *logrus.Logger
	vizierClient  *vizier.Client
	exportFactory *export.ExporterFactory
	dataConverter *export.DataConverter
	port          int
}

// NewServer 创建新的HTTP服务器
func NewServer(port int, vizierClient *vizier.Client) *Server {
	s := &Server{
		router:        mux.NewRouter(),
		logger:        logrus.New(),
		vizierClient:  vizierClient,
		exportFactory: export.NewExporterFactory(),
		dataConverter: export.NewDataConverter(),
		port:          port,
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
	query := r.URL.Query().Get("query")
	clusterID := r.URL.Query().Get("cluster_id")

	if query == "" {
		// 如果没有指定查询，返回默认的内部指标
		s.writeDefaultMetrics(w)
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

	// 转换为Prometheus格式
	s.handleExportResponse(w, result, common.FormatPrometheusText, common.ExportOptions{})
}

// executeQuery 执行查询
func (s *Server) executeQuery(ctx context.Context, query, clusterID string) (*common.QueryResult, error) {
	s.logger.WithFields(logrus.Fields{
		"query":      query,
		"cluster_id": clusterID,
	}).Info("Executing query")

	// TODO: Implement ExecuteScriptAndExtractData method
	return nil, fmt.Errorf("ExecuteScriptAndExtractData not implemented yet")
}

// writeDefaultMetrics 写入默认指标
func (s *Server) writeDefaultMetrics(w http.ResponseWriter) {
	metrics := `# HELP observo_connector_queries_total Total number of queries executed
# TYPE observo_connector_queries_total counter
observo_connector_queries_total 0

# HELP observo_connector_up Whether the connector is up
# TYPE observo_connector_up gauge
observo_connector_up 1

# HELP observo_connector_build_info Build information
# TYPE observo_connector_build_info gauge
observo_connector_build_info{version="dev"} 1
`

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(metrics))
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
