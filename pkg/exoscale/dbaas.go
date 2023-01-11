package exoscale

import (
	"context"
	"fmt"
	"time"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/vshn/exoscale-metrics-collector/pkg/exofixtures"
	"github.com/vshn/exoscale-metrics-collector/pkg/reporting"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	k8s "sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	groupVersionKinds = map[string]schema.GroupVersionKind{
		"pg": {
			Group:   "exoscale.crossplane.io",
			Version: "v1",
			Kind:    "PostgreSQLList",
		},
		"mysql": {
			Group:   "exoscale.crossplane.io",
			Version: "v1",
			Kind:    "MySQLList",
		},
		"opensearch": {
			Group:   "exoscale.crossplane.io",
			Version: "v1",
			Kind:    "OpenSearchList",
		},
		"redis": {
			Group:   "exoscale.crossplane.io",
			Version: "v1",
			Kind:    "RedisList",
		},
		"kafka": {
			Group:   "exoscale.crossplane.io",
			Version: "v1",
			Kind:    "KafkaList",
		},
	}
)

// Detail a helper structure for intermediate operations
type Detail struct {
	Organization, DBName, Namespace, Plan, Zone, Kind string
}

// DBaaS provides DBaaS Database info and required clients
type DBaaS struct {
	exoscaleClient *egoscale.Client
	k8sClient      k8s.Client
	databaseURL    string
	billingDate    time.Time
}

// NewDBaaS creates a Service with the initial setup
func NewDBaaS(exoscaleClient *egoscale.Client, k8sClient k8s.Client, databaseURL string) (*DBaaS, error) {
	location, err := time.LoadLocation(timeZone)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize location from time zone %s: %w", location, err)
	}
	now := time.Now().In(location)
	billingDate := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())

	return &DBaaS{
		exoscaleClient: exoscaleClient,
		k8sClient:      k8sClient,
		databaseURL:    databaseURL,
		billingDate:    billingDate,
	}, nil
}

// Execute executes the main business logic for this application by gathering, matching and saving data to the database
func (ds *DBaaS) Execute(ctx context.Context) error {
	logger := ctrl.LoggerFrom(ctx)

	s, err := reporting.NewStore(ds.databaseURL, logger.WithName("reporting-store"))
	if err != nil {
		return fmt.Errorf("reporting.NewStore: %w", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			logger.Error(err, "unable to close")
		}
	}()

	if err := ds.initialize(ctx, s); err != nil {
		return err
	}
	accumulated, err := ds.accumulate(ctx)
	if err != nil {
		return err
	}
	return ds.save(ctx, s, accumulated)
}

func (ds *DBaaS) initialize(ctx context.Context, s *reporting.Store) error {
	logger := ctrl.LoggerFrom(ctx)

	for t, fixtures := range exofixtures.DBaaS {
		if err := s.Initialize(ctx, fixtures.Products, []*db.Discount{&fixtures.Discount}, []*db.Query{&fixtures.Query}); err != nil {
			return fmt.Errorf("initialize(%q): %w", t, err)
		}
	}
	logger.Info("initialized reporting db")
	return nil
}

func (ds *DBaaS) accumulate(ctx context.Context) (map[Key]Aggregated, error) {
	detail, err := ds.fetchManagedDBaaSAndNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetchManagedDBaaSAndNamespaces: %w", err)
	}

	usage, err := ds.fetchDBaaSUsage(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetchDBaaSUsage: %w", err)
	}

	return aggregateDBaaS(ctx, usage, detail), nil
}

func (ds *DBaaS) save(ctx context.Context, s *reporting.Store, aggregatedObjects map[Key]Aggregated) error {
	logger := ctrl.LoggerFrom(ctx)
	if len(aggregatedObjects) == 0 {
		logger.Info("there are no DBaaS instances to be saved in the database")
		return nil
	}
	for _, aggregated := range aggregatedObjects {
		err := ds.ensureAggregatedServiceUsage(ctx, aggregated, s)
		if err != nil {
			logger.Error(err, "Cannot save aggregated DBaaS service record to billing database")
			continue
		}
	}
	return nil
}

// fetchManagedDBaaSAndNamespaces fetches instances and namespaces from kubernetes cluster
func (ds *DBaaS) fetchManagedDBaaSAndNamespaces(ctx context.Context) ([]Detail, error) {
	logger := ctrl.LoggerFrom(ctx)

	logger.V(1).Info("Listing namespaces from cluster")
	namespaces, err := fetchNamespaceWithOrganizationMap(ctx, ds.k8sClient)
	if err != nil {
		return nil, fmt.Errorf("cannot list namespaces: %w", err)
	}

	var dbaasDetails []Detail
	for _, gvk := range groupVersionKinds {
		metaList := &metav1.PartialObjectMetadataList{}
		metaList.SetGroupVersionKind(gvk)
		err := ds.k8sClient.List(ctx, metaList)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("cannot list managed resource kind %s from cluster: %w", gvk.Kind, err)
		}

		for _, item := range metaList.Items {
			dbaasDetail := findDBaaSDetailInNamespacesMap(ctx, item, gvk, namespaces)
			if dbaasDetail == nil {
				continue
			}
			dbaasDetails = append(dbaasDetails, *dbaasDetail)
		}
	}

	return dbaasDetails, nil
}

func findDBaaSDetailInNamespacesMap(ctx context.Context, resource metav1.PartialObjectMetadata, gvk schema.GroupVersionKind, namespaces map[string]string) *Detail {
	logger := ctrl.LoggerFrom(ctx).WithValues("dbaas", resource.GetName())

	namespace, exist := resource.GetLabels()[namespaceLabel]
	if !exist {
		// cannot get namespace from DBaaS
		logger.Info("Namespace label is missing in DBaaS, skipping...", "label", namespaceLabel)
		return nil
	}

	organization, ok := namespaces[namespace]
	if !ok {
		// cannot find namespace in namespace list
		logger.Info("Namespace not found in namespace list, skipping...", "namespace", namespace)
		return nil
	}

	dbaasDetail := Detail{
		DBName:       resource.GetName(),
		Kind:         gvk.Kind,
		Namespace:    namespace,
		Organization: organization,
	}

	logger.V(1).Info("Added namespace and organization to DBaaS", "namespace", dbaasDetail.Namespace, "organization", dbaasDetail.Organization)
	return &dbaasDetail
}

// fetchDBaaSUsage gets DBaaS service usage from Exoscale
func (ds *DBaaS) fetchDBaaSUsage(ctx context.Context) ([]*egoscale.DatabaseService, error) {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("Fetching DBaaS usage from Exoscale")

	var databaseServices []*egoscale.DatabaseService
	for _, zone := range Zones {
		databaseServicesByZone, err := ds.exoscaleClient.ListDatabaseServices(ctx, zone)
		if err != nil {
			logger.V(1).Error(err, "Cannot get exoscale database services on zone", "zone", zone)
			return nil, err
		}
		databaseServices = append(databaseServices, databaseServicesByZone...)
	}
	return databaseServices, nil
}

// aggregateDBaaS aggregates DBaaS services by namespaces and plan
func aggregateDBaaS(ctx context.Context, exoscaleDBaaS []*egoscale.DatabaseService, dbaasDetails []Detail) map[Key]Aggregated {
	logger := ctrl.LoggerFrom(ctx)
	logger.Info("Aggregating DBaaS instances by namespace and plan")

	// The DBaaS names are unique across DB types in an Exoscale organization.
	dbaasServiceUsageMap := make(map[string]egoscale.DatabaseService, len(exoscaleDBaaS))
	for _, usage := range exoscaleDBaaS {
		dbaasServiceUsageMap[*usage.Name] = *usage
	}

	aggregatedDBaasS := make(map[Key]Aggregated)
	for _, dbaasDetail := range dbaasDetails {
		logger.V(1).Info("Checking DBaaS", "instance", dbaasDetail.DBName)

		dbaasUsage, exists := dbaasServiceUsageMap[dbaasDetail.DBName]
		if exists && dbaasDetail.Kind == groupVersionKinds[*dbaasUsage.Type].Kind {
			logger.V(1).Info("Found exoscale dbaas usage", "instance", dbaasUsage.Name, "instance created", dbaasUsage.CreatedAt)
			key := NewKey(dbaasDetail.Namespace, *dbaasUsage.Plan, *dbaasUsage.Type)
			aggregated := aggregatedDBaasS[key]
			aggregated.Key = key
			aggregated.Organization = dbaasDetail.Organization
			aggregated.Value++
			aggregatedDBaasS[key] = aggregated
		} else {
			logger.Info("Could not find any DBaaS on exoscale", "instance", dbaasDetail.DBName)
		}
	}

	return aggregatedDBaasS
}

// ensureAggregatedServiceUsage saves the aggregated database service usage by namespace and plan to the billing database
// To save the correct data to the database the function also matches a relevant product, Discount (if any) and Query.
func (ds *DBaaS) ensureAggregatedServiceUsage(ctx context.Context, aggregatedDatabaseService Aggregated, s *reporting.Store) error {
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

	sourceString := exofixtures.DBaaSSourceString{
		Query:        exofixtures.BillingTypes[dbType],
		Organization: aggregatedDatabaseService.Organization,
		Namespace:    namespace,
		Plan:         plan,
	}

	return s.WriteRecord(ctx, reporting.Record{
		TenantSource:   aggregatedDatabaseService.Organization,
		CategorySource: exofixtures.Provider + ":" + namespace,
		BillingDate:    ds.billingDate,
		ProductSource:  sourceString.GetSourceString(),
		DiscountSource: sourceString.GetSourceString(),
		QueryName:      sourceString.GetQuery(),
		Value:          aggregatedDatabaseService.Value,
	})
}
