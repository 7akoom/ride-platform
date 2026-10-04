package deletion

import (
	"context"
	"time"
)

const eraseBatch = 20

// RunEraser erases accounts whose grace period is over, every interval,
// until ctx ends.
func (s *Service) RunEraser(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		s.EraseDue(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// EraseDue erases the accounts due now and returns how many it erased.
// Before erasing it checks again that nothing stands in the way (a trip may
// have started with a token issued before the sessions ended); if something
// does, or a service does not answer, it tries again later. It erases the
// person's files first, so a failure there leaves the account to be tried
// again rather than erased with files left behind.
func (s *Service) EraseDue(ctx context.Context) int {
	due, err := s.store.Due(ctx, s.clock.Now(), eraseBatch)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to list the accounts to erase", "error", err)

		return 0
	}

	erased := 0

	for _, deletion := range due {
		if ctx.Err() != nil {
			return erased
		}

		if s.erase(ctx, deletion) {
			erased++
		}
	}

	return erased
}

func (s *Service) erase(ctx context.Context, deletion Deletion) bool {
	postpone := func(reason string) {
		if err := s.store.Postpone(ctx, deletion.IdentityID, s.clock.Now().Add(s.settings.RetryAfter), reason); err != nil {
			s.logger.ErrorContext(ctx, "failed to postpone erasing an account", "identity_id", deletion.IdentityID, "error", err)
		}
	}

	standing, err := s.accounts.Standing(ctx, deletion.IdentityID)
	if err != nil {
		s.logger.WarnContext(ctx, "an account due for erasing could not be checked", "identity_id", deletion.IdentityID, "error", err)
		postpone("check: " + err.Error())

		return false
	}

	if len(standing.Blockers) > 0 {
		blocked := &BlockedError{Blockers: standing.Blockers}
		s.logger.InfoContext(ctx, "an account due for erasing has something in the way", "identity_id", deletion.IdentityID, "blockers", blocked.Error())
		postpone(blocked.Error())

		return false
	}

	if err := s.media.DeleteOwnerMedia(ctx, deletion.IdentityID); err != nil {
		s.logger.WarnContext(ctx, "failed to erase the files of an account", "identity_id", deletion.IdentityID, "error", err)
		postpone("files: " + err.Error())

		return false
	}

	// A profile created after the deletion was asked (unlikely: no session)
	// is erased too; one that disappeared is kept as recorded.
	riderID, driverID := firstNonEmpty(standing.RiderID, deletion.RiderID), firstNonEmpty(standing.DriverID, deletion.DriverID)

	done, err := s.store.Complete(ctx, CompleteInput{
		IdentityID:  deletion.IdentityID,
		RiderID:     riderID,
		DriverID:    driverID,
		CompletedAt: s.clock.Now(),
	})
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to erase an account", "identity_id", deletion.IdentityID, "error", err)
		postpone("erase: " + err.Error())

		return false
	}

	if done {
		s.logger.InfoContext(ctx, "account erased", "identity_id", deletion.IdentityID)
	}

	return done
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}
