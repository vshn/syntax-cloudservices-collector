package prom

import (
	"context"
	"fmt"
	"github.com/appuio/appuio-cloud-reporting/pkg/thanos"
	"github.com/prometheus/client_golang/api"
	apiv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"time"
)

const (
	salesOrderPromMetrics = "control_api_organization_info"
	salesOrderIDLabel     = "sales_order_id"
)

func NewPrometheusAPIClient(promURL string) (apiv1.API, error) {
	rt := api.DefaultRoundTripper
	rt = &thanos.PartialResponseRoundTripper{
		RoundTripper: rt,
	}

	client, err := api.NewClient(api.Config{
		Address:      promURL,
		RoundTripper: rt,
	})

	return apiv1.NewAPI(client), err
}

// GetSalesOrderId retrieves from prometheus the sales order id associated to orgId
func GetSalesOrderId(ctx context.Context, client apiv1.API, orgId string) (string, error) {
	query := salesOrderPromMetrics + fmt.Sprintf("{name=\"%s\"}", orgId)

	res, _, err := client.Query(ctx, query, time.Now())
	if err != nil {
		return "", fmt.Errorf("cannot query '%s' for organisation %s, err: %v", salesOrderPromMetrics, orgId, err)
	}
	samples := res.(model.Vector)
	if samples.Len() > 1 {
		return "", fmt.Errorf("prometheus metric '%s' has multiple results for organisation %s ", salesOrderPromMetrics, orgId)
	}

	return getMetricLabel(samples[0].Metric)
}

func getMetricLabel(m model.Metric) (string, error) {
	value, ok := m[model.LabelName(salesOrderIDLabel)]
	if !ok {
		return "", fmt.Errorf("no '%s' label in metrics '%s'", salesOrderIDLabel, salesOrderPromMetrics)
	}
	return string(value), nil
}
