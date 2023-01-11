package exoscale

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	k8s "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// organizationLabel represents the label used for organization when fetching the metrics
	organizationLabel = "appuio.io/organization"
	// namespaceLabel represents the label used for namespace when fetching the metrics
	namespaceLabel = "crossplane.io/claim-namespace"

	// billingHour represents the hour when metrics are collected
	billingHour = 6
	// timeZone represents the time zone for billingHour
	timeZone = "UTC"
)

func fetchNamespaceWithOrganizationMap(ctx context.Context, k8sClient k8s.Client) (map[string]string, error) {
	log := ctrl.LoggerFrom(ctx)
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
		org, ok := ns.GetLabels()[organizationLabel]
		if !ok {
			log.Info("Organization label not found in namespace", "namespace", ns.GetName())
			continue
		}
		namespaces[ns.GetName()] = org
	}
	return namespaces, nil
}
