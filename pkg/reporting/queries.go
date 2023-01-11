package reporting

import (
	"context"
	"fmt"
	"reflect"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
	"github.com/jmoiron/sqlx"
	ctrl "sigs.k8s.io/controller-runtime"
)

func GetQueryByName(ctx context.Context, tx *sqlx.Tx, name string) (*db.Query, error) {
	var queries []db.Query
	err := sqlx.SelectContext(ctx, tx, &queries, `SELECT queries.* FROM queries WHERE name = $1`, name)
	if err != nil {
		return nil, fmt.Errorf("cannot get queries by name %s: %w", name, err)
	}
	if len(queries) == 0 {
		return nil, nil
	}
	return &queries[0], nil
}

func EnsureQuery(ctx context.Context, tx *sqlx.Tx, ensureQuery *db.Query) (*db.Query, error) {
	logger := ctrl.LoggerFrom(ctx)

	query, err := GetQueryByName(ctx, tx, ensureQuery.Name)
	if err != nil {
		return nil, err
	}
	if query == nil {
		query, err = createQuery(tx, ensureQuery)
		if err != nil {
			return nil, err
		}
	} else {
		ensureQuery.Id = query.Id
		if !reflect.DeepEqual(query, ensureQuery) {
			logger.Info("updating query", "id", query.Id)
			err = updateQuery(tx, ensureQuery)
			if err != nil {
				return nil, err
			}
		}
	}
	return query, nil
}

func createQuery(p db.NamedPreparer, in *db.Query) (*db.Query, error) {
	var query db.Query
	err := db.GetNamed(p, &query,
		"INSERT INTO queries (parent_id, name, description, query, unit, during) VALUES (:parent_id, :name, :description, :query, :unit, :during) RETURNING *", in)
	if err != nil {
		err = fmt.Errorf("cannot create query %v: %w", in, err)
	}
	return &query, err
}

func updateQuery(p db.NamedPreparer, in *db.Query) error {
	var query db.Query
	err := db.GetNamed(p, &query,
		"UPDATE queries SET name=:name, description=:description, query=:query, unit=:unit, during=:during, parent_id=:parent_id WHERE id=:id RETURNING *", in)
	if err != nil {
		err = fmt.Errorf("cannot update query %v: %w", in, err)
	}
	return err
}
