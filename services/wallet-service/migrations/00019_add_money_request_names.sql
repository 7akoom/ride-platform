-- +goose Up

-- Who asks and who is asked, by their short rider name ("Haidar K."), kept
-- when a request is made (and for an open request, when it is paid): the
-- one asked sees who asks before paying, the requester sees whom they asked.
-- Only shown; erased with the rest of a rider's data.
ALTER TABLE money_requests
    ADD COLUMN requester_name VARCHAR(60) NOT NULL DEFAULT '',
    ADD COLUMN payer_name VARCHAR(60) NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE money_requests
    DROP COLUMN IF EXISTS payer_name,
    DROP COLUMN IF EXISTS requester_name;
