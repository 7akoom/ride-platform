package grpc

import (
	"context"
	"errors"
	"testing"

	supportv1 "github.com/7akoom/ride-platform/gen/go/ride/support/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/support-service/internal/application/support"
)

type recordingAuthorizer struct {
	allowed    bool
	err        error
	permission string
	target     string
	completed  []codes.Code
}

func (a *recordingAuthorizer) Authorize(_ context.Context, _, permission, _, target string) (bool, string, string, error) {
	a.permission = permission
	a.target = target

	return a.allowed, "staff-1", "audit-1", a.err
}

func (a *recordingAuthorizer) Complete(_ context.Context, _ string, code codes.Code) {
	a.completed = append(a.completed, code)
}

func call(t *testing.T, authorizer *recordingAuthorizer, identity, method string, request any) (support.Staff, error) {
	t.Helper()

	ctx := context.Background()
	if identity != "" {
		ctx = contextWithAuthenticatedPrincipal(ctx, authenticatedPrincipal{IdentityID: identity, SessionID: "s"})
	}

	var seen support.Staff

	_, err := NewAuthorizationUnaryInterceptor(authorizer)(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: method},
		func(ctx context.Context, _ any) (any, error) {
			seen, _ = staffFromContext(ctx)

			return "ok", nil
		})

	return seen, err
}

func TestAuthorization(t *testing.T) {
	user := "11111111-1111-4111-8111-111111111111"

	t.Run("no principal", func(t *testing.T) {
		_, err := call(t, &recordingAuthorizer{}, "", supportRPCPrefix+"CreateTicket", nil)
		if status.Code(err) != codes.Unauthenticated {
			t.Fatal(err)
		}
	})

	t.Run("the internal token has no methods here", func(t *testing.T) {
		_, err := call(t, &recordingAuthorizer{allowed: true}, internalServicePrincipalID, supportRPCPrefix+"ListMyTickets", nil)
		if status.Code(err) != codes.PermissionDenied {
			t.Fatal(err)
		}
	})

	t.Run("an unknown method is denied", func(t *testing.T) {
		_, err := call(t, &recordingAuthorizer{allowed: true}, user, supportRPCPrefix+"Nope", nil)
		if status.Code(err) != codes.PermissionDenied {
			t.Fatal(err)
		}
	})

	t.Run("a person's method runs without staff-service", func(t *testing.T) {
		a := &recordingAuthorizer{}
		if _, err := call(t, a, user, supportRPCPrefix+"CreateTicket", nil); err != nil || a.permission != "" {
			t.Fatal(err, a.permission)
		}
	})

	t.Run("a staff method needs its permission", func(t *testing.T) {
		a := &recordingAuthorizer{allowed: true}

		staff, err := call(t, a, user, supportRPCPrefix+"ClaimTicket", &supportv1.ClaimTicketRequest{TicketId: "t-1"})
		if err != nil || a.permission != support.PermissionReply || a.target != "t-1" ||
			staff.StaffID != "staff-1" || staff.IdentityID != user || len(a.completed) != 1 || a.completed[0] != codes.OK {
			t.Fatalf("%v %+v %+v", err, a, staff)
		}
	})

	t.Run("denied", func(t *testing.T) {
		_, err := call(t, &recordingAuthorizer{}, user, supportRPCPrefix+"ListSupportQueue", &supportv1.ListSupportQueueRequest{})
		if status.Code(err) != codes.PermissionDenied {
			t.Fatal(err)
		}
	})

	t.Run("staff-service down fails closed", func(t *testing.T) {
		_, err := call(t, &recordingAuthorizer{allowed: true, err: errors.New("down")}, user, supportRPCPrefix+"ListSupportQueue", &supportv1.ListSupportQueueRequest{})
		if status.Code(err) != codes.Unavailable {
			t.Fatal(err)
		}
	})

	t.Run("an action needs the permission of its kind", func(t *testing.T) {
		cases := map[supportv1.ActionKind]string{
			supportv1.ActionKind_ACTION_KIND_REFUND:             support.PermissionRefund,
			supportv1.ActionKind_ACTION_KIND_WAIVE_FEE:          support.PermissionRefund,
			supportv1.ActionKind_ACTION_KIND_COMPENSATION:       support.PermissionRefund,
			supportv1.ActionKind_ACTION_KIND_SUSPEND_ACCOUNT:    support.PermissionSuspend,
			supportv1.ActionKind_ACTION_KIND_REACTIVATE_ACCOUNT: support.PermissionSuspend,
		}

		for kind, want := range cases {
			a := &recordingAuthorizer{allowed: true}
			if _, err := call(t, a, user, supportRPCPrefix+"RequestTicketAction", &supportv1.RequestTicketActionRequest{Kind: kind}); err != nil || a.permission != want {
				t.Fatalf("%s: %v %s", kind, err, a.permission)
			}
		}

		_, err := call(t, &recordingAuthorizer{allowed: true}, user, supportRPCPrefix+"RequestTicketAction", &supportv1.RequestTicketActionRequest{})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatal(err)
		}
	})

	t.Run("every RPC is classified", func(t *testing.T) {
		for _, m := range supportv1.SupportService_ServiceDesc.Methods {
			full := supportRPCPrefix + m.MethodName
			_, user := userMethods[full]
			_, staff := staffMethods[full]

			if user == staff {
				t.Errorf("%s: user %v, staff %v", m.MethodName, user, staff)
			}
		}
	})
}
