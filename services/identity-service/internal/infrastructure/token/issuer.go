package token

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type sessionIDGenerator interface {
	Generate() (string, error)
}

type refreshTokenGenerator interface {
	Generate() (string, error)
}

type accessTokenSigner interface {
	Issue(
		identityID string,
		sessionID string,
		issuedAt time.Time,
	) (string, int32, error)
}

type sessionStore interface {
	Create(
		ctx context.Context,
		sessionID string,
		identityID string,
		sessionExpiresAt time.Time,
		refreshTokenHash string,
		refreshTokenExpiresAt time.Time,
	) (IssuedSession, error)
}

type Issuer struct {
	sessionIDGenerator    sessionIDGenerator
	refreshTokenGenerator refreshTokenGenerator
	accessTokenSigner     accessTokenSigner
	sessionStore          sessionStore
	clock                 auth.Clock
	sessionTTL            time.Duration
	refreshTokenTTL       time.Duration
}

var _ auth.TokenIssuer = (*Issuer)(nil)

func NewIssuer(
	sessionIDGenerator sessionIDGenerator,
	refreshTokenGenerator refreshTokenGenerator,
	accessTokenSigner accessTokenSigner,
	sessionStore sessionStore,
	clock auth.Clock,
	sessionTTL time.Duration,
	refreshTokenTTL time.Duration,
) (*Issuer, error) {
	if sessionIDGenerator == nil {
		return nil, errors.New("session ID generator is required")
	}

	if refreshTokenGenerator == nil {
		return nil, errors.New("refresh token generator is required")
	}

	if accessTokenSigner == nil {
		return nil, errors.New("access token signer is required")
	}

	if sessionStore == nil {
		return nil, errors.New("session store is required")
	}

	if clock == nil {
		return nil, errors.New("clock is required")
	}

	if sessionTTL <= 0 {
		return nil, errors.New("session TTL must be greater than zero")
	}

	if refreshTokenTTL <= 0 {
		return nil, errors.New(
			"refresh token TTL must be greater than zero",
		)
	}

	if refreshTokenTTL > sessionTTL {
		return nil, errors.New(
			"refresh token TTL cannot exceed session TTL",
		)
	}

	return &Issuer{
		sessionIDGenerator:    sessionIDGenerator,
		refreshTokenGenerator: refreshTokenGenerator,
		accessTokenSigner:     accessTokenSigner,
		sessionStore:          sessionStore,
		clock:                 clock,
		sessionTTL:            sessionTTL,
		refreshTokenTTL:       refreshTokenTTL,
	}, nil
}

func (i *Issuer) Issue(
	ctx context.Context,
	identity auth.Identity,
) (auth.TokenPair, error) {
	if identity.ID == "" {
		return auth.TokenPair{}, errors.New(
			"identity ID cannot be empty",
		)
	}

	sessionID, err := i.sessionIDGenerator.Generate()
	if err != nil {
		return auth.TokenPair{}, fmt.Errorf(
			"generate session ID: %w",
			err,
		)
	}

	refreshToken, err := i.refreshTokenGenerator.Generate()
	if err != nil {
		return auth.TokenPair{}, fmt.Errorf(
			"generate refresh token: %w",
			err,
		)
	}

	refreshTokenHash := HashRefreshToken(refreshToken)

	now := i.clock.Now().UTC()

	sessionExpiresAt := now.Add(i.sessionTTL)
	refreshTokenExpiresAt := now.Add(i.refreshTokenTTL)

	accessToken, accessTokenExpiresInSeconds, err :=
		i.accessTokenSigner.Issue(
			identity.ID,
			sessionID,
			now,
		)
	if err != nil {
		return auth.TokenPair{}, fmt.Errorf(
			"issue access token: %w",
			err,
		)
	}

	if _, err := i.sessionStore.Create(
		ctx,
		sessionID,
		identity.ID,
		sessionExpiresAt,
		refreshTokenHash,
		refreshTokenExpiresAt,
	); err != nil {
		return auth.TokenPair{}, fmt.Errorf(
			"persist authentication session: %w",
			err,
		)
	}

	return auth.TokenPair{
		AccessToken:                 accessToken,
		RefreshToken:                refreshToken,
		AccessTokenExpiresInSeconds: accessTokenExpiresInSeconds,
	}, nil
}