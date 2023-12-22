package cloudscale

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/vshn/billing-collector-cloudservices/pkg/controlAPI"
	"github.com/vshn/billing-collector-cloudservices/pkg/kubernetes"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
	"github.com/vshn/billing-collector-cloudservices/pkg/odoo"
	cloudscalev1 "github.com/vshn/provider-cloudscale/apis/cloudscale/v1"

	"github.com/cloudscale-ch/cloudscale-go-sdk/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8s "sigs.k8s.io/controller-runtime/pkg/client"
)

type BucketDetail struct {
	Namespace string
	Zone      string
}

type ObjectStorage struct {
	client           *cloudscale.Client
	k8sClient        k8s.Client
	controlApiClient k8s.Client
	salesOrder       string
	clusterId        string
	cloudZone        string
	uomMapping       map[string]string
}

const (
	namespaceLabel = "crossplane.io/claim-namespace"
)

func NewObjectStorage(client *cloudscale.Client, k8sClient k8s.Client, controlApiClient k8s.Client, salesOrder, clusterId string, cloudZone string, uomMapping map[string]string) (*ObjectStorage, error) {
	return &ObjectStorage{
		client:           client,
		k8sClient:        k8sClient,
		controlApiClient: controlApiClient,
		salesOrder:       salesOrder,
		clusterId:        clusterId,
		cloudZone:        cloudZone,
		uomMapping:       uomMapping,
	}, nil
}

func (o *ObjectStorage) GetMetrics(ctx context.Context, billingDate time.Time) ([]odoo.OdooMeteredBillingRecord, error) {
	logger := log.Logger(ctx)

	logger.V(1).Info("fetching bucket metrics from cloudscale", "date", billingDate)

	bucketMetricsRequest := cloudscale.BucketMetricsRequest{Start: billingDate, End: billingDate}
	bucketMetrics, err := o.client.Metrics.GetBucketMetrics(ctx, &bucketMetricsRequest)
	if err != nil {
		return nil, err
	}

	// Fetch organisations in case salesOrder is missing
	var nsTenants map[string]string
	if o.salesOrder == "" {
		logger.V(1).Info("Sales order id is missing, fetching namespaces to get the associated org id")
		nsTenants, err = kubernetes.FetchNamespaceWithOrganizationMap(ctx, o.k8sClient)
		if err != nil {
			return nil, err
		}
	}

	logger.V(1).Info("fetching buckets")

	buckets, err := fetchBuckets(ctx, o.k8sClient)
	if err != nil {
		return nil, err
	}

	allRecords := make([]odoo.OdooMeteredBillingRecord, 0)
	for _, bucketMetricsData := range bucketMetrics.Data {
		name := bucketMetricsData.Subject.BucketName
		logger = logger.WithValues("bucket", name)
		bd, ok := buckets[name]
		if !ok {
			logger.Info("unable to sync bucket, ObjectBucket not found")
			continue
		}
		appuioManaged := true
		salesOrder := o.salesOrder
		if salesOrder == "" {
			appuioManaged = false
			salesOrder, err = controlAPI.GetSalesOrder(ctx, o.controlApiClient, nsTenants[bd.Namespace])
			if err != nil {
				logger.Error(err, "unable to sync bucket", "namespace", bd.Namespace)
				continue
			}
		}
		records, err := o.createOdooRecord(bucketMetricsData, bd, appuioManaged, salesOrder)
		if err != nil {
			logger.Error(err, "unable to create Odoo Record", "namespace", bd.Namespace)
			continue
		}
		allRecords = append(allRecords, records...)
		logger.V(1).Info("Created Odoo records", "namespace", bd.Namespace, "records", records)
	}

	return allRecords, nil
}

func (o *ObjectStorage) createOdooRecord(bucketMetricsData cloudscale.BucketMetricsData, b BucketDetail, appuioManaged bool, salesOrder string) ([]odoo.OdooMeteredBillingRecord, error) {
	if len(bucketMetricsData.TimeSeries) != 1 {
		return nil, fmt.Errorf("there must be exactly one metrics data point, found %d", len(bucketMetricsData.TimeSeries))
	}

	storageBytesValue, err := convertUnit(units[productIdStorage], uint64(bucketMetricsData.TimeSeries[0].Usage.StorageBytes))
	if err != nil {
		return nil, err
	}
	trafficOutValue, err := convertUnit(units[productIdTrafficOut], uint64(bucketMetricsData.TimeSeries[0].Usage.SentBytes))
	if err != nil {
		return nil, err
	}
	queryRequestsValue, err := convertUnit(units[productIdQueryRequests], uint64(bucketMetricsData.TimeSeries[0].Usage.SentBytes))
	if err != nil {
		return nil, err
	}

	itemGroup := ""
	if appuioManaged {
		itemGroup = fmt.Sprintf("APPUiO Managed - Cluster: %s / Namespace: %s", o.clusterId, b.Namespace)
	} else {
		itemGroup = fmt.Sprintf("APPUiO Cloud - Zone: %s / Namespace: %s", o.cloudZone, b.Namespace)
	}

	instanceId := fmt.Sprintf("%s/%s", b.Zone, bucketMetricsData.Subject.BucketName)

	return []odoo.OdooMeteredBillingRecord{
		{
			ProductID:            productIdStorage,
			InstanceID:           instanceId,
			ItemDescription:      bucketMetricsData.Subject.BucketName,
			ItemGroupDescription: itemGroup,
			SalesOrder:           salesOrder,
			UnitID:               o.uomMapping[units[productIdStorage]],
			ConsumedUnits:        storageBytesValue,
			TimeRange: odoo.TimeRange{
				From: bucketMetricsData.TimeSeries[0].Start,
				To:   bucketMetricsData.TimeSeries[0].End,
			},
		},
		{
			ProductID:            productIdTrafficOut,
			InstanceID:           instanceId,
			ItemDescription:      bucketMetricsData.Subject.BucketName,
			ItemGroupDescription: itemGroup,
			SalesOrder:           salesOrder,
			UnitID:               o.uomMapping[units[productIdTrafficOut]],
			ConsumedUnits:        trafficOutValue,
			TimeRange: odoo.TimeRange{
				From: bucketMetricsData.TimeSeries[0].Start,
				To:   bucketMetricsData.TimeSeries[0].End,
			},
		},
		{
			ProductID:            productIdQueryRequests,
			InstanceID:           instanceId,
			ItemDescription:      bucketMetricsData.Subject.BucketName,
			ItemGroupDescription: itemGroup,
			SalesOrder:           salesOrder,
			UnitID:               o.uomMapping[units[productIdQueryRequests]],
			ConsumedUnits:        queryRequestsValue,
			TimeRange: odoo.TimeRange{
				From: bucketMetricsData.TimeSeries[0].Start,
				To:   bucketMetricsData.TimeSeries[0].End,
			},
		},
	}, nil
}

func fetchBuckets(ctx context.Context, k8sclient client.Client) (map[string]BucketDetail, error) {
	buckets := &cloudscalev1.BucketList{}
	if err := k8sclient.List(ctx, buckets, client.HasLabels{namespaceLabel}); err != nil {
		return nil, fmt.Errorf("bucket list: %w", err)
	}

	bucketDetails := map[string]BucketDetail{}
	for _, b := range buckets.Items {
		var bd BucketDetail
		bd.Namespace = b.Labels[namespaceLabel]
		bd.Zone = b.Spec.ForProvider.Region
		bucketDetails[b.GetBucketName()] = bd

	}
	return bucketDetails, nil
}

func CheckUnitExistence(mapping map[string]string) error {
	if mapping[odoo.GB] == "" || mapping[odoo.GBDay] == "" || mapping[odoo.KReq] == "" {
		return fmt.Errorf("missing UOM mapping %s, %s or %s", odoo.GB, odoo.GBDay, odoo.KReq)
	}
	return nil
}

func convertUnit(unit string, value uint64) (float64, error) {
	if unit == "GB" || unit == "GBDay" {
		return float64(value) / 1000 / 1000 / 1000, nil
	}
	if unit == "KReq" {
		return float64(value) / 1000, nil
	}
	return 0, errors.New("Unknown query unit " + unit)
}
