package kubernetes

import (
	"fmt"
	"github.com/vshn/provider-exoscale/apis"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InitK8sClient creates a k8s client from the server url and token url
func InitK8sClient(url, token string) (*client.Client, error) {
	scheme := runtime.NewScheme()
	err := apis.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("cannot add k8s exoscale scheme: %w", err)
	}
	config := rest.Config{Host: url, BearerToken: token}
	k8sClient, err := client.New(&config, client.Options{
		Scheme: scheme,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot initialize k8s client: %w", err)
	}
	return &k8sClient, nil
}
