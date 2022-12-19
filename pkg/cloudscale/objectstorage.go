package cloudscale

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
	"github.com/cloudscale-ch/cloudscale-go-sdk/v2"
	"github.com/jmoiron/sqlx"
	"github.com/vshn/exoscale-metrics-collector/pkg/categoriesmodel"
	"github.com/vshn/exoscale-metrics-collector/pkg/cmd"
	"github.com/vshn/exoscale-metrics-collector/pkg/datetimesmodel"
	"github.com/vshn/exoscale-metrics-collector/pkg/discountsmodel"
	"github.com/vshn/exoscale-metrics-collector/pkg/factsmodel"
	"github.com/vshn/exoscale-metrics-collector/pkg/productsmodel"
	"github.com/vshn/exoscale-metrics-collector/pkg/queriesmodel"
	"github.com/vshn/exoscale-metrics-collector/pkg/reporting"
	"github.com/vshn/exoscale-metrics-collector/pkg/tenantsmodel"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var location *time.Location

func init() {
	l, err := time.LoadLocation("Europe/Zurich")
	if err != nil {
		panic(fmt.Errorf("load loaction: %w", err))
	}
	location = l
}

type ObjectStorage struct {
	client      *cloudscale.Client
	k8sClient   client.Client
	date        time.Time
	databaseURL string
}

func NewObjectStorage(client *cloudscale.Client, k8sClient client.Client, days int, databaseURL string) *ObjectStorage {
	now := time.Now().In(location)
	date := time.Date(now.Year(), now.Month(), now.Day()-days, 0, 0, 0, 0, now.Location())

	return &ObjectStorage{
		client:      client,
		k8sClient:   k8sClient,
		date:        date,
		databaseURL: databaseURL,
	}
}

func (obj *ObjectStorage) Execute(ctx context.Context) error {
	logger := cmd.AppLogger(ctx)
	s, err := reporting.NewStore(obj.databaseURL, logger.WithName("reporting-store"))
	if err != nil {
		return fmt.Errorf("reporting.NewStore: %w", err)
	}
	defer func() {
		if err := s.Close(); err != nil {
			logger.Error(err, "unable to close")
		}
	}()

	if err := s.Initialize(ctx, ensureProducts, ensureDiscounts, ensureQueries); err != nil {
		return err
	}

	accumulated, err := accumulateBucketMetrics(ctx, obj.date, obj.client, obj.k8sClient)
	if err != nil {
		return err
	}

	for source, value := range accumulated {
		if value == 0 {
			continue
		}

		logger.Info("accumulating source", "source", source)

		err := s.WithTransaction(ctx, func(tx *sqlx.Tx) error {
			tenant, err := tenantsmodel.Ensure(ctx, tx, &db.Tenant{Source: source.Tenant})
			if err != nil {
				return fmt.Errorf("tenants: %w", err)
			}

			category, err := categoriesmodel.Ensure(ctx, tx, &db.Category{Source: source.Zone + ":" + source.Namespace})
			if err != nil {
				return fmt.Errorf("categories: %w", err)
			}

			dateTime := datetimesmodel.New(source.Start)
			dateTime, err = datetimesmodel.Ensure(ctx, tx, dateTime)
			if err != nil {
				return fmt.Errorf("datetimes: %w", err)
			}

			product, err := productsmodel.GetBestMatch(ctx, tx, source.String(), source.Start)
			if err != nil {
				return fmt.Errorf("products: %w", err)
			}

			discount, err := discountsmodel.GetBestMatch(ctx, tx, source.String(), source.Start)
			if err != nil {
				return fmt.Errorf("discounts: %w", err)
			}

			query, err := queriesmodel.GetByName(ctx, tx, source.Query+":"+source.Zone)
			if err != nil {
				return fmt.Errorf("queries: %w", err)
			}

			quantity, err := convertUnit(query, value)
			if err != nil {
				return fmt.Errorf("unit convesion: %w", err)
			}
			storageFact := factsmodel.New(dateTime, query, tenant, category, product, discount, quantity)
			_, err = factsmodel.Ensure(ctx, tx, storageFact)
			if err != nil {
				return fmt.Errorf("facts: %w", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func convertUnit(query *db.Query, value uint64) (float64, error) {
	if query.Unit == "GB" || query.Unit == "GBDay" {
		return float64(value) / 1000 / 1000 / 1000, nil
	}
	if query.Unit == "KReq" {
		return float64(value) / 1000, nil
	}
	return 0, errors.New("Unknown query unit " + query.Unit)
}
