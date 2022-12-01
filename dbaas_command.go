package main

import (
	"github.com/urfave/cli/v2"
	"github.com/vshn/exoscale-metrics-collector/pkg/clients/cluster"
	"github.com/vshn/exoscale-metrics-collector/pkg/clients/exoscale"
	"github.com/vshn/exoscale-metrics-collector/pkg/service/dbaas"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	dbaasName = "dbaas"
)

type dbaasCommand struct {
	command
}

func newDBaasSCommand() *cli.Command {
	command := &dbaasCommand{}
	return &cli.Command{
		Name:   dbaasName,
		Usage:  "Get metrics from database service",
		Action: command.execute,
		Flags: []cli.Flag{
			getClusterURLFlag(&command.clusterURL),
			getK8sServerTokenURLFlag(&command.clusterToken),
			getDatabaseURLFlag(&command.databaseURL),
			getExoscaleAccessKeyFlag(&command.exoscaleKey),
			getExoscaleSecretFlag(&command.exoscaleSecret),
		},
	}
}

func (c *dbaasCommand) execute(ctx *cli.Context) error {
	log := AppLogger(ctx).WithName(dbaasName)
	ctrl.SetLogger(log)

	log.Info("Creating Exoscale client")
	exoscaleClient, err := exoscale.InitClient(c.exoscaleKey, c.exoscaleSecret)
	if err != nil {
		return err
	}

	log.Info("Creating k8s client")
	k8sClient, err := cluster.InitK8sClientDynamic(c.clusterURL, c.clusterToken)
	if err != nil {
		return err
	}

	d := dbaas.NewDBaaSService(exoscaleClient, k8sClient, c.databaseURL)
	return d.Execute(ctx.Context)
}
