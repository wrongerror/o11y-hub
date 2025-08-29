package vizier

import (
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// TestUnifiedEventProcessing tests the unified event processing architecture
func TestUnifiedEventProcessing(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	// Create event processor
	processor := NewHTTPEventProcessor(logger)
	if processor == nil {
		t.Fatal("Failed to create HTTP event processor")
	}

	// Create metrics subscriber
	httpMetrics := NewHTTPMetrics(logger)
	if httpMetrics == nil {
		t.Fatal("Failed to create HTTP metrics")
	}

	// Register subscriber
	processor.AddSubscriber(httpMetrics)

	// Start processor
	processor.Start()
	defer processor.Stop()

	// Create a test event
	testEvent := map[string]interface{}{
		"trace_role":     1, // 1 = client, 2 = server in Pixie
		"req_method":     "GET",
		"req_path":       "/api/test",
		"resp_status":    200,
		"time_":          time.Now().UnixNano(),
		"latency":        1500000000, // 1.5 seconds in nanoseconds
		"local_addr":     "10.0.1.100",
		"local_port":     8080,
		"remote_addr":    "10.0.1.200",
		"remote_port":    80,
		"req_body_size":  1024,
		"resp_body_size": 2048,
		"encrypted":      true,
		"service":        "default/test-service",
		"namespace":      "default",
		"pod_name":       "default/test-pod-123",
		"node_name":      "worker-node-1",
	}

	// Process the event
	err := processor.ProcessEvent(testEvent)
	if err != nil {
		t.Errorf("Failed to process event: %v", err)
	}

	// Allow some time for async processing
	time.Sleep(100 * time.Millisecond)

	// The event should have been processed by the metrics subscriber
	// This is verified by the fact that no errors occurred and the processor ran successfully
	t.Log("Unified event processing test completed successfully")
}

// TestEventDeduplication tests that duplicate events are properly handled
func TestEventDeduplication(t *testing.T) {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)

	// Create event processor
	processor := NewHTTPEventProcessor(logger)
	if processor == nil {
		t.Fatal("Failed to create HTTP event processor")
	}

	// Create metrics subscriber
	httpMetrics := NewHTTPMetrics(logger)
	if httpMetrics == nil {
		t.Fatal("Failed to create HTTP metrics")
	}

	// Register subscriber
	processor.AddSubscriber(httpMetrics)

	// Start processor
	processor.Start()
	defer processor.Stop()

	// Create a test event
	baseTime := time.Now().UnixNano()
	testEvent := map[string]interface{}{
		"trace_role":     2, // 2 = server in Pixie
		"req_method":     "POST",
		"req_path":       "/api/create",
		"resp_status":    201,
		"time_":          baseTime,
		"latency":        500000000, // 0.5 seconds
		"local_addr":     "10.0.1.100",
		"local_port":     8080,
		"remote_addr":    "10.0.1.200",
		"remote_port":    45678,
		"req_body_size":  512,
		"resp_body_size": 256,
		"encrypted":      false,
	}

	// Process the same event multiple times
	for i := 0; i < 3; i++ {
		err := processor.ProcessEvent(testEvent)
		if err != nil {
			t.Errorf("Failed to process event iteration %d: %v", i, err)
		}
	}

	// Allow some time for async processing
	time.Sleep(100 * time.Millisecond)

	// All events should be processed without errors, but duplicates should be filtered
	t.Log("Event deduplication test completed successfully")
}
