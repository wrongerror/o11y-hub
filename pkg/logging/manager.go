package logging

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/sirupsen/logrus"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

// LoggableEvent interface for events that can be logged
type LoggableEvent interface {
	GetLogType() string
	ToJSON() ([]byte, error)
}

// LogManager manages logging configuration and file operations
type LogManager struct {
	config  *LogConfig
	writers map[string]*lumberjack.Logger
	mutex   sync.RWMutex
	logger  *logrus.Logger
}

// NewLogManager creates a new LogManager with the given configuration
func NewLogManager(config *LogConfig, logger *logrus.Logger) *LogManager {
	return &LogManager{
		config:  config,
		writers: make(map[string]*lumberjack.Logger),
		logger:  logger,
	}
}

// Initialize sets up the logging system
func (lm *LogManager) Initialize() error {
	// Ensure the log directory exists
	if err := os.MkdirAll(lm.config.Directory, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// Initialize writers for each enabled log type
	lm.mutex.Lock()
	defer lm.mutex.Unlock()

	for logType, enabled := range lm.config.EnabledLogs {
		if !enabled {
			continue
		}

		logPath := lm.config.GetLogFilePath(logType)
		writer := &lumberjack.Logger{
			Filename:   logPath,
			MaxSize:    int(lm.config.MaxFileSize / (1024 * 1024)), // Convert to MB
			MaxBackups: lm.config.MaxFiles,
			MaxAge:     7, // days
			Compress:   true,
		}

		lm.writers[logType] = writer
		lm.logger.WithField("log_type", logType).WithField("path", logPath).Info("Initialized log writer")
	}

	return nil
}

// GetWriter returns the writer for a specific log type
func (lm *LogManager) GetWriter(logType string) io.Writer {
	lm.mutex.RLock()
	defer lm.mutex.RUnlock()

	if writer, exists := lm.writers[logType]; exists {
		return writer
	}
	return nil
}

// LogEvent logs an event to the appropriate log file
func (lm *LogManager) LogEvent(event LoggableEvent) error {
	logType := event.GetLogType()

	// Check if this log type is enabled
	if !lm.config.IsLogTypeEnabled(logType) {
		return nil // Silently ignore disabled log types
	}

	writer := lm.GetWriter(logType)
	if writer == nil {
		return fmt.Errorf("no writer found for log type: %s", logType)
	}

	var data []byte
	var err error

	if lm.config.JSONFormat {
		data, err = event.ToJSON()
		if err != nil {
			return fmt.Errorf("failed to serialize event to JSON: %w", err)
		}
		data = append(data, '\n')
	} else {
		// For non-JSON format, we could implement a simple text format
		// For now, we'll still use JSON but could be extended
		data, err = event.ToJSON()
		if err != nil {
			return fmt.Errorf("failed to serialize event: %w", err)
		}
		data = append(data, '\n')
	}

	_, err = writer.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write log entry: %w", err)
	}

	return nil
}

// Close closes all log writers
func (lm *LogManager) Close() error {
	lm.mutex.Lock()
	defer lm.mutex.Unlock()

	var lastErr error
	for logType, writer := range lm.writers {
		if err := writer.Close(); err != nil {
			lm.logger.WithError(err).WithField("log_type", logType).Error("Failed to close log writer")
			lastErr = err
		}
	}

	return lastErr
}

// LogChannel represents a channel for logging events asynchronously
type LogChannel struct {
	manager   *LogManager
	eventChan chan LoggableEvent
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	logger    *logrus.Logger
}

// NewLogChannel creates a new asynchronous log channel
func NewLogChannel(manager *LogManager, bufferSize int, logger *logrus.Logger) *LogChannel {
	ctx, cancel := context.WithCancel(context.Background())
	return &LogChannel{
		manager:   manager,
		eventChan: make(chan LoggableEvent, bufferSize),
		ctx:       ctx,
		cancel:    cancel,
		logger:    logger,
	}
}

// Start begins the asynchronous logging goroutine
func (lc *LogChannel) Start() {
	lc.wg.Add(1)
	go lc.logWorker()
	lc.logger.Info("Log channel started")
}

// Stop stops the asynchronous logging goroutine
func (lc *LogChannel) Stop() {
	lc.cancel()
	close(lc.eventChan)
	lc.wg.Wait()
	lc.logger.Info("Log channel stopped")
}

// LogEvent sends an event to the asynchronous logging channel
func (lc *LogChannel) LogEvent(event LoggableEvent) error {
	select {
	case lc.eventChan <- event:
		return nil
	case <-lc.ctx.Done():
		return fmt.Errorf("log channel is closed")
	default:
		// Channel is full, drop the event or handle it differently
		lc.logger.Warn("Log channel buffer full, dropping event")
		return fmt.Errorf("log channel buffer full")
	}
}

// logWorker is the goroutine that processes log events
func (lc *LogChannel) logWorker() {
	defer lc.wg.Done()

	for {
		select {
		case event, ok := <-lc.eventChan:
			if !ok {
				// Channel closed
				return
			}
			if err := lc.manager.LogEvent(event); err != nil {
				lc.logger.WithError(err).Error("Failed to log event")
			}
		case <-lc.ctx.Done():
			// Context cancelled, drain remaining events
			for {
				select {
				case event := <-lc.eventChan:
					if err := lc.manager.LogEvent(event); err != nil {
						lc.logger.WithError(err).Error("Failed to log event during shutdown")
					}
				default:
					return
				}
			}
		}
	}
}
