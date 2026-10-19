package observability

import (
	"context"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// startHealth serves the gRPC health service on address ("127.0.0.1:0" for any
// port) and returns the address it listens on.
func startHealth(t *testing.T, address string) (string, *grpc.Server) {
	t.Helper()

	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	server := grpc.NewServer()
	healthpb.RegisterHealthServer(server, health.NewServer())

	go func() { _ = server.Serve(listener) }()

	return listener.Addr().String(), server
}

func check(conn *grpc.ClientConn) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})

	return err
}

// A service that goes away for longer than the old backoff and comes back is
// reached again within a few seconds, through a name (not an IP address).
func TestBackendIsReachedAgainSoonAfterItReturns(t *testing.T) {
	address, server := startHealth(t, "127.0.0.1:0")
	_, port, _ := net.SplitHostPort(address)

	conn, err := grpc.NewClient(
		net.JoinHostPort("localhost", port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		LookUpOnEveryConnect(),
		QuickReconnect(),
	)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer conn.Close()

	if err := check(conn); err != nil {
		t.Fatalf("first call: %v", err)
	}

	server.Stop()

	// Gone long enough for the default backoff to have grown past five seconds.
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		_ = check(conn)
		time.Sleep(200 * time.Millisecond)
	}

	_, server = startHealth(t, address)
	defer server.Stop()

	back := time.Now()
	for check(conn) != nil {
		if time.Since(back) > 8*time.Second {
			t.Fatal("not reached again within 8 seconds of coming back")
		}

		time.Sleep(100 * time.Millisecond)
	}
}
