package reporting

import (
	"context"
	"fmt"
	"reflect"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
	"github.com/jmoiron/sqlx"
	ctrl "sigs.k8s.io/controller-runtime"
)

func FactByRecord(ctx context.Context, tx *sqlx.Tx, dt *db.DateTime, record Record) (*db.Fact, error) {
	query, err := GetQueryByName(ctx, tx, record.QueryName)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}

	tenant, err := GetTenantBySource(ctx, tx, record.TenantSource)
	if err != nil {
		return nil, fmt.Errorf("tenant: %w", err)
	}

	category, err := GetCategory(ctx, tx, record.CategorySource)
	if err != nil {
		return nil, fmt.Errorf("category: %w", err)
	}

	product, err := GetBestMatchingProduct(ctx, tx, record.ProductSource, record.BillingDate)
	if err != nil {
		return nil, fmt.Errorf("product: %w", err)
	}

	discount, err := GetBestMatchingDiscount(ctx, tx, record.DiscountSource, record.BillingDate)
	if err != nil {
		return nil, fmt.Errorf("discount: %w", err)
	}

	fact, err := GetByFact(ctx, tx, &db.Fact{
		DateTimeId: dt.Id,
		QueryId:    query.Id,
		TenantId:   tenant.Id,
		CategoryId: category.Id,
		ProductId:  product.Id,
		DiscountId: discount.Id,
	})
	if err != nil {
		return nil, fmt.Errorf("fact: %w", err)
	}
	return fact, nil
}

func GetByFact(ctx context.Context, tx *sqlx.Tx, fact *db.Fact) (*db.Fact, error) {
	var facts []db.Fact
	err := sqlx.SelectContext(ctx, tx, &facts,
		`SELECT facts.* FROM facts WHERE date_time_id = $1 AND query_id = $2 AND tenant_id = $3 AND category_id = $4 AND product_id = $5 AND discount_id = $6`,
		fact.DateTimeId, fact.QueryId, fact.TenantId, fact.CategoryId, fact.ProductId, fact.DiscountId)
	if err != nil {
		return nil, fmt.Errorf("cannot get facts by fact %v: %w", fact, err)
	}
	if len(facts) == 0 {
		return nil, nil
	}
	return &facts[0], nil
}

func EnsureFact(ctx context.Context, tx *sqlx.Tx, ensureFact *db.Fact) (*db.Fact, error) {
	logger := ctrl.LoggerFrom(ctx)

	fact, err := GetByFact(ctx, tx, ensureFact)
	if err != nil {
		return nil, err
	}
	if fact == nil {
		fact, err = createFact(tx, ensureFact)
		if err != nil {
			return nil, err
		}
	} else {
		ensureFact.Id = fact.Id
		if !reflect.DeepEqual(fact, ensureFact) {
			logger.Info("updating fact", "id", fact.Id)
			err = updateFact(tx, ensureFact)
			if err != nil {
				return nil, err
			}
		}
	}
	return fact, nil
}

func createFact(p db.NamedPreparer, in *db.Fact) (*db.Fact, error) {
	var category db.Fact
	err := db.GetNamed(p, &category,
		"INSERT INTO facts (date_time_id, query_id, tenant_id, category_id, product_id, discount_id, quantity) VALUES (:date_time_id, :query_id, :tenant_id, :category_id, :product_id, :discount_id, :quantity) RETURNING *", in)
	if err != nil {
		err = fmt.Errorf("cannot create fact %v: %w", in, err)
	}
	return &category, err
}

func updateFact(p db.NamedPreparer, in *db.Fact) error {
	var fact db.Fact
	err := db.GetNamed(p, &fact,
		"UPDATE facts SET date_time_id=:date_time_id, query_id=:query_id, tenant_id=:tenant_id, category_id=:category_id, product_id=:product_id, discount_id=:discount_id, quantity=:quantity WHERE id=:id RETURNING *", in)
	if err != nil {
		err = fmt.Errorf("cannot update fact %v: %w", in, err)
	}
	return err
}

func NewFact(dateTime *db.DateTime, query *db.Query, tenant *db.Tenant, category *db.Category, product *db.Product, discount *db.Discount, quanity float64) *db.Fact {
	return &db.Fact{
		DateTimeId: dateTime.Id,
		QueryId:    query.Id,
		TenantId:   tenant.Id,
		CategoryId: category.Id,
		ProductId:  product.Id,
		DiscountId: discount.Id,
		Quantity:   quanity,
	}
}
