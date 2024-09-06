package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
	"github.com/vshn/billing-collector-cloudservices/pkg/cmd"
	"github.com/vshn/billing-collector-cloudservices/pkg/log"
)

var (
	// these variables are populated by Goreleaser when releasing
	version = "unknown"
	commit  = "-dirty-"
	date    = time.Now().Format("2006-01-02")

	appName     = "billing-collector-cloudservices"
	appLongName = "Metrics collector which gathers metrics information for cloud services"

	odooFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "billing_cloud_collector_http_requests_odoo_failed_total",
		Help: "Total number of failed HTTP requests to Odoo",
	})
	odooSucceeded = promauto.NewCounter(prometheus.CounterOpts{
		Name: "billing_cloud_collector_http_requests_odoo_succeeded_total",
		Help: "Total number of successful HTTP requests to Odoo",
	})

	providerFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "billing_cloud_collector_http_requests_provider_failed_total",
		Help: "Total number of failed HTTP requests to the cloud provider",
	})

	providerSucceeded = promauto.NewCounter(prometheus.CounterOpts{
		Name: "billing_cloud_collector_http_requests_provider_succeeded_total",
		Help: "Total number of successful HTTP requests to the cloud provider",
	})

	providerMetrics = map[string]prometheus.Counter{
		"providerFailed":    providerFailed,
		"providerSucceeded": providerSucceeded,
	}

	odooMetrics = map[string]prometheus.Counter{
		"odooFailed":    odooFailed,
		"odooSucceeded": odooSucceeded,
	}

	allMetrics = map[string]map[string]prometheus.Counter{
		"odooMetrics":     odooMetrics,
		"providerMetrics": providerMetrics,
	}
)

func init() {
	// Remove `-v` short option from --version flag
	cli.VersionFlag.(*cli.BoolFlag).Aliases = nil
}

func main() {
	ctx, stop, app := newApp()
	defer stop()

	go func(ctx context.Context) {
		ctxx, cancel := context.WithCancel(ctx)
		defer cancel()
		select {
		case <-ctxx.Done():
			fmt.Println("Shutting down prometheus server")
			return
		default:
			http.Handle("/metrics", promhttp.Handler())
			err := http.ListenAndServe(":2112", nil)
			if err != nil {
				fmt.Println("Error starting prometheus server: ", err.Error())
			}
			os.Exit(1)
		}
	}(ctx)

	err := app.RunContext(ctx, os.Args)
	// If required flags aren't set, it will return with error before we could set up logging
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

}

func newApp() (context.Context, context.CancelFunc, *cli.App) {
	var (
		logLevel  int
		logFormat string
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)

	app := &cli.App{
		Name:    appName,
		Usage:   appLongName,
		Version: fmt.Sprintf("%s, revision=%s, date=%s", version, commit, date),

		EnableBashCompletion: true,

		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:        "log-level",
				Aliases:     []string{"v"},
				EnvVars:     []string{"LOG_LEVEL"},
				Usage:       "number of the log level verbosity",
				Value:       0,
				Destination: &logLevel,
			},
			&cli.StringFlag{
				Name:        "log-format",
				EnvVars:     []string{"LOG_FORMAT"},
				Usage:       "sets the log format (values: [json, console])",
				DefaultText: "console",
				Destination: &logFormat,
			},
			&cli.IntFlag{
				Name:  "collectInterval",
				Usage: "Interval in which the exporter checks the cloud resources",
				Value: 10,
			},
			&cli.IntFlag{
				Name:  "billingHour",
				Usage: "After which hour every day the objectstorage collector should start",
				Value: 6,
				Action: func(c *cli.Context, i int) error {
					if i > 23 || i < 0 {
						return fmt.Errorf("invalid billingHour value, needs to be between 0 and 23")
					}
					return nil
				},
			},
			&cli.StringFlag{
				Name:  "organizationOverride",
				Usage: "If the collector is collecting the metrics for an APPUiO managed instance. It needs to set the name of the customer.",
				Value: "",
			},
			&cli.StringFlag{
				Name:  "bind",
				Usage: "Golang bind string. Will be used for the exporter",
				Value: ":9123",
			},
		},
		Before: func(c *cli.Context) error {
			logger, err := log.NewLogger(appName, version, logLevel, logFormat)
			if err != nil {
				return fmt.Errorf("before: %w", err)
			}
			c.Context = log.NewLoggingContext(c.Context, logger)
			log.Logger(c.Context).WithValues(
				"date", date,
				"commit", commit,
				"go_os", runtime.GOOS,
				"go_arch", runtime.GOARCH,
				"go_version", runtime.Version(),
				"uid", os.Getuid(),
				"gid", os.Getgid(),
			).Info("Starting up " + appName)
			return nil
		},
		Action: func(c *cli.Context) error {
			if true {
				return cli.ShowAppHelp(c)
			}

			return nil
		},
		Commands: []*cli.Command{
			cmd.ExoscaleCmds(allMetrics),
			cmd.CloudscaleCmds(allMetrics),
			cmd.SpksCMD(allMetrics, ctx),
		},
		ExitErrHandler: func(c *cli.Context, err error) {
			if err != nil {
				log.Logger(c.Context).Error(err, "fatal error")
				cli.HandleExitCoder(cli.Exit("", 1))
			}
		},
	}

	return ctx, stop, app
}
