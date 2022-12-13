package cluster

import (
	"fmt"

	"github.com/vshn/provider-exoscale/apis"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InitK8sClient creates a k8s client from the server url and token url
func InitK8sClient(url, token string) (client.Client, error) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("core scheme: %w", err)
	}
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
	return k8sClient, nil
}

// InitK8sClientDynamic creates a dynamic k8s client from the server url and token url
func InitK8sClientDynamic(url, token string) (dynamic.Interface, error) {
	config := rest.Config{Host: url, BearerToken: token}
	k8sClient, err := dynamic.NewForConfig(&config)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize k8s client: %w", err)
	}
	return k8sClient, nil
}
