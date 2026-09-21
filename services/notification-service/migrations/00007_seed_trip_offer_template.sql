-- +goose Up

-- The push a driver gets when a trip is offered to them (an offer lasts only seconds, so this
-- is what wakes the app up). The text carries no detail about the trip or the rider: the app
-- opens and reads the offer itself, and the push data carries only the trip id.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('trip.offer_received', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('trip.offer_received', 'en', 'New trip request',
     'A trip is waiting for you. Open the app to accept it.'),
    ('trip.offer_received', 'ar', 'طلب رحلة جديد',
     'في رحلة بانتظارك. افتح التطبيق لقبولها.'),
    ('trip.offer_received', 'ku', 'داواکاری گەشتی نوێ',
     'گەشتێک چاوەڕێتە. ئەپەکە بکەرەوە بۆ وەرگرتنی.');

-- +goose Down

DELETE FROM notification_templates
WHERE event_key = 'trip.offer_received';
