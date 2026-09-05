//go:build integration

package postgres

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestLogoutAndSessionManagementRevocationRaceEmitsOneSessionRevokedEvent(
	t *testing.T,
) {
	fixture := newRefreshTokenRotationIntegrationFixture(
		t,
		"+9647500000058",
	)

	refreshTokenHash := strings.Repeat(
		"f",
		64,
	)

	fixture.createRefreshToken(
		refreshTokenHash,
		fixture.now.Add(29*24*time.Hour),
	)

	cleanupSessionRevokedRaceOutboxEvents(
		t,
		fixture,
	)

	logoutStore := NewSessionRevocationStore(
		fixture.pool,
	)

	sessionManagementStore := NewAllSessionsRevocationStore(
		fixture.pool,
	)

	revokedAt := fixture.now.Add(
		time.Minute,
	)

	start := make(chan struct{})

	var (
		logoutErr            error
		sessionManagementErr error
		waitGroup            sync.WaitGroup
	)

	waitGroup.Add(2)

	go func() {
		defer waitGroup.Done()

		<-start

		logoutErr =
			logoutStore.RevokeSessionByRefreshTokenHash(
				fixture.ctx,
				refreshTokenHash,
				revokedAt,
			)
	}()

	go func() {
		defer waitGroup.Done()

		<-start

		sessionManagementErr =
			sessionManagementStore.RevokeSession(
				fixture.ctx,
				fixture.identityID,
				fixture.sessionID,
				revokedAt,
			)
	}()

	close(start)

	waitGroup.Wait()

	if logoutErr != nil {
		t.Fatalf(
			"concurrent logout revocation returned an error: %v",
			logoutErr,
		)
	}

	if sessionManagementErr != nil {
		t.Fatalf(
			"concurrent session management revocation returned an error: %v",
			sessionManagementErr,
		)
	}

	sessionRevokedAt := fixture.sessionRevokedAt()

	if sessionRevokedAt == nil {
		t.Fatal(
			"session was not revoked after concurrent revocations",
		)
	}

	if !sessionRevokedAt.Equal(revokedAt) {
		t.Fatalf(
			"session revoked at = %v, expected %v",
			sessionRevokedAt,
			revokedAt,
		)
	}

	activeRefreshTokens :=
		fixture.activeRefreshTokenCount()

	if activeRefreshTokens != 0 {
		t.Fatalf(
			"active refresh token count after concurrent revocations = %d, expected 0",
			activeRefreshTokens,
		)
	}

	eventCount :=
		countSessionRevokedRaceOutboxEvents(
			t,
			fixture,
		)

	if eventCount != 1 {
		t.Fatalf(
			"identity.session_revoked outbox event count after concurrent revocations = %d, expected 1",
			eventCount,
		)
	}
}

func countSessionRevokedRaceOutboxEvents(
	t *testing.T,
	fixture *refreshTokenRotationIntegrationFixture,
) int {
	t.Helper()

	var count int

	err := fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT COUNT(*)
			FROM outbox_events
			WHERE aggregate_type = $1
			  AND aggregate_id = $2::uuid
			  AND event_type = $3
		`,
		identityOutboxAggregateType,
		fixture.identityID,
		string(
			auth.IdentityDomainEventSessionRevoked,
		),
	).Scan(
		&count,
	)
	if err != nil {
		t.Fatalf(
			"count identity.session_revoked race outbox events: %v",
			err,
		)
	}

	return count
}

func cleanupSessionRevokedRaceOutboxEvents(
	t *testing.T,
	fixture *refreshTokenRotationIntegrationFixture,
) {
	t.Helper()

	_, err := fixture.pool.Exec(
		fixture.ctx,
		`
			DELETE FROM outbox_events
			WHERE aggregate_type = $1
			  AND aggregate_id = $2::uuid
			  AND event_type = $3
		`,
		identityOutboxAggregateType,
		fixture.identityID,
		string(
			auth.IdentityDomainEventSessionRevoked,
		),
	)
	if err != nil {
		t.Fatalf(
			"clean existing identity.session_revoked race events: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := fixture.pool.Exec(
			fixture.ctx,
			`
				DELETE FROM outbox_events
				WHERE aggregate_type = $1
				  AND aggregate_id = $2::uuid
				  AND event_type = $3
			`,
			identityOutboxAggregateType,
			fixture.identityID,
			string(
				auth.IdentityDomainEventSessionRevoked,
			),
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean identity.session_revoked race events: %v",
				cleanupErr,
			)
		}
	})
}
