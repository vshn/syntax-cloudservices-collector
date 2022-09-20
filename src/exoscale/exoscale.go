package exoscale

import (
	"fmt"
	egoscale "github.com/exoscale/egoscale/v2"
	"os"
)

const (
	keyEnvVariable    = "EXOSCALE_API_KEY"
	secretEnvVariable = "EXOSCALE_API_SECRET"

	// sosEndpoint has buckets across all zones
	sosEndpoint = "https://api-ch-gva-2.exoscale.com"
)

// LoadAPICredentials retrieves exoscale access and secret keys from environment variables
func LoadAPICredentials() (exoscaleAccessKey, exoscaleSecretKey string, err error) {
	APIKey := os.Getenv(keyEnvVariable)
	if APIKey == "" {
		return "", "", fmt.Errorf("cannot find environment variable %s", keyEnvVariable)
	}
	APISecret := os.Getenv(secretEnvVariable)
	if APISecret == "" {
		return "", "", fmt.Errorf("cannot find environment variable  %s", secretEnvVariable)
	}
	return APIKey, APISecret, nil
}

// CreateExoscaleClient creates exoscale client with given access and secret keys
func CreateExoscaleClient(exoscaleAccessKey, exoscaleSecretKey string) (*egoscale.Client, error) {
	options := egoscale.ClientOptWithAPIEndpoint(sosEndpoint)
	client, err := egoscale.NewClient(exoscaleAccessKey, exoscaleSecretKey, options)
	if err != nil {
		return nil, fmt.Errorf("cannot create Exoscale client: %w", err)
	}
	return client, err
}
