package token

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type sessionAccessRevocationStore interface {
	IsRevoked(
		ctx context.Context,
		sessionID string,
	) (bool, error)

	MarkRevoked(
		ctx context.Context,
		sessionID string,
		ttl time.Duration,
	) error
}

type SessionAccessChecker struct {
	revocationStore sessionAccessRevocationStore
	stateStore      auth.SessionAccessStateStore
	clock           auth.Clock
}

var _ SessionAccessRevocationChecker = (*SessionAccessChecker)(nil)

func NewSessionAccessChecker(
	revocationStore sessionAccessRevocationStore,
	stateStore auth.SessionAccessStateStore,
	clock auth.Clock,
) (*SessionAccessChecker, error) {
	if revocationStore == nil {
		return nil, errors.New(
			"session access revocation store is required",
		)
	}

	if stateStore == nil {
		return nil, errors.New(
			"session access state store is required",
		)
	}

	if clock == nil {
		return nil, errors.New(
			"clock is required",
		)
	}

	return &SessionAccessChecker{
		revocationStore: revocationStore,
		stateStore:      stateStore,
		clock:           clock,
	}, nil
}

func (c *SessionAccessChecker) IsRevoked(
	ctx context.Context,
	sessionID string,
) (bool, error) {
	if sessionID == "" {
		return false, errors.New(
			"session ID cannot be empty",
		)
	}

	revoked, err := c.revocationStore.IsRevoked(
		ctx,
		sessionID,
	)
	if err != nil {
		return false, fmt.Errorf(
			"check cached session revocation: %w",
			err,
		)
	}

	if revoked {
		return true, nil
	}

	state, found, err :=
		c.stateStore.FindSessionAccessState(
			ctx,
			sessionID,
		)
	if err != nil {
		return false, fmt.Errorf(
			"load session access state: %w",
			err,
		)
	}

	if !found {
		return true, nil
	}

	now := c.clock.Now().UTC()

	if state.Revoked {
		c.rebuildRevocationMarker(
			ctx,
			sessionID,
			state.SessionExpiresAt,
			now,
		)

		return true, nil
	}

	if !state.SessionExpiresAt.After(now) {
		return true, nil
	}

	return false, nil
}

func (c *SessionAccessChecker) rebuildRevocationMarker(
	ctx context.Context,
	sessionID string,
	sessionExpiresAt time.Time,
	now time.Time,
) {
	sessionExpiresAt =
		sessionExpiresAt.UTC()

	ttl := sessionExpiresAt.Sub(now)

	if ttl <= 0 {
		return
	}

	_ = c.revocationStore.MarkRevoked(
		ctx,
		sessionID,
		ttl,
	)
}
