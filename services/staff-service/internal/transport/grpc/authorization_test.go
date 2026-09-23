package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	staffv1 "github.com/7akoom/ride-platform/gen/go/ride/staff/v1"
	"github.com/7akoom/ride-platform/services/staff-service/internal/application/staff"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestEveryRPCIsClassified(t *testing.T) {
	desc := staffv1.StaffService_ServiceDesc
	prefix := "/" + desc.ServiceName + "/"

	registered := map[string]struct{}{}

	for _, method := range desc.Methods {
		full := prefix + method.MethodName
		registered[full] = struct{}{}

		level, classified := methodAccess[full]
		if !classified {
			t.Errorf("%s has no access level in methodAccess", full)

			continue
		}

		permission, hasPermission := staffPermissions[full]

		if level == accessStaff && !hasPermission {
			t.Errorf("%s is accessStaff but names no permission", full)
		}

		if level != accessStaff && hasPermission {
			t.Errorf("%s names a permission but is not accessStaff", full)
		}

		if hasPermission && !staff.IsKnownPermission(permission) {
			t.Errorf("%s needs %q, which is not in the permission catalog", full, permission)
		}
	}

	for full := range methodAccess {
		if _, ok := registered[full]; !ok {
			t.Errorf("methodAccess lists %s, which is not an RPC of %s", full, desc.ServiceName)
		}
	}
}

type fakeAuthorizer struct {
	mu        sync.Mutex
	allowed   bool
	err       error
	requests  []staff.AuthorizeInput
	completed map[string]string
}

func (f *fakeAuthorizer) Authorize(_ context.Context, input staff.AuthorizeInput) (staff.Authorization, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.requests = append(f.requests, input)

	if f.err != nil {
		return staff.Authorization{}, f.err
	}

	result := staff.Authorization{Allowed: f.allowed, AuditEntryID: "audit-1"}
	if f.allowed {
		result.Actor = staff.Actor{Member: staff.Member{ID: "staff-1"}}
	}

	return result, nil
}

func (f *fakeAuthorizer) CompleteAction(_ context.Context, id string, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.completed == nil {
		f.completed = map[string]string{}
	}

	f.completed[id] = code

	return nil
}

func callAs(identityID string) context.Context {
	return contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{
		IdentityID: identityID,
		SessionID:  "session-1",
	})
}

func run(
	t *testing.T,
	authorizer *fakeAuthorizer,
	ctx context.Context,
	method string,
	request any,
	handlerErr error,
) (bool, staff.Actor, error) {
	t.Helper()

	interceptor := NewAuthorizationUnaryInterceptor(authorizer, slog.New(slog.NewTextHandler(io.Discard, nil)))

	var ran bool
	var actor staff.Actor

	_, err := interceptor(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: method}, func(ctx context.Context, _ any) (any, error) {
		ran = true
		actor = actorFromContext(ctx)

		return nil, handlerErr
	})

	return ran, actor, err
}

func TestStaffMethodsAskForThePermissionAndRecordTheOutcome(t *testing.T) {
	authorizer := &fakeAuthorizer{allowed: true}

	ran, actor, err := run(t, authorizer, callAs("identity-1"), staffServicePrefix+"SuspendStaffMember",
		&staffv1.SuspendStaffMemberRequest{StaffId: "target-1"}, status.Error(codes.FailedPrecondition, "no"))

	if !ran || actor.Member.ID != "staff-1" {
		t.Fatalf("handler ran=%v actor=%+v", ran, actor)
	}

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("the handler's error must reach the caller, got %v", err)
	}

	request := authorizer.requests[0]
	if request.IdentityID != "identity-1" || request.Permission != staff.PermissionStaffManage ||
		request.TargetID != "target-1" || !strings.HasSuffix(request.Method, "/SuspendStaffMember") {
		t.Fatalf("unexpected authorization request %+v", request)
	}

	if authorizer.completed["audit-1"] != "FailedPrecondition" {
		t.Fatalf("outcome = %q", authorizer.completed["audit-1"])
	}
}

func TestStaffMethodsAreDeniedWithoutThePermission(t *testing.T) {
	authorizer := &fakeAuthorizer{allowed: false}

	ran, _, err := run(t, authorizer, callAs("identity-1"), staffServicePrefix+"ListAuditEntries", &staffv1.ListAuditEntriesRequest{}, nil)
	if ran || status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ran=%v err=%v", ran, err)
	}

	if len(authorizer.completed) != 0 {
		t.Fatal("a denied action has no outcome to record")
	}
}

func TestStaffMethodsFailClosedWhenTheDecisionCannotBeMade(t *testing.T) {
	authorizer := &fakeAuthorizer{err: errors.New("database down")}

	ran, _, err := run(t, authorizer, callAs("identity-1"), staffServicePrefix+"ListRoles", &staffv1.ListRolesRequest{}, nil)
	if ran || status.Code(err) != codes.Unavailable {
		t.Fatalf("ran=%v err=%v", ran, err)
	}
}

func TestInternalMethodsRefuseUserTokens(t *testing.T) {
	authorizer := &fakeAuthorizer{allowed: true}

	for _, method := range []string{"AuthorizeStaffAction", "CompleteStaffAction"} {
		ran, _, err := run(t, authorizer, callAs("identity-1"), staffServicePrefix+method, nil, nil)
		if ran || status.Code(err) != codes.PermissionDenied {
			t.Errorf("%s: ran=%v err=%v", method, ran, err)
		}

		ran, _, err = run(t, authorizer, callAs(internalServicePrincipalID), staffServicePrefix+method, nil, nil)
		if !ran || err != nil {
			t.Errorf("%s with the internal token: ran=%v err=%v", method, ran, err)
		}
	}

	if len(authorizer.requests) != 0 {
		t.Fatal("internal methods must not go through staff authorization")
	}
}

func TestSelfServiceMethodsNeedOnlyASignedInCaller(t *testing.T) {
	authorizer := &fakeAuthorizer{}

	ran, _, err := run(t, authorizer, callAs("identity-1"), staffServicePrefix+"GetMyStaffProfile", nil, nil)
	if !ran || err != nil {
		t.Fatalf("ran=%v err=%v", ran, err)
	}

	ran, _, err = run(t, authorizer, context.Background(), staffServicePrefix+"GetMyStaffProfile", nil, nil)
	if ran || status.Code(err) != codes.Unauthenticated {
		t.Fatalf("no principal: ran=%v err=%v", ran, err)
	}
}

func TestUnknownMethodsAreDenied(t *testing.T) {
	ran, _, err := run(t, &fakeAuthorizer{allowed: true}, callAs("identity-1"), "/ride.staff.v1.StaffService/Nope", nil, nil)
	if ran || status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ran=%v err=%v", ran, err)
	}
}
