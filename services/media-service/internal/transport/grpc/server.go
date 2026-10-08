package grpc

import (
	"fmt"
	"github.com/7akoom/ride-platform/services/media-service/internal/observability"
	"log/slog"
	"net"
	"strings"

	mediav1 "github.com/7akoom/ride-platform/gen/go/ride/media/v1"
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

const maxStoredFileMessage = 80 << 20

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

	// A data export ZIP reaches StoreFile in one message (up to 64 MB).
	serverOptions := []googlegrpc.ServerOption{googlegrpc.MaxRecvMsgSize(maxStoredFileMessage)}
	serverOptions = append(serverOptions, observability.GRPCServerOption())

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

func (s *Server) RegisterMediaService(
	handler mediav1.MediaServiceServer,
) {
	if handler == nil {
		panic("media service handler is required")
	}

	mediav1.RegisterMediaServiceServer(
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
