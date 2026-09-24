package grpc

import (
	"context"
	"testing"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const testInternalToken = "a-long-enough-internal-service-token-for-tests"

func callThrough(t *testing.T, method, authorization string) (bool, error) {
	t.Helper()

	ctx := context.Background()
	if authorization != "" {
		ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", authorization))
	}

	internal := false

	_, err := NewInternalServiceUnaryInterceptor(testInternalToken)(ctx, nil,
		&googlegrpc.UnaryServerInfo{FullMethod: method},
		func(ctx context.Context, _ any) (any, error) {
			internal = requireInternalCall(ctx) == nil

			return nil, nil
		})

	return internal, err
}

func TestInternalMethodsTakeOnlyTheInternalToken(t *testing.T) {
	methods := []string{
		identityv1.WalletPinService_VerifyWalletPin_FullMethodName,
		identityv1.IdentityDirectoryService_FindIdentityByPhone_FullMethodName,
		identityv1.IdentityDirectoryService_GetIdentityPhone_FullMethodName,
	}

	for _, method := range methods {
		if requiresAuthentication(method) {
			t.Fatalf("%s must not ask for an access token", method)
		}

		if internal, err := callThrough(t, method, "Bearer "+testInternalToken); err != nil || !internal {
			t.Fatalf("%s with the token: %v %v", method, internal, err)
		}

		if _, err := callThrough(t, method, "Bearer a-person's-access-token"); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("%s with another token: %v", method, err)
		}

		if _, err := callThrough(t, method, ""); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("%s without a token: %v", method, err)
		}
	}
}

func TestOtherMethodsAreNotInternal(t *testing.T) {
	for _, method := range []string{
		identityv1.WalletPinService_SetWalletPin_FullMethodName,
		identityv1.WalletPinService_GetWalletPinStatus_FullMethodName,
		identityv1.IdentityService_GetMyIdentity_FullMethodName,
	} {
		if !requiresAuthentication(method) {
			t.Fatalf("%s must ask for an access token", method)
		}

		// The internal token does not make them internal calls.
		if internal, err := callThrough(t, method, "Bearer "+testInternalToken); err != nil || internal {
			t.Fatalf("%s: %v %v", method, internal, err)
		}
	}
}

func TestAnInternalHandlerRefusesACallThatSkippedTheInterceptor(t *testing.T) {
	if err := requireInternalCall(context.Background()); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("got %v", err)
	}

	handler := &WalletPinHandler{}
	if _, err := handler.VerifyWalletPin(context.Background(), &identityv1.VerifyWalletPinRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("verify: %v", err)
	}

	if _, err := handler.Directory().FindIdentityByPhone(context.Background(), &identityv1.FindIdentityByPhoneRequest{}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("find: %v", err)
	}
}
