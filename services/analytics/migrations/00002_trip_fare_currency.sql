-- +goose Up
-- +goose StatementBegin

-- يربط كل trip_id بعملته وقت fare.calculated، عشان نقدر نضيف الـ commission
-- الصحيح للعملة الصحيحة وقت وصول trip.settled لاحقاً (هاد الحدث ما بيحمل
-- currency_code بمتنه).
CREATE TABLE trip_fare_currency (
    trip_id     TEXT NOT NULL PRIMARY KEY,
    currency    TEXT NOT NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS trip_fare_currency;
-- +goose StatementEnd
