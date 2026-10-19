-- +goose Up

-- "Your trip has started" now rings the rider's phone too. Seeded in 00005 as in-app
-- only, on the thought that the rider is already in the car; but a rider whose app is
-- closed (a ride booked for someone else, or the phone in a pocket) learns of it only
-- this way. With the app open it shows as a short message, as the other trip news do.
UPDATE notification_templates
   SET default_channels = ARRAY['in_app', 'push'],
       updated_at = CURRENT_TIMESTAMP
 WHERE event_key = 'trip.started';

-- +goose Down

UPDATE notification_templates
   SET default_channels = ARRAY['in_app'],
       updated_at = CURRENT_TIMESTAMP
 WHERE event_key = 'trip.started';
