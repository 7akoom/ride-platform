-- +goose Up
-- +goose StatementBegin

-- Which events were already counted (redelivery is a no-op). Only the id,
-- type and time: no payloads, so nothing about a person is kept here.
CREATE TABLE processed_events (
    event_id    TEXT PRIMARY KEY,
    event_type  TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per trip: where and what it was, how far it got and what it paid.
-- Ids and numbers only. Reports group it by day on the report's clock.
CREATE TABLE trip_facts (
    trip_id        TEXT PRIMARY KEY,
    rider_id       TEXT,
    driver_id      TEXT,
    city_id        TEXT,
    zone_id        TEXT,
    vehicle_class  TEXT,
    payment_method TEXT,
    scheduled      BOOLEAN,
    requested_at   TIMESTAMPTZ,
    accepted_at    TIMESTAMPTZ,
    arrived_at     TIMESTAMPTZ,
    started_at     TIMESTAMPTZ,
    completed_at   TIMESTAMPTZ,
    cancelled_at   TIMESTAMPTZ,
    cancelled_by   TEXT,
    cancel_stage   TEXT,
    rider_no_show  BOOLEAN NOT NULL DEFAULT false,
    fare_kind      TEXT,
    currency       TEXT,
    fare_total     NUMERIC(18,3),
    fare_at        TIMESTAMPTZ,
    commission     NUMERIC(18,3),
    settled_at     TIMESTAMPTZ,
    CONSTRAINT trip_facts_cancel_stage_check
        CHECK (cancel_stage IS NULL OR cancel_stage IN ('requested', 'accepted', 'arrived', 'started'))
);

CREATE INDEX trip_facts_requested_at_idx ON trip_facts (requested_at);
CREATE INDEX trip_facts_fare_at_idx ON trip_facts (fare_at) WHERE fare_at IS NOT NULL;
CREATE INDEX trip_facts_city_requested_idx ON trip_facts (city_id, requested_at);
CREATE INDEX trip_facts_rider_idx ON trip_facts (rider_id, requested_at);
CREATE INDEX trip_facts_driver_idx ON trip_facts (driver_id, accepted_at);

CREATE TABLE rider_signups (
    rider_id     TEXT PRIMARY KEY,
    signed_up_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE driver_signups (
    driver_id    TEXT PRIMARY KEY,
    signed_up_at TIMESTAMPTZ,
    approved_at  TIMESTAMPTZ
);

CREATE INDEX rider_signups_at_idx ON rider_signups (signed_up_at);
CREATE INDEX driver_signups_at_idx ON driver_signups ((COALESCE(approved_at, signed_up_at)));

-- Carry over what was already consumed.
INSERT INTO processed_events (event_id, event_type, occurred_at, received_at)
SELECT event_id, event_type, occurred_at, received_at FROM raw_events;

INSERT INTO trip_facts (trip_id)
SELECT DISTINCT payload->>'trip_id'
FROM raw_events
WHERE event_type IN ('trip.requested', 'trip.accepted', 'trip.driver_arrived', 'trip.started',
                     'trip.completed', 'trip.cancelled', 'fare.calculated', 'trip.settled')
  AND COALESCE(payload->>'trip_id', '') <> '';

UPDATE trip_facts f SET rider_id = e.payload->>'rider_id', requested_at = e.occurred_at,
       city_id = NULLIF(e.payload->>'city_id', ''), zone_id = NULLIF(e.payload->>'zone_id', ''),
       vehicle_class = NULLIF(e.payload->>'vehicle_class', ''),
       payment_method = NULLIF(e.payload->>'payment_method', ''),
       scheduled = CASE e.payload->>'scheduled' WHEN 'true' THEN true WHEN 'false' THEN false END
FROM (SELECT DISTINCT ON (payload->>'trip_id') payload, occurred_at FROM raw_events
      WHERE event_type = 'trip.requested' ORDER BY payload->>'trip_id', occurred_at) e
WHERE f.trip_id = e.payload->>'trip_id';

UPDATE trip_facts f SET driver_id = e.payload->>'driver_id', accepted_at = e.occurred_at
FROM (SELECT DISTINCT ON (payload->>'trip_id') payload, occurred_at FROM raw_events
      WHERE event_type = 'trip.accepted' ORDER BY payload->>'trip_id', occurred_at) e
WHERE f.trip_id = e.payload->>'trip_id';

UPDATE trip_facts f SET arrived_at = e.occurred_at
FROM (SELECT payload->>'trip_id' AS trip_id, min(occurred_at) AS occurred_at FROM raw_events
      WHERE event_type = 'trip.driver_arrived' GROUP BY 1) e
WHERE f.trip_id = e.trip_id;

UPDATE trip_facts f SET started_at = e.occurred_at
FROM (SELECT payload->>'trip_id' AS trip_id, min(occurred_at) AS occurred_at FROM raw_events
      WHERE event_type = 'trip.started' GROUP BY 1) e
WHERE f.trip_id = e.trip_id;

UPDATE trip_facts f SET completed_at = e.occurred_at,
       rider_id = COALESCE(f.rider_id, NULLIF(e.payload->>'rider_id', '')),
       driver_id = COALESCE(f.driver_id, NULLIF(e.payload->>'driver_id', ''))
FROM (SELECT DISTINCT ON (payload->>'trip_id') payload, occurred_at FROM raw_events
      WHERE event_type = 'trip.completed' ORDER BY payload->>'trip_id', occurred_at) e
WHERE f.trip_id = e.payload->>'trip_id';

UPDATE trip_facts f SET cancelled_at = e.occurred_at,
       cancelled_by = NULLIF(e.payload->>'cancelled_by', ''),
       rider_no_show = COALESCE(e.payload->>'rider_no_show', '') = 'true',
       cancel_stage = CASE
           WHEN f.started_at IS NOT NULL THEN 'started'
           WHEN f.arrived_at IS NOT NULL THEN 'arrived'
           WHEN f.accepted_at IS NOT NULL THEN 'accepted'
           ELSE 'requested'
       END
FROM (SELECT DISTINCT ON (payload->>'trip_id') payload, occurred_at FROM raw_events
      WHERE event_type = 'trip.cancelled' ORDER BY payload->>'trip_id', occurred_at) e
WHERE f.trip_id = e.payload->>'trip_id';

UPDATE trip_facts f SET fare_kind = COALESCE(NULLIF(e.payload->>'kind', ''), 'trip'),
       currency = e.payload->>'currency_code', fare_total = (e.payload->>'total')::numeric,
       fare_at = e.occurred_at, rider_id = COALESCE(f.rider_id, NULLIF(e.payload->>'rider_id', ''))
FROM (SELECT DISTINCT ON (payload->>'trip_id') payload, occurred_at FROM raw_events
      WHERE event_type = 'fare.calculated' ORDER BY payload->>'trip_id', occurred_at DESC) e
WHERE f.trip_id = e.payload->>'trip_id';

UPDATE trip_facts f SET commission = (e.payload->>'commission_amount')::numeric, settled_at = e.occurred_at,
       driver_id = COALESCE(f.driver_id, NULLIF(e.payload->>'driver_id', ''))
FROM (SELECT DISTINCT ON (payload->>'trip_id') payload, occurred_at FROM raw_events
      WHERE event_type = 'trip.settled' ORDER BY payload->>'trip_id', occurred_at DESC) e
WHERE f.trip_id = e.payload->>'trip_id';

INSERT INTO rider_signups (rider_id, signed_up_at)
SELECT payload->>'rider_id', min(occurred_at) FROM raw_events
WHERE event_type = 'rider.created' AND COALESCE(payload->>'rider_id', '') <> ''
GROUP BY 1;

INSERT INTO driver_signups (driver_id, signed_up_at, approved_at)
SELECT payload->>'driver_id',
       min(occurred_at) FILTER (WHERE event_type = 'driver.created'),
       min(occurred_at) FILTER (WHERE event_type = 'driver.approved')
FROM raw_events
WHERE event_type IN ('driver.created', 'driver.approved') AND COALESCE(payload->>'driver_id', '') <> ''
GROUP BY 1;

-- The old per-day counters and the stored payloads (names included) go.
DROP TABLE trip_fare_currency;
DROP TABLE driver_weekly_activity;
DROP TABLE rider_weekly_activity;
DROP TABLE driver_cohorts;
DROP TABLE rider_cohorts;
DROP TABLE revenue_daily;
DROP TABLE trip_last_known_stage;
DROP TABLE trip_cancellations;
DROP TABLE trip_funnel_daily;
DROP TABLE raw_events;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Rebuilds the old tables from the facts (UTC days, as before). The stored
-- payloads cannot come back: raw_events returns with empty ones.
CREATE TABLE raw_events (
    id          BIGSERIAL PRIMARY KEY,
    event_id    TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    payload     JSONB NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_id)
);
CREATE INDEX idx_raw_events_event_type ON raw_events (event_type);
CREATE INDEX idx_raw_events_occurred_at ON raw_events (occurred_at);

INSERT INTO raw_events (event_id, event_type, payload, occurred_at, received_at)
SELECT event_id, event_type, '{}'::jsonb, occurred_at, received_at FROM processed_events;

CREATE TABLE trip_funnel_daily (
    day             DATE NOT NULL PRIMARY KEY,
    requested_count BIGINT NOT NULL DEFAULT 0,
    accepted_count  BIGINT NOT NULL DEFAULT 0,
    started_count   BIGINT NOT NULL DEFAULT 0,
    completed_count BIGINT NOT NULL DEFAULT 0,
    cancelled_count BIGINT NOT NULL DEFAULT 0
);

INSERT INTO trip_funnel_daily (day, requested_count, accepted_count, started_count, completed_count, cancelled_count)
SELECT day, sum(r), sum(a), sum(s), sum(c), sum(x) FROM (
    SELECT (requested_at AT TIME ZONE 'UTC')::date AS day, 1 AS r, 0 AS a, 0 AS s, 0 AS c, 0 AS x FROM trip_facts WHERE requested_at IS NOT NULL
    UNION ALL SELECT (accepted_at AT TIME ZONE 'UTC')::date, 0, 1, 0, 0, 0 FROM trip_facts WHERE accepted_at IS NOT NULL
    UNION ALL SELECT (started_at AT TIME ZONE 'UTC')::date, 0, 0, 1, 0, 0 FROM trip_facts WHERE started_at IS NOT NULL
    UNION ALL SELECT (completed_at AT TIME ZONE 'UTC')::date, 0, 0, 0, 1, 0 FROM trip_facts WHERE completed_at IS NOT NULL
    UNION ALL SELECT (cancelled_at AT TIME ZONE 'UTC')::date, 0, 0, 0, 0, 1 FROM trip_facts WHERE cancelled_at IS NOT NULL
) t GROUP BY day;

CREATE TABLE trip_cancellations (
    trip_id               TEXT NOT NULL PRIMARY KEY,
    stage_at_cancellation TEXT NOT NULL,
    cancelled_at          TIMESTAMPTZ NOT NULL
);

INSERT INTO trip_cancellations (trip_id, stage_at_cancellation, cancelled_at)
SELECT trip_id, CASE cancel_stage WHEN 'arrived' THEN 'accepted' ELSE COALESCE(cancel_stage, 'requested') END, cancelled_at
FROM trip_facts WHERE cancelled_at IS NOT NULL;

CREATE TABLE trip_last_known_stage (
    trip_id    TEXT NOT NULL PRIMARY KEY,
    stage      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

INSERT INTO trip_last_known_stage (trip_id, stage, updated_at)
SELECT trip_id,
       CASE WHEN completed_at IS NOT NULL THEN 'completed' WHEN started_at IS NOT NULL THEN 'started'
            WHEN accepted_at IS NOT NULL THEN 'accepted' ELSE 'requested' END,
       GREATEST(requested_at, accepted_at, started_at, completed_at)
FROM trip_facts WHERE requested_at IS NOT NULL;

CREATE TABLE revenue_daily (
    day              DATE NOT NULL,
    currency         TEXT NOT NULL,
    gross_fare_total NUMERIC(18,3) NOT NULL DEFAULT 0,
    commission_total NUMERIC(18,3) NOT NULL DEFAULT 0,
    trip_count       BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (day, currency)
);

INSERT INTO revenue_daily (day, currency, gross_fare_total, commission_total, trip_count)
SELECT (fare_at AT TIME ZONE 'UTC')::date, currency, sum(fare_total), COALESCE(sum(commission), 0), count(settled_at)
FROM trip_facts WHERE fare_at IS NOT NULL AND currency IS NOT NULL
GROUP BY 1, 2;

CREATE TABLE trip_fare_currency (
    trip_id  TEXT NOT NULL PRIMARY KEY,
    currency TEXT NOT NULL
);

INSERT INTO trip_fare_currency (trip_id, currency)
SELECT trip_id, currency FROM trip_facts WHERE currency IS NOT NULL;

CREATE TABLE rider_cohorts (
    rider_id          TEXT NOT NULL PRIMARY KEY,
    cohort_week_start DATE NOT NULL
);
CREATE TABLE driver_cohorts (
    driver_id         TEXT NOT NULL PRIMARY KEY,
    cohort_week_start DATE NOT NULL
);
CREATE INDEX idx_rider_cohorts_week ON rider_cohorts (cohort_week_start);
CREATE INDEX idx_driver_cohorts_week ON driver_cohorts (cohort_week_start);

INSERT INTO rider_cohorts (rider_id, cohort_week_start)
SELECT rider_id, date_trunc('week', signed_up_at AT TIME ZONE 'UTC')::date FROM rider_signups;

INSERT INTO driver_cohorts (driver_id, cohort_week_start)
SELECT driver_id, date_trunc('week', signed_up_at AT TIME ZONE 'UTC')::date FROM driver_signups WHERE signed_up_at IS NOT NULL;

CREATE TABLE rider_weekly_activity (
    rider_id            TEXT NOT NULL,
    activity_week_start DATE NOT NULL,
    PRIMARY KEY (rider_id, activity_week_start)
);
CREATE TABLE driver_weekly_activity (
    driver_id           TEXT NOT NULL,
    activity_week_start DATE NOT NULL,
    PRIMARY KEY (driver_id, activity_week_start)
);
CREATE INDEX idx_rider_weekly_activity_week ON rider_weekly_activity (activity_week_start);
CREATE INDEX idx_driver_weekly_activity_week ON driver_weekly_activity (activity_week_start);

INSERT INTO rider_weekly_activity (rider_id, activity_week_start)
SELECT DISTINCT rider_id, date_trunc('week', requested_at AT TIME ZONE 'UTC')::date
FROM trip_facts WHERE rider_id IS NOT NULL AND requested_at IS NOT NULL;

INSERT INTO driver_weekly_activity (driver_id, activity_week_start)
SELECT DISTINCT driver_id, date_trunc('week', accepted_at AT TIME ZONE 'UTC')::date
FROM trip_facts WHERE driver_id IS NOT NULL AND accepted_at IS NOT NULL;

DROP TABLE driver_signups;
DROP TABLE rider_signups;
DROP TABLE trip_facts;
DROP TABLE processed_events;

-- +goose StatementEnd
