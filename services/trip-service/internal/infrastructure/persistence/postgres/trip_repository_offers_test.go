package postgres

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

func TestOfferInsertViolationsBecomeTheRefusalsOfTheChecksBeforeIt(t *testing.T) {
	cases := map[string]error{
		offerPerDriverIndex: trip.ErrDriverHasPendingOffer,
		offerPerTripIndex:   trip.ErrOfferInProgress,
		offerPairKey:        trip.ErrAlreadyOffered,
	}

	for constraint, want := range cases {
		violation := &pgconn.PgError{Code: pgUniqueViolation, ConstraintName: constraint}

		// A wrapped violation is recognised too, as pgx wraps errors on some paths.
		for _, err := range []error{violation, fmt.Errorf("query: %w", violation)} {
			if got := mapOfferInsertError(err); !errors.Is(got, want) {
				t.Errorf("%s: expected %v, got %v", constraint, want, got)
			}
		}
	}
}

func TestOtherInsertErrorsAreNotMistakenForRefusals(t *testing.T) {
	for name, err := range map[string]error{
		"a unique violation of some other constraint": &pgconn.PgError{Code: pgUniqueViolation, ConstraintName: "something_else"},
		"a foreign key violation":                     &pgconn.PgError{Code: "23503", ConstraintName: offerPerTripIndex},
		"a plain error":                               errors.New("connection reset"),
	} {
		got := mapOfferInsertError(err)

		for _, refusal := range []error{trip.ErrDriverHasPendingOffer, trip.ErrOfferInProgress, trip.ErrAlreadyOffered} {
			if errors.Is(got, refusal) {
				t.Errorf("%s was mistaken for %v", name, refusal)
			}
		}

		if !errors.Is(got, err) {
			t.Errorf("%s: the original error must stay reachable, got %v", name, got)
		}
	}
}

// The unique-index names the code matches on are the ones migration 00009 creates.
func TestTheConstraintNamesMatchTheMigration(t *testing.T) {
	migration := readMigration(t, "../../../../migrations/00009_create_trip_offers.sql")

	for _, name := range []string{offerPerDriverIndex, offerPerTripIndex, offerPairKey} {
		if !strings.Contains(migration, name) {
			t.Errorf("migration 00009 does not define %q", name)
		}
	}
}

func TestEveryOfferStatementThatChangesStateIsGuardedByStatus(t *testing.T) {
	for name, statement := range map[string]string{
		"reject":           sqlRejectOffer,
		"expire stale":     sqlExpireStaleOffers,
		"lock the offer":   sqlLockOffer,
		"find the pending": sqlFindPendingOffer,
	} {
		if !strings.Contains(statement, "'pending'") {
			t.Errorf("%s: must only ever touch pending offers", name)
		}
	}

	if !strings.Contains(sqlLockOffer, "FOR UPDATE") || !strings.Contains(sqlLockTripStatus, "FOR UPDATE") {
		t.Error("the trip and the offer must be locked before they are changed")
	}

	if !strings.Contains(sqlFindPendingOffer, "t.status = 'requested'") {
		t.Error("an offer of a trip that is no longer waiting must not be shown")
	}

	if !strings.Contains(sqlAcceptTripForOffer, historyColumns) {
		t.Error("the accept statement must return the columns scanTrip reads")
	}
}
