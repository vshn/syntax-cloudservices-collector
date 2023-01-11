package test

import (
	"context"
	"os"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap/zaptest"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Suite struct {
	suite.Suite

	NS      string
	Client  client.Client
	Config  *rest.Config
	Env     *envtest.Environment
	Logger  logr.Logger
	Context context.Context
	cancel  context.CancelFunc
	Scheme  *runtime.Scheme
}

func (ts *Suite) SetupSuite() {
	assert := ts.Assert()

	ts.Logger = zapr.NewLogger(zaptest.NewLogger(ts.T()))
	log.SetLogger(ts.Logger)

	ts.Context, ts.cancel = context.WithCancel(context.Background())

	envtestAssets, ok := os.LookupEnv("KUBEBUILDER_ASSETS")
	if !ok {
		ts.FailNow("The environment variable KUBEBUILDER_ASSETS is undefined. Configure your IDE to set this variable when running the integration test.")
	}

	testEnv := &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: envtestAssets,
	}

	config, err := testEnv.Start()
	assert.NoError(err)
	assert.NotNil(config)

	k8sClient, err := client.New(config, client.Options{
		Scheme: ts.Scheme,
	})
	assert.NoError(err)
	assert.NotNil(k8sClient)

	ts.Env = testEnv
	ts.Config = config
	ts.Client = k8sClient

	//TODO(mw): register CRDs and schemes
}

// RegisterScheme passes the current scheme to the given SchemeBuilder func.
func (ts *Suite) RegisterScheme(addToScheme func(s *runtime.Scheme) error) {
	ts.Assert().NoError(addToScheme(ts.Scheme))
}

// TearDownSuite implements suite.TearDownAllSuite.
// It is used to shut down the local envtest environment.
func (ts *Suite) TearDownSuite() {
	ts.Logger.Info("starting tear down")
	ts.cancel()
	err := ts.Env.Stop()
	ts.Assert().NoErrorf(err, "error while stopping test environment")
	ts.Logger.Info("test environment stopped")
}

// EnsureResources ensures that the given resources are existing in the suite. Each error will fail the test.
func (ts *Suite) EnsureResources(resources ...client.Object) {
	for _, resource := range resources {
		ts.T().Logf("creating resource '%s/%s'", resource.GetNamespace(), resource.GetName())
		ts.Assert().NoError(ts.Client.Create(ts.Context, resource))
	}
}
