package observability

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

const (
	metricsPath = "/metrics"

	metricsReadHeaderTimeout = 5 * time.Second
	metricsReadTimeout       = 10 * time.Second
	metricsWriteTimeout      = 10 * time.Second
	metricsIdleTimeout       = 60 * time.Second
)

// latencyHistogramBoundariesSeconds gives every latency histogram the same
// bucket layout, so dashboards line up across services.
var latencyHistogramBoundariesSeconds = []float64{
	0.005,
	0.010,
	0.025,
	0.050,
	0.100,
	0.250,
	0.500,
	1,
	2.5,
	5,
	10,
}

// MetricsRuntime owns this service's Prometheus registry, OTel meter
// provider, and the /metrics HTTP server. It is intentionally generic —
// no service-specific instruments live here; those are added by the
// RegisterXxx/NewXxx methods in the other files of this package (see
// outbox_metrics.go, rpc_interceptor.go). This file is meant to be
// copied byte-for-byte into every service; only the constructor's
// serviceName argument (passed by the caller in main.go) differs.
type MetricsRuntime struct {
	serviceName   string
	meterProvider *sdkmetric.MeterProvider
	server        *http.Server
}

func NewMetricsRuntime(
	serviceName string,
	address string,
) (*MetricsRuntime, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return nil, errors.New(
			"metrics service name is required",
		)
	}

	address = strings.TrimSpace(address)
	if address == "" {
		return nil, errors.New(
			"metrics address is required",
		)
	}

	registry := prometheus.NewRegistry()

	exporter, err := otelprometheus.New(
		otelprometheus.WithRegisterer(
			registry,
		),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create Prometheus metrics exporter: %w",
			err,
		)
	}

	serviceResource := resource.NewSchemaless(
		attribute.String(
			"service.name",
			serviceName,
		),
	)

	rpcDurationView := sdkmetric.NewView(
		sdkmetric.Instrument{
			Name: "rpc.server.duration",
		},
		sdkmetric.Stream{
			Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
				Boundaries: latencyHistogramBoundariesSeconds,
				NoMinMax:   true,
			},
		},
	)

	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(
			exporter,
		),
		sdkmetric.WithResource(
			serviceResource,
		),
		sdkmetric.WithView(
			rpcDurationView,
		),
	)

	mux := http.NewServeMux()

	mux.Handle(
		metricsPath,
		promhttp.HandlerFor(
			registry,
			promhttp.HandlerOpts{},
		),
	)

	server := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: metricsReadHeaderTimeout,
		ReadTimeout:       metricsReadTimeout,
		WriteTimeout:      metricsWriteTimeout,
		IdleTimeout:       metricsIdleTimeout,
	}

	return &MetricsRuntime{
		serviceName:   serviceName,
		meterProvider: meterProvider,
		server:        server,
	}, nil
}

// Serve blocks until the metrics HTTP server stops. A clean shutdown
// (via Shutdown) is not reported as an error.
func (r *MetricsRuntime) Serve() error {
	if r == nil || r.server == nil {
		return errors.New(
			"metrics runtime is not configured",
		)
	}

	err := r.server.ListenAndServe()
	if err == nil ||
		errors.Is(
			err,
			http.ErrServerClosed,
		) {
		return nil
	}

	return fmt.Errorf(
		"serve metrics endpoint: %w",
		err,
	)
}

func (r *MetricsRuntime) Shutdown(
	ctx context.Context,
) error {
	if r == nil {
		return nil
	}

	var shutdownErrors []error

	if r.server != nil {
		if err := r.server.Shutdown(
			ctx,
		); err != nil {
			shutdownErrors = append(
				shutdownErrors,
				fmt.Errorf(
					"shutdown metrics HTTP server: %w",
					err,
				),
			)
		}
	}

	if r.meterProvider != nil {
		if err := r.meterProvider.Shutdown(
			ctx,
		); err != nil {
			shutdownErrors = append(
				shutdownErrors,
				fmt.Errorf(
					"shutdown metrics meter provider: %w",
					err,
				),
			)
		}
	}

	return errors.Join(
		shutdownErrors...,
	)
}
