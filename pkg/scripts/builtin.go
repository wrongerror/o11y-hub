package scripts

import (
	"fmt"
	"strings"
)

// ScriptTemplate represents a parameterized PxL script
type ScriptTemplate struct {
	Name        string
	Description string
	Category    string
	Template    string
	Parameters  []Parameter
}

// Parameter represents a script parameter
type Parameter struct {
	Name         string
	Type         string
	Description  string
	DefaultValue string
	Required     bool
}

// Execute renders the script template with provided parameters
func (st *ScriptTemplate) Execute(params map[string]string) (string, error) {
	script := st.Template

	// Replace parameters in template
	for _, param := range st.Parameters {
		placeholder := fmt.Sprintf("{{%s}}", param.Name)
		value := params[param.Name]

		// Use default value if parameter not provided
		if value == "" {
			value = param.DefaultValue
		}

		// Check if required parameter is still empty after applying default
		if value == "" && param.Required {
			return "", fmt.Errorf("required parameter %s not provided", param.Name)
		}

		script = strings.ReplaceAll(script, placeholder, value)
	}

	return script, nil
}

// BuiltinScripts contains all predefined monitoring scripts
var BuiltinScripts = map[string]*ScriptTemplate{
	"http_overview": {
		Name:        "HTTP Overview",
		Description: "Monitor HTTP request metrics including latency, error rate, and throughput",
		Category:    "Application",
		Template: `import px

df = px.DataFrame('http_events', start_time='{{start_time}}')
df.service = df.ctx['service']
df.error = df.resp_status >= 400

# Calculate metrics by service
df = df.groupby(['service']).agg(
    total_requests=('latency', px.count),
    avg_latency=('latency', px.mean),
    error_rate=('error', px.mean)
)

px.display(df)`,
		Parameters: []Parameter{
			{Name: "start_time", Type: "string", Description: "Start time for the query", DefaultValue: "-5m", Required: true},
		},
	},

	"resource_usage": {
		Name:        "Resource Usage",
		Description: "Monitor CPU and memory usage by pod and namespace",
		Category:    "Infrastructure",
		Template: `import px

df = px.DataFrame('process_stats', start_time='{{start_time}}')
df.namespace = df.ctx['namespace']
df.pod = df.ctx['pod']
df.container = df.ctx['container_name']

# Calculate resource metrics
df = df.groupby(['namespace', 'pod', 'container']).agg(
    cpu_usage=('cpu_utime_ns', px.mean),
    memory_rss=('rss_bytes', px.mean),
    memory_vsize=('vsize_bytes', px.mean),
    read_bytes=('read_bytes', px.mean),
    write_bytes=('write_bytes', px.mean)
)

# Convert to readable format
df.memory_rss_mb = df.memory_rss / (1024 * 1024)
df.memory_vsize_mb = df.memory_vsize / (1024 * 1024)

px.display(df[['namespace', 'pod', 'container', 'cpu_usage', 'memory_rss_mb', 'memory_vsize_mb']], 'Resource Usage by Pod')`,
		Parameters: []Parameter{
			{Name: "start_time", Type: "string", Description: "Start time for the query", DefaultValue: "-5m", Required: true},
		},
	},

	"network_stats": {
		Name:        "Network Statistics",
		Description: "Monitor network traffic and statistics by pod",
		Category:    "Network",
		Template: `import px

df = px.DataFrame('network_stats', start_time='{{start_time}}')
df.pod = df.ctx['pod']
df.namespace = df.ctx['namespace']

# Calculate network metrics
df = df.groupby(['namespace', 'pod']).agg(
    rx_bytes=('rx_bytes', px.mean),
    tx_bytes=('tx_bytes', px.mean),
    rx_packets=('rx_packets', px.mean),
    tx_packets=('tx_packets', px.mean),
    rx_errors=('rx_errors', px.mean),
    tx_errors=('tx_errors', px.mean)
)

# Convert to readable format
df.rx_bytes_mb = df.rx_bytes / (1024 * 1024)
df.tx_bytes_mb = df.tx_bytes / (1024 * 1024)

px.display(df[['namespace', 'pod', 'rx_bytes_mb', 'tx_bytes_mb', 'rx_packets', 'tx_packets']], 'Network Statistics')`,
		Parameters: []Parameter{
			{Name: "start_time", Type: "string", Description: "Start time for the query", DefaultValue: "-5m", Required: true},
		},
	},

	"pod_overview": {
		Name:        "Pod Overview",
		Description: "Comprehensive overview of a specific pod including resources and network",
		Category:    "Infrastructure",
		Template: `import px

# Get process stats for the pod
proc_df = px.DataFrame('process_stats', start_time='{{start_time}}')
proc_df = proc_df[proc_df.ctx['pod'] == '{{pod_name}}']
proc_df.container = proc_df.ctx['container_name']

proc_stats = proc_df.groupby(['container']).agg(
    cpu_usage=('cpu_utime_ns', px.mean),
    memory_rss=('rss_bytes', px.mean),
    memory_vsize=('vsize_bytes', px.mean)
)

# Get network stats for the pod
net_df = px.DataFrame('network_stats', start_time='{{start_time}}')
net_df = net_df[net_df.ctx['pod'] == '{{pod_name}}']

net_stats = net_df.groupby(['time_']).agg(
    rx_bytes=('rx_bytes', px.mean),
    tx_bytes=('tx_bytes', px.mean)
)

px.display(proc_stats, 'Pod Resource Usage')
px.display(net_stats.head(10), 'Pod Network Traffic')`,
		Parameters: []Parameter{
			{Name: "start_time", Type: "string", Description: "Start time for the query", DefaultValue: "-5m", Required: true},
			{Name: "pod_name", Type: "string", Description: "Name of the pod to analyze", DefaultValue: "", Required: true},
		},
	},

	"error_analysis": {
		Name:        "Error Analysis",
		Description: "Analyze HTTP errors and failure patterns",
		Category:    "Application",
		Template: `import px

df = px.DataFrame('http_events', start_time='{{start_time}}')
df.service = df.ctx['service']
df.pod = df.ctx['pod']

# Filter to only error responses
df = df[df.resp_status >= 400]

# Analyze error patterns
error_summary = df.groupby(['service', 'resp_status']).agg(
    error_count=('latency', px.count),
    avg_latency=('latency', px.mean),
    req_paths=('req_path', lambda x: px.any(x))
)

error_summary.avg_latency = px.DurationNanos(error_summary.avg_latency)

px.display(error_summary, 'Error Analysis by Service and Status Code')

# Get recent error details
recent_errors = df[['time_', 'service', 'pod', 'req_method', 'req_path', 'resp_status', 'latency']].head(20)
px.display(recent_errors, 'Recent Error Details')`,
		Parameters: []Parameter{
			{Name: "start_time", Type: "string", Description: "Start time for the query", DefaultValue: "-10m", Required: true},
		},
	},

	// Network Flow Graph
	"net_flow_graph": {
		Name:        "Network Flow Graph",
		Description: "Visualize network connections and traffic flow between services",
		Category:    "Network",
		Template: `import px

# Get network connection data
df = px.DataFrame('conn_stats', start_time='{{start_time}}')

# Extract source and destination information
df.src_service = df.ctx['service']
df.src_pod = df.ctx['pod'] 
df.dest_service = px.Service(px.nslookup(df.remote_addr))
df.dest_port = df.remote_port

# Aggregate connection stats
connections = df.groupby(['src_service', 'dest_service', 'dest_port']).agg(
    bytes_sent=('bytes_sent', px.sum),
    bytes_recv=('bytes_recv', px.sum),
    conn_open=('conn_open', px.sum),
    conn_close=('conn_close', px.sum)
)

# Calculate throughput rates
connections.throughput_mbps = (connections.bytes_sent + connections.bytes_recv) / (1024 * 1024) / 300  # 5min window
connections.active_connections = connections.conn_open - connections.conn_close

px.display(connections, 'Network Flow Graph')`,
		Parameters: []Parameter{
			{Name: "start_time", Type: "string", Description: "Start time for the query", DefaultValue: "-5m", Required: true},
			{Name: "namespace", Type: "string", Description: "Filter by namespace (optional)", DefaultValue: "", Required: false},
			{Name: "min_throughput", Type: "float", Description: "Minimum throughput in Mbps", DefaultValue: "0.01", Required: false},
		},
	},

	// HTTP Data Tracer
	"http_data_tracer": {
		Name:        "HTTP Data Tracer",
		Description: "Trace HTTP requests and responses with detailed data inspection",
		Category:    "Application",
		Template: `import px

df = px.DataFrame('http_events', start_time='{{start_time}}')

# Extract detailed HTTP information
df.service = df.ctx['service']
df.pod = df.ctx['pod']
df.trace_role = df.trace_role
df.duration_ms = df.latency / 1000000  # Convert to milliseconds

# Get request/response details
http_traces = df[['time_', 'service', 'pod', 'trace_role', 'req_method', 'req_path', 
                 'resp_status', 'duration_ms', 'req_headers', 'resp_headers']].head(100)

# Calculate summary statistics
summary = df.groupby(['service', 'req_method', 'resp_status']).agg(
    request_count=('latency', px.count),
    avg_duration_ms=('duration_ms', px.mean),
    p50_duration_ms=('duration_ms', px.quantiles),
    p90_duration_ms=('duration_ms', px.quantiles),
    p99_duration_ms=('duration_ms', px.quantiles)
)

px.display(http_traces, 'HTTP Request Traces')
px.display(summary, 'HTTP Summary Statistics')`,
		Parameters: []Parameter{
			{Name: "start_time", Type: "string", Description: "Start time for the query", DefaultValue: "-5m", Required: true},
			{Name: "service", Type: "string", Description: "Filter by service name (optional)", DefaultValue: "", Required: false},
			{Name: "path_filter", Type: "string", Description: "Filter by request path pattern (optional)", DefaultValue: "", Required: false},
			{Name: "limit", Type: "int", Description: "Maximum number of traces to display", DefaultValue: "100", Required: false},
		},
	},

	// HTTP Request Stats
	"http_request_stats": {
		Name:        "HTTP Request Statistics",
		Description: "Comprehensive HTTP request statistics and performance metrics",
		Category:    "Application",
		Template: `import px

df = px.DataFrame('http_events', start_time='{{start_time}}')

df.service = df.ctx['service']
df.pod = df.ctx['pod']
df.request_size_kb = df.req_body_size / 1024
df.response_size_kb = df.resp_body_size / 1024
df.duration_ms = df.latency / 1000000
df.is_error = df.resp_status >= 400

# HTTP method statistics
method_stats = df.groupby(['service', 'req_method']).agg(
    request_count=('latency', px.count),
    avg_duration_ms=('duration_ms', px.mean),
    error_rate=('is_error', px.mean),
    avg_request_size_kb=('request_size_kb', px.mean),
    avg_response_size_kb=('response_size_kb', px.mean)
)

# Status code distribution
status_stats = df.groupby(['service', 'resp_status']).agg(
    count=('latency', px.count),
    avg_duration_ms=('duration_ms', px.mean)
)

# Top endpoints by request count
endpoint_stats = df.groupby(['service', 'req_path']).agg(
    request_count=('latency', px.count),
    avg_duration_ms=('duration_ms', px.mean),
    error_rate=('is_error', px.mean)
).head(20)

px.display(method_stats, 'HTTP Method Statistics')
px.display(status_stats, 'HTTP Status Code Distribution')
px.display(endpoint_stats, 'Top Endpoints')`,
		Parameters: []Parameter{
			{Name: "start_time", Type: "string", Description: "Start time for the query", DefaultValue: "-5m", Required: true},
			{Name: "namespace", Type: "string", Description: "Filter by namespace (optional)", DefaultValue: "", Required: false},
			{Name: "top_endpoints", Type: "int", Description: "Number of top endpoints to show", DefaultValue: "20", Required: false},
		},
	},

	// HTTP Performance Analysis
	"http_performance": {
		Name:        "HTTP Performance Analysis",
		Description: "Analyze HTTP performance patterns and identify bottlenecks",
		Category:    "Application",
		Template: `import px

df = px.DataFrame('http_events', start_time='{{start_time}}')

df.service = df.ctx['service']
df.duration_ms = df.latency / 1000000
df.is_slow = df.duration_ms > 1000
df.is_error = df.resp_status >= 400

# Performance metrics by service
perf_by_service = df.groupby(['service']).agg(
    total_requests=('latency', px.count),
    avg_duration_ms=('duration_ms', px.mean),
    p50_duration_ms=('duration_ms', px.quantiles),
    p90_duration_ms=('duration_ms', px.quantiles),
    p99_duration_ms=('duration_ms', px.quantiles),
    slow_request_rate=('is_slow', px.mean),
    error_rate=('is_error', px.mean)
)

# Slow requests analysis
slow_requests = df[df.is_slow][['time_', 'service', 'req_method', 'req_path', 
                                'resp_status', 'duration_ms']].head(50)

# Time series performance trend
time_series = df.groupby(['service', px.bin(df.time_, px.seconds(30))]).agg(
    request_rate=('latency', px.count),
    avg_duration_ms=('duration_ms', px.mean),
    error_rate=('is_error', px.mean)
)

px.display(perf_by_service, 'Performance Metrics by Service')
px.display(slow_requests, 'Slow Requests')
px.display(time_series.tail(50), 'Performance Trend')`,
		Parameters: []Parameter{
			{Name: "start_time", Type: "string", Description: "Start time for the query", DefaultValue: "-5m", Required: true},
			{Name: "service", Type: "string", Description: "Filter by service name (optional)", DefaultValue: "", Required: false},
			{Name: "slow_threshold", Type: "float", Description: "Slow request threshold in ms", DefaultValue: "1000", Required: false},
			{Name: "slow_limit", Type: "int", Description: "Number of slow requests to show", DefaultValue: "50", Required: false},
			{Name: "bin_size", Type: "int", Description: "Time series bin size in seconds", DefaultValue: "30", Required: false},
		},
	},

	// Service Dependencies
	"service_dependencies": {
		Name:        "Service Dependencies",
		Description: "Map service dependencies and communication patterns",
		Category:    "Service Mesh",
		Template: `import px

# Get HTTP connections between services
http_df = px.DataFrame('http_events', start_time='{{start_time}}')

http_df.src_service = http_df.ctx['service']
http_df.dest_service = px.Service(http_df.remote_addr)

# HTTP service dependencies
http_deps = http_df.groupby(['src_service', 'dest_service']).agg(
    request_count=('latency', px.count),
    avg_latency_ms=('latency', px.mean) / 1000000,
    error_rate=('resp_status', lambda x: px.mean(x >= 400)),
    protocol=('remote_port', lambda x: 'HTTP')
)

# Get network connections for non-HTTP services
conn_df = px.DataFrame('conn_stats', start_time='{{start_time}}')

conn_df.src_service = conn_df.ctx['service']
conn_df.dest_service = px.Service(px.nslookup(conn_df.remote_addr))
conn_df.dest_port = conn_df.remote_port

# Network service dependencies  
net_deps = conn_df.groupby(['src_service', 'dest_service', 'dest_port']).agg(
    bytes_sent=('bytes_sent', px.sum),
    bytes_recv=('bytes_recv', px.sum),
    conn_count=('conn_open', px.sum),
    protocol=('dest_port', lambda x: 'TCP')
)

# Filter out low-traffic connections
net_deps = net_deps[net_deps.bytes_sent + net_deps.bytes_recv > 1024]

px.display(http_deps, 'HTTP Service Dependencies')
px.display(net_deps, 'Network Service Dependencies')`,
		Parameters: []Parameter{
			{Name: "start_time", Type: "string", Description: "Start time for the query", DefaultValue: "-5m", Required: true},
			{Name: "namespace", Type: "string", Description: "Filter by namespace (optional)", DefaultValue: "", Required: false},
			{Name: "min_bytes", Type: "int", Description: "Minimum bytes threshold", DefaultValue: "1024", Required: false},
		},
	},

	// Service Performance Overview
	"service_performance": {
		Name:        "Service Performance Overview",
		Description: "Comprehensive service performance metrics and health indicators",
		Category:    "Service Mesh",
		Template: `import px

# Get HTTP metrics
http_df = px.DataFrame('http_events', start_time='{{start_time}}')

http_df.service = http_df.ctx['service']
http_df.duration_ms = http_df.latency / 1000000
http_df.is_error = http_df.resp_status >= 400

# HTTP performance metrics
http_metrics = http_df.groupby(['service']).agg(
    http_request_count=('latency', px.count),
    http_avg_latency_ms=('duration_ms', px.mean),
    http_max_latency_ms=('duration_ms', px.max),
    http_min_latency_ms=('duration_ms', px.min),
    http_error_rate=('is_error', px.mean)
)

# Calculate RPS (requests per second over 5min = 300 seconds)
http_metrics.http_rps = http_metrics.http_request_count / 300

# Get resource usage metrics
proc_df = px.DataFrame('process_stats', start_time='{{start_time}}')

proc_df.service = proc_df.ctx['service']

resource_metrics = proc_df.groupby(['service']).agg(
    avg_cpu_usage=('cpu_utime_ns', px.mean),
    avg_memory_bytes=('rss_bytes', px.mean),
    max_memory_bytes=('rss_bytes', px.max)
)

# Convert bytes to MB
resource_metrics.avg_memory_mb = resource_metrics.avg_memory_bytes / (1024 * 1024)
resource_metrics.max_memory_mb = resource_metrics.max_memory_bytes / (1024 * 1024)

# Combine metrics
service_health = http_metrics.merge(resource_metrics, 
                                   left_on=['service'], 
                                   right_on=['service'], 
                                   how='outer',
                                   suffixes=['', '_r'])

px.display(service_health, 'Service Performance Overview')

# Display individual metrics tables
px.display(http_metrics, 'HTTP Performance Metrics')
px.display(resource_metrics, 'Resource Usage Metrics')`,
		Parameters: []Parameter{
			{Name: "start_time", Type: "string", Description: "Start time for the query", DefaultValue: "-5m", Required: true},
			{Name: "namespace", Type: "string", Description: "Filter by namespace (optional)", DefaultValue: "", Required: false},
			{Name: "top_count", Type: "int", Description: "Number of top services to show", DefaultValue: "10", Required: false},
		},
	},

	// Service Map
	"service_map": {
		Name:        "Service Map",
		Description: "Generate a service map showing all service interactions",
		Category:    "Service Mesh",
		Template: `import px

# Get all HTTP connections
df = px.DataFrame('http_events', start_time='{{start_time}}')

df.src_service = df.ctx['service']
df.src_pod = df.ctx['pod']
df.dest_service = px.Service(df.remote_addr)
df.duration_ms = df.latency / 1000000
df.is_error = df.resp_status >= 400

# Service-to-service communication
service_map = df.groupby(['src_service', 'dest_service']).agg(
    request_count=('latency', px.count),
    request_rate_per_sec=('latency', px.count) / 300,
    avg_latency_ms=('duration_ms', px.mean),
    p90_latency_ms=('duration_ms', px.quantiles),
    error_rate=('is_error', px.mean),
    total_errors=('is_error', px.sum)
)

# Filter out very low traffic connections
service_map = service_map[service_map.request_count >= 10]

# Get unique services
src_services = df.groupby(['src_service']).agg(
    outbound_requests=('latency', px.count),
    avg_outbound_latency=('duration_ms', px.mean)
)

dest_services = df.groupby(['dest_service']).agg(
    inbound_requests=('latency', px.count),
    avg_inbound_latency=('duration_ms', px.mean)
)

px.display(service_map, 'Service Communication Map')
px.display(src_services, 'Services - Outbound Traffic')
px.display(dest_services, 'Services - Inbound Traffic')`,
		Parameters: []Parameter{
			{Name: "start_time", Type: "string", Description: "Start time for the query", DefaultValue: "-5m", Required: true},
			{Name: "namespace", Type: "string", Description: "Filter by namespace (optional)", DefaultValue: "", Required: false},
			{Name: "min_requests", Type: "int", Description: "Minimum request count threshold", DefaultValue: "10", Required: false},
		},
	},
}
