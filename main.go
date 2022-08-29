package main

import (
	"context"
	"fmt"
	egoscale "github.com/exoscale/egoscale/v2"
	"os"
	"time"
)

var (
	// these variables are populated by Goreleaser when releasing
	version = "unknown"
	commit  = "-dirty-"
	date    = time.Now().Format("2006-01-02")
	appName = "exoscale-metrics-collector"

	// constants
	keyEnvVariable    = "EXOSCALE_API_KEY"
	secretEnvVariable = "EXOSCALE_API_SECRET"
)

func main() {
	ctx := context.Background()
	err := sync(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func cfg() (string, string) {
	exoscaleApiKey := os.Getenv(keyEnvVariable)
	if exoscaleApiKey == "" {
		fmt.Fprintf(os.Stderr, "ERROR: Environment variable %s must be set\n", keyEnvVariable)
		os.Exit(1)
	}

	exoscaleApiSecret := os.Getenv(secretEnvVariable)
	if exoscaleApiSecret == "" {
		fmt.Fprintf(os.Stderr, "ERROR: Environment variable %s must be set\n", secretEnvVariable)
		os.Exit(1)
	}

	return exoscaleApiKey, exoscaleApiSecret
}

func sync(ctx context.Context) error {
	exoscaleApiKey, exoscaleApiSecret := cfg()

	client, err := egoscale.NewClient(exoscaleApiKey, exoscaleApiSecret, egoscale.ClientOptWithAPIEndpoint("https://api-ch-gva-2.exoscale.com"))
	if err != nil {
		return err
	}

	resp, err := client.ListSosBucketsUsageWithResponse(ctx)
	if err != nil {
		return err
	}

	for _, v := range *resp.JSON200.SosBucketsUsage {
		fmt.Printf("name: %s, zoneName: %s, size: %d, createdAt: %s\n", *v.Name, *v.ZoneName, *v.Size, *v.CreatedAt)
	}

	return nil
}
