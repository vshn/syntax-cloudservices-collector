package exoscale

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/exoscale/egoscale/v2/oapi"
	"github.com/vshn/exoscale-metrics-collector/pkg/exofixtures"
	"github.com/vshn/exoscale-metrics-collector/pkg/reporting"
	exoscalev1 "github.com/vshn/provider-exoscale/apis/exoscale/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	k8s "sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectStorage gathers bucket data from exoscale provider and cluster and saves to the database
type ObjectStorage struct {
	k8sClient      k8s.Client
	exoscaleClient *egoscale.Client
	databaseURL    string
	billingDate    time.Time
}

// BucketDetail a k8s bucket object with relevant data
type BucketDetail struct {
	Organization, BucketName, Namespace string
}

// NewObjectStorage creates an ObjectStorage with the initial setup
func NewObjectStorage(exoscaleClient *egoscale.Client, k8sClient k8s.Client, databaseURL string, billingDate time.Time) (*ObjectStorage, error) {
	return &ObjectStorage{
		exoscaleClient: exoscaleClient,
		k8sClient:      k8sClient,
		databaseURL:    databaseURL,
		billingDate:    billingDate,
	}, nil
}

// Execute executes the main business logic for this application by gathering, matching and saving data to the database
func (o *ObjectStorage) Execute(ctx context.Context) error {
	logger := ctrl.LoggerFrom(ctx)
	s, err := reporting.NewStore(o.databaseURL, logger.WithName("reporting-store"))
	if err != nil {
		return fmt.Errorf("reporting.NewStore: %w", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			logger.Error(err, "unable to close")
		}
	}()

	if err := o.initialize(ctx, s); err != nil {
		return err
	}
	accumulated, err := o.accumulate(ctx)
	if err != nil {
		return err
	}
	return o.save(ctx, s, accumulated)
}

func (o *ObjectStorage) initialize(ctx context.Context, s *reporting.Store) error {
	logger := ctrl.LoggerFrom(ctx)

	fixtures := exofixtures.ObjectStorage
	if err := s.Initialize(ctx, fixtures.Products, []*db.Discount{&fixtures.Discount}, []*db.Query{&fixtures.Query}); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	logger.Info("initialized reporting db")
	return nil
}

func (o *ObjectStorage) accumulate(ctx context.Context) (map[Key]Aggregated, error) {
	detail, err := o.fetchManagedBucketsAndNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetchManagedBucketsAndNamespaces: %w", err)
	}
	aggregated, err := o.getBucketUsage(ctx, detail)
	if err != nil {
		return nil, fmt.Errorf("getBucketUsage: %w", err)
	}
	return aggregated, nil
}

func (o *ObjectStorage) save(ctx context.Context, s *reporting.Store, aggregatedObjects map[Key]Aggregated) error {
	logger := ctrl.LoggerFrom(ctx)

	if len(aggregatedObjects) == 0 {
		logger.Info("no buckets to be saved to the database")
		return nil
	}

	for _, aggregated := range aggregatedObjects {
		err := o.ensureBucketUsage(ctx, s, aggregated)
		if err != nil {
			logger.Error(err, "cannot save aggregated buckets service record to billing database")
			continue
		}
	}
	return nil
}

// getBucketUsage gets bucket usage from Exoscale and matches them with the bucket from the cluster
// If there are no buckets in Exoscale, the API will return an empty slice
func (o *ObjectStorage) getBucketUsage(ctx context.Context, bucketDetails []BucketDetail) (map[Key]Aggregated, error) {
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

func getAggregatedBuckets(ctx context.Context, sosBucketsUsage []oapi.SosBucketUsage, bucketDetails []BucketDetail) map[Key]Aggregated {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("Aggregating buckets by namespace")

	sosBucketsUsageMap := make(map[string]oapi.SosBucketUsage, len(sosBucketsUsage))
	for _, usage := range sosBucketsUsage {
		sosBucketsUsageMap[*usage.Name] = usage
	}

	aggregatedBuckets := make(map[Key]Aggregated)
	for _, bucketDetail := range bucketDetails {
		logger.V(1).Info("Checking bucket", "bucket", bucketDetail.BucketName)

		if bucketUsage, exists := sosBucketsUsageMap[bucketDetail.BucketName]; exists {
			logger.V(1).Info("Found exoscale bucket usage", "bucket", bucketUsage.Name, "bucket size", bucketUsage.Name)
			key := NewKey(bucketDetail.Namespace)
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

// ensureBucketUsage saves the aggregated buckets usage by namespace to the billing database
// To save the correct data to the database the function also matches a relevant product, Discount (if any) and Query.
// The storage usage is referred to a day before the application ran (yesterday)
func (o *ObjectStorage) ensureBucketUsage(ctx context.Context, store *reporting.Store, aggregatedBucket Aggregated) error {
	logger := ctrl.LoggerFrom(ctx)

	tokens, err := aggregatedBucket.DecodeKey()
	if err != nil {
		return fmt.Errorf("cannot decode key namespace-plan-dbtype - %s, organization %s, number of instances %f: %w",
			aggregatedBucket.Key,
			aggregatedBucket.Organization,
			aggregatedBucket.Value,
			err)
	}
	namespace := tokens[0]

	logger.Info("Saving buckets usage for namespace", "namespace", namespace, "storage used", aggregatedBucket.Value)
	organization := aggregatedBucket.Organization
	value := aggregatedBucket.Value

	sourceString := sosSourceString{
		ObjectType: exofixtures.SosType,
		provider:   exofixtures.Provider,
	}
	value, err = adjustStorageSizeUnit(value)
	if err != nil {
		return fmt.Errorf("adjustStorageSizeUnit(%v): %w", value, err)
	}

	return store.WriteRecord(ctx, reporting.Record{
		TenantSource:   organization,
		CategorySource: exofixtures.Provider + ":" + namespace,
		BillingDate:    o.billingDate,
		ProductSource:  sourceString.getSourceString(),
		DiscountSource: sourceString.getSourceString(),
		QueryName:      sourceString.getQuery(),
		Value:          value,
	})
}

func adjustStorageSizeUnit(value float64) (float64, error) {
	sosUnit := exofixtures.ObjectStorage.Query.Unit
	if sosUnit == exofixtures.DefaultUnitSos {
		return value / 1024 / 1024 / 1024, nil
	}
	return 0, fmt.Errorf("unknown Query unit %s", sosUnit)
}

type sosSourceString struct {
	exofixtures.ObjectType
	provider string
}

func (ss sosSourceString) getQuery() string {
	return strings.Join([]string{string(ss.ObjectType), ss.provider}, ":")
}

func (ss sosSourceString) getSourceString() string {
	return strings.Join([]string{string(ss.ObjectType), ss.provider}, ":")
}
