package observability

// tracing_runtime.go traces requests across services and ties log lines to
// them. Like metrics_runtime.go it is copied byte-for-byte into every service
// (and the gateway); scripts/tools/check-observability-copies.sh keeps the
// copies identical.
//
// Every request gets a trace: the gateway starts it, each service the request
// reaches adds its part (otelgrpc, both as a server and when it calls another
// service), and the trace ID travels in the gRPC metadata (W3C traceparent).
// The trace ID is what the gateway returns as X-Request-Id and what log lines
// carry as trace_id, so one ID finds a request everywhere.
//
// Traces are always made, so IDs exist and propagate even with nothing to
// send them to; they are exported only when OTEL_EXPORTER_OTLP_ENDPOINT is
// set (the Alloy agent on the server, which forwards to Grafana Cloud).
// OTEL_TRACES_SAMPLER_ARG keeps that share of traces (0..1, default 1).

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc/filters"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	otlpEndpointEnv     = "OTEL_EXPORTER_OTLP_ENDPOINT"
	traceSampleRatioEnv = "OTEL_TRACES_SAMPLER_ARG"

	// SlowRequest is how long a request may take before it is logged as slow.
	SlowRequest = time.Second
)

// TracingRuntime owns this process's tracer provider.
type TracingRuntime struct {
	provider  *sdktrace.TracerProvider
	exporting bool
}

// NewTracingRuntime installs the global tracer provider and the W3C trace
// context propagator.
func NewTracingRuntime(
	ctx context.Context,
	serviceName string,
	environment string,
) (*TracingRuntime, error) {
	if strings.TrimSpace(serviceName) == "" {
		return nil, errors.New("tracing needs the service name")
	}

	ratio := 1.0

	if value := strings.TrimSpace(os.Getenv(traceSampleRatioEnv)); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || parsed < 0 || parsed > 1 {
			return nil, fmt.Errorf("%s must be a number from 0 to 1", traceSampleRatioEnv)
		}

		ratio = parsed
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			attribute.String("service.name", serviceName),
			attribute.String("deployment.environment.name", environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("describe this service for tracing: %w", err)
	}

	options := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	}

	exporting := strings.TrimSpace(os.Getenv(otlpEndpointEnv)) != ""

	if exporting {
		// Reads OTEL_EXPORTER_OTLP_ENDPOINT itself; an http:// endpoint is
		// plain text (the agent sits on the same Docker network).
		exporter, err := otlptracegrpc.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("connect the trace exporter: %w", err)
		}

		options = append(options, sdktrace.WithBatcher(exporter))
	}

	provider := sdktrace.NewTracerProvider(options...)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return &TracingRuntime{provider: provider, exporting: exporting}, nil
}

// Exporting reports whether traces leave this process.
func (r *TracingRuntime) Exporting() bool {
	return r != nil && r.exporting
}

// Shutdown sends what is left and stops the provider.
func (r *TracingRuntime) Shutdown(ctx context.Context) error {
	if r == nil || r.provider == nil {
		return nil
	}

	return r.provider.Shutdown(ctx)
}

// GRPCServerOption traces every RPC this process serves, health checks aside.
func GRPCServerOption() grpc.ServerOption {
	return grpc.StatsHandler(
		otelgrpc.NewServerHandler(
			otelgrpc.WithFilter(filters.Not(filters.HealthCheck())),
		),
	)
}

// GRPCClientOption traces every call this process makes to another service and
// carries the trace along with it.
func GRPCClientOption() grpc.DialOption {
	return grpc.WithStatsHandler(
		otelgrpc.NewClientHandler(
			otelgrpc.WithFilter(filters.Not(filters.HealthCheck())),
		),
	)
}

// serverFault reports codes that mean this service (or one behind it) failed,
// as opposed to a request it refused.
func serverFault(code codes.Code) bool {
	switch code {
	case codes.Unknown, codes.Internal, codes.Unavailable, codes.DeadlineExceeded, codes.DataLoss, codes.Unimplemented:
		return true
	default:
		return false
	}
}

// ErrorLogUnaryServerInterceptor writes one line for every RPC that does not
// succeed, and for every slow one, with its trace_id (see TraceLogHandler):
//   - level ERROR "rpc failed": the service failed (internal, unavailable...)
//   - level INFO "rpc refused": the request was refused (invalid, not found,
//     not allowed...), which is normal but worth finding
//   - level WARN "rpc slow": it succeeded after SlowRequest or more
//
// Successful, quick RPCs are not logged (their traces are enough). The status
// message is the one the caller gets, so it carries no internal detail.
func ErrorLogUnaryServerInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	if logger == nil {
		panic("error log interceptor needs a logger")
	}

	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		elapsed := time.Since(start)

		attrs := []slog.Attr{
			slog.String("rpc", info.FullMethod),
			slog.Int64("duration_ms", elapsed.Milliseconds()),
		}

		if err != nil {
			st := status.Convert(err)
			attrs = append(attrs, slog.String("code", st.Code().String()), slog.String("message", st.Message()))

			if serverFault(st.Code()) {
				logger.LogAttrs(ctx, slog.LevelError, "rpc failed", attrs...)
			} else {
				logger.LogAttrs(ctx, slog.LevelInfo, "rpc refused", attrs...)
			}
		} else if elapsed >= SlowRequest {
			logger.LogAttrs(ctx, slog.LevelWarn, "rpc slow", attrs...)
		}

		return resp, err
	}
}

// TraceLogHandler adds trace_id and span_id to every line logged with a
// context that is part of a trace (logger.InfoContext, ErrorContext, LogAttrs).
type TraceLogHandler struct {
	next slog.Handler
}

// NewTraceLogHandler wraps a handler, e.g. slog.NewJSONHandler(os.Stdout, nil).
func NewTraceLogHandler(next slog.Handler) *TraceLogHandler {
	if next == nil {
		panic("trace log handler needs a handler to wrap")
	}

	return &TraceLogHandler{next: next}
}

func (h *TraceLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *TraceLogHandler) Handle(ctx context.Context, record slog.Record) error {
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", span.TraceID().String()),
			slog.String("span_id", span.SpanID().String()),
		)
	}

	return h.next.Handle(ctx, record)
}

func (h *TraceLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &TraceLogHandler{next: h.next.WithAttrs(attrs)}
}

func (h *TraceLogHandler) WithGroup(name string) slog.Handler {
	return &TraceLogHandler{next: h.next.WithGroup(name)}
}

// TraceID returns the ID of the trace ctx is part of, or "".
func TraceID(ctx context.Context) string {
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		return span.TraceID().String()
	}

	return ""
}

// Close is Shutdown with a deadline, for a deferred call in main.
func (r *TracingRuntime) Close(logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := r.Shutdown(ctx); err != nil && logger != nil {
		logger.Warn("traces not all sent at shutdown", "error", err)
	}
}
