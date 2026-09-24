//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/walletpin"
	databaseinfra "github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/pinhash"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newWalletPinIntegrationTest(t *testing.T) (context.Context, *pgxpool.Pool, *WalletPinStore) {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}

	ctx := context.Background()

	pool, err := databaseinfra.NewPostgresPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}

	t.Cleanup(pool.Close)

	return ctx, pool, NewWalletPinStore(pool)
}

func addPhone(t *testing.T, ctx context.Context, pool *pgxpool.Pool, identityID, phone string) {
	t.Helper()

	if _, err := pool.Exec(ctx,
		`INSERT INTO identity_identifiers (identity_id, identifier_type, normalized_value, verified_at)
		 VALUES ($1, 'phone', $2, now())`,
		identityID, phone,
	); err != nil {
		t.Fatalf("add phone: %v", err)
	}
}

func TestWalletPinsAreStoredAndLocked(t *testing.T) {
	ctx, pool, store := newWalletPinIntegrationTest(t)
	identityID := createIntegrationIdentity(t, ctx, pool)
	service := walletpin.NewService(store, pinhash.New(bcrypt.MinCost), store)

	if status, err := service.Status(ctx, identityID); err != nil || status.IsSet {
		t.Fatalf("before: %+v %v", status, err)
	}

	if _, err := service.Set(ctx, walletpin.SetInput{IdentityID: identityID, NewPIN: "2580"}); err != nil {
		t.Fatal(err)
	}

	if result, err := service.Verify(ctx, identityID, "2580"); err != nil || result.Check != walletpin.CheckOK {
		t.Fatalf("right: %+v %v", result, err)
	}

	for i := 0; i < walletpin.MaxFailedAttempts; i++ {
		if _, err := service.Verify(ctx, identityID, "1111"); err != nil {
			t.Fatal(err)
		}
	}

	status, err := service.Status(ctx, identityID)
	if err != nil || status.LockedUntil == nil || status.AttemptsLeft != 0 {
		t.Fatalf("locked: %+v %v", status, err)
	}

	if _, err := service.Set(ctx, walletpin.SetInput{IdentityID: identityID, NewPIN: "1357", CurrentPIN: "2580"}); !errors.Is(err, walletpin.ErrPINLocked) {
		t.Fatalf("changing while locked: %v", err)
	}

	// Signing in again (a new session) lets a new PIN be set and lifts the lock.
	var sessionID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO auth_sessions (identity_id, expires_at) VALUES ($1, now() + interval '1 day') RETURNING id::text`,
		identityID,
	).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}

	status, err = service.Set(ctx, walletpin.SetInput{IdentityID: identityID, SessionID: sessionID, NewPIN: "1357"})
	if err != nil || status.LockedUntil != nil {
		t.Fatalf("reset: %+v %v", status, err)
	}

	if result, _ := service.Verify(ctx, identityID, "1357"); result.Check != walletpin.CheckOK {
		t.Fatalf("new PIN: %+v", result)
	}

	// A session signed in long ago does not.
	if _, err := pool.Exec(ctx, `UPDATE auth_sessions SET created_at = now() - interval '1 hour' WHERE id = $1`, sessionID); err != nil {
		t.Fatal(err)
	}

	if _, err := service.Set(ctx, walletpin.SetInput{IdentityID: identityID, SessionID: sessionID, NewPIN: "2468"}); !errors.Is(err, walletpin.ErrCurrentPINRequired) {
		t.Fatalf("an old session: %v", err)
	}
}

func TestParallelGuessesAreCountedOneByOne(t *testing.T) {
	ctx, pool, store := newWalletPinIntegrationTest(t)
	identityID := createIntegrationIdentity(t, ctx, pool)
	service := walletpin.NewService(store, pinhash.New(bcrypt.MinCost), store)

	if _, err := service.Set(ctx, walletpin.SetInput{IdentityID: identityID, NewPIN: "2580"}); err != nil {
		t.Fatal(err)
	}

	const guesses = 20

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		wrong  int
		locked int
	)

	for i := 0; i < guesses; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			result, err := service.Verify(ctx, identityID, fmt.Sprintf("%04d", 3000+i*7))
			if err != nil {
				t.Errorf("verify: %v", err)

				return
			}

			mu.Lock()
			defer mu.Unlock()

			switch result.Check {
			case walletpin.CheckWrong:
				wrong++
			case walletpin.CheckLocked:
				locked++
			}
		}(i)
	}

	wg.Wait()

	// Exactly MaxFailedAttempts guesses were compared; the rest met the lock.
	if wrong != walletpin.MaxFailedAttempts-1 || locked != guesses-wrong {
		t.Fatalf("wrong %d, locked %d", wrong, locked)
	}
}

func TestThePhoneDirectory(t *testing.T) {
	ctx, pool, store := newWalletPinIntegrationTest(t)
	identityID := createIntegrationIdentity(t, ctx, pool)
	phone := fmt.Sprintf("+96477%08d", time.Now().UnixNano()%100000000)
	addPhone(t, ctx, pool, identityID, phone)

	if found, ok, err := store.FindActiveByPhone(ctx, phone); err != nil || !ok || found != identityID {
		t.Fatalf("found %v %v %v", found, ok, err)
	}

	if got, err := store.PhoneOf(ctx, identityID); err != nil || got != phone {
		t.Fatalf("phone %v %v", got, err)
	}

	if _, err := pool.Exec(ctx, `UPDATE identities SET status = 'suspended' WHERE id = $1`, identityID); err != nil {
		t.Skipf("identities have no suspended status here: %v", err)
	}

	if _, ok, err := store.FindActiveByPhone(ctx, phone); err != nil || ok {
		t.Fatalf("an inactive identity must not be found: %v %v", ok, err)
	}
}
