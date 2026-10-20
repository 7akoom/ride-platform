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

	sqlInsertOffer = `INSERT INTO trip_offers (trip_id, driver_id, offered_at, expires_at, after_trip_id, pickup_eta_seconds)
        VALUES ($1, $2, clock_timestamp(), clock_timestamp() + make_interval(secs => $3::double precision),
                NULLIF($4::text, '')::uuid, NULLIF($5::integer, 0))
        RETURNING offered_at, expires_at`

	// An offer after another trip: that trip is the driver's current one and the
	// rider is in the car.
	sqlTripChainable = `SELECT EXISTS (SELECT 1 FROM trips
        WHERE id = $1 AND driver_id = $2 AND status = 'in_progress' AND heading_at IS NOT NULL)`

	sqlDriverHasNextTrip = `SELECT EXISTS (SELECT 1 FROM trips
        WHERE driver_id = $1 AND status = 'accepted' AND heading_at IS NULL)`

	// The driver's accepted and in-progress trips, locked, as an offer is accepted.
	sqlLockDriverTrips = `SELECT id::text, status, heading_at IS NOT NULL
        FROM trips
        WHERE driver_id = $1 AND status IN ('accepted', 'in_progress')
        FOR UPDATE`

	sqlFindPendingOffer = `SELECT o.trip_id, o.offered_at, o.expires_at,
               COALESCE(o.after_trip_id::text, ''), COALESCE(o.pickup_eta_seconds, 0)
        FROM trip_offers o
        JOIN trips t ON t.id = o.trip_id
        WHERE o.driver_id = $1
          AND o.status = 'pending'
          AND o.expires_at > clock_timestamp()
          AND t.status = 'requested'
        ORDER BY o.offered_at DESC
        LIMIT 1`

	sqlLockOffer = `SELECT id, expires_at > clock_timestamp(),
               COALESCE(after_trip_id::text, ''), COALESCE(pickup_eta_seconds, 0), offered_at
        FROM trip_offers
        WHERE trip_id = $1 AND driver_id = $2 AND status = 'pending'
        FOR UPDATE`

	sqlMarkOfferAccepted = `UPDATE trip_offers SET status = 'accepted', responded_at = clock_timestamp() WHERE id = $1`

	sqlRejectOffer = `UPDATE trip_offers
        SET status = 'rejected', responded_at = clock_timestamp()
        WHERE trip_id = $1 AND driver_id = $2 AND status = 'pending' AND expires_at > clock_timestamp()`

	// $3: the driver sets off now (false for a next trip, which waits for the
	// driver's current one to end); $4 the trip the driver was on when offered
	// this one; $5 when they are expected at the pickup.
	sqlAcceptTripForOffer = `UPDATE trips
        SET driver_id = $2,
            status = 'accepted',
            accepted_at = CURRENT_TIMESTAMP,
            heading_at = CASE WHEN $3::boolean THEN CURRENT_TIMESTAMP END,
            after_trip_id = NULLIF($4::text, '')::uuid,
            expected_pickup_at = $5::timestamptz,
            updated_at = CURRENT_TIMESTAMP
        WHERE id = $1
        RETURNING ` + historyColumns
)

// CreateOffer offers the trip to the driver. See trip.OfferStore.
func (r *TripRepository) CreateOffer(
	ctx context.Context,
	tripID string,
	driverID string,
	ttl time.Duration,
) (trip.Offer, error) {
	return r.CreateChainedOffer(ctx, tripID, driverID, ttl, trip.OfferChain{})
}

// CreateChainedOffer offers the trip to the driver, to take after their current
// trip when chain.AfterTripID names it. See trip.ChainedOfferStore.
//
// The trip row is locked first, so offers of one trip are serialized; the unique
// indexes of migration 00009 are the backstop for the driver side, where two trips
// can be offered to the same driver at the same moment.
func (r *TripRepository) CreateChainedOffer(
	ctx context.Context,
	tripID string,
	driverID string,
	ttl time.Duration,
	chain trip.OfferChain,
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
	}

	if chain.AfterTripID == "" {
		checks = append(checks, struct {
			query   string
			args    []any
			refusal error
		}{sqlDriverHasActiveTrip, []any{driverID}, trip.ErrDriverHasActiveTrip})
	} else {
		checks = append(checks, struct {
			query   string
			args    []any
			refusal error
		}{sqlDriverHasNextTrip, []any{driverID}, trip.ErrNotOfferableAfterTrip})
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

	if chain.AfterTripID != "" {
		var chainable bool

		if err := tx.QueryRow(ctx, sqlTripChainable, chain.AfterTripID, driverID).Scan(&chainable); err != nil {
			return trip.Offer{}, fmt.Errorf("check the driver's current trip: %w", err)
		}

		if !chainable {
			return trip.Offer{}, trip.ErrNotOfferableAfterTrip
		}
	}

	offer := trip.Offer{TripID: tripID, DriverID: driverID, AfterTripID: chain.AfterTripID, PickupETA: chain.PickupETA}
	etaSeconds := int32(chain.PickupETA / time.Second)

	if err := tx.QueryRow(ctx, sqlInsertOffer, tripID, driverID, ttl.Seconds(), chain.AfterTripID, etaSeconds).Scan(&offer.OfferedAt, &offer.ExpiresAt); err != nil {
		return trip.Offer{}, mapOfferInsertError(err)
	}

	// The driver's app is told about the offer through this event (see notification-service).
	// It names the trip, the driver and when the offer ends, and nothing about the rider. It is
	// written in the same transaction as the offer, so an offer that exists always has its event
	// and an offer that was refused never does.
	if err := writeOutboxEvent(ctx, tx, "trip.offered", tripID, map[string]string{
		"trip_id":       tripID,
		"driver_id":     driverID,
		"offered_at":    offer.OfferedAt.UTC().Format(time.RFC3339Nano),
		"expires_at":    offer.ExpiresAt.UTC().Format(time.RFC3339Nano),
		"after_trip_id": chain.AfterTripID,
	}); err != nil {
		return trip.Offer{}, err
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

	var etaSeconds int32

	err := r.pool.QueryRow(ctx, sqlFindPendingOffer, driverID).Scan(&offer.TripID, &offer.OfferedAt, &offer.ExpiresAt, &offer.AfterTripID, &etaSeconds)
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
	offer.PickupETA = time.Duration(etaSeconds) * time.Second

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
			offerID     string
			live        bool
			afterTripID string
			etaSeconds  int32
			offeredAt   time.Time
		)

		if err := tx.QueryRow(ctx, sqlLockOffer, tripID, driverID).Scan(&offerID, &live, &afterTripID, &etaSeconds, &offeredAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return trip.Trip{}, trip.ErrOfferNotFound
			}

			return trip.Trip{}, fmt.Errorf("lock trip offer: %w", err)
		}

		if !live {
			return trip.Trip{}, trip.ErrOfferExpired
		}

		driverTrips, err := lockDriverTrips(ctx, tx, driverID)
		if err != nil {
			return trip.Trip{}, err
		}

		headingNow, err := offerAcceptance(driverTrips, afterTripID)
		if err != nil {
			return trip.Trip{}, err
		}

		if _, err := tx.Exec(ctx, sqlMarkOfferAccepted, offerID); err != nil {
			return trip.Trip{}, fmt.Errorf("mark offer accepted: %w", err)
		}

		var expectedAt *time.Time

		if etaSeconds > 0 {
			at := offeredAt.Add(time.Duration(etaSeconds) * time.Second)
			expectedAt = &at
		}

		var updated trip.Trip

		if err := scanTrip(tx.QueryRow(ctx, sqlAcceptTripForOffer, tripID, driverID, headingNow, afterTripID, expectedAt), &updated); err != nil {
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
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, sqlRejectOffer, tripID, driverID)
	if err != nil {
		return fmt.Errorf("reject offer: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return trip.ErrOfferNotFound
	}

	// Reports count a driver's refusals from this event.
	if err := writeOutboxEvent(ctx, tx, "trip.offer_rejected", tripID, map[string]string{
		"trip_id":   tripID,
		"driver_id": driverID,
	}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
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

// driverTrip is one of a driver's accepted or in-progress trips; heading is false
// for a next trip they have not set off for.
type driverTrip struct {
	id      string
	status  trip.Status
	heading bool
}

func lockDriverTrips(ctx context.Context, tx pgx.Tx, driverID string) ([]driverTrip, error) {
	rows, err := tx.Query(ctx, sqlLockDriverTrips, driverID)
	if err != nil {
		return nil, fmt.Errorf("lock driver's trips: %w", err)
	}
	defer rows.Close()

	var trips []driverTrip

	for rows.Next() {
		var (
			t      driverTrip
			status string
		)

		if err := rows.Scan(&t.id, &status, &t.heading); err != nil {
			return nil, fmt.Errorf("read driver's trips: %w", err)
		}

		t.status = trip.Status(status)
		trips = append(trips, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read driver's trips: %w", err)
	}

	return trips, nil
}

// offerAcceptance decides, from the driver's trips as the offer is accepted,
// whether they set off for the pickup now (headingNow) or take it as their next
// trip. An offer made after afterTripID becomes a next trip while that trip is
// still under way, and an ordinary trip once it has ended; a driver never has
// more than one current and one next trip.
func offerAcceptance(driverTrips []driverTrip, afterTripID string) (headingNow bool, err error) {
	var current *driverTrip

	for i := range driverTrips {
		if !driverTrips[i].heading {
			return false, trip.ErrDriverHasActiveTrip
		}

		current = &driverTrips[i]
	}

	switch {
	case current == nil:
		return true, nil
	case afterTripID != "" && current.id == afterTripID && current.status == trip.StatusInProgress:
		return false, nil
	default:
		return false, trip.ErrDriverHasActiveTrip
	}
}
