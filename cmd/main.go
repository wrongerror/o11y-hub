package main

import (
	"context"
	"flag"
	"fmt"
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

		// TLS flags
		tlsEnabled = flag.Bool("tls", true, "Enable TLS")
		skipVerify = flag.Bool("skip-verify", false, "Skip TLS verification")

		// Action flags
		healthCheck  = flag.Bool("health-check", false, "Perform health check")
		execScript   = flag.Bool("exec-script", false, "Execute a PxL script")
		scriptQuery  = flag.String("script-query", "", "PxL script query to execute")
		scriptName   = flag.String("script-name", "", "Name for the script execution")
		scriptMutation = flag.Bool("script-mutation", false, "Enable mutations in script")

		// Misc flags
		verbose = flag.Bool("verbose", false, "Enable verbose logging")
	)
	flag.Parse()

	// Setup logging
	logger := logrus.New()
	if *verbose {
		logger.SetLevel(logrus.DebugLevel)
	}

	// Validate required parameters
	if *address == "" {
		logger.Fatal("Address is required. Use --address flag")
	}

	if *clusterID == "" {
		logger.Fatal("Cluster ID is required. Use --cluster-id flag")
	}

	// Validate authentication
	hasJWT := *jwtKey != ""

	if !hasJWT {
		logger.Fatal("JWT authentication is required. Use --jwt-key flag")
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
			logger.Fatal("Script query is required for script execution. Use --script-query flag")
		}
		
		logger.Info("Executing PxL script...")
		
		req := &vizier.ExecuteScriptRequest{
			ClusterID: *clusterID,
			QueryStr:  *scriptQuery,
			Mutation:  *scriptMutation,
			QueryName: *scriptName,
		}
		
		stream, err := client.ExecuteScript(ctx, req)
		if err != nil {
			logger.WithError(err).Fatal("Failed to execute script")
		}
		defer stream.Close()
		
		logger.Info("Receiving script execution results...")
		responses, err := stream.RecvAll()
		if err != nil {
			logger.WithError(err).Fatal("Failed to receive script results")
		}
		
		logger.WithField("response_count", len(responses)).Info("Script execution completed")
		
		// Print summary of responses
		for i, resp := range responses {
			logger.WithFields(logrus.Fields{
				"response_index": i,
				"query_id":       resp.QueryId,
				"has_data":       resp.GetData() != nil,
				"has_metadata":   resp.GetMetaData() != nil,
				"has_status":     resp.Status != nil,
			}).Info("Response received")
			
			// Print detailed data if available
			if resp.GetData() != nil && resp.GetData().Batch != nil {
				batch := resp.GetData().Batch
				logger.WithFields(logrus.Fields{
					"num_cols": len(batch.Cols),
					"num_rows": batch.NumRows,
					"eos":      batch.Eos,
					"eow":      batch.Eow,
				}).Info("Data batch details")
			}
			
			// Print metadata if available
			if resp.GetMetaData() != nil {
				meta := resp.GetMetaData()
				logger.WithFields(logrus.Fields{
					"name":       meta.Name,
					"id":         meta.Id,
					"has_schema": len(meta.Relation.Columns) > 0,
				}).Info("Metadata received")
			}
		}
	} else {
		fmt.Println("Use --health-check to perform a health check or --exec-script to execute a script")
		flag.Usage()
	}
}
