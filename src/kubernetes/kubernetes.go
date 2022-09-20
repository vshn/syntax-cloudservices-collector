package kubernetes

import (
	"context"
	"fmt"
	"github.com/vshn/provider-exoscale/apis"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Cluster struct {
	client.Client
	Name  string
	Url   string
	Token string
}

// ReadConf reads a yaml config file and unmarshalls it into an object of type Cluster
func ReadConf(filename string) (*[]Cluster, error) {
	buf, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("cannot read cluster config file %q: %w", filename, err)
	}

	var clusters []Cluster
	err = yaml.Unmarshal(buf, &clusters)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshall cluster config file %q: %w", filename, err)
	}

	return &clusters, err
}

// InitKubernetesClients creates as many k8s clients as there are cluster entries from the parsed file
// The function skips clients that cannot be initialized
func InitKubernetesClients(ctx context.Context, clusters *[]Cluster) error {
	log := ctrl.LoggerFrom(ctx)
	scheme := runtime.NewScheme()
	err := apis.AddToScheme(scheme)
	if err != nil {
		return fmt.Errorf("cannot add k8s exoscale scheme: %w", err)
	}
	noAvailableClients := true
	for index, cluster := range *clusters {
		config := rest.Config{Host: cluster.Url, BearerToken: cluster.Token}
		clientInstance, e := client.New(&config, client.Options{
			Scheme: scheme,
		})
		(*clusters)[index].Client = clientInstance
		if e != nil {
			// Continue in case a k8s client cannot be initiated.
			log.Info("Cannot initiate k8s client, skipping config", "cluster", cluster.Url, "error", e.Error())
			continue
		}
		noAvailableClients = false
	}
	if noAvailableClients {
		return fmt.Errorf("there are no valid k8s clients configured")
	}
	return nil
}
