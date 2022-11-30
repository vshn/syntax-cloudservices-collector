package dbaas

import (
	"context"
	"reflect"
	"testing"

	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/stretchr/testify/assert"
	db "github.com/vshn/exoscale-metrics-collector/pkg/database"
)

func TestDBaaS_aggregatedDBaaS(t *testing.T) {
	ctx := context.Background()

	key1 := db.NewKey("vshn-xyz", db.Hobbyist2.Plan, string(db.PostgresDBaaSType))
	key2 := db.NewKey("vshn-abc", db.Business128.Plan, string(db.PostgresDBaaSType))

	expectedAggregatedDBaaS := map[db.Key]db.Aggregated{
		key1: {
			Key:          key1,
			Organization: "org1",
			Value:        1,
		},
		key2: {
			Key:          key2,
			Organization: "org2",
			Value:        1,
		},
	}

	tests := map[string]struct {
		ctx                     Context
		expectedAggregatedDBaaS map[db.Key]db.Aggregated
	}{
		"given DBaaS details and Exoscale DBaasS, we should get the ExpectedAggregatedDBaasS": {
			ctx: Context{
				Context: ctx,
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
				exoscaleDBaasS: []*egoscale.DatabaseService{
					{
						Name: strToPointer("postgres-abc"),
						Type: strToPointer(string(db.PostgresDBaaSType)),
						Plan: strToPointer(db.Hobbyist2.Plan),
					},
					{
						Name: strToPointer("postgres-def"),
						Type: strToPointer(string(db.PostgresDBaaSType)),
						Plan: strToPointer(db.Business128.Plan),
					},
				},
				aggregatedDBaasS: map[db.Key]db.Aggregated{},
			},
			expectedAggregatedDBaaS: expectedAggregatedDBaaS,
		},
		"given DBaaS details and different names in Exoscale DBaasS, we should not get the ExpectedAggregatedDBaasS": {
			ctx: Context{
				Context: ctx,
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
				exoscaleDBaasS: []*egoscale.DatabaseService{
					{
						Name: strToPointer("postgres-123"),
						Type: strToPointer(string(db.PostgresDBaaSType)),
					},
					{
						Name: strToPointer("postgres-456"),
						Type: strToPointer(string(db.PostgresDBaaSType)),
					},
				},
				aggregatedDBaasS: map[db.Key]db.Aggregated{},
			},
			expectedAggregatedDBaaS: map[db.Key]db.Aggregated{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := aggregateDBaaS(&tc.ctx)
			assert.NoError(t, err)
			assert.True(t, reflect.DeepEqual(tc.expectedAggregatedDBaaS, tc.ctx.aggregatedDBaasS))
		})
	}
}

func TestDBaaS_filterSupportedServiceUsage(t *testing.T) {
	ctx := context.Background()

	tests := map[string]struct {
		ctx                    Context
		expectedExoscaleDBaasS []*egoscale.DatabaseService
	}{
		"given Exoscale DBaasS, we should get filtered by type ExpectedExoscaleDBaasS": {
			ctx: Context{
				Context:      ctx,
				dbaasDetails: []Detail{},
				exoscaleDBaasS: []*egoscale.DatabaseService{
					{
						Name: strToPointer("postgres-abc"),
						Type: strToPointer("pg"),
					},
					{
						Name: strToPointer("postgres-def"),
						Type: strToPointer("pg"),
					},
					{
						Name: strToPointer("mysql-abc"),
						Type: strToPointer("mysql"),
					},
					{
						Name: strToPointer("mysql-def"),
						Type: strToPointer("mysql"),
					},
					{
						Name: strToPointer("redis-abc"),
						Type: strToPointer("redis"),
					},
				},
				aggregatedDBaasS: map[db.Key]db.Aggregated{},
			},
			expectedExoscaleDBaasS: []*egoscale.DatabaseService{
				{
					Name: strToPointer("postgres-abc"),
					Type: strToPointer("pg"),
				},
				{
					Name: strToPointer("postgres-def"),
					Type: strToPointer("pg"),
				},
				{
					Name: strToPointer("mysql-abc"),
					Type: strToPointer("mysql"),
				},
				{
					Name: strToPointer("mysql-def"),
					Type: strToPointer("mysql"),
				},
			},
		},
		"given Exoscale DBaasS, we should not get ExpectedExoscaleDBaasS": {
			ctx: Context{
				Context:      ctx,
				dbaasDetails: []Detail{},
				exoscaleDBaasS: []*egoscale.DatabaseService{
					{
						Name: strToPointer("unknown-abc"),
						Type: strToPointer("unknown"),
					},
					{
						Name: strToPointer("kafka-def"),
						Type: strToPointer("kafka"),
					},
					{
						Name: strToPointer("redis-abc"),
						Type: strToPointer("redis"),
					},
				},
				aggregatedDBaasS: map[db.Key]db.Aggregated{},
			},
			expectedExoscaleDBaasS: []*egoscale.DatabaseService{},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			err := filterSupportedServiceUsage(&tc.ctx)
			assert.NoError(t, err)
			assert.ElementsMatch(t, tc.expectedExoscaleDBaasS, tc.ctx.exoscaleDBaasS)
		})
	}
}

func strToPointer(s string) *string {
	return &s
}
