package reporting

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
	ctrl "sigs.k8s.io/controller-runtime"
)

type Store struct {
	db     *sqlx.DB
	logger logr.Logger
}

// NewStore opens up a db connection to the specified db.
func NewStore(url string, logger logr.Logger) (*Store, error) {
	rdb, err := db.Openx(url)
	if err != nil {
		return nil, fmt.Errorf("newReporting: open db failed: %w", err)
	}

	return &Store{
		db:     rdb,
		logger: logger,
	}, nil
}

// Close the db connection.
func (r *Store) Close() error {
	return r.db.Close()
}

// WithTransaction runs fn within a transaction and does commit/rollback when necessary.
func (r *Store) WithTransaction(ctx context.Context, fn func(*sqlx.Tx) error) error {
	tx, err := r.db.BeginTxx(ctx, &sql.TxOptions{})
	if err != nil {
		return fmt.Errorf("beginTransaction: transaction failed: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			// a panic occurred, rollback and repanic
			err := tx.Rollback()
			if err != nil && !errors.Is(err, sql.ErrTxDone) {
				r.logger.Error(err, "unable to rollback after panic")
			}
			panic(p)
		} else if err != nil {
			// something went wrong, rollback
			err := tx.Rollback()
			if err != nil && !errors.Is(err, sql.ErrTxDone) {
				r.logger.Error(err, "unable to rollback after error")
			}
		}
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("tx: unable to commit: %w", err)
	}
	return nil
}

// Initialize uses a transaction to ensure given entities in reporting store.
func (r *Store) Initialize(ctx context.Context, products []*db.Product, discounts []*db.Discount, queries []*db.Query) error {
	err := r.WithTransaction(ctx, func(tx *sqlx.Tx) error {
		for _, product := range products {
			_, err := EnsureProduct(ctx, tx, product)
			if err != nil {
				return fmt.Errorf("product ensure: %w", err)
			}
		}
		for _, discount := range discounts {
			_, err := EnsureDiscount(ctx, tx, discount)
			if err != nil {
				return fmt.Errorf("discount ensure: %w", err)
			}
		}
		for _, query := range queries {
			_, err := EnsureQuery(ctx, tx, query)
			if err != nil {
				return fmt.Errorf("query ensure: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	return nil
}

type Record struct {
	TenantSource   string
	CategorySource string
	BillingDate    time.Time
	ProductSource  string
	DiscountSource string
	QueryName      string
	Value          float64
}

func (r *Store) WriteRecord(ctx context.Context, record Record) error {
	return r.WithTransaction(ctx, func(tx *sqlx.Tx) error {
		tenant, err := EnsureTenant(ctx, tx, &db.Tenant{Source: record.TenantSource})
		if err != nil {
			return fmt.Errorf("EnsureTenant(%q): %w", record.TenantSource, err)
		}

		category, err := EnsureCategory(ctx, tx, &db.Category{Source: record.CategorySource})
		if err != nil {
			return fmt.Errorf("EnsureCategory(%q): %w", record.CategorySource, err)
		}

		dateTime, err := EnsureDateTime(ctx, tx, NewDateTime(record.BillingDate))
		if err != nil {
			return fmt.Errorf("EnsureDateTime(%q): %w", record.BillingDate, err)
		}

		product, err := GetBestMatchingProduct(ctx, tx, record.ProductSource, record.BillingDate)
		if err != nil {
			return fmt.Errorf("GetBestMatchingProduct(%q, %q): %w", record.ProductSource, record.BillingDate, err)
		}

		discount, err := GetBestMatchingDiscount(ctx, tx, record.DiscountSource, record.BillingDate)
		if err != nil {
			return fmt.Errorf("GetBestMatchingDiscount(%q, %q): %w", record.DiscountSource, record.BillingDate, err)
		}

		query, err := GetQueryByName(ctx, tx, record.QueryName)
		if err != nil {
			return fmt.Errorf("GetQueryByName(%q): %w", record.QueryName, err)
		}

		fact := NewFact(dateTime, query, tenant, category, product, discount, record.Value)
		if !isFactUpdatable(ctx, tx, fact, record.Value) {
			return nil
		}

		_, err = EnsureFact(ctx, tx, fact)
		if err != nil {
			return fmt.Errorf("EnsureFact: %w", err)
		}
		return nil
	})
}

// isFactUpdatable makes sure that only missing data or higher quantity values are saved in the billing database
func isFactUpdatable(ctx context.Context, tx *sqlx.Tx, f *db.Fact, value float64) bool {
	logger := ctrl.LoggerFrom(ctx)

	fact, _ := GetByFact(ctx, tx, f)
	if fact == nil || fact.Quantity < value {
		return true
	}
	logger.Info(fmt.Sprintf("skipped saving, higher or equal number already recorded in DB "+
		"for this hour: saved: \"%v\", new: \"%v\"", fact.Quantity, value))
	return false
}
