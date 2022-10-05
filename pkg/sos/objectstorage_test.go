package sos

import (
	"context"
	"github.com/exoscale/egoscale/v2/oapi"
	"github.com/stretchr/testify/assert"
	db "github.com/vshn/exoscale-metrics-collector/pkg/database"
	exoscalev1 "github.com/vshn/provider-exoscale/apis/exoscale/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"
)

func TestObjectStorage_GetBillingDate(t *testing.T) {
	t.Run("GivenContext_WhenGetBillingDate_ThenReturnYesterdayDate", func(t *testing.T) {
		// Given
		ctx := context.Background()
		utc, _ := time.LoadLocation("UTC")
		now := time.Now().In(utc)
		expected := time.Date(now.Year(), now.Month(), time.Now().Day()-1, 6, 0, 0, 0, now.Location())

		//When
		o := ObjectStorage{
			database: &db.Database{},
		}
		err := o.getBillingDate(ctx)

		// Then
		assert.NoError(t, err)
		assert.Equal(t, o.database.BillingDate, expected)
	})
}

func TestObjectStorage_GetAggregatedBuckets(t *testing.T) {
	tests := map[string]struct {
		givenSosBucketsUsage      []oapi.SosBucketUsage
		givenBucketDetails        []BucketDetail
		expectedAggregatedBuckets map[string]db.AggregatedBucket
	}{
		"GivenSosBucketsUsageAndBuckets_WhenMatch_ThenExpectAggregatedBucketObjects": {
			givenSosBucketsUsage: []oapi.SosBucketUsage{
				createSosBucketUsage("bucket-test-1", 1),
				createSosBucketUsage("bucket-test-2", 4),
				createSosBucketUsage("bucket-test-3", 9),
				createSosBucketUsage("bucket-test-4", 0),
				createSosBucketUsage("bucket-test-5", 5),
			},
			givenBucketDetails: []BucketDetail{
				createBucketDetail("bucket-test-1", "default", "orgA"),
				createBucketDetail("bucket-test-2", "alpha", "orgB"),
				createBucketDetail("bucket-test-3", "alpha", "orgB"),
				createBucketDetail("bucket-test-4", "omega", "orgC"),
				createBucketDetail("no-metrics-bucket", "beta", "orgD"),
			},
			expectedAggregatedBuckets: map[string]db.AggregatedBucket{
				"default": createAggregatedBucket("orgA", 1),
				"alpha":   createAggregatedBucket("orgB", 13),
				"omega":   createAggregatedBucket("orgC", 0),
			},
		},
		"GivenSosBucketsUsageAndBuckets_WhenMatch_ThenExpectNoAggregatedBucketObjects": {
			givenSosBucketsUsage: []oapi.SosBucketUsage{
				createSosBucketUsage("bucket-test-1", 1),
				createSosBucketUsage("bucket-test-2", 4),
			},
			givenBucketDetails: []BucketDetail{
				createBucketDetail("bucket-test-3", "default", "orgA"),
				createBucketDetail("bucket-test-4", "alpha", "orgB"),
				createBucketDetail("bucket-test-5", "alpha", "orgB"),
			},
			expectedAggregatedBuckets: map[string]db.AggregatedBucket{},
		},
		"GivenSosBucketsUsageAndBuckets_WhenSosBucketsUsageEmpty_ThenExpectNoAggregatedBucketObjects": {
			givenSosBucketsUsage: []oapi.SosBucketUsage{
				createSosBucketUsage("bucket-test-1", 1),
				createSosBucketUsage("bucket-test-2", 4),
			},
			givenBucketDetails:        []BucketDetail{},
			expectedAggregatedBuckets: map[string]db.AggregatedBucket{},
		},
		"GivenSosBucketsUsageAndBuckets_WhenNoBuckets_ThenExpectNoAggregatedBucketObjects": {
			givenSosBucketsUsage: []oapi.SosBucketUsage{},
			givenBucketDetails: []BucketDetail{
				createBucketDetail("bucket-test-3", "default", "orgA"),
				createBucketDetail("bucket-test-4", "alpha", "orgB"),
			},
			expectedAggregatedBuckets: map[string]db.AggregatedBucket{},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Given
			ctx := context.Background()

			// When
			aggregatedBuckets := getAggregatedBuckets(ctx, tc.givenSosBucketsUsage, tc.givenBucketDetails)

			// Then
			assert.Equal(t, aggregatedBuckets, tc.expectedAggregatedBuckets)
		})
	}
}

func TestObjectStorage_AadOrgAndNamespaceToBucket(t *testing.T) {
	tests := map[string]struct {
		givenBucketList       exoscalev1.BucketList
		expectedBucketDetails []BucketDetail
	}{
		"GivenBucketListFromExoscale_WhenOrgAndNamespaces_ThenExpectBucketDetailObjects": {
			givenBucketList: exoscalev1.BucketList{
				Items: []exoscalev1.Bucket{
					createBucket("bucket-1", "alpha", "orgA"),
					createBucket("bucket-2", "beta", "orgB"),
					createBucket("bucket-3", "alpha", "orgA"),
					createBucket("bucket-4", "omega", "orgB"),
					createBucket("bucket-5", "theta", "orgC"),
				},
			},
			expectedBucketDetails: []BucketDetail{
				createBucketDetail("bucket-1", "alpha", "orgA"),
				createBucketDetail("bucket-2", "beta", "orgB"),
				createBucketDetail("bucket-3", "alpha", "orgA"),
				createBucketDetail("bucket-4", "omega", "orgB"),
				createBucketDetail("bucket-5", "theta", "orgC"),
			},
		},
		"GivenBucketListFromExoscale_WhenNoOrgOrNamespaces_ThenExpectNoBucketDetailObjects": {
			givenBucketList: exoscalev1.BucketList{
				Items: []exoscalev1.Bucket{
					createBucket("bucket-1", "", "orgA"),
					createBucket("bucket-2", "beta", ""),
					createBucket("bucket-3", "", ""),
				},
			},
			expectedBucketDetails: []BucketDetail{},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Given
			ctx := context.Background()

			// When
			bucketDetails := addOrgAndNamespaceToBucket(ctx, tc.givenBucketList)

			// Then
			assert.Equal(t, tc.expectedBucketDetails, bucketDetails)
		})
	}
}

func createBucket(name, namespace, organization string) exoscalev1.Bucket {
	labels := make(map[string]string)
	if namespace != "" {
		labels[namespaceLabel] = namespace
	}
	if organization != "" {
		labels[organizationLabel] = organization
	}
	return exoscalev1.Bucket{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: exoscalev1.BucketSpec{
			ForProvider: exoscalev1.BucketParameters{
				BucketName: name,
			},
		},
	}
}

func createAggregatedBucket(organization string, size float64) db.AggregatedBucket {
	return db.AggregatedBucket{
		Organization: organization,
		StorageUsed:  size,
	}
}

func createBucketDetail(bucketName, namespace, organization string) BucketDetail {
	return BucketDetail{
		Organization: organization,
		BucketName:   bucketName,
		Namespace:    namespace,
	}
}

func createSosBucketUsage(bucketName string, size int) oapi.SosBucketUsage {
	date := time.Now()
	actualSize := int64(size)
	zone := oapi.ZoneName("ch-gva-2")
	return oapi.SosBucketUsage{
		CreatedAt: &date,
		Name:      &bucketName,
		Size:      &actualSize,
		ZoneName:  &zone,
	}
}
