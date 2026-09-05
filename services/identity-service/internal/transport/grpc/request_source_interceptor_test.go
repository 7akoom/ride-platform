package grpc

import (
	"context"
	"net"
	"testing"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

func TestRequestSourceUnaryInterceptorUsesTransportPeerIPAddress(
	t *testing.T,
) {
	interceptor := NewRequestSourceUnaryInterceptor()

	ctx := peer.NewContext(
		context.Background(),
		&peer.Peer{
			Addr: &net.TCPAddr{
				IP:   net.ParseIP("203.0.113.25"),
				Port: 50000,
			},
		},
	)

	handlerCalled := false

	_, err := interceptor(
		ctx,
		nil,
		&googlegrpc.UnaryServerInfo{
			FullMethod: "/test.Service/Test",
		},
		func(
			handlerCtx context.Context,
			request any,
		) (any, error) {
			handlerCalled = true

			source, ok := requestSourceFromContext(
				handlerCtx,
			)
			if !ok {
				t.Fatal(
					"request source was not added to context",
				)
			}

			if source.IPAddress != "203.0.113.25" {
				t.Fatalf(
					"source IP address = %q, want %q",
					source.IPAddress,
					"203.0.113.25",
				)
			}

			return nil, nil
		},
	)
	if err != nil {
		t.Fatalf(
			"interceptor returned error: %v",
			err,
		)
	}

	if !handlerCalled {
		t.Fatal("handler was not called")
	}
}

func TestRequestSourceUnaryInterceptorIgnoresSpoofedForwardedMetadata(
	t *testing.T,
) {
	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs(
			"x-forwarded-for",
			"198.51.100.200",
			"x-real-ip",
			"198.51.100.201",
		),
	)

	ctx = peer.NewContext(
		ctx,
		&peer.Peer{
			Addr: &net.TCPAddr{
				IP:   net.ParseIP("203.0.113.30"),
				Port: 50001,
			},
		},
	)

	interceptor := NewRequestSourceUnaryInterceptor()

	_, err := interceptor(
		ctx,
		nil,
		&googlegrpc.UnaryServerInfo{
			FullMethod: "/test.Service/Test",
		},
		func(
			handlerCtx context.Context,
			request any,
		) (any, error) {
			source, ok := requestSourceFromContext(
				handlerCtx,
			)
			if !ok {
				t.Fatal(
					"request source was not added to context",
				)
			}

			if source.IPAddress != "203.0.113.30" {
				t.Fatalf(
					"source IP address = %q, want transport peer IP %q",
					source.IPAddress,
					"203.0.113.30",
				)
			}

			return nil, nil
		},
	)
	if err != nil {
		t.Fatalf(
			"interceptor returned error: %v",
			err,
		)
	}
}

func TestRequestSourceUnaryInterceptorContinuesWithoutIPAddress(
	t *testing.T,
) {
	interceptor := NewRequestSourceUnaryInterceptor()

	handlerCalled := false

	_, err := interceptor(
		context.Background(),
		nil,
		&googlegrpc.UnaryServerInfo{
			FullMethod: "/test.Service/Test",
		},
		func(
			handlerCtx context.Context,
			request any,
		) (any, error) {
			handlerCalled = true

			if _, ok := requestSourceFromContext(
				handlerCtx,
			); ok {
				t.Fatal(
					"request source unexpectedly exists",
				)
			}

			return nil, nil
		},
	)
	if err != nil {
		t.Fatalf(
			"interceptor returned error: %v",
			err,
		)
	}

	if !handlerCalled {
		t.Fatal("handler was not called")
	}
}
