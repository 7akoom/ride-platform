//go:build integration

package token

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	valkeyrepo "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/persistence/valkey"
)

func TestAccessTokenVerifierRejectsTokenAfterRealValkeyRevocation(
	t *testing.T,
) {
	valkeyAddress := os.Getenv("VALKEY_ADDRESS")
	valkeyPassword := os.Getenv("VALKEY_PASSWORD")

	privateKeyPath := os.Getenv(
		"ACCESS_TOKEN_PRIVATE_KEY_PATH",
	)
	publicKeyPath := os.Getenv(
		"ACCESS_TOKEN_PUBLIC_KEY_PATH",
	)
	privateKeyPath = resolveModuleRelativePath(
		t,
		privateKeyPath,
	)

	publicKeyPath = resolveModuleRelativePath(
		t,
		publicKeyPath,
	)
	issuer := os.Getenv(
		"ACCESS_TOKEN_ISSUER",
	)
	audience := os.Getenv(
		"ACCESS_TOKEN_AUDIENCE",
	)
	keyID := os.Getenv(
		"ACCESS_TOKEN_KEY_ID",
	)

	if valkeyAddress == "" ||
		valkeyPassword == "" ||
		privateKeyPath == "" ||
		publicKeyPath == "" ||
		issuer == "" ||
		audience == "" ||
		keyID == "" {
		t.Skip(
			"required Valkey or access token environment variables are not configured",
		)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	valkeyClient, err := database.NewValkeyClient(
		ctx,
		valkeyAddress,
		valkeyPassword,
	)
	if err != nil {
		t.Fatalf(
			"connect to Valkey: %v",
			err,
		)
	}
	defer valkeyClient.Close()

	accessRevocationStore :=
		valkeyrepo.NewSessionAccessRevocationStore(
			valkeyClient,
		)

	const accessTokenTTL = 15 * time.Minute

	signer, err := NewAccessTokenSigner(
		privateKeyPath,
		issuer,
		audience,
		keyID,
		accessTokenTTL,
	)
	if err != nil {
		t.Fatalf(
			"NewAccessTokenSigner() returned an error: %v",
			err,
		)
	}

	verifier, err := NewAccessTokenVerifier(
		publicKeyPath,
		issuer,
		audience,
		keyID,
		accessRevocationStore,
	)
	if err != nil {
		t.Fatalf(
			"NewAccessTokenVerifier() returned an error: %v",
			err,
		)
	}

	sessionID := "integration-session-" +
		time.Now().UTC().Format(
			"20060102150405.000000000",
		)

	issuedAt := time.Now().
		UTC().
		Truncate(time.Second)

	sessionExpiresAt := issuedAt.Add(
		24 * time.Hour,
	)

	signedToken, _, err := signer.IssueForSession(
		"integration-identity",
		sessionID,
		"",
		issuedAt,
		sessionExpiresAt,
	)
	if err != nil {
		t.Fatalf(
			"IssueForSession() returned an error: %v",
			err,
		)
	}

	claims, err := verifier.Verify(
		ctx,
		signedToken,
	)
	if err != nil {
		t.Fatalf(
			"Verify() rejected token before revocation: %v",
			err,
		)
	}

	if claims.SessionID != sessionID {
		t.Fatalf(
			"verified session ID = %q, expected %q",
			claims.SessionID,
			sessionID,
		)
	}

	if err := accessRevocationStore.MarkRevoked(
		ctx,
		sessionID,
		30*time.Second,
	); err != nil {
		t.Fatalf(
			"MarkRevoked() returned an error: %v",
			err,
		)
	}

	_, err = verifier.Verify(
		ctx,
		signedToken,
	)
	if err == nil {
		t.Fatal(
			"Verify() accepted token after session revocation",
		)
	}

	if !errors.Is(
		err,
		ErrAccessTokenRevoked,
	) {
		t.Fatalf(
			"Verify() error = %v, expected %v",
			err,
			ErrAccessTokenRevoked,
		)
	}
}

func resolveModuleRelativePath(
	t *testing.T,
	path string,
) string {
	t.Helper()

	if path == "" || filepath.IsAbs(path) {
		return path
	}

	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatalf(
			"get current working directory: %v",
			err,
		)
	}

	dir := currentDir

	for {
		goModPath := filepath.Join(
			dir,
			"go.mod",
		)

		if _, err := os.Stat(goModPath); err == nil {
			return filepath.Join(
				dir,
				path,
			)
		}

		parent := filepath.Dir(dir)

		if parent == dir {
			t.Fatalf(
				"could not find module root for relative path %q",
				path,
			)
		}

		dir = parent
	}
}
