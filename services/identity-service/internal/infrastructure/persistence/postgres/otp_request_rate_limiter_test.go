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
		auth.OTPRequestScope{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: "+9647500000000",
			},
			Purpose: auth.OTPPurposeLogin,
		},
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
func TestOTPRequestRateLimiterRejectsMissingSourceWhenAbuseLimitEnabled(
	t *testing.T,
) {
	rateLimiter := &OTPRequestRateLimiter{}

	err := rateLimiter.Allow(
		context.Background(),
		auth.OTPRequestScope{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: "+9647500000050",
			},
			Purpose: auth.OTPPurposeLogin,
		},
		time.Now().UTC(),
		auth.OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
			Abuse: auth.OTPRequestAbuseLimitPolicy{
				Window:      10 * time.Minute,
				MaxRequests: 30,
			},
		},
	)

	if err == nil {
		t.Fatal(
			"Allow() accepted a missing source IP address while abuse limit is enabled",
		)
	}
}

func TestOTPRequestRateLimiterRejectsInvalidSourceIPAddress(
	t *testing.T,
) {
	rateLimiter := &OTPRequestRateLimiter{}

	err := rateLimiter.Allow(
		context.Background(),
		auth.OTPRequestScope{
			Identifier: auth.Identifier{
				Type:  auth.IdentifierTypePhone,
				Value: "+9647500000051",
			},
			Purpose:         auth.OTPPurposeLogin,
			SourceIPAddress: "not-an-ip-address",
		},
		time.Now().UTC(),
		auth.OTPRequestRateLimitPolicy{
			Cooldown:    time.Minute,
			Window:      15 * time.Minute,
			MaxRequests: 5,
		},
	)

	if err == nil {
		t.Fatal(
			"Allow() accepted an invalid source IP address",
		)
	}
}
