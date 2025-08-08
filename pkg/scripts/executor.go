package scripts

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
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
