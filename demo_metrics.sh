#!/bin/bash

echo "=== Testing Prometheus Metrics Conversion ==="

# Test with a simple mock approach first
echo "Testing builtin scripts list..."
./observo-connector --list-scripts

echo ""
echo "Testing resource usage script help..."
./observo-connector --script-help resource_usage

echo ""
echo "=== Metrics Format Test ==="
echo "The metrics endpoint would be available at:"
echo "  http://localhost:8080/api/v1/metrics"
echo "  http://localhost:8080/api/v1/metrics?script=resource_usage"
echo "  http://localhost:8080/api/v1/metrics?script=http_overview"
echo ""
echo "To start the server, run:"
echo "  ./observo-connector --server --port 8080 --address <pixie-address> --cluster-id <cluster> --jwt-key <key>"
