package valkey

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	valkeygo "github.com/valkey-io/valkey-go"
)

const revokedSessionKeyPrefix = "auth:revoked-session:"

type SessionAccessRevocationStore struct {
	client valkeygo.Client
}

var _ auth.SessionAccessRevocationStore = (*SessionAccessRevocationStore)(nil)

func NewSessionAccessRevocationStore(
	client valkeygo.Client,
) *SessionAccessRevocationStore {
	if client == nil {
		panic("Valkey client is required")
	}

	return &SessionAccessRevocationStore{
		client: client,
	}
}

func (s *SessionAccessRevocationStore) MarkRevoked(
	ctx context.Context,
	sessionID string,
	ttl time.Duration,
) error {
	if sessionID == "" {
		return errors.New(
			"session ID cannot be empty",
		)
	}

	if ttl <= 0 {
		return errors.New(
			"revocation TTL must be greater than zero",
		)
	}

	key := revokedSessionKeyPrefix + sessionID

	err := s.client.Do(
		ctx,
		s.client.B().
			Set().
			Key(key).
			Value("1").
			Ex(ttl).
			Build(),
	).Error()
	if err != nil {
		return fmt.Errorf(
			"mark session access revoked: %w",
			err,
		)
	}

	return nil
}

func (s *SessionAccessRevocationStore) IsRevoked(
	ctx context.Context,
	sessionID string,
) (bool, error) {
	if sessionID == "" {
		return false, errors.New(
			"session ID cannot be empty",
		)
	}

	key := revokedSessionKeyPrefix + sessionID

	exists, err := s.client.Do(
		ctx,
		s.client.B().
			Exists().
			Key(key).
			Build(),
	).ToInt64()
	if err != nil {
		return false, fmt.Errorf(
			"check session access revocation: %w",
			err,
		)
	}

	return exists > 0, nil
}
