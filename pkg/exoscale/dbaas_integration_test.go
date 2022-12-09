//go:build integration

package exoscale

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
	"github.com/exoscale/egoscale/v2/oapi"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/suite"
	"github.com/vshn/exoscale-metrics-collector/pkg/exofixtures"
	"github.com/vshn/exoscale-metrics-collector/pkg/reporting"
	"github.com/vshn/exoscale-metrics-collector/pkg/test"
	exoscalev1 "github.com/vshn/provider-exoscale/apis/exoscale/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DBaaSTestSuite struct {
	test.Suite
	billingDate time.Time
}

func (ts *DBaaSTestSuite) SetupSuite() {
	exoscaleCRDPaths := os.Getenv("EXOSCALE_CRDS_PATH")
	ts.Require().NotZero(exoscaleCRDPaths, "missing env variable EXOSCALE_CRDS_PATH")

	ts.SetupEnv([]string{exoscaleCRDPaths})

	ts.RegisterScheme(exoscalev1.SchemeBuilder.AddToScheme)
}

func (ts *DBaaSTestSuite) TestMetrics() {
	assert := ts.Assert()
	ctx := ts.Context

	ds, cancel := ts.setupDBaaS()
	defer cancel()

	type testcase struct {
		gvk    schema.GroupVersionKind
		ns     string
		plan   string
		dbType oapi.DbaasServiceTypeName
	}

	nsTenantMap := map[string]string{
		"example-project": "example-company",
		"next-big-thing":  "big-corporation",
	}
	for ns, tenant := range nsTenantMap {
		ts.EnsureNS(ns, map[string]string{organizationLabel: tenant})
	}

	tests := make(map[string]testcase)
	for key, gvk := range groupVersionKinds {
		plan := "hobbyist-2"
		// kafka has no hobbyist plan
		if key == "kafka" {
			plan = "startup-2"
		}
		tests[key+"-example-project"] = testcase{
			gvk:    gvk,
			ns:     "example-project",
			plan:   plan,
			dbType: oapi.DbaasServiceTypeName(key),
		}
	}

	tests["pg-expensive-example-project"] = testcase{
		gvk:    groupVersionKinds["pg"],
		ns:     "example-project",
		plan:   "premium-225",
		dbType: "pg",
	}

	tests["pg-next-big-thing"] = testcase{
		gvk:    groupVersionKinds["pg"],
		ns:     "next-big-thing",
		plan:   "business-225",
		dbType: "pg",
	}

	type expectation struct {
		value float64
		tc    testcase
	}
	expectedQuantities := make(map[Key]expectation, 0)

	for name, tc := range tests {
		key := NewKey(tc.ns, tc.plan, string(tc.dbType))
		if _, ok := expectedQuantities[key]; !ok {
			expectedQuantities[key] = expectation{
				value: 0,
				tc:    tc,
			}
		}
		expectedQuantities[key] = expectation{
			value: expectedQuantities[key].value + 1,
			tc:    tc,
		}

		obj := &unstructured.Unstructured{}
		obj.SetUnstructuredContent(map[string]interface{}{
			"apiVersion": tc.gvk.GroupVersion().String(),
			// a bit ugly, but I wanted to avoid adding more than necessary code
			"kind": strings.Replace(tc.gvk.Kind, "List", "", 1),
			"metadata": map[string]interface{}{
				"name": name,
				"labels": map[string]string{
					namespaceLabel: tc.ns,
				},
			},
			"spec": map[string]interface{}{
				"forProvider": map[string]interface{}{
					"zone": "ch-gva-2",
				},
			},
		})
		ts.EnsureResources(obj)
	}

	assert.NoError(ds.Execute(ctx))

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

		for _, want := range expectedQuantities {
			fact, err := ts.getFact(ctx, tx, ts.billingDate, dt, dbaasSource{
				dbType:    string(want.tc.dbType),
				tenant:    nsTenantMap[want.tc.ns],
				namespace: want.tc.ns,
				plan:      want.tc.plan,
			})
			assert.NoError(err, want.tc.ns)

			assert.NotNil(fact, want.tc.ns)
			assert.Equal(want.value, fact.Quantity, want.tc.ns)
		}
		return nil
	}))
}

type dbaasSource struct {
	dbType    string
	tenant    string
	namespace string
	plan      string
}

func (ts *DBaaSTestSuite) getFact(ctx context.Context, tx *sqlx.Tx, date time.Time, dt *db.DateTime, src dbaasSource) (*db.Fact, error) {
	sourceString := exofixtures.DBaaSSourceString{
		Query:        exofixtures.BillingTypes[src.dbType],
		Organization: src.tenant,
		Namespace:    src.namespace,
		Plan:         src.plan,
	}
	record := reporting.Record{
		TenantSource:   src.tenant,
		CategorySource: exofixtures.Provider + ":" + src.namespace,
		BillingDate:    date,
		ProductSource:  sourceString.GetSourceString(),
		DiscountSource: sourceString.GetSourceString(),
		QueryName:      sourceString.GetQuery(),
	}
	return test.FactByRecord(ctx, tx, dt, record)
}

func (ts *DBaaSTestSuite) ensureBuckets(nameNsMap map[string]string) {
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

func (ts *DBaaSTestSuite) setupDBaaS() (*DBaaS, func()) {
	exoClient, cancel, err := newEgoscaleClient(ts.T())
	ts.Assert().NoError(err)

	ts.billingDate = time.Date(2023, 1, 11, 6, 0, 0, 0, time.UTC)
	ds, err := NewDBaaS(exoClient, ts.Client, ts.DatabaseURL, ts.billingDate)
	ts.Assert().NoError(err)
	return ds, cancel
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestDBaaSTestSuite(t *testing.T) {
	suite.Run(t, new(DBaaSTestSuite))
}
