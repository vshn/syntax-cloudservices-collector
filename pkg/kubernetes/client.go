package kubernetes

import (
	"context"
	"fmt"

	orgv1 "github.com/appuio/control-api/apis/organization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"

	cloudscaleapis "github.com/vshn/provider-cloudscale/apis"
	exoapis "github.com/vshn/provider-exoscale/apis"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// OrganizationLabel represents the label used for organization when fetching the metrics
	OrganizationLabel = "appuio.io/organization"
)

// NewClient creates a k8s client from the server url and token url
// If kubeconfig (path to it) is supplied, that takes precedence. Its use is mainly for local development
// since local clusters usually don't have a valid certificate.
func NewClient(kubeconfig, url, token string) (client.Client, error) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("core scheme: %w", err)
	}
	if err := exoapis.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("exoscale scheme: %w", err)
	}
	if err := cloudscaleapis.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("cloudscale scheme: %w", err)
	}
	if err := orgv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("control api org scheme: %w", err)
	}

	var c client.Client
	var err error
	config, err := restConfig(kubeconfig, url, token)
	if err != nil {
		return nil, fmt.Errorf("cannot initialize k8s client: %w", err)
	}
	if kubeconfig != "" || (url != "" && token != "") {
		c, err = client.New(config, client.Options{
			Scheme: scheme,
		})
		if err != nil {
			return nil, fmt.Errorf("cannot create new k8s client: %w", err)
		}
	} else {
		c, err = client.New(ctrl.GetConfigOrDie(), client.Options{
			Scheme: scheme,
		})
	}

	if err != nil {
		return nil, fmt.Errorf("cannot initialize k8s client: %w", err)
	}
	return c, nil

}

func restConfig(kubeconfig string, url string, token string) (*rest.Config, error) {
	// kubeconfig takes precedence if set.
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return &rest.Config{Host: url, BearerToken: token}, nil
}

func FetchNamespaceWithOrganizationMap(ctx context.Context, k8sClient client.Client) (map[string]string, error) {

	gvk := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "NamespaceList",
	}
	list := &metav1.PartialObjectMetadataList{}
	list.SetGroupVersionKind(gvk)

	err := k8sClient.List(ctx, list)
	if err != nil {
		return nil, fmt.Errorf("cannot get namespace list: %w", err)
	}

	namespaces := map[string]string{}
	for _, ns := range list.Items {
		orgLabel, ok := ns.GetLabels()[OrganizationLabel]
		if !ok {
			continue
		}
		namespaces[ns.GetName()] = orgLabel
	}
	return namespaces, nil
}
