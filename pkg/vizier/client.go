package vizier

import (
	"context"
	"fmt"
	"io"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip" // Register gzip compressor

	"github.com/wrongerror/observo-connector/pkg/config"
	pb "github.com/wrongerror/observo-connector/proto"
)

type Client struct {
	conn   *grpc.ClientConn
	client pb.VizierServiceClient
	logger *logrus.Logger
}

type ConnectOptions struct {
	Address   string
	TLSConfig *config.TLSConfig
}

// NewClient creates a new Vizier client
func NewClient(ctx context.Context, opts ConnectOptions) (*Client, error) {
	logger := logrus.New()

	dialOpts, err := opts.TLSConfig.GetGRPCDialOpts()
	if err != nil {
		return nil, fmt.Errorf("failed to get gRPC dial options: %w", err)
	}

	// Add compression and other options
	dialOpts = append(dialOpts,
		grpc.WithDefaultCallOptions(grpc.UseCompressor("gzip")),
		grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
	)

	logger.WithFields(logrus.Fields{
		"address":     opts.Address,
		"tls_enabled": !opts.TLSConfig.DisableSSL,
	}).Info("Connecting to Vizier")

	conn, err := grpc.DialContext(ctx, opts.Address, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Vizier: %w", err)
	}

	client := pb.NewVizierServiceClient(conn)

	return &Client{
		conn:   conn,
		client: client,
		logger: logger,
	}, nil
}

// Close closes the connection to Vizier
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// ExecuteScript executes a PxL script on Vizier and returns the results
func (c *Client) ExecuteScript(ctx context.Context, clusterID, query string) error {
	req := &pb.ExecuteScriptRequest{
		ClusterId: clusterID,
		QueryStr:  query,
	}

	c.logger.WithFields(logrus.Fields{
		"cluster_id": clusterID,
		"query":      query,
	}).Info("Executing script")

	stream, err := c.client.ExecuteScript(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to execute script: %w", err)
	}

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to receive response: %w", err)
		}

		if resp.Status != nil && resp.Status.Code != 0 {
			c.logger.WithFields(logrus.Fields{
				"code":    resp.Status.Code,
				"message": resp.Status.Message,
			}).Error("Query execution error")
			return fmt.Errorf("query execution failed: %s", resp.Status.Message)
		}

		switch result := resp.Result.(type) {
		case *pb.ExecuteScriptResponse_MetaData:
			c.logger.WithFields(logrus.Fields{
				"table_name": result.MetaData.Name,
				"table_id":   result.MetaData.Id,
			}).Info("Received table metadata")

		case *pb.ExecuteScriptResponse_Data:
			batch := result.Data.Batch
			c.logger.WithFields(logrus.Fields{
				"num_rows": batch.NumRows,
				"num_cols": len(batch.Cols),
			}).Info("Received data batch")

			c.printTableData(batch)

			if result.Data.ExecutionStats != nil {
				stats := result.Data.ExecutionStats
				c.logger.WithFields(logrus.Fields{
					"timing_ns":         stats.Timing,
					"bytes_processed":   stats.BytesProcessed,
					"records_processed": stats.RecordsProcessed,
				}).Info("Execution statistics")
			}
		}
	}

	c.logger.Info("Script execution completed")
	return nil
}

// HealthCheck performs a health check on the Vizier cluster
func (c *Client) HealthCheck(ctx context.Context, clusterID string) error {
	req := &pb.HealthCheckRequest{
		ClusterId: clusterID,
	}

	c.logger.WithField("cluster_id", clusterID).Info("Performing health check")

	stream, err := c.client.HealthCheck(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to perform health check: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to receive health check response: %w", err)
	}

	if resp != nil && resp.Status != nil {
		if resp.Status.Code != 0 {
			c.logger.WithFields(logrus.Fields{
				"code":    resp.Status.Code,
				"message": resp.Status.Message,
			}).Error("Health check failed")
			return fmt.Errorf("health check failed: %s", resp.Status.Message)
		}
	}

	c.logger.Info("Health check passed")
	return nil
}

// printTableData prints the table data in a readable format
func (c *Client) printTableData(batch *pb.RowBatchData) {
	if len(batch.Cols) == 0 {
		c.logger.Info("No columns in batch")
		return
	}

	// Print column headers
	fmt.Printf("\n")
	for i, col := range batch.Cols {
		if i > 0 {
			fmt.Printf("\t")
		}
		fmt.Printf("%s", col.ColumnName)
	}
	fmt.Printf("\n")

	// Print separator
	for i := range batch.Cols {
		if i > 0 {
			fmt.Printf("\t")
		}
		fmt.Printf("--------")
	}
	fmt.Printf("\n")

	// Print data rows (simplified - assumes string data for demo)
	stringDataIdx := 0
	int64DataIdx := 0
	float64DataIdx := 0
	boolDataIdx := 0

	for row := int64(0); row < batch.NumRows; row++ {
		for colIdx, col := range batch.Cols {
			if colIdx > 0 {
				fmt.Printf("\t")
			}

			switch col.ColumnType {
			case pb.DataType_STRING:
				if stringDataIdx < len(batch.StringData) {
					fmt.Printf("%s", batch.StringData[stringDataIdx])
					stringDataIdx++
				} else {
					fmt.Printf("NULL")
				}
			case pb.DataType_INT64:
				if int64DataIdx < len(batch.Int64Data) {
					fmt.Printf("%d", batch.Int64Data[int64DataIdx])
					int64DataIdx++
				} else {
					fmt.Printf("NULL")
				}
			case pb.DataType_FLOAT64:
				if float64DataIdx < len(batch.Float64Data) {
					fmt.Printf("%.2f", batch.Float64Data[float64DataIdx])
					float64DataIdx++
				} else {
					fmt.Printf("NULL")
				}
			case pb.DataType_BOOLEAN:
				if boolDataIdx < len(batch.BoolData) {
					fmt.Printf("%t", batch.BoolData[boolDataIdx])
					boolDataIdx++
				} else {
					fmt.Printf("NULL")
				}
			default:
				fmt.Printf("?")
			}
		}
		fmt.Printf("\n")
	}
	fmt.Printf("\n")
}
