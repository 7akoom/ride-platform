package observability

// tracing_runtime_test.go is copied byte-for-byte with tracing_runtime.go.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestATraceCrossesFromOneServiceToTheNext(t *testing.T) {
	t.Setenv(otlpEndpointEnv, "")

	runtime, err := NewTracingRuntime(context.Background(), "test-service", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(nil)

	if runtime.Exporting() {
		t.Fatal("exporting with no endpoint")
	}

	listener := bufconn.Listen(1 << 20)
	seen := make(chan string, 1)

	server := grpc.NewServer(
		GRPCServerOption(),
		grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
			seen <- TraceID(stream.Context())

			var in emptypb.Empty
			if err := stream.RecvMsg(&in); err != nil {
				return err
			}

			return stream.SendMsg(&emptypb.Empty{})
		}),
	)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		GRPCClientOption(),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx, span := otel.Tracer("test").Start(context.Background(), "caller")
	defer span.End()

	if err := conn.Invoke(ctx, "/test.Service/Do", &emptypb.Empty{}, &emptypb.Empty{}); err != nil {
		t.Fatal(err)
	}

	want := span.SpanContext().TraceID().String()
	if got := <-seen; got != want {
		t.Fatalf("the called service saw trace %q, want the caller's %q", got, want)
	}
}

func TestTheSampleRatioMustMakeSense(t *testing.T) {
	t.Setenv(otlpEndpointEnv, "")

	for _, bad := range []string{"two", "-0.1", "1.5"} {
		t.Setenv(traceSampleRatioEnv, bad)

		if _, err := NewTracingRuntime(context.Background(), "test-service", "test"); err == nil {
			t.Errorf("%s=%q was accepted", traceSampleRatioEnv, bad)
		}
	}
}

func logLines(t *testing.T, buffer *bytes.Buffer) []map[string]any {
	t.Helper()

	var lines []map[string]any

	for _, raw := range strings.Split(strings.TrimSpace(buffer.String()), "\n") {
		if raw == "" {
			continue
		}

		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("not JSON: %s", raw)
		}

		lines = append(lines, line)
	}

	return lines
}

func TestFailedAndRefusedRPCsAreLoggedWithTheirTrace(t *testing.T) {
	t.Setenv(otlpEndpointEnv, "")

	runtime, err := NewTracingRuntime(context.Background(), "test-service", "test")
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close(nil)

	var buffer bytes.Buffer
	logger := slog.New(NewTraceLogHandler(slog.NewJSONHandler(&buffer, nil)))
	interceptor := ErrorLogUnaryServerInterceptor(logger)
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Do"}

	ctx, span := otel.Tracer("test").Start(context.Background(), "request")
	defer span.End()

	answer := func(err error) grpc.UnaryHandler {
		return func(context.Context, any) (any, error) { return nil, err }
	}

	_, _ = interceptor(ctx, nil, info, answer(nil))
	_, _ = interceptor(ctx, nil, info, answer(status.Error(codes.Internal, "internal error")))
	_, _ = interceptor(ctx, nil, info, answer(status.Error(codes.NotFound, "trip not found")))

	lines := logLines(t, &buffer)
	if len(lines) != 2 {
		t.Fatalf("%d lines, want 2 (a quick success is not logged): %v", len(lines), lines)
	}

	want := []struct{ level, msg, code string }{
		{"ERROR", "rpc failed", "Internal"},
		{"INFO", "rpc refused", "NotFound"},
	}

	for i, w := range want {
		line := lines[i]
		if line["level"] != w.level || line["msg"] != w.msg || line["code"] != w.code {
			t.Errorf("line %d = %v, want %s %q %s", i, line, w.level, w.msg, w.code)
		}

		if line["trace_id"] != span.SpanContext().TraceID().String() || line["rpc"] != "/test.Service/Do" {
			t.Errorf("line %d does not carry the trace and the method: %v", i, line)
		}
	}
}

func TestLinesOutsideATraceHaveNoTraceID(t *testing.T) {
	var buffer bytes.Buffer
	logger := slog.New(NewTraceLogHandler(slog.NewJSONHandler(&buffer, nil))).With("service", "x")

	logger.InfoContext(context.Background(), "started")

	lines := logLines(t, &buffer)
	if len(lines) != 1 || lines[0]["trace_id"] != nil || lines[0]["service"] != "x" {
		t.Fatalf("got %v", lines)
	}
}
