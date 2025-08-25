package main

import (
	"flag"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/wrongerror/o11y-hub/pkg/collector"
	"github.com/wrongerror/o11y-hub/pkg/server"
)

var (
	// Server configuration
	port = flag.Int("port", 8080, "HTTP server port")

	// Vizier/Pixie configuration
	vizierAddress   = flag.String("address", "localhost:50300", "Vizier gRPC address")
	vizierClusterID = flag.String("cluster-id", "", "Pixie cluster ID")
	jwtKey          = flag.String("jwt-key", "", "JWT signing key")
	jwtService      = flag.String("jwt-service", "o11y-hub", "JWT service name")

	// TLS configuration
	tlsEnabled    = flag.Bool("tls", false, "Enable TLS for Vizier connection")
	tlsSkipVerify = flag.Bool("skip-verify", false, "Skip TLS certificate verification")
	tlsCertFile   = flag.String("cert-file", "", "TLS certificate file")
	tlsKeyFile    = flag.String("key-file", "", "TLS private key file")
	tlsCAFile     = flag.String("ca-file", "", "TLS CA certificate file")

	// Beyla configuration
	beylaEnabled = flag.Bool("beyla-enabled", false, "Enable Beyla collector")
	beylaAddress = flag.String("beyla-address", "http://localhost:8999", "Beyla metrics endpoint")

	// Kubernetes configuration
	kubeconfigPath = flag.String("kubeconfig", "", "Path to kubeconfig file (defaults to ~/.kube/config if empty)")

	// Collector configuration
	enabledCollectors = flag.String("collectors", "vizier", "Comma-separated list of enabled collectors")
	scrapeTimeout     = flag.Duration("scrape-timeout", 30*time.Second, "Scrape timeout")
	maxConcurrency    = flag.Int("max-concurrency", 10, "Maximum concurrent scrapes")

	// Logging configuration
	logLevel = flag.String("log-level", "info", "Log level (debug, info, warn, error)")

	// Version information
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

func main() {
	flag.Parse()

	// Setup logging
	logger := logrus.New()
	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logger.WithError(err).Fatal("Invalid log level")
	}
	logger.SetLevel(level)

	logger.WithFields(logrus.Fields{
		"version":    version,
		"build_date": buildDate,
		"git_commit": gitCommit,
	}).Info("Starting O11y Hub")

	// Validate required parameters for Vizier
	collectors := parseCollectors(*enabledCollectors)
	if contains(collectors, "vizier") {
		if *vizierClusterID == "" {
			logger.Fatal("Cluster ID is required when vizier collector is enabled")
		}
		if *jwtKey == "" {
			logger.Fatal("JWT key is required when vizier collector is enabled")
		}
	}

	// Create collector configuration
	config := collector.Config{
		// Vizier configuration
		VizierAddress:    *vizierAddress,
		VizierClusterID:  *vizierClusterID,
		VizierJWTKey:     *jwtKey,
		VizierJWTService: *jwtService,

		// TLS configuration
		TLSEnabled:    *tlsEnabled,
		TLSSkipVerify: *tlsSkipVerify,
		TLSCertFile:   *tlsCertFile,
		TLSKeyFile:    *tlsKeyFile,
		TLSCAFile:     *tlsCAFile,

		// Kubernetes configuration
		KubeconfigPath: *kubeconfigPath,

		// Beyla configuration
		BeylaEnabled: *beylaEnabled,
		BeylaAddress: *beylaAddress,

		// General configuration
		ScrapeTimeout:     *scrapeTimeout,
		MaxConcurrency:    *maxConcurrency,
		EnabledCollectors: collectors,
	}

	// Create and start HTTP server
	srv := server.NewServer(*port, config, logger)

	// Handle graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan

		logger.Info("Shutting down...")
		os.Exit(0)
	}()

	// Start server
	logger.WithFields(logrus.Fields{
		"port":       *port,
		"collectors": collectors,
	}).Info("Server configuration")

	if err := srv.Start(); err != nil {
		logger.WithError(err).Fatal("Failed to start server")
	}
}

// parseCollectors parses the comma-separated list of collectors
func parseCollectors(collectorsStr string) []string {
	if collectorsStr == "" {
		return []string{"vizier"} // Default to vizier collector
	}

	collectors := strings.Split(collectorsStr, ",")
	for i, collector := range collectors {
		collectors[i] = strings.TrimSpace(collector)
	}

	return collectors
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// getEnvOrDefault gets environment variable or returns default value
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvOrDefaultInt gets environment variable as int or returns default value
func getEnvOrDefaultInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvOrDefaultBool gets environment variable as bool or returns default value
func getEnvOrDefaultBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}
