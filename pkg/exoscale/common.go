package exoscale

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

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
)

// Aggregated contains information needed to save the metrics of the different resource types in the database
type Aggregated struct {
	Key
	Organization string
	// Value represents the aggregate amount by Key of used service
	Value float64
}

// Key is the base64 key
type Key string

// NewKey creates new Key with slice of strings as inputs
func NewKey(tokens ...string) Key {
	return Key(base64.StdEncoding.EncodeToString([]byte(strings.Join(tokens, ";"))))
}

func (k *Key) String() string {
	if k == nil {
		return ""
	}
	tokens, err := k.DecodeKey()
	if err != nil {
		return ""
	}

	return fmt.Sprintf("Decoded key with tokens: %v", tokens)
}

// DecodeKey decodes Key with slice of strings as output
func (k *Key) DecodeKey() (tokens []string, err error) {
	if k == nil {
		return []string{}, fmt.Errorf("key not initialized")
	}
	decodedKey, err := base64.StdEncoding.DecodeString(string(*k))
	if err != nil {
		return []string{}, fmt.Errorf("cannot decode key %s: %w", k, err)
	}
	s := strings.Split(string(decodedKey), ";")
	return s, nil
}

func fetchNamespaceWithOrganizationMap(ctx context.Context, k8sClient k8s.Client) (map[string]string, error) {
	logger := ctrl.LoggerFrom(ctx)

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
			logger.Info("Organization label not found in namespace", "namespace", ns.GetName())
			continue
		}
		namespaces[ns.GetName()] = org
	}
	return namespaces, nil
}
