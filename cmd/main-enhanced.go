package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/wrongerror/observo-connector/pkg/config"
	"github.com/wrongerror/observo-connector/pkg/server"
	"github.com/wrongerror/observo-connector/pkg/vizier"
)

var (
	cfgFile    string
	address    string
	clusterID  string
	disableSSL bool
	caCert     string
	clientCert string
	clientKey  string
	serverName string
	skipVerify bool
	certDir    string = "./certs"
	serverPort int    = 8080
)

var rootCmd = &cobra.Command{
	Use:   "observo-connector",
	Short: "Observability data connector for Pixie Vizier and other sources",
	Long:  `A universal observability data connector for various data sources including Pixie Vizier, Beyla, etc.`,
}

var queryCmd = &cobra.Command{
	Use:   "query [query_string]",
	Short: "Execute a PxL query on Vizier",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runQuery(args[0])
	},
}

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check health of Vizier cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHealthCheck()
	},
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start HTTP API server",
	Long:  `Start the HTTP API server to provide REST endpoints for querying and data export`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runServer()
	},
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.observo-connector.yaml)")
	rootCmd.PersistentFlags().StringVarP(&address, "address", "a", "localhost:50051", "Vizier address")
	rootCmd.PersistentFlags().StringVarP(&clusterID, "cluster-id", "c", "", "Cluster ID")
	rootCmd.PersistentFlags().BoolVar(&disableSSL, "disable-ssl", false, "Disable SSL/TLS")
	rootCmd.PersistentFlags().StringVar(&caCert, "ca-cert", "", "CA certificate file")
	rootCmd.PersistentFlags().StringVar(&clientCert, "client-cert", "", "Client certificate file")
	rootCmd.PersistentFlags().StringVar(&clientKey, "client-key", "", "Client key file")
	rootCmd.PersistentFlags().StringVar(&serverName, "server-name", "", "Server name for TLS")
	rootCmd.PersistentFlags().BoolVar(&skipVerify, "skip-verify", false, "Skip TLS verification")
	rootCmd.PersistentFlags().StringVar(&certDir, "cert-dir", "./certs", "Certificate directory")

	// Server specific flags
	serverCmd.Flags().IntVarP(&serverPort, "port", "p", 8080, "HTTP server port")

	rootCmd.AddCommand(queryCmd)
	rootCmd.AddCommand(healthCmd)
	rootCmd.AddCommand(serverCmd)

	// Bind flags to viper
	viper.BindPFlag("address", rootCmd.PersistentFlags().Lookup("address"))
	viper.BindPFlag("cluster_id", rootCmd.PersistentFlags().Lookup("cluster-id"))
	viper.BindPFlag("disable_ssl", rootCmd.PersistentFlags().Lookup("disable-ssl"))
	viper.BindPFlag("ca_cert", rootCmd.PersistentFlags().Lookup("ca-cert"))
	viper.BindPFlag("client_cert", rootCmd.PersistentFlags().Lookup("client-cert"))
	viper.BindPFlag("client_key", rootCmd.PersistentFlags().Lookup("client-key"))
	viper.BindPFlag("server_name", rootCmd.PersistentFlags().Lookup("server-name"))
	viper.BindPFlag("skip_verify", rootCmd.PersistentFlags().Lookup("skip-verify"))
	viper.BindPFlag("cert_dir", rootCmd.PersistentFlags().Lookup("cert-dir"))
	viper.BindPFlag("server_port", serverCmd.Flags().Lookup("port"))
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		viper.AddConfigPath(home)
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(".observo-connector")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}

func createVizierClient(ctx context.Context) (*vizier.Client, error) {
	tlsConfig := &config.TLSConfig{
		DisableSSL: viper.GetBool("disable_ssl"),
		CACert:     viper.GetString("ca_cert"),
		ClientCert: viper.GetString("client_cert"),
		ClientKey:  viper.GetString("client_key"),
		ServerName: viper.GetString("server_name"),
		SkipVerify: viper.GetBool("skip_verify"),
		CertDir:    viper.GetString("cert_dir"),
	}

	opts := vizier.ConnectOptions{
		Address:   viper.GetString("address"),
		TLSConfig: tlsConfig,
	}

	return vizier.NewClient(ctx, opts)
}

func runQuery(query string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := createVizierClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	clusterIDValue := viper.GetString("cluster_id")
	if clusterIDValue == "" {
		return fmt.Errorf("cluster ID is required")
	}

	result, err := client.ExecuteScriptAndExtractData(ctx, clusterIDValue, query)
	if err != nil {
		return fmt.Errorf("failed to execute query: %w", err)
	}

	fmt.Printf("Query executed successfully!\n")
	fmt.Printf("Duration: %v\n", result.Duration)
	fmt.Printf("Rows: %d\n", result.RowCount)
	fmt.Printf("Columns: %v\n", result.Columns)

	// 打印前几行数据
	maxRows := 5
	if len(result.Data) < maxRows {
		maxRows = len(result.Data)
	}

	for i := 0; i < maxRows; i++ {
		fmt.Printf("Row %d: %v\n", i+1, result.Data[i])
	}

	return nil
}

func runHealthCheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := createVizierClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	clusterIDValue := viper.GetString("cluster_id")
	if clusterIDValue == "" {
		return fmt.Errorf("cluster ID is required")
	}

	err = client.HealthCheck(ctx, clusterIDValue)
	if err != nil {
		fmt.Printf("Health check failed: %v\n", err)
		return err
	}

	fmt.Println("Health check passed!")
	return nil
}

func runServer() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 创建Vizier客户端
	client, err := createVizierClient(ctx)
	if err != nil {
		return fmt.Errorf("failed to create Vizier client: %w", err)
	}

	// 创建HTTP服务器
	port := viper.GetInt("server_port")
	if port == 0 {
		port = serverPort
	}

	httpServer := server.NewServer(port, client)

	// 设置优雅关闭
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Println("\nShutting down server...")
		client.Close()
		os.Exit(0)
	}()

	fmt.Printf("Starting HTTP server on port %d...\n", port)
	fmt.Printf("API endpoints:\n")
	fmt.Printf("  POST /api/v1/query        - Execute queries\n")
	fmt.Printf("  GET  /api/v1/query        - Execute queries (GET)\n")
	fmt.Printf("  GET  /api/v1/health       - Health check\n")
	fmt.Printf("  GET  /api/v1/metrics      - Prometheus metrics\n")
	fmt.Printf("  POST /api/v1/export       - Export data\n")

	return httpServer.Start()
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
