package logging

import (
	"encoding/json"
	"time"
)

// HTTPTrafficLog represents a complete HTTP traffic log entry
type HTTPTrafficLog struct {
	// Basic timing and identification
	TimestampNS int64  `json:"timestamp_ns"`
	UPID        string `json:"upid"`
	TraceRole   string `json:"trace_role"` // "client" or "server"

	// Network information
	RemoteAddr string `json:"remote_addr"`
	RemotePort uint16 `json:"remote_port"`
	LocalAddr  string `json:"local_addr"`
	LocalPort  uint16 `json:"local_port"`
	Encrypted  bool   `json:"encrypted"`

	// HTTP protocol information
	MajorVersion uint8 `json:"major_version"`
	MinorVersion uint8 `json:"minor_version"`

	// Request information
	ReqHeaders  string `json:"req_headers"`
	ReqMethod   string `json:"req_method"`
	ReqPath     string `json:"req_path"`
	ReqBodySize int64  `json:"req_body_size"`
	ReqBody     string `json:"req_body"`

	// Response information
	RespHeaders    string `json:"resp_headers"`
	RespStatusCode uint16 `json:"resp_status_code"`
	RespMessage    string `json:"resp_message"`
	RespBodySize   int64  `json:"resp_body_size"`
	RespBody       string `json:"resp_body"`

	// Duration in nanoseconds
	Duration int64 `json:"duration"`

	// Source endpoint metadata
	SrcNamespace   string `json:"src_namespace"`
	SrcType        string `json:"src_type"`
	SrcAddress     string `json:"src_address"`
	SrcPodName     string `json:"src_pod_name"`
	SrcServiceName string `json:"src_service_name"`
	SrcNodeName    string `json:"src_node_name"`
	SrcOwnerName   string `json:"src_owner_name"`
	SrcOwnerType   string `json:"src_owner_type"`
	SrcContainer   string `json:"src_container"`

	// Destination endpoint metadata
	DstNamespace   string `json:"dst_namespace"`
	DstType        string `json:"dst_type"`
	DstAddress     string `json:"dst_address"`
	DstPodName     string `json:"dst_pod_name"`
	DstServiceName string `json:"dst_service_name"`
	DstNodeName    string `json:"dst_node_name"`
	DstOwnerName   string `json:"dst_owner_name"`
	DstOwnerType   string `json:"dst_owner_type"`
}

// GetLogType implements LoggableEvent interface
func (h *HTTPTrafficLog) GetLogType() string {
	return "http_traffic"
}

// ToJSON implements LoggableEvent interface
func (h *HTTPTrafficLog) ToJSON() ([]byte, error) {
	return json.Marshal(h)
}

// CreateEventID creates a unique identifier for deduplication based on core fields
func (h *HTTPTrafficLog) CreateEventID() string {
	// Use combination of time, trace_role, upid, and response details for uniqueness
	// This matches the logic from http_metrics.go
	return h.UPID + ":" + h.TraceRole + ":" +
		string(rune(h.RespStatusCode)) + ":" +
		string(rune(h.Duration)) + ":" +
		string(rune(h.TimestampNS))
}

// NewHTTPTrafficLogFromEvent creates an HTTPTrafficLog from a Pixie event
func NewHTTPTrafficLogFromEvent(event map[string]interface{}) *HTTPTrafficLog {
	log := &HTTPTrafficLog{
		TimestampNS: time.Now().UnixNano(),
	}

	// Helper function to safely extract string values
	getStringValue := func(key, defaultValue string) string {
		if val, ok := event[key]; ok {
			if str, ok := val.(string); ok {
				return str
			}
		}
		return defaultValue
	}

	// Helper function to safely extract float64 values and convert to appropriate types
	getFloat64Value := func(key string) float64 {
		if val, ok := event[key]; ok {
			switch v := val.(type) {
			case float64:
				return v
			case int:
				return float64(v)
			case int64:
				return float64(v)
			}
		}
		return 0
	}

	// Extract basic fields
	log.UPID = getStringValue("upid", "")

	// Extract trace role
	traceRoleRaw := getStringValue("trace_role", "1")
	if traceRoleRaw == "2" {
		log.TraceRole = "server"
	} else {
		log.TraceRole = "client"
	}

	// Extract network information
	log.RemoteAddr = getStringValue("remote_addr", "")
	log.RemotePort = uint16(getFloat64Value("remote_port"))
	log.LocalAddr = getStringValue("local_addr", "")
	log.LocalPort = uint16(getFloat64Value("local_port"))

	// Extract encryption status
	if encryptedVal, ok := event["encrypted"].(bool); ok {
		log.Encrypted = encryptedVal
	}

	// Extract HTTP version (if available)
	log.MajorVersion = uint8(getFloat64Value("major_version"))
	log.MinorVersion = uint8(getFloat64Value("minor_version"))

	// Extract request information
	log.ReqHeaders = getStringValue("req_headers", "")
	log.ReqMethod = getStringValue("req_method", "")
	log.ReqPath = getStringValue("req_path", "")
	log.ReqBodySize = int64(getFloat64Value("req_body_size"))
	log.ReqBody = getStringValue("req_body", "")

	// Extract response information
	log.RespHeaders = getStringValue("resp_headers", "")
	log.RespStatusCode = uint16(getFloat64Value("resp_status"))
	log.RespMessage = getStringValue("resp_message", "")
	log.RespBodySize = int64(getFloat64Value("resp_body_size"))
	log.RespBody = getStringValue("resp_body", "")

	// Extract duration (convert from nanoseconds)
	log.Duration = int64(getFloat64Value("latency"))

	// Set timestamp from event if available
	if timeVal := getFloat64Value("time_"); timeVal > 0 {
		log.TimestampNS = int64(timeVal)
	}

	return log
}
