package grpc

import (
	"fmt"
	"log/slog"
	"net"
	"strings"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
)

type Server struct {
	address      string
	logger       *slog.Logger
	grpcServer   *googlegrpc.Server
	healthServer *health.Server
}

func NewServer(
	address string,
	logger *slog.Logger,
	unaryInterceptors ...googlegrpc.UnaryServerInterceptor,
) *Server {
	if strings.TrimSpace(address) == "" {
		panic("gRPC server address is required")
	}

	if logger == nil {
		panic("gRPC server logger is required")
	}

	serverOptions := make([]googlegrpc.ServerOption, 0, 1)

	if len(unaryInterceptors) > 0 {
		serverOptions = append(
			serverOptions,
			googlegrpc.ChainUnaryInterceptor(
				unaryInterceptors...,
			),
		)
	}

	grpcServer := googlegrpc.NewServer(
		serverOptions...,
	)
	healthServer := health.NewServer()

	healthv1.RegisterHealthServer(
		grpcServer,
		healthServer,
	)

	healthServer.SetServingStatus(
		"",
		healthv1.HealthCheckResponse_NOT_SERVING,
	)

	return &Server{
		address:      address,
		logger:       logger,
		grpcServer:   grpcServer,
		healthServer: healthServer,
	}
}

func (s *Server) RegisterIdentityService(
	handler identityv1.IdentityServiceServer,
) {
	if handler == nil {
		panic("identity service handler is required")
	}

	identityv1.RegisterIdentityServiceServer(
		s.grpcServer,
		handler,
	)
}

func (s *Server) Run() error {
	listener, err := net.Listen("tcp", s.address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", s.address, err)
	}

	s.healthServer.SetServingStatus(
		"",
		healthv1.HealthCheckResponse_SERVING,
	)

	s.logger.Info(
		"gRPC server listening",
		"address", s.address,
	)

	if err := s.grpcServer.Serve(listener); err != nil {
		return fmt.Errorf("serve gRPC: %w", err)
	}

	return nil
}

func (s *Server) GracefulStop() {
	s.healthServer.Shutdown()
	s.grpcServer.GracefulStop()
}

func (s *Server) Stop() {
	s.healthServer.Shutdown()
	s.grpcServer.Stop()
}
