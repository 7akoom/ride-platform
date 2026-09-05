package auth

import (
	"context"
	"time"
)

type RequestOTPInput struct {
	Identifier       Identifier
	Purpose          OTPPurpose
	TargetIdentityID *string
	TenantHint       string
	Channel          OTPDeliveryChannel
	Locale           string
	SourceIPAddress  string
}

type RequestOTPResult struct {
	ChallengeID      string
	ExpiresInSeconds int32
}

type SessionMetadata struct {
	ClientID   string
	DeviceID   string
	DeviceName string
	Platform   string
	AppVersion string
	IPAddress  string
	UserAgent  string
}

type VerifyOTPInput struct {
	ChallengeID              string
	Code                     string
	ExpectedPurpose          OTPPurpose
	ExpectedTargetIdentityID *string
	SessionMetadata          SessionMetadata
}

type VerifyOTPResult struct {
	IdentityID                  string
	AccessToken                 string
	RefreshToken                string
	AccessTokenExpiresInSeconds int32
}

type GetMyIdentityInput struct {
	IdentityID string
}

type RequestIdentifierUnlinkOTPInput struct {
	IdentityID       string
	TargetIdentifier Identifier
	TenantHint       string
	Channel          OTPDeliveryChannel
	Locale           string
	SourceIPAddress  string
}

type RequestIdentifierUnlinkOTPResult struct {
	ChallengeID      string
	ExpiresInSeconds int32
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

type ListMySessionsInput struct {
	IdentityID       string
	CurrentSessionID string
}

type SessionInfo struct {
	SessionID  string
	ClientID   *string
	DeviceID   *string
	DeviceName *string
	Platform   *string
	AppVersion *string
	IPAddress  *string
	UserAgent  *string

	ExpiresAt  time.Time
	LastSeenAt *time.Time
	CreatedAt  time.Time

	IsCurrent bool
}

type ListMySessionsResult struct {
	Sessions []SessionInfo
}

type RevokeSessionInput struct {
	IdentityID string
	SessionID  string
}

type IdentityLifecycleInput struct {
	IdentityID string
}

type IdentityLifecycleResult struct {
	PreviousStatus IdentityStatus
	CurrentStatus  IdentityStatus
	Changed        bool
}

type IdentityLifecycleService interface {
	SuspendIdentity(
		ctx context.Context,
		input IdentityLifecycleInput,
	) (IdentityLifecycleResult, error)

	DisableIdentity(
		ctx context.Context,
		input IdentityLifecycleInput,
	) (IdentityLifecycleResult, error)

	ReactivateIdentity(
		ctx context.Context,
		input IdentityLifecycleInput,
	) (IdentityLifecycleResult, error)
}

type ServiceWithIdentityLifecycle interface {
	Service
	IdentityLifecycleService
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

	RequestIdentifierUnlinkOTP(
		ctx context.Context,
		input RequestIdentifierUnlinkOTPInput,
	) (RequestIdentifierUnlinkOTPResult, error)

	GetMyIdentity(
		ctx context.Context,
		input GetMyIdentityInput,
	) (IdentityDetails, error)

	ListMySessions(
		ctx context.Context,
		input ListMySessionsInput,
	) (ListMySessionsResult, error)

	RevokeSession(
		ctx context.Context,
		input RevokeSessionInput,
	) error

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
