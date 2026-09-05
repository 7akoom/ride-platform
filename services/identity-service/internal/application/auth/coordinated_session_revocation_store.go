package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type CoordinatedSessionRevocationStore struct {
	targetStore           SessionRevocationTargetStore
	accessRevocationStore SessionAccessRevocationStore
	persistentStore       PersistentSessionRevocationStore
}

var _ SessionRevocationStore = (*CoordinatedSessionRevocationStore)(nil)

func NewCoordinatedSessionRevocationStore(
	targetStore SessionRevocationTargetStore,
	accessRevocationStore SessionAccessRevocationStore,
	persistentStore PersistentSessionRevocationStore,
) (*CoordinatedSessionRevocationStore, error) {
	if targetStore == nil {
		return nil, errors.New(
			"session revocation target store is required",
		)
	}

	if accessRevocationStore == nil {
		return nil, errors.New(
			"session access revocation store is required",
		)
	}

	if persistentStore == nil {
		return nil, errors.New(
			"persistent session revocation store is required",
		)
	}

	return &CoordinatedSessionRevocationStore{
		targetStore:           targetStore,
		accessRevocationStore: accessRevocationStore,
		persistentStore:       persistentStore,
	}, nil
}

func (s *CoordinatedSessionRevocationStore) RevokeByRefreshTokenHash(
	ctx context.Context,
	refreshTokenHash string,
	revokedAt time.Time,
) error {
	if refreshTokenHash == "" {
		return ErrInvalidRefreshToken
	}

	if revokedAt.IsZero() {
		return errors.New(
			"session revocation time cannot be zero",
		)
	}

	revokedAt = revokedAt.UTC()

	target, found, err :=
		s.targetStore.FindRevocationTargetByRefreshTokenHash(
			ctx,
			refreshTokenHash,
		)
	if err != nil {
		return fmt.Errorf(
			"find session revocation target: %w",
			err,
		)
	}

	if !found {
		return nil
	}

	if target.SessionID == "" {
		return errors.New(
			"session revocation target has empty session ID",
		)
	}

	if target.SessionExpiresAt.IsZero() {
		return errors.New(
			"session revocation target has zero expiration",
		)
	}

	markerExpiresAt := target.SessionExpiresAt.UTC()

	revocationTTL := markerExpiresAt.Sub(
		revokedAt,
	)

	if revocationTTL > 0 {
		if err := s.accessRevocationStore.MarkRevoked(
			ctx,
			target.SessionID,
			revocationTTL,
		); err != nil {
			return fmt.Errorf(
				"mark session access revoked: %w",
				err,
			)
		}
	}

	if err := s.persistentStore.RevokeSessionByRefreshTokenHash(
		ctx,
		refreshTokenHash,
		revokedAt,
	); err != nil {
		return fmt.Errorf(
			"persist session revocation: %w",
			err,
		)
	}

	return nil
}
