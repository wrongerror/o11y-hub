package main

import (
	"fmt"
	"log"
	"net"
	"time"

	pb "github.com/wrongerror/observo-connector/proto"
	"google.golang.org/grpc"
)

// MockVizierServer implements the VizierService for testing
type MockVizierServer struct {
	pb.UnimplementedVizierServiceServer
}

// HealthCheck implements the health check endpoint
func (s *MockVizierServer) HealthCheck(req *pb.HealthCheckRequest, stream pb.VizierService_HealthCheckServer) error {
	log.Printf("Health check requested for cluster: %s", req.ClusterId)

	response := &pb.HealthCheckResponse{
		Status: &pb.Status{
			Code:    0,
			Message: "OK",
		},
	}

	return stream.Send(response)
}

// ExecuteScript implements the script execution endpoint
func (s *MockVizierServer) ExecuteScript(req *pb.ExecuteScriptRequest, stream pb.VizierService_ExecuteScriptServer) error {
	log.Printf("Script execution requested for cluster: %s", req.ClusterId)
	log.Printf("Query: %s", req.QueryStr)

	queryId := fmt.Sprintf("mock-query-%d", time.Now().Unix())

	// Send metadata first
	metadata := &pb.QueryMetadata{
		Relation: &pb.Relation{
			Columns: []*pb.Relation_ColumnInfo{
				{
					ColumnName:         "service",
					ColumnType:         pb.DataType_STRING,
					ColumnDesc:         "Service name",
					ColumnSemanticType: pb.SemanticType_ST_SERVICE_NAME,
				},
				{
					ColumnName:         "namespace",
					ColumnType:         pb.DataType_STRING,
					ColumnDesc:         "Kubernetes namespace",
					ColumnSemanticType: pb.SemanticType_ST_NAMESPACE_NAME,
				},
				{
					ColumnName:         "request_count",
					ColumnType:         pb.DataType_INT64,
					ColumnDesc:         "Number of requests",
					ColumnSemanticType: pb.SemanticType_ST_NONE,
				},
				{
					ColumnName:         "avg_latency_ms",
					ColumnType:         pb.DataType_FLOAT64,
					ColumnDesc:         "Average latency in milliseconds",
					ColumnSemanticType: pb.SemanticType_ST_DURATION_NS,
				},
				{
					ColumnName:         "error_rate",
					ColumnType:         pb.DataType_FLOAT64,
					ColumnDesc:         "Error rate percentage",
					ColumnSemanticType: pb.SemanticType_ST_PERCENT,
				},
			},
		},
		Name: "http_overview_result",
		Id:   "table_123",
	}

	metadataResponse := &pb.ExecuteScriptResponse{
		Status:  &pb.Status{Code: 0, Message: "OK"},
		QueryId: queryId,
		Result: &pb.ExecuteScriptResponse_MetaData{
			MetaData: metadata,
		},
	}

	if err := stream.Send(metadataResponse); err != nil {
		return err
	}

	log.Println("Sent metadata response")

	// Create sample data
	stringCol1 := &pb.StringColumn{
		Data: [][]byte{
			[]byte("frontend"),
			[]byte("backend"),
			[]byte("database"),
		},
	}

	stringCol2 := &pb.StringColumn{
		Data: [][]byte{
			[]byte("default"),
			[]byte("default"),
			[]byte("data"),
		},
	}

	int64Col := &pb.Int64Column{
		Data: []int64{1234, 5678, 90},
	}

	float64Col1 := &pb.Float64Column{
		Data: []float64{45.6, 23.4, 89.1},
	}

	float64Col2 := &pb.Float64Column{
		Data: []float64{0.02, 0.01, 0.05},
	}

	// Create batch data
	batchData := &pb.RowBatchData{
		TableId: "table_123",
		Cols: []*pb.Column{
			{ColData: &pb.Column_StringData{StringData: stringCol1}},
			{ColData: &pb.Column_StringData{StringData: stringCol2}},
			{ColData: &pb.Column_Int64Data{Int64Data: int64Col}},
			{ColData: &pb.Column_Float64Data{Float64Data: float64Col1}},
			{ColData: &pb.Column_Float64Data{Float64Data: float64Col2}},
		},
		NumRows: 3,
		Eow:     false,
		Eos:     true,
	}

	queryData := &pb.QueryData{
		Batch: batchData,
		ExecutionStats: &pb.QueryExecutionStats{
			Timing: &pb.QueryTimingInfo{
				ExecutionTimeNs:   2000000000, // 2 seconds
				CompilationTimeNs: 500000000,  // 0.5 seconds
			},
			BytesProcessed:   12345,
			RecordsProcessed: 3,
		},
	}

	dataResponse := &pb.ExecuteScriptResponse{
		Status:  &pb.Status{Code: 0, Message: "OK"},
		QueryId: queryId,
		Result: &pb.ExecuteScriptResponse_Data{
			Data: queryData,
		},
	}

	if err := stream.Send(dataResponse); err != nil {
		return err
	}

	log.Println("Sent data response with sample HTTP metrics")

	return nil
}

func main() {
	lis, err := net.Listen("tcp", ":50300")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	server := grpc.NewServer()
	pb.RegisterVizierServiceServer(server, &MockVizierServer{})

	log.Println("🚀 Mock Pixie Vizier server starting on :50300")
	log.Println("📊 Will return sample HTTP metrics data for testing")

	if err := server.Serve(lis); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}
