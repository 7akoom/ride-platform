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

func TestChallengeRepositoryCreateFindAndConsumePhoneLoginChallenge(
	t *testing.T,
) {
	ctx, pool, repository := newChallengeRepositoryIntegrationTest(t)

	const challengeID = "otp_ch_integration_phone_single_use"

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000001",
	}

	cleanupChallengeIDs(
		t,
		pool,
		challengeID,
	)

	now := time.Now().UTC()

	challenge := auth.OTPChallenge{
		ID:         challengeID,
		Identifier: identifier,
		Purpose:    auth.OTPPurposeLogin,
		CodeHash:   "integration-test-code-hash",
		ExpiresAt:  now.Add(5 * time.Minute),
	}

	if err := repository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"Create() returned an error: %v",
			err,
		)
	}

	storedChallenge, err := repository.FindByID(
		ctx,
		challengeID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() returned an error: %v",
			err,
		)
	}

	if storedChallenge.ID != challenge.ID {
		t.Fatalf(
			"FindByID() returned ID %q, want %q",
			storedChallenge.ID,
			challenge.ID,
		)
	}

	if storedChallenge.Identifier != identifier {
		t.Fatalf(
			"FindByID() returned identifier %+v, want %+v",
			storedChallenge.Identifier,
			identifier,
		)
	}

	if storedChallenge.Purpose != auth.OTPPurposeLogin {
		t.Fatalf(
			"FindByID() returned purpose %q, want %q",
			storedChallenge.Purpose,
			auth.OTPPurposeLogin,
		)
	}

	if storedChallenge.TargetIdentityID != nil {
		t.Fatalf(
			"login challenge target identity = %v, want nil",
			storedChallenge.TargetIdentityID,
		)
	}

	if storedChallenge.CodeHash != challenge.CodeHash {
		t.Fatal(
			"FindByID() returned unexpected code hash",
		)
	}

	if storedChallenge.VerifiedAt != nil {
		t.Fatal(
			"new challenge is already marked as verified",
		)
	}

	verifiedAt := time.Now().UTC()

	if err := repository.MarkVerified(
		ctx,
		challengeID,
		verifiedAt,
	); err != nil {
		t.Fatalf(
			"first MarkVerified() returned an error: %v",
			err,
		)
	}

	err = repository.MarkVerified(
		ctx,
		challengeID,
		verifiedAt.Add(time.Second),
	)

	if !errors.Is(
		err,
		auth.ErrChallengeUsed,
	) {
		t.Fatalf(
			"second MarkVerified() error = %v, want %v",
			err,
			auth.ErrChallengeUsed,
		)
	}

	consumedChallenge, err := repository.FindByID(
		ctx,
		challengeID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() after verification returned an error: %v",
			err,
		)
	}

	if consumedChallenge.VerifiedAt == nil {
		t.Fatal(
			"consumed challenge has nil VerifiedAt",
		)
	}
}

func TestChallengeRepositoryNormalizesAndRestoresEmailLoginChallenge(
	t *testing.T,
) {
	ctx, pool, repository := newChallengeRepositoryIntegrationTest(t)

	const challengeID = "otp_ch_integration_email_login"

	cleanupChallengeIDs(
		t,
		pool,
		challengeID,
	)

	challenge := auth.OTPChallenge{
		ID: challengeID,
		Identifier: auth.Identifier{
			Type:  auth.IdentifierTypeEmail,
			Value: "Login.User@EXAMPLE.COM",
		},
		Purpose:  auth.OTPPurposeLogin,
		CodeHash: "email-login-code-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := repository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"Create() returned an error: %v",
			err,
		)
	}

	storedChallenge, err := repository.FindByID(
		ctx,
		challengeID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() returned an error: %v",
			err,
		)
	}

	expectedIdentifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "login.user@example.com",
	}

	if storedChallenge.Identifier != expectedIdentifier {
		t.Fatalf(
			"stored identifier = %+v, want %+v",
			storedChallenge.Identifier,
			expectedIdentifier,
		)
	}

	if storedChallenge.Purpose != auth.OTPPurposeLogin {
		t.Fatalf(
			"stored purpose = %q, want %q",
			storedChallenge.Purpose,
			auth.OTPPurposeLogin,
		)
	}

	if storedChallenge.TargetIdentityID != nil {
		t.Fatal(
			"email login challenge unexpectedly targets an identity",
		)
	}
}

func TestChallengeRepositoryStoresLinkIdentifierTarget(
	t *testing.T,
) {
	ctx, pool, repository := newChallengeRepositoryIntegrationTest(t)

	const challengeID = "otp_ch_integration_link_identifier"

	identityID := createIntegrationIdentity(
		t,
		ctx,
		pool,
	)

	cleanupChallengeIDs(
		t,
		pool,
		challengeID,
	)

	challenge := auth.OTPChallenge{
		ID: challengeID,
		Identifier: auth.Identifier{
			Type:  auth.IdentifierTypeEmail,
			Value: "linked@example.com",
		},
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &identityID,
		CodeHash:         "link-identifier-code-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	if err := repository.Create(
		ctx,
		challenge,
	); err != nil {
		t.Fatalf(
			"Create() returned an error: %v",
			err,
		)
	}

	storedChallenge, err := repository.FindByID(
		ctx,
		challengeID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() returned an error: %v",
			err,
		)
	}

	if storedChallenge.TargetIdentityID == nil {
		t.Fatal(
			"link identifier challenge has nil target identity",
		)
	}

	if *storedChallenge.TargetIdentityID != identityID {
		t.Fatalf(
			"target identity = %q, want %q",
			*storedChallenge.TargetIdentityID,
			identityID,
		)
	}

	if storedChallenge.Purpose !=
		auth.OTPPurposeLinkIdentifier {
		t.Fatalf(
			"purpose = %q, want %q",
			storedChallenge.Purpose,
			auth.OTPPurposeLinkIdentifier,
		)
	}
}

func TestChallengeRepositoryKeepsOnlyLatestChallengeActiveConcurrently(
	t *testing.T,
) {
	ctx, pool, repository := newChallengeRepositoryIntegrationTest(t)

	const firstChallengeID = "otp_ch_concurrent_latest_first"
	const secondChallengeID = "otp_ch_concurrent_latest_second"

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "latest@example.com",
	}

	cleanupChallengeIDs(
		t,
		pool,
		firstChallengeID,
		secondChallengeID,
	)

	now := time.Now().UTC()

	firstChallenge := auth.OTPChallenge{
		ID:         firstChallengeID,
		Identifier: identifier,
		Purpose:    auth.OTPPurposeLogin,
		CodeHash:   "first-concurrent-code-hash",
		ExpiresAt:  now.Add(5 * time.Minute),
	}

	secondChallenge := auth.OTPChallenge{
		ID:         secondChallengeID,
		Identifier: identifier,
		Purpose:    auth.OTPPurposeLogin,
		CodeHash:   "second-concurrent-code-hash",
		ExpiresAt:  now.Add(5 * time.Minute),
	}

	start := make(chan struct{})
	results := make(chan error, 2)

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)

	go func() {
		defer waitGroup.Done()

		<-start

		results <- repository.Create(
			ctx,
			firstChallenge,
		)
	}()

	go func() {
		defer waitGroup.Done()

		<-start

		results <- repository.Create(
			ctx,
			secondChallenge,
		)
	}()

	close(start)

	waitGroup.Wait()
	close(results)

	for createErr := range results {
		if createErr != nil {
			t.Fatalf(
				"concurrent Create() returned an error: %v",
				createErr,
			)
		}
	}

	var totalChallenges int
	var activeChallenges int
	var cancelledChallenges int

	err := pool.QueryRow(
		ctx,
		`
			SELECT
				COUNT(*),
				COUNT(*) FILTER (
					WHERE verified_at IS NULL
					  AND cancelled_at IS NULL
					  AND expires_at > CURRENT_TIMESTAMP
				),
				COUNT(*) FILTER (
					WHERE cancelled_at IS NOT NULL
				)
			FROM otp_challenges
			WHERE id IN ($1, $2)
		`,
		firstChallengeID,
		secondChallengeID,
	).Scan(
		&totalChallenges,
		&activeChallenges,
		&cancelledChallenges,
	)
	if err != nil {
		t.Fatalf(
			"query concurrent OTP challenge state: %v",
			err,
		)
	}

	if totalChallenges != 2 {
		t.Fatalf(
			"total challenges = %d, want 2",
			totalChallenges,
		)
	}

	if activeChallenges != 1 {
		t.Fatalf(
			"active challenges = %d, want 1",
			activeChallenges,
		)
	}

	if cancelledChallenges != 1 {
		t.Fatalf(
			"cancelled challenges = %d, want 1",
			cancelledChallenges,
		)
	}
}

func TestChallengeRepositoryScopesLinkIdentifierLatestChallengeByTargetIdentity(
	t *testing.T,
) {
	ctx, pool, repository := newChallengeRepositoryIntegrationTest(t)

	const firstTargetChallengeID = "otp_ch_link_target_a_first"
	const secondTargetChallengeID = "otp_ch_link_target_a_second"
	const otherTargetChallengeID = "otp_ch_link_target_b"

	firstIdentityID := createIntegrationIdentity(
		t,
		ctx,
		pool,
	)

	secondIdentityID := createIntegrationIdentity(
		t,
		ctx,
		pool,
	)

	cleanupChallengeIDs(
		t,
		pool,
		firstTargetChallengeID,
		secondTargetChallengeID,
		otherTargetChallengeID,
	)

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypeEmail,
		Value: "scope@example.com",
	}

	now := time.Now().UTC()

	firstTargetChallenge := auth.OTPChallenge{
		ID:               firstTargetChallengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &firstIdentityID,
		CodeHash:         "target-a-first-code-hash",
		ExpiresAt:        now.Add(5 * time.Minute),
	}

	otherTargetChallenge := auth.OTPChallenge{
		ID:               otherTargetChallengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &secondIdentityID,
		CodeHash:         "target-b-code-hash",
		ExpiresAt:        now.Add(5 * time.Minute),
	}

	secondTargetChallenge := auth.OTPChallenge{
		ID:               secondTargetChallengeID,
		Identifier:       identifier,
		Purpose:          auth.OTPPurposeLinkIdentifier,
		TargetIdentityID: &firstIdentityID,
		CodeHash:         "target-a-second-code-hash",
		ExpiresAt:        now.Add(5 * time.Minute),
	}

	for _, challenge := range []auth.OTPChallenge{
		firstTargetChallenge,
		otherTargetChallenge,
		secondTargetChallenge,
	} {
		if err := repository.Create(
			ctx,
			challenge,
		); err != nil {
			t.Fatalf(
				"Create(%q) returned an error: %v",
				challenge.ID,
				err,
			)
		}
	}

	firstStored, err := repository.FindByID(
		ctx,
		firstTargetChallengeID,
	)
	if err != nil {
		t.Fatalf(
			"find first target challenge: %v",
			err,
		)
	}

	secondStored, err := repository.FindByID(
		ctx,
		secondTargetChallengeID,
	)
	if err != nil {
		t.Fatalf(
			"find second target challenge: %v",
			err,
		)
	}

	otherStored, err := repository.FindByID(
		ctx,
		otherTargetChallengeID,
	)
	if err != nil {
		t.Fatalf(
			"find other target challenge: %v",
			err,
		)
	}

	if firstStored.CancelledAt == nil {
		t.Fatal(
			"older challenge for same target identity remained active",
		)
	}

	if secondStored.CancelledAt != nil {
		t.Fatal(
			"latest challenge for first target identity was cancelled",
		)
	}

	if otherStored.CancelledAt != nil {
		t.Fatal(
			"challenge for different target identity was incorrectly cancelled",
		)
	}
}

func TestChallengeRepositoryUsesCancellationTimeAfterWaitingForScopeLock(
	t *testing.T,
) {
	ctx, pool, repository := newChallengeRepositoryIntegrationTest(t)

	const existingChallengeID = "otp_ch_lock_wait_existing"
	const newChallengeID = "otp_ch_lock_wait_new"

	identifier := auth.Identifier{
		Type:  auth.IdentifierTypePhone,
		Value: "+9647500000098",
	}

	purpose := auth.OTPPurposeLogin

	cleanupChallengeIDs(
		t,
		pool,
		existingChallengeID,
		newChallengeID,
	)

	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf(
			"begin advisory lock transaction: %v",
			err,
		)
	}

	lockReleased := false

	t.Cleanup(func() {
		if !lockReleased {
			_ = lockTx.Rollback(
				context.Background(),
			)
		}
	})

	lockKey := challengeScopeLockKey(
		identifier,
		purpose,
		nil,
	)

	_, err = lockTx.Exec(
		ctx,
		`
			SELECT pg_advisory_xact_lock(
				hashtextextended($1, 0)
			)
		`,
		lockKey,
	)
	if err != nil {
		t.Fatalf(
			"acquire advisory challenge scope lock: %v",
			err,
		)
	}

	createResult := make(chan error, 1)

	newChallenge := auth.OTPChallenge{
		ID:         newChallengeID,
		Identifier: identifier,
		Purpose:    purpose,
		CodeHash:   "new-lock-wait-code-hash",
		ExpiresAt: time.Now().
			UTC().
			Add(5 * time.Minute),
	}

	go func() {
		createResult <- repository.Create(
			context.Background(),
			newChallenge,
		)
	}()

	time.Sleep(100 * time.Millisecond)

	_, err = pool.Exec(
		ctx,
		`
			INSERT INTO otp_challenges (
				id,
				identifier_type,
				normalized_value,
				purpose,
				target_identity_id,
				code_hash,
				expires_at
			)
			VALUES (
				$1,
				$2,
				$3,
				$4,
				NULL,
				$5,
				$6
			)
		`,
		existingChallengeID,
		string(identifier.Type),
		identifier.Value,
		string(purpose),
		"existing-lock-wait-code-hash",
		time.Now().UTC().Add(5*time.Minute),
	)
	if err != nil {
		t.Fatalf(
			"insert challenge while Create() waits for lock: %v",
			err,
		)
	}

	if err := lockTx.Commit(ctx); err != nil {
		t.Fatalf(
			"release advisory challenge scope lock: %v",
			err,
		)
	}

	lockReleased = true

	select {
	case createErr := <-createResult:
		if createErr != nil {
			t.Fatalf(
				"Create() after waiting for lock returned an error: %v",
				createErr,
			)
		}

	case <-time.After(5 * time.Second):
		t.Fatal(
			"Create() did not finish after advisory lock was released",
		)
	}

	existingChallenge, err := repository.FindByID(
		ctx,
		existingChallengeID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() for existing challenge returned an error: %v",
			err,
		)
	}

	if existingChallenge.CancelledAt == nil {
		t.Fatal(
			"existing challenge remained active after newer challenge was created",
		)
	}

	newStoredChallenge, err := repository.FindByID(
		ctx,
		newChallengeID,
	)
	if err != nil {
		t.Fatalf(
			"FindByID() for new challenge returned an error: %v",
			err,
		)
	}

	if newStoredChallenge.CancelledAt != nil {
		t.Fatal(
			"new challenge was unexpectedly cancelled",
		)
	}
}

func newChallengeRepositoryIntegrationTest(
	t *testing.T,
) (
	context.Context,
	*pgxpool.Pool,
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

	return ctx, pool, NewChallengeRepository(pool)
}

func cleanupChallengeIDs(
	t *testing.T,
	pool *pgxpool.Pool,
	challengeIDs ...string,
) {
	t.Helper()

	cleanup := func() {
		for _, challengeID := range challengeIDs {
			if _, err := pool.Exec(
				context.Background(),
				`
					DELETE FROM otp_challenges
					WHERE id = $1
				`,
				challengeID,
			); err != nil {
				t.Errorf(
					"clean test challenge %q: %v",
					challengeID,
					err,
				)
			}
		}
	}

	cleanup()

	t.Cleanup(cleanup)
}

func createIntegrationIdentity(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) string {
	t.Helper()

	var identityID string

	if err := pool.QueryRow(
		ctx,
		`
			INSERT INTO identities
				DEFAULT VALUES
			RETURNING id::text
		`,
	).Scan(
		&identityID,
	); err != nil {
		t.Fatalf(
			"create integration test identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		if _, err := pool.Exec(
			context.Background(),
			`
				DELETE FROM identities
				WHERE id = $1
			`,
			identityID,
		); err != nil {
			t.Errorf(
				"clean integration test identity %q: %v",
				identityID,
				err,
			)
		}
	})

	return identityID
}
