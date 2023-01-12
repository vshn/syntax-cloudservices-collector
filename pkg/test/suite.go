package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/appuio/appuio-cloud-reporting/pkg/db"
	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v4"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap/zaptest"
	"gopkg.in/dnaeon/go-vcr.v3/cassette"
	"gopkg.in/dnaeon/go-vcr.v3/recorder"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
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

	DatabaseURL   string
	tmpDBName     string
	maintenanceDB *sqlx.DB
}

// SetupSuite is used for setting up the testsuite before all tests are run. If you need to override it, make sure to call `SetupEnv()` first.
func (ts *Suite) SetupSuite() {
	ts.SetupEnv(nil)
}

// SetupTest ensures a separate temporary DB for each test
func (ts *Suite) SetupTest() {
	ts.setupDB()
}

// TearDownTest cleans up temporary DB after each test
func (ts *Suite) TearDownTest() {
	assert := ts.Assert()
	assert.NoError(dropDB(ts.maintenanceDB, pgx.Identifier{ts.tmpDBName}))
	assert.NoError(ts.maintenanceDB.Close())
}

func (ts *Suite) SetupEnv(crdPaths []string) {
	ts.T().Helper()

	assert := ts.Assert()

	ts.Logger = zapr.NewLogger(zaptest.NewLogger(ts.T()))
	log.SetLogger(ts.Logger)

	ts.Context, ts.cancel = context.WithCancel(context.Background())

	envtestAssets, ok := os.LookupEnv("KUBEBUILDER_ASSETS")
	if !ok {
		ts.FailNow("The environment variable KUBEBUILDER_ASSETS is undefined. Configure your IDE to set this variable when running the integration test.")
	}

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     crdPaths,
		ErrorIfCRDPathMissing: true,
		BinaryAssetsDirectory: envtestAssets,
	}

	config, err := testEnv.Start()
	assert.NoError(err)
	assert.NotNil(config)

	ts.Scheme = runtime.NewScheme()
	ts.RegisterScheme(corev1.SchemeBuilder.AddToScheme)

	k8sClient, err := client.New(config, client.Options{
		Scheme: ts.Scheme,
	})
	assert.NoError(err)
	assert.NotNil(k8sClient)

	ts.Env = testEnv
	ts.Config = config
	ts.Client = k8sClient
}

func (ts *Suite) RequestRecorder(path string) (*http.Client, func(), error) {
	ts.T().Helper()

	r, err := recorder.NewWithOptions(&recorder.Options{
		CassetteName:       path,
		Mode:               recorder.ModeRecordOnce,
		RealTransport:      nil,
		SkipRequestLatency: true,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("recorder: %w", err)
	}
	cancel := func() {
		if err := r.Stop(); err != nil {
			ts.T().Logf("recorder stop: %v", err)
		}
	}

	r.AddHook(func(i *cassette.Interaction) error {
		// ensure API token is not stored in recorded response
		delete(i.Request.Headers, "Authorization")
		return nil
	}, recorder.AfterCaptureHook)

	return r.GetDefaultClient(), cancel, nil
}

// RegisterScheme passes the current scheme to the given SchemeBuilder func.
func (ts *Suite) RegisterScheme(addToScheme func(s *runtime.Scheme) error) {
	ts.T().Helper()
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
	ts.T().Helper()

	for _, resource := range resources {
		ts.T().Logf("creating resource '%s/%s'", resource.GetNamespace(), resource.GetName())
		ts.Assert().NoError(ts.Client.Create(ts.Context, resource))
	}
}

// EnsureNS creates a new Namespace, validating the name in accordance with k8s validation rules.
func (ts *Suite) EnsureNS(name string, labels map[string]string) {
	ts.T().Helper()

	ts.Assert().Emptyf(validation.IsDNS1123Label(name), "'%s' does not appear to be a valid name for a namespace", name)
	ts.EnsureResources(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	})
}

func (ts *Suite) setupDB() {
	ts.T().Helper()

	assert := ts.Assert()

	databaseURL := os.Getenv("ACR_DB_URL")
	assert.NotZero(databaseURL)

	u, err := url.Parse(databaseURL)
	assert.NoError(err)

	dbName := strings.TrimPrefix(u.Path, "/")
	tmpDbName := dbName + "-tmp-" + uuid.NewString()
	ts.tmpDBName = tmpDbName

	// Connect to a neutral database
	mdb, err := openMaintenance(databaseURL)
	require.NoError(ts.T(), err)
	ts.maintenanceDB = mdb

	require.NoError(ts.T(),
		cloneDB(ts.maintenanceDB, pgx.Identifier{tmpDbName}, pgx.Identifier{dbName}),
	)

	// Connect to the temporary database
	tmpURL := new(url.URL)
	*tmpURL = *u
	tmpURL.Path = "/" + tmpDbName
	ts.T().Logf("Using database name: %s", tmpDbName)
	ts.DatabaseURL = tmpURL.String()
}

func cloneDB(maint *sqlx.DB, dst, src pgx.Identifier) error {
	_, err := maint.Exec(fmt.Sprintf(`CREATE DATABASE %s TEMPLATE %s`,
		dst.Sanitize(),
		src.Sanitize()))
	if err != nil {
		return fmt.Errorf("error cloning database `%s` to `%s`: %w", src.Sanitize(), dst.Sanitize(), err)
	}
	return nil
}

func dropDB(maint *sqlx.DB, name pgx.Identifier) error {
	_, err := maint.Exec(fmt.Sprintf(`DROP DATABASE %s WITH (FORCE)`, name.Sanitize()))
	if err != nil {
		return fmt.Errorf("error dropping database `%s`: %w", name.Sanitize(), err)
	}
	return nil
}

func openMaintenance(dbURL string) (*sqlx.DB, error) {
	maintURL, err := url.Parse(dbURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing url: %w", err)
	}
	maintURL.Path = "/postgres"
	mdb, err := db.Openx(maintURL.String())
	if err != nil {
		return nil, fmt.Errorf("error connecting to maintenance (`%s`) database: %w", maintURL.Path, err)
	}
	return mdb, nil
}
