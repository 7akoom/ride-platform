package deletion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

// OTP is what sending and checking the confirmation code needs: the same
// generator, hasher, delivery, limiter and challenge lookups as signing in.
type OTP struct {
	Generator    auth.OTPGenerator
	Hasher       auth.OTPHasher
	Delivery     auth.OTPDelivery
	RateLimiter  auth.OTPRequestRateLimiter
	RateLimit    auth.OTPRequestRateLimitPolicy
	ChallengeIDs auth.ChallengeIDGenerator
	Challenges   auth.ChallengeRepository
	TTL          time.Duration
}

// Settings are the grace period and the eraser's pace.
type Settings struct {
	GracePeriod time.Duration
	// How long the eraser waits before trying again after something stood in
	// the way of erasing a due account.
	RetryAfter time.Duration
}

type Service struct {
	store      Store
	identities auth.IdentityReader
	accounts   Accounts
	media      Media
	otp        OTP
	settings   Settings
	clock      Clock
	logger     *slog.Logger
}

func NewService(
	store Store,
	identities auth.IdentityReader,
	accounts Accounts,
	media Media,
	otp OTP,
	settings Settings,
	clock Clock,
	logger *slog.Logger,
) *Service {
	if store == nil || identities == nil || accounts == nil || media == nil || clock == nil || logger == nil {
		panic("deletion service dependencies are required")
	}

	if otp.Generator == nil || otp.Hasher == nil || otp.Delivery == nil || otp.RateLimiter == nil ||
		otp.ChallengeIDs == nil || otp.Challenges == nil || otp.TTL <= 0 {
		panic("deletion OTP dependencies are required")
	}

	if settings.GracePeriod <= 0 || settings.RetryAfter <= 0 {
		panic("deletion settings are required")
	}

	return &Service{
		store: store, identities: identities, accounts: accounts, media: media,
		otp: otp, settings: settings, clock: clock, logger: logger,
	}
}

// GracePeriod is how long a deletion waits.
func (s *Service) GracePeriod() time.Duration { return s.settings.GracePeriod }

// Overview is what the account page shows.
type Overview struct {
	Deletion Deletion
	Standing Standing
}

// Get says whether a deletion is pending and what deleting would meet now.
func (s *Service) Get(ctx context.Context, identityID string) (Overview, error) {
	identityID = strings.TrimSpace(identityID)

	current, found, err := s.store.Find(ctx, identityID)
	if err != nil {
		return Overview{}, err
	}

	if !found || current.Status != StatusPending {
		current = Deletion{IdentityID: identityID, Status: StatusNone}
	}

	standing, err := s.accounts.Standing(ctx, identityID)
	if err != nil {
		return Overview{}, err
	}

	return Overview{Deletion: current, Standing: standing}, nil
}

// RequestOTPInput asks for the confirmation code.
type RequestOTPInput struct {
	IdentityID      string
	TenantHint      string
	Channel         auth.OTPDeliveryChannel
	Locale          string
	SourceIPAddress string
}

// RequestOTPResult is the challenge to confirm with.
type RequestOTPResult struct {
	ChallengeID      string
	ExpiresInSeconds int32
}

// RequestOTP sends the code to one of the account's own sign-in methods,
// once nothing stands in the way.
func (s *Service) RequestOTP(ctx context.Context, input RequestOTPInput) (RequestOTPResult, error) {
	identityID := strings.TrimSpace(input.IdentityID)

	channel, err := auth.ParseOTPDeliveryChannel(string(input.Channel))
	if err != nil {
		return RequestOTPResult{}, err
	}

	identifier, err := s.eligible(ctx, identityID, channel)
	if err != nil {
		return RequestOTPResult{}, err
	}

	if _, err := s.mustHaveNoBlockers(ctx, identityID); err != nil {
		return RequestOTPResult{}, err
	}

	tenantHint, err := auth.NormalizeTenantHint(input.TenantHint)
	if err != nil {
		return RequestOTPResult{}, err
	}

	now := s.clock.Now()
	target := identityID

	if err := s.otp.RateLimiter.Allow(ctx, auth.OTPRequestScope{
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeDeleteAccount,
		TargetIdentityID: &target,
		SourceIPAddress:  strings.TrimSpace(input.SourceIPAddress),
	}, now, s.otp.RateLimit); err != nil {
		if errors.Is(err, auth.ErrOTPRequestRateLimited) {
			return RequestOTPResult{}, auth.ErrOTPRequestRateLimited
		}

		return RequestOTPResult{}, fmt.Errorf("apply the deletion code rate limit: %w", err)
	}

	code, err := s.otp.Generator.Generate()
	if err != nil {
		return RequestOTPResult{}, fmt.Errorf("generate the deletion code: %w", err)
	}

	challengeID, err := s.otp.ChallengeIDs.Generate()
	if err != nil {
		return RequestOTPResult{}, fmt.Errorf("generate the deletion challenge id: %w", err)
	}

	codeHash, err := s.otp.Hasher.Hash(challengeID, code)
	if err != nil {
		return RequestOTPResult{}, fmt.Errorf("hash the deletion code: %w", err)
	}

	challenge := auth.OTPChallenge{
		ID:               challengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeDeleteAccount,
		TargetIdentityID: &target,
		TenantHint:       tenantHint,
		CodeHash:         codeHash,
		ExpiresAt:        now.Add(s.otp.TTL),
	}

	if err := s.store.CreateChallenge(ctx, challenge); err != nil {
		return RequestOTPResult{}, fmt.Errorf("store the deletion challenge: %w", err)
	}

	if err := s.otp.Delivery.Send(ctx, auth.OTPDeliveryInput{
		ChallengeID: challengeID,
		Identifier:  identifier,
		Code:        code,
		Purpose:     auth.OTPPurposeDeleteAccount,
		Channel:     channel,
		Locale:      input.Locale,
	}); err != nil {
		cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()

		if cancelErr := s.otp.Challenges.Cancel(cancelCtx, challengeID, s.clock.Now()); cancelErr != nil {
			return RequestOTPResult{}, errors.Join(
				fmt.Errorf("%w: %v", ErrDeliveryFailed, err),
				fmt.Errorf("cancel the deletion challenge after a delivery failure: %w", cancelErr),
			)
		}

		return RequestOTPResult{}, fmt.Errorf("%w: %v", ErrDeliveryFailed, err)
	}

	return RequestOTPResult{ChallengeID: challengeID, ExpiresInSeconds: int32(s.otp.TTL.Seconds())}, nil
}

// eligible checks the account may ask and picks where the code goes: a phone
// for SMS, WhatsApp and auto (an email when there is no phone), an email for
// email.
func (s *Service) eligible(ctx context.Context, identityID string, channel auth.OTPDeliveryChannel) (auth.Identifier, error) {
	if identityID == "" {
		return auth.Identifier{}, auth.ErrIdentityNotFound
	}

	details, found, err := s.identities.FindByID(ctx, identityID)
	if err != nil {
		return auth.Identifier{}, fmt.Errorf("read the identity: %w", err)
	}

	if !found {
		return auth.Identifier{}, auth.ErrIdentityNotFound
	}

	if details.Status != auth.IdentityStatusActive {
		return auth.Identifier{}, ErrNotActive
	}

	current, pending, err := s.store.Find(ctx, identityID)
	if err != nil {
		return auth.Identifier{}, err
	}

	if pending && current.Status == StatusPending {
		return auth.Identifier{}, ErrAlreadyPending
	}

	var phone, email *auth.Identifier

	for _, linked := range details.Identifiers {
		identifier := linked.Identifier

		switch identifier.Type {
		case auth.IdentifierTypePhone:
			if phone == nil {
				phone = &identifier
			}
		case auth.IdentifierTypeEmail:
			if email == nil {
				email = &identifier
			}
		}
	}

	switch channel {
	case auth.OTPDeliveryChannelSMS, auth.OTPDeliveryChannelWhatsApp:
		if phone != nil {
			return *phone, nil
		}
	case auth.OTPDeliveryChannelEmail:
		if email != nil {
			return *email, nil
		}
	default:
		if phone != nil {
			return *phone, nil
		}

		if email != nil {
			return *email, nil
		}
	}

	return auth.Identifier{}, auth.ErrOTPDeliveryChannelUnavailable
}

func (s *Service) mustHaveNoBlockers(ctx context.Context, identityID string) (Standing, error) {
	standing, err := s.accounts.Standing(ctx, identityID)
	if err != nil {
		return Standing{}, err
	}

	if len(standing.Blockers) > 0 {
		return standing, &BlockedError{Blockers: standing.Blockers}
	}

	return standing, nil
}

// BlockedError lists what stands in the way; it matches ErrBlocked.
type BlockedError struct {
	Blockers []Blocker
}

func (e *BlockedError) Error() string {
	names := make([]string, len(e.Blockers))
	for i, b := range e.Blockers {
		names[i] = string(b)
	}

	return ErrBlocked.Error() + ": " + strings.Join(names, ", ")
}

func (e *BlockedError) Unwrap() error { return ErrBlocked }

// ConfirmRequest is the person confirming with the code.
type ConfirmRequest struct {
	IdentityID        string
	ChallengeID       string
	Code              string
	AcceptBalanceLoss bool
}

// Confirm checks the code and, once nothing stands in the way and any
// balance loss is accepted, starts the deletion: every session ends now and
// the account is erased after the grace period.
func (s *Service) Confirm(ctx context.Context, request ConfirmRequest) (Deletion, error) {
	identityID := strings.TrimSpace(request.IdentityID)
	if identityID == "" {
		return Deletion{}, auth.ErrIdentityNotFound
	}

	challenge, err := s.otp.Challenges.FindByID(ctx, strings.TrimSpace(request.ChallengeID))
	if err != nil {
		if errors.Is(err, auth.ErrChallengeNotFound) {
			return Deletion{}, auth.ErrChallengeNotFound
		}

		return Deletion{}, fmt.Errorf("find the deletion challenge: %w", err)
	}

	now := s.clock.Now()

	switch {
	case challenge.Purpose != auth.OTPPurposeDeleteAccount:
		return Deletion{}, auth.ErrOTPPurposeMismatch
	case challenge.TargetIdentityID == nil || strings.TrimSpace(*challenge.TargetIdentityID) != identityID:
		return Deletion{}, auth.ErrOTPChallengeTargetMismatch
	case challenge.VerifiedAt != nil:
		return Deletion{}, auth.ErrChallengeUsed
	case challenge.CancelledAt != nil:
		return Deletion{}, auth.ErrChallengeCancelled
	case !now.Before(challenge.ExpiresAt):
		return Deletion{}, auth.ErrChallengeExpired
	case challenge.FailedAttempts >= challenge.MaxAttempts:
		return Deletion{}, auth.ErrChallengeAttemptsExceeded
	}

	matches, err := s.otp.Hasher.Compare(challenge.CodeHash, challenge.ID, strings.TrimSpace(request.Code))
	if err != nil {
		return Deletion{}, fmt.Errorf("compare the deletion code: %w", err)
	}

	if !matches {
		if err := s.otp.Challenges.RecordFailedAttempt(ctx, challenge.ID, now); err != nil {
			if errors.Is(err, auth.ErrChallengeAttemptsExceeded) {
				return Deletion{}, auth.ErrChallengeAttemptsExceeded
			}

			return Deletion{}, fmt.Errorf("record a wrong deletion code: %w", err)
		}

		return Deletion{}, auth.ErrInvalidOTP
	}

	if _, err := s.eligible(ctx, identityID, auth.OTPDeliveryChannelAuto); err != nil &&
		!errors.Is(err, auth.ErrOTPDeliveryChannelUnavailable) {
		return Deletion{}, err
	}

	standing, err := s.mustHaveNoBlockers(ctx, identityID)
	if err != nil {
		return Deletion{}, err
	}

	if len(standing.Balances) > 0 && !request.AcceptBalanceLoss {
		return Deletion{}, ErrBalanceNotAccepted
	}

	return s.store.Confirm(ctx, ConfirmInput{
		ChallengeID:         challenge.ID,
		IdentityID:          identityID,
		VerifiedAt:          now,
		RiderID:             standing.RiderID,
		DriverID:            standing.DriverID,
		PurgeAfter:          now.Add(s.settings.GracePeriod),
		BalanceLossAccepted: len(standing.Balances) > 0,
	})
}
