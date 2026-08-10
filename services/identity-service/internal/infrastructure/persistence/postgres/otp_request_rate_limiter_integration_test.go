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
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestOTPRequestRateLimiterAllowsOnlyOneConcurrentRequestDuringCooldown(
	t *testing.T,
) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}

	ctx := context.Background()

	pool, err := database.NewPostgresPool(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}

	t.Cleanup(pool.Close)

	const phoneNumber = "+9647500000020"

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM otp_request_events
			WHERE phone_number = $1
		`,
		phoneNumber,
	)
	if err != nil {
		t.Fatalf(
			"clean existing OTP request events: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM otp_request_events
				WHERE phone_number = $1
			`,
			phoneNumber,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean OTP request events: %v",
				cleanupErr,
			)
		}
	})

	rateLimiter := NewOTPRequestRateLimiter(pool)

	policy := auth.OTPRequestRateLimitPolicy{
		Cooldown:    time.Minute,
		Window:      15 * time.Minute,
		MaxRequests: 5,
	}

	now := time.Now().UTC()

	const concurrentRequests = 10

	start := make(chan struct{})
	results := make(chan error, concurrentRequests)

	var waitGroup sync.WaitGroup

	for request := 0; request < concurrentRequests; request++ {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			<-start

			results <- rateLimiter.Allow(
				ctx,
				phoneNumber,
				now,
				policy,
			)
		}()
	}

	close(start)

	waitGroup.Wait()
	close(results)

	successCount := 0
	rateLimitedCount := 0

	for result := range results {
		switch {
		case result == nil:
			successCount++

		case errors.Is(
			result,
			auth.ErrOTPRequestRateLimited,
		):
			rateLimitedCount++

		default:
			t.Fatalf(
				"Allow() returned unexpected error: %v",
				result,
			)
		}
	}

	if successCount != 1 {
		t.Fatalf(
			"successful requests = %d, expected 1",
			successCount,
		)
	}

	if rateLimitedCount != concurrentRequests-1 {
		t.Fatalf(
			"rate limited requests = %d, expected %d",
			rateLimitedCount,
			concurrentRequests-1,
		)
	}

	var eventCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_request_events
			WHERE phone_number = $1
		`,
		phoneNumber,
	).Scan(
		&eventCount,
	)
	if err != nil {
		t.Fatalf(
			"count OTP request events: %v",
			err,
		)
	}

	if eventCount != 1 {
		t.Fatalf(
			"OTP request event count = %d, expected 1",
			eventCount,
		)
	}
}

func TestOTPRequestRateLimiterBlocksSixthRequestInsideWindow(
	t *testing.T,
) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL is required for integration test")
	}

	ctx := context.Background()

	pool, err := database.NewPostgresPool(
		ctx,
		databaseURL,
	)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}

	t.Cleanup(pool.Close)

	const phoneNumber = "+9647500000021"

	_, err = pool.Exec(
		ctx,
		`
			DELETE FROM otp_request_events
			WHERE phone_number = $1
		`,
		phoneNumber,
	)
	if err != nil {
		t.Fatalf(
			"clean existing OTP request events: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM otp_request_events
				WHERE phone_number = $1
			`,
			phoneNumber,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean OTP request events: %v",
				cleanupErr,
			)
		}
	})

	rateLimiter := NewOTPRequestRateLimiter(pool)

	policy := auth.OTPRequestRateLimitPolicy{
		Cooldown:    time.Minute,
		Window:      15 * time.Minute,
		MaxRequests: 5,
	}

	startTime := time.Date(
		2026,
		time.August,
		10,
		6,
		0,
		0,
		0,
		time.UTC,
	)

	for request := 0; request < 5; request++ {
		requestedAt := startTime.Add(
			time.Duration(request) * 61 * time.Second,
		)

		err := rateLimiter.Allow(
			ctx,
			phoneNumber,
			requestedAt,
			policy,
		)
		if err != nil {
			t.Fatalf(
				"request %d returned error: %v",
				request+1,
				err,
			)
		}
	}

	sixthRequestAt := startTime.Add(
		5 * 61 * time.Second,
	)

	err = rateLimiter.Allow(
		ctx,
		phoneNumber,
		sixthRequestAt,
		policy,
	)

	if !errors.Is(
		err,
		auth.ErrOTPRequestRateLimited,
	) {
		t.Fatalf(
			"sixth request returned %v, expected %v",
			err,
			auth.ErrOTPRequestRateLimited,
		)
	}

	var eventCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_request_events
			WHERE phone_number = $1
		`,
		phoneNumber,
	).Scan(
		&eventCount,
	)
	if err != nil {
		t.Fatalf(
			"count OTP request events: %v",
			err,
		)
	}

	if eventCount != 5 {
		t.Fatalf(
			"OTP request event count = %d, expected 5",
			eventCount,
		)
	}
}