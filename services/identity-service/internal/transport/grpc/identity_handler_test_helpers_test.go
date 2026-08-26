package grpc

import (
	"context"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
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

	listMySessionsResult auth.ListMySessionsResult
	listMySessionsErr    error
	listMySessionsInput  auth.ListMySessionsInput
	listMySessionsCalled bool

	revokeSessionErr    error
	revokeSessionInput  auth.RevokeSessionInput
	revokeSessionCalled bool

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

func (f *fakeAuthService) ListMySessions(
	ctx context.Context,
	input auth.ListMySessionsInput,
) (auth.ListMySessionsResult, error) {
	f.listMySessionsCalled = true
	f.listMySessionsInput = input

	if f.listMySessionsErr != nil {
		return auth.ListMySessionsResult{}, f.listMySessionsErr
	}

	return f.listMySessionsResult, nil
}

func (f *fakeAuthService) RevokeSession(
	ctx context.Context,
	input auth.RevokeSessionInput,
) error {
	f.revokeSessionCalled = true
	f.revokeSessionInput = input

	return f.revokeSessionErr
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
