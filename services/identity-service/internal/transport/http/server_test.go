package httptransport

import (
	"context"
	"net/http"
	"testing"
)

func TestNewServerRejectsBlankAddress(
	t *testing.T,
) {
	_, err := NewServer(
		"   ",
		http.NewServeMux(),
	)

	if err == nil {
		t.Fatal(
			"NewServer() accepted blank address",
		)
	}
}

func TestNewServerRejectsNilHandler(
	t *testing.T,
) {
	_, err := NewServer(
		":8080",
		nil,
	)

	if err == nil {
		t.Fatal(
			"NewServer() accepted nil handler",
		)
	}
}

func TestNewServerConfiguresHTTPServer(
	t *testing.T,
) {
	handler := http.NewServeMux()

	server, err := NewServer(
		" :8080 ",
		handler,
	)
	if err != nil {
		t.Fatalf(
			"NewServer() returned an error: %v",
			err,
		)
	}

	if server.server == nil {
		t.Fatal(
			"NewServer() returned nil HTTP server",
		)
	}

	if server.server.Addr != ":8080" {
		t.Fatalf(
			"server address = %q, want %q",
			server.server.Addr,
			":8080",
		)
	}

	if server.server.Handler != handler {
		t.Fatal(
			"server handler was not preserved",
		)
	}

	if server.server.ReadHeaderTimeout !=
		defaultReadHeaderTimeout {
		t.Fatalf(
			"ReadHeaderTimeout = %v, want %v",
			server.server.ReadHeaderTimeout,
			defaultReadHeaderTimeout,
		)
	}

	if server.server.ReadTimeout !=
		defaultReadTimeout {
		t.Fatalf(
			"ReadTimeout = %v, want %v",
			server.server.ReadTimeout,
			defaultReadTimeout,
		)
	}

	if server.server.WriteTimeout !=
		defaultWriteTimeout {
		t.Fatalf(
			"WriteTimeout = %v, want %v",
			server.server.WriteTimeout,
			defaultWriteTimeout,
		)
	}

	if server.server.IdleTimeout !=
		defaultIdleTimeout {
		t.Fatalf(
			"IdleTimeout = %v, want %v",
			server.server.IdleTimeout,
			defaultIdleTimeout,
		)
	}

	if server.server.MaxHeaderBytes !=
		defaultMaxHeaderBytes {
		t.Fatalf(
			"MaxHeaderBytes = %d, want %d",
			server.server.MaxHeaderBytes,
			defaultMaxHeaderBytes,
		)
	}
}

func TestServerRunRejectsUnconfiguredServer(
	t *testing.T,
) {
	var server *Server

	if err := server.Run(); err == nil {
		t.Fatal(
			"Run() returned nil error for unconfigured server",
		)
	}
}

func TestServerShutdownAllowsUnconfiguredServer(
	t *testing.T,
) {
	var server *Server

	if err := server.Shutdown(
		context.Background(),
	); err != nil {
		t.Fatalf(
			"Shutdown() returned an error: %v",
			err,
		)
	}
}

func TestServerCloseAllowsUnconfiguredServer(
	t *testing.T,
) {
	var server *Server

	if err := server.Close(); err != nil {
		t.Fatalf(
			"Close() returned an error: %v",
			err,
		)
	}
}
