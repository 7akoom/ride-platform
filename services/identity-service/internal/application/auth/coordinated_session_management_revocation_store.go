package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type CoordinatedSessionManagementRevocationStore struct {
	targetStore           SessionManagementRevocationTargetStore
	accessRevocationStore SessionAccessRevocationStore
	persistentStore       SingleSessionPersistentRevocationStore
}

var _ SessionManagementRevocationStore = (*CoordinatedSessionManagementRevocationStore)(nil)

func NewCoordinatedSessionManagementRevocationStore(
	targetStore SessionManagementRevocationTargetStore,
	accessRevocationStore SessionAccessRevocationStore,
	persistentStore SingleSessionPersistentRevocationStore,
) (*CoordinatedSessionManagementRevocationStore, error) {
	if targetStore == nil {
		return nil, errors.New(
			"session management revocation target store is required",
		)
	}

	if accessRevocationStore == nil {
		return nil, errors.New(
			"session access revocation store is required",
		)
	}

	if persistentStore == nil {
		return nil, errors.New(
			"persistent session management revocation store is required",
		)
	}

	return &CoordinatedSessionManagementRevocationStore{
		targetStore:           targetStore,
		accessRevocationStore: accessRevocationStore,
		persistentStore:       persistentStore,
	}, nil
}

func (s *CoordinatedSessionManagementRevocationStore) RevokeSession(
	ctx context.Context,
	identityID string,
	sessionID string,
	revokedAt time.Time,
) error {
	identityID = strings.TrimSpace(identityID)
	sessionID = strings.TrimSpace(sessionID)

	if identityID == "" {
		return errors.New(
			"identity ID cannot be blank",
		)
	}

	if sessionID == "" {
		return errors.New(
			"session ID cannot be blank",
		)
	}

	if revokedAt.IsZero() {
		return errors.New(
			"session revocation time cannot be zero",
		)
	}

	revokedAt = revokedAt.UTC()

	target, found, err :=
		s.targetStore.FindRevocationTargetByIdentityAndSessionID(
			ctx,
			identityID,
			sessionID,
		)
	if err != nil {
		return fmt.Errorf(
			"find session management revocation target: %w",
			err,
		)
	}

	if !found {
		return ErrSessionNotFound
	}

	if target.SessionID == "" {
		return errors.New(
			"session management revocation target has empty session ID",
		)
	}

	if target.SessionID != sessionID {
		return errors.New(
			"session management revocation target ID mismatch",
		)
	}

	if target.SessionExpiresAt.IsZero() {
		return errors.New(
			"session management revocation target has zero expiration",
		)
	}

	revocationTTL := target.SessionExpiresAt.
		UTC().
		Sub(revokedAt)

	if revocationTTL > 0 {
		if err := s.accessRevocationStore.MarkRevoked(
			ctx,
			sessionID,
			revocationTTL,
		); err != nil {
			return fmt.Errorf(
				"mark managed session access revoked: %w",
				err,
			)
		}
	}

	if err := s.persistentStore.RevokeSession(
		ctx,
		identityID,
		sessionID,
		revokedAt,
	); err != nil {
		return fmt.Errorf(
			"persist managed session revocation: %w",
			err,
		)
	}

	return nil
}
