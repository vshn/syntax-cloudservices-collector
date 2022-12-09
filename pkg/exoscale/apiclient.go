package exoscale

import (
	"fmt"

	egoscale "github.com/exoscale/egoscale/v2"
)

const (
	// sosEndpoint has buckets across all zones
	sosEndpoint = "https://api-ch-gva-2.exoscale.com"
)

// Zones represents the available zones on the exoscale metrics collector
var Zones = []string{
	"at-vie-1",
	"bg-sof-1",
	"de-fra-1",
	"de-muc-1",
	"ch-gva-2",
	"ch-dk-2",
}

// NewClient creates exoscale client with given access and secret keys
func NewClient(exoscaleAccessKey, exoscaleSecret string) (*egoscale.Client, error) {
	return NewClientWithOptions(exoscaleAccessKey, exoscaleSecret)
}

func NewClientWithOptions(exoscaleAccessKey string, exoscaleSecret string, options ...egoscale.ClientOpt) (*egoscale.Client, error) {
	options = append(options, egoscale.ClientOptWithAPIEndpoint(sosEndpoint))
	client, err := egoscale.NewClient(exoscaleAccessKey, exoscaleSecret, options...)
	if err != nil {
		return nil, fmt.Errorf("cannot create Exoscale client: %w", err)
	}
	return client, nil
}
