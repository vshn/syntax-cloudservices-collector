package database

import (
	"fmt"
	"strings"

	pipeline "github.com/ccremer/go-command-pipeline"
	ctrl "sigs.k8s.io/controller-runtime"
)

// SosDatabase contains the Database struct needed
type SosDatabase struct {
	Database
}

// Execute starts the saving process of the data in the billing database
func (s *SosDatabase) Execute(dctx *Context) error {
	p := pipeline.NewPipeline[*Context]()
	p.WithSteps(
		p.NewStep("Open database connection", s.openConnection),
		p.WithNestedSteps("Save initial billing configuration", nil,
			p.NewStep("Begin transaction", s.beginTransaction),
			p.NewStep("Ensure initial billing database configuration", s.ensureInitConfiguration),
			p.NewStep("Commit transaction", s.commitTransaction),
		).WithErrorHandler(s.rollback),
		p.NewStep("Save buckets usage to billing database", s.saveUsageToDatabase),
		p.NewStep("Close database connection", s.closeConnection),
	)
	return p.RunWithContext(dctx)
}

// saveUsageToDatabase saves each previously aggregated buckets to the billing database
func (s *SosDatabase) saveUsageToDatabase(dctx *Context) error {
	log := ctrl.LoggerFrom(dctx)
	for _, aggregated := range *dctx.AggregatedObjects {
		err := s.ensureBucketUsage(dctx, aggregated)
		if err != nil {
			log.Error(err, "Cannot save aggregated buckets service record to billing database")
			continue
		}
	}
	return nil
}

// ensureBucketUsage saves the aggregated buckets usage by namespace to the billing database
// To save the correct data to the database the function also matches a relevant product, discount (if any) and query.
// The storage usage is referred to a day before the application ran (yesterday)
func (s *SosDatabase) ensureBucketUsage(dctx *Context, aggregatedBucket Aggregated) error {
	log := ctrl.LoggerFrom(dctx)

	tokens, err := aggregatedBucket.DecodeKey()
	if err != nil {
		return fmt.Errorf("cannot decode key namespace-plan-dbtype - %s, organization %s, number of instances %f: %w",
			aggregatedBucket.Key,
			aggregatedBucket.Organization,
			aggregatedBucket.Value,
			err)
	}
	namespace := tokens[0]

	log.Info("Saving buckets usage for namespace", "namespace", namespace, "storage used", aggregatedBucket.Value)
	dctx.Aggregated = &aggregatedBucket
	dctx.namespace = &namespace
	dctx.organization = &aggregatedBucket.Organization

	s.sourceString = sosSourceString{
		ObjectType: SosType,
		provider:   provider,
	}

	p := pipeline.NewPipeline[*Context]()

	p.WithSteps(
		p.WithNestedSteps(fmt.Sprintf("Saving buckets usage namespace %s", namespace), nil,
			p.NewStep("Begin database transaction", s.beginTransaction),
			p.NewStep("Ensure necessary models", s.ensureModels),
			p.NewStep("Get best match", s.getBestMatch),
			p.NewStep("Adjust storage size", adjustStorageSizeUnit),
			p.NewStep("Save facts", s.saveFacts),
			p.NewStep("Commit transaction", s.commitTransaction),
		).WithErrorHandler(s.rollback),
	)

	return p.RunWithContext(dctx)
}

func adjustStorageSizeUnit(ctx *Context) error {
	var quantity float64
	sosUnit := initConfigs[SosType].query.Unit
	if sosUnit == defaultUnitSos {
		quantity = ctx.Aggregated.Value / 1024 / 1024 / 1024
	} else {
		return fmt.Errorf("unknown query unit %s", sosUnit)
	}
	ctx.value = &quantity
	return nil
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
