package database

import (
	"database/sql"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
)

type productDBaaS struct {
	Plan   string
	Target string
	Amount float64
}

var (
	// Hobbyist1 plan
	Hobbyist1 = productDBaaS{Plan: "hobbyist-1", Target: "1411", Amount: 0.06148}
	// Hobbyist2 plan
	Hobbyist2 = productDBaaS{Plan: "hobbyist-2", Target: "1411", Amount: 0.06683}
	// Startup4 plan
	Startup4 = productDBaaS{Plan: "startup-4", Target: "1411", Amount: 0.15731}
	// Startup8 plan
	Startup8 = productDBaaS{Plan: "startup-8", Target: "1411", Amount: 0.30889}
	// Startup16 plan
	Startup16 = productDBaaS{Plan: "startup-16", Target: "1411", Amount: 0.60507}
	// Startup32 plan
	Startup32 = productDBaaS{Plan: "startup-32", Target: "1411", Amount: 1.10238}
	// Startup64 plan
	Startup64 = productDBaaS{Plan: "startup-64", Target: "1411", Amount: 2.02408}
	// Startup128 plan
	Startup128 = productDBaaS{Plan: "startup-128", Target: "1411", Amount: 3.58055}
	// Startup225 plan
	Startup225 = productDBaaS{Plan: "startup-225", Target: "1411", Amount: 5.65519}
	// Business4 plan
	Business4 = productDBaaS{Plan: "business-4", Target: "1411", Amount: 0.30787}
	// Business8 plan
	Business8 = productDBaaS{Plan: "business-8", Target: "1411", Amount: 0.60525}
	// Business16 plan
	Business16 = productDBaaS{Plan: "business-16", Target: "1411", Amount: 1.17123}
	// Business32 plan
	Business32 = productDBaaS{Plan: "business-32", Target: "1411", Amount: 2.1285}
	// Business64 plan
	Business64 = productDBaaS{Plan: "business-64", Target: "1411", Amount: 3.80662}
	// Business128 plan
	Business128 = productDBaaS{Plan: "business-128", Target: "1411", Amount: 7.30291}
	// Business225 plan
	Business225 = productDBaaS{Plan: "business-225", Target: "1411", Amount: 9.97887}
	// Premium4 plan
	Premium4 = productDBaaS{Plan: "premium-4", Target: "1411", Amount: 0.44811}
	// Premium8 plan
	Premium8 = productDBaaS{Plan: "premium-8", Target: "1411", Amount: 0.86957}
	// Premium16 plan
	Premium16 = productDBaaS{Plan: "premium-16", Target: "1411", Amount: 1.72469}
	// Premium32 plan
	Premium32 = productDBaaS{Plan: "premium-32", Target: "1411", Amount: 3.14683}
	// Premium64 plan
	Premium64 = productDBaaS{Plan: "premium-64", Target: "1411", Amount: 5.64105}
	// Premium128 plan
	Premium128 = productDBaaS{Plan: "premium-128", Target: "1411", Amount: 9.49136}
	// Premium225 plan
	Premium225 = productDBaaS{Plan: "premium-225", Target: "1411", Amount: 14.84892}
)

var postgresProductDBaaS = []productDBaaS{
	Hobbyist2,
	Startup4,
	Startup8,
	Startup16,
	Startup32,
	Startup64,
	Startup128,
	Startup225,
	Business4,
	Business8,
	Business16,
	Business32,
	Business64,
	Business128,
	Business225,
	Premium4,
	Premium8,
	Premium16,
	Premium32,
	Premium64,
	Premium128,
	Premium225,
}

func generatePostgresProducts() []db.Product {
	products := make([]db.Product, 0, len(postgresProductDBaaS))
	for _, p := range postgresProductDBaaS {
		s := dbaasSourceString{
			Query:        queryDBaaSPostgres,
			Organization: "*",
			Namespace:    "*",
			Plan:         p.Plan,
		}
		product := db.Product{
			Source: s.getSourceString(),
			Target: sql.NullString{String: p.Target, Valid: true},
			Amount: p.Amount,
			Unit:   defaultUnitDBaaS,
			During: db.InfiniteRange(),
		}
		products = append(products, product)
	}
	return products
}
