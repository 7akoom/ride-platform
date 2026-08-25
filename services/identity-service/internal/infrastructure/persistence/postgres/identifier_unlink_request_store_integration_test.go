//go:build integration

package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdentifierUnlinkRequestStoreCreatesChallengeAndOperationAtomically(
	t *testing.T,
) {
	ctx, pool, store := newIdentifierUnlinkRequestIntegrationTest(t)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000301",
	}

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-request-success@example.com",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	)

	const challengeID = "otp_ch_unlink_request_success"

	challenge := auth.OTPChallenge{
		ID:               challengeID,
		Identifier:       verificationIdentifier,
		Purpose:          auth.OTPPurposeUnlinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "unlink-request-success-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	err := store.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge:        challenge,
			TargetIdentifier: targetIdentifier,
		},
	)
	if err != nil {
		t.Fatalf(
			"Create() returned an error: %v",
			err,
		)
	}

	var (
		storedVerificationType  string
		storedVerificationValue string
		storedPurpose           string
		storedTargetIdentityID  string
	)

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				identifier_type,
				normalized_value,
				purpose,
				target_identity_id::text
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&storedVerificationType,
		&storedVerificationValue,
		&storedPurpose,
		&storedTargetIdentityID,
	)
	if err != nil {
		t.Fatalf(
			"query created unlink OTP challenge: %v",
			err,
		)
	}

	if storedVerificationType != string(auth.IdentifierTypeEmail) {
		t.Fatalf(
			"verification identifier type = %q, want %q",
			storedVerificationType,
			auth.IdentifierTypeEmail,
		)
	}

	if storedVerificationValue != verificationIdentifier.Value {
		t.Fatalf(
			"verification identifier value = %q, want %q",
			storedVerificationValue,
			verificationIdentifier.Value,
		)
	}

	if storedPurpose != string(auth.OTPPurposeUnlinkIdentifier) {
		t.Fatalf(
			"OTP purpose = %q, want %q",
			storedPurpose,
			auth.OTPPurposeUnlinkIdentifier,
		)
	}

	if storedTargetIdentityID != identityID {
		t.Fatalf(
			"OTP target identity ID = %q, want %q",
			storedTargetIdentityID,
			identityID,
		)
	}

	var (
		operationIdentityID      string
		operationIdentifierType  string
		operationNormalizedValue string
	)

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				identity_id::text,
				identifier_type,
				normalized_value
			FROM identifier_unlink_operations
			WHERE challenge_id = $1
		`,
		challengeID,
	).Scan(
		&operationIdentityID,
		&operationIdentifierType,
		&operationNormalizedValue,
	)
	if err != nil {
		t.Fatalf(
			"query created identifier unlink operation: %v",
			err,
		)
	}

	if operationIdentityID != identityID {
		t.Fatalf(
			"unlink operation identity ID = %q, want %q",
			operationIdentityID,
			identityID,
		)
	}

	if operationIdentifierType != string(targetIdentifier.Type) {
		t.Fatalf(
			"unlink operation identifier type = %q, want %q",
			operationIdentifierType,
			targetIdentifier.Type,
		)
	}

	if operationNormalizedValue != targetIdentifier.Value {
		t.Fatalf(
			"unlink operation identifier value = %q, want %q",
			operationNormalizedValue,
			targetIdentifier.Value,
		)
	}
}

func TestIdentifierUnlinkRequestStoreRejectsLastIdentifierRemoval(
	t *testing.T,
) {
	ctx, pool, store := newIdentifierUnlinkRequestIntegrationTest(t)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000302",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	)

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-last-check@example.com",
	}

	const challengeID = "otp_ch_unlink_request_last_identifier"

	err := store.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge: auth.OTPChallenge{
				ID:               challengeID,
				Identifier:       verificationIdentifier,
				Purpose:          auth.OTPPurposeUnlinkIdentifier,
				TargetIdentityID: &identityID,
				CodeHash:         "unlink-last-identifier-hash",
				ExpiresAt: time.Now().
					UTC().
					Add(5 * time.Minute),
			},
			TargetIdentifier: targetIdentifier,
		},
	)

	if !errors.Is(err, auth.ErrLastIdentifierRemoval) {
		t.Fatalf(
			"Create() error = %v, want %v",
			err,
			auth.ErrLastIdentifierRemoval,
		)
	}

	var challengeCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&challengeCount,
	); err != nil {
		t.Fatalf(
			"count rejected unlink OTP challenges: %v",
			err,
		)
	}

	if challengeCount != 0 {
		t.Fatalf(
			"rejected unlink challenge count = %d, want 0",
			challengeCount,
		)
	}

	var operationCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identifier_unlink_operations
			WHERE challenge_id = $1
		`,
		challengeID,
	).Scan(
		&operationCount,
	); err != nil {
		t.Fatalf(
			"count rejected unlink operations: %v",
			err,
		)
	}

	if operationCount != 0 {
		t.Fatalf(
			"rejected unlink operation count = %d, want 0",
			operationCount,
		)
	}
}

func TestIdentifierUnlinkRequestStoreRejectsTargetIdentifierNotLinked(
	t *testing.T,
) {
	ctx, pool, store := newIdentifierUnlinkRequestIntegrationTest(t)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-target-missing-verification@example.com",
	}

	otherIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000303",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		otherIdentifier,
	)

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-target-missing@example.com",
	}

	const challengeID = "otp_ch_unlink_request_target_missing"

	err := store.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge: auth.OTPChallenge{
				ID:               challengeID,
				Identifier:       verificationIdentifier,
				Purpose:          auth.OTPPurposeUnlinkIdentifier,
				TargetIdentityID: &identityID,
				CodeHash:         "unlink-target-missing-hash",
				ExpiresAt: time.Now().
					UTC().
					Add(5 * time.Minute),
			},
			TargetIdentifier: targetIdentifier,
		},
	)

	if !errors.Is(err, auth.ErrIdentifierNotLinked) {
		t.Fatalf(
			"Create() error = %v, want %v",
			err,
			auth.ErrIdentifierNotLinked,
		)
	}

	var challengeCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&challengeCount,
	); err != nil {
		t.Fatalf(
			"count rejected unlink OTP challenges: %v",
			err,
		)
	}

	if challengeCount != 0 {
		t.Fatalf(
			"rejected unlink challenge count = %d, want 0",
			challengeCount,
		)
	}

	var operationCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identifier_unlink_operations
			WHERE challenge_id = $1
		`,
		challengeID,
	).Scan(
		&operationCount,
	); err != nil {
		t.Fatalf(
			"count rejected unlink operations: %v",
			err,
		)
	}

	if operationCount != 0 {
		t.Fatalf(
			"rejected unlink operation count = %d, want 0",
			operationCount,
		)
	}
}

func TestIdentifierUnlinkRequestStoreRejectsVerificationIdentifierNotLinked(
	t *testing.T,
) {
	ctx, pool, store := newIdentifierUnlinkRequestIntegrationTest(t)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000304",
	}

	otherIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-verification-missing-other@example.com",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		otherIdentifier,
	)

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-verification-missing@example.com",
	}

	const challengeID = "otp_ch_unlink_request_verification_missing"

	err := store.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge: auth.OTPChallenge{
				ID:               challengeID,
				Identifier:       verificationIdentifier,
				Purpose:          auth.OTPPurposeUnlinkIdentifier,
				TargetIdentityID: &identityID,
				CodeHash:         "unlink-verification-missing-hash",
				ExpiresAt: time.Now().
					UTC().
					Add(5 * time.Minute),
			},
			TargetIdentifier: targetIdentifier,
		},
	)

	if !errors.Is(err, auth.ErrIdentifierNotLinked) {
		t.Fatalf(
			"Create() error = %v, want %v",
			err,
			auth.ErrIdentifierNotLinked,
		)
	}

	var challengeCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(
		&challengeCount,
	); err != nil {
		t.Fatalf(
			"count rejected unlink OTP challenges: %v",
			err,
		)
	}

	if challengeCount != 0 {
		t.Fatalf(
			"rejected unlink challenge count = %d, want 0",
			challengeCount,
		)
	}

	var operationCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identifier_unlink_operations
			WHERE challenge_id = $1
		`,
		challengeID,
	).Scan(
		&operationCount,
	); err != nil {
		t.Fatalf(
			"count rejected unlink operations: %v",
			err,
		)
	}

	if operationCount != 0 {
		t.Fatalf(
			"rejected unlink operation count = %d, want 0",
			operationCount,
		)
	}
}

func TestIdentifierUnlinkRequestStoreCancelsPreviousActiveChallenge(
	t *testing.T,
) {
	ctx, pool, store := newIdentifierUnlinkRequestIntegrationTest(t)

	identityID := createIdentifierUnlinkTestIdentity(
		t,
		ctx,
		pool,
	)

	targetIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000305",
	}

	verificationIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "unlink-request-replacement@example.com",
	}

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		targetIdentifier,
	)

	insertIdentifierUnlinkTestIdentifier(
		t,
		ctx,
		pool,
		identityID,
		verificationIdentifier,
	)

	const firstChallengeID = "otp_ch_unlink_request_replacement_first"
	const secondChallengeID = "otp_ch_unlink_request_replacement_second"

	firstChallenge := auth.OTPChallenge{
		ID:               firstChallengeID,
		Identifier:       verificationIdentifier,
		Purpose:          auth.OTPPurposeUnlinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "unlink-request-replacement-first-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := store.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge:        firstChallenge,
			TargetIdentifier: targetIdentifier,
		},
	); err != nil {
		t.Fatalf(
			"create first unlink request: %v",
			err,
		)
	}

	secondChallenge := auth.OTPChallenge{
		ID:               secondChallengeID,
		Identifier:       verificationIdentifier,
		Purpose:          auth.OTPPurposeUnlinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "unlink-request-replacement-second-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := store.Create(
		ctx,
		auth.IdentifierUnlinkRequestInput{
			Challenge:        secondChallenge,
			TargetIdentifier: targetIdentifier,
		},
	); err != nil {
		t.Fatalf(
			"create replacement unlink request: %v",
			err,
		)
	}

	var firstCancelledAt *time.Time

	if err := pool.QueryRow(
		ctx,
		`
			SELECT cancelled_at
			FROM otp_challenges
			WHERE id = $1
		`,
		firstChallengeID,
	).Scan(
		&firstCancelledAt,
	); err != nil {
		t.Fatalf(
			"query first unlink challenge: %v",
			err,
		)
	}

	if firstCancelledAt == nil {
		t.Fatal(
			"first unlink challenge was not cancelled",
		)
	}

	var secondCancelledAt *time.Time

	if err := pool.QueryRow(
		ctx,
		`
			SELECT cancelled_at
			FROM otp_challenges
			WHERE id = $1
		`,
		secondChallengeID,
	).Scan(
		&secondCancelledAt,
	); err != nil {
		t.Fatalf(
			"query replacement unlink challenge: %v",
			err,
		)
	}

	if secondCancelledAt != nil {
		t.Fatal(
			"replacement unlink challenge was unexpectedly cancelled",
		)
	}

	var operationCount int

	if err := pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM identifier_unlink_operations
			WHERE challenge_id IN ($1, $2)
		`,
		firstChallengeID,
		secondChallengeID,
	).Scan(
		&operationCount,
	); err != nil {
		t.Fatalf(
			"count replacement unlink operations: %v",
			err,
		)
	}

	if operationCount != 2 {
		t.Fatalf(
			"unlink operation count = %d, want 2",
			operationCount,
		)
	}
}

func newIdentifierUnlinkRequestIntegrationTest(
	t *testing.T,
) (
	context.Context,
	*pgxpool.Pool,
	*IdentifierUnlinkRequestStore,
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
		NewIdentifierUnlinkRequestStore(pool)
}

func createIdentifierUnlinkTestIdentity(
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
			"create identifier unlink test identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM identities
				WHERE id = $1::uuid
			`,
			identityID,
		); cleanupErr != nil {
			t.Errorf(
				"clean identifier unlink test identity %q: %v",
				identityID,
				cleanupErr,
			)
		}
	})

	return identityID
}

func insertIdentifierUnlinkTestIdentifier(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	identityID string,
	identifier auth.Identifier,
) {
	t.Helper()

	normalizedIdentifier, err := auth.NewIdentifier(
		identifier.Type,
		identifier.Value,
	)
	if err != nil {
		t.Fatalf(
			"normalize identifier unlink test identifier: %v",
			err,
		)
	}

	if _, err := pool.Exec(
		ctx,
		`
			INSERT INTO identity_identifiers (
				identity_id,
				identifier_type,
				normalized_value,
				verified_at
			)
			VALUES ($1::uuid, $2, $3, $4)
		`,
		identityID,
		string(normalizedIdentifier.Type),
		normalizedIdentifier.Value,
		time.Now().UTC(),
	); err != nil {
		t.Fatalf(
			"insert identifier unlink test identifier: %v",
			err,
		)
	}
}
