//go:build integration

package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestRefreshTokenRotationStoreRotatesAndDetectsReuse(
	t *testing.T,
) {
	fixture := newRefreshTokenRotationIntegrationFixture(
		t,
		"+9647500000050",
	)

	cleanupIdentityRefreshTokenReuseDetectedOutboxEvents(
		t,
		fixture,
	)

	currentTokenHash := strings.Repeat(
		"a",
		64,
	)

	replacementTokenHash := strings.Repeat(
		"b",
		64,
	)

	currentTokenExpiresAt := fixture.now.Add(
		29 * 24 * time.Hour,
	)

	currentTokenID := fixture.createRefreshToken(
		currentTokenHash,
		currentTokenExpiresAt,
	)

	store := NewRefreshTokenRotationStore(
		fixture.pool,
	)

	refreshContext, err := store.Inspect(
		fixture.ctx,
		currentTokenHash,
		fixture.now.Add(time.Minute),
	)
	if err != nil {
		t.Fatalf(
			"Inspect() returned an error: %v",
			err,
		)
	}

	if refreshContext.IdentityID != fixture.identityID {
		t.Fatalf(
			"IdentityID is %q, expected %q",
			refreshContext.IdentityID,
			fixture.identityID,
		)
	}

	if refreshContext.SessionID != fixture.sessionID {
		t.Fatalf(
			"SessionID is %q, expected %q",
			refreshContext.SessionID,
			fixture.sessionID,
		)
	}

	rotatedAt := fixture.now.Add(
		2 * time.Minute,
	)

	replacementExpiresAt := rotatedAt.Add(
		29 * 24 * time.Hour,
	)

	if replacementExpiresAt.After(
		fixture.sessionExpiresAt,
	) {
		replacementExpiresAt =
			fixture.sessionExpiresAt
	}

	err = store.Rotate(
		fixture.ctx,
		auth.RefreshTokenRotationInput{
			CurrentTokenHash:     currentTokenHash,
			ReplacementTokenHash: replacementTokenHash,
			RotatedAt:            rotatedAt,
			ReplacementExpiresAt: replacementExpiresAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"Rotate() returned an error: %v",
			err,
		)
	}

	var (
		currentUsedAt        *time.Time
		replacedByTokenID    *string
		currentRevokedAt     *time.Time
		replacementTokenID   string
		replacementUsedAt    *time.Time
		replacementRevokedAt *time.Time
	)

	err = fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT
				current_rt.used_at,
				current_rt.replaced_by_token_id::text,
				current_rt.revoked_at,
				replacement_rt.id::text,
				replacement_rt.used_at,
				replacement_rt.revoked_at
			FROM refresh_tokens AS current_rt
			INNER JOIN refresh_tokens AS replacement_rt
				ON replacement_rt.id =
					current_rt.replaced_by_token_id
			WHERE current_rt.id = $1::uuid
		`,
		currentTokenID,
	).Scan(
		&currentUsedAt,
		&replacedByTokenID,
		&currentRevokedAt,
		&replacementTokenID,
		&replacementUsedAt,
		&replacementRevokedAt,
	)
	if err != nil {
		t.Fatalf(
			"query rotated refresh tokens: %v",
			err,
		)
	}

	if currentUsedAt == nil {
		t.Fatal(
			"current refresh token was not marked used",
		)
	}

	if replacedByTokenID == nil {
		t.Fatal(
			"current refresh token has no replacement token ID",
		)
	}

	if *replacedByTokenID != replacementTokenID {
		t.Fatalf(
			"replacement token link is %q, expected %q",
			*replacedByTokenID,
			replacementTokenID,
		)
	}

	if currentRevokedAt != nil {
		t.Fatal(
			"current refresh token was unexpectedly revoked during normal rotation",
		)
	}

	if replacementUsedAt != nil {
		t.Fatal(
			"replacement refresh token is unexpectedly already used",
		)
	}

	if replacementRevokedAt != nil {
		t.Fatal(
			"replacement refresh token is unexpectedly revoked",
		)
	}

	replacementContext, err := store.Inspect(
		fixture.ctx,
		replacementTokenHash,
		rotatedAt.Add(time.Second),
	)
	if err != nil {
		t.Fatalf(
			"Inspect() replacement token returned an error: %v",
			err,
		)
	}

	if replacementContext.IdentityID != fixture.identityID {
		t.Fatalf(
			"replacement token IdentityID is %q, expected %q",
			replacementContext.IdentityID,
			fixture.identityID,
		)
	}

	reuseDetectedAt := rotatedAt.Add(
		2 * time.Second,
	)

	_, err = store.Inspect(
		fixture.ctx,
		currentTokenHash,
		reuseDetectedAt,
	)

	if !errors.Is(
		err,
		auth.ErrRefreshTokenReused,
	) {
		t.Fatalf(
			"reused current token returned %v, expected %v",
			err,
			auth.ErrRefreshTokenReused,
		)
	}

	if fixture.sessionRevokedAt() == nil {
		t.Fatal(
			"session was not revoked after refresh token reuse",
		)
	}

	activeRefreshTokenCount :=
		fixture.activeRefreshTokenCount()

	if activeRefreshTokenCount != 0 {
		t.Fatalf(
			"active refresh tokens after reuse = %d, expected 0",
			activeRefreshTokenCount,
		)
	}

	var storedReuseDetectedAt *time.Time

	err = fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT reuse_detected_at
			FROM refresh_tokens
			WHERE id = $1::uuid
		`,
		currentTokenID,
	).Scan(
		&storedReuseDetectedAt,
	)
	if err != nil {
		t.Fatalf(
			"query refresh token reuse detection marker: %v",
			err,
		)
	}

	if storedReuseDetectedAt == nil {
		t.Fatal(
			"refresh token reuse detection marker was not stored",
		)
	}

	if !storedReuseDetectedAt.Equal(
		reuseDetectedAt,
	) {
		t.Fatalf(
			"reuse detected at = %v, want %v",
			storedReuseDetectedAt,
			reuseDetectedAt,
		)
	}

	assertIdentityRefreshTokenReuseDetectedOutboxEvent(
		t,
		fixture,
		reuseDetectedAt,
	)

	eventCount :=
		countIdentityRefreshTokenReuseDetectedOutboxEvents(
			t,
			fixture,
		)

	if eventCount != 1 {
		t.Fatalf(
			"refresh token reuse detected outbox event count = %d, want 1",
			eventCount,
		)
	}

	secondReuseDetectedAt := reuseDetectedAt.Add(
		time.Second,
	)

	_, err = store.Inspect(
		fixture.ctx,
		currentTokenHash,
		secondReuseDetectedAt,
	)

	if !errors.Is(
		err,
		auth.ErrRefreshTokenReused,
	) && !errors.Is(
		err,
		auth.ErrSessionRevoked,
	) {
		t.Fatalf(
			"repeated reused token returned %v, expected reuse or revoked-session error",
			err,
		)
	}

	eventCount =
		countIdentityRefreshTokenReuseDetectedOutboxEvents(
			t,
			fixture,
		)

	if eventCount != 1 {
		t.Fatalf(
			"refresh token reuse detected outbox event count after repeated reuse = %d, want 1",
			eventCount,
		)
	}

	var reuseDetectedAtAfterRepeat *time.Time

	err = fixture.pool.QueryRow(
		fixture.ctx,
		`
		SELECT reuse_detected_at
		FROM refresh_tokens
		WHERE id = $1::uuid
	`,
		currentTokenID,
	).Scan(
		&reuseDetectedAtAfterRepeat,
	)
	if err != nil {
		t.Fatalf(
			"query reuse detection marker after repeated reuse: %v",
			err,
		)
	}

	if reuseDetectedAtAfterRepeat == nil {
		t.Fatal(
			"refresh token reuse detection marker disappeared",
		)
	}

	if !reuseDetectedAtAfterRepeat.Equal(
		reuseDetectedAt,
	) {
		t.Fatalf(
			"reuse detected at after repeated reuse = %v, want original %v",
			reuseDetectedAtAfterRepeat,
			reuseDetectedAt,
		)
	}
}

func assertIdentityRefreshTokenReuseDetectedOutboxEvent(
	t *testing.T,
	fixture *refreshTokenRotationIntegrationFixture,
	occurredAt time.Time,
) {
	t.Helper()

	var aggregateType string
	var aggregateID string
	var eventType string
	var schemaVersion int16
	var payloadIdentityID string
	var payloadSessionID string
	var storedOccurredAt time.Time
	var published bool
	var publishAttempts int

	err := fixture.pool.QueryRow(
		fixture.ctx,
		`
			SELECT
				aggregate_type,
				aggregate_id::text,
				event_type,
				schema_version,
				payload ->> 'identity_id',
				payload ->> 'session_id',
				occurred_at,
				published_at IS NOT NULL,
				publish_attempts
			FROM outbox_events
			WHERE aggregate_type = $1
			  AND aggregate_id = $2::uuid
			  AND event_type = $3
		`,
		identityOutboxAggregateType,
		fixture.identityID,
		string(
			auth.IdentityDomainEventRefreshTokenReuseDetected,
		),
	).Scan(
		&aggregateType,
		&aggregateID,
		&eventType,
		&schemaVersion,
		&payloadIdentityID,
		&payloadSessionID,
		&storedOccurredAt,
		&published,
		&publishAttempts,
	)
	if err != nil {
		t.Fatalf(
			"query refresh token reuse detected outbox event: %v",
			err,
		)
	}

	if aggregateType != identityOutboxAggregateType {
		t.Fatalf(
			"aggregate type = %q, want %q",
			aggregateType,
			identityOutboxAggregateType,
		)
	}

	if aggregateID != fixture.identityID {
		t.Fatalf(
			"aggregate ID = %q, want %q",
			aggregateID,
			fixture.identityID,
		)
	}

	if eventType != string(
		auth.IdentityDomainEventRefreshTokenReuseDetected,
	) {
		t.Fatalf(
			"event type = %q, want %q",
			eventType,
			auth.IdentityDomainEventRefreshTokenReuseDetected,
		)
	}

	if schemaVersion !=
		auth.IdentityDomainEventSchemaVersion {
		t.Fatalf(
			"schema version = %d, want %d",
			schemaVersion,
			auth.IdentityDomainEventSchemaVersion,
		)
	}

	if payloadIdentityID != fixture.identityID {
		t.Fatalf(
			"payload identity ID = %q, want %q",
			payloadIdentityID,
			fixture.identityID,
		)
	}

	if payloadSessionID != fixture.sessionID {
		t.Fatalf(
			"payload session ID = %q, want %q",
			payloadSessionID,
			fixture.sessionID,
		)
	}

	if !storedOccurredAt.Equal(occurredAt) {
		t.Fatalf(
			"occurred at = %v, want %v",
			storedOccurredAt,
			occurredAt,
		)
	}

	if published {
		t.Fatal(
			"refresh token reuse detected outbox event is already published",
		)
	}

	if publishAttempts != 0 {
		t.Fatalf(
			"publish attempts = %d, want 0",
			publishAttempts,
		)
	}
}

func countIdentityRefreshTokenReuseDetectedOutboxEvents(
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
			auth.IdentityDomainEventRefreshTokenReuseDetected,
		),
	).Scan(
		&count,
	)
	if err != nil {
		t.Fatalf(
			"count refresh token reuse detected outbox events: %v",
			err,
		)
	}

	return count
}

func cleanupIdentityRefreshTokenReuseDetectedOutboxEvents(
	t *testing.T,
	fixture *refreshTokenRotationIntegrationFixture,
) {
	t.Helper()

	t.Cleanup(func() {
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
				auth.IdentityDomainEventRefreshTokenReuseDetected,
			),
		)
		if err != nil {
			t.Errorf(
				"clean refresh token reuse detected outbox events: %v",
				err,
			)
		}
	})
}
