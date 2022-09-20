package main

import (
	"github.com/urfave/cli/v2"
	"github.com/vshn/exoscale-metrics-collector/src/exoscale"
	"github.com/vshn/exoscale-metrics-collector/src/kubernetes"
	"github.com/vshn/exoscale-metrics-collector/src/sos"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	objectStorageName = "objectstorage"
)

type objectStorageCommand struct {
	clusterFilePath string
}

func NewCommand() *cli.Command {
	command := &objectStorageCommand{}
	return &cli.Command{
		Name:   objectStorageName,
		Usage:  "Get metrics from object storage service",
		Action: command.execute,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "path-to-cluster-file", Aliases: []string{"clusters"}, Required: true, TakesFile: true,
				Usage:       "File containing a list of clusters with the format server=token..",
				Destination: &command.clusterFilePath,
			},
		},
	}
}

func (c *objectStorageCommand) execute(ctx *cli.Context) error {
	log := AppLogger(ctx).WithName(objectStorageName)
	ctrl.SetLogger(log)
	log.Info("Loading Exoscale API credentials")
	accessKey, secretKey, err := exoscale.LoadAPICredentials()
	if err != nil {
		return err
	}
	log.Info("Creating Exoscale client")
	exoscaleClient, err := exoscale.CreateExoscaleClient(accessKey, secretKey)
	if err != nil {
		return err
	}
	log.Info("Reading input cluster file configuration")
	clusters, err := kubernetes.ReadConf(c.clusterFilePath)
	if err != nil {
		return err
	}
	log.Info("Creating k8s clients")
	err = kubernetes.InitKubernetesClients(ctx.Context, clusters)
	if err != nil {
		return err
	}

	o := sos.NewObjectStorage(exoscaleClient, clusters)
	err = o.Execute(ctx.Context)
	return err
}
