//go:build integration

package cloudscale

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
	"github.com/cloudscale-ch/cloudscale-go-sdk/v2"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/suite"
	"github.com/vshn/exoscale-metrics-collector/pkg/reporting"
	"github.com/vshn/exoscale-metrics-collector/pkg/test"
	cloudscalev1 "github.com/vshn/provider-cloudscale/apis/cloudscale/v1"
	"gopkg.in/dnaeon/go-vcr.v3/cassette"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ObjectStorageTestSuite struct {
	test.Suite
	days int
}

func (ts *ObjectStorageTestSuite) SetupSuite() {
	cloudscaleCRDsPath := os.Getenv("CLOUDSCALE_CRDS_PATH")
	ts.Require().NotZero(cloudscaleCRDsPath, "missing env variable CLOUDSCALE_CRDS_PATH")

	ts.SetupEnv([]string{cloudscaleCRDsPath})

	ts.RegisterScheme(cloudscalev1.SchemeBuilder.AddToScheme)
}

// TestMetrics sets up a couple of buckets and associated namespaces with organizations set.
// The cloudscale client is set up with an HTTP replay recorder (go-vcr) which looks into testdata/ for recorded
// HTTP responses.
// For simplicity reasons, the recorder only uses URL and method for matching recorded responses. The upside
// of this is it doesn't matter when we execute the tests since the date used to fetch metrics doesn't matter for matching.
// Downside of course is it doesn't do any validation related to the date matching but that's not the main thing to test here.
func (ts *ObjectStorageTestSuite) TestMetrics() {
	assert := ts.Assert()
	ctx := ts.Context

	o, cancel := ts.setupObjectStorage()
	defer cancel()

	date, err := billingDate(ts.days)
	assert.NoError(err)

	expectedQuantities := map[AccumulateKey]float64{
		AccumulateKey{
			Query:     sourceQueryStorage,
			Zone:      sourceZones[0],
			Tenant:    "example-company",
			Namespace: "example-project",
			Start:     date,
		}: 1000.000004096,
		AccumulateKey{
			Query:     sourceQueryRequests,
			Zone:      sourceZones[0],
			Tenant:    "example-company",
			Namespace: "example-project",
			Start:     date,
		}: 100.001,
		AccumulateKey{
			Query:     sourceQueryTrafficOut,
			Zone:      sourceZones[0],
			Tenant:    "example-company",
			Namespace: "example-project",
			Start:     date,
		}: 50.0,
		AccumulateKey{
			Query:     sourceQueryStorage,
			Zone:      sourceZones[0],
			Tenant:    "big-corporation",
			Namespace: "next-big-thing",
			Start:     date,
		}: 0,
		AccumulateKey{
			Query:     sourceQueryRequests,
			Zone:      sourceZones[0],
			Tenant:    "big-corporation",
			Namespace: "next-big-thing",
			Start:     date,
		}: 0.001,
		AccumulateKey{
			Query:     sourceQueryTrafficOut,
			Zone:      sourceZones[0],
			Tenant:    "big-corporation",
			Namespace: "next-big-thing",
			Start:     date,
		}: 0,
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
		dt, err := reporting.GetDateTime(ctx, tx, date)
		assert.NoError(err)

		for key, expectedQuantity := range expectedQuantities {
			fact, err := ts.getFact(ctx, tx, date, dt, key)
			assert.NoError(err, key)
			if expectedQuantity == 0 {
				assert.Nil(fact, "fact found but expectedQuantity was zero")
			} else {
				assert.NotNil(fact, key)
				assert.Equal(expectedQuantity, fact.Quantity, key)
			}
		}
		return nil
	}))
}

func (ts *ObjectStorageTestSuite) getFact(ctx context.Context, tx *sqlx.Tx, date time.Time, dt *db.DateTime, src AccumulateKey) (*db.Fact, error) {
	record := reporting.Record{
		TenantSource:   src.Tenant,
		CategorySource: src.Zone + ":" + src.Namespace,
		BillingDate:    date,
		ProductSource:  src.String(),
		DiscountSource: src.String(),
		QueryName:      src.Query + ":" + src.Zone,
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
		resources = append(resources, &cloudscalev1.Bucket{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: map[string]string{namespaceLabel: ns},
			},
			Spec: cloudscalev1.BucketSpec{
				ForProvider: cloudscalev1.BucketParameters{BucketName: name},
			},
		})
	}
	ts.EnsureResources(resources...)
}

func (ts *ObjectStorageTestSuite) setupObjectStorage() (*ObjectStorage, func()) {
	assert := ts.Assert()
	httpClient, cancel, err := ts.requestRecorder()

	c := cloudscale.NewClient(httpClient)
	// required to be set when recording new response.
	if apiToken := os.Getenv("CLOUDSCALE_API_TOKEN"); apiToken != "" {
		c.AuthToken = apiToken
		ts.T().Log("API token set")
	} else {
		ts.T().Log("no API token provided")
	}

	ts.days = 1
	o, err := NewObjectStorage(c, ts.Client, ts.days, ts.DatabaseURL)
	assert.NoError(err)
	return o, cancel
}

func (ts *ObjectStorageTestSuite) requestRecorder() (*http.Client, func(), error) {
	ts.T().Helper()

	r, err := recorder.NewWithOptions(&recorder.Options{
		CassetteName:       "testdata/cloudscale/" + ts.T().Name(),
		Mode:               recorder.ModeRecordOnce,
		RealTransport:      nil,
		SkipRequestLatency: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("recorder: %w", err)
	}
	cancel := func() {
		if err := r.Stop(); err != nil {
			ts.T().Logf("recorder stop: %v", err)
		}
	}

	r.AddHook(func(i *cassette.Interaction) error {
		// ensure API token is not stored in recorded response
		delete(i.Request.Headers, "Authorization")
		return nil
	}, recorder.AfterCaptureHook)

	return r.GetDefaultClient(), cancel, nil
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestObjectStorageTestSuite(t *testing.T) {
	suite.Run(t, new(ObjectStorageTestSuite))
}
