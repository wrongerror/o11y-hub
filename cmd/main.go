package main

import (
	"context"
	"flag"
	"time"

	"github.com/sirupsen/logrus"
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
		apiKey     = flag.String("api-key", "", "Direct Pixie API key")

		// TLS flags
		tlsEnabled = flag.Bool("tls", true, "Enable TLS")
		skipVerify = flag.Bool("skip-verify", false, "Skip TLS verification")

		// Action flags
		healthCheck = flag.Bool("health-check", false, "Perform health check")
		execScript  = flag.Bool("exec-script", false, "Execute a PxL script")
		scriptQuery = flag.String("script-query", "", "PxL script query to execute")

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
		"health_check": *healthCheck,
		"exec_script":  *execScript,
		"script_query": *scriptQuery,
	}).Debug("Command flags parsed")

	// Validate required parameters
	if *address == "" {
		logger.Fatal("Address is required. Use --address flag")
	}

	if *clusterID == "" {
		logger.Fatal("Cluster ID is required. Use --cluster-id flag")
	}

	// Validate authentication
	hasJWT := *jwtKey != ""
	hasAPIKey := *apiKey != ""

	if !hasJWT && !hasAPIKey && *address != "localhost:50300" {
		logger.Fatal("Authentication is required. Use --jwt-key or --api-key flag")
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
	} else if hasAPIKey {
		opts = append(opts, vizier.WithDirectVizierKey(*apiKey))
	}

	// Create client
	client, err := vizier.NewClient(*address, logger, opts...)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create Vizier client")
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

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
	} else {
		logger.Info("No action specified. Use --health-check to test connection or --exec-script to execute scripts")
	}
}
