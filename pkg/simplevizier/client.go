package simplevizier

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	pb "github.com/wrongerror/observo-connector/proto"
)

type SimpleClient struct {
	conn      *grpc.ClientConn
	client    pb.VizierServiceClient
	token     string
	clusterID string
}

func NewSimpleClient(address, token, clusterID string) (*SimpleClient, error) {
	// Load the certificate
	cert, err := tls.LoadX509KeyPair("certs/client.crt", "certs/client.key")
	if err != nil {
		return nil, fmt.Errorf("failed to load client certs: %v", err)
	}

	// Load CA certificate
	caCert, err := os.ReadFile("certs/ca.crt")
	if err != nil {
		return nil, fmt.Errorf("failed to read CA cert: %v", err)
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// Create TLS configuration
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            caCertPool,
		ServerName:         "withpixie.ai",
		InsecureSkipVerify: true, // Skip cert verification for testing
	}

	// Create gRPC connection
	conn, err := grpc.Dial(address, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %v", err)
	}

	client := pb.NewVizierServiceClient(conn)

	return &SimpleClient{
		conn:      conn,
		client:    client,
		token:     token,
		clusterID: clusterID,
	}, nil
}

func (c *SimpleClient) ExecuteScript(query string) error {
	ctx := context.Background()

	// Add authentication metadata
	md := metadata.New(map[string]string{
		"authorization": c.token,
	})
	ctx = metadata.NewOutgoingContext(ctx, md)

	// Create request
	req := &pb.ExecuteScriptRequest{
		QueryStr:  query,
		ClusterId: c.clusterID,
		Mutation:  false,
	}

	// Execute script
	stream, err := c.client.ExecuteScript(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to execute script: %v", err)
	}

	// Receive first response
	resp, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("failed to receive first response: %v", err)
	}

	if resp.Status != nil && resp.Status.GetCode() != 0 {
		return fmt.Errorf("script execution failed with status: %d, message: %s", resp.Status.GetCode(), resp.Status.GetMessage())
	}

	fmt.Printf("Query ID: %s\n", resp.QueryId)

	// Receive all responses
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to receive response: %v", err)
		}

		fmt.Printf("Received response with status: %v\n", resp.Status)
	}

	return nil
}

func (c *SimpleClient) Close() error {
	return c.conn.Close()
}
