package exoscale

import (
	"context"
	"testing"
	"time"

	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/stretchr/testify/assert"
	"github.com/vshn/billing-collector-cloudservices/pkg/exofixtures"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
)

func TestDBaaS_aggregatedDBaaS(t *testing.T) {
	ctx := getTestContext(t)

	key1 := NewKey("vshn-xyz", "hobbyist-2", string(exofixtures.PostgresDBaaSType))
	key2 := NewKey("vshn-abc", "business-128", string(exofixtures.PostgresDBaaSType))

	now := time.Now().In(location)
	record1 := odoo.OdooMeteredBillingRecord{
		ProductID:            "appcat-exoscale-dbaas-appcat_postgres-hobbyist-2",
		InstanceID:           "postgres-abc",
		ItemDescription:      "Exoscale DBaaS",
		ItemGroupDescription: "AppCat Exoscale DBaaS",
		SalesOrder:           "1234",
		UnitID:               "",
		ConsumedUnits:        1,
		TimeRange: odoo.TimeRange{
			From: time.Date(now.Year(), now.Month(), now.Day(), now.Hour()-1, 0, 0, 0, now.Location()),
			To:   time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location()),
		},
	}
	record2 := odoo.OdooMeteredBillingRecord{
		ProductID:  "appcat-exoscale-dbaas-appcat_postgres-business-128",
		InstanceID: "postgres-def", ItemDescription: "Exoscale DBaaS",
		ItemGroupDescription: "AppCat Exoscale DBaaS",
		SalesOrder:           "1234",
		UnitID:               "",
		ConsumedUnits:        1,
		TimeRange: odoo.TimeRange{
			From: time.Date(now.Year(), now.Month(), now.Day(), now.Hour()-1, 0, 0, 0, now.Location()),
			To:   time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location()),
		},
	}

	expectedAggregatedOdooRecords := []odoo.OdooMeteredBillingRecord{record1, record2}

	tests := map[string]struct {
		dbaasDetails            []Detail
		exoscaleDBaaS           []*egoscale.DatabaseService
		expectedAggregatedDBaaS map[Key]Aggregated
	}{
		"given DBaaS details and Exoscale DBaasS, we should get the ExpectedAggregatedDBaasS": {
			dbaasDetails: []Detail{
				{
					Organization: "org1",
					DBName:       "postgres-abc",
					Namespace:    "vshn-xyz",
					Zone:         "ch-gva-2",
				},
				{
					Organization: "org2",
					DBName:       "postgres-def",
					Namespace:    "vshn-abc",
					Zone:         "ch-gva-2",
				},
			},
			exoscaleDBaaS: []*egoscale.DatabaseService{
				{
					Name: strToPointer("postgres-abc"),
					Type: strToPointer(string(exofixtures.PostgresDBaaSType)),
					Plan: strToPointer("hobbyist-2"),
				},
				{
					Name: strToPointer("postgres-def"),
					Type: strToPointer(string(exofixtures.PostgresDBaaSType)),
					Plan: strToPointer("business-128"),
				},
			},
			expectedAggregatedDBaaS: expectedAggregatedDBaaS,
		},
		"given DBaaS details and different names in Exoscale DBaasS, we should not get the ExpectedAggregatedDBaasS": {
			dbaasDetails: []Detail{
				{
					Organization: "org1",
					DBName:       "postgres-abc",
					Namespace:    "vshn-xyz",
					Zone:         "ch-gva-2",
				},
				{
					Organization: "org2",
					DBName:       "postgres-def",
					Namespace:    "vshn-abc",
					Zone:         "ch-gva-2",
				},
			},
			exoscaleDBaaS: []*egoscale.DatabaseService{
				{
					Name: strToPointer("postgres-123"),
					Type: strToPointer(string(exofixtures.PostgresDBaaSType)),
				},
				{
					Name: strToPointer("postgres-456"),
					Type: strToPointer(string(exofixtures.PostgresDBaaSType)),
				},
			},

			expectedAggregatedDBaaS: map[Key]Aggregated{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			aggregatedDBaaS := aggregateDBaaS(ctx, tc.exoscaleDBaaS, tc.dbaasDetails)
			assert.Equal(t, tc.expectedAggregatedDBaaS, aggregatedDBaaS)
		})
	}
}

func strToPointer(s string) *string {
	return &s
}

func getTestContext(t assert.TestingT) context.Context {
	logger, err := log.NewLogger("test", time.Now().String(), 1, "console")
	assert.NoError(t, err, "cannot create logger")
	ctx := log.NewLoggingContext(context.Background(), logger)
	return ctx
}
