package main

import (
	"fmt"
	"log"

	pb "github.com/wrongerror/observo-connector/proto"
)

func main() {
	// Test creating protobuf structures to verify compatibility

	// Test UInt128
	uint128 := &pb.UInt128{
		Low:  123456789,
		High: 987654321,
	}
	fmt.Printf("UInt128: %+v\n", uint128)

	// Test column data
	stringCol := &pb.StringColumn{
		Data: [][]byte{
			[]byte("hello"),
			[]byte("world"),
			[]byte("测试"), // UTF-8 test
		},
	}

	boolCol := &pb.BooleanColumn{
		Data: []bool{true, false, true},
	}

	int64Col := &pb.Int64Column{
		Data: []int64{100, 200, 300},
	}

	// Test Column with oneof
	columns := []*pb.Column{
		{
			ColData: &pb.Column_StringData{
				StringData: stringCol,
			},
		},
		{
			ColData: &pb.Column_BooleanData{
				BooleanData: boolCol,
			},
		},
		{
			ColData: &pb.Column_Int64Data{
				Int64Data: int64Col,
			},
		},
	}

	// Test RowBatchData
	batchData := &pb.RowBatchData{
		TableId: "test_table",
		Cols:    columns,
		NumRows: 3,
		Eow:     false,
		Eos:     false,
	}

	fmt.Printf("RowBatchData created successfully with %d columns and %d rows\n",
		len(batchData.Cols), batchData.NumRows)

	// Test Relation and ColumnInfo
	relation := &pb.Relation{
		Columns: []*pb.Relation_ColumnInfo{
			{
				ColumnName:         "name",
				ColumnType:         pb.DataType_STRING,
				ColumnDesc:         "Name column",
				ColumnSemanticType: pb.SemanticType_ST_NONE,
			},
			{
				ColumnName:         "active",
				ColumnType:         pb.DataType_BOOLEAN,
				ColumnDesc:         "Active status",
				ColumnSemanticType: pb.SemanticType_ST_NONE,
			},
			{
				ColumnName:         "count",
				ColumnType:         pb.DataType_INT64,
				ColumnDesc:         "Count value",
				ColumnSemanticType: pb.SemanticType_ST_NONE,
			},
		},
	}

	// Test QueryMetadata
	metadata := &pb.QueryMetadata{
		Relation: relation,
		Name:     "test_query",
		Id:       "query_123",
	}

	fmt.Printf("QueryMetadata created with %d columns\n", len(metadata.Relation.Columns))

	// Test QueryData
	queryData := &pb.QueryData{
		Batch: batchData,
		ExecutionStats: &pb.QueryExecutionStats{
			Timing: &pb.QueryTimingInfo{
				ExecutionTimeNs:   1000000,
				CompilationTimeNs: 500000,
			},
			BytesProcessed:   12345,
			RecordsProcessed: 3,
		},
	}

	fmt.Printf("QueryData created successfully\n")

	// Test ExecuteScriptResponse
	response := &pb.ExecuteScriptResponse{
		Status: &pb.Status{
			Code:    0,
			Message: "OK",
		},
		QueryId: "query_456",
		Result: &pb.ExecuteScriptResponse_Data{
			Data: queryData,
		},
	}

	fmt.Printf("ExecuteScriptResponse created successfully with query_id: %s\n", response.QueryId)

	// Test string data extraction (the critical UTF-8 part)
	for i, col := range columns {
		fmt.Printf("Column %d type: ", i)
		switch colData := col.ColData.(type) {
		case *pb.Column_StringData:
			fmt.Printf("STRING - data: ")
			for j, data := range colData.StringData.GetData() {
				fmt.Printf("[%d]=%s ", j, string(data))
			}
			fmt.Println()
		case *pb.Column_BooleanData:
			fmt.Printf("BOOLEAN - data: %v\n", colData.BooleanData.GetData())
		case *pb.Column_Int64Data:
			fmt.Printf("INT64 - data: %v\n", colData.Int64Data.GetData())
		default:
			fmt.Printf("UNKNOWN\n")
		}
	}

	log.Println("✅ All protobuf structures created and tested successfully!")
	log.Println("✅ UTF-8 string handling appears to be working correctly!")
}
