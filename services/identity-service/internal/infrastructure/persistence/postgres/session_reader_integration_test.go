//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/database"
)

func TestSessionReaderListsOnlyActiveIdentitySessionsNewestFirst(
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
		t.Fatalf(
			"connect to PostgreSQL: %v",
			err,
		)
	}

	t.Cleanup(pool.Close)

	now := time.Now().
		UTC().
		Truncate(time.Microsecond)

	var identityID string

	err = pool.QueryRow(
		ctx,
		`
			INSERT INTO identities (
				created_at,
				updated_at
			)
			VALUES ($1, $1)
			RETURNING id::text
		`,
		now.Add(-48*time.Hour),
	).Scan(
		&identityID,
	)
	if err != nil {
		t.Fatalf(
			"create test identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			`
				DELETE FROM identities
				WHERE id = $1::uuid
			`,
			identityID,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean test identity: %v",
				cleanupErr,
			)
		}
	})

	type sessionFixture struct {
		clientID   *string
		deviceID   *string
		deviceName *string
		platform   *string
		appVersion *string
		ipAddress  *string
		userAgent  *string

		expiresAt  time.Time
		revokedAt  *time.Time
		lastSeenAt *time.Time
		createdAt  time.Time
	}

	stringPointer := func(value string) *string {
		return &value
	}

	timePointer := func(value time.Time) *time.Time {
		return &value
	}

	olderCreatedAt := now.Add(-2 * time.Hour)
	olderLastSeenAt := now.Add(-30 * time.Minute)

	newerCreatedAt := now.Add(-1 * time.Hour)
	newerLastSeenAt := now.Add(-10 * time.Minute)

	expiredCreatedAt := now.Add(-24 * time.Hour)

	revokedCreatedAt := now.Add(-3 * time.Hour)
	revokedAt := now.Add(-20 * time.Minute)

	fixtures := []sessionFixture{
		{
			clientID: stringPointer(
				"customer-app",
			),
			deviceID: stringPointer(
				"device-older",
			),
			deviceName: stringPointer(
				"Older Device",
			),
			platform: stringPointer(
				"android",
			),
			appVersion: stringPointer(
				"1.0.0",
			),
			ipAddress: stringPointer(
				"203.0.113.10",
			),
			userAgent: stringPointer(
				"ride-platform-test/older",
			),
			expiresAt: now.Add(
				7 * 24 * time.Hour,
			),
			lastSeenAt: timePointer(
				olderLastSeenAt,
			),
			createdAt: olderCreatedAt,
		},
		{
			clientID: stringPointer(
				"customer-app",
			),
			deviceID: stringPointer(
				"device-newer",
			),
			deviceName: stringPointer(
				"Newer Device",
			),
			platform: stringPointer(
				"ios",
			),
			appVersion: stringPointer(
				"2.0.0",
			),
			ipAddress: stringPointer(
				"203.0.113.11",
			),
			userAgent: stringPointer(
				"ride-platform-test/newer",
			),
			expiresAt: now.Add(
				10 * 24 * time.Hour,
			),
			lastSeenAt: timePointer(
				newerLastSeenAt,
			),
			createdAt: newerCreatedAt,
		},
		{
			expiresAt: now.Add(
				-1 * time.Hour,
			),
			createdAt: expiredCreatedAt,
		},
		{
			expiresAt: now.Add(
				5 * 24 * time.Hour,
			),
			revokedAt: timePointer(
				revokedAt,
			),
			createdAt: revokedCreatedAt,
		},
	}

	sessionIDs := make(
		[]string,
		0,
		len(fixtures),
	)

	for _, fixture := range fixtures {
		var sessionID string

		err = pool.QueryRow(
			ctx,
			`
				INSERT INTO auth_sessions (
					identity_id,
					client_id,
					device_id,
					device_name,
					platform,
					app_version,
					ip_address,
					user_agent,
					expires_at,
					revoked_at,
					last_seen_at,
					created_at,
					updated_at
				)
				VALUES (
					$1::uuid,
					$2,
					$3,
					$4,
					$5,
					$6,
					$7::inet,
					$8,
					$9,
					$10,
					$11,
					$12,
					$12
				)
				RETURNING id::text
			`,
			identityID,
			fixture.clientID,
			fixture.deviceID,
			fixture.deviceName,
			fixture.platform,
			fixture.appVersion,
			fixture.ipAddress,
			fixture.userAgent,
			fixture.expiresAt,
			fixture.revokedAt,
			fixture.lastSeenAt,
			fixture.createdAt,
		).Scan(
			&sessionID,
		)
		if err != nil {
			t.Fatalf(
				"create authentication session: %v",
				err,
			)
		}

		sessionIDs = append(
			sessionIDs,
			sessionID,
		)
	}

	var otherIdentityID string

	err = pool.QueryRow(
		ctx,
		`
		INSERT INTO identities (
			created_at,
			updated_at
		)
		VALUES ($1, $1)
		RETURNING id::text
	`,
		now.Add(-24*time.Hour),
	).Scan(
		&otherIdentityID,
	)
	if err != nil {
		t.Fatalf(
			"create other test identity: %v",
			err,
		)
	}

	t.Cleanup(func() {
		_, cleanupErr := pool.Exec(
			context.Background(),
			`
			DELETE FROM identities
			WHERE id = $1::uuid
		`,
			otherIdentityID,
		)
		if cleanupErr != nil {
			t.Errorf(
				"clean other test identity: %v",
				cleanupErr,
			)
		}
	})

	_, err = pool.Exec(
		ctx,
		`
		INSERT INTO auth_sessions (
			identity_id,
			expires_at,
			created_at,
			updated_at
		)
		VALUES (
			$1::uuid,
			$2,
			$3,
			$3
		)
	`,
		otherIdentityID,
		now.Add(30*24*time.Hour),
		now.Add(-5*time.Minute),
	)
	if err != nil {
		t.Fatalf(
			"create other identity authentication session: %v",
			err,
		)
	}

	reader := NewSessionReader(
		pool,
	)

	sessions, err := reader.ListActiveByIdentity(
		ctx,
		identityID,
		now,
	)
	if err != nil {
		t.Fatalf(
			"ListActiveByIdentity() returned an error: %v",
			err,
		)
	}

	if len(sessions) != 2 {
		t.Fatalf(
			"ListActiveByIdentity() returned %d sessions, expected 2",
			len(sessions),
		)
	}

	newerSession := sessions[0]

	if newerSession.ID != sessionIDs[1] {
		t.Errorf(
			"newest session ID = %q, expected %q",
			newerSession.ID,
			sessionIDs[1],
		)
	}

	if newerSession.DeviceID == nil ||
		*newerSession.DeviceID != "device-newer" {
		t.Errorf(
			"newest session device ID = %v, expected %q",
			newerSession.DeviceID,
			"device-newer",
		)
	}

	if newerSession.DeviceName == nil ||
		*newerSession.DeviceName != "Newer Device" {
		t.Errorf(
			"newest session device name = %v, expected %q",
			newerSession.DeviceName,
			"Newer Device",
		)
	}

	if newerSession.Platform == nil ||
		*newerSession.Platform != "ios" {
		t.Errorf(
			"newest session platform = %v, expected %q",
			newerSession.Platform,
			"ios",
		)
	}

	if newerSession.IPAddress == nil ||
		*newerSession.IPAddress != "203.0.113.11" {
		t.Errorf(
			"newest session IP address = %v, expected %q",
			newerSession.IPAddress,
			"203.0.113.11",
		)
	}

	if newerSession.LastSeenAt == nil ||
		!newerSession.LastSeenAt.Equal(
			newerLastSeenAt,
		) {
		t.Errorf(
			"newest session last seen at = %v, expected %v",
			newerSession.LastSeenAt,
			newerLastSeenAt,
		)
	}

	if !newerSession.CreatedAt.Equal(
		newerCreatedAt,
	) {
		t.Errorf(
			"newest session created at = %v, expected %v",
			newerSession.CreatedAt,
			newerCreatedAt,
		)
	}

	olderSession := sessions[1]

	if olderSession.ID != sessionIDs[0] {
		t.Errorf(
			"older session ID = %q, expected %q",
			olderSession.ID,
			sessionIDs[0],
		)
	}
}
