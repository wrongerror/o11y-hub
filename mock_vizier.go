package main

import (
	"fmt"
	"log"
	"net"

	pb "github.com/wrongerror/observo-connector/proto"
	"google.golang.org/grpc"
)

type mockVizierServer struct {
	pb.UnimplementedVizierServiceServer
}

func (s *mockVizierServer) ExecuteScript(req *pb.ExecuteScriptRequest, stream pb.VizierService_ExecuteScriptServer) error {
	log.Printf("📥 Received ExecuteScript request: %s", req.QueryStr[:50])

	// Send metadata first
	metadata := &pb.QueryMetadata{
		Id:   "test_table_1",
		Name: "http_overview_results",
		Relation: &pb.Relation{
			Columns: []*pb.Relation_ColumnInfo{
				{
					ColumnName:         "namespace",
					ColumnType:         pb.DataType_STRING,
					ColumnDesc:         "Kubernetes namespace",
					ColumnSemanticType: pb.SemanticType_ST_NAMESPACE_NAME,
				},
				{
					ColumnName:         "service",
					ColumnType:         pb.DataType_STRING,
					ColumnDesc:         "Service name",
					ColumnSemanticType: pb.SemanticType_ST_SERVICE_NAME,
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
			},
		},
	}

	err := stream.Send(&pb.ExecuteScriptResponse{
		Status:  &pb.Status{Code: 0, Message: "OK"},
		QueryId: "query_123",
		Result: &pb.ExecuteScriptResponse_MetaData{
			MetaData: metadata,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send metadata: %w", err)
	}
	log.Printf("📤 Sent table metadata for: %s", metadata.Name)

	// Send sample data
	batchData := &pb.RowBatchData{
		TableId: "test_table_1",
		NumRows: 3,
		Cols: []*pb.Column{
			{
				ColData: &pb.Column_StringData{
					StringData: &pb.StringColumn{
						Data: [][]byte{
							[]byte("default"),
							[]byte("kube-system"),
							[]byte("pixie-system"),
						},
					},
				},
			},
			{
				ColData: &pb.Column_StringData{
					StringData: &pb.StringColumn{
						Data: [][]byte{
							[]byte("frontend"),
							[]byte("backend"),
							[]byte("monitoring"),
						},
					},
				},
			},
			{
				ColData: &pb.Column_Int64Data{
					Int64Data: &pb.Int64Column{
						Data: []int64{1500, 850, 320},
					},
				},
			},
			{
				ColData: &pb.Column_Float64Data{
					Float64Data: &pb.Float64Column{
						Data: []float64{45.2, 23.7, 12.1},
					},
				},
			},
		},
		Eos: true,
	}

	queryData := &pb.QueryData{
		Batch: batchData,
		ExecutionStats: &pb.QueryExecutionStats{
			Timing: &pb.QueryTimingInfo{
				ExecutionTimeNs:   2500000000, // 2.5 seconds
				CompilationTimeNs: 500000000,  // 0.5 seconds
			},
			BytesProcessed:   8192,
			RecordsProcessed: 3,
		},
	}

	err = stream.Send(&pb.ExecuteScriptResponse{
		Status:  &pb.Status{Code: 0, Message: "OK"},
		QueryId: "query_123",
		Result: &pb.ExecuteScriptResponse_Data{
			Data: queryData,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send data: %w", err)
	}
	log.Printf("📤 Sent batch data with %d rows", batchData.NumRows)

	return nil
}

func (s *mockVizierServer) HealthCheck(req *pb.HealthCheckRequest, stream pb.VizierService_HealthCheckServer) error {
	err := stream.Send(&pb.HealthCheckResponse{
		Status: &pb.Status{Code: 0, Message: "OK"},
	})
	return err
}

func main() {
	lis, err := net.Listen("tcp", ":50300")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterVizierServiceServer(s, &mockVizierServer{})

	log.Println("🚀 Mock Vizier server starting on :50300")
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
