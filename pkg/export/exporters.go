package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/wrongerror/observo-connector/pkg/common"
)

// DataExporter 数据导出器接口
type DataExporter interface {
	Export(data *common.QueryResult, opts common.ExportOptions) ([]byte, error)
	ContentType() string
}

// PrometheusExporter Prometheus格式导出器
type PrometheusExporter struct{}

// Export 导出为Prometheus文本格式
func (p *PrometheusExporter) Export(data *common.QueryResult, opts common.ExportOptions) ([]byte, error) {
	var lines []string

	for _, metric := range data.Metrics {
		// 添加HELP注释
		if metric.Description != "" {
			lines = append(lines, fmt.Sprintf("# HELP %s %s", metric.Name, metric.Description))
		}

		// 添加TYPE注释
		lines = append(lines, fmt.Sprintf("# TYPE %s %s", metric.Name, metric.Type))

		// 构建标签字符串
		var labelPairs []string
		for k, v := range metric.Labels {
			labelPairs = append(labelPairs, fmt.Sprintf(`%s="%s"`, k, v))
		}

		labelsStr := ""
		if len(labelPairs) > 0 {
			labelsStr = fmt.Sprintf("{%s}", strings.Join(labelPairs, ","))
		}

		// 添加指标行
		timestamp := metric.Timestamp.UnixMilli()
		lines = append(lines, fmt.Sprintf("%s%s %g %d", metric.Name, labelsStr, metric.Value, timestamp))
	}

	return []byte(strings.Join(lines, "\n")), nil
}

func (p *PrometheusExporter) ContentType() string {
	return "text/plain; version=0.0.4; charset=utf-8"
}

// JSONExporter JSON格式导出器
type JSONExporter struct{}

func (j *JSONExporter) Export(data *common.QueryResult, opts common.ExportOptions) ([]byte, error) {
	return json.MarshalIndent(data, "", "  ")
}

func (j *JSONExporter) ContentType() string {
	return "application/json"
}

// CSVExporter CSV格式导出器
type CSVExporter struct{}

func (c *CSVExporter) Export(data *common.QueryResult, opts common.ExportOptions) ([]byte, error) {
	var buf strings.Builder
	writer := csv.NewWriter(&buf)

	// 写入表头
	if err := writer.Write(data.Columns); err != nil {
		return nil, fmt.Errorf("failed to write CSV header: %w", err)
	}

	// 写入数据行
	for _, row := range data.Data {
		var record []string
		for _, col := range data.Columns {
			value := row[col]
			record = append(record, c.formatValue(value))
		}
		if err := writer.Write(record); err != nil {
			return nil, fmt.Errorf("failed to write CSV row: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("CSV writer error: %w", err)
	}

	return []byte(buf.String()), nil
}

func (c *CSVExporter) formatValue(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case int, int32, int64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%g", v)
	case bool:
		return strconv.FormatBool(v)
	case time.Time:
		return v.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (c *CSVExporter) ContentType() string {
	return "text/csv"
}

// ExporterFactory 导出器工厂
type ExporterFactory struct{}

func NewExporterFactory() *ExporterFactory {
	return &ExporterFactory{}
}

func (f *ExporterFactory) CreateExporter(format common.ExportFormat) (DataExporter, error) {
	switch format {
	case common.FormatPrometheusText:
		return &PrometheusExporter{}, nil
	case common.FormatJSON:
		return &JSONExporter{}, nil
	case common.FormatCSV:
		return &CSVExporter{}, nil
	default:
		return nil, fmt.Errorf("unsupported export format: %s", format)
	}
}

// DataConverter 数据转换器
type DataConverter struct{}

func NewDataConverter() *DataConverter {
	return &DataConverter{}
}

// ConvertToMetrics 将原始数据转换为指标格式
func (dc *DataConverter) ConvertToMetrics(data *common.QueryResult) []common.Metric {
	var metrics []common.Metric

	for _, row := range data.Data {
		metric := common.Metric{
			Timestamp: time.Now(),
			Labels:    make(map[string]string),
		}

		// 尝试从行数据中提取指标信息
		for col, value := range row {
			switch strings.ToLower(col) {
			case "name", "metric_name":
				if s, ok := value.(string); ok {
					metric.Name = s
				}
			case "value", "metric_value":
				if f, ok := dc.convertToFloat64(value); ok {
					metric.Value = f
				}
			case "timestamp", "time":
				if t, ok := dc.convertToTime(value); ok {
					metric.Timestamp = t
				}
			case "description":
				if s, ok := value.(string); ok {
					metric.Description = s
				}
			case "unit":
				if s, ok := value.(string); ok {
					metric.Unit = s
				}
			case "type":
				if s, ok := value.(string); ok {
					metric.Type = common.MetricType(s)
				}
			default:
				// 其他字段作为标签
				if s, ok := value.(string); ok {
					metric.Labels[col] = s
				} else {
					metric.Labels[col] = fmt.Sprintf("%v", value)
				}
			}
		}

		if metric.Name != "" {
			metrics = append(metrics, metric)
		}
	}

	return metrics
}

func (dc *DataConverter) convertToFloat64(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func (dc *DataConverter) convertToTime(value interface{}) (time.Time, bool) {
	switch v := value.(type) {
	case time.Time:
		return v, true
	case string:
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t, true
		}
		if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
			return t, true
		}
	case int64:
		// 假设是Unix时间戳
		return time.Unix(v, 0), true
	}
	return time.Time{}, false
}
