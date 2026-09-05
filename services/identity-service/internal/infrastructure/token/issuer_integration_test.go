//go:build integration

package token

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	clockinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/clock"
	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestIssuerIssuePersistsSessionAndReturnsValidTokens(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}

	ctx := context.Background()

	pool, err := databaseinfra.NewPostgresPool(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}

	t.Cleanup(pool.Close)

	const phoneNumber = "+9647500000005"

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM identities AS i
			USING identity_identifiers AS ii
			WHERE ii.identity_id = i.id
			AND ii.identifier_type = 'phone'
			AND ii.normalized_value = $1
		`,
		phoneNumber,
	)
	if err != nil {
		t.Fatalf("clean existing test identity: %v", err)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM identities AS i
				USING identity_identifiers AS ii
				WHERE ii.identity_id = i.id
				AND ii.identifier_type = 'phone'
				AND ii.normalized_value = $1
			`,
			phoneNumber,
		)
		if cleanupErr != nil {
			t.Errorf("clean test identity: %v", cleanupErr)
		}
	})

	var identityID string

	err = pool.QueryRow(
		ctx,
		`
			WITH created_identity AS (
				INSERT INTO identities DEFAULT VALUES
				RETURNING id
			)
			INSERT INTO identity_identifiers (
				identity_id,
				identifier_type,
				normalized_value,
				verified_at
			)
			SELECT
				id,
				'phone',
				$1,
				CURRENT_TIMESTAMP
			FROM created_identity
			RETURNING identity_id::text
		`,
		phoneNumber,
	).Scan(&identityID)
	if err != nil {
		t.Fatalf("create test identity: %v", err)
	}

	challengeID := "otp-issuer-integration-" + identityID

	var verifiedAt time.Time

	err = pool.QueryRow(
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
				'phone',
				$2,
				'login',
				NULL,
				$3,
				statement_timestamp() + INTERVAL '5 minutes'
			)
			RETURNING created_at
		`,
		challengeID,
		phoneNumber,
		strings.Repeat("a", 64),
	).Scan(&verifiedAt)
	if err != nil {
		t.Fatalf(
			"create OTP challenge: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			"DELETE FROM otp_challenges WHERE id = $1",
			challengeID,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean OTP challenge: %v",
				cleanupErr,
			)
		}
	})

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate Ed25519 key pair: %v", err)
	}

	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}

	privateKeyPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PRIVATE KEY",
			Bytes: privateKeyDER,
		},
	)

	privateKeyPath := filepath.Join(
		t.TempDir(),
		"access_token_private.pem",
	)

	if err := os.WriteFile(
		privateKeyPath,
		privateKeyPEM,
		0600,
	); err != nil {
		t.Fatalf("write temporary private key: %v", err)
	}

	const (
		issuer     = "ride-identity"
		audience   = "ride-platform"
		keyID      = "identity-integration-1"
		accessTTL  = 15 * time.Minute
		sessionTTL = 30 * 24 * time.Hour
		refreshTTL = 29 * 24 * time.Hour
	)

	accessTokenSigner, err := NewAccessTokenSigner(
		privateKeyPath,
		issuer,
		audience,
		keyID,
		accessTTL,
	)
	if err != nil {
		t.Fatalf(
			"create access token signer: %v",
			err,
		)
	}

	issuerService, err := NewIssuer(
		NewSessionIDGenerator(),
		NewRefreshTokenGenerator(),
		accessTokenSigner,
		NewSessionStore(pool),
		clockinfra.NewSystemClock(),
		sessionTTL,
		refreshTTL,
	)
	if err != nil {
		t.Fatalf("create token issuer: %v", err)
	}

	tokenPair, err := issuerService.Issue(
		ctx,
		auth.TokenIssueInput{
			Identity: auth.Identity{
				ID:       identityID,
				IsActive: true,
			},
			ChallengeID: challengeID,
			VerifiedAt:  verifiedAt,
		},
	)
	if err != nil {
		t.Fatalf("Issue() returned an error: %v", err)
	}

	var storedVerifiedAt *time.Time

	err = pool.QueryRow(
		ctx,
		`
			SELECT verified_at
			FROM otp_challenges
			WHERE id = $1
		`,
		challengeID,
	).Scan(&storedVerifiedAt)
	if err != nil {
		t.Fatalf(
			"query verified OTP challenge: %v",
			err,
		)
	}

	if storedVerifiedAt == nil {
		t.Fatal(
			"OTP challenge was not marked verified after successful token issuance",
		)
	}

	if !storedVerifiedAt.Equal(verifiedAt) {
		t.Fatalf(
			"OTP challenge verification time = %v, expected %v",
			*storedVerifiedAt,
			verifiedAt,
		)
	}

	if tokenPair.AccessToken == "" {
		t.Fatal("Issue() returned empty access token")
	}

	if tokenPair.RefreshToken == "" {
		t.Fatal("Issue() returned empty refresh token")
	}

	if !strings.HasPrefix(
		tokenPair.RefreshToken,
		"rt_",
	) {
		t.Fatalf(
			"refresh token has unexpected format: %q",
			tokenPair.RefreshToken,
		)
	}

	if tokenPair.AccessTokenExpiresInSeconds !=
		int32(accessTTL.Seconds()) {
		t.Fatalf(
			"access token expiry is %d, expected %d",
			tokenPair.AccessTokenExpiresInSeconds,
			int32(accessTTL.Seconds()),
		)
	}

	expectedRefreshTokenHash := HashRefreshToken(
		tokenPair.RefreshToken,
	)

	var storedSessionID string
	var storedRefreshTokenHash string

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				s.id::text,
				rt.token_hash
			FROM auth_sessions AS s
			INNER JOIN refresh_tokens AS rt
				ON rt.session_id = s.id
			WHERE s.identity_id = $1::uuid
		`,
		identityID,
	).Scan(
		&storedSessionID,
		&storedRefreshTokenHash,
	)
	if err != nil {
		t.Fatalf(
			"query persisted authentication session: %v",
			err,
		)
	}

	if storedRefreshTokenHash != expectedRefreshTokenHash {
		t.Fatal(
			"database does not contain expected refresh token hash",
		)
	}

	if storedRefreshTokenHash == tokenPair.RefreshToken {
		t.Fatal(
			"database contains raw refresh token",
		)
	}

	var claims AccessTokenClaims

	parsedToken, err := jwt.ParseWithClaims(
		tokenPair.AccessToken,
		&claims,
		func(token *jwt.Token) (any, error) {
			return publicKey, nil
		},
		jwt.WithValidMethods(
			[]string{
				jwt.SigningMethodEdDSA.Alg(),
			},
		),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(audience),
	)
	if err != nil {
		t.Fatalf(
			"verify issued access token: %v",
			err,
		)
	}

	if !parsedToken.Valid {
		t.Fatal("issued access token is invalid")
	}

	if claims.Subject != identityID {
		t.Fatalf(
			"access token subject is %q, expected %q",
			claims.Subject,
			identityID,
		)
	}

	if claims.SessionID != storedSessionID {
		t.Fatalf(
			"access token session ID is %q, database session ID is %q",
			claims.SessionID,
			storedSessionID,
		)
	}
}
