#!/bin/bash

# Prometheus metrics test script

echo "=== Observo Connector Prometheus Metrics Test ==="

# Start the server in background (use demo values)
echo "Starting HTTP server..."
./observo-connector --server \
  --port 8080 \
  --address localhost:50300 \
  --cluster-id demo-cluster \
  --jwt-service demo-service \
  --jwt-key c7144cb925767f9030ca5e6a2efb8bdc12e12ba0ff0bb5a88065e30dbeef6bc5 \
  --verbose &

SERVER_PID=$!
echo "Server started with PID: $SERVER_PID"

# Wait for server to start
sleep 3

echo ""
echo "=== Testing Metrics Endpoints ==="

# Test 1: Basic server metrics
echo "1. Testing basic server metrics..."
echo "GET /api/v1/metrics"
curl -s "http://localhost:8080/api/v1/metrics" | head -20
echo ""

# Test 2: Specific script metrics (resource_usage)
echo "2. Testing resource usage metrics..."
echo "GET /api/v1/metrics?script=resource_usage"
curl -s "http://localhost:8080/api/v1/metrics?script=resource_usage" | head -20
echo ""

# Test 3: HTTP overview metrics
echo "3. Testing HTTP overview metrics..."
echo "GET /api/v1/metrics?script=http_overview"
curl -s "http://localhost:8080/api/v1/metrics?script=http_overview" | head -20
echo ""

# Test 4: Network stats metrics
echo "4. Testing network stats metrics..."
echo "GET /api/v1/metrics?script=network_stats"
curl -s "http://localhost:8080/api/v1/metrics?script=network_stats" | head -20
echo ""

# Test 5: All metrics with custom parameters
echo "5. Testing all metrics with parameters..."
echo "GET /api/v1/metrics?start_time=-10m&namespace=default"
curl -s "http://localhost:8080/api/v1/metrics?start_time=-10m&namespace=default" | head -30
echo ""

echo "=== Test completed ==="
echo "Stopping server (PID: $SERVER_PID)..."
kill $SERVER_PID

echo "Done!"
