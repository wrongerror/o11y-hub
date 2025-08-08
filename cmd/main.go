package main

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/wrongerror/observo-connector/pkg/scripts"
	"github.com/wrongerror/observo-connector/pkg/server"
	"github.com/wrongerror/observo-connector/pkg/vizier"
)

func main() {
	// Command line flags
	var (
		address   = flag.String("address", "", "Vizier server address (required)")
		clusterID = flag.String("cluster-id", "", "Cluster ID (required)")

		// Authentication flags
		jwtKey     = flag.String("jwt-key", "", "JWT signing key")
		jwtUserID  = flag.String("jwt-user-id", "", "JWT user ID")
		jwtOrgID   = flag.String("jwt-org-id", "", "JWT organization ID")
		jwtEmail   = flag.String("jwt-email", "", "JWT email")
		jwtService = flag.String("jwt-service", "", "JWT service name (for service auth)")

		// TLS flags
		tlsEnabled = flag.Bool("tls", true, "Enable TLS")
		skipVerify = flag.Bool("skip-verify", false, "Skip TLS verification")

		// Action flags
		healthCheck   = flag.Bool("health-check", false, "Perform health check")
		execScript    = flag.Bool("exec-script", false, "Execute a PxL script")
		scriptQuery   = flag.String("script-query", "", "PxL script query to execute")
		builtinScript = flag.String("builtin-script", "", "Execute a builtin script by name")
		scriptParams  = flag.String("script-params", "", "Parameters for builtin script (key1=value1,key2=value2)")
		listScripts   = flag.Bool("list-scripts", false, "List all available builtin scripts")
		scriptHelp    = flag.String("script-help", "", "Show help for a specific builtin script")
		startServer   = flag.Bool("server", false, "Start HTTP server for metrics and API endpoints")
		serverPort    = flag.Int("port", 8080, "HTTP server port")

		// Misc flags
		verbose = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()

	// Setup logging
	logger := logrus.New()
	if *verbose {
		logger.SetLevel(logrus.DebugLevel)
	}

	logger.WithFields(logrus.Fields{
		"health_check":   *healthCheck,
		"exec_script":    *execScript,
		"script_query":   *scriptQuery,
		"builtin_script": *builtinScript,
		"list_scripts":   *listScripts,
	}).Debug("Command flags parsed")

	// Handle script-related actions first (don't need client connection)
	if *listScripts {
		executor := scripts.NewExecutor(nil, logger)
		listBuiltinScripts(executor)
		return
	}

	if *scriptHelp != "" {
		executor := scripts.NewExecutor(nil, logger)
		showScriptHelp(executor, *scriptHelp)
		return
	}

	// Validate required parameters for actions that need connection
	if *address == "" {
		logger.Fatal("Address is required. Use --address flag")
	}

	if *clusterID == "" {
		logger.Fatal("Cluster ID is required. Use --cluster-id flag")
	}

	// Validate authentication
	hasJWT := *jwtKey != ""

	if !hasJWT && *address != "localhost:50300" {
		logger.Fatal("Authentication is required. Use --jwt-key flag")
	}

	// Build client options
	var opts []vizier.Option

	opts = append(opts, vizier.WithTLS(*tlsEnabled))

	if *skipVerify {
		opts = append(opts, vizier.WithInsecureSkipVerify(*skipVerify))
	}

	// Add authentication options
	if hasJWT {
		if *jwtService != "" {
			opts = append(opts, vizier.WithJWTServiceAuth(*jwtKey, *jwtService))
		} else {
			if *jwtUserID == "" || *jwtOrgID == "" || *jwtEmail == "" {
				logger.Fatal("For user JWT auth, user-id, org-id, and email are required")
			}
			opts = append(opts, vizier.WithJWTAuth(*jwtKey, *jwtUserID, *jwtOrgID, *jwtEmail))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Handle script-related actions first (don't need client connection)
	if *listScripts {
		executor := scripts.NewExecutor(nil, logger)
		listBuiltinScripts(executor)
		return
	}

	if *scriptHelp != "" {
		executor := scripts.NewExecutor(nil, logger)
		showScriptHelp(executor, *scriptHelp)
		return
	}

	// Create client for actions that need connection
	client, err := vizier.NewClient(*address, logger, opts...)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create Vizier client")
	}
	defer client.Close()

	// Create script executor
	executor := scripts.NewExecutor(client, logger)

	// Perform requested action
	if *healthCheck {
		logger.Info("Performing health check...")
		err := client.HealthCheck(ctx, *clusterID)
		if err != nil {
			logger.WithError(err).Fatal("Health check failed")
		}
		logger.Info("Health check passed successfully!")
	} else if *execScript {
		if *scriptQuery == "" {
			logger.Fatal("Script query is required. Use --script-query flag")
		}
		logger.Info("Executing PxL script...")
		err := client.ExecuteScript(ctx, *clusterID, *scriptQuery)
		if err != nil {
			logger.WithError(err).Fatal("Script execution failed")
		}
		logger.Info("Script execution completed!")
	} else if *builtinScript != "" {
		// Parse script parameters
		params := parseScriptParams(*scriptParams)
		logger.WithFields(logrus.Fields{
			"script_name": *builtinScript,
			"parameters":  params,
		}).Info("Executing builtin script...")

		err := executor.ExecuteBuiltinScript(ctx, *clusterID, *builtinScript, params)
		if err != nil {
			logger.WithError(err).Fatal("Builtin script execution failed")
		}
		logger.Info("Builtin script execution completed!")
	} else if *startServer {
		// Start HTTP server
		logger.WithFields(logrus.Fields{
			"port":       *serverPort,
			"cluster_id": *clusterID,
		}).Info("Starting HTTP server...")

		httpServer := server.NewServer(*serverPort, client, *clusterID)
		if err := httpServer.Start(); err != nil {
			logger.WithError(err).Fatal("Failed to start HTTP server")
		}
	} else {
		logger.Info("No action specified. Use --health-check, --exec-script, --builtin-script, --server, or --list-scripts")
	}
}

// Helper functions
func listBuiltinScripts(executor *scripts.Executor) {
	scriptInfos := executor.ListBuiltinScripts()

	fmt.Println("\nAvailable Builtin Scripts:")
	fmt.Println("==========================")

	categories := make(map[string][]string)
	for name, script := range scriptInfos {
		categories[script.Category] = append(categories[script.Category], name)
	}

	for category, scriptNames := range categories {
		fmt.Printf("\n%s:\n", category)
		for _, name := range scriptNames {
			script := scriptInfos[name]
			fmt.Printf("  %-20s %s\n", name, script.Description)
		}
	}

	fmt.Println("\nUse --script-help <script-name> for detailed help on a specific script.")
}

func showScriptHelp(executor *scripts.Executor, scriptName string) {
	info, err := executor.GetScriptHelp(scriptName)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("\nScript: %s\n", info.Name)
	fmt.Printf("Category: %s\n", info.Category)
	fmt.Printf("Description: %s\n\n", info.Description)

	if len(info.Parameters) > 0 {
		fmt.Println("Parameters:")
		for _, param := range info.Parameters {
			required := ""
			if param.Required {
				required = " (required)"
			}
			defaultVal := ""
			if param.DefaultValue != "" {
				defaultVal = fmt.Sprintf(" [default: %s]", param.DefaultValue)
			}
			fmt.Printf("  %-15s %s%s%s\n", param.Name, param.Description, required, defaultVal)
		}
	}

	fmt.Printf("\nExample usage:\n")
	fmt.Printf("  --builtin-script %s", scriptName)
	if len(info.Parameters) > 0 {
		fmt.Printf(" --script-params \"param1=value1,param2=value2\"")
	}
	fmt.Println()
}

func parseScriptParams(paramStr string) map[string]string {
	params := make(map[string]string)
	if paramStr == "" {
		return params
	}

	pairs := strings.Split(paramStr, ",")
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			params[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	return params
}
