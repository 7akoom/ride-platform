package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	googlegrpc "google.golang.org/grpc"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
)

func TestNewServerPanicsWhenAddressIsBlank(
	t *testing.T,
) {
	logger := slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)

	tests := []struct {
		name    string
		address string
	}{
		{
			name:    "empty",
			address: "",
		},
		{
			name:    "spaces",
			address: "   ",
		},
		{
			name:    "tabs and newlines",
			address: "\t\n ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered == nil {
					t.Fatal(
						"NewServer() did not panic for blank address",
					)
				}
			}()

			NewServer(
				tt.address,
				logger,
			)
		})
	}
}

func TestNewServerPanicsWhenLoggerIsNil(
	t *testing.T,
) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"NewServer() did not panic for nil logger",
			)
		}
	}()

	NewServer(
		":50051",
		nil,
	)
}

func TestServerRegisterIdentityServicePanicsWhenHandlerIsNil(
	t *testing.T,
) {
	logger := slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)

	server := NewServer(
		":50051",
		logger,
	)

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"RegisterIdentityService() did not panic for nil handler",
			)
		}
	}()

	server.RegisterIdentityService(nil)
}

func TestNewServerRegistersHealthServiceAsNotServing(
	t *testing.T,
) {
	logger := slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)

	server := NewServer(
		":50051",
		logger,
	)

	serviceInfo := server.grpcServer.GetServiceInfo()

	if _, exists := serviceInfo["grpc.health.v1.Health"]; !exists {
		t.Fatal(
			"gRPC health service is not registered",
		)
	}

	response, err := server.healthServer.Check(
		context.Background(),
		&healthv1.HealthCheckRequest{
			Service: "",
		},
	)
	if err != nil {
		t.Fatalf(
			"check initial gRPC health status: %v",
			err,
		)
	}

	if response.GetStatus() != healthv1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf(
			"initial gRPC health status = %s, expected %s",
			response.GetStatus(),
			healthv1.HealthCheckResponse_NOT_SERVING,
		)
	}
}

func TestServerRunTransitionsHealthAndStopsCleanly(
	t *testing.T,
) {
	logger := slog.New(
		slog.NewTextHandler(
			io.Discard,
			nil,
		),
	)

	server := NewServer(
		"127.0.0.1:0",
		logger,
	)

	runResult := make(chan error, 1)

	go func() {
		runResult <- server.Run()
	}()

	servingTimeout := time.NewTimer(
		2 * time.Second,
	)
	defer servingTimeout.Stop()

	pollTicker := time.NewTicker(
		5 * time.Millisecond,
	)
	defer pollTicker.Stop()

	for {
		response, err := server.healthServer.Check(
			context.Background(),
			&healthv1.HealthCheckRequest{
				Service: "",
			},
		)
		if err != nil {
			server.Stop()

			t.Fatalf(
				"check gRPC health while starting: %v",
				err,
			)
		}

		if response.GetStatus() == healthv1.HealthCheckResponse_SERVING {
			break
		}

		select {
		case err := <-runResult:
			t.Fatalf(
				"Run() exited before gRPC server became serving: %v",
				err,
			)

		case <-servingTimeout.C:
			server.Stop()

			t.Fatal(
				"gRPC server did not become serving before timeout",
			)

		case <-pollTicker.C:
		}
	}

	server.Stop()

	select {
	case err := <-runResult:
		if err != nil &&
			!errors.Is(
				err,
				googlegrpc.ErrServerStopped,
			) {
			t.Fatalf(
				"Run() returned unexpected error after Stop(): %v",
				err,
			)
		}

	case <-time.After(2 * time.Second):
		t.Fatal(
			"Run() did not return after Stop()",
		)
	}

	response, err := server.healthServer.Check(
		context.Background(),
		&healthv1.HealthCheckRequest{
			Service: "",
		},
	)
	if err != nil {
		t.Fatalf(
			"check gRPC health after Stop(): %v",
			err,
		)
	}

	if response.GetStatus() != healthv1.HealthCheckResponse_NOT_SERVING {
		t.Fatalf(
			"gRPC health status after Stop() = %s, expected %s",
			response.GetStatus(),
			healthv1.HealthCheckResponse_NOT_SERVING,
		)
	}
}
