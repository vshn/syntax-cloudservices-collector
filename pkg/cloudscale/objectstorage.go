package cloudscale

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudscale-ch/cloudscale-go-sdk/v2"
	"github.com/vshn/exoscale-metrics-collector/pkg/reporting"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ObjectStorage struct {
	client      *cloudscale.Client
	k8sClient   client.Client
	date        time.Time
	databaseURL string
}

func NewObjectStorage(client *cloudscale.Client, k8sClient client.Client, days int, databaseURL string) (*ObjectStorage, error) {
	location, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		return nil, fmt.Errorf("load loaction: %w", err)
	}
	now := time.Now().In(location)
	date := time.Date(now.Year(), now.Month(), now.Day()-days, 0, 0, 0, 0, now.Location())

	return &ObjectStorage{
		client:      client,
		k8sClient:   k8sClient,
		date:        date,
		databaseURL: databaseURL,
	}, nil
}

func (obj *ObjectStorage) Execute(ctx context.Context) error {
	logger := ctrl.LoggerFrom(ctx)
	s, err := reporting.NewStore(obj.databaseURL, logger.WithName("reporting-store"))
	if err != nil {
		return fmt.Errorf("reporting.NewStore: %w", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			logger.Error(err, "unable to close")
		}
	}()

	if err := obj.initialize(ctx, s); err != nil {
		return err
	}
	accumulated, err := obj.accumulate(ctx)
	if err != nil {
		return err
	}
	return obj.save(ctx, s, accumulated)
}

func (obj *ObjectStorage) initialize(ctx context.Context, s *reporting.Store) error {
	logger := ctrl.LoggerFrom(ctx)
	if err := s.Initialize(ctx, ensureProducts, ensureDiscounts, ensureQueries); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	logger.Info("initialized reporting db")
	return nil
}

func (obj *ObjectStorage) accumulate(ctx context.Context) (map[AccumulateKey]uint64, error) {
	return accumulateBucketMetrics(ctx, obj.date, obj.client, obj.k8sClient)
}

func (obj *ObjectStorage) save(ctx context.Context, s *reporting.Store, accumulated map[AccumulateKey]uint64) error {
	logger := ctrl.LoggerFrom(ctx)

	for source, value := range accumulated {
		logger = logger.WithValues("source", source)
		if value == 0 {
			logger.V(1).Info("skipping zero valued entry")
			continue
		}
		logger.Info("accumulating source")

		quantity, err := convertUnit(units[source.Query], value)
		if err != nil {
			logger.Error(err, "convertUnit failed, skip entry", "unit", units[source.Query], value)
			continue
		}

		err = s.WriteRecord(ctx, reporting.Record{
			TenantSource:   source.Tenant,
			CategorySource: source.Zone + ":" + source.Namespace,
			BillingDate:    source.Start,
			ProductSource:  source.String(),
			DiscountSource: source.String(),
			QueryName:      source.Query + ":" + source.Zone,
			Value:          quantity,
		})
		if err != nil {
			logger.Error(err, "writeRecord failed, skip entry")
			continue
		}
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
