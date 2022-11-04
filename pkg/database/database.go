package database

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/appuio/appuio-cloud-reporting/pkg/db"
	pipeline "github.com/ccremer/go-command-pipeline"
	"github.com/jmoiron/sqlx"
	"github.com/vshn/cloudscale-metrics-collector/pkg/categoriesmodel"
	"github.com/vshn/cloudscale-metrics-collector/pkg/datetimesmodel"
	"github.com/vshn/cloudscale-metrics-collector/pkg/discountsmodel"
	"github.com/vshn/cloudscale-metrics-collector/pkg/factsmodel"
	"github.com/vshn/cloudscale-metrics-collector/pkg/productsmodel"
	"github.com/vshn/cloudscale-metrics-collector/pkg/queriesmodel"
	"github.com/vshn/cloudscale-metrics-collector/pkg/tenantsmodel"
	"strings"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	sourceQueryStorage = "object-storage-storage"
	provider           = "exoscale"
	queryAndZone       = sourceQueryStorage + ":" + provider
	defaultUnit        = "GBDay"
)

var (
	product = db.Product{
		Source: queryAndZone,
		Target: sql.NullString{String: "1402", Valid: true},
		Amount: 0.000726,
		Unit:   "GBDay",
		During: db.InfiniteRange(),
	}
	discount = db.Discount{
		Source:   sourceQueryStorage,
		Discount: 0,
		During:   db.InfiniteRange(),
	}
	query = db.Query{
		Name:        queryAndZone,
		Description: "Object Storage - Storage (exoscale.com)",
		Query:       "",
		Unit:        "GBDay",
		During:      db.InfiniteRange(),
	}
)

// AggregatedBucket contains total used storage in an organization namespace
type AggregatedBucket struct {
	Organization string
	// Storage in bytes
	StorageUsed float64
}

// Database holds raw url of the postgresql database with the opened connection
type Database struct {
	URL         string
	BillingDate time.Time
	connection  *sqlx.DB
}

type transactionContext struct {
	context.Context
	billingDate      time.Time
	namespace        *string
	aggregatedBucket *AggregatedBucket
	transaction      *sqlx.Tx
	tenant           *db.Tenant
	category         *db.Category
	dateTime         *db.DateTime
	product          *db.Product
	discount         *db.Discount
	query            *db.Query
	quantity         *float64
}

// OpenConnection opens a connection to the postgres database
func (d *Database) OpenConnection() error {
	connection, err := db.Openx(d.URL)
	if err != nil {
		return fmt.Errorf("cannot create a connection to the database: %w", err)
	}
	d.connection = connection
	return nil
}

// CloseConnection closes the connection to the postgres database
func (d *Database) CloseConnection() error {
	err := d.connection.Close()
	if err != nil {
		return fmt.Errorf("cannot close database connection: %w", err)
	}
	return nil
}

// EnsureBucketUsage saves the aggregated buckets usage by namespace to the postgresql database
// To save the correct data to the database the function also matches a relevant product, discount (if any) and query.
// The storage usage is referred to a day before the application ran (yesterday)
func (d *Database) EnsureBucketUsage(ctx context.Context, namespace string, aggregatedBucket AggregatedBucket) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Saving buckets usage for namespace", "namespace", namespace, "storage used", aggregatedBucket.StorageUsed)

	tctx := &transactionContext{
		Context:          ctx,
		namespace:        &namespace,
		aggregatedBucket: &aggregatedBucket,
		billingDate:      d.BillingDate,
	}
	p := pipeline.NewPipeline[*transactionContext]()
	p.WithSteps(
		p.NewStep("Begin database transaction", d.beginTransaction),
		p.NewStep("Ensure necessary models", d.ensureModels),
		p.NewStep("Get best match", d.getBestMatch),
		p.NewStep("Adjust storage size", d.adjustStorageSizeUnit),
		p.NewStep("Save facts", d.saveFacts),
		p.NewStep("Commit transaction", d.commitTransaction),
	)
	err := p.RunWithContext(tctx)
	if err != nil {
		log.Info("Buckets usage have not been saved to the database", "namespace", namespace, "error", err.Error())
		tctx.transaction.Rollback()
		return err
	}
	return nil
}

func (d *Database) beginTransaction(ctx *transactionContext) error {
	tx, err := d.connection.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("cannot create database transaction for namespace %s: %w", *ctx.namespace, err)
	}
	ctx.transaction = tx
	return nil
}

func (d *Database) ensureModels(ctx *transactionContext) error {
	namespace := *ctx.namespace
	tenant, err := tenantsmodel.Ensure(ctx, ctx.transaction, &db.Tenant{Source: ctx.aggregatedBucket.Organization})
	if err != nil {
		return fmt.Errorf("cannot ensure organization for namespace %s: %w", namespace, err)
	}
	ctx.tenant = tenant

	category, err := categoriesmodel.Ensure(ctx, ctx.transaction, &db.Category{Source: provider + ":" + namespace})
	if err != nil {
		return fmt.Errorf("cannot ensure category for namespace %s: %w", namespace, err)
	}
	ctx.category = category

	dateTime := datetimesmodel.New(ctx.billingDate)
	dateTime, err = datetimesmodel.Ensure(ctx, ctx.transaction, dateTime)
	if err != nil {
		return fmt.Errorf("cannot ensure date time for namespace %s: %w", namespace, err)
	}
	ctx.dateTime = dateTime
	return nil
}

func (d *Database) getBestMatch(ctx *transactionContext) error {
	namespace := *ctx.namespace
	productMatch, err := productsmodel.GetBestMatch(ctx, ctx.transaction, getSourceString(namespace, ctx.aggregatedBucket.Organization), ctx.billingDate)
	if err != nil {
		return fmt.Errorf("cannot get product best match for namespace %s: %w", namespace, err)
	}
	ctx.product = productMatch

	discountMatch, err := discountsmodel.GetBestMatch(ctx, ctx.transaction, getSourceString(namespace, ctx.aggregatedBucket.Organization), ctx.billingDate)
	if err != nil {
		return fmt.Errorf("cannot get discount best match for namespace %s: %w", namespace, err)
	}
	ctx.discount = discountMatch

	queryMatch, err := queriesmodel.GetByName(ctx, ctx.transaction, queryAndZone)
	if err != nil {
		return fmt.Errorf("cannot get query by name for namespace %s: %w", namespace, err)
	}
	ctx.query = queryMatch

	return nil
}

func (d *Database) adjustStorageSizeUnit(ctx *transactionContext) error {
	var quantity float64
	if query.Unit == defaultUnit {
		quantity = ctx.aggregatedBucket.StorageUsed / 1024 / 1024 / 1024
	} else {
		return fmt.Errorf("unknown query unit %s", query.Unit)
	}
	ctx.quantity = &quantity
	return nil
}

func (d *Database) saveFacts(ctx *transactionContext) error {
	storageFact := factsmodel.New(ctx.dateTime, ctx.query, ctx.tenant, ctx.category, ctx.product, ctx.discount, *ctx.quantity)
	_, err := factsmodel.Ensure(ctx, ctx.transaction, storageFact)
	if err != nil {
		return fmt.Errorf("cannot save fact for namespace %s: %w", *ctx.namespace, err)
	}
	return nil
}

func (d *Database) commitTransaction(ctx *transactionContext) error {
	err := ctx.transaction.Commit()
	if err != nil {
		return fmt.Errorf("cannot commit transaction for buckets in namespace %s: %w", *ctx.namespace, err)
	}
	return nil
}

// EnsureInitConfiguration ensures the minimum exoscale object storage configuration data is present in the database
// before saving buckets usage
func (d *Database) EnsureInitConfiguration(ctx context.Context) error {
	transaction, err := d.connection.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("cannot begin transaction for initial database configuration: %w", err)
	}
	defer transaction.Rollback()
	err = ensureInitConfigurationModels(ctx, err, transaction)
	if err != nil {
		return err
	}
	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("cannot commit transaction for initial database configuration: %w", err)
	}
	return nil
}

func ensureInitConfigurationModels(ctx context.Context, err error, transaction *sqlx.Tx) error {
	_, err = productsmodel.Ensure(ctx, transaction, &product)
	if err != nil {
		return fmt.Errorf("cannot ensure exoscale product model in the database: %w", err)
	}
	_, err = discountsmodel.Ensure(ctx, transaction, &discount)
	if err != nil {
		return fmt.Errorf("cannot ensure exoscale discount model in the database: %w", err)
	}
	_, err = queriesmodel.Ensure(ctx, transaction, &query)
	if err != nil {
		return fmt.Errorf("cannot ensure exoscale query model in the database: %w", err)
	}
	return nil
}

func getSourceString(namespace, organization string) string {
	return strings.Join([]string{queryAndZone, organization, namespace}, ":")
}
