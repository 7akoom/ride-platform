package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestNewOTPRequestRateLimiterPanicsWhenPoolIsNil(
	t *testing.T,
) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"NewOTPRequestRateLimiter() did not panic for nil PostgreSQL pool",
			)
		}
	}()

	NewOTPRequestRateLimiter(nil)
}

func TestOTPRequestRateLimiterRejectsZeroRequestTime(
	t *testing.T,
) {
	rateLimiter := &OTPRequestRateLimiter{}

	err := rateLimiter.Allow(
		context.Background(),
		"+9647500000000",
		time.Time{},
		auth.OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
	)

	if err == nil {
		t.Fatal(
			"Allow() accepted a zero OTP request time",
		)
	}
}
