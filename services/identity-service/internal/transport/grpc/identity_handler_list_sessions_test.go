package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestIdentityHandlerListMySessionsReturnsAuthenticatedIdentitySessions(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.August,
		26,
		17,
		0,
		0,
		0,
		time.UTC,
	)

	clientID := "mobile-app"
	deviceID := "device-123"
	deviceName := "iPhone"
	platform := "ios"
	appVersion := "1.0.0"
	ipAddress := "192.0.2.10"
	userAgent := "ride-app/1.0.0"
	lastSeenAt := now.Add(-2 * time.Minute)

	authService := &fakeAuthService{
		listMySessionsResult: auth.ListMySessionsResult{
			Sessions: []auth.SessionInfo{
				{
					SessionID:  "22222222-2222-2222-2222-222222222222",
					ClientID:   &clientID,
					DeviceID:   &deviceID,
					DeviceName: &deviceName,
					Platform:   &platform,
					AppVersion: &appVersion,
					IPAddress:  &ipAddress,
					UserAgent:  &userAgent,
					ExpiresAt:  now.Add(time.Hour),
					LastSeenAt: &lastSeenAt,
					CreatedAt:  now.Add(-10 * time.Minute),
					IsCurrent:  true,
				},
				{
					SessionID: "33333333-3333-3333-3333-333333333333",
					ExpiresAt: now.Add(2 * time.Hour),
					CreatedAt: now.Add(-20 * time.Minute),
					IsCurrent: false,
				},
			},
		},
	}

	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "11111111-1111-1111-1111-111111111111",
			SessionID:  "22222222-2222-2222-2222-222222222222",
		},
	)

	response, err := handler.ListMySessions(
		ctx,
		&identityv1.ListMySessionsRequest{},
	)
	if err != nil {
		t.Fatalf(
			"ListMySessions() returned an error: %v",
			err,
		)
	}

	if !authService.listMySessionsCalled {
		t.Fatal(
			"ListMySessions() did not call auth service",
		)
	}

	if authService.listMySessionsInput.IdentityID !=
		"11111111-1111-1111-1111-111111111111" {
		t.Fatalf(
			"auth service identity ID = %q",
			authService.listMySessionsInput.IdentityID,
		)
	}

	if authService.listMySessionsInput.CurrentSessionID !=
		"22222222-2222-2222-2222-222222222222" {
		t.Fatalf(
			"auth service current session ID = %q",
			authService.listMySessionsInput.CurrentSessionID,
		)
	}

	if len(response.GetSessions()) != 2 {
		t.Fatalf(
			"sessions count = %d, want 2",
			len(response.GetSessions()),
		)
	}

	current := response.GetSessions()[0]

	if current.GetSessionId() !=
		"22222222-2222-2222-2222-222222222222" {
		t.Fatalf(
			"current session ID = %q",
			current.GetSessionId(),
		)
	}

	if !current.GetIsCurrent() {
		t.Fatal(
			"current session is_current = false, want true",
		)
	}

	if current.GetClientId() != clientID {
		t.Fatalf(
			"current session client ID = %q, want %q",
			current.GetClientId(),
			clientID,
		)
	}

	if current.GetDeviceId() != deviceID {
		t.Fatalf(
			"current session device ID = %q, want %q",
			current.GetDeviceId(),
			deviceID,
		)
	}

	if current.GetIpAddress() != ipAddress {
		t.Fatalf(
			"current session IP address = %q, want %q",
			current.GetIpAddress(),
			ipAddress,
		)
	}

	if !current.GetExpiresAt().AsTime().Equal(
		now.Add(time.Hour),
	) {
		t.Fatalf(
			"current session expires_at = %v",
			current.GetExpiresAt().AsTime(),
		)
	}

	if !current.GetLastSeenAt().AsTime().Equal(lastSeenAt) {
		t.Fatalf(
			"current session last_seen_at = %v",
			current.GetLastSeenAt().AsTime(),
		)
	}

	if !current.GetCreatedAt().AsTime().Equal(
		now.Add(-10 * time.Minute),
	) {
		t.Fatalf(
			"current session created_at = %v",
			current.GetCreatedAt().AsTime(),
		)
	}

	if response.GetSessions()[1].GetIsCurrent() {
		t.Fatal(
			"non-current session is_current = true, want false",
		)
	}
}

func TestIdentityHandlerListMySessionsRejectsMissingAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}
	handler := NewIdentityHandler(authService)

	response, err := handler.ListMySessions(
		context.Background(),
		&identityv1.ListMySessionsRequest{},
	)

	if response != nil {
		t.Fatal(
			"ListMySessions() returned a response, want nil",
		)
	}

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"ListMySessions() code = %v, want %v",
			status.Code(err),
			codes.Unauthenticated,
		)
	}

	if authService.listMySessionsCalled {
		t.Fatal(
			"ListMySessions() called auth service without authenticated identity",
		)
	}
}
func TestIdentityHandlerListMySessionsRejectsNilRequest(
	t *testing.T,
) {
	authService := &fakeAuthService{}
	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "11111111-1111-1111-1111-111111111111",
			SessionID:  "22222222-2222-2222-2222-222222222222",
		},
	)

	response, err := handler.ListMySessions(
		ctx,
		nil,
	)

	if response != nil {
		t.Fatal(
			"ListMySessions() returned a response, want nil",
		)
	}

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf(
			"ListMySessions() code = %v, want %v",
			status.Code(err),
			codes.InvalidArgument,
		)
	}

	if authService.listMySessionsCalled {
		t.Fatal(
			"ListMySessions() called auth service for nil request",
		)
	}
}
func TestIdentityHandlerListMySessionsMapsUnexpectedErrorToInternal(
	t *testing.T,
) {
	authService := &fakeAuthService{
		listMySessionsErr: errors.New(
			"session listing failed",
		),
	}

	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "11111111-1111-1111-1111-111111111111",
			SessionID:  "22222222-2222-2222-2222-222222222222",
		},
	)

	response, err := handler.ListMySessions(
		ctx,
		&identityv1.ListMySessionsRequest{},
	)

	if response != nil {
		t.Fatal(
			"ListMySessions() returned a response, want nil",
		)
	}

	if status.Code(err) != codes.Internal {
		t.Fatalf(
			"ListMySessions() code = %v, want %v",
			status.Code(err),
			codes.Internal,
		)
	}

	if !authService.listMySessionsCalled {
		t.Fatal(
			"ListMySessions() did not call auth service",
		)
	}
}
