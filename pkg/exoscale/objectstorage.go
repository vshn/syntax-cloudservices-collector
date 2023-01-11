package exoscale

import (
	"context"
	"fmt"
	"time"

	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/exoscale/egoscale/v2/oapi"
	db "github.com/vshn/exoscale-metrics-collector/pkg/dbaas"
	exoscalev1 "github.com/vshn/provider-exoscale/apis/exoscale/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	k8s "sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectStorage gathers bucket data from exoscale provider and cluster and saves to the database
type ObjectStorage struct {
	k8sClient      k8s.Client
	exoscaleClient *egoscale.Client
	database       *db.SosDatabase
}

// BucketDetail a k8s bucket object with relevant data
type BucketDetail struct {
	Organization, BucketName, Namespace string
}

// NewObjectStorage creates an ObjectStorage with the initial setup
func NewObjectStorage(exoscaleClient *egoscale.Client, k8sClient k8s.Client, databaseURL string) (*ObjectStorage, error) {
	location, err := time.LoadLocation(timeZone)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize location from time zone %s: %w", location, err)
	}
	now := time.Now().In(location)
	previousDay := now.Day() - 1
	billingDate := time.Date(now.Year(), now.Month(), previousDay, billingHour, 0, 0, 0, now.Location())

	return &ObjectStorage{
		exoscaleClient: exoscaleClient,
		k8sClient:      k8sClient,
		database: &db.SosDatabase{
			URL:         databaseURL,
			BillingDate: billingDate,
		},
	}, nil
}

// Execute executes the main business logic for this application by gathering, matching and saving data to the database
func (o *ObjectStorage) Execute(ctx context.Context) error {
	logger := ctrl.LoggerFrom(ctx)

	detail, err := o.fetchManagedBucketsAndNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("fetchManagedBucketsAndNamespaces: %w", err)
	}
	aggregated, err := o.getBucketUsage(ctx, detail)
	if err != nil {
		return fmt.Errorf("getBucketUsage: %w", err)
	}
	if len(aggregated) == 0 {
		logger.Info("no buckets to be saved to the database")
		return nil
	}

	err = o.database.Execute(ctx, aggregated)
	if err != nil {
		return fmt.Errorf("db execute: %w", err)
	}
	return nil
}

// getBucketUsage gets bucket usage from Exoscale and matches them with the bucket from the cluster
// If there are no buckets in Exoscale, the API will return an empty slice
func (o *ObjectStorage) getBucketUsage(ctx context.Context, bucketDetails []BucketDetail) (map[db.Key]db.Aggregated, error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("Fetching bucket usage from Exoscale")
	resp, err := o.exoscaleClient.ListSosBucketsUsageWithResponse(ctx)
	if err != nil {
		return nil, err
	}

	aggregatedBuckets := getAggregatedBuckets(ctx, *resp.JSON200.SosBucketsUsage, bucketDetails)
	if len(aggregatedBuckets) == 0 {
		logger.Info("There are no bucket usage to be saved in the database")
		return nil, nil
	}

	return aggregatedBuckets, nil
}

func (o *ObjectStorage) fetchManagedBucketsAndNamespaces(ctx context.Context) ([]BucketDetail, error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("Fetching buckets and namespaces from cluster")

	buckets := exoscalev1.BucketList{}
	logger.V(1).Info("Listing buckets from cluster")
	err := o.k8sClient.List(ctx, &buckets)
	if err != nil {
		return nil, fmt.Errorf("cannot list buckets: %w", err)
	}

	logger.V(1).Info("Listing namespaces from cluster")
	namespaces, err := fetchNamespaceWithOrganizationMap(ctx, o.k8sClient)
	if err != nil {
		return nil, fmt.Errorf("cannot list namespaces: %w", err)
	}

	return addOrgAndNamespaceToBucket(ctx, buckets, namespaces), nil
}

func getAggregatedBuckets(ctx context.Context, sosBucketsUsage []oapi.SosBucketUsage, bucketDetails []BucketDetail) map[db.Key]db.Aggregated {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("Aggregating buckets by namespace")

	sosBucketsUsageMap := make(map[string]oapi.SosBucketUsage, len(sosBucketsUsage))
	for _, usage := range sosBucketsUsage {
		sosBucketsUsageMap[*usage.Name] = usage
	}

	aggregatedBuckets := make(map[db.Key]db.Aggregated)
	for _, bucketDetail := range bucketDetails {
		logger.V(1).Info("Checking bucket", "bucket", bucketDetail.BucketName)

		if bucketUsage, exists := sosBucketsUsageMap[bucketDetail.BucketName]; exists {
			logger.V(1).Info("Found exoscale bucket usage", "bucket", bucketUsage.Name, "bucket size", bucketUsage.Name)
			key := db.NewKey(bucketDetail.Namespace)
			aggregatedBucket := aggregatedBuckets[key]
			aggregatedBucket.Key = key
			aggregatedBucket.Organization = bucketDetail.Organization
			aggregatedBucket.Value += float64(*bucketUsage.Size)
			aggregatedBuckets[key] = aggregatedBucket
		} else {
			logger.Info("Could not find any bucket on exoscale", "bucket", bucketDetail.BucketName)
		}
	}
	return aggregatedBuckets
}

func addOrgAndNamespaceToBucket(ctx context.Context, buckets exoscalev1.BucketList, namespaces map[string]string) []BucketDetail {
	logger := ctrl.LoggerFrom(ctx)
	logger.V(1).Info("Gathering org and namespace from buckets")

	bucketDetails := make([]BucketDetail, 0, 10)
	for _, bucket := range buckets.Items {
		bucketDetail := BucketDetail{
			BucketName: bucket.Spec.ForProvider.BucketName,
		}
		if namespace, exist := bucket.ObjectMeta.Labels[namespaceLabel]; exist {
			organization, ok := namespaces[namespace]
			if !ok {
				// cannot find namespace in namespace list
				logger.Info("Namespace not found in namespace list, skipping...",
					"namespace", namespace,
					"bucket", bucket.Name)
				continue
			}
			bucketDetail.Namespace = namespace
			bucketDetail.Organization = organization
		} else {
			// cannot get namespace from bucket
			logger.Info("Namespace label is missing in bucket, skipping...",
				"label", namespaceLabel,
				"bucket", bucket.Name)
			continue
		}
		logger.V(1).Info("Added namespace and organization to bucket",
			"bucket", bucket.Name,
			"namespace", bucketDetail.Namespace,
			"organization", bucketDetail.Organization)
		bucketDetails = append(bucketDetails, bucketDetail)
	}
	return bucketDetails
}
