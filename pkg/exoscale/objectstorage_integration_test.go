//go:build integration

package exoscale

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/suite"
	"github.com/vshn/exoscale-metrics-collector/pkg/exofixtures"
	"github.com/vshn/exoscale-metrics-collector/pkg/reporting"
	"github.com/vshn/exoscale-metrics-collector/pkg/test"
	exoscalev1 "github.com/vshn/provider-exoscale/apis/exoscale/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ObjectStorageTestSuite struct {
	test.Suite
	billingDate time.Time
}

func (ts *ObjectStorageTestSuite) SetupSuite() {
	exoscaleCRDPaths := os.Getenv("EXOSCALE_CRDS_PATH")
	ts.Require().NotZero(exoscaleCRDPaths, "missing env variable EXOSCALE_CRDS_PATH")

	ts.SetupEnv([]string{exoscaleCRDPaths})

	ts.RegisterScheme(exoscalev1.SchemeBuilder.AddToScheme)
}

type source struct {
	namespace   string
	tenant      string
	objectType  exofixtures.ObjectType
	billingDate time.Time
}

func (ts *ObjectStorageTestSuite) TestMetrics() {
	assert := ts.Assert()
	ctx := ts.Context

	o, cancel := ts.setupObjectStorage()
	defer cancel()

	expectedQuantities := map[string]float64{
		"example-project": 932.253897190094,
		"next-big-thing":  0,
	}
	nameNsMap := map[string]string{
		"example-project-a": "example-project",
		"example-project-b": "example-project",
		"next-big-thing-a":  "next-big-thing",
	}
	nsTenantMap := map[string]string{
		"example-project": "example-company",
		"next-big-thing":  "big-corporation",
	}
	ts.ensureBuckets(nameNsMap)

	createdNs := make(map[string]bool)
	for _, ns := range nameNsMap {
		if _, ok := createdNs[ns]; !ok {
			ts.EnsureNS(ns, map[string]string{organizationLabel: nsTenantMap[ns]})
			createdNs[ns] = true
		}
	}

	assert.NoError(o.Execute(ctx))

	store, err := reporting.NewStore(ts.DatabaseURL, ts.Logger)
	assert.NoError(err)
	defer func() {
		assert.NoError(store.Close())
	}()

	// a bit pointless to use a transaction for checking the results but I wanted to avoid exposing something
	// which should not be used outside test code.
	assert.NoError(store.WithTransaction(ctx, func(tx *sqlx.Tx) error {
		dt, err := reporting.GetDateTime(ctx, tx, ts.billingDate)
		if !assert.NoError(err) || !assert.NotZero(dt) {
			return fmt.Errorf("no dateTime found(%q): %w (nil? %v)", ts.billingDate, err, dt)
		}

		for ns, expectedQuantity := range expectedQuantities {
			fact, err := ts.getFact(ctx, tx, ts.billingDate, dt, source{
				namespace:   ns,
				tenant:      nsTenantMap[ns],
				objectType:  exofixtures.SosType,
				billingDate: ts.billingDate,
			})
			assert.NoError(err, ns)

			assert.NotNil(fact, ns)
			assert.Equal(expectedQuantity, fact.Quantity, ns)
		}
		return nil
	}))
}

func (ts *ObjectStorageTestSuite) getFact(ctx context.Context, tx *sqlx.Tx, date time.Time, dt *db.DateTime, src source) (*db.Fact, error) {
	sourceString := sosSourceString{
		ObjectType: src.objectType,
		provider:   exofixtures.Provider,
	}
	record := reporting.Record{
		TenantSource:   src.tenant,
		CategorySource: exofixtures.Provider + ":" + src.namespace,
		BillingDate:    date,
		ProductSource:  sourceString.getSourceString(),
		DiscountSource: sourceString.getSourceString(),
		QueryName:      sourceString.getQuery(),
	}

	query, err := reporting.GetQueryByName(ctx, tx, record.QueryName)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	tenant, err := reporting.GetTenantBySource(ctx, tx, record.TenantSource)
	if err != nil {
		return nil, fmt.Errorf("tenant: %w", err)
	}

	category, err := reporting.GetCategory(ctx, tx, record.CategorySource)
	if err != nil {
		return nil, fmt.Errorf("category: %w", err)
	}

	product, err := reporting.GetBestMatchingProduct(ctx, tx, record.ProductSource, record.BillingDate)
	if err != nil {
		return nil, fmt.Errorf("product: %w", err)
	}

	discount, err := reporting.GetBestMatchingDiscount(ctx, tx, record.DiscountSource, record.BillingDate)
	if err != nil {
		return nil, fmt.Errorf("discount: %w", err)
	}

	fact, err := reporting.GetByFact(ctx, tx, &db.Fact{
		DateTimeId: dt.Id,
		QueryId:    query.Id,
		TenantId:   tenant.Id,
		CategoryId: category.Id,
		ProductId:  product.Id,
		DiscountId: discount.Id,
	})
	if err != nil {
		return nil, fmt.Errorf("fact: %w", err)
	}
	return fact, nil
}

func (ts *ObjectStorageTestSuite) ensureBuckets(nameNsMap map[string]string) {
	resources := make([]client.Object, 0)
	for name, ns := range nameNsMap {
		resources = append(resources, &exoscalev1.Bucket{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{namespaceLabel: ns},
			},
			Spec: exoscalev1.BucketSpec{
				ForProvider: exoscalev1.BucketParameters{BucketName: name},
			},
		})
	}
	ts.EnsureResources(resources...)
}

func (ts *ObjectStorageTestSuite) setupObjectStorage() (*ObjectStorage, func()) {
	assert := ts.Assert()
	httpClient, cancel, err := ts.RequestRecorder("testdata/exoscale/" + ts.T().Name())

	apiKey := os.Getenv("EXOSCALE_API_KEY")
	secret := os.Getenv("EXOSCALE_API_SECRET")
	if apiKey != "" && secret != "" {
		ts.T().Log("api key & secret set")
	} else {
		// override empty values since otherwise egoscale complains
		apiKey = "NOTVALID"
		secret = "NOTVALIDSECRET"
		ts.T().Log("api key or secret not set")
	}

	exoClient, err := NewClientWithOptions(apiKey, secret, egoscale.ClientOptWithHTTPClient(httpClient))
	assert.NoError(err)

	ts.billingDate = time.Date(2023, 1, 11, 6, 0, 0, 0, time.UTC)
	o, err := NewObjectStorage(exoClient, ts.Client, ts.DatabaseURL, ts.billingDate)
	assert.NoError(err)
	return o, cancel
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestObjectStorageTestSuite(t *testing.T) {
	suite.Run(t, new(ObjectStorageTestSuite))
}
