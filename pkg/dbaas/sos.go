package dbaas

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
	"github.com/jmoiron/sqlx"
	"github.com/vshn/exoscale-metrics-collector/pkg/reporting"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// SosType represents object storage storage type
	SosType        ObjectType = "object-storage-storage"
	querySos                  = string(SosType) + ":" + provider
	defaultUnitSos            = "GBDay"
)

// SosDatabase contains the Database struct needed
type SosDatabase struct {
	URL          string
	BillingDate  time.Time
	connection   *sqlx.DB
	sourceString SourceString
}

// Execute starts the saving process of the data in the billing database
func (s *SosDatabase) Execute(ctx context.Context, aggregated map[Key]Aggregated) error {
	logger := ctrl.LoggerFrom(ctx)

	store, err := reporting.NewStore(s.URL, logger.WithName("reporting-store"))
	if err != nil {
		return fmt.Errorf("reporting.NewStore: %w", err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error(err, "unable to close")
		}
	}()

	// TODO: split sos/dbaas
	for t, config := range initConfigs {
		if err := store.Initialize(ctx, config.products, []*db.Discount{&config.discount}, []*db.Query{&config.query}); err != nil {
			return fmt.Errorf("init(%q): %w", t, err)
		}
	}

	if err := s.saveUsageToDatabase(ctx, store, aggregated); err != nil {
		return fmt.Errorf("save usage: %w", err)
	}
	return nil
}

// saveUsageToDatabase saves each previously aggregated buckets to the billing database
func (s *SosDatabase) saveUsageToDatabase(ctx context.Context, store *reporting.Store, aggregatedObjects map[Key]Aggregated) error {
	logger := ctrl.LoggerFrom(ctx)

	for _, aggregated := range aggregatedObjects {
		err := s.ensureBucketUsage(ctx, store, aggregated)
		if err != nil {
			logger.Error(err, "cannot save aggregated buckets service record to billing database")
			continue
		}
	}
	return nil
}

// ensureBucketUsage saves the aggregated buckets usage by namespace to the billing database
// To save the correct data to the database the function also matches a relevant product, discount (if any) and query.
// The storage usage is referred to a day before the application ran (yesterday)
func (s *SosDatabase) ensureBucketUsage(ctx context.Context, store *reporting.Store, aggregatedBucket Aggregated) error {
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
		ObjectType: SosType,
		provider:   provider,
	}
	value, err = adjustStorageSizeUnit(value)
	if err != nil {
		return fmt.Errorf("adjustStorageSizeUnit(%v): %w", value, err)
	}

	return store.WriteRecord(ctx, reporting.Record{
		TenantSource:   organization,
		CategorySource: provider + ":" + namespace,
		BillingDate:    s.BillingDate,
		ProductSource:  sourceString.getSourceString(),
		DiscountSource: sourceString.getSourceString(),
		QueryName:      sourceString.getQuery(),
		Value:          value,
	})
}

func adjustStorageSizeUnit(value float64) (float64, error) {
	sosUnit := initConfigs[SosType].query.Unit
	if sosUnit == defaultUnitSos {
		return value / 1024 / 1024 / 1024, nil
	}
	return 0, fmt.Errorf("unknown query unit %s", sosUnit)
}

type sosSourceString struct {
	ObjectType
	provider string
}

func (ss sosSourceString) getQuery() string {
	return strings.Join([]string{string(ss.ObjectType), ss.provider}, ":")
}

func (ss sosSourceString) getSourceString() string {
	return strings.Join([]string{string(ss.ObjectType), ss.provider}, ":")
}
