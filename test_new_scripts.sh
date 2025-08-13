#!/bin/bash

# Test script for new Pixie scripts

echo "=== Testing New Pixie Scripts Integration ==="

# Start the server in background
echo "Starting HTTP server..."
./observo-connector --server \
  --port 8080 \
  --address localhost:50300 \
  --cluster-id demo-cluster \
  --jwt-service demo-service \
  --jwt-key c7144cb925767f9030ca5e6a2efb8bdc12e12ba0ff0bb5a88065e30dbeef6bc5 \
  --tls=true \
  --skip-verify=true \
  --verbose &

SERVER_PID=$!
echo "Server started with PID: $SERVER_PID"

# Wait for server to start
sleep 3

echo ""
echo "=== Testing New Script Endpoints ==="

# Test 1: Network Flow Graph
echo "1. Testing Network Flow Graph..."
echo "GET /api/v1/metrics?script=net_flow_graph"
curl -s "http://localhost:8080/api/v1/metrics?script=net_flow_graph" | head -20
echo ""

# Test 2: HTTP Data Tracer
echo "2. Testing HTTP Data Tracer..."
echo "GET /api/v1/metrics?script=http_data_tracer"
curl -s "http://localhost:8080/api/v1/metrics?script=http_data_tracer" | head -20
echo ""

# Test 3: HTTP Request Stats
echo "3. Testing HTTP Request Stats..."
echo "GET /api/v1/metrics?script=http_request_stats"
curl -s "http://localhost:8080/api/v1/metrics?script=http_request_stats" | head -20
echo ""

# Test 4: HTTP Performance
echo "4. Testing HTTP Performance..."
echo "GET /api/v1/metrics?script=http_performance"
curl -s "http://localhost:8080/api/v1/metrics?script=http_performance" | head -20
echo ""

# Test 5: Service Dependencies
echo "5. Testing Service Dependencies..."
echo "GET /api/v1/metrics?script=service_dependencies"
curl -s "http://localhost:8080/api/v1/metrics?script=service_dependencies" | head -20
echo ""

# Test 6: Service Performance
echo "6. Testing Service Performance..."
echo "GET /api/v1/metrics?script=service_performance"
curl -s "http://localhost:8080/api/v1/metrics?script=service_performance" | head -20
echo ""

# Test 7: Service Map
echo "7. Testing Service Map..."
echo "GET /api/v1/metrics?script=service_map"
curl -s "http://localhost:8080/api/v1/metrics?script=service_map" | head -20
echo ""

# Test script listing API
echo "8. Testing script listing..."
echo "GET /api/v1/scripts"
curl -s "http://localhost:8080/api/v1/scripts" | jq '.'
echo ""

echo "=== Test completed ==="
echo "Stopping server (PID: $SERVER_PID)..."
kill $SERVER_PID

echo "Done!"
