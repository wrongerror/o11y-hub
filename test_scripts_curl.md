# Pixie Scripts Testing with curl

This document provides curl commands to test all the new Pixie scripts that have been added to the observo-connector.

## Prerequisites

1. Start the observo-connector server:
```bash
./observo-connector --server \
  --port 8080 \
  --address localhost:50300 \
  --cluster-id demo-cluster \
  --jwt-service demo-service \
  --jwt-key c7144cb925767f9030ca5e6a2efb8bdc12e12ba0ff0bb5a88065e30dbeef6bc5 \
  --tls=true \
  --skip-verify=true \
  --verbose
```

## Basic Server Health Check

```bash
# Check server health
curl "http://localhost:8080/api/v1/health"

# Get basic connector metrics
curl "http://localhost:8080/api/v1/metrics"
```

## Original Scripts Testing

### 1. HTTP Overview
```bash
curl "http://localhost:8080/api/v1/metrics?script=http_overview"
```

### 2. Resource Usage
```bash
curl "http://localhost:8080/api/v1/metrics?script=resource_usage"
```

### 3. Network Stats
```bash
curl "http://localhost:8080/api/v1/metrics?script=network_stats"
```

### 4. Error Analysis
```bash
curl "http://localhost:8080/api/v1/metrics?script=error_analysis"
```

## New Scripts Testing

### 5. Network Flow Graph
```bash
# Basic network flow graph
curl "http://localhost:8080/api/v1/metrics?script=net_flow_graph"

# With namespace filter
curl "http://localhost:8080/api/v1/metrics?script=net_flow_graph&namespace=default"

# With custom time range and throughput threshold
curl "http://localhost:8080/api/v1/metrics?script=net_flow_graph&start_time=-10m&min_throughput=0.05"
```

### 6. HTTP Data Tracer
```bash
# Basic HTTP data tracer
curl "http://localhost:8080/api/v1/metrics?script=http_data_tracer"

# Filter by service
curl "http://localhost:8080/api/v1/metrics?script=http_data_tracer&service=my-service"

# Filter by path pattern and limit results
curl "http://localhost:8080/api/v1/metrics?script=http_data_tracer&path_filter=/api&limit=50"
```

### 7. HTTP Request Stats
```bash
# Basic HTTP request statistics
curl "http://localhost:8080/api/v1/metrics?script=http_request_stats"

# With namespace filter
curl "http://localhost:8080/api/v1/metrics?script=http_request_stats&namespace=default"

# Show top 30 endpoints
curl "http://localhost:8080/api/v1/metrics?script=http_request_stats&top_endpoints=30"
```

### 8. HTTP Performance Analysis
```bash
# Basic HTTP performance analysis
curl "http://localhost:8080/api/v1/metrics?script=http_performance"

# Filter by specific service
curl "http://localhost:8080/api/v1/metrics?script=http_performance&service=my-service"

# Custom slow threshold and limits
curl "http://localhost:8080/api/v1/metrics?script=http_performance&slow_threshold=500&slow_limit=100"
```

### 9. Service Dependencies
```bash
# Basic service dependencies
curl "http://localhost:8080/api/v1/metrics?script=service_dependencies"

# With namespace filter
curl "http://localhost:8080/api/v1/metrics?script=service_dependencies&namespace=default"

# Custom minimum bytes threshold
curl "http://localhost:8080/api/v1/metrics?script=service_dependencies&min_bytes=2048"
```

### 10. Service Performance Overview
```bash
# Basic service performance overview
curl "http://localhost:8080/api/v1/metrics?script=service_performance"

# With namespace filter
curl "http://localhost:8080/api/v1/metrics?script=service_performance&namespace=default"

# Show top 20 services
curl "http://localhost:8080/api/v1/metrics?script=service_performance&top_count=20"
```

### 11. Service Map
```bash
# Basic service map
curl "http://localhost:8080/api/v1/metrics?script=service_map"

# With namespace filter
curl "http://localhost:8080/api/v1/metrics?script=service_map&namespace=default"

# Custom minimum requests threshold
curl "http://localhost:8080/api/v1/metrics?script=service_map&min_requests=5"
```

## Script Information APIs

### List All Available Scripts
```bash
curl "http://localhost:8080/api/v1/scripts"
```

### Get Specific Script Information
```bash
curl "http://localhost:8080/api/v1/scripts/http_overview"
curl "http://localhost:8080/api/v1/scripts/net_flow_graph"
curl "http://localhost:8080/api/v1/scripts/service_performance"
```

## Advanced Testing with Parameters

### Time Range Testing
```bash
# Last 1 minute
curl "http://localhost:8080/api/v1/metrics?script=http_overview&start_time=-1m"

# Last 10 minutes
curl "http://localhost:8080/api/v1/metrics?script=http_overview&start_time=-10m"

# Last 30 minutes
curl "http://localhost:8080/api/v1/metrics?script=http_overview&start_time=-30m"
```

### Namespace Filtering
```bash
# Filter by specific namespace
curl "http://localhost:8080/api/v1/metrics?script=resource_usage&namespace=kube-system"
curl "http://localhost:8080/api/v1/metrics?script=http_overview&namespace=default"
```

### Combined Parameters
```bash
# HTTP performance with multiple parameters
curl "http://localhost:8080/api/v1/metrics?script=http_performance&start_time=-15m&service=my-service&slow_threshold=1000"

# Service dependencies with time and namespace
curl "http://localhost:8080/api/v1/metrics?script=service_dependencies&start_time=-10m&namespace=default&min_bytes=1024"
```

## Output Formats

### Raw Prometheus Metrics (Default)
```bash
curl "http://localhost:8080/api/v1/metrics?script=http_overview"
```

### Pretty Print with Headers
```bash
curl -v "http://localhost:8080/api/v1/metrics?script=http_overview"
```

### Save to File
```bash
curl "http://localhost:8080/api/v1/metrics?script=service_map" > service_map_metrics.txt
```

### Filter Specific Metrics
```bash
# Get only pixie_http_requests_total metrics
curl "http://localhost:8080/api/v1/metrics?script=http_overview" | grep "pixie_http_requests_total"

# Get all histogram bucket metrics
curl "http://localhost:8080/api/v1/metrics?script=http_overview" | grep "_bucket"
```

## Error Testing

### Test Non-existent Script
```bash
curl "http://localhost:8080/api/v1/metrics?script=non_existent_script"
```

### Test Invalid Parameters
```bash
curl "http://localhost:8080/api/v1/metrics?script=http_performance&slow_threshold=invalid"
```

## Monitoring Script Performance

### Time Script Execution
```bash
time curl "http://localhost:8080/api/v1/metrics?script=service_performance"
```

### Check Response Size
```bash
curl -w "Total: %{size_download} bytes\n" "http://localhost:8080/api/v1/metrics?script=http_overview" > /dev/null
```

## Troubleshooting

### Check Server Logs
```bash
# Server should be running with --verbose flag to see detailed logs
```

### Verify Pixie Connection
```bash
curl "http://localhost:8080/api/v1/health?cluster_id=demo-cluster"
```

### Test Basic Connectivity
```bash
curl -I "http://localhost:8080/api/v1/health"
```
