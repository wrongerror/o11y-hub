package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wrongerror/observo-connector/pkg/vizier"
)

var cfgFile string

var rootCmd = &cobra.Command{
	Use:   "observo-connector",
	Short: "Observo Connector for Pixie Vizier",
	Long:  `A connector to extract data from Pixie Vizier and export it in various formats`,
}

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Check health of Vizier cluster",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHealthCheck()
	},
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.observo-connector.yaml)")
	rootCmd.PersistentFlags().StringP("address", "a", "localhost:50051", "Vizier address")
	rootCmd.PersistentFlags().StringP("cluster-id", "c", "", "Cluster ID")
	rootCmd.PersistentFlags().Bool("disable-ssl", false, "Disable SSL/TLS")
	rootCmd.PersistentFlags().String("ca-cert", "", "CA certificate file")
	rootCmd.PersistentFlags().String("client-cert", "", "Client certificate file")
	rootCmd.PersistentFlags().String("client-key", "", "Client key file")
	rootCmd.PersistentFlags().String("server-name", "", "Server name for TLS")
	rootCmd.PersistentFlags().Bool("skip-verify", false, "Skip TLS verification")
	rootCmd.PersistentFlags().String("cert-dir", "./certs", "Certificate directory")

	// JWT Authentication flags
	rootCmd.PersistentFlags().String("jwt-signing-key", "", "JWT signing key for authentication")
	rootCmd.PersistentFlags().String("jwt-user-id", "", "JWT user ID")
	rootCmd.PersistentFlags().String("jwt-org-id", "", "JWT organization ID")
	rootCmd.PersistentFlags().String("jwt-email", "", "JWT email")
	rootCmd.PersistentFlags().String("jwt-service-name", "", "JWT service name (for service authentication)")

	// Direct Vizier authentication flags
	rootCmd.PersistentFlags().String("direct-vizier-key", "", "Direct Vizier authentication key")

	rootCmd.AddCommand(healthCmd)

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
	viper.BindPFlag("jwt_signing_key", rootCmd.PersistentFlags().Lookup("jwt-signing-key"))
	viper.BindPFlag("jwt_user_id", rootCmd.PersistentFlags().Lookup("jwt-user-id"))
	viper.BindPFlag("jwt_org_id", rootCmd.PersistentFlags().Lookup("jwt-org-id"))
	viper.BindPFlag("jwt_email", rootCmd.PersistentFlags().Lookup("jwt-email"))
	viper.BindPFlag("jwt_service_name", rootCmd.PersistentFlags().Lookup("jwt-service-name"))
	viper.BindPFlag("direct_vizier_key", rootCmd.PersistentFlags().Lookup("direct-vizier-key"))
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
	address := viper.GetString("address")
	var opts []vizier.Option

	if !viper.GetBool("disable_ssl") {
		opts = append(opts, vizier.WithTLS(true))

		if viper.GetString("ca_cert") != "" {
			opts = append(opts, vizier.WithCACert(viper.GetString("ca_cert")))
		}

		if viper.GetString("client_cert") != "" && viper.GetString("client_key") != "" {
			opts = append(opts, vizier.WithClientCert(viper.GetString("client_cert"), viper.GetString("client_key")))
		}

		if viper.GetString("server_name") != "" {
			opts = append(opts, vizier.WithServerName(viper.GetString("server_name")))
		}

		if viper.GetBool("skip_verify") {
			opts = append(opts, vizier.WithInsecureSkipVerify(true))
		}
	} else {
		opts = append(opts, vizier.WithTLS(false))
	}

	// Add authentication options
	if viper.GetString("direct_vizier_key") != "" {
		opts = append(opts, vizier.WithDirectVizierKey(viper.GetString("direct_vizier_key")))
	} else if viper.GetString("jwt_signing_key") != "" {
		if viper.GetString("jwt_service_name") != "" {
			opts = append(opts, vizier.WithJWTServiceAuth(viper.GetString("jwt_signing_key"), viper.GetString("jwt_service_name")))
		} else {
			opts = append(opts, vizier.WithJWTAuth(
				viper.GetString("jwt_signing_key"),
				viper.GetString("jwt_user_id"),
				viper.GetString("jwt_org_id"),
				viper.GetString("jwt_email"),
			))
		}
	}

	return vizier.NewClient(address, opts...)
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

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
