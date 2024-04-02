package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/urfave/cli/v2"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
	"github.com/vshn/billing-collector-cloudservices/pkg/odoo"
)

func SpksCMD() *cli.Command {
	var (
		prometheusQueryArr = [4]string{
			"count(max_over_time(crossplane_resource_info{kind=\"compositemariadbinstances\", service_level=\"standard\"}[1d:1d]))",
			"count(max_over_time(crossplane_resource_info{kind=\"compositemariadbinstances\", service_level=\"premium\"}[1d:1d]))",
			"count(max_over_time(crossplane_resource_info{kind=\"compositeredisinstances\", service_level=\"standard\"}[1d:1d]))",
			"count(max_over_time(crossplane_resource_info{kind=\"compositeredisinstances\", service_level=\"premium\"}[1d:1d]))",
		}
		odooURL           string
		odooOauthTokenURL string
		odooClientId      string
		odooClientSecret  string
		salesOrder        string
		prometheusURL     string
		UnitID            string
	)

	return &cli.Command{
		Name:   "spks",
		Usage:  "Collect metrics from spks",
		Before: addCommandName,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "odoo-url", Usage: "URL of the Odoo Metered Billing API",
				EnvVars: []string{"ODOO_URL"}, Destination: &odooURL, Value: "https://preprod.central.vshn.ch/api/v2/product_usage_report_POST"},
			&cli.StringFlag{Name: "odoo-oauth-token-url", Usage: "Oauth Token URL to authenticate with Odoo metered billing API",
				EnvVars: []string{"ODOO_OAUTH_TOKEN_URL"}, Destination: &odooOauthTokenURL, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "odoo-oauth-client-id", Usage: "Client ID of the oauth client to interact with Odoo metered billing API",
				EnvVars: []string{"ODOO_OAUTH_CLIENT_ID"}, Destination: &odooClientId, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "odoo-oauth-client-secret", Usage: "Client secret of the oauth client to interact with Odoo metered billing API",
				EnvVars: []string{"ODOO_OAUTH_CLIENT_SECRET"}, Destination: &odooClientSecret, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "sales-order", Usage: "Sales order for APPUiO Managed clusters",
				EnvVars: []string{"SALES_ORDER"}, Destination: &salesOrder, Required: false, DefaultText: defaultTextForOptionalFlags, Value: "S10121"},
			&cli.StringFlag{Name: "prometheus-url", Usage: "URL of the Prometheus API",
				EnvVars: []string{"PROMETHEUS_URL"}, Destination: &prometheusURL, Required: true, DefaultText: defaultTextForRequiredFlags, Value: "http://prometheus-monitoring-application.monitoring-application.svc.cluster.local:9090"},
			&cli.StringFlag{Name: "unit-id", Usage: "Unit ID for the consumed units",
				EnvVars: []string{"UNIT_ID"}, Destination: &UnitID, Required: true, DefaultText: defaultTextForRequiredFlags, Value: "uom_uom_68_b1811ca1"},
		},
		Action: func(c *cli.Context) error {
			// this function is intended to run once a day
			// it will query the prometheus metrics for the last 24 hours
			// it will be run via kubernetes cronjob at midnight
			logger := log.Logger(c.Context)
			logger.Info("starting spks data collector")

			logger.Info("Getting specific metric from thanos")

			client, err := api.NewClient(api.Config{
				Address: prometheusURL,
			})
			if err != nil {
				logger.Error(err, "Error creating Prometheus client")
				return err
			}

			v1api := v1.NewAPI(client)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			// send this value to odoo

			mariadbStandard, err := QueryPrometheus(ctx, v1api, prometheusQueryArr[0], logger)
			if err != nil {
				logger.Error(err, "Error querying Prometheus")
				return err
			}
			mariadbPremium, err := QueryPrometheus(ctx, v1api, prometheusQueryArr[1], logger)
			if err != nil {
				logger.Error(err, "Error querying Prometheus")
				return err
			}
			redisStandard, err := QueryPrometheus(ctx, v1api, prometheusQueryArr[2], logger)
			if err != nil {
				logger.Error(err, "Error querying Prometheus")
				return err
			}
			redisPremium, err := QueryPrometheus(ctx, v1api, prometheusQueryArr[3], logger)
			if err != nil {
				logger.Error(err, "Error querying Prometheus")
				return err
			}

			odooClient := odoo.NewOdooAPIClient(c.Context, odooURL, odooOauthTokenURL, odooClientId, odooClientSecret, logger)
			location, err := time.LoadLocation("Europe/Zurich")
			if err != nil {
				return fmt.Errorf("load loaction: %w", err)
			}

			from := time.Now().In(location).UTC()
			to := time.Now().In(location).Add(-(time.Hour * 24)).UTC()

			billingRecords := []odoo.OdooMeteredBillingRecord{
				{
					ProductID:            "appcat-spks-mariadb-standard",
					InstanceID:           "mariadb-standard",
					ItemDescription:      "appcat-spks-mariadb-standard",
					ItemGroupDescription: "SPKS",
					SalesOrder:           salesOrder,
					UnitID:               UnitID,
					ConsumedUnits:        float64(mariadbStandard),
					TimeRange: odoo.TimeRange{
						From: from,
						To:   to,
					},
				},
				{
					ProductID:            "appcat-spks-mariadb-premium",
					InstanceID:           "mariadb-premium",
					ItemDescription:      "appcat-spks-mariadb-premium",
					ItemGroupDescription: "SPKS",
					SalesOrder:           salesOrder,
					UnitID:               UnitID,
					ConsumedUnits:        float64(mariadbPremium),
					TimeRange: odoo.TimeRange{
						From: from,
						To:   to,
					},
				},
				{
					ProductID:            "appcat-spks-redis-standard",
					InstanceID:           "redis-standard",
					ItemDescription:      "appcat-spks-redis-standard",
					ItemGroupDescription: "SPKS",
					SalesOrder:           salesOrder,
					UnitID:               UnitID,
					ConsumedUnits:        float64(redisStandard),
					TimeRange: odoo.TimeRange{
						From: from,
						To:   to,
					},
				},
				{
					ProductID:            "appcat-spks-redis-premium",
					InstanceID:           "redis-premium",
					ItemDescription:      "appcat-spks-redis-premium",
					ItemGroupDescription: "SPKS",
					SalesOrder:           salesOrder,
					UnitID:               UnitID,
					ConsumedUnits:        float64(redisPremium),
					TimeRange: odoo.TimeRange{
						From: from,
						To:   to,
					},
				},
			}

			ticker := time.NewTicker(24 * time.Hour)
			sigint := make(chan os.Signal, 1)
			signal.Notify(sigint, os.Interrupt)

			for loop := true; loop; {
				select {
				case <-ticker.C:
					err = odooClient.SendData(billingRecords)
					if err != nil {
						logger.Error(err, "can't export data to Odoo")
						panic(err)
					}
				case <-sigint:
					loop = false
					// this one breaks select{} statement
					// immediately and finish function
					break
				}
			}
			return nil
		},
	}
}

func QueryPrometheus(ctx context.Context, v1api v1.API, query string, logger logr.Logger) (int, error) {
	result, warnings, err := v1api.Query(ctx, query, time.Now(), v1.WithTimeout(5*time.Second))
	if err != nil {
		logger.Error(err, "Error querying Prometheus")
		return 0, err
	}
	if len(warnings) > 0 {
		logger.Info("Warnings", "warnings from Prometheus query", warnings)
	}

	switch result.Type() {
	case model.ValVector:
		vectorVal := result.(model.Vector)
		if len(vectorVal) != 1 {
			return 0, fmt.Errorf("expected 1 result, got %d", len(vectorVal))
		}
	default:
		return 0, fmt.Errorf("result type is not Vector: %s", result.Type())

	}
	return int(result.(model.Vector)[0].Value), nil
}
