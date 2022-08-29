package main

import (
	"context"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"os"
)

type NamespaceInfo struct {
	K8sName      string
	Namespace    string
	Organization string
}

func (this *NamespaceInfo) String() string {
	if this == nil {
		return "nil"
	}
	org := "<unknown>"
	if this.Organization != "" {
		org = this.Organization
	}
	return fmt.Sprintf("[Namespace: %s/%s, Org: %s]", this.K8sName, this.Namespace, org)
}

func extractNamespaceInfoByBucket(k8sConfig *K8sConfig,
	list *unstructured.UnstructuredList,
	orgByNamespace map[string]string,
	namespaceInfoByBucket map[string]*NamespaceInfo) error {

	// helper data structure to look up existing NamespaceInfo instances
	namespaceInfos := make(map[string]*NamespaceInfo)

	// FIXME: The nested 'if's should be rewritten. Either use a flat 'if' hierarchy or find some way to use an object mapper to get typed access to the metadata.
	for _, objectBucket := range list.Items {
		if metadata, ok := objectBucket.UnstructuredContent()["metadata"]; ok {
			if metadataMap, ok := metadata.(map[string]interface{}); ok {
				if name, ok := metadataMap["name"]; ok {
					if nameStr, ok := name.(string); ok {
						if namespace, ok := metadataMap["namespace"]; ok {
							if namespaceString, ok := namespace.(string); ok {
								// Check if NamespaceInfo for this namespace already exists. Create it if not.
								// Note that we create it without organization name; that gets added later.
								if _, ok := namespaceInfos[namespaceString]; !ok {
									namespaceInfos[namespaceString] = &NamespaceInfo{K8sName: k8sConfig.Name, Organization: orgByNamespace[namespaceString], Namespace: namespaceString}
								}
								namespaceInfoByBucket[nameStr] = namespaceInfos[namespaceString]
							} else {
								fmt.Fprintf(os.Stderr, "ObjectBucket field [\"metadata\"][\"namespace\"] is not a string")
								continue
							}
						} else {
							fmt.Fprintf(os.Stderr, "ObjectBucket field [\"metadata\"][\"namespace\"] ] not found")
							continue
						}
					} else {
						fmt.Fprintf(os.Stderr, "ObjectBucket field [\"metadata\"][\"name\"] ] is not a string")
						continue
					}
				} else {
					fmt.Fprintf(os.Stderr, "ObjectBucket field [\"metadata\"][\"name\"] ] not found")
					continue
				}
			} else {
				fmt.Fprintf(os.Stderr, "ObjectBucket field [\"metadata\"] ] is not a map")
				continue
			}
		} else {
			fmt.Fprintf(os.Stderr, "ObjectBucket field [\"metadata\"] ] not found")
			continue
		}
	}
	return nil
}

func extractOrgByNamespace(list *unstructured.UnstructuredList) (map[string]string, error) {
	// FIXME: The nested 'if's should be rewritten. Either use a flat 'if' hierarchy or find some way to use an object mapper to get typed access to the metadata.
	orgByNamespace := make(map[string]string)
	for _, namespace := range list.Items {
		if metadata, ok := namespace.UnstructuredContent()["metadata"]; ok {
			if metadataMap, ok := metadata.(map[string]interface{}); ok {
				if name, ok := metadataMap["name"]; ok {
					if nameStr, ok := name.(string); ok {
						if labels, ok := metadataMap["labels"]; ok {
							if labelsMap, ok := labels.(map[string]interface{}); ok {
								if organization, ok := labelsMap[orgAnnotation]; ok {
									if organizationStr, ok := organization.(string); ok {
										orgByNamespace[nameStr] = organizationStr
									} else {
										fmt.Fprintf(os.Stderr, "Namespace '%s' field [\"metadata\"][\"labels\"][\"%s\"] is not a string, ignoring\n", nameStr, orgAnnotation)
										continue
									}
								} else {
									fmt.Fprintf(os.Stderr, "Namespace '%s' field [\"metadata\"][\"labels\"][\"%s\"] not found, ignoring\n", nameStr, orgAnnotation)
									continue
								}
							} else {
								fmt.Fprintf(os.Stderr, "Namespace '%s' field [\"metadata\"][\"labels\"] is not a map, ignoring\n", nameStr)
								continue
							}
						} else {
							fmt.Fprintf(os.Stderr, "Namespace '%s' field [\"metadata\"][\"labels\"] not found, ignoring\n", nameStr)
							continue
						}
					} else {
						fmt.Fprintf(os.Stderr, "Namespace field [\"metadata\"][\"name\"] is not a string, ignoring\n")
						continue
					}
				} else {
					fmt.Fprintf(os.Stderr, "Namespace field [\"metadata\"][\"name\"] not found, ignoring\n")
					continue
				}
			} else {
				fmt.Fprintf(os.Stderr, "Namespace field [\"metadata\"] is not a map, ignoring\n")
				continue
			}
		} else {
			fmt.Fprintf(os.Stderr, "Namespace field [\"metadata\"] not found, ignoring\n")
			continue
		}
	}
	return orgByNamespace, nil
}

func generateNamespaceLookupMap(ctx context.Context, k8sConfigs map[string]*K8sConfig) (map[string]*NamespaceInfo, error) {
	// Resulting data structure to look up NamespaceInfo from bucket name.
	// We aggregate all information from possibly multiple clusters in here, hence we give the same map instance to
	// multiple invocations of extractNamespaceInfoByBucket()
	namespaceInfoByBucket := make(map[string]*NamespaceInfo)

	for _, k8sConfig := range k8sConfigs {
		// connect to k8s cluster
		cfg := rest.Config{Host: k8sConfig.Api, BearerToken: k8sConfig.Token}
		k8s, err := dynamic.NewForConfig(&cfg)
		if err != nil {
			return nil, err
		}

		// Generate namespace to organization lookup table.
		// This fetches all namespaces from the cluster and looks at the annotations to find the organization.
		result, err := k8s.Resource(objectBucketsResource).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		orgByNamespace, err := extractOrgByNamespace(result)
		if err != nil {
			return nil, err
		}

		// Get ObjectBuckets and put information into namespaceInfoByBucket
		result, err = k8s.Resource(objectBucketsResource).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		err = extractNamespaceInfoByBucket(k8sConfig, result, orgByNamespace, namespaceInfoByBucket)
	}

	return namespaceInfoByBucket, nil
}
