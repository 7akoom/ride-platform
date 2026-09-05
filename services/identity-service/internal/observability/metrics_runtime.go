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

type MetricsRuntime struct {
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

	authDurationView := sdkmetric.NewView(
		sdkmetric.Instrument{
			Name: "identity.auth.duration",
		},
		sdkmetric.Stream{
			Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
				Boundaries: latencyHistogramBoundariesSeconds,
				NoMinMax:   true,
			},
		},
	)

	otpDeliveryDurationView := sdkmetric.NewView(
		sdkmetric.Instrument{
			Name: "identity.otp.delivery.duration",
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
			authDurationView,
			otpDeliveryDurationView,
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
		meterProvider: meterProvider,
		server:        server,
	}, nil
}

func (r *MetricsRuntime) NewAuthMetrics() (
	*AuthMetrics,
	error,
) {
	if r == nil ||
		r.meterProvider == nil {
		return nil, errors.New(
			"metrics runtime is not configured",
		)
	}

	return NewAuthMetricsWithMeter(
		r.meterProvider.Meter(
			authMetricsInstrumentationName,
		),
	)
}

func (r *MetricsRuntime) Serve() error {
	if r == nil ||
		r.server == nil {
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
