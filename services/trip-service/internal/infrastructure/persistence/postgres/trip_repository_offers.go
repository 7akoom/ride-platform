package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

const (
	pgUniqueViolation = "23505"

	// Names of the unique constraints and indexes of trip_offers (migration 00009).
	offerPerTripIndex   = "trip_offers_one_pending_per_trip"
	offerPerDriverIndex = "trip_offers_one_pending_per_driver"
	offerPairKey        = "trip_offers_trip_driver_key"
)

// The statements behind driver offers. They are constants so that the SQL the
// database tests exercise is exactly the SQL this file runs.
const (
	sqlLockTripStatus = `SELECT status FROM trips WHERE id = $1 FOR UPDATE`

	// A live offer is a pending one that has not expired and whose trip is still
	// waiting. Anything else that is still marked pending is stale and is closed
	// here, so it can never block a new offer.
	sqlExpireStaleOffers = `UPDATE trip_offers o
        SET status = 'expired'
        FROM trips t
        WHERE t.id = o.trip_id
          AND o.status = 'pending'
          AND (o.trip_id = $1 OR o.driver_id = $2)
          AND (o.expires_at <= clock_timestamp() OR t.status <> 'requested')`

	sqlOfferedBefore         = `SELECT EXISTS (SELECT 1 FROM trip_offers WHERE trip_id = $1 AND driver_id = $2)`
	sqlPendingOfferOnTrip    = `SELECT EXISTS (SELECT 1 FROM trip_offers WHERE trip_id = $1 AND status = 'pending')`
	sqlDriverHasPendingOffer = `SELECT EXISTS (SELECT 1 FROM trip_offers WHERE driver_id = $1 AND status = 'pending')`
	sqlDriverHasActiveTrip   = `SELECT EXISTS (SELECT 1 FROM trips WHERE driver_id = $1 AND status IN ('accepted', 'in_progress'))`

	sqlInsertOffer = `INSERT INTO trip_offers (trip_id, driver_id, offered_at, expires_at)
        VALUES ($1, $2, clock_timestamp(), clock_timestamp() + make_interval(secs => $3::double precision))
        RETURNING offered_at, expires_at`

	sqlFindPendingOffer = `SELECT o.trip_id, o.offered_at, o.expires_at
        FROM trip_offers o
        JOIN trips t ON t.id = o.trip_id
        WHERE o.driver_id = $1
          AND o.status = 'pending'
          AND o.expires_at > clock_timestamp()
          AND t.status = 'requested'
        ORDER BY o.offered_at DESC
        LIMIT 1`

	sqlLockOffer = `SELECT id, expires_at > clock_timestamp()
        FROM trip_offers
        WHERE trip_id = $1 AND driver_id = $2 AND status = 'pending'
        FOR UPDATE`

	sqlMarkOfferAccepted = `UPDATE trip_offers SET status = 'accepted', responded_at = clock_timestamp() WHERE id = $1`

	sqlRejectOffer = `UPDATE trip_offers
        SET status = 'rejected', responded_at = clock_timestamp()
        WHERE trip_id = $1 AND driver_id = $2 AND status = 'pending' AND expires_at > clock_timestamp()`

	sqlAcceptTripForOffer = `UPDATE trips
        SET driver_id = $2,
            status = 'accepted',
            accepted_at = CURRENT_TIMESTAMP,
            updated_at = CURRENT_TIMESTAMP
        WHERE id = $1
        RETURNING ` + historyColumns
)

// CreateOffer offers the trip to the driver. See trip.OfferStore.
//
// The trip row is locked first, so offers of one trip are serialized; the unique
// indexes of migration 00009 are the backstop for the driver side, where two trips
// can be offered to the same driver at the same moment.
func (r *TripRepository) CreateOffer(
	ctx context.Context,
	tripID string,
	driverID string,
	ttl time.Duration,
) (trip.Offer, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return trip.Offer{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string

	if err := tx.QueryRow(ctx, sqlLockTripStatus, tripID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return trip.Offer{}, trip.ErrTripNotFound
		}

		return trip.Offer{}, fmt.Errorf("lock trip: %w", err)
	}

	if trip.Status(status) != trip.StatusRequested {
		return trip.Offer{}, trip.ErrTripNotOfferable
	}

	if _, err := tx.Exec(ctx, sqlExpireStaleOffers, tripID, driverID); err != nil {
		return trip.Offer{}, fmt.Errorf("close stale offers: %w", err)
	}

	checks := []struct {
		query   string
		args    []any
		refusal error
	}{
		{sqlOfferedBefore, []any{tripID, driverID}, trip.ErrAlreadyOffered},
		{sqlPendingOfferOnTrip, []any{tripID}, trip.ErrOfferInProgress},
		{sqlDriverHasPendingOffer, []any{driverID}, trip.ErrDriverHasPendingOffer},
		{sqlDriverHasActiveTrip, []any{driverID}, trip.ErrDriverHasActiveTrip},
	}

	for _, check := range checks {
		var refused bool

		if err := tx.QueryRow(ctx, check.query, check.args...).Scan(&refused); err != nil {
			return trip.Offer{}, fmt.Errorf("check offer rules: %w", err)
		}

		if refused {
			return trip.Offer{}, check.refusal
		}
	}

	offer := trip.Offer{TripID: tripID, DriverID: driverID}

	if err := tx.QueryRow(ctx, sqlInsertOffer, tripID, driverID, ttl.Seconds()).Scan(&offer.OfferedAt, &offer.ExpiresAt); err != nil {
		return trip.Offer{}, mapOfferInsertError(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return trip.Offer{}, fmt.Errorf("commit transaction: %w", err)
	}

	return offer, nil
}

// FindPendingOffer returns the driver's live offer with its trip. See trip.OfferStore.
func (r *TripRepository) FindPendingOffer(
	ctx context.Context,
	driverID string,
) (trip.Offer, error) {
	offer := trip.Offer{DriverID: driverID}

	err := r.pool.QueryRow(ctx, sqlFindPendingOffer, driverID).Scan(&offer.TripID, &offer.OfferedAt, &offer.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return trip.Offer{}, trip.ErrOfferNotFound
	}

	if err != nil {
		return trip.Offer{}, fmt.Errorf("query pending offer: %w", err)
	}

	found, err := r.FindByID(ctx, offer.TripID)
	if errors.Is(err, trip.ErrTripNotFound) {
		return trip.Offer{}, trip.ErrOfferNotFound
	}

	if err != nil {
		return trip.Offer{}, fmt.Errorf("load offered trip: %w", err)
	}

	offer.Trip = found

	return offer, nil
}

// AcceptOffer accepts the driver's live offer and the trip in one transaction:
// the same requested-to-accepted transition, and the same trip.accepted event,
// as AcceptTrip. See trip.OfferStore.
func (r *TripRepository) AcceptOffer(
	ctx context.Context,
	tripID string,
	driverID string,
) (trip.Trip, error) {
	return r.transition(ctx, tripID, trip.StatusAccepted, func(tx pgx.Tx, current trip.Trip) (trip.Trip, error) {
		var (
			offerID string
			live    bool
		)

		if err := tx.QueryRow(ctx, sqlLockOffer, tripID, driverID).Scan(&offerID, &live); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return trip.Trip{}, trip.ErrOfferNotFound
			}

			return trip.Trip{}, fmt.Errorf("lock trip offer: %w", err)
		}

		if !live {
			return trip.Trip{}, trip.ErrOfferExpired
		}

		var busy bool

		if err := tx.QueryRow(ctx, sqlDriverHasActiveTrip, driverID).Scan(&busy); err != nil {
			return trip.Trip{}, fmt.Errorf("check driver's active trip: %w", err)
		}

		if busy {
			return trip.Trip{}, trip.ErrDriverHasActiveTrip
		}

		if _, err := tx.Exec(ctx, sqlMarkOfferAccepted, offerID); err != nil {
			return trip.Trip{}, fmt.Errorf("mark offer accepted: %w", err)
		}

		var updated trip.Trip

		if err := scanTrip(tx.QueryRow(ctx, sqlAcceptTripForOffer, tripID, driverID), &updated); err != nil {
			return trip.Trip{}, err
		}

		return updated, writeOutboxEvent(ctx, tx, "trip.accepted", updated.ID, map[string]string{
			"trip_id":   updated.ID,
			"driver_id": driverID,
		})
	})
}

// RejectOffer declines the driver's live offer. See trip.OfferStore.
func (r *TripRepository) RejectOffer(
	ctx context.Context,
	tripID string,
	driverID string,
) error {
	tag, err := r.pool.Exec(ctx, sqlRejectOffer, tripID, driverID)
	if err != nil {
		return fmt.Errorf("reject offer: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return trip.ErrOfferNotFound
	}

	return nil
}

// mapOfferInsertError turns the unique-index violations of an offer insert (two
// offers racing) into the same refusals the checks before it give.
func mapOfferInsertError(err error) error {
	var pgErr *pgconn.PgError

	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		switch pgErr.ConstraintName {
		case offerPerDriverIndex:
			return trip.ErrDriverHasPendingOffer
		case offerPerTripIndex:
			return trip.ErrOfferInProgress
		case offerPairKey:
			return trip.ErrAlreadyOffered
		}
	}

	return fmt.Errorf("insert trip offer: %w", err)
}
