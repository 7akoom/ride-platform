package grpc

import (
	"fmt"
	"log/slog"
	"net"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	googlegrpc "google.golang.org/grpc"
)

type Server struct {
	address    string
	logger     *slog.Logger
	grpcServer *googlegrpc.Server
}

func NewServer(address string, logger *slog.Logger) *Server {
	return &Server{
		address:    address,
		logger:     logger,
		grpcServer: googlegrpc.NewServer(),
	}
}

func (s *Server) RegisterIdentityService(handler identityv1.IdentityServiceServer) {
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
	s.grpcServer.GracefulStop()
}

func (s *Server) Stop() {
	s.grpcServer.Stop()
}
