package scripts

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
)

// ScriptManager 管理PxL脚本的加载和执行
type ScriptManager struct {
	scriptsDir string
	scripts    map[string]*Script
}

// Script 表示一个PxL脚本
type Script struct {
	Name       string            `yaml:"-"`
	Short      string            `yaml:"short"`
	Long       string            `yaml:"long"`
	Category   string            `yaml:"category"`
	Tags       []string          `yaml:"tags"`
	Parameters []ScriptParameter `yaml:"parameters"`
	Content    string            `yaml:"-"`
	Path       string            `yaml:"-"`
	IsBuiltin  bool              `yaml:"-"`
}

// ScriptParameter 表示脚本参数
type ScriptParameter struct {
	Name        string      `yaml:"name"`
	Type        string      `yaml:"type"`
	Description string      `yaml:"description"`
	Default     interface{} `yaml:"default"`
	Required    bool        `yaml:"required"`
}

// NewScriptManager 创建新的脚本管理器
func NewScriptManager(scriptsDir string) *ScriptManager {
	return &ScriptManager{
		scriptsDir: scriptsDir,
		scripts:    make(map[string]*Script),
	}
}

// LoadScripts 加载所有脚本（包括内置脚本和文件系统脚本）
func (sm *ScriptManager) LoadScripts() error {
	// 首先加载内置脚本
	sm.loadBuiltinScripts()

	// 然后加载文件系统脚本（如果存在的话）
	if err := sm.loadFileScripts(); err != nil {
		// 如果文件系统脚本加载失败，只记录警告，不返回错误
		fmt.Printf("Warning: failed to load file system scripts: %v\n", err)
	}

	return nil
}

// loadBuiltinScripts 加载内置脚本
func (sm *ScriptManager) loadBuiltinScripts() {
	for name, template := range BuiltinScripts {
		script := &Script{
			Name:      name,
			Short:     template.Description,
			Long:      template.Description,
			Category:  template.Category,
			Tags:      []string{},
			Content:   template.Template,
			Path:      "",
			IsBuiltin: true,
		}

		// 转换参数格式
		for _, param := range template.Parameters {
			scriptParam := ScriptParameter{
				Name:        param.Name,
				Type:        param.Type,
				Description: param.Description,
				Default:     param.DefaultValue,
				Required:    param.Required,
			}
			script.Parameters = append(script.Parameters, scriptParam)
		}

		sm.scripts[name] = script
	}
}

// loadFileScripts 加载文件系统脚本
func (sm *ScriptManager) loadFileScripts() error {
	pxDir := filepath.Join(sm.scriptsDir, "px")

	// 检查scripts/px目录是否存在
	if _, err := os.Stat(pxDir); os.IsNotExist(err) {
		return fmt.Errorf("scripts directory not found: %s", pxDir)
	}

	// 遍历px目录下的所有子目录
	entries, err := ioutil.ReadDir(pxDir)
	if err != nil {
		return fmt.Errorf("failed to read scripts directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		scriptName := entry.Name()
		scriptDir := filepath.Join(pxDir, scriptName)

		script, err := sm.loadScript(scriptName, scriptDir)
		if err != nil {
			// 记录错误但不中断加载
			fmt.Printf("Warning: failed to load script %s: %v\n", scriptName, err)
			continue
		}

		// 文件系统脚本会覆盖同名的内置脚本
		sm.scripts[scriptName] = script
	}

	return nil
}

// loadScript 加载单个脚本
func (sm *ScriptManager) loadScript(name, dir string) (*Script, error) {
	// 读取manifest.yaml
	manifestPath := filepath.Join(dir, "manifest.yaml")
	manifestData, err := ioutil.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var script Script
	if err := yaml.Unmarshal(manifestData, &script); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	script.Name = name
	script.Path = dir
	script.IsBuiltin = false

	// 查找PxL文件（优先查找与目录同名的文件）
	pxlFiles := []string{
		fmt.Sprintf("%s.pxl", name),
		"data.pxl",
		"script.pxl",
	}

	var pxlContent string
	for _, pxlFile := range pxlFiles {
		pxlPath := filepath.Join(dir, pxlFile)
		if data, err := ioutil.ReadFile(pxlPath); err == nil {
			pxlContent = string(data)
			break
		}
	}

	if pxlContent == "" {
		return nil, fmt.Errorf("no PxL script file found in %s", dir)
	}

	script.Content = pxlContent

	return &script, nil
}

// GetScript 获取指定脚本
func (sm *ScriptManager) GetScript(name string) (*Script, error) {
	script, exists := sm.scripts[name]
	if !exists {
		return nil, fmt.Errorf("script not found: %s", name)
	}
	return script, nil
}

// ListScripts 列出所有脚本
func (sm *ScriptManager) ListScripts() []string {
	var names []string
	for name := range sm.scripts {
		names = append(names, name)
	}
	return names
}

// GetAllScripts 获取所有脚本的详细信息
func (sm *ScriptManager) GetAllScripts() map[string]*Script {
	result := make(map[string]*Script)
	for name, script := range sm.scripts {
		result[name] = script
	}
	return result
}

// GetScriptsByCategory 按类别获取脚本
func (sm *ScriptManager) GetScriptsByCategory(category string) []*Script {
	var scripts []*Script
	for _, script := range sm.scripts {
		if script.Category == category {
			scripts = append(scripts, script)
		}
	}
	return scripts
}

// GetScriptsByTag 按标签获取脚本
func (sm *ScriptManager) GetScriptsByTag(tag string) []*Script {
	var scripts []*Script
	for _, script := range sm.scripts {
		for _, scriptTag := range script.Tags {
			if scriptTag == tag {
				scripts = append(scripts, script)
				break
			}
		}
	}
	return scripts
}

// ExecuteScript 执行脚本（替换参数）
func (sm *ScriptManager) ExecuteScript(name string, params map[string]string) (string, error) {
	script, err := sm.GetScript(name)
	if err != nil {
		return "", err
	}

	// 替换参数
	content := script.Content

	// 处理脚本参数
	for _, param := range script.Parameters {
		placeholder := fmt.Sprintf("{{%s}}", param.Name)
		value := params[param.Name]

		// 如果没有提供参数值，使用默认值
		if value == "" {
			if param.Required {
				return "", fmt.Errorf("required parameter %s not provided", param.Name)
			}
			if param.Default != nil {
				value = fmt.Sprintf("%v", param.Default)
			}
		}

		content = strings.ReplaceAll(content, placeholder, value)
	}

	// 处理特殊的内置变量
	for key, value := range params {
		placeholder := fmt.Sprintf("{{%s}}", key)
		content = strings.ReplaceAll(content, placeholder, value)
	}

	return content, nil
}

// GetScriptHelp 获取脚本帮助信息
func (sm *ScriptManager) GetScriptHelp(name string) (string, error) {
	script, err := sm.GetScript(name)
	if err != nil {
		return "", err
	}

	help := fmt.Sprintf("Script: %s\n", script.Name)
	help += fmt.Sprintf("Description: %s\n", script.Short)
	if script.Long != "" {
		help += fmt.Sprintf("\nDetails:\n%s\n", script.Long)
	}

	if script.Category != "" {
		help += fmt.Sprintf("\nCategory: %s\n", script.Category)
	}

	if len(script.Tags) > 0 {
		help += fmt.Sprintf("Tags: %s\n", strings.Join(script.Tags, ", "))
	}

	help += "\nParameters:\n"
	for _, param := range script.Parameters {
		required := ""
		if param.Required {
			required = " (required)"
		}
		defaultVal := ""
		if param.Default != nil {
			defaultVal = fmt.Sprintf(" [default: %v]", param.Default)
		}
		help += fmt.Sprintf("  %s (%s)%s%s: %s\n",
			param.Name, param.Type, required, defaultVal, param.Description)
	}

	return help, nil
}
