//go:build integration

package postgres

func (f *cleanupStoreIntegrationFixture) remainingOTPRequestEvents() int {
	f.t.Helper()

	var count int

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT COUNT(*)
			FROM otp_request_events
			WHERE identifier_type = 'phone'
			  AND normalized_value = $1
			  AND purpose = 'login'
			  AND target_identity_id IS NULL
		`,
		f.phoneNumber,
	).Scan(
		&count,
	)
	if err != nil {
		f.t.Fatalf(
			"count remaining OTP request events: %v",
			err,
		)
	}

	return count
}

func (f *cleanupStoreIntegrationFixture) remainingOTPChallenges() int {
	f.t.Helper()

	var count int

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT COUNT(*)
			FROM otp_challenges
			WHERE identifier_type = 'phone'
			  AND normalized_value = $1
			  AND purpose = 'login'
			  AND target_identity_id IS NULL
		`,
		f.phoneNumber,
	).Scan(
		&count,
	)
	if err != nil {
		f.t.Fatalf(
			"count remaining OTP challenges: %v",
			err,
		)
	}

	return count
}

func (f *cleanupStoreIntegrationFixture) remainingSessions(
	identityID string,
) int {
	f.t.Helper()

	var count int

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT COUNT(*)
			FROM auth_sessions
			WHERE identity_id = $1::uuid
		`,
		identityID,
	).Scan(
		&count,
	)
	if err != nil {
		f.t.Fatalf(
			"count remaining authentication sessions: %v",
			err,
		)
	}

	return count
}

func (f *cleanupStoreIntegrationFixture) remainingRefreshTokens(
	identityID string,
) int {
	f.t.Helper()

	var count int

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT COUNT(*)
			FROM refresh_tokens rt
			JOIN auth_sessions s
				ON s.id = rt.session_id
			WHERE s.identity_id = $1::uuid
		`,
		identityID,
	).Scan(
		&count,
	)
	if err != nil {
		f.t.Fatalf(
			"count remaining refresh tokens: %v",
			err,
		)
	}

	return count
}

func (f *cleanupStoreIntegrationFixture) refreshTokenExists(
	tokenHash string,
) bool {
	f.t.Helper()

	var exists bool

	err := f.pool.QueryRow(
		f.ctx,
		`
			SELECT EXISTS (
				SELECT 1
				FROM refresh_tokens
				WHERE token_hash = $1
			)
		`,
		tokenHash,
	).Scan(
		&exists,
	)
	if err != nil {
		f.t.Fatalf(
			"query retained refresh token: %v",
			err,
		)
	}

	return exists
}
