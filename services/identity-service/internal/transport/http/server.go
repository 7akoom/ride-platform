package httptransport

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 10 * time.Second
	defaultWriteTimeout      = 10 * time.Second
	defaultIdleTimeout       = 60 * time.Second
	defaultMaxHeaderBytes    = 1 << 20
)

type Server struct {
	server *http.Server
}

func NewServer(
	address string,
	handler http.Handler,
) (*Server, error) {
	address = strings.TrimSpace(address)

	if address == "" {
		return nil, errors.New(
			"HTTP server address is required",
		)
	}

	if handler == nil {
		return nil, errors.New(
			"HTTP server handler is required",
		)
	}

	return &Server{
		server: &http.Server{
			Addr:              address,
			Handler:           handler,
			ReadHeaderTimeout: defaultReadHeaderTimeout,
			ReadTimeout:       defaultReadTimeout,
			WriteTimeout:      defaultWriteTimeout,
			IdleTimeout:       defaultIdleTimeout,
			MaxHeaderBytes:    defaultMaxHeaderBytes,
		},
	}, nil
}

func (s *Server) Run() error {
	if s == nil ||
		s.server == nil {
		return errors.New(
			"HTTP server is not configured",
		)
	}

	err := s.server.ListenAndServe()
	if errors.Is(
		err,
		http.ErrServerClosed,
	) {
		return nil
	}

	return err
}

func (s *Server) Shutdown(
	ctx context.Context,
) error {
	if s == nil ||
		s.server == nil {
		return nil
	}

	return s.server.Shutdown(ctx)
}

func (s *Server) Close() error {
	if s == nil ||
		s.server == nil {
		return nil
	}

	return s.server.Close()
}
