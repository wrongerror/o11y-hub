package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestHTTPTrafficLog(t *testing.T) {
	// Create a temporary directory for test logs
	tempDir := t.TempDir()

	// Create log configuration
	config := &LogConfig{
		Directory:   tempDir,
		MaxFiles:    3,
		EnabledLogs: map[string]bool{"http_traffic": true},
		JSONFormat:  true,
		MaxFileSize: 1024 * 1024, // 1MB
	}

	// Create log manager
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	logManager := NewLogManager(config, logger)
	err := logManager.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize log manager: %v", err)
	}
	defer logManager.Close()

	// Create sample HTTP traffic log
	trafficLog := &HTTPTrafficLog{
		TimestampNS:    time.Now().UnixNano(),
		UPID:           "test-upid-123",
		TraceRole:      "client",
		RemoteAddr:     "192.168.1.100",
		RemotePort:     8080,
		LocalAddr:      "192.168.1.50",
		LocalPort:      45678,
		Encrypted:      false,
		MajorVersion:   1,
		MinorVersion:   1,
		ReqHeaders:     `{"Content-Type": "application/json", "User-Agent": "test-client"}`,
		ReqMethod:      "POST",
		ReqPath:        "/api/v1/users",
		ReqBodySize:    256,
		ReqBody:        `{"name": "test user", "email": "test@example.com"}`,
		RespHeaders:    `{"Content-Type": "application/json", "Server": "test-server"}`,
		RespStatusCode: 201,
		RespMessage:    "Created",
		RespBodySize:   128,
		RespBody:       `{"id": 12345, "status": "created"}`,
		Duration:       5000000, // 5ms in nanoseconds
		SrcNamespace:   "default",
		SrcType:        "pod",
		SrcAddress:     "192.168.1.50",
		SrcPodName:     "test-client-pod",
		SrcServiceName: "test-client-service",
		SrcNodeName:    "node-1",
		SrcOwnerName:   "test-deployment",
		SrcOwnerType:   "Deployment",
		DstNamespace:   "api",
		DstType:        "service",
		DstAddress:     "192.168.1.100",
		DstPodName:     "api-server-pod",
		DstServiceName: "api-service",
		DstNodeName:    "node-2",
		DstOwnerName:   "api-deployment",
		DstOwnerType:   "Deployment",
	}

	// Log the event
	err = logManager.LogEvent(trafficLog)
	if err != nil {
		t.Fatalf("Failed to log HTTP traffic event: %v", err)
	}

	// Verify the log file was created
	logFilePath := filepath.Join(tempDir, "http_traffic.log")
	if _, err := os.Stat(logFilePath); os.IsNotExist(err) {
		t.Fatalf("Log file was not created: %s", logFilePath)
	}

	// Read and verify log content
	content, err := os.ReadFile(logFilePath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if len(content) == 0 {
		t.Fatal("Log file is empty")
	}

	// Basic checks for JSON content
	logStr := string(content)
	if !strings.Contains(logStr, "test-upid-123") {
		t.Error("Log does not contain expected UPID")
	}
	if !strings.Contains(logStr, "POST") {
		t.Error("Log does not contain expected HTTP method")
	}
	if !strings.Contains(logStr, "/api/v1/users") {
		t.Error("Log does not contain expected path")
	}

	t.Logf("Successfully logged HTTP traffic event to: %s", logFilePath)
	t.Logf("Log content length: %d bytes", len(content))
}

func TestNewHTTPTrafficLogFromEvent(t *testing.T) {
	// Create a mock event similar to what Pixie would return
	event := map[string]interface{}{
		"upid":           "test-upid-456",
		"trace_role":     "2", // server
		"remote_addr":    "10.0.0.1",
		"remote_port":    float64(54321),
		"local_addr":     "10.0.0.2",
		"local_port":     float64(8080),
		"encrypted":      true,
		"major_version":  float64(2),
		"minor_version":  float64(0),
		"req_headers":    "Authorization: Bearer token",
		"req_method":     "GET",
		"req_path":       "/health",
		"req_body_size":  float64(0),
		"req_body":       "",
		"resp_headers":   "Content-Type: application/json",
		"resp_status":    float64(200),
		"resp_message":   "OK",
		"resp_body_size": float64(64),
		"resp_body":      `{"status": "healthy"}`,
		"latency":        float64(2500000),             // 2.5ms in nanoseconds
		"time_":          float64(1640995200000000000), // Unix timestamp in nanoseconds
	}

	// Create traffic log from event
	trafficLog := NewHTTPTrafficLogFromEvent(event)

	// Verify the parsed values
	if trafficLog.UPID != "test-upid-456" {
		t.Errorf("Expected UPID 'test-upid-456', got '%s'", trafficLog.UPID)
	}

	if trafficLog.TraceRole != "server" {
		t.Errorf("Expected trace role 'server', got '%s'", trafficLog.TraceRole)
	}

	if trafficLog.RemoteAddr != "10.0.0.1" {
		t.Errorf("Expected remote addr '10.0.0.1', got '%s'", trafficLog.RemoteAddr)
	}

	if trafficLog.RemotePort != 54321 {
		t.Errorf("Expected remote port 54321, got %d", trafficLog.RemotePort)
	}

	if !trafficLog.Encrypted {
		t.Error("Expected encrypted to be true")
	}

	if trafficLog.ReqMethod != "GET" {
		t.Errorf("Expected method 'GET', got '%s'", trafficLog.ReqMethod)
	}

	if trafficLog.RespStatusCode != 200 {
		t.Errorf("Expected status code 200, got %d", trafficLog.RespStatusCode)
	}

	if trafficLog.Duration != 2500000 {
		t.Errorf("Expected duration 2500000, got %d", trafficLog.Duration)
	}

	// Test JSON serialization
	jsonData, err := trafficLog.ToJSON()
	if err != nil {
		t.Fatalf("Failed to serialize to JSON: %v", err)
	}

	if len(jsonData) == 0 {
		t.Error("JSON serialization resulted in empty data")
	}

	t.Logf("Successfully created and serialized HTTP traffic log")
	t.Logf("JSON length: %d bytes", len(jsonData))
}
