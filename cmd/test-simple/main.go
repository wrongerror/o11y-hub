package main

import (
	"log"
	"os"

	"github.com/wrongerror/observo-connector/pkg/simplevizier"
)

func main() {
	token := os.Getenv("PIXIE_API_TOKEN")
	if token == "" {
		log.Fatal("PIXIE_API_TOKEN not set")
	}

	clusterID := os.Getenv("PIXIE_CLUSTER_ID")
	if clusterID == "" {
		log.Fatal("PIXIE_CLUSTER_ID not set")
	}

	client, err := simplevizier.NewSimpleClient("withpixie.ai:443", token, clusterID)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	script := `
import px
df = px.DataFrame('http_events', start_time='-30s')
df = df.head(5)
px.display(df)
`

	log.Println("Executing script...")
	if err := client.ExecuteScript(script); err != nil {
		log.Fatalf("Script execution failed: %v", err)
	}

	log.Println("Script executed successfully!")
}
