package main

import (
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/7akoom/ride-platform/infrastructure/gateway/internal/observability"
)

// traced starts a trace for every request (the first part of it; each service
// the request reaches adds its own) and hands it to withRequestLog. A trace
// context sent by a client is not continued: the gateway always starts a new
// trace, so a client cannot choose IDs or force sampling.
func traced(logger *slog.Logger, next http.Handler) http.Handler {
	return otelhttp.NewHandler(
		withRequestLog(logger, next),
		"gateway",
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return "HTTP " + r.Method
		}),
	)
}

// withRequestLog returns the trace ID to the client as X-Request-Id (an app
// shows or reports it with an error; scripts/deploy/trace.sh and Grafana find
// the request by it) and writes one line per request that fails, is refused or
// is slow. Quick successes are not logged (their traces are enough). Only the
// method, the path, the status and the time are logged: never a header, a body
// or the query string.
func withRequestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := observability.TraceID(r.Context()); id != "" {
			w.Header().Set("X-Request-Id", id)
		}

		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()

		next.ServeHTTP(recorder, r)

		elapsed := time.Since(start)
		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", recorder.status),
			slog.Int64("duration_ms", elapsed.Milliseconds()),
		}

		switch {
		case recorder.status >= http.StatusInternalServerError:
			logger.LogAttrs(r.Context(), slog.LevelError, "request failed", attrs...)
		case recorder.status >= http.StatusBadRequest:
			logger.LogAttrs(r.Context(), slog.LevelInfo, "request refused", attrs...)
		case elapsed >= observability.SlowRequest:
			logger.LogAttrs(r.Context(), slog.LevelWarn, "request slow", attrs...)
		}
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}

	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true

	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the real writer (Flush...).
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
