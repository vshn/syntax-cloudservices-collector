package exoscale

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestObjectStorage_getProductId(t *testing.T) {
	tests := map[string]struct {
		value      float64 // in GiB
		expectTier string
	}{
		"given SOS with below 512TiB capacity, we should get the Product Tier 1": {
			value:      300.1, // in GiB
			expectTier: productIdStorageTier1,
		},
		"given SOS with above 512 TiB and below 1Pib capacity, we should get the Product Tier 2": {
			value:      813000.4, // in GiB
			expectTier: productIdStorageTier2,
		},
		"given SOS with above 1Pib capacity, we should get the Product Tier 3": {
			value:      1300345.6, // in GiB
			expectTier: productIdStorageTier3,
		},
		"given SOS with below 0 capacity, we should get the Product Tier 1": {
			value:      0, // in GiB
			expectTier: productIdStorageTier1,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			tier := getProductId(tc.value)
			assert.Equal(t, tc.expectTier, tier)
		})
	}
}
