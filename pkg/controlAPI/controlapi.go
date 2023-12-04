package controlAPI

import (
	"context"
	"fmt"

	orgv1 "github.com/appuio/control-api/apis/organization/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetSalesOrder(ctx context.Context, k8sClient client.Client, orgId string) (string, error) {

	org := &orgv1.Organization{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: orgId}, org)
	if err != nil {
		return "", fmt.Errorf("cannot get Organization object '%s', err: %v", orgId, err)
	}
	if org.Status.SalesOrderName == "" {
		return "", fmt.Errorf("Cannot get SalesOrder from organization object '%s', err: %v", orgId, err)
	}
	return org.Status.SalesOrderName, nil
}
