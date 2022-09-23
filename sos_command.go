package main

import (
	"github.com/urfave/cli/v2"
	"github.com/vshn/exoscale-metrics-collector/pkg/exoscale"
	k8s "github.com/vshn/exoscale-metrics-collector/pkg/kubernetes"
	"github.com/vshn/exoscale-metrics-collector/pkg/sos"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	objectStorageName       = "objectstorage"
	keyEnvVariable          = "EXOSCALE_API_KEY"
	secretEnvVariable       = "EXOSCALE_API_SECRET"
	dbURLEnvVariable        = "ACR_DB_URL"
	k8sServerURLEnvVariable = "K8S_SERVER_URL"
	k8sTokenEnvVariable     = "K8S_TOKEN"
)

type objectStorageCommand struct {
	clusterURL     string
	clusterToken   string
	databaseURL    string
	exoscaleKey    string
	exoscaleSecret string
}

func NewCommand() *cli.Command {
	command := &objectStorageCommand{}
	return &cli.Command{
		Name:   objectStorageName,
		Usage:  "Get metrics from object storage service",
		Action: command.execute,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "k8s-server-url",
				Aliases:     []string{"u"},
				EnvVars:     []string{k8sServerURLEnvVariable},
				Required:    true,
				Usage:       "A Kubernetes server URL from where to get the data from",
				Destination: &command.clusterURL,
			},
			&cli.StringFlag{
				Name:        "k8s-server-token",
				Aliases:     []string{"t"},
				EnvVars:     []string{k8sTokenEnvVariable},
				Required:    true,
				Usage:       "A Kubernetes server token which can view buckets.exoscale.crossplane.io resources",
				Destination: &command.clusterToken,
			},
			&cli.StringFlag{
				Name:        "database-url",
				Aliases:     []string{"d"},
				EnvVars:     []string{dbURLEnvVariable},
				Required:    true,
				Usage:       "A PostgreSQL database URL where to save relevant metrics",
				Destination: &command.databaseURL,
			},
			&cli.StringFlag{
				Name:        "exoscale-access-key",
				Aliases:     []string{"k"},
				EnvVars:     []string{keyEnvVariable},
				Required:    true,
				Usage:       "A key which has unrestricted SOS service access in an Exoscale organization",
				Destination: &command.exoscaleKey,
			},
			&cli.StringFlag{
				Name:        "exoscale-secret",
				Aliases:     []string{"s"},
				EnvVars:     []string{secretEnvVariable},
				Required:    true,
				Usage:       "The secret which has unrestricted SOS service access in an Exoscale organization",
				Destination: &command.exoscaleSecret,
			},
		},
	}
}

func (c *objectStorageCommand) execute(ctx *cli.Context) error {
	log := AppLogger(ctx).WithName(objectStorageName)
	ctrl.SetLogger(log)

	log.Info("Creating Exoscale client")
	exoscaleClient, err := exoscale.InitClient(c.exoscaleKey, c.exoscaleSecret)
	if err != nil {
		return err
	}

	log.Info("Creating k8s client")
	k8sClient, err := k8s.InitK8sClient(c.clusterURL, c.clusterToken)
	if err != nil {
		return err
	}

	o := sos.NewObjectStorage(exoscaleClient, k8sClient, c.databaseURL)
	return o.Execute(ctx.Context)
}
