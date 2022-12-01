package sos

import (
	"context"
	"fmt"
	"time"

	"github.com/exoscale/egoscale/v2/oapi"
	"github.com/vshn/exoscale-metrics-collector/pkg/database"
	"github.com/vshn/exoscale-metrics-collector/pkg/service"

	pipeline "github.com/ccremer/go-command-pipeline"
	egoscale "github.com/exoscale/egoscale/v2"
	db "github.com/vshn/exoscale-metrics-collector/pkg/database"
	exoscalev1 "github.com/vshn/provider-exoscale/apis/exoscale/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	k8s "sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectStorage gathers bucket data from exoscale provider and cluster and saves to the database
type ObjectStorage struct {
	k8sClient         k8s.Client
	exoscaleClient    *egoscale.Client
	database          *db.SosDatabase
	bucketDetails     []BucketDetail
	aggregatedBuckets map[db.Key]db.Aggregated
}

// BucketDetail a k8s bucket object with relevant data
type BucketDetail struct {
	Organization, BucketName, Namespace string
}

// NewObjectStorage creates an ObjectStorage with the initial setup
func NewObjectStorage(exoscaleClient *egoscale.Client, k8sClient *k8s.Client, databaseURL string) ObjectStorage {
	return ObjectStorage{
		exoscaleClient: exoscaleClient,
		k8sClient:      *k8sClient,
		database: &db.SosDatabase{
			Database: db.Database{
				URL: databaseURL,
			},
		},
	}
}

// Execute executes the main business logic for this application by gathering, matching and saving data to the database
func (o *ObjectStorage) Execute(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Running metrics collector by step")

	p := pipeline.NewPipeline[context.Context]()
	p.WithSteps(
		p.NewStep("Fetch managed buckets", o.fetchManagedBuckets),
		p.NewStep("Get bucket usage", o.getBucketUsage),
		p.NewStep("Get billing date", o.getBillingDate),
		p.NewStep("Save to database", o.saveToDatabase),
	)
	return p.RunWithContext(ctx)
}

// getBucketUsage gets bucket usage from Exoscale and matches them with the bucket from the cluster
// If there are no buckets in Exoscale, the API will return an empty slice
func (o *ObjectStorage) getBucketUsage(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Fetching bucket usage from Exoscale")
	resp, err := o.exoscaleClient.ListSosBucketsUsageWithResponse(ctx)
	if err != nil {
		return err
	}

	aggregatedBuckets := getAggregatedBuckets(ctx, *resp.JSON200.SosBucketsUsage, o.bucketDetails)
	if len(aggregatedBuckets) == 0 {
		log.Info("There are no bucket usage to be saved in the database")
		return nil
	}

	o.aggregatedBuckets = aggregatedBuckets
	return nil
}

func (o *ObjectStorage) fetchManagedBuckets(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Fetching buckets from cluster")

	buckets := exoscalev1.BucketList{}
	log.V(1).Info("Listing buckets from cluster")
	err := o.k8sClient.List(ctx, &buckets)
	if err != nil {
		return fmt.Errorf("cannot list buckets: %w", err)
	}
	o.bucketDetails = addOrgAndNamespaceToBucket(ctx, buckets)
	return nil
}

func (o *ObjectStorage) saveToDatabase(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Creating a database connection")

	dctx := &database.Context{
		Context:           ctx,
		AggregatedObjects: &o.aggregatedBuckets,
	}

	err := o.database.Execute(dctx)
	if err != nil {
		log.Error(err, "Cannot save to database")
	}
	return nil
}

func (o *ObjectStorage) getBillingDate(_ context.Context) error {
	location, err := time.LoadLocation(service.ExoscaleTimeZone)
	if err != nil {
		return fmt.Errorf("cannot initialize location from time zone %s: %w", location, err)
	}
	now := time.Now().In(location)
	previousDay := now.Day() - 1
	o.database.BillingDate = time.Date(now.Year(), now.Month(), previousDay, service.ExoscaleBillingHour, 0, 0, 0, now.Location())
	return nil
}

func getAggregatedBuckets(ctx context.Context, sosBucketsUsage []oapi.SosBucketUsage, bucketDetails []BucketDetail) map[db.Key]db.Aggregated {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Aggregating buckets by namespace")

	sosBucketsUsageMap := make(map[string]oapi.SosBucketUsage, len(sosBucketsUsage))
	for _, usage := range sosBucketsUsage {
		sosBucketsUsageMap[*usage.Name] = usage
	}

	aggregatedBuckets := make(map[db.Key]db.Aggregated)
	for _, bucketDetail := range bucketDetails {
		log.V(1).Info("Checking bucket", "bucket", bucketDetail.BucketName)

		if bucketUsage, exists := sosBucketsUsageMap[bucketDetail.BucketName]; exists {
			log.V(1).Info("Found exoscale bucket usage", "bucket", bucketUsage.Name, "bucket size", bucketUsage.Name)
			key := db.NewKey(bucketDetail.Namespace)
			aggregatedBucket := aggregatedBuckets[key]
			aggregatedBucket.Key = key
			aggregatedBucket.Organization = bucketDetail.Organization
			aggregatedBucket.Value += float64(*bucketUsage.Size)
			aggregatedBuckets[key] = aggregatedBucket
		} else {
			log.Info("Could not find any bucket on exoscale", "bucket", bucketDetail.BucketName)
		}
	}
	return aggregatedBuckets
}

func addOrgAndNamespaceToBucket(ctx context.Context, buckets exoscalev1.BucketList) []BucketDetail {
	log := ctrl.LoggerFrom(ctx)
	log.V(1).Info("Gathering org and namespace from buckets")

	bucketDetails := make([]BucketDetail, 0, 10)
	for _, bucket := range buckets.Items {
		bucketDetail := BucketDetail{
			BucketName: bucket.Spec.ForProvider.BucketName,
		}
		if organization, exist := bucket.ObjectMeta.Labels[service.OrganizationLabel]; exist {
			bucketDetail.Organization = organization
		} else {
			// cannot get organization from bucket
			log.Info("Organization label is missing in bucket, skipping...",
				"label", service.OrganizationLabel,
				"bucket", bucket.Name)
			continue
		}
		if namespace, exist := bucket.ObjectMeta.Labels[service.NamespaceLabel]; exist {
			bucketDetail.Namespace = namespace
		} else {
			// cannot get namespace from bucket
			log.Info("Namespace label is missing in bucket, skipping...",
				"label", service.NamespaceLabel,
				"bucket", bucket.Name)
			continue
		}
		log.V(1).Info("Added namespace and organization to bucket",
			"bucket", bucket.Name,
			"namespace", bucketDetail.Namespace,
			"organization", bucketDetail.Organization)
		bucketDetails = append(bucketDetails, bucketDetail)
	}
	return bucketDetails
}
