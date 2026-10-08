package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/7akoom/ride-platform/infrastructure/gateway/internal/observability"
)

func TestEveryAnswerCarriesItsRequestIDAndFailuresAreLogged(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	runtime, err := observability.NewTracingRuntime(context.Background(), "gateway", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(nil)

	var logs bytes.Buffer
	logger := slog.New(observability.NewTraceLogHandler(slog.NewJSONHandler(&logs, nil)))

	seen := ""
	handler := traced(logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = observability.TraceID(r.Context())
		switch r.URL.Path {
		case "/v1/broken":
			w.WriteHeader(http.StatusInternalServerError)
		case "/v1/missing":
			w.WriteHeader(http.StatusNotFound)
		default:
			_, _ = w.Write([]byte("{}"))
		}
	}))

	ids := map[string]bool{}

	for _, path := range []string{"/v1/fine", "/v1/broken", "/v1/missing"} {
		request := httptest.NewRequest(http.MethodGet, path+"?phone=+9647500000000", nil)
		// A client cannot pick the trace.
		request.Header.Set("traceparent", "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
		answer := httptest.NewRecorder()

		handler.ServeHTTP(answer, request)

		id := answer.Header().Get("X-Request-Id")
		if len(id) != 32 || id != seen || id == "0af7651916cd43dd8448eb211c80319c" {
			t.Fatalf("%s: X-Request-Id %q, the request ran in trace %q", path, id, seen)
		}

		ids[id] = true
	}

	if len(ids) != 3 {
		t.Fatalf("requests share IDs: %v", ids)
	}

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines, want the failure and the refusal only: %s", len(lines), logs.String())
	}

	for i, want := range []struct {
		level, msg string
		status     float64
	}{{"ERROR", "request failed", 500}, {"INFO", "request refused", 404}} {
		var line map[string]any
		if err := json.Unmarshal([]byte(lines[i]), &line); err != nil {
			t.Fatal(err)
		}

		if line["level"] != want.level || line["msg"] != want.msg || line["status"] != want.status || line["trace_id"] == nil {
			t.Errorf("line %d: %v", i, line)
		}

		if strings.Contains(lines[i], "phone") || strings.Contains(lines[i], "9647500000000") {
			t.Errorf("the query string reached the log: %s", lines[i])
		}
	}
}
