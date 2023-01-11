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
	queryDBaaSPostgres   = string(PostgresDBaaSType) + ":" + provider
	queryDBaaSMysql      = string(MysqlDBaaSType) + ":" + provider
	queryDBaaSOpensearch = string(OpensearchDBaaSType) + ":" + provider
	queryDBaaSRedis      = string(RedisDBaaSType) + ":" + provider
	queryDBaaSKafka      = string(KafkaDBaaSType) + ":" + provider
	defaultUnitDBaaS     = "Instances"
)

// exoscale service types to query billing Database types
var (
	billingTypes = map[string]string{
		"pg":         queryDBaaSPostgres,
		"mysql":      queryDBaaSMysql,
		"opensearch": queryDBaaSOpensearch,
		"redis":      queryDBaaSRedis,
		"kafka":      queryDBaaSKafka,
	}
)

// DBaaSDatabase contains the Database struct needed with the plan, specific of DBaaS
type DBaaSDatabase struct {
	URL          string
	BillingDate  time.Time
	connection   *sqlx.DB
	sourceString SourceString
}

// SourceString allows to get the full source or query substring
type SourceString interface {
	getSourceString() string
	getQuery() string
}

// Execute starts the saving process of the data in the billing database
func (d *DBaaSDatabase) Execute(ctx context.Context, aggregatedDBaaS map[Key]Aggregated) error {
	logger := ctrl.LoggerFrom(ctx)

	s, err := reporting.NewStore(d.URL, logger.WithName("reporting-store"))
	if err != nil {
		return fmt.Errorf("reporting.NewStore: %w", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			logger.Error(err, "unable to close")
		}
	}()

	// TODO: split sos/dbaas
	for t, config := range initConfigs {
		if err := s.Initialize(ctx, config.products, []*db.Discount{&config.discount}, []*db.Query{&config.query}); err != nil {
			return fmt.Errorf("init(%q): %w", t, err)
		}
	}

	if err := d.saveUsageToDatabase(ctx, s, aggregatedDBaaS); err != nil {
		return fmt.Errorf("save usage: %w", err)
	}
	return nil
}

// saveUsageToDatabase saves each previously aggregated DBaaS to the billing database
func (d *DBaaSDatabase) saveUsageToDatabase(ctx context.Context, s *reporting.Store, aggregatedDBaaS map[Key]Aggregated) error {
	logger := ctrl.LoggerFrom(ctx)

	for _, aggregated := range aggregatedDBaaS {
		err := d.ensureAggregatedServiceUsage(ctx, aggregated, s)
		if err != nil {
			logger.Error(err, "Cannot save aggregated DBaaS service record to billing database")
			continue
		}
	}
	return nil
}

// ensureAggregatedServiceUsage saves the aggregated database service usage by namespace and plan to the billing database
// To save the correct data to the database the function also matches a relevant product, discount (if any) and query.
func (d *DBaaSDatabase) ensureAggregatedServiceUsage(ctx context.Context, aggregatedDatabaseService Aggregated, s *reporting.Store) error {
	logger := ctrl.LoggerFrom(ctx)

	tokens, err := aggregatedDatabaseService.DecodeKey()
	if err != nil {
		return fmt.Errorf("cannot decode key namespace-plan-dbtype - %s, organization %s, number of instances %f: %w",
			aggregatedDatabaseService.Key,
			aggregatedDatabaseService.Organization,
			aggregatedDatabaseService.Value,
			err)
	}
	namespace := tokens[0]
	plan := tokens[1]
	dbType := tokens[2]

	logger.Info("Saving DBaaS usage", "namespace", namespace, "plan", plan, "type", dbType, "quantity", aggregatedDatabaseService.Value)

	sourceString := dbaasSourceString{
		Query:        billingTypes[dbType],
		Organization: aggregatedDatabaseService.Organization,
		Namespace:    namespace,
		Plan:         plan,
	}

	return s.WriteRecord(ctx, reporting.Record{
		TenantSource:   aggregatedDatabaseService.Organization,
		CategorySource: provider + ":" + namespace,
		BillingDate:    d.BillingDate,
		ProductSource:  sourceString.getSourceString(),
		DiscountSource: sourceString.getSourceString(),
		QueryName:      sourceString.getQuery(),
		Value:          aggregatedDatabaseService.Value,
	})
}

type dbaasSourceString struct {
	Query        string
	Organization string
	Namespace    string
	Plan         string
}

func (ss dbaasSourceString) getQuery() string {
	return ss.Query
}

func (ss dbaasSourceString) getSourceString() string {
	return strings.Join([]string{ss.Query, ss.Organization, ss.Namespace, ss.Plan}, ":")
}
