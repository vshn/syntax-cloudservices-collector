package odoo

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/oauth2/clientcredentials"
)

const (
	GB           = "GB"
	GBDay        = "GBDay"
	KReq         = "KReq"
	InstanceHour = "InstanceHour"
)

type OdooAPIClient struct {
	odooURL     string
	logger      logr.Logger
	oauthClient *http.Client
	odooMetrics map[string]prometheus.Counter
}

type apiObject struct {
	Data []OdooMeteredBillingRecord `json:"data"`
}

type OdooMeteredBillingRecord struct {
	ProductID            string    `json:"product_id"`
	InstanceID           string    `json:"instance_id"`
	ItemDescription      string    `json:"item_description,omitempty"`
	ItemGroupDescription string    `json:"item_group_description,omitempty"`
	SalesOrder           string    `json:"sales_order_id"`
	UnitID               string    `json:"unit_id"`
	ConsumedUnits        float64   `json:"consumed_units"`
	TimeRange            TimeRange `json:"timerange"`
}

type TimeRange struct {
	From time.Time
	To   time.Time
}

func (t TimeRange) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.From.Format(time.RFC3339) + "/" + t.To.Format(time.RFC3339) + `"`), nil
}

func (t *TimeRange) UnmarshalJSON([]byte) error {
	return errors.New("Not implemented")
}

func NewOdooAPIClient(ctx context.Context, odooURL string, oauthTokenURL string, oauthClientId string, oauthClientSecret string, logger logr.Logger, odooMetrics map[string]prometheus.Counter) *OdooAPIClient {
	oauthConfig := clientcredentials.Config{
		ClientID:     oauthClientId,
		ClientSecret: oauthClientSecret,
		TokenURL:     oauthTokenURL,
	}
	oauthClient := oauthConfig.Client(ctx)
	return &OdooAPIClient{
		odooURL:     odooURL,
		logger:      logger,
		oauthClient: oauthClient,
		odooMetrics: odooMetrics,
	}
}

func (c OdooAPIClient) SendData(data []OdooMeteredBillingRecord) error {
	apiObject := apiObject{
		Data: data,
	}
	str, err := json.Marshal(apiObject)
	if err != nil {
		return err
	}
	resp, err := c.oauthClient.Post(c.odooURL, "application/json", bytes.NewBuffer(str))
	if err != nil {
		c.odooMetrics["odooFailed"].Inc()
		return err
	}

	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	c.logger.Info("Records sent to Odoo API", "status", resp.Status, "body", string(body), "numberOfRecords", len(data))

	if resp.StatusCode != 200 {
		c.odooMetrics["odooFailed"].Inc()
		return errors.New(fmt.Sprintf("API error when sending records to Odoo:\n%s", body))
	} else {
		c.odooMetrics["odooSucceeded"].Inc()
	}

	return nil
}

func LoadUOM(uom string) (m map[string]string, err error) {
	err = json.Unmarshal([]byte(uom), &m)
	if err != nil || len(m) == 0 {
		return nil, fmt.Errorf("no unit of measure found: %v", err)
	}
	return m, nil
}
