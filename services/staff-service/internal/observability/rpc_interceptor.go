package observability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

type rpcMetrics struct {
	requestCount metric.Int64Counter
	duration     metric.Float64Histogram
}

func newRPCMetrics(
	meter metric.Meter,
) (*rpcMetrics, error) {
	requestCount, err := meter.Int64Counter(
		"rpc.server.requests",
		metric.WithDescription(
			"Total gRPC requests received, by method and status code.",
		),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create rpc request counter: %w",
			err,
		)
	}

	duration, err := meter.Float64Histogram(
		"rpc.server.duration",
		metric.WithUnit(
			"s",
		),
		metric.WithDescription(
			"gRPC request duration in seconds, by method and status code.",
		),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create rpc duration histogram: %w",
			err,
		)
	}

	return &rpcMetrics{
		requestCount: requestCount,
		duration:     duration,
	}, nil
}

// UnaryServerInterceptor returns a gRPC unary server interceptor that
// records request count and duration for every RPC this service handles,
// labeled by method and status code. It requires no per-endpoint wiring:
// unlike identity-service's hand-instrumented domain metrics, this covers
// every method automatically, which is the deliberate MVP trade-off for
// every other service — deeper domain-specific metrics (like identity's
// auth/OTP duration histograms) can be added later where they earn their
// keep.
//
// Place this interceptor first (outermost) in the chain passed to
// grpcserver.NewServer, so it also captures requests rejected by later
// interceptors (e.g. an authentication failure still counts as a
// request).
func (r *MetricsRuntime) UnaryServerInterceptor() (
	grpc.UnaryServerInterceptor,
	error,
) {
	if r == nil || r.meterProvider == nil {
		return nil, errors.New(
			"metrics runtime is not configured",
		)
	}

	metrics, err := newRPCMetrics(
		r.meterProvider.Meter(
			r.serviceName + "/rpc",
		),
	)
	if err != nil {
		return nil, err
	}

	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		attrs := metric.WithAttributes(
			attribute.String(
				"rpc.method",
				info.FullMethod,
			),
			attribute.String(
				"rpc.grpc.status_code",
				status.Code(err).String(),
			),
		)

		metrics.requestCount.Add(
			ctx,
			1,
			attrs,
		)

		metrics.duration.Record(
			ctx,
			time.Since(start).Seconds(),
			attrs,
		)

		return resp, err
	}, nil
}
