package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/wrongerror/observo-connector/pkg/config"
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
)

var rootCmd = &cobra.Command{
	Use:   "github.com/wrongerror/observo-connector",
	Short: "Demo connector for Pixie Vizier",
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

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file")
	rootCmd.PersistentFlags().StringVar(&address, "address", "vizier.pixie.svc.cluster.local:51400", "Vizier address")
	rootCmd.PersistentFlags().StringVar(&clusterID, "cluster-id", "", "Cluster ID")
	rootCmd.PersistentFlags().BoolVar(&disableSSL, "disable-ssl", false, "Disable SSL")
	rootCmd.PersistentFlags().StringVar(&caCert, "ca-cert", "", "CA certificate file")
	rootCmd.PersistentFlags().StringVar(&clientCert, "client-cert", "", "Client certificate file")
	rootCmd.PersistentFlags().StringVar(&clientKey, "client-key", "", "Client key file")
	rootCmd.PersistentFlags().StringVar(&serverName, "server-name", "", "Server name for TLS verification")
	rootCmd.PersistentFlags().BoolVar(&skipVerify, "skip-verify", false, "Skip TLS certificate verification")

	rootCmd.AddCommand(queryCmd)
	rootCmd.AddCommand(healthCmd)

	// Bind flags to viper
	viper.BindPFlags(rootCmd.PersistentFlags())
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runQuery(queryStr string) error {
	client, err := createVizierClient()
	if err != nil {
		return fmt.Errorf("failed to create Vizier client: %w", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return client.ExecuteScript(ctx, viper.GetString("cluster-id"), queryStr)
}

func runHealthCheck() error {
	client, err := createVizierClient()
	if err != nil {
		return fmt.Errorf("failed to create Vizier client: %w", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return client.HealthCheck(ctx, viper.GetString("cluster-id"))
}

func createVizierClient() (*vizier.Client, error) {
	tlsConfig := &config.TLSConfig{
		DisableSSL: viper.GetBool("disable-ssl"),
		CACert:     viper.GetString("ca-cert"),
		ClientCert: viper.GetString("client-cert"),
		ClientKey:  viper.GetString("client-key"),
		ServerName: viper.GetString("server-name"),
		SkipVerify: viper.GetBool("skip-verify"),
	}

	opts := vizier.ConnectOptions{
		Address:   viper.GetString("address"),
		TLSConfig: tlsConfig,
	}

	return vizier.NewClient(context.Background(), opts)
}
