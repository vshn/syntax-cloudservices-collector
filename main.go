package main

import (
	"context"
	"fmt"
	egoscale "github.com/exoscale/egoscale/v2"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	keyEnvVariable        = "EXOSCALE_API_KEY"
	secretEnvVariable     = "EXOSCALE_API_SECRET"
	k8sApiPrefix          = "K8S_API_"
	k8sTokenPrefix        = "K8S_TOKEN_"
	orgAnnotation         = "appuio.io/organization"
	objectBucketsResource = schema.GroupVersionResource{Group: "appcat.vshn.io", Version: "v1", Resource: "objectbuckets"}
	namespaceResource     = schema.GroupVersionResource{Group: "", Version: "v1", Resource: "namespaces"}
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

func sync(ctx context.Context) error {
	exoscaleApiKey, exoscaleApiSecret, k8sConfigs := cfg()

	// Fetch bucket name to namespace/tenant information lookup table from the configured k8s clusters
	namespaceInfoByBucket, err := generateNamespaceLookupMap(ctx, k8sConfigs)
	if err != nil {
		return err
	}

	// Fetch actual billing data from Exoscale
	client, err := egoscale.NewClient(exoscaleApiKey, exoscaleApiSecret, egoscale.ClientOptWithAPIEndpoint("https://api-ch-gva-2.exoscale.com"))
	if err != nil {
		return err
	}
	resp, err := client.ListSosBucketsUsageWithResponse(ctx)
	if err != nil {
		return err
	}
	for _, v := range *resp.JSON200.SosBucketsUsage {
		fmt.Printf("name: %s, zoneName: %s, size: %d, createdAt: %s, namespace info: %s\n", *v.Name, *v.ZoneName, *v.Size, *v.CreatedAt, namespaceInfoByBucket[*v.Name])
	}

	return nil
}
