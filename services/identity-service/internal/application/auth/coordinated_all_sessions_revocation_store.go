package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type CoordinatedAllSessionsRevocationStore struct {
	targetStore           AllSessionsRevocationTargetStore
	accessRevocationStore SessionAccessRevocationStore
	persistentStore       AllSessionsPersistentRevocationStore
}

var _ AllSessionsRevocationStore = (*CoordinatedAllSessionsRevocationStore)(nil)

func NewCoordinatedAllSessionsRevocationStore(
	targetStore AllSessionsRevocationTargetStore,
	accessRevocationStore SessionAccessRevocationStore,
	persistentStore AllSessionsPersistentRevocationStore,
) (*CoordinatedAllSessionsRevocationStore, error) {
	if targetStore == nil {
		return nil, errors.New(
			"all sessions revocation target store is required",
		)
	}

	if accessRevocationStore == nil {
		return nil, errors.New(
			"session access revocation store is required",
		)
	}

	if persistentStore == nil {
		return nil, errors.New(
			"all sessions persistent revocation store is required",
		)
	}

	return &CoordinatedAllSessionsRevocationStore{
		targetStore:           targetStore,
		accessRevocationStore: accessRevocationStore,
		persistentStore:       persistentStore,
	}, nil
}

func (s *CoordinatedAllSessionsRevocationStore) RevokeAllByRefreshTokenHash(
	ctx context.Context,
	refreshTokenHash string,
	revokedAt time.Time,
) error {
	if refreshTokenHash == "" {
		return ErrInvalidRefreshToken
	}

	if revokedAt.IsZero() {
		return errors.New(
			"all sessions revocation time cannot be zero",
		)
	}

	revokedAt = revokedAt.UTC()

	target, found, err :=
		s.targetStore.FindAllSessionRevocationTargetsByRefreshTokenHash(
			ctx,
			refreshTokenHash,
			revokedAt,
		)
	if err != nil {
		return fmt.Errorf(
			"find all sessions revocation targets: %w",
			err,
		)
	}

	if !found {
		return nil
	}

	if target.IdentityID == "" {
		return errors.New(
			"all sessions revocation target has empty identity ID",
		)
	}

	if len(target.Sessions) == 0 {
		return errors.New(
			"all sessions revocation target has no sessions",
		)
	}

	sessionIDs := make(
		[]string,
		0,
		len(target.Sessions),
	)

	for _, session := range target.Sessions {
		if session.SessionID == "" {
			return errors.New(
				"all sessions revocation target has empty session ID",
			)
		}

		if session.SessionExpiresAt.IsZero() {
			return errors.New(
				"all sessions revocation target has zero session expiration",
			)
		}

		revocationTTL := session.SessionExpiresAt.
			UTC().
			Sub(revokedAt)

		if revocationTTL > 0 {
			if err := s.accessRevocationStore.MarkRevoked(
				ctx,
				session.SessionID,
				revocationTTL,
			); err != nil {
				return fmt.Errorf(
					"mark session %q access revoked: %w",
					session.SessionID,
					err,
				)
			}
		}

		sessionIDs = append(
			sessionIDs,
			session.SessionID,
		)
	}

	if err := s.persistentStore.RevokeSessions(
		ctx,
		target.IdentityID,
		sessionIDs,
		revokedAt,
	); err != nil {
		return fmt.Errorf(
			"persist all sessions revocation: %w",
			err,
		)
	}

	return nil
}
