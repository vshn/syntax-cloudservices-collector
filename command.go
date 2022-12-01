package main

import (
	"github.com/urfave/cli/v2"
)

const (
	keyEnvVariable          = "EXOSCALE_API_KEY"
	secretEnvVariable       = "EXOSCALE_API_SECRET"
	dbURLEnvVariable        = "ACR_DB_URL"
	k8sServerURLEnvVariable = "K8S_SERVER_URL"
	k8sTokenEnvVariable     = "K8S_TOKEN"
)

type command struct {
	clusterURL     string
	clusterToken   string
	databaseURL    string
	exoscaleKey    string
	exoscaleSecret string
}

func getExoscaleSecretFlag(exoscaleSecret *string) *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "exoscale-secret",
		Aliases:     []string{"s"},
		EnvVars:     []string{secretEnvVariable},
		Required:    true,
		Usage:       "The secret which has unrestricted SOS service access in an Exoscale organization",
		Destination: exoscaleSecret,
	}
}

func getExoscaleAccessKeyFlag(exoscaleKey *string) *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "exoscale-access-key",
		Aliases:     []string{"k"},
		EnvVars:     []string{keyEnvVariable},
		Required:    true,
		Usage:       "A key which has unrestricted SOS service access in an Exoscale organization",
		Destination: exoscaleKey,
	}
}

func getDatabaseURLFlag(databaseURL *string) *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "database-url",
		Aliases:     []string{"d"},
		EnvVars:     []string{dbURLEnvVariable},
		Required:    true,
		Usage:       "A PostgreSQL database URL where to save relevant metrics",
		Destination: databaseURL,
	}
}

func getK8sServerTokenURLFlag(clusterToken *string) *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "k8s-server-token",
		Aliases:     []string{"t"},
		EnvVars:     []string{k8sTokenEnvVariable},
		Required:    true,
		Usage:       "A Kubernetes server token which can view buckets.exoscale.crossplane.io resources",
		Destination: clusterToken,
	}
}

func getClusterURLFlag(clusterURL *string) *cli.StringFlag {
	return &cli.StringFlag{
		Name:        "k8s-server-url",
		Aliases:     []string{"u"},
		EnvVars:     []string{k8sServerURLEnvVariable},
		Required:    true,
		Usage:       "A Kubernetes server URL from where to get the data from",
		Destination: clusterURL,
	}
}
