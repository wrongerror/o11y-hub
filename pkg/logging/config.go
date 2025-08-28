package logging

import (
	"path/filepath"
)

// LogConfig contains configuration for the logging system
type LogConfig struct {
	// Directory where log files will be stored
	Directory string `yaml:"directory" json:"directory"`
	// Maximum number of rotated log files to keep
	MaxFiles int `yaml:"max_files" json:"max_files"`
	// Set of enabled log types
	EnabledLogs map[string]bool `yaml:"enabled_logs" json:"enabled_logs"`
	// Whether to use JSON format for logs
	JSONFormat bool `yaml:"json_format" json:"json_format"`
	// Max file size for log rotation (in bytes)
	MaxFileSize int64 `yaml:"max_file_size" json:"max_file_size"`
}

// DefaultLogConfig returns a default logging configuration
func DefaultLogConfig() *LogConfig {
	return &LogConfig{
		Directory:   "./logs",
		MaxFiles:    5,
		EnabledLogs: map[string]bool{"http_traffic": true},
		JSONFormat:  true,
		MaxFileSize: 100 * 1024 * 1024, // 100MB
	}
}

// IsLogTypeEnabled checks if a specific log type is enabled
func (c *LogConfig) IsLogTypeEnabled(logType string) bool {
	enabled, exists := c.EnabledLogs[logType]
	return exists && enabled
}

// GetLogFilePath returns the full path for a log file of the given type
func (c *LogConfig) GetLogFilePath(logType string) string {
	return filepath.Join(c.Directory, logType+".log")
}
