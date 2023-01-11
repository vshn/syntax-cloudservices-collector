//go:build integration

package cloudscale

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/vshn/exoscale-metrics-collector/pkg/test"
)

type ObjectStorageTestSuite struct {
	test.Suite
}

func (ts *ObjectStorageTestSuite) TestMetrics() {
	ts.Fail("not implemented")
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestObjectStorageTestSuite(t *testing.T) {
	suite.Run(t, new(ObjectStorageTestSuite))
}
