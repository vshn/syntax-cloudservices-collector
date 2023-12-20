package cloudscale

import "github.com/vshn/billing-collector-cloudservices/pkg/odoo"

const (
	// source format: 'query:zone:tenant:namespace' or 'query:zone:tenant:namespace:class'
	// We do not have real (prometheus) queries here, just random hardcoded strings.
	productIdStorage       = "appcat-cloudscale-object-storage-storage"
	productIdTrafficOut    = "appcat-cloudscale-object-storage-traffic-out"
	productIdQueryRequests = "appcat_object-storage-requests"
)

var (
	// SourceZone represents the zone of the bucket, not of the cluster where the request for the bucket originated.
	// All the zones we use here must be known to the appuio-odoo-adapter as well.
	sourceZones = []string{"cloudscale"}
)

var units = map[string]string{
	productIdStorage:       odoo.GBDay,
	productIdTrafficOut:    odoo.GB,
	productIdQueryRequests: odoo.KReq,
}
