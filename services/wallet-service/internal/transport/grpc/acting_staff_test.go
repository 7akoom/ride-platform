package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestStaffActor(t *testing.T) {
	staff := "11111111-1111-4111-8111-111111111111"
	other := "22222222-2222-4222-8222-222222222222"

	asStaff := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{IdentityID: staff, SessionID: "s"})
	asService := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{IdentityID: internalServicePrincipalID, SessionID: internalServicePrincipalID})

	if got, err := staffActor(asStaff, "", "x"); err != nil || got != staff {
		t.Fatalf("signed-in staff: %q %v", got, err)
	}

	if _, err := staffActor(asStaff, other, "x"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("staff naming someone else: %v", err)
	}

	if got, err := staffActor(asService, other, "x"); err != nil || got != other {
		t.Fatalf("service acting for staff: %q %v", got, err)
	}

	if _, err := staffActor(asService, "", "x"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("service acting for nobody: %v", err)
	}

	if _, err := staffActor(asService, "not-a-uuid", "x"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("service acting for garbage: %v", err)
	}

	if _, err := staffActor(context.Background(), "", "x"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("nobody: %v", err)
	}
}
