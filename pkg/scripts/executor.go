package scripts

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wrongerror/observo-connector/pkg/common"
	"github.com/wrongerror/observo-connector/pkg/vizier"
)

// Executor manages script execution and result handling
type Executor struct {
	client *vizier.Client
	logger *logrus.Logger
}

// NewExecutor creates a new script executor
func NewExecutor(client *vizier.Client, logger *logrus.Logger) *Executor {
	return &Executor{
		client: client,
		logger: logger,
	}
}

// ExecuteBuiltinScript executes a builtin script by name with parameters
func (e *Executor) ExecuteBuiltinScript(ctx context.Context, clusterID, scriptName string, params map[string]string) error {
	// Get the script template
	script, exists := BuiltinScripts[scriptName]
	if !exists {
		return fmt.Errorf("builtin script '%s' not found", scriptName)
	}

	e.logger.WithFields(logrus.Fields{
		"script_name": scriptName,
		"description": script.Description,
		"category":    script.Category,
		"parameters":  params,
	}).Info("Executing builtin script")

	// Render the script with parameters
	renderedScript, err := script.Execute(params)
	if err != nil {
		return fmt.Errorf("failed to render script: %w", err)
	}

	e.logger.WithField("rendered_script", renderedScript).Debug("Rendered script")

	// Execute the script
	return e.client.ExecuteScript(ctx, clusterID, renderedScript)
}

// ListBuiltinScripts returns information about all available builtin scripts
func (e *Executor) ListBuiltinScripts() map[string]ScriptInfo {
	result := make(map[string]ScriptInfo)

	for name, script := range BuiltinScripts {
		result[name] = ScriptInfo{
			Name:        script.Name,
			Description: script.Description,
			Category:    script.Category,
			Parameters:  script.Parameters,
		}
	}

	return result
}

// ScriptInfo contains information about a script without the template
type ScriptInfo struct {
	Name        string
	Description string
	Category    string
	Parameters  []Parameter
}

// GetScriptHelp returns detailed help for a specific script
func (e *Executor) GetScriptHelp(scriptName string) (*ScriptInfo, error) {
	script, exists := BuiltinScripts[scriptName]
	if !exists {
		return nil, fmt.Errorf("script '%s' not found", scriptName)
	}

	return &ScriptInfo{
		Name:        script.Name,
		Description: script.Description,
		Category:    script.Category,
		Parameters:  script.Parameters,
	}, nil
}

// ExecuteBuiltinScriptForMetrics executes a builtin script and returns structured data for metrics conversion
func (e *Executor) ExecuteBuiltinScriptForMetrics(ctx context.Context, clusterID, scriptName string, params map[string]string) (*common.QueryResult, error) {
	// Get the script template
	script, exists := BuiltinScripts[scriptName]
	if !exists {
		return nil, fmt.Errorf("builtin script '%s' not found", scriptName)
	}

	e.logger.WithFields(logrus.Fields{
		"script_name": scriptName,
		"description": script.Description,
		"category":    script.Category,
		"parameters":  params,
	}).Info("Executing builtin script for metrics collection")

	// Render the script with parameters
	renderedScript, err := script.Execute(params)
	if err != nil {
		return nil, fmt.Errorf("failed to render script: %w", err)
	}

	// Execute the script and capture results
	startTime := time.Now()
	result, err := e.client.ExecuteScriptAndExtractData(ctx, clusterID, renderedScript)
	if err != nil {
		return nil, fmt.Errorf("failed to execute script: %w", err)
	}

	// Enhance result with script metadata
	if result != nil {
		result.Query = renderedScript
		result.ExecutedAt = startTime
		result.Duration = time.Since(startTime)
		
		if result.Metadata == nil {
			result.Metadata = make(map[string]string)
		}
		result.Metadata["script_name"] = scriptName
		result.Metadata["script_category"] = script.Category
		result.Metadata["script_description"] = script.Description
	}

	return result, nil
}

// ExecuteAllMetricScripts executes all builtin scripts suitable for metrics collection
func (e *Executor) ExecuteAllMetricScripts(ctx context.Context, clusterID string) ([]*common.QueryResult, error) {
	var results []*common.QueryResult
	var errors []string

	// Define default parameters for automatic execution
	defaultParams := map[string]map[string]string{
		"resource_usage": {
			"start_time": "-5m",
			"namespace":  "",
		},
		"http_overview": {
			"start_time": "-5m",
			"namespace":  "",
		},
		"network_stats": {
			"start_time": "-5m", 
			"namespace":  "",
		},
		"error_analysis": {
			"start_time": "-5m",
			"namespace":  "",
		},
		"pod_overview": {
			"pod_name":  "",
			"namespace": "",
		},
	}

	for scriptName := range BuiltinScripts {
		// Skip pod_overview as it requires specific pod name
		if scriptName == "pod_overview" {
			continue
		}

		params := defaultParams[scriptName]
		if params == nil {
			params = make(map[string]string)
		}

		result, err := e.ExecuteBuiltinScriptForMetrics(ctx, clusterID, scriptName, params)
		if err != nil {
			e.logger.WithError(err).WithField("script_name", scriptName).Warn("Failed to execute script for metrics")
			errors = append(errors, fmt.Sprintf("%s: %v", scriptName, err))
			continue
		}

		if result != nil {
			results = append(results, result)
		}
	}

	// Log any errors but don't fail completely
	if len(errors) > 0 {
		e.logger.WithField("errors", strings.Join(errors, "; ")).Warn("Some scripts failed during metrics collection")
	}

	return results, nil
}
