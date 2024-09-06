package cmd

import (
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/vshn/billing-collector-cloudservices/pkg/odoo"

	"github.com/cloudscale-ch/cloudscale-go-sdk/v2"
	"github.com/urfave/cli/v2"
	cs "github.com/vshn/billing-collector-cloudservices/pkg/cloudscale"
	"github.com/vshn/billing-collector-cloudservices/pkg/kubernetes"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
)

const defaultTextForRequiredFlags = "<required>"
const defaultTextForOptionalFlags = "<optional>"

func CloudscaleCmds(allMetrics map[string]map[string]prometheus.Counter) *cli.Command {
	var (
		apiToken          string
		kubeconfig        string
		controlApiUrl     string
		controlApiToken   string
		days              int
		collectInterval   int
		billingHour       int
		odooURL           string
		odooOauthTokenURL string
		odooClientId      string
		odooClientSecret  string
		salesOrder        string
		clusterId         string
		cloudZone         string
		uom               string
	)
	return &cli.Command{
		Name:  "cloudscale",
		Usage: "Collect metrics from cloudscale",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "cloudscale-api-token", Usage: "API token for cloudscale",
				EnvVars: []string{"CLOUDSCALE_API_TOKEN"}, Destination: &apiToken, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "kubeconfig", Usage: "Path to a kubeconfig file which will be used instead of url/token flags if set",
				EnvVars: []string{"KUBECONFIG"}, Destination: &kubeconfig, Required: false, DefaultText: defaultTextForOptionalFlags},
			&cli.StringFlag{Name: "control-api-url", Usage: "URL of the APPUiO Cloud Control API",
				EnvVars: []string{"CONTROL_API_URL"}, Destination: &controlApiUrl, Required: false, DefaultText: defaultTextForOptionalFlags},
			&cli.StringFlag{Name: "control-api-token", Usage: "Token of the APPUiO Cloud Control API",
				EnvVars: []string{"CONTROL_API_TOKEN"}, Destination: &controlApiToken, Required: false, DefaultText: defaultTextForOptionalFlags},
			&cli.IntFlag{Name: "days", Usage: "Days of metrics to fetch since today, set to 0 to get current metrics",
				EnvVars: []string{"DAYS"}, Destination: &days, Value: 1, Required: false, DefaultText: defaultTextForOptionalFlags},
			&cli.StringFlag{Name: "odoo-url", Usage: "URL of the Odoo Metered Billing API",
				EnvVars: []string{"ODOO_URL"}, Destination: &odooURL, Value: "http://localhost:8080"},
			&cli.StringFlag{Name: "odoo-oauth-token-url", Usage: "Oauth Token URL to authenticate with Odoo metered billing API",
				EnvVars: []string{"ODOO_OAUTH_TOKEN_URL"}, Destination: &odooOauthTokenURL, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "odoo-oauth-client-id", Usage: "Client ID of the oauth client to interact with Odoo metered billing API",
				EnvVars: []string{"ODOO_OAUTH_CLIENT_ID"}, Destination: &odooClientId, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "odoo-oauth-client-secret", Usage: "Client secret of the oauth client to interact with Odoo metered billing API",
				EnvVars: []string{"ODOO_OAUTH_CLIENT_SECRET"}, Destination: &odooClientSecret, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "appuio-managed-sales-order", Usage: "Sales order id to save in the billing record for APPUiO Managed only",
				EnvVars: []string{"APPUIO_MANAGED_SALES_ORDER"}, Destination: &salesOrder, Required: false, DefaultText: defaultTextForOptionalFlags},
			&cli.StringFlag{Name: "cluster-id", Usage: "The cluster id to save in the billing record",
				EnvVars: []string{"CLUSTER_ID"}, Destination: &clusterId, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.StringFlag{Name: "cluster-zone", Usage: "The cluster zone to save in the billing record",
				EnvVars: []string{"CLOUD_ZONE"}, Destination: &cloudZone, Required: false, DefaultText: defaultTextForOptionalFlags},
			&cli.StringFlag{Name: "uom", Usage: "Unit of measure mapping between cloud services and Odoo16 in json format",
				EnvVars: []string{"UOM"}, Destination: &uom, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.IntFlag{Name: "collect-interval", Usage: "How often to collect the metrics from the Cloud Service in hours - 1-23",
				EnvVars: []string{"COLLECT_INTERVAL"}, Destination: &collectInterval, Required: true, DefaultText: defaultTextForRequiredFlags},
			&cli.IntFlag{Name: "billing-hour", Usage: "At what time to start collect the metrics (ex 6 would start running from 6)",
				EnvVars: []string{"BILLING_HOUR"}, Destination: &billingHour, Required: true, DefaultText: defaultTextForRequiredFlags},
		},
		Before: addCommandName,
		Action: func(c *cli.Context) error {
			logger := log.Logger(c.Context)
			var wg sync.WaitGroup

			logger.Info("Checking UOM mappings")
			mapping, err := odoo.LoadUOM(uom)
			if err != nil {
				return err
			}
			err = cs.CheckUnitExistence(mapping)
			if err != nil {
				return err
			}

			logger.Info("Creating cloudscale client")
			cloudscaleClient := cloudscale.NewClient(http.DefaultClient)
			cloudscaleClient.AuthToken = apiToken

			logger.Info("Creating k8s client")
			k8sClient, err := kubernetes.NewClient(kubeconfig, "", "")
			if err != nil {
				return fmt.Errorf("k8s client: %w", err)
			}

			k8sControlClient, err := kubernetes.NewClient("", controlApiUrl, controlApiToken)
			if err != nil {
				return fmt.Errorf("k8s control client: %w", err)
			}

			odooClient := odoo.NewOdooAPIClient(c.Context, odooURL, odooOauthTokenURL, odooClientId, odooClientSecret, logger, allMetrics["odooMetrics"])

			location, err := time.LoadLocation("Europe/Zurich")
			if err != nil {
				return fmt.Errorf("load loaction: %w", err)
			}

			o, err := cs.NewObjectStorage(cloudscaleClient, k8sClient, k8sControlClient, salesOrder, clusterId, cloudZone, mapping, allMetrics["providerMetrics"])
			if err != nil {
				return fmt.Errorf("object storage: %w", err)
			}

			if collectInterval < 1 || collectInterval > 23 {
				// Set to run once a day after billingHour in case the collectInterval is out of boundaries
				collectInterval = 23
			}

			wg.Add(1)
			go func() {
				for {
					if true {

						billingDate := time.Now().In(location)
						if days != 0 {
							billingDate = time.Date(billingDate.Year(), billingDate.Month(), billingDate.Day()-days, 0, 0, 0, 0, billingDate.Location())
						}

						logger.V(1).Info("Running cloudscale collector")
						metrics, err := o.GetMetrics(c.Context, billingDate)
						if err != nil {
							logger.Error(err, "could not collect cloudscale bucket metrics")
							wg.Done()
						}
						if len(metrics) == 0 {
							logger.Info("No data to export to odoo", "date", billingDate)
							time.Sleep(time.Hour)
							continue
						}
						logger.Info("Exporting data to Odoo", "billingHour", billingHour, "date", billingDate)
						err = odooClient.SendData(metrics)
						if err != nil {
							logger.Error(err, "could not export cloudscale bucket metrics")
						}
						time.Sleep(time.Hour * time.Duration(15))
					}
					time.Sleep(time.Minute * 1)
				}
			}()
			wg.Wait()
			os.Exit(1)
			return nil
		},
	}
}
