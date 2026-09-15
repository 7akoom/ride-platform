package grpc

import (
	"context"
	"testing"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func noopHandler(ctx context.Context, request any) (any, error) {
	return "ok", nil
}

func contextForIdentity(identityID string) context.Context {
	return contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: identityID,
			SessionID:  "session-1",
		},
	)
}

func callN(
	t *testing.T,
	interceptor googlegrpc.UnaryServerInterceptor,
	ctx context.Context,
	info *googlegrpc.UnaryServerInfo,
	n int,
) []error {
	t.Helper()

	errs := make([]error, n)

	for i := 0; i < n; i++ {
		_, err := interceptor(ctx, nil, info, noopHandler)
		errs[i] = err
	}

	return errs
}

func isResourceExhausted(err error) bool {
	return status.Code(err) == codes.ResourceExhausted
}

// --- NewRateLimitUnaryInterceptor ------------------------------------

func TestNewRateLimitUnaryInterceptor_PanicsOnInvalidArguments(t *testing.T) {
	cases := map[string]struct {
		rps   float64
		burst int
	}{
		"zero rps":    {0, 10},
		"negative rps": {-1, 10},
		"zero burst":  {10, 0},
		"negative burst": {10, -1},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("expected a panic for %q", name)
				}
			}()

			NewRateLimitUnaryInterceptor(tc.rps, tc.burst)
		})
	}
}

// --- Burst behavior -----------------------------------------------------

func TestRateLimitInterceptor_AllowsUpToBurstThenRejects(t *testing.T) {
	// An intentionally tiny refill rate so the bucket never refills
	// meaningfully during the test — only the initial burst is
	// available, making this deterministic regardless of test speed.
	interceptor := NewRateLimitUnaryInterceptor(0.001, 3)
	ctx := contextForIdentity("rider-1")
	info := &googlegrpc.UnaryServerInfo{FullMethod: "/ride.rider.v1.RiderService/GetRider"}

	errs := callN(t, interceptor, ctx, info, 4)

	for i := 0; i < 3; i++ {
		if errs[i] != nil {
			t.Fatalf("call %d: expected the burst to be allowed, got %v", i, errs[i])
		}
	}

	if !isResourceExhausted(errs[3]) {
		t.Fatalf("call 4: expected ResourceExhausted, got %v", errs[3])
	}
}

func TestRateLimitInterceptor_TracksSeparateBucketsPerPrincipal(t *testing.T) {
	interceptor := NewRateLimitUnaryInterceptor(0.001, 1)
	info := &googlegrpc.UnaryServerInfo{FullMethod: "/ride.rider.v1.RiderService/GetRider"}

	// Exhaust rider-1's single-token bucket.
	if _, err := interceptor(contextForIdentity("rider-1"), nil, info, noopHandler); err != nil {
		t.Fatalf("rider-1 first call: unexpected error %v", err)
	}
	if _, err := interceptor(contextForIdentity("rider-1"), nil, info, noopHandler); !isResourceExhausted(err) {
		t.Fatalf("rider-1 second call: expected ResourceExhausted, got %v", err)
	}

	// A different caller must not be affected by rider-1's exhausted bucket.
	if _, err := interceptor(contextForIdentity("rider-2"), nil, info, noopHandler); err != nil {
		t.Fatalf("rider-2 first call: expected a fresh bucket, got %v", err)
	}
}

// --- Exemptions -----------------------------------------------------------

func TestRateLimitInterceptor_ExemptsHealthCheck(t *testing.T) {
	interceptor := NewRateLimitUnaryInterceptor(0.001, 1)
	ctx := contextForIdentity("rider-1")
	healthInfo := &googlegrpc.UnaryServerInfo{FullMethod: healthv1.Health_Check_FullMethodName}

	// Exhaust the caller's bucket on an ordinary method first.
	ordinaryInfo := &googlegrpc.UnaryServerInfo{FullMethod: "/ride.rider.v1.RiderService/GetRider"}
	if _, err := interceptor(ctx, nil, ordinaryInfo, noopHandler); err != nil {
		t.Fatalf("unexpected error priming the bucket: %v", err)
	}

	// The health check must go through regardless of that exhausted bucket.
	for i := 0; i < 5; i++ {
		if _, err := interceptor(ctx, nil, healthInfo, noopHandler); err != nil {
			t.Fatalf("health check call %d: expected no error, got %v", i, err)
		}
	}
}

func TestRateLimitInterceptor_ExemptsInternalServiceCalls(t *testing.T) {
	interceptor := NewRateLimitUnaryInterceptor(0.001, 1)
	ctx := contextForIdentity(internalServicePrincipalID)
	info := &googlegrpc.UnaryServerInfo{FullMethod: "/ride.rider.v1.RiderService/GetRider"}

	errs := callN(t, interceptor, ctx, info, 20)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("internal-service call %d: expected no error, got %v", i, err)
		}
	}
}

func TestRateLimitInterceptor_ContextWithoutPrincipalIsTreatedAsInternal(t *testing.T) {
	interceptor := NewRateLimitUnaryInterceptor(0.001, 1)
	info := &googlegrpc.UnaryServerInfo{FullMethod: "/ride.rider.v1.RiderService/GetRider"}

	errs := callN(t, interceptor, context.Background(), info, 5)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d without a principal: expected no error, got %v", i, err)
		}
	}
}
