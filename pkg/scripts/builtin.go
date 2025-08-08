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
			if param.Required {
				return "", fmt.Errorf("required parameter %s not provided", param.Name)
			}
			value = param.DefaultValue
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
}
