//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdentifierLinkCompletionStoreLinksIdentifierAndConsumesChallenge(
	t *testing.T,
) {
	ctx, pool, store, challengeRepository :=
		newIdentifierLinkCompletionIntegrationTest(t)

	identityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	const challengeID = "otp_ch_link_completion_success"

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "Link.Success@EXAMPLE.COM",
	}

	challenge := auth.OTPChallenge{
		ID:               challengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "link-completion-success-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := challengeRepository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"create link identifier challenge: %v",
			err,
		)
	}

	verifiedAt := time.Now().UTC()

	err := store.Complete(
		ctx,
		auth.IdentifierLinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			Identifier:  identifier,
			VerifiedAt:  verifiedAt,
		},
	)
	if err != nil {
		t.Fatalf(
			"Complete() returned an error: %v",
			err,
		)
	}

	var storedIdentityID string
	var normalizedValue string
	var identifierVerifiedAt time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				identity_id::text,
				normalized_value,
				verified_at
			FROM identity_identifiers
			WHERE identifier_type = $1
			  AND normalized_value = $2
		`,
		string(auth.IdentifierTypeEmail),
		"link.success@example.com",
	).Scan(
		&storedIdentityID,
		&normalizedValue,
		&identifierVerifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"query linked identifier: %v",
			err,
		)
	}

	if storedIdentityID != identityID {
		t.Fatalf(
			"linked identity ID = %q, want %q",
			storedIdentityID,
			identityID,
		)
	}

	if normalizedValue != "link.success@example.com" {
		t.Fatalf(
			"normalized identifier = %q, want %q",
			normalizedValue,
			"link.success@example.com",
		)
	}

	var challengeVerifiedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT verified_at
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&challengeVerifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"query completed challenge: %v",
			err,
		)
	}

	if challengeVerifiedAt == nil {
		t.Fatal(
			"identifier was linked but challenge was not consumed",
		)
	}

	if identifierVerifiedAt.IsZero() {
		t.Fatal(
			"linked identifier has zero verification time",
		)
	}
}

func TestIdentifierLinkCompletionStoreOwnershipConflictDoesNotConsumeChallenge(
	t *testing.T,
) {
	ctx, pool, store, challengeRepository :=
		newIdentifierLinkCompletionIntegrationTest(t)

	ownerIdentityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	targetIdentityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "owned@example.com",
	}

	_, err := pool.Exec(
		ctx,
		`
			INSERT INTO identity_identifiers (
				identity_id,
				identifier_type,
				normalized_value,
				verified_at
			)
			VALUES ($1, $2, $3, $4)
		`,
		ownerIdentityID,
		string(identifier.Type),
		identifier.Value,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf(
			"seed existing identifier owner: %v",
			err,
		)
	}

	const challengeID = "otp_ch_link_completion_conflict"

	challenge := auth.OTPChallenge{
		ID:               challengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &targetIdentityID,
		CodeHash:         "link-completion-conflict-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := challengeRepository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"create conflicting link challenge: %v",
			err,
		)
	}

	err = store.Complete(
		ctx,
		auth.IdentifierLinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  targetIdentityID,
			Identifier:  identifier,
			VerifiedAt:  time.Now().UTC(),
		},
	)

	if !errors.Is(
		err,
		auth.ErrIdentifierAlreadyLinked,
	) {
		t.Fatalf(
			"Complete() error = %v, want %v",
			err,
			auth.ErrIdentifierAlreadyLinked,
		)
	}

	var storedOwnerIdentityID string

	err = pool.QueryRow(
		ctx,
		`
			SELECT identity_id::text
			FROM identity_identifiers
			WHERE identifier_type = $1
			  AND normalized_value = $2
		`,
		string(identifier.Type),
		identifier.Value,
	).Scan(
		&storedOwnerIdentityID,
	)
	if err != nil {
		t.Fatalf(
			"query existing identifier owner: %v",
			err,
		)
	}

	if storedOwnerIdentityID != ownerIdentityID {
		t.Fatalf(
			"identifier owner changed to %q, want %q",
			storedOwnerIdentityID,
			ownerIdentityID,
		)
	}

	var challengeVerifiedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT verified_at
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&challengeVerifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"query conflicting challenge state: %v",
			err,
		)
	}

	if challengeVerifiedAt != nil {
		t.Fatal(
			"ownership conflict consumed the OTP challenge",
		)
	}
}

func TestIdentifierLinkCompletionStoreAllowsExistingLinkForSameIdentity(
	t *testing.T,
) {
	ctx, pool, store, challengeRepository :=
		newIdentifierLinkCompletionIntegrationTest(t)

	identityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000088",
	}

	_, err := pool.Exec(
		ctx,
		`
			INSERT INTO identity_identifiers (
				identity_id,
				identifier_type,
				normalized_value,
				verified_at
			)
			VALUES ($1, $2, $3, $4)
		`,
		identityID,
		string(identifier.Type),
		identifier.Value,
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatalf(
			"seed identifier already linked to same identity: %v",
			err,
		)
	}

	const challengeID = "otp_ch_link_completion_idempotent"

	challenge := auth.OTPChallenge{
		ID:               challengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "link-completion-idempotent-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := challengeRepository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"create idempotent link challenge: %v",
			err,
		)
	}

	err = store.Complete(
		ctx,
		auth.IdentifierLinkCompletionInput{
			ChallengeID: challengeID,
			IdentityID:  identityID,
			Identifier:  identifier,
			VerifiedAt:  time.Now().UTC(),
		},
	)
	if err != nil {
		t.Fatalf(
			"Complete() for existing same-identity link returned: %v",
			err,
		)
	}

	var identifierCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identity_identifiers
			WHERE identifier_type = $1
			  AND normalized_value = $2
		`,
		string(identifier.Type),
		identifier.Value,
	).Scan(
		&identifierCount,
	)
	if err != nil {
		t.Fatalf(
			"count identifier rows: %v",
			err,
		)
	}

	if identifierCount != 1 {
		t.Fatalf(
			"identifier row count = %d, want 1",
			identifierCount,
		)
	}

	var challengeVerifiedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT verified_at
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&challengeVerifiedAt,
	)
	if err != nil {
		t.Fatalf(
			"query idempotent challenge state: %v",
			err,
		)
	}

	if challengeVerifiedAt == nil {
		t.Fatal(
			"idempotent link did not consume its OTP challenge",
		)
	}
}

func TestIdentifierLinkCompletionStoreSerializesCompetingOwners(
	t *testing.T,
) {
	ctx, pool, store, challengeRepository :=
		newIdentifierLinkCompletionIntegrationTest(t)

	firstIdentityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	secondIdentityID := createIdentifierLinkTestIdentity(
		t,
		ctx,
		pool,
	)

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "race@example.com",
	}

	const firstChallengeID = "otp_ch_link_completion_race_first"
	const secondChallengeID = "otp_ch_link_completion_race_second"

	firstChallenge := auth.OTPChallenge{
		ID:               firstChallengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &firstIdentityID,
		CodeHash:         "first-race-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	secondChallenge := auth.OTPChallenge{
		ID:               secondChallengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &secondIdentityID,
		CodeHash:         "second-race-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	for _, challenge := range []auth.OTPChallenge{
		firstChallenge,
		secondChallenge,
	} {
		if err := challengeRepository.Create(
			ctx,
			challenge,
		); err != nil {
			t.Fatalf(
				"create competing challenge %q: %v",
				challenge.ID,
				err,
			)
		}
	}

	start := make(chan struct{})
	results := make(chan error, 2)

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)

	runComplete := func(
		challengeID string,
		identityID string,
	) {
		defer waitGroup.Done()

		<-start

		results <- store.Complete(
			context.Background(),
			auth.IdentifierLinkCompletionInput{
				ChallengeID: challengeID,
				IdentityID:  identityID,
				Identifier:  identifier,
				VerifiedAt:  time.Now().UTC(),
			},
		)
	}

	go runComplete(
		firstChallengeID,
		firstIdentityID,
	)

	go runComplete(
		secondChallengeID,
		secondIdentityID,
	)

	close(start)

	waitGroup.Wait()
	close(results)

	var successCount int
	var conflictCount int

	for completeErr := range results {
		switch {
		case completeErr == nil:
			successCount++

		case errors.Is(
			completeErr,
			auth.ErrIdentifierAlreadyLinked,
		):
			conflictCount++

		default:
			t.Fatalf(
				"unexpected concurrent Complete() error: %v",
				completeErr,
			)
		}
	}

	if successCount != 1 {
		t.Fatalf(
			"successful completions = %d, want 1",
			successCount,
		)
	}

	if conflictCount != 1 {
		t.Fatalf(
			"ownership conflicts = %d, want 1",
			conflictCount,
		)
	}

	var identifierCount int

	err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identity_identifiers
			WHERE identifier_type = $1
			  AND normalized_value = $2
		`,
		string(identifier.Type),
		identifier.Value,
	).Scan(
		&identifierCount,
	)
	if err != nil {
		t.Fatalf(
			"count competing identifier rows: %v",
			err,
		)
	}

	if identifierCount != 1 {
		t.Fatalf(
			"identifier row count after race = %d, want 1",
			identifierCount,
		)
	}

	var verifiedChallenges int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_challenges
			WHERE id IN ($1, $2)
			  AND verified_at IS NOT NULL
		`,
		firstChallengeID,
		secondChallengeID,
	).Scan(
		&verifiedChallenges,
	)
	if err != nil {
		t.Fatalf(
			"count verified competing challenges: %v",
			err,
		)
	}

	if verifiedChallenges != 1 {
		t.Fatalf(
			"verified competing challenges = %d, want 1",
			verifiedChallenges,
		)
	}
}

func newIdentifierLinkCompletionIntegrationTest(
	t *testing.T,
) (
	context.Context,
	*pgxpool.Pool,
	*IdentifierLinkCompletionStore,
	*ChallengeRepository,
) {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal(
			"DATABASE_URL is required for integration test",
		)
	}

	ctx := context.Background()

	pool, err := databaseinfra.NewPostgresPool(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf(
			"connect to PostgreSQL: %v",
			err,
		)
	}

	t.Cleanup(pool.Close)

	return ctx,
		pool,
		NewIdentifierLinkCompletionStore(pool),
		NewChallengeRepository(pool)
}

func createIdentifierLinkTestIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) string {
	t.Helper()

	var identityID string

	err := pool.QueryRow(
		ctx,
		`
			INSERT INTO identities
				DEFAULT VALUES
			RETURNING id::text
		`,
	).Scan(
		&identityID,
	)
	if err != nil {
		t.Fatalf(
			"create identifier link test identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM identities
				WHERE id = $1
			`,
			identityID,
		); cleanupErr != nil {
			t.Errorf(
				"clean identifier link test identity %q: %v",
				identityID,
				cleanupErr,
			)
		}
	})

	return identityID
}
