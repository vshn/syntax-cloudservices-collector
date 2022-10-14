package exoscale

import (
	"fmt"
	egoscale "github.com/exoscale/egoscale/v2"
)

// sosEndpoint has buckets across all zones
const sosEndpoint = "https://api-ch-gva-2.exoscale.com"

// InitClient creates exoscale client with given access and secret keys
func InitClient(exoscaleAccessKey, exoscaleSecret string) (*egoscale.Client, error) {
	options := egoscale.ClientOptWithAPIEndpoint(sosEndpoint)
	client, err := egoscale.NewClient(exoscaleAccessKey, exoscaleSecret, options)
	if err != nil {
		return nil, fmt.Errorf("cannot create Exoscale client: %w", err)
	}
	return client, nil
}
