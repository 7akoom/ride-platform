package grpc

import (
	"context"
	"io"
	"log/slog"
	"testing"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type fakeLifecycle struct {
	calls []string
}

func (f *fakeLifecycle) SuspendIdentity(_ context.Context, in auth.IdentityLifecycleInput) (auth.IdentityLifecycleResult, error) {
	f.calls = append(f.calls, "suspend:"+in.IdentityID)

	return auth.IdentityLifecycleResult{PreviousStatus: auth.IdentityStatusActive, CurrentStatus: auth.IdentityStatusSuspended, Changed: true}, nil
}

func (f *fakeLifecycle) DisableIdentity(context.Context, auth.IdentityLifecycleInput) (auth.IdentityLifecycleResult, error) {
	return auth.IdentityLifecycleResult{}, nil
}

func (f *fakeLifecycle) ReactivateIdentity(_ context.Context, in auth.IdentityLifecycleInput) (auth.IdentityLifecycleResult, error) {
	f.calls = append(f.calls, "reactivate:"+in.IdentityID)

	return auth.IdentityLifecycleResult{}, auth.ErrIdentityNotFound
}

func TestAccountStatusHandler(t *testing.T) {
	lifecycle := &fakeLifecycle{}
	handler := NewAccountStatusHandler(lifecycle, slog.New(slog.NewTextHandler(io.Discard, nil)))
	id := "11111111-1111-4111-8111-111111111111"
	internal := context.WithValue(context.Background(), internalCallContextKey{}, true)

	if _, err := handler.SuspendIdentity(context.Background(), &identityv1.SuspendIdentityRequest{IdentityId: id}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("without the internal mark: %v", err)
	}

	if _, err := handler.SuspendIdentity(internal, &identityv1.SuspendIdentityRequest{IdentityId: "nope"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("bad id: %v", err)
	}

	response, err := handler.SuspendIdentity(internal, &identityv1.SuspendIdentityRequest{IdentityId: id})
	if err != nil || response.GetCurrentStatus() != "suspended" || !response.GetChanged() {
		t.Fatalf("suspend: %+v %v", response, err)
	}

	if _, err := handler.ReactivateIdentity(internal, &identityv1.ReactivateIdentityRequest{IdentityId: id}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown identity: %v", err)
	}

	if len(lifecycle.calls) != 2 {
		t.Fatalf("calls: %v", lifecycle.calls)
	}

	if !isInternalMethod(identityv1.IdentityAccountService_SuspendIdentity_FullMethodName) ||
		!isInternalMethod(identityv1.IdentityAccountService_ReactivateIdentity_FullMethodName) {
		t.Fatal("the account methods are not internal")
	}
}
