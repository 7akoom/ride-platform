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

type fakeAuthService struct {
	requestOTPResult                 auth.RequestOTPResult
	requestOTPErr                    error
	requestOTPInput                  auth.RequestOTPInput
	requestOTPCalled                 bool
	requestIdentifierUnlinkOTPResult auth.RequestIdentifierUnlinkOTPResult
	requestIdentifierUnlinkOTPErr    error
	requestIdentifierUnlinkOTPInput  auth.RequestIdentifierUnlinkOTPInput
	requestIdentifierUnlinkOTPCalled bool

	verifyOTPResult auth.VerifyOTPResult
	verifyOTPErr    error
	verifyOTPInput  auth.VerifyOTPInput
	verifyOTPCalled bool

	getMyIdentityResult auth.IdentityDetails
	getMyIdentityErr    error
	getMyIdentityInput  auth.GetMyIdentityInput
	getMyIdentityCalled bool

	refreshTokenResult auth.RefreshTokenResult
	refreshTokenErr    error
	refreshTokenInput  auth.RefreshTokenInput
	refreshTokenCalled bool

	logoutErr    error
	logoutInput  auth.LogoutInput
	logoutCalled bool

	logoutAllSessionsErr    error
	logoutAllSessionsInput  auth.LogoutAllSessionsInput
	logoutAllSessionsCalled bool
}

func (f *fakeAuthService) RequestOTP(
	ctx context.Context,
	input auth.RequestOTPInput,
) (auth.RequestOTPResult, error) {
	f.requestOTPCalled = true
	f.requestOTPInput = input

	if f.requestOTPErr != nil {
		return auth.RequestOTPResult{}, f.requestOTPErr
	}

	return f.requestOTPResult, nil
}

func (f *fakeAuthService) RequestIdentifierUnlinkOTP(
	ctx context.Context,
	input auth.RequestIdentifierUnlinkOTPInput,
) (auth.RequestIdentifierUnlinkOTPResult, error) {
	f.requestIdentifierUnlinkOTPCalled = true
	f.requestIdentifierUnlinkOTPInput = input

	if f.requestIdentifierUnlinkOTPErr != nil {
		return auth.RequestIdentifierUnlinkOTPResult{},
			f.requestIdentifierUnlinkOTPErr
	}

	return f.requestIdentifierUnlinkOTPResult, nil
}

func (f *fakeAuthService) VerifyOTP(
	ctx context.Context,
	input auth.VerifyOTPInput,
) (auth.VerifyOTPResult, error) {
	f.verifyOTPCalled = true
	f.verifyOTPInput = input

	if f.verifyOTPErr != nil {
		return auth.VerifyOTPResult{}, f.verifyOTPErr
	}

	return f.verifyOTPResult, nil
}

func (f *fakeAuthService) GetMyIdentity(
	ctx context.Context,
	input auth.GetMyIdentityInput,
) (auth.IdentityDetails, error) {
	f.getMyIdentityCalled = true
	f.getMyIdentityInput = input

	if f.getMyIdentityErr != nil {
		return auth.IdentityDetails{}, f.getMyIdentityErr
	}

	return f.getMyIdentityResult, nil
}

func (f *fakeAuthService) RefreshToken(
	ctx context.Context,
	input auth.RefreshTokenInput,
) (auth.RefreshTokenResult, error) {
	f.refreshTokenCalled = true
	f.refreshTokenInput = input

	if f.refreshTokenErr != nil {
		return auth.RefreshTokenResult{}, f.refreshTokenErr
	}

	return f.refreshTokenResult, nil
}

func (f *fakeAuthService) Logout(
	ctx context.Context,
	input auth.LogoutInput,
) error {
	f.logoutCalled = true
	f.logoutInput = input

	return f.logoutErr
}

func (f *fakeAuthService) LogoutAllSessions(
	ctx context.Context,
	input auth.LogoutAllSessionsInput,
) error {
	f.logoutAllSessionsCalled = true
	f.logoutAllSessionsInput = input

	return f.logoutAllSessionsErr
}

func TestIdentityHandlerRequestLoginOTPReturnsChallenge(t *testing.T) {
	authService := &fakeAuthService{
		requestOTPResult: auth.RequestOTPResult{
			ChallengeID:      "otp_ch_test",
			ExpiresInSeconds: 300,
		},
	}

	handler := NewIdentityHandler(authService)

	response, err := handler.RequestLoginOTP(
		context.Background(),
		&identityv1.RequestLoginOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE,
				Value: "  +9647500000000  ",
			},
		},
	)
	if err != nil {
		t.Fatalf("RequestLoginOTP() returned an error: %v", err)
	}

	if !authService.requestOTPCalled {
		t.Fatal("auth service RequestOTP() was not called")
	}

	expectedIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000000",
	}

	if authService.requestOTPInput.Identifier != expectedIdentifier {
		t.Fatalf(
			"auth service received identifier %+v, expected %+v",
			authService.requestOTPInput.Identifier,
			expectedIdentifier,
		)
	}

	if authService.requestOTPInput.Purpose != auth.OTPPurposeLogin {
		t.Fatalf(
			"auth service received purpose %q, expected %q",
			authService.requestOTPInput.Purpose,
			auth.OTPPurposeLogin,
		)
	}

	if response.GetChallengeId() != "otp_ch_test" {
		t.Fatalf(
			"ChallengeId is %q, expected %q",
			response.GetChallengeId(),
			"otp_ch_test",
		)
	}

	if response.GetExpiresInSeconds() != 300 {
		t.Fatalf(
			"ExpiresInSeconds is %d, expected 300",
			response.GetExpiresInSeconds(),
		)
	}
}

func TestIdentityHandlerRequestLoginOTPRejectsNilRequest(t *testing.T) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestLoginOTP(
		context.Background(),
		nil,
	)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.InvalidArgument,
		)
	}

	if authService.requestOTPCalled {
		t.Fatal(
			"auth service RequestOTP() was called for nil request",
		)
	}
}

func TestIdentityHandlerRequestLoginOTPRejectsEmptyIdentifierValue(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestLoginOTP(
		context.Background(),
		&identityv1.RequestLoginOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE,
				Value: "   ",
			},
		},
	)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.InvalidArgument,
		)
	}

	if authService.requestOTPCalled {
		t.Fatal(
			"auth service RequestOTP() was called for empty identifier value",
		)
	}
}

func TestIdentityHandlerRequestIdentifierLinkOTPUsesAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{
		requestOTPResult: auth.RequestOTPResult{
			ChallengeID:      "otp_ch_link_test",
			ExpiresInSeconds: 300,
		},
	}

	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "identity-123",
			SessionID:  "session-456",
		},
	)

	response, err := handler.RequestIdentifierLinkOTP(
		ctx,
		&identityv1.RequestIdentifierLinkOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL,
				Value: "  user@example.com  ",
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"RequestIdentifierLinkOTP() returned an error: %v",
			err,
		)
	}

	if !authService.requestOTPCalled {
		t.Fatal(
			"auth service RequestOTP() was not called",
		)
	}

	expectedIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "user@example.com",
	}

	if authService.requestOTPInput.Identifier != expectedIdentifier {
		t.Fatalf(
			"auth service received identifier %+v, expected %+v",
			authService.requestOTPInput.Identifier,
			expectedIdentifier,
		)
	}

	if authService.requestOTPInput.Purpose !=
		auth.OTPPurposeLinkIdentifier {
		t.Fatalf(
			"auth service received purpose %q, expected %q",
			authService.requestOTPInput.Purpose,
			auth.OTPPurposeLinkIdentifier,
		)
	}

	if authService.requestOTPInput.TargetIdentityID == nil {
		t.Fatal(
			"auth service received nil target identity ID",
		)
	}

	if *authService.requestOTPInput.TargetIdentityID !=
		"identity-123" {
		t.Fatalf(
			"target identity ID = %q, expected %q",
			*authService.requestOTPInput.TargetIdentityID,
			"identity-123",
		)
	}

	if response.GetChallengeId() != "otp_ch_link_test" {
		t.Fatalf(
			"ChallengeId is %q, expected %q",
			response.GetChallengeId(),
			"otp_ch_link_test",
		)
	}

	if response.GetExpiresInSeconds() != 300 {
		t.Fatalf(
			"ExpiresInSeconds is %d, expected 300",
			response.GetExpiresInSeconds(),
		)
	}
}

func TestIdentityHandlerRequestIdentifierLinkOTPRejectsMissingAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestIdentifierLinkOTP(
		context.Background(),
		&identityv1.RequestIdentifierLinkOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL,
				Value: "user@example.com",
			},
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.Unauthenticated,
		)
	}

	if authService.requestOTPCalled {
		t.Fatal(
			"auth service RequestOTP() was called without authenticated identity",
		)
	}
}

func TestIdentityHandlerVerifyIdentifierLinkOTPUsesAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "identity-123",
			SessionID:  "session-456",
		},
	)

	response, err := handler.VerifyIdentifierLinkOTP(
		ctx,
		&identityv1.VerifyIdentifierLinkOTPRequest{
			ChallengeId: "  otp_ch_link_test  ",
			Code:        "  123456  ",
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyIdentifierLinkOTP() returned an error: %v",
			err,
		)
	}

	if !authService.verifyOTPCalled {
		t.Fatal(
			"auth service VerifyOTP() was not called",
		)
	}

	if authService.verifyOTPInput.ChallengeID !=
		"otp_ch_link_test" {
		t.Fatalf(
			"challenge ID = %q, expected %q",
			authService.verifyOTPInput.ChallengeID,
			"otp_ch_link_test",
		)
	}

	if authService.verifyOTPInput.Code != "123456" {
		t.Fatalf(
			"OTP code = %q, expected %q",
			authService.verifyOTPInput.Code,
			"123456",
		)
	}

	if authService.verifyOTPInput.ExpectedPurpose !=
		auth.OTPPurposeLinkIdentifier {
		t.Fatalf(
			"expected purpose = %q, expected %q",
			authService.verifyOTPInput.ExpectedPurpose,
			auth.OTPPurposeLinkIdentifier,
		)
	}

	if authService.verifyOTPInput.ExpectedTargetIdentityID == nil {
		t.Fatal(
			"expected target identity ID is nil",
		)
	}

	if *authService.verifyOTPInput.ExpectedTargetIdentityID !=
		"identity-123" {
		t.Fatalf(
			"expected target identity ID = %q, expected %q",
			*authService.verifyOTPInput.ExpectedTargetIdentityID,
			"identity-123",
		)
	}

	if response == nil {
		t.Fatal(
			"VerifyIdentifierLinkOTP() returned nil response",
		)
	}
}

func TestIdentityHandlerVerifyIdentifierLinkOTPRejectsMissingAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.VerifyIdentifierLinkOTP(
		context.Background(),
		&identityv1.VerifyIdentifierLinkOTPRequest{
			ChallengeId: "otp_ch_link_test",
			Code:        "123456",
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.Unauthenticated,
		)
	}

	if authService.verifyOTPCalled {
		t.Fatal(
			"auth service VerifyOTP() was called without authenticated identity",
		)
	}
}

func TestIdentityHandlerVerifyIdentifierLinkOTPMapsTargetMismatchToFailedPrecondition(
	t *testing.T,
) {
	authService := &fakeAuthService{
		verifyOTPErr: auth.ErrOTPChallengeTargetMismatch,
	}

	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "identity-123",
			SessionID:  "session-456",
		},
	)

	_, err := handler.VerifyIdentifierLinkOTP(
		ctx,
		&identityv1.VerifyIdentifierLinkOTPRequest{
			ChallengeId: "otp_ch_link_test",
			Code:        "123456",
		},
	)

	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.FailedPrecondition,
		)
	}

	if status.Convert(err).Message() !=
		"OTP challenge is not valid for identifier linking" {
		t.Fatalf(
			"error message is %q, expected %q",
			status.Convert(err).Message(),
			"OTP challenge is not valid for identifier linking",
		)
	}
}

func TestIdentityHandlerRequestLoginOTPMapsInvalidPhoneNumberToInvalidArgument(
	t *testing.T,
) {
	authService := &fakeAuthService{
		requestOTPErr: auth.ErrInvalidPhoneNumber,
	}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestLoginOTP(
		context.Background(),
		&identityv1.RequestLoginOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE,
				Value: "07501234567",
			},
		},
	)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.InvalidArgument,
		)
	}

	if status.Convert(err).Message() != "invalid phone number" {
		t.Fatalf(
			"error message is %q, expected %q",
			status.Convert(err).Message(),
			"invalid phone number",
		)
	}
}

func TestIdentityHandlerRequestLoginOTPMapsServiceFailureToInternal(
	t *testing.T,
) {
	authService := &fakeAuthService{
		requestOTPErr: errors.New("database unavailable"),
	}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestLoginOTP(
		context.Background(),
		&identityv1.RequestLoginOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE,
				Value: "+9647500000000",
			},
		},
	)

	if status.Code(err) != codes.Internal {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.Internal,
		)
	}

	if status.Convert(err).Message() != "failed to request login OTP" {
		t.Fatalf(
			"error message is %q, expected %q",
			status.Convert(err).Message(),
			"failed to request login OTP",
		)
	}
}

func TestIdentityHandlerVerifyLoginOTPReturnsTokens(t *testing.T) {
	authService := &fakeAuthService{
		verifyOTPResult: auth.VerifyOTPResult{
			IdentityID:                  "identity-test-id",
			AccessToken:                 "access-token",
			RefreshToken:                "refresh-token",
			AccessTokenExpiresInSeconds: 900,
		},
	}

	handler := NewIdentityHandler(authService)

	response, err := handler.VerifyLoginOTP(
		context.Background(),
		&identityv1.VerifyLoginOTPRequest{
			ChallengeId: "  otp_ch_test  ",
			Code:        "  123456  ",
		},
	)
	if err != nil {
		t.Fatalf("VerifyLoginOTP() returned an error: %v", err)
	}

	if !authService.verifyOTPCalled {
		t.Fatal("auth service VerifyOTP() was not called")
	}

	if authService.verifyOTPInput.ChallengeID != "otp_ch_test" {
		t.Fatalf(
			"auth service received challenge ID %q, expected %q",
			authService.verifyOTPInput.ChallengeID,
			"otp_ch_test",
		)
	}

	if authService.verifyOTPInput.Code != "123456" {
		t.Fatalf(
			"auth service received OTP code %q, expected %q",
			authService.verifyOTPInput.Code,
			"123456",
		)
	}

	if authService.verifyOTPInput.ExpectedPurpose != auth.OTPPurposeLogin {
		t.Fatalf(
			"auth service received purpose %q, expected %q",
			authService.verifyOTPInput.ExpectedPurpose,
			auth.OTPPurposeLogin,
		)
	}

	if response.GetIdentityId() != "identity-test-id" {
		t.Fatalf(
			"IdentityId is %q, expected %q",
			response.GetIdentityId(),
			"identity-test-id",
		)
	}

	if response.GetAccessToken() != "access-token" {
		t.Fatal("unexpected access token")
	}

	if response.GetRefreshToken() != "refresh-token" {
		t.Fatal("unexpected refresh token")
	}

	if response.GetAccessTokenExpiresInSeconds() != 900 {
		t.Fatalf(
			"AccessTokenExpiresInSeconds is %d, expected 900",
			response.GetAccessTokenExpiresInSeconds(),
		)
	}
}

func TestIdentityHandlerVerifyLoginOTPRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		request *identityv1.VerifyLoginOTPRequest
	}{
		{
			name:    "nil request",
			request: nil,
		},
		{
			name: "empty challenge ID",
			request: &identityv1.VerifyLoginOTPRequest{
				ChallengeId: "   ",
				Code:        "123456",
			},
		},
		{
			name: "empty OTP code",
			request: &identityv1.VerifyLoginOTPRequest{
				ChallengeId: "otp_ch_test",
				Code:        "   ",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authService := &fakeAuthService{}
			handler := NewIdentityHandler(authService)

			_, err := handler.VerifyLoginOTP(
				context.Background(),
				test.request,
			)

			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf(
					"status code is %v, expected %v",
					status.Code(err),
					codes.InvalidArgument,
				)
			}

			if authService.verifyOTPCalled {
				t.Fatal(
					"auth service VerifyOTP() was called for invalid input",
				)
			}
		})
	}
}

func TestIdentityHandlerRequestLoginOTPMapsRateLimitToResourceExhausted(
	t *testing.T,
) {
	authService := &fakeAuthService{
		requestOTPErr: auth.ErrOTPRequestRateLimited,
	}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestLoginOTP(
		context.Background(),
		&identityv1.RequestLoginOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE,
				Value: "+9647501234567",
			},
		},
	)

	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf(
			"status code is %v, expected %v",
			status.Code(err),
			codes.ResourceExhausted,
		)
	}

	if status.Convert(err).Message() !=
		"OTP request rate limit exceeded" {
		t.Fatalf(
			"error message is %q, expected %q",
			status.Convert(err).Message(),
			"OTP request rate limit exceeded",
		)
	}
}

func TestIdentityHandlerVerifyLoginOTPMapsApplicationErrors(t *testing.T) {
	tests := []struct {
		name         string
		serviceError error
		expectedCode codes.Code
	}{
		{
			name:         "challenge not found",
			serviceError: auth.ErrChallengeNotFound,
			expectedCode: codes.NotFound,
		},
		{
			name:         "challenge expired",
			serviceError: auth.ErrChallengeExpired,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "challenge already used",
			serviceError: auth.ErrChallengeUsed,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "challenge cancelled",
			serviceError: auth.ErrChallengeCancelled,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "challenge attempts exceeded",
			serviceError: auth.ErrChallengeAttemptsExceeded,
			expectedCode: codes.ResourceExhausted,
		},
		{
			name:         "OTP purpose mismatch",
			serviceError: auth.ErrOTPPurposeMismatch,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "invalid OTP",
			serviceError: auth.ErrInvalidOTP,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "identity inactive",
			serviceError: auth.ErrIdentityInactive,
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "unexpected internal failure",
			serviceError: errors.New("database unavailable"),
			expectedCode: codes.Internal,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authService := &fakeAuthService{
				verifyOTPErr: test.serviceError,
			}

			handler := NewIdentityHandler(authService)

			_, err := handler.VerifyLoginOTP(
				context.Background(),
				&identityv1.VerifyLoginOTPRequest{
					ChallengeId: "otp_ch_test",
					Code:        "123456",
				},
			)

			if status.Code(err) != test.expectedCode {
				t.Fatalf(
					"status code is %v, expected %v",
					status.Code(err),
					test.expectedCode,
				)
			}
		})
	}
}

func TestIdentityHandlerGetMyIdentityReturnsAuthenticatedIdentity(
	t *testing.T,
) {
	verifiedAt := time.Date(
		2026,
		time.August,
		16,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	authService := &fakeAuthService{
		getMyIdentityResult: auth.IdentityDetails{
			ID:     "11111111-1111-1111-1111-111111111111",
			Status: auth.IdentityStatusActive,
			Identifiers: []auth.IdentityDetailsIdentifier{
				{
					Identifier: auth.Identifier{
						Type:  auth.IdentifierTypePhone,
						Value: "+9647501234567",
					},
					VerifiedAt: verifiedAt,
				},
				{
					Identifier: auth.Identifier{
						Type:  auth.IdentifierTypeEmail,
						Value: "user@example.com",
					},
					VerifiedAt: verifiedAt.Add(time.Minute),
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

	response, err := handler.GetMyIdentity(
		ctx,
		&identityv1.GetMyIdentityRequest{},
	)
	if err != nil {
		t.Fatalf(
			"GetMyIdentity() returned an error: %v",
			err,
		)
	}

	if !authService.getMyIdentityCalled {
		t.Fatal("GetMyIdentity() did not call auth service")
	}

	if authService.getMyIdentityInput.IdentityID !=
		"11111111-1111-1111-1111-111111111111" {
		t.Fatalf(
			"auth service identity ID = %q",
			authService.getMyIdentityInput.IdentityID,
		)
	}

	if response.GetIdentityId() !=
		"11111111-1111-1111-1111-111111111111" {
		t.Fatalf(
			"response identity ID = %q",
			response.GetIdentityId(),
		)
	}

	if response.GetStatus() !=
		identityv1.IdentityStatus_IDENTITY_STATUS_ACTIVE {
		t.Fatalf(
			"response status = %v",
			response.GetStatus(),
		)
	}

	if len(response.GetIdentifiers()) != 2 {
		t.Fatalf(
			"identifiers count = %d, want 2",
			len(response.GetIdentifiers()),
		)
	}

	if response.GetIdentifiers()[0].GetType() !=
		identityv1.IdentifierType_IDENTIFIER_TYPE_PHONE {
		t.Fatalf(
			"first identifier type = %v",
			response.GetIdentifiers()[0].GetType(),
		)
	}

	if response.GetIdentifiers()[0].GetValue() !=
		"+9647501234567" {
		t.Fatalf(
			"first identifier value = %q",
			response.GetIdentifiers()[0].GetValue(),
		)
	}

	if !response.GetIdentifiers()[0].
		GetVerifiedAt().
		AsTime().
		Equal(verifiedAt) {
		t.Fatalf(
			"first identifier verified_at = %v, want %v",
			response.GetIdentifiers()[0].GetVerifiedAt().AsTime(),
			verifiedAt,
		)
	}

	if response.GetIdentifiers()[1].GetType() !=
		identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL {
		t.Fatalf(
			"second identifier type = %v",
			response.GetIdentifiers()[1].GetType(),
		)
	}

	if response.GetIdentifiers()[1].GetValue() !=
		"user@example.com" {
		t.Fatalf(
			"second identifier value = %q",
			response.GetIdentifiers()[1].GetValue(),
		)
	}
}

func TestIdentityHandlerGetMyIdentityRejectsMissingAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.GetMyIdentity(
		context.Background(),
		&identityv1.GetMyIdentityRequest{},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"GetMyIdentity() code = %v, want %v",
			status.Code(err),
			codes.Unauthenticated,
		)
	}

	if authService.getMyIdentityCalled {
		t.Fatal(
			"GetMyIdentity() called auth service without authenticated identity",
		)
	}
}

func TestIdentityHandlerGetMyIdentityMapsIdentityNotFound(
	t *testing.T,
) {
	authService := &fakeAuthService{
		getMyIdentityErr: auth.ErrIdentityNotFound,
	}

	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "11111111-1111-1111-1111-111111111111",
			SessionID:  "22222222-2222-2222-2222-222222222222",
		},
	)

	_, err := handler.GetMyIdentity(
		ctx,
		&identityv1.GetMyIdentityRequest{},
	)

	if status.Code(err) != codes.NotFound {
		t.Fatalf(
			"GetMyIdentity() code = %v, want %v",
			status.Code(err),
			codes.NotFound,
		)
	}
}

func TestIdentityHandlerGetMyIdentityMapsUnexpectedErrorToInternal(
	t *testing.T,
) {
	authService := &fakeAuthService{
		getMyIdentityErr: errors.New(
			"identity store unavailable",
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

	_, err := handler.GetMyIdentity(
		ctx,
		&identityv1.GetMyIdentityRequest{},
	)

	if status.Code(err) != codes.Internal {
		t.Fatalf(
			"GetMyIdentity() code = %v, want %v",
			status.Code(err),
			codes.Internal,
		)
	}
}

func TestIdentityHandlerRefreshTokenReturnsRotatedTokens(
	t *testing.T,
) {
	authService := &fakeAuthService{
		refreshTokenResult: auth.RefreshTokenResult{
			IdentityID:                  "identity-123",
			AccessToken:                 "new-access-token",
			RefreshToken:                "new-refresh-token",
			AccessTokenExpiresInSeconds: 900,
		},
	}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.RefreshToken(
		context.Background(),
		&identityv1.RefreshTokenRequest{
			RefreshToken: "current-refresh-token",
		},
	)
	if err != nil {
		t.Fatalf(
			"RefreshToken() returned an error: %v",
			err,
		)
	}

	if !authService.refreshTokenCalled {
		t.Fatal(
			"auth service RefreshToken() was not called",
		)
	}

	if authService.refreshTokenInput.RefreshToken !=
		"current-refresh-token" {
		t.Fatalf(
			"auth service received refresh token %q, expected %q",
			authService.refreshTokenInput.RefreshToken,
			"current-refresh-token",
		)
	}

	if response.GetIdentityId() != "identity-123" {
		t.Fatalf(
			"IdentityId = %q, expected %q",
			response.GetIdentityId(),
			"identity-123",
		)
	}

	if response.GetAccessToken() != "new-access-token" {
		t.Fatalf(
			"AccessToken = %q, expected %q",
			response.GetAccessToken(),
			"new-access-token",
		)
	}

	if response.GetRefreshToken() != "new-refresh-token" {
		t.Fatalf(
			"RefreshToken = %q, expected %q",
			response.GetRefreshToken(),
			"new-refresh-token",
		)
	}

	if response.GetAccessTokenExpiresInSeconds() != 900 {
		t.Fatalf(
			"AccessTokenExpiresInSeconds = %d, expected 900",
			response.GetAccessTokenExpiresInSeconds(),
		)
	}
}

func TestIdentityHandlerRefreshTokenRejectsInvalidInput(
	t *testing.T,
) {
	tests := []struct {
		name    string
		request *identityv1.RefreshTokenRequest
	}{
		{
			name:    "nil request",
			request: nil,
		},
		{
			name: "empty refresh token",
			request: &identityv1.RefreshTokenRequest{
				RefreshToken: "",
			},
		},
		{
			name: "spaces only refresh token",
			request: &identityv1.RefreshTokenRequest{
				RefreshToken: "   ",
			},
		},
		{
			name: "tabs and newlines refresh token",
			request: &identityv1.RefreshTokenRequest{
				RefreshToken: "\t\n ",
			},
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				authService := &fakeAuthService{}

				handler := NewIdentityHandler(
					authService,
				)

				_, err := handler.RefreshToken(
					context.Background(),
					tt.request,
				)

				if status.Code(err) != codes.InvalidArgument {
					t.Fatalf(
						"RefreshToken() code = %v, expected %v",
						status.Code(err),
						codes.InvalidArgument,
					)
				}

				if authService.refreshTokenCalled {
					t.Fatal(
						"auth service RefreshToken() was called for invalid input",
					)
				}
			},
		)
	}
}

func TestIdentityHandlerRefreshTokenMapsApplicationErrors(
	t *testing.T,
) {
	tests := []struct {
		name         string
		serviceErr   error
		expectedCode codes.Code
	}{
		{
			name:         "invalid refresh token",
			serviceErr:   auth.ErrInvalidRefreshToken,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "refresh token expired",
			serviceErr:   auth.ErrRefreshTokenExpired,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "refresh token revoked",
			serviceErr:   auth.ErrRefreshTokenRevoked,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "refresh token reused",
			serviceErr:   auth.ErrRefreshTokenReused,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "session expired",
			serviceErr:   auth.ErrSessionExpired,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "session revoked",
			serviceErr:   auth.ErrSessionRevoked,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "identity inactive",
			serviceErr:   auth.ErrIdentityInactive,
			expectedCode: codes.PermissionDenied,
		},
		{
			name:         "unexpected internal failure",
			serviceErr:   errors.New("unexpected failure"),
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				authService := &fakeAuthService{
					refreshTokenErr: tt.serviceErr,
				}

				handler := NewIdentityHandler(
					authService,
				)

				_, err := handler.RefreshToken(
					context.Background(),
					&identityv1.RefreshTokenRequest{
						RefreshToken: "current-refresh-token",
					},
				)

				if status.Code(err) != tt.expectedCode {
					t.Fatalf(
						"RefreshToken() code = %v, expected %v",
						status.Code(err),
						tt.expectedCode,
					)
				}

				if !authService.refreshTokenCalled {
					t.Fatal(
						"auth service RefreshToken() was not called",
					)
				}
			},
		)
	}
}

func TestIdentityHandlerLogoutRevokesCurrentSession(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.Logout(
		context.Background(),
		&identityv1.LogoutRequest{
			RefreshToken: "current-refresh-token",
		},
	)
	if err != nil {
		t.Fatalf(
			"Logout() returned an error: %v",
			err,
		)
	}

	if response == nil {
		t.Fatal(
			"Logout() returned nil response",
		)
	}

	if !authService.logoutCalled {
		t.Fatal(
			"auth service Logout() was not called",
		)
	}

	if authService.logoutInput.RefreshToken !=
		"current-refresh-token" {
		t.Fatalf(
			"auth service received refresh token %q, expected %q",
			authService.logoutInput.RefreshToken,
			"current-refresh-token",
		)
	}
}

func TestIdentityHandlerLogoutRejectsInvalidInput(
	t *testing.T,
) {
	tests := []struct {
		name    string
		request *identityv1.LogoutRequest
	}{
		{
			name:    "nil request",
			request: nil,
		},
		{
			name: "empty refresh token",
			request: &identityv1.LogoutRequest{
				RefreshToken: "",
			},
		},
		{
			name: "spaces only refresh token",
			request: &identityv1.LogoutRequest{
				RefreshToken: "   ",
			},
		},
		{
			name: "tabs and newlines refresh token",
			request: &identityv1.LogoutRequest{
				RefreshToken: "\t\n ",
			},
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				authService := &fakeAuthService{}

				handler := NewIdentityHandler(
					authService,
				)

				_, err := handler.Logout(
					context.Background(),
					tt.request,
				)

				if status.Code(err) != codes.InvalidArgument {
					t.Fatalf(
						"Logout() code = %v, expected %v",
						status.Code(err),
						codes.InvalidArgument,
					)
				}

				if authService.logoutCalled {
					t.Fatal(
						"auth service Logout() was called for invalid input",
					)
				}
			},
		)
	}
}

func TestIdentityHandlerLogoutMapsInvalidRefreshTokenToInvalidArgument(
	t *testing.T,
) {
	authService := &fakeAuthService{
		logoutErr: auth.ErrInvalidRefreshToken,
	}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.Logout(
		context.Background(),
		&identityv1.LogoutRequest{
			RefreshToken: "current-refresh-token",
		},
	)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf(
			"Logout() code = %v, expected %v",
			status.Code(err),
			codes.InvalidArgument,
		)
	}

	if response != nil {
		t.Fatal(
			"Logout() returned a response for invalid refresh token",
		)
	}

	if !authService.logoutCalled {
		t.Fatal(
			"auth service Logout() was not called",
		)
	}
}

func TestIdentityHandlerLogoutMapsServiceFailureToInternal(
	t *testing.T,
) {
	authService := &fakeAuthService{
		logoutErr: errors.New("database failure"),
	}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.Logout(
		context.Background(),
		&identityv1.LogoutRequest{
			RefreshToken: "current-refresh-token",
		},
	)

	if status.Code(err) != codes.Internal {
		t.Fatalf(
			"Logout() code = %v, expected %v",
			status.Code(err),
			codes.Internal,
		)
	}

	if response != nil {
		t.Fatal(
			"Logout() returned a response on failure",
		)
	}

	if !authService.logoutCalled {
		t.Fatal(
			"auth service Logout() was not called",
		)
	}
}

func TestIdentityHandlerLogoutAllSessionsRevokesAllSessions(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.LogoutAllSessions(
		context.Background(),
		&identityv1.LogoutAllSessionsRequest{
			RefreshToken: "current-refresh-token",
		},
	)
	if err != nil {
		t.Fatalf(
			"LogoutAllSessions() returned an error: %v",
			err,
		)
	}

	if response == nil {
		t.Fatal(
			"LogoutAllSessions() returned nil response",
		)
	}

	if !authService.logoutAllSessionsCalled {
		t.Fatal(
			"auth service LogoutAllSessions() was not called",
		)
	}

	if authService.logoutAllSessionsInput.RefreshToken !=
		"current-refresh-token" {
		t.Fatalf(
			"auth service received refresh token %q, expected %q",
			authService.logoutAllSessionsInput.RefreshToken,
			"current-refresh-token",
		)
	}
}

func TestIdentityHandlerLogoutAllSessionsRejectsInvalidInput(
	t *testing.T,
) {
	tests := []struct {
		name    string
		request *identityv1.LogoutAllSessionsRequest
	}{
		{
			name:    "nil request",
			request: nil,
		},
		{
			name: "empty refresh token",
			request: &identityv1.LogoutAllSessionsRequest{
				RefreshToken: "",
			},
		},
		{
			name: "spaces only refresh token",
			request: &identityv1.LogoutAllSessionsRequest{
				RefreshToken: "   ",
			},
		},
		{
			name: "tabs and newlines refresh token",
			request: &identityv1.LogoutAllSessionsRequest{
				RefreshToken: "\t\n ",
			},
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				authService := &fakeAuthService{}

				handler := NewIdentityHandler(
					authService,
				)

				_, err := handler.LogoutAllSessions(
					context.Background(),
					tt.request,
				)

				if status.Code(err) != codes.InvalidArgument {
					t.Fatalf(
						"LogoutAllSessions() code = %v, expected %v",
						status.Code(err),
						codes.InvalidArgument,
					)
				}

				if authService.logoutAllSessionsCalled {
					t.Fatal(
						"auth service LogoutAllSessions() was called for invalid input",
					)
				}
			},
		)
	}
}

func TestIdentityHandlerLogoutAllSessionsMapsInvalidRefreshTokenToInvalidArgument(
	t *testing.T,
) {
	authService := &fakeAuthService{
		logoutAllSessionsErr: auth.ErrInvalidRefreshToken,
	}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.LogoutAllSessions(
		context.Background(),
		&identityv1.LogoutAllSessionsRequest{
			RefreshToken: "current-refresh-token",
		},
	)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf(
			"LogoutAllSessions() code = %v, expected %v",
			status.Code(err),
			codes.InvalidArgument,
		)
	}

	if response != nil {
		t.Fatal(
			"LogoutAllSessions() returned a response for invalid refresh token",
		)
	}

	if !authService.logoutAllSessionsCalled {
		t.Fatal(
			"auth service LogoutAllSessions() was not called",
		)
	}
}

func TestNewIdentityHandlerPanicsWhenAuthServiceIsNil(
	t *testing.T,
) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"NewIdentityHandler() did not panic for nil auth service",
			)
		}
	}()

	NewIdentityHandler(nil)
}

func TestIdentityHandlerLogoutAllSessionsMapsServiceFailureToInternal(
	t *testing.T,
) {
	authService := &fakeAuthService{
		logoutAllSessionsErr: errors.New("database failure"),
	}

	handler := NewIdentityHandler(
		authService,
	)

	response, err := handler.LogoutAllSessions(
		context.Background(),
		&identityv1.LogoutAllSessionsRequest{
			RefreshToken: "current-refresh-token",
		},
	)

	if status.Code(err) != codes.Internal {
		t.Fatalf(
			"LogoutAllSessions() code = %v, expected %v",
			status.Code(err),
			codes.Internal,
		)
	}

	if response != nil {
		t.Fatal(
			"LogoutAllSessions() returned a response on failure",
		)
	}

	if !authService.logoutAllSessionsCalled {
		t.Fatal(
			"auth service LogoutAllSessions() was not called",
		)
	}
}

func TestIdentityHandlerRequestIdentifierUnlinkOTPUsesAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{
		requestIdentifierUnlinkOTPResult: auth.RequestIdentifierUnlinkOTPResult{
			ChallengeID:      "otp_ch_unlink_test",
			ExpiresInSeconds: 300,
		},
	}

	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "identity-123",
			SessionID:  "session-456",
		},
	)

	response, err := handler.RequestIdentifierUnlinkOTP(
		ctx,
		&identityv1.RequestIdentifierUnlinkOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL,
				Value: "  user@example.com  ",
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"RequestIdentifierUnlinkOTP() returned an error: %v",
			err,
		)
	}

	if !authService.requestIdentifierUnlinkOTPCalled {
		t.Fatal(
			"auth service RequestIdentifierUnlinkOTP() was not called",
		)
	}

	if authService.requestIdentifierUnlinkOTPInput.IdentityID !=
		"identity-123" {
		t.Fatalf(
			"identity ID = %q, expected %q",
			authService.requestIdentifierUnlinkOTPInput.IdentityID,
			"identity-123",
		)
	}

	expectedIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "user@example.com",
	}

	if authService.requestIdentifierUnlinkOTPInput.TargetIdentifier !=
		expectedIdentifier {
		t.Fatalf(
			"target identifier = %+v, expected %+v",
			authService.requestIdentifierUnlinkOTPInput.TargetIdentifier,
			expectedIdentifier,
		)
	}

	if response.GetChallengeId() != "otp_ch_unlink_test" {
		t.Fatalf(
			"ChallengeId = %q, expected %q",
			response.GetChallengeId(),
			"otp_ch_unlink_test",
		)
	}

	if response.GetExpiresInSeconds() != 300 {
		t.Fatalf(
			"ExpiresInSeconds = %d, expected 300",
			response.GetExpiresInSeconds(),
		)
	}
}

func TestIdentityHandlerRequestIdentifierUnlinkOTPRejectsMissingAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.RequestIdentifierUnlinkOTP(
		context.Background(),
		&identityv1.RequestIdentifierUnlinkOTPRequest{
			Identifier: &identityv1.Identifier{
				Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL,
				Value: "user@example.com",
			},
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"status code = %v, expected %v",
			status.Code(err),
			codes.Unauthenticated,
		)
	}

	if authService.requestIdentifierUnlinkOTPCalled {
		t.Fatal(
			"auth service RequestIdentifierUnlinkOTP() was called without authenticated identity",
		)
	}
}

func TestIdentityHandlerVerifyIdentifierUnlinkOTPUsesAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	ctx := contextWithAuthenticatedPrincipal(
		context.Background(),
		authenticatedPrincipal{
			IdentityID: "identity-123",
			SessionID:  "session-456",
		},
	)

	response, err := handler.VerifyIdentifierUnlinkOTP(
		ctx,
		&identityv1.VerifyIdentifierUnlinkOTPRequest{
			ChallengeId: "  otp_ch_unlink_test  ",
			Code:        "  123456  ",
		},
	)
	if err != nil {
		t.Fatalf(
			"VerifyIdentifierUnlinkOTP() returned an error: %v",
			err,
		)
	}

	if !authService.verifyOTPCalled {
		t.Fatal(
			"auth service VerifyOTP() was not called",
		)
	}

	if authService.verifyOTPInput.ChallengeID !=
		"otp_ch_unlink_test" {
		t.Fatalf(
			"challenge ID = %q, expected %q",
			authService.verifyOTPInput.ChallengeID,
			"otp_ch_unlink_test",
		)
	}

	if authService.verifyOTPInput.Code != "123456" {
		t.Fatalf(
			"OTP code = %q, expected %q",
			authService.verifyOTPInput.Code,
			"123456",
		)
	}

	if authService.verifyOTPInput.ExpectedPurpose !=
		auth.OTPPurposeUnlinkIdentifier {
		t.Fatalf(
			"expected purpose = %q, expected %q",
			authService.verifyOTPInput.ExpectedPurpose,
			auth.OTPPurposeUnlinkIdentifier,
		)
	}

	if authService.verifyOTPInput.ExpectedTargetIdentityID == nil {
		t.Fatal(
			"expected target identity ID is nil",
		)
	}

	if *authService.verifyOTPInput.ExpectedTargetIdentityID !=
		"identity-123" {
		t.Fatalf(
			"expected target identity ID = %q, expected %q",
			*authService.verifyOTPInput.ExpectedTargetIdentityID,
			"identity-123",
		)
	}

	if response == nil {
		t.Fatal(
			"VerifyIdentifierUnlinkOTP() returned nil response",
		)
	}
}

func TestIdentityHandlerVerifyIdentifierUnlinkOTPRejectsMissingAuthenticatedIdentity(
	t *testing.T,
) {
	authService := &fakeAuthService{}

	handler := NewIdentityHandler(authService)

	_, err := handler.VerifyIdentifierUnlinkOTP(
		context.Background(),
		&identityv1.VerifyIdentifierUnlinkOTPRequest{
			ChallengeId: "otp_ch_unlink_test",
			Code:        "123456",
		},
	)

	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf(
			"status code = %v, expected %v",
			status.Code(err),
			codes.Unauthenticated,
		)
	}

	if authService.verifyOTPCalled {
		t.Fatal(
			"auth service VerifyOTP() was called without authenticated identity",
		)
	}
}
func TestIdentityHandlerRequestIdentifierUnlinkOTPMapsErrors(
	t *testing.T,
) {
	tests := []struct {
		name         string
		serviceErr   error
		expectedCode codes.Code
	}{
		{
			name:         "invalid phone number",
			serviceErr:   auth.ErrInvalidPhoneNumber,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "invalid email address",
			serviceErr:   auth.ErrInvalidEmailAddress,
			expectedCode: codes.InvalidArgument,
		},
		{
			name:         "identity not found",
			serviceErr:   auth.ErrIdentityNotFound,
			expectedCode: codes.NotFound,
		},
		{
			name:         "identifier not linked",
			serviceErr:   auth.ErrIdentifierNotLinked,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "last identifier removal",
			serviceErr:   auth.ErrLastIdentifierRemoval,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "rate limited",
			serviceErr:   auth.ErrOTPRequestRateLimited,
			expectedCode: codes.ResourceExhausted,
		},
		{
			name:         "unexpected error",
			serviceErr:   errors.New("unexpected unlink request failure"),
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authService := &fakeAuthService{
				requestIdentifierUnlinkOTPErr: tt.serviceErr,
			}

			handler := NewIdentityHandler(authService)

			ctx := contextWithAuthenticatedPrincipal(
				context.Background(),
				authenticatedPrincipal{
					IdentityID: "identity-123",
					SessionID:  "session-456",
				},
			)

			_, err := handler.RequestIdentifierUnlinkOTP(
				ctx,
				&identityv1.RequestIdentifierUnlinkOTPRequest{
					Identifier: &identityv1.Identifier{
						Type:  identityv1.IdentifierType_IDENTIFIER_TYPE_EMAIL,
						Value: "user@example.com",
					},
				},
			)

			if status.Code(err) != tt.expectedCode {
				t.Fatalf(
					"status code = %v, expected %v",
					status.Code(err),
					tt.expectedCode,
				)
			}
		})
	}
}

func TestIdentityHandlerVerifyIdentifierUnlinkOTPMapsErrors(
	t *testing.T,
) {
	tests := []struct {
		name         string
		serviceErr   error
		expectedCode codes.Code
	}{
		{
			name:         "challenge not found",
			serviceErr:   auth.ErrChallengeNotFound,
			expectedCode: codes.NotFound,
		},
		{
			name:         "challenge expired",
			serviceErr:   auth.ErrChallengeExpired,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "challenge used",
			serviceErr:   auth.ErrChallengeUsed,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "challenge cancelled",
			serviceErr:   auth.ErrChallengeCancelled,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "attempts exceeded",
			serviceErr:   auth.ErrChallengeAttemptsExceeded,
			expectedCode: codes.ResourceExhausted,
		},
		{
			name:         "purpose mismatch",
			serviceErr:   auth.ErrOTPPurposeMismatch,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "target identity mismatch",
			serviceErr:   auth.ErrOTPChallengeTargetMismatch,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "invalid OTP",
			serviceErr:   auth.ErrInvalidOTP,
			expectedCode: codes.Unauthenticated,
		},
		{
			name:         "identity not found",
			serviceErr:   auth.ErrIdentityNotFound,
			expectedCode: codes.NotFound,
		},
		{
			name:         "identifier not linked",
			serviceErr:   auth.ErrIdentifierNotLinked,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "last identifier removal",
			serviceErr:   auth.ErrLastIdentifierRemoval,
			expectedCode: codes.FailedPrecondition,
		},
		{
			name:         "unexpected error",
			serviceErr:   errors.New("unexpected unlink verification failure"),
			expectedCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authService := &fakeAuthService{
				verifyOTPErr: tt.serviceErr,
			}

			handler := NewIdentityHandler(authService)

			ctx := contextWithAuthenticatedPrincipal(
				context.Background(),
				authenticatedPrincipal{
					IdentityID: "identity-123",
					SessionID:  "session-456",
				},
			)

			_, err := handler.VerifyIdentifierUnlinkOTP(
				ctx,
				&identityv1.VerifyIdentifierUnlinkOTPRequest{
					ChallengeId: "otp_ch_unlink_test",
					Code:        "123456",
				},
			)

			if status.Code(err) != tt.expectedCode {
				t.Fatalf(
					"status code = %v, expected %v",
					status.Code(err),
					tt.expectedCode,
				)
			}
		})
	}
}
