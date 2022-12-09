package cmd

import (
	"fmt"
	"time"

	"github.com/urfave/cli/v2"
	"github.com/vshn/exoscale-metrics-collector/pkg/exoscale"
	"github.com/vshn/exoscale-metrics-collector/pkg/kubernetes"
	"github.com/vshn/exoscale-metrics-collector/pkg/log"
	ctrl "sigs.k8s.io/controller-runtime"
)

// billingHour represents the hour when metrics are collected
const billingHour = 6

func addCommandName(c *cli.Context) error {
	c.Context = log.NewLoggingContext(c.Context, log.Logger(c.Context).WithName(c.Command.Name))
	return nil
}

func ExoscaleCmds() *cli.Command {
	var (
		secret                string
		accessKey             string
		dbURL                 string
		kubernetesServerToken string
		kubernetesServerURL   string
		kubeconfig            string
	)
	return &cli.Command{
		Name:  "exoscale",
		Usage: "Collect metrics from exoscale",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "exoscale-secret",
				Aliases:     []string{"s"},
				EnvVars:     []string{"EXOSCALE_API_SECRET"},
				Required:    true,
				Usage:       "The secret which has unrestricted SOS service access in an Exoscale organization",
				Destination: &secret,
			},
			&cli.StringFlag{
				Name:        "exoscale-access-key",
				Aliases:     []string{"k"},
				EnvVars:     []string{"EXOSCALE_API_KEY"},
				Required:    true,
				Usage:       "A key which has unrestricted SOS service access in an Exoscale organization",
				Destination: &accessKey,
			},
			&cli.StringFlag{
				Name:        "database-url",
				EnvVars:     []string{"ACR_DB_URL"},
				Required:    true,
				Usage:       "A PostgreSQL database URL where to save relevant metrics",
				Destination: &dbURL,
			},
			&cli.StringFlag{
				Name:        "kubernetes-server-url",
				EnvVars:     []string{"KUBERNETES_SERVER_URL"},
				Required:    true,
				Usage:       "A Kubernetes server URL from where to get the data from",
				Destination: &kubernetesServerURL,
			},
			&cli.StringFlag{
				Name:        "kubernetes-server-token",
				EnvVars:     []string{"KUBERNETES_SERVER_TOKEN"},
				Required:    true,
				Usage:       "A Kubernetes server token which can view buckets.cloudscale.crossplane.io resources",
				Destination: &kubernetesServerToken,
			},
			&cli.StringFlag{
				Name:        "kubeconfig",
				EnvVars:     []string{"KUBECONFIG"},
				Usage:       "Path to a kubeconfig file which will be used instead of url/token flags if set",
				Destination: &kubeconfig,
			},
		},
		Before: addCommandName,
		Subcommands: []*cli.Command{
			{
				Name:   "objectstorage",
				Usage:  "Get metrics from object storage service",
				Before: addCommandName,
				Action: func(c *cli.Context) error {
					logger := log.Logger(c.Context)
					ctrl.SetLogger(logger)

					logger.Info("Creating Exoscale client")
					exoscaleClient, err := exoscale.NewClient(accessKey, secret)
					if err != nil {
						return fmt.Errorf("exoscale client: %w", err)
					}

					logger.Info("Creating k8s client")
					k8sClient, err := kubernetes.NewClient(kubeconfig, kubernetesServerURL, kubernetesServerToken)
					if err != nil {
						return fmt.Errorf("k8s client: %w", err)
					}

					now := time.Now().In(time.UTC)
					previousDay := now.Day() - 1
					billingDate := time.Date(now.Year(), now.Month(), previousDay, billingHour, 0, 0, 0, now.Location())

					o, err := exoscale.NewObjectStorage(exoscaleClient, k8sClient, dbURL, billingDate)
					if err != nil {
						return fmt.Errorf("object storage: %w", err)
					}
					return o.Execute(c.Context)
				},
			},
			{
				Name:   "dbaas",
				Usage:  "Get metrics from database service",
				Before: addCommandName,
				Action: func(c *cli.Context) error {
					logger := log.Logger(c.Context)
					ctrl.SetLogger(logger)

					logger.Info("Creating Exoscale client")
					exoscaleClient, err := exoscale.NewClient(accessKey, secret)
					if err != nil {
						return fmt.Errorf("exoscale client: %w", err)
					}

					logger.Info("Creating k8s client")
					k8sClient, err := kubernetes.NewClient(kubeconfig, kubernetesServerURL, kubernetesServerToken)
					if err != nil {
						return fmt.Errorf("k8s client: %w", err)
					}

					billingDate := time.Now().In(time.UTC).Truncate(time.Hour)

					o, err := exoscale.NewDBaaS(exoscaleClient, k8sClient, dbURL, billingDate)
					if err != nil {
						return fmt.Errorf("dbaas service: %w", err)
					}
					return o.Execute(c.Context)
				},
			},
		},
	}
}
