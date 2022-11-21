package database

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
)

// ObjectType defines model for DBaaS types
type ObjectType string

const (
	// PostgresDBaaSType represents postgres DBaaS type
	PostgresDBaaSType ObjectType = "postgres"
	// SosType represents object storage storage type
	SosType ObjectType = "object-storage-storage"
)

const provider = "exoscale"

const (
	querySos       = string(SosType) + ":" + provider
	defaultUnitSos = "GBDay"

	queryDBaaSPostgres = string(PostgresDBaaSType) + ":" + provider
	defaultUnitDBaaS   = "Instances"
)

// exoscale service types to query billing Database types
var (
	billingTypes = map[string]string{
		"pg": queryDBaaSPostgres,
	}
)

var (
	initConfigs = map[ObjectType]InitConfig{

		// ObjectStorage specific objects for billing database
		SosType: {
			products: []db.Product{
				{
					Source: querySos,
					Target: sql.NullString{String: "1402", Valid: true},
					Amount: 0.000726,
					Unit:   "GBDay",
					During: db.InfiniteRange(),
				},
			},
			discount: db.Discount{
				Source:   string(SosType),
				Discount: 0,
				During:   db.InfiniteRange(),
			},
			query: db.Query{
				Name:        querySos,
				Description: "Object Storage - Storage (exoscale.com)",
				Query:       "",
				Unit:        "GBDay",
				During:      db.InfiniteRange(),
			},
		},

		// Postgres specific objects for billing database
		PostgresDBaaSType: {
			products: generatePostgresProducts(),
			discount: db.Discount{
				Source:   string(PostgresDBaaSType),
				Discount: 0,
				During:   db.InfiniteRange(),
			},
			query: db.Query{
				Name:        queryDBaaSPostgres,
				Description: "Database Service - PostgreSQL (exoscale.com)",
				Query:       "",
				Unit:        defaultUnitDBaaS,
				During:      db.InfiniteRange(),
			},
		},
	}
)

// InitConfig is used to define and then save the initial configuration
type InitConfig struct {
	products []db.Product
	discount db.Discount
	query    db.Query
}

// Aggregated contains information needed to save the metrics of the different resource types in the database
type Aggregated struct {
	Key
	Organization string
	// Value represents the aggregate amount by Key of used service
	Value float64
}

// Key is the base64 key
type Key string

// NewKey creates new Key with slice of strings as inputs
func NewKey(tokens ...string) Key {
	return Key(base64.StdEncoding.EncodeToString([]byte(strings.Join(tokens, ";"))))
}

func (k *Key) String() string {
	if k == nil {
		return ""
	}
	tokens, err := k.DecodeKey()
	if err != nil {
		return ""
	}

	return fmt.Sprintf("Decoded key with tokens: %v", tokens)
}

// DecodeKey decodes Key with slice of strings as output
func (k *Key) DecodeKey() (tokens []string, err error) {
	if k == nil {
		return []string{}, fmt.Errorf("key not initialized")
	}
	decodedKey, err := base64.StdEncoding.DecodeString(string(*k))
	if err != nil {
		return []string{}, fmt.Errorf("cannot decode key %s: %w", k, err)
	}
	s := strings.Split(string(decodedKey), ";")
	return s, nil
}
