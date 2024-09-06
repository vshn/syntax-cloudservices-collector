package cloudscale

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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
	providerMetrics  map[string]prometheus.Counter
}

const (
	namespaceLabel = "crossplane.io/claim-namespace"
)

type ObjectStorageData struct {
	cloudscale.BucketMetricsData
	BucketDetail
	Organization string
}

func NewObjectStorage(client *cloudscale.Client, k8sClient k8s.Client, controlApiClient k8s.Client, salesOrder, clusterId string, cloudZone string, uomMapping map[string]string, providerMetrics map[string]prometheus.Counter) (*ObjectStorage, error) {
	return &ObjectStorage{
		client:           client,
		k8sClient:        k8sClient,
		controlApiClient: controlApiClient,
		salesOrder:       salesOrder,
		clusterId:        clusterId,
		cloudZone:        cloudZone,
		uomMapping:       uomMapping,
		providerMetrics:  providerMetrics,
	}, nil
}

func (o *ObjectStorage) GetMetrics(ctx context.Context, billingDate time.Time) ([]odoo.OdooMeteredBillingRecord, error) {
	logger := log.Logger(ctx)

	logger.V(1).Info("fetching bucket metrics from cloudscale", "date", billingDate)

	bucketMetricsRequest := cloudscale.BucketMetricsRequest{Start: billingDate, End: billingDate}
	bucketMetrics, err := o.client.Metrics.GetBucketMetrics(ctx, &bucketMetricsRequest)
	if err != nil {
		o.providerMetrics["providerFailed"].Inc()
		return nil, err
	}

	bucketMap := make(map[string]*ObjectStorageData)

	// create a map with bucket name as key, this way we match buckets created manually and not via Appcat service
	for i, bucketMetric := range bucketMetrics.Data {
		bucketMap[bucketMetric.Subject.BucketName] = &ObjectStorageData{
			BucketMetricsData: bucketMetrics.Data[i],
		}
	}

	// Since our buckets are always created in the convention $namespace.$bucketname, we can extract the namespace from the bucket name by splitting it.
	// However, we need to fetch the user details to get the actual namespace.
	for _, bucket := range bucketMap {
		// fetch bucket user by id
		logger.Info("fetching user details", "userID", bucket.Subject.ObjectsUserID)
		userDetails, err := o.client.ObjectsUsers.Get(ctx, bucket.Subject.ObjectsUserID)
		if err != nil {
			o.providerMetrics["providerFailed"].Inc()
			logger.Error(err, "unknown userID, something broke here fatally")
			return nil, err
		}
		bucket.BucketDetail.Namespace = strings.Split(userDetails.DisplayName, ".")[0]
	}

	// Fetch organisations in case salesOrder is missing
	var nsTenants map[string]string
	if o.salesOrder == "" {
		logger.V(1).Info("Sales order id is missing, fetching namespaces to get the associated org id")
		nsTenants, err = kubernetes.FetchNamespaceWithOrganizationMap(ctx, o.k8sClient)
		if err != nil {
			o.providerMetrics["providerFailed"].Inc()
			return nil, err
		}
	}

	logger.V(1).Info("fetching buckets")

	buckets, err := fetchBuckets(ctx, o.k8sClient)
	if err != nil {
		o.providerMetrics["providerFailed"].Inc()
		return nil, err
	}

	for bucket := range bucketMap {
		bucketName := bucketMap[bucket].BucketMetricsData.Subject.BucketName
		if val, ok := buckets[bucketName]; ok {
			bucketMap[bucket].Zone = val.Zone
		}

		// assign organisation to bucketMap
		if val, ok := nsTenants[bucketMap[bucket].Namespace]; ok {
			bucketMap[bucket].Organization = val
		}

	}

	allRecords := make([]odoo.OdooMeteredBillingRecord, 0)
	for _, bucket := range bucketMap {

		appuioManaged := true
		salesOrder := o.salesOrder
		if salesOrder == "" {
			appuioManaged = false
			salesOrder, err = controlAPI.GetSalesOrder(ctx, o.controlApiClient, bucket.Organization)
			if err != nil {
				logger.Error(err, "unable to sync bucket", "namespace", bucket)
				continue
			}
		}
		records, err := o.createOdooRecord(bucket.BucketMetricsData, bucket.BucketDetail, appuioManaged, salesOrder, billingDate)
		if err != nil {
			logger.Error(err, "unable to create Odoo Record", "namespace", bucket.Namespace)
			continue
		}
		allRecords = append(allRecords, records...)
		logger.V(1).Info("Created Odoo records", "namespace", bucket, "records", records)
	}
	o.providerMetrics["providerSucceeded"].Inc()
	return allRecords, nil
}

func (o *ObjectStorage) createOdooRecord(bucketMetricsData cloudscale.BucketMetricsData, b BucketDetail, appuioManaged bool, salesOrder string, billingDate time.Time) ([]odoo.OdooMeteredBillingRecord, error) {
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
	queryRequestsValue, err := convertUnit(units[productIdQueryRequests], uint64(bucketMetricsData.TimeSeries[0].Usage.Requests))
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

	billingStart := time.Date(billingDate.Year(), billingDate.Month(), billingDate.Day(), 0, 0, 0, 0, time.UTC)
	billingEnd := time.Date(billingDate.Year(), billingDate.Month(), billingDate.Day()+1, 0, 0, 0, 0, time.UTC)

	return []odoo.OdooMeteredBillingRecord{
		{
			ProductID:            productIdStorage,
			InstanceID:           instanceId + "/storage",
			ItemDescription:      bucketMetricsData.Subject.BucketName,
			ItemGroupDescription: itemGroup,
			SalesOrder:           salesOrder,
			UnitID:               o.uomMapping[units[productIdStorage]],
			ConsumedUnits:        storageBytesValue,
			TimeRange: odoo.TimeRange{
				From: billingStart,
				To:   billingEnd,
			},
		},
		{
			ProductID:            productIdTrafficOut,
			InstanceID:           instanceId + "/trafficout",
			ItemDescription:      bucketMetricsData.Subject.BucketName,
			ItemGroupDescription: itemGroup,
			SalesOrder:           salesOrder,
			UnitID:               o.uomMapping[units[productIdTrafficOut]],
			ConsumedUnits:        trafficOutValue,
			TimeRange: odoo.TimeRange{
				From: billingStart,
				To:   billingEnd,
			},
		},
		{
			ProductID:            productIdQueryRequests,
			InstanceID:           instanceId + "/requests",
			ItemDescription:      bucketMetricsData.Subject.BucketName,
			ItemGroupDescription: itemGroup,
			SalesOrder:           salesOrder,
			UnitID:               o.uomMapping[units[productIdQueryRequests]],
			ConsumedUnits:        queryRequestsValue,
			TimeRange: odoo.TimeRange{
				From: billingStart,
				To:   billingEnd,
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
