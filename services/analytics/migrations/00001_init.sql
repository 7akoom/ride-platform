-- +goose Up
-- +goose StatementBegin

-- Append-only raw event log — مصدر الحقيقة، وأي query مستقبلي مش مغطى بجدول
-- مخصص بينبني منه. event_id فريد لضمان idempotency عند إعادة تسليم JetStream.
CREATE TABLE raw_events (
    id              BIGSERIAL PRIMARY KEY,
    event_id        TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    payload         JSONB NOT NULL,
    occurred_at     TIMESTAMPTZ NOT NULL,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_id)
);

CREATE INDEX idx_raw_events_event_type ON raw_events (event_type);
CREATE INDEX idx_raw_events_occurred_at ON raw_events (occurred_at);

-- Trip funnel: عداد يومي متراكم، يتحدث incrementally مع كل حدث trip.*
CREATE TABLE trip_funnel_daily (
    day                 DATE NOT NULL PRIMARY KEY,
    requested_count     BIGINT NOT NULL DEFAULT 0,
    accepted_count      BIGINT NOT NULL DEFAULT 0,
    started_count       BIGINT NOT NULL DEFAULT 0,
    completed_count     BIGINT NOT NULL DEFAULT 0,
    cancelled_count     BIGINT NOT NULL DEFAULT 0
);

-- سجل تفصيلي لكل إلغاء — عشان نعرف بأي مرحلة صار الإلغاء (requested/accepted/started)
CREATE TABLE trip_cancellations (
    trip_id             TEXT NOT NULL PRIMARY KEY,
    stage_at_cancellation TEXT NOT NULL,
    cancelled_at        TIMESTAMPTZ NOT NULL
);

-- تتبع حالة كل trip عشان نعرف "آخر مرحلة وصلها" وقت وصول trip.cancelled
-- (event واحد بس بيوصل بالإلغاء، ما فيه ذكر للمرحلة السابقة بمتنه)
CREATE TABLE trip_last_known_stage (
    trip_id     TEXT NOT NULL PRIMARY KEY,
    stage       TEXT NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL
);

-- الإيرادات اليومية — من fare.calculated (gross) + trip.settled (commission)
CREATE TABLE revenue_daily (
    day                 DATE NOT NULL,
    currency            TEXT NOT NULL,
    gross_fare_total    NUMERIC(18,3) NOT NULL DEFAULT 0,
    commission_total    NUMERIC(18,3) NOT NULL DEFAULT 0,
    trip_count          BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (day, currency)
);

-- Cohorts: ركاب وسائقين، بداية أسبوع أول ظهور (rider.created / driver.created)
-- كـ DATE (يوم الإثنين ISO) عشان يسهل حساب "بعد N أسبوع" بعمليات تاريخ عادية،
-- بدل ما نحلل نص "YYYY-Www" بالـ SQL.
CREATE TABLE rider_cohorts (
    rider_id            TEXT NOT NULL PRIMARY KEY,
    cohort_week_start   DATE NOT NULL
);

CREATE TABLE driver_cohorts (
    driver_id           TEXT NOT NULL PRIMARY KEY,
    cohort_week_start   DATE NOT NULL
);

CREATE INDEX idx_rider_cohorts_week ON rider_cohorts (cohort_week_start);
CREATE INDEX idx_driver_cohorts_week ON driver_cohorts (cohort_week_start);

-- نشاط أسبوعي: أي أسابيع (بداية الأسبوع) كان فيها الراكب/السائق نشط
-- (طلب/قبل رحلة) — عشان نحسب retention_percent_by_week بدون مسح raw_events.
CREATE TABLE rider_weekly_activity (
    rider_id            TEXT NOT NULL,
    activity_week_start DATE NOT NULL,
    PRIMARY KEY (rider_id, activity_week_start)
);

CREATE TABLE driver_weekly_activity (
    driver_id            TEXT NOT NULL,
    activity_week_start  DATE NOT NULL,
    PRIMARY KEY (driver_id, activity_week_start)
);

CREATE INDEX idx_rider_weekly_activity_week ON rider_weekly_activity (activity_week_start);
CREATE INDEX idx_driver_weekly_activity_week ON driver_weekly_activity (activity_week_start);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS driver_weekly_activity;
DROP TABLE IF EXISTS rider_weekly_activity;
DROP TABLE IF EXISTS driver_cohorts;
DROP TABLE IF EXISTS rider_cohorts;
DROP TABLE IF EXISTS revenue_daily;
DROP TABLE IF EXISTS trip_last_known_stage;
DROP TABLE IF EXISTS trip_cancellations;
DROP TABLE IF EXISTS trip_funnel_daily;
DROP TABLE IF EXISTS raw_events;
-- +goose StatementEnd
