package dbaas

import (
	"context"
	"fmt"
	"time"

	pipeline "github.com/ccremer/go-command-pipeline"
	"github.com/vshn/exoscale-metrics-collector/pkg/clients/exoscale"
	"github.com/vshn/exoscale-metrics-collector/pkg/database"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	egoscale "github.com/exoscale/egoscale/v2"
	db "github.com/vshn/exoscale-metrics-collector/pkg/database"
	"github.com/vshn/exoscale-metrics-collector/pkg/service"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	groupVersionResources = map[string]schema.GroupVersionResource{
		"pg": {
			Group:    "exoscale.crossplane.io",
			Version:  "v1",
			Resource: "postgresqls",
		},
	}
)

// Detail a helper structure for intermediate operations
type Detail struct {
	Organization, DBName, Namespace, Plan, Zone string
}

// Context is the context of the DBaaS service
type Context struct {
	context.Context
	dbaasDetails     []Detail
	exoscaleDBaasS   []*egoscale.DatabaseService
	aggregatedDBaasS map[db.Key]db.Aggregated
}

// Service provides DBaaS Database info and required clients
type Service struct {
	exoscaleClient *egoscale.Client
	k8sClient      dynamic.Interface
	database       *db.DBaaSDatabase
}

// NewDBaaSService creates a Service with the initial setup
func NewDBaaSService(exoscaleClient *egoscale.Client, k8sClient dynamic.Interface, databaseURL string) Service {
	return Service{
		exoscaleClient: exoscaleClient,
		k8sClient:      k8sClient,
		database: &db.DBaaSDatabase{
			Database: db.Database{
				URL: databaseURL,
			},
		},
	}
}

// Execute executes the main business logic for this application by gathering, matching and saving data to the database
func (s *Service) Execute(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Running metrics collector by step")

	p := pipeline.NewPipeline[*Context]()
	p.WithSteps(
		p.NewStep("Fetch cluster managed DBaaS", s.fetchManagedDBaaS),
		p.NewStep("Fetch exoscale DBaaS usage", s.fetchDBaaSUsage),
		p.NewStep("Filter supported DBaaS", filterSupportedServiceUsage),
		p.NewStep("Aggregate DBaaS services by namespace and plan", aggregateDBaaS),
		p.WithNestedSteps("Save to billing database", hasAggregatedInstances,
			p.NewStep("Get billing date", s.getBillingDate),
			p.NewStep("Save to database", s.saveToDatabase),
		),
	)

	return p.RunWithContext(&Context{Context: ctx})
}

// fetchManagedDBaaS fetches instances from kubernetes cluster
func (s *Service) fetchManagedDBaaS(ctx *Context) error {
	log := ctrl.LoggerFrom(ctx)

	var dbaasDetails []Detail
	for _, groupVersionResource := range groupVersionResources {
		managedResources, err := s.k8sClient.Resource(groupVersionResource).List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("cannot list managed resource %s from cluster: %w", groupVersionResource.Resource, err)
		}

		for _, managedResource := range managedResources.Items {
			dbaasDetail := Detail{
				DBName: managedResource.GetName(),
			}
			if organization, exist := managedResource.GetLabels()[service.OrganizationLabel]; exist {
				dbaasDetail.Organization = organization
			} else {
				// cannot get organization from DBaaS
				log.Info("Organization label is missing in DBaaS service, skipping...",
					"label", service.OrganizationLabel,
					"dbaas", managedResource.GetName())
				continue
			}
			if namespace, exist := managedResource.GetLabels()[service.NamespaceLabel]; exist {
				dbaasDetail.Namespace = namespace
			} else {
				// cannot get namespace from DBaaS
				log.Info("Namespace label is missing in DBaaS, skipping...",
					"label", service.NamespaceLabel,
					"dbaas", managedResource.GetName())
				continue
			}
			log.V(1).Info("Added namespace and organization to DBaaS",
				"dbaas", managedResource.GetName(),
				"namespace", dbaasDetail.Namespace,
				"organization", dbaasDetail.Organization)
			dbaasDetails = append(dbaasDetails, dbaasDetail)
		}
	}

	ctx.dbaasDetails = dbaasDetails
	return nil
}

// fetchDBaaSUsage gets DBaaS service usage from Exoscale
func (s *Service) fetchDBaaSUsage(ctx *Context) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Fetching DBaaS usage from Exoscale")

	var databaseServices []*egoscale.DatabaseService
	for _, zone := range exoscale.Zones {
		databaseServicesByZone, err := s.exoscaleClient.ListDatabaseServices(ctx, zone)
		if err != nil {
			log.V(1).Error(err, "Cannot get exoscale database services on zone", "zone", zone)
			return err
		}
		databaseServices = append(databaseServices, databaseServicesByZone...)
	}
	ctx.exoscaleDBaasS = databaseServices
	return nil
}

// filterSupportedServiceUsage filters exoscale dbaas service by supported DBaaS groupVersionResources
func filterSupportedServiceUsage(ctx *Context) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Filtering by dbaas type")

	var exoscaleDBaasS []*egoscale.DatabaseService
	for _, exoscaleService := range ctx.exoscaleDBaasS {
		if _, ok := groupVersionResources[*exoscaleService.Type]; ok {
			exoscaleDBaasS = append(exoscaleDBaasS, exoscaleService)
		}
	}

	ctx.exoscaleDBaasS = exoscaleDBaasS
	return nil
}

// aggregateDBaaS aggregates DBaaS services by namespaces and plan
func aggregateDBaaS(ctx *Context) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Aggregating DBaaS instances by namespace and plan")

	dbaasServiceUsageMap := make(map[string]egoscale.DatabaseService, len(ctx.exoscaleDBaasS))
	for _, usage := range ctx.exoscaleDBaasS {
		dbaasServiceUsageMap[*usage.Name] = *usage
	}

	aggregatedDBaasS := make(map[db.Key]db.Aggregated)
	for _, dbaasDetail := range ctx.dbaasDetails {
		log.V(1).Info("Checking DBaaS", "instance", dbaasDetail.DBName)

		if dbaasUsage, exists := dbaasServiceUsageMap[dbaasDetail.DBName]; exists {
			log.V(1).Info("Found exoscale dbaas usage", "instance", dbaasUsage.Name, "instance created", dbaasUsage.CreatedAt)
			key := db.NewKey(dbaasDetail.Namespace, *dbaasUsage.Plan, *dbaasUsage.Type)
			aggregated := aggregatedDBaasS[key]
			aggregated.Key = key
			aggregated.Organization = dbaasDetail.Organization
			aggregated.Value++
			aggregatedDBaasS[key] = aggregated
		} else {
			log.Info("Could not find any DBaaS on exoscale", "instance", dbaasDetail.DBName)
		}
	}

	ctx.aggregatedDBaasS = aggregatedDBaasS
	return nil
}

// getBillingDate sets the date for which the billing takes place.
func (s *Service) getBillingDate(_ *Context) error {
	location, err := time.LoadLocation(service.ExoscaleTimeZone)
	if err != nil {
		return fmt.Errorf("cannot initialize location from time zone %s: %w", location, err)
	}
	now := time.Now().In(location)
	s.database.BillingDate = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
	return nil
}

// saveToDatabase tries to save metrics in the database.
func (s *Service) saveToDatabase(ctx *Context) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Creating a database connection")

	dctx := &database.Context{
		Context:           ctx,
		AggregatedObjects: &ctx.aggregatedDBaasS,
	}
	err := s.database.Execute(dctx)
	if err != nil {
		log.Error(err, "Cannot save to database")
	}
	return nil
}

func hasAggregatedInstances(ctx *Context) bool {
	log := ctrl.LoggerFrom(ctx)
	nrAggregatedInstances := len(ctx.aggregatedDBaasS)
	if nrAggregatedInstances == 0 {
		log.Info("There are no DBaaS instances to be saved in the database")
		return false
	}
	return true
}
