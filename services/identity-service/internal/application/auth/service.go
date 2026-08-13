package auth

import "context"

type RequestOTPInput struct {
	Identifier       Identifier
	Purpose          OTPPurpose
	TargetIdentityID *string
}

type RequestOTPResult struct {
	ChallengeID      string
	ExpiresInSeconds int32
}

type VerifyOTPInput struct {
	ChallengeID              string
	Code                     string
	ExpectedPurpose          OTPPurpose
	ExpectedTargetIdentityID *string
}

type VerifyOTPResult struct {
	IdentityID                  string
	AccessToken                 string
	RefreshToken                string
	AccessTokenExpiresInSeconds int32
}

type RefreshTokenInput struct {
	RefreshToken string
}

type RefreshTokenResult struct {
	IdentityID                  string
	AccessToken                 string
	RefreshToken                string
	AccessTokenExpiresInSeconds int32
}

type LogoutInput struct {
	RefreshToken string
}

type LogoutAllSessionsInput struct {
	RefreshToken string
}

type Service interface {
	RequestOTP(
		ctx context.Context,
		input RequestOTPInput,
	) (RequestOTPResult, error)

	VerifyOTP(
		ctx context.Context,
		input VerifyOTPInput,
	) (VerifyOTPResult, error)

	RefreshToken(
		ctx context.Context,
		input RefreshTokenInput,
	) (RefreshTokenResult, error)

	Logout(ctx context.Context, input LogoutInput) error

	LogoutAllSessions(
		ctx context.Context,
		input LogoutAllSessionsInput,
	) error
}
