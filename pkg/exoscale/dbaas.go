package exoscale

import (
	"context"
	"fmt"
	"time"

	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/go-logr/logr"
	db "github.com/vshn/exoscale-metrics-collector/pkg/dbaas"
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

// Service provides DBaaS Database info and required clients
type Service struct {
	exoscaleClient *egoscale.Client
	k8sClient      k8s.Client
	database       *db.DBaaSDatabase
}

// NewDBaaSService creates a Service with the initial setup
func NewDBaaSService(exoscaleClient *egoscale.Client, k8sClient k8s.Client, databaseURL string) (*Service, error) {
	location, err := time.LoadLocation(timeZone)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize location from time zone %s: %w", location, err)
	}
	now := time.Now().In(location)
	billingDate := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())

	return &Service{
		exoscaleClient: exoscaleClient,
		k8sClient:      k8sClient,
		database: &db.DBaaSDatabase{
			URL:         databaseURL,
			BillingDate: billingDate,
		},
	}, nil
}

// Execute executes the main business logic for this application by gathering, matching and saving data to the database
func (s *Service) Execute(ctx context.Context) error {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Running metrics collector by step")

	detail, err := s.fetchManagedDBaaSAndNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("fetchManagedDBaaSAndNamespaces: %w", err)
	}

	usage, err := s.fetchDBaaSUsage(ctx)
	if err != nil {
		return fmt.Errorf("fetchDBaaSUsage: %w", err)
	}

	aggregated := aggregateDBaaS(ctx, usage, detail)

	nrAggregatedInstances := len(aggregated)
	if nrAggregatedInstances == 0 {
		log.Info("there are no DBaaS instances to be saved in the database")
		return nil
	}

	if err := s.database.Execute(ctx, aggregated); err != nil {
		return fmt.Errorf("db execute: %w", err)
	}
	return nil
}

// fetchManagedDBaaSAndNamespaces fetches instances and namespaces from kubernetes cluster
func (s *Service) fetchManagedDBaaSAndNamespaces(ctx context.Context) ([]Detail, error) {
	log := ctrl.LoggerFrom(ctx)

	log.V(1).Info("Listing namespaces from cluster")
	namespaces, err := fetchNamespaceWithOrganizationMap(ctx, s.k8sClient)
	if err != nil {
		return nil, fmt.Errorf("cannot list namespaces: %w", err)
	}

	var dbaasDetails []Detail
	for _, gvk := range groupVersionKinds {
		metaList := &metav1.PartialObjectMetadataList{}
		metaList.SetGroupVersionKind(gvk)
		err := s.k8sClient.List(ctx, metaList)
		if err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("cannot list managed resource kind %s from cluster: %w", gvk.Kind, err)
		}

		for _, item := range metaList.Items {
			dbaasDetail := findDBaaSDetailInNamespacesMap(item, gvk, namespaces, log)
			if dbaasDetail == nil {
				continue
			}
			dbaasDetails = append(dbaasDetails, *dbaasDetail)
		}
	}

	return dbaasDetails, nil
}

func findDBaaSDetailInNamespacesMap(resource metav1.PartialObjectMetadata, gvk schema.GroupVersionKind, namespaces map[string]string, log logr.Logger) *Detail {
	dbaasDetail := Detail{
		DBName: resource.GetName(),
		Kind:   gvk.Kind,
	}
	if namespace, exist := resource.GetLabels()[namespaceLabel]; exist {
		organization, ok := namespaces[namespace]
		if !ok {
			// cannot find namespace in namespace list
			log.Info("Namespace not found in namespace list, skipping...",
				"namespace", namespace,
				"dbaas", resource.GetName())
			return nil
		}
		dbaasDetail.Namespace = namespace
		dbaasDetail.Organization = organization
	} else {
		// cannot get namespace from DBaaS
		log.Info("Namespace label is missing in DBaaS, skipping...",
			"label", namespaceLabel,
			"dbaas", resource.GetName())
		return nil
	}
	log.V(1).Info("Added namespace and organization to DBaaS",
		"dbaas", resource.GetName(),
		"namespace", dbaasDetail.Namespace,
		"organization", dbaasDetail.Organization)
	return &dbaasDetail
}

// fetchDBaaSUsage gets DBaaS service usage from Exoscale
func (s *Service) fetchDBaaSUsage(ctx context.Context) ([]*egoscale.DatabaseService, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Fetching DBaaS usage from Exoscale")

	var databaseServices []*egoscale.DatabaseService
	for _, zone := range Zones {
		databaseServicesByZone, err := s.exoscaleClient.ListDatabaseServices(ctx, zone)
		if err != nil {
			log.V(1).Error(err, "Cannot get exoscale database services on zone", "zone", zone)
			return nil, err
		}
		databaseServices = append(databaseServices, databaseServicesByZone...)
	}
	return databaseServices, nil
}

// aggregateDBaaS aggregates DBaaS services by namespaces and plan
func aggregateDBaaS(ctx context.Context, exoscaleDBaaS []*egoscale.DatabaseService, dbaasDetails []Detail) map[db.Key]db.Aggregated {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Aggregating DBaaS instances by namespace and plan")

	// The DBaaS names are unique across DB types in an Exoscale organization.
	dbaasServiceUsageMap := make(map[string]egoscale.DatabaseService, len(exoscaleDBaaS))
	for _, usage := range exoscaleDBaaS {
		dbaasServiceUsageMap[*usage.Name] = *usage
	}

	aggregatedDBaasS := make(map[db.Key]db.Aggregated)
	for _, dbaasDetail := range dbaasDetails {
		log.V(1).Info("Checking DBaaS", "instance", dbaasDetail.DBName)

		dbaasUsage, exists := dbaasServiceUsageMap[dbaasDetail.DBName]
		if exists && dbaasDetail.Kind == groupVersionKinds[*dbaasUsage.Type].Kind {
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

	return aggregatedDBaasS
}
