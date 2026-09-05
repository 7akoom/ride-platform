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
	"github.com/jackc/pgx/v5/pgxpool"
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
			WHERE identifier_type = 'phone'
			AND normalized_value = $1
			AND purpose = 'login'
			AND target_identity_id IS NULL
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
				WHERE identifier_type = 'phone'
				AND normalized_value = $1
				AND purpose = 'login'
				AND target_identity_id IS NULL
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
				auth.OTPRequestScope{
					Identifier: auth.Identifier{
						Type:  auth.IdentifierTypePhone,
						Value: phoneNumber,
					},
					Purpose: auth.OTPPurposeLogin,
				},
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
			WHERE identifier_type = 'phone'
			AND normalized_value = $1
			AND purpose = 'login'
			AND target_identity_id IS NULL
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
			WHERE identifier_type = 'phone'
			AND normalized_value = $1
			AND purpose = 'login'
			AND target_identity_id IS NULL
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
				WHERE identifier_type = 'phone'
				AND normalized_value = $1
				AND purpose = 'login'
				AND target_identity_id IS NULL
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
			auth.OTPRequestScope{
				Identifier: auth.Identifier{
					Type:  auth.IdentifierTypePhone,
					Value: phoneNumber,
				},
				Purpose: auth.OTPPurposeLogin,
			},
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
		auth.OTPRequestScope{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumber,
			},
			Purpose: auth.OTPPurposeLogin,
		},
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
			WHERE identifier_type = 'phone'
			AND normalized_value = $1
			AND purpose = 'login'
			AND target_identity_id IS NULL
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
func TestOTPRequestRateLimiterAppliesAbuseLimitAcrossIdentifiersFromSameSource(
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

	const sourceIPAddress = "203.0.113.70"
	const alternateSourceIPAddress = "203.0.113.71"

	phoneNumbers := []string{
		"+9647500000030",
		"+9647500000031",
		"+9647500000032",
		"+9647500000033",
	}

	cleanOTPRequestRateLimiterEvents(
		t,
		pool,
		sourceIPAddress,
		phoneNumbers,
	)
	cleanOTPRequestRateLimiterEvents(
		t,
		pool,
		alternateSourceIPAddress,
		phoneNumbers,
	)

	t.Cleanup(func() {
		cleanOTPRequestRateLimiterEvents(
			t,
			pool,
			sourceIPAddress,
			phoneNumbers,
		)
		cleanOTPRequestRateLimiterEvents(
			t,
			pool,
			alternateSourceIPAddress,
			phoneNumbers,
		)
	})

	rateLimiter := NewOTPRequestRateLimiter(pool)

	policy := auth.OTPRequestRateLimitPolicy{
		Cooldown:    time.Minute,
		Window:      15 * time.Minute,
		MaxRequests: 5,
		Abuse: auth.OTPRequestAbuseLimitPolicy{
			Window:      10 * time.Minute,
			MaxRequests: 3,
		},
	}

	now := time.Now().UTC()

	for index := 0; index < 3; index++ {
		err := rateLimiter.Allow(
			ctx,
			auth.OTPRequestScope{
				Identifier: auth.Identifier{
					Type:  auth.IdentifierTypePhone,
					Value: phoneNumbers[index],
				},
				Purpose:         auth.OTPPurposeLogin,
				SourceIPAddress: sourceIPAddress,
			},
			now,
			policy,
		)
		if err != nil {
			t.Fatalf(
				"allowed source request %d returned error: %v",
				index+1,
				err,
			)
		}
	}

	err = rateLimiter.Allow(
		ctx,
		auth.OTPRequestScope{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumbers[3],
			},
			Purpose:         auth.OTPPurposeLogin,
			SourceIPAddress: sourceIPAddress,
		},
		now,
		policy,
	)

	if !errors.Is(
		err,
		auth.ErrOTPRequestRateLimited,
	) {
		t.Fatalf(
			"fourth source request returned %v, expected %v",
			err,
			auth.ErrOTPRequestRateLimited,
		)
	}

	err = rateLimiter.Allow(
		ctx,
		auth.OTPRequestScope{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumbers[3],
			},
			Purpose:         auth.OTPPurposeLogin,
			SourceIPAddress: alternateSourceIPAddress,
		},
		now,
		policy,
	)
	if err != nil {
		t.Fatalf(
			"same identifier from alternate source returned error: %v",
			err,
		)
	}

	var sourceEventCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_request_events
			WHERE source_ip_address = $1::inet
		`,
		sourceIPAddress,
	).Scan(
		&sourceEventCount,
	)
	if err != nil {
		t.Fatalf(
			"count source OTP request events: %v",
			err,
		)
	}

	if sourceEventCount != 3 {
		t.Fatalf(
			"source OTP request event count = %d, expected 3",
			sourceEventCount,
		)
	}

	var alternateSourceEventCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_request_events
			WHERE source_ip_address = $1::inet
		`,
		alternateSourceIPAddress,
	).Scan(
		&alternateSourceEventCount,
	)
	if err != nil {
		t.Fatalf(
			"count alternate source OTP request events: %v",
			err,
		)
	}

	if alternateSourceEventCount != 1 {
		t.Fatalf(
			"alternate source OTP request event count = %d, expected 1",
			alternateSourceEventCount,
		)
	}
}

func TestOTPRequestRateLimiterIdentifierRejectionDoesNotConsumeSourceQuota(
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

	const firstSourceIPAddress = "203.0.113.72"
	const secondSourceIPAddress = "203.0.113.73"

	phoneNumbers := []string{
		"+9647500000034",
		"+9647500000035",
	}

	cleanOTPRequestRateLimiterEvents(
		t,
		pool,
		firstSourceIPAddress,
		phoneNumbers,
	)
	cleanOTPRequestRateLimiterEvents(
		t,
		pool,
		secondSourceIPAddress,
		phoneNumbers,
	)

	t.Cleanup(func() {
		cleanOTPRequestRateLimiterEvents(
			t,
			pool,
			firstSourceIPAddress,
			phoneNumbers,
		)
		cleanOTPRequestRateLimiterEvents(
			t,
			pool,
			secondSourceIPAddress,
			phoneNumbers,
		)
	})

	rateLimiter := NewOTPRequestRateLimiter(pool)

	policy := auth.OTPRequestRateLimitPolicy{
		Cooldown:    time.Minute,
		Window:      15 * time.Minute,
		MaxRequests: 5,
		Abuse: auth.OTPRequestAbuseLimitPolicy{
			Window:      10 * time.Minute,
			MaxRequests: 1,
		},
	}

	now := time.Now().UTC()

	err = rateLimiter.Allow(
		ctx,
		auth.OTPRequestScope{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumbers[0],
			},
			Purpose:         auth.OTPPurposeLogin,
			SourceIPAddress: firstSourceIPAddress,
		},
		now,
		policy,
	)
	if err != nil {
		t.Fatalf(
			"initial OTP request returned error: %v",
			err,
		)
	}

	err = rateLimiter.Allow(
		ctx,
		auth.OTPRequestScope{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumbers[0],
			},
			Purpose:         auth.OTPPurposeLogin,
			SourceIPAddress: secondSourceIPAddress,
		},
		now.Add(10*time.Second),
		policy,
	)

	if !errors.Is(
		err,
		auth.ErrOTPRequestRateLimited,
	) {
		t.Fatalf(
			"identifier cooldown request returned %v, expected %v",
			err,
			auth.ErrOTPRequestRateLimited,
		)
	}

	err = rateLimiter.Allow(
		ctx,
		auth.OTPRequestScope{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumbers[1],
			},
			Purpose:         auth.OTPPurposeLogin,
			SourceIPAddress: secondSourceIPAddress,
		},
		now.Add(10*time.Second),
		policy,
	)
	if err != nil {
		t.Fatalf(
			"request after identifier rejection returned error: %v",
			err,
		)
	}

	var sourceEventCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_request_events
			WHERE source_ip_address = $1::inet
		`,
		secondSourceIPAddress,
	).Scan(
		&sourceEventCount,
	)
	if err != nil {
		t.Fatalf(
			"count second source OTP request events: %v",
			err,
		)
	}

	if sourceEventCount != 1 {
		t.Fatalf(
			"second source OTP request event count = %d, expected 1",
			sourceEventCount,
		)
	}
}

func TestOTPRequestRateLimiterSerializesConcurrentRequestsFromSameSource(
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

	const sourceIPAddress = "203.0.113.74"

	phoneNumbers := []string{
		"+9647500000040",
		"+9647500000041",
		"+9647500000042",
		"+9647500000043",
		"+9647500000044",
		"+9647500000045",
		"+9647500000046",
		"+9647500000047",
		"+9647500000048",
		"+9647500000049",
	}

	cleanOTPRequestRateLimiterEvents(
		t,
		pool,
		sourceIPAddress,
		phoneNumbers,
	)

	t.Cleanup(func() {
		cleanOTPRequestRateLimiterEvents(
			t,
			pool,
			sourceIPAddress,
			phoneNumbers,
		)
	})

	rateLimiter := NewOTPRequestRateLimiter(pool)

	policy := auth.OTPRequestRateLimitPolicy{
		Cooldown:    time.Minute,
		Window:      15 * time.Minute,
		MaxRequests: 5,
		Abuse: auth.OTPRequestAbuseLimitPolicy{
			Window:      10 * time.Minute,
			MaxRequests: 3,
		},
	}

	now := time.Now().UTC()

	start := make(chan struct{})
	results := make(chan error, len(phoneNumbers))

	var waitGroup sync.WaitGroup

	for _, phoneNumber := range phoneNumbers {
		phoneNumber := phoneNumber

		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			<-start

			results <- rateLimiter.Allow(
				ctx,
				auth.OTPRequestScope{
					Identifier: auth.Identifier{
						Type:  auth.IdentifierTypePhone,
						Value: phoneNumber,
					},
					Purpose:         auth.OTPPurposeLogin,
					SourceIPAddress: sourceIPAddress,
				},
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

	if successCount != 3 {
		t.Fatalf(
			"successful source requests = %d, expected 3",
			successCount,
		)
	}

	if rateLimitedCount != len(phoneNumbers)-3 {
		t.Fatalf(
			"rate limited source requests = %d, expected %d",
			rateLimitedCount,
			len(phoneNumbers)-3,
		)
	}

	var eventCount int

	err = pool.QueryRow(
		ctx,
		`
			SELECT COUNT(*)
			FROM otp_request_events
			WHERE source_ip_address = $1::inet
		`,
		sourceIPAddress,
	).Scan(
		&eventCount,
	)
	if err != nil {
		t.Fatalf(
			"count concurrent source OTP request events: %v",
			err,
		)
	}

	if eventCount != 3 {
		t.Fatalf(
			"concurrent source OTP request event count = %d, expected 3",
			eventCount,
		)
	}
}

func cleanOTPRequestRateLimiterEvents(
	t *testing.T,
	pool *pgxpool.Pool,
	sourceIPAddress string,
	identifiers []string,
) {
	t.Helper()

	_, err := pool.Exec(
		context.Background(),
		`
			DELETE FROM otp_request_events
			WHERE source_ip_address = $1::inet
			OR normalized_value = ANY($2::text[])
		`,
		sourceIPAddress,
		identifiers,
	)
	if err != nil {
		t.Fatalf(
			"clean OTP request rate limiter events: %v",
			err,
		)
	}
}
func TestOTPRequestRateLimiterCanonicalizesIPv4MappedIPv6Source(
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

	const mappedSourceIPAddress = "::ffff:203.0.113.80"
	const canonicalSourceIPAddress = "203.0.113.80"

	phoneNumbers := []string{
		"+9647500000052",
		"+9647500000053",
	}

	cleanOTPRequestRateLimiterEvents(
		t,
		pool,
		canonicalSourceIPAddress,
		phoneNumbers,
	)

	t.Cleanup(func() {
		cleanOTPRequestRateLimiterEvents(
			t,
			pool,
			canonicalSourceIPAddress,
			phoneNumbers,
		)
	})

	rateLimiter := NewOTPRequestRateLimiter(pool)

	policy := auth.OTPRequestRateLimitPolicy{
		Cooldown:    time.Minute,
		Window:      15 * time.Minute,
		MaxRequests: 5,
		Abuse: auth.OTPRequestAbuseLimitPolicy{
			Window:      10 * time.Minute,
			MaxRequests: 1,
		},
	}

	now := time.Now().UTC()

	err = rateLimiter.Allow(
		ctx,
		auth.OTPRequestScope{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumbers[0],
			},
			Purpose:         auth.OTPPurposeLogin,
			SourceIPAddress: mappedSourceIPAddress,
		},
		now,
		policy,
	)
	if err != nil {
		t.Fatalf(
			"mapped IPv6 source request returned error: %v",
			err,
		)
	}

	var storedSourceIPAddress string

	err = pool.QueryRow(
		ctx,
		`
			SELECT host(source_ip_address)
			FROM otp_request_events
			WHERE normalized_value = $1
			AND purpose = 'login'
		`,
		phoneNumbers[0],
	).Scan(
		&storedSourceIPAddress,
	)
	if err != nil {
		t.Fatalf(
			"query stored source IP address: %v",
			err,
		)
	}

	if storedSourceIPAddress != canonicalSourceIPAddress {
		t.Fatalf(
			"stored source IP address = %q, expected %q",
			storedSourceIPAddress,
			canonicalSourceIPAddress,
		)
	}

	err = rateLimiter.Allow(
		ctx,
		auth.OTPRequestScope{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: phoneNumbers[1],
			},
			Purpose:         auth.OTPPurposeLogin,
			SourceIPAddress: canonicalSourceIPAddress,
		},
		now,
		policy,
	)

	if !errors.Is(
		err,
		auth.ErrOTPRequestRateLimited,
	) {
		t.Fatalf(
			"canonical IPv4 source request returned %v, expected %v",
			err,
			auth.ErrOTPRequestRateLimited,
		)
	}
}
