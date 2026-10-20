-- +goose Up

-- A driver finishing a trip near the rider's pickup took their trip, to come once
-- that one ends (trip.driver_assigned_next, instead of trip.driver_assigned); then
-- they end it and set off (trip.driver_heading). Both ring the rider's phone.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('trip.driver_assigned_next', ARRAY['in_app', 'push']),
    ('trip.driver_heading', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('trip.driver_assigned_next', 'en', 'Captain found nearby',
     '{driver_name} is finishing a ride near you and will come to pick you up right after, in a {vehicle_color} {vehicle_model}, plate {plate_number}.'),
    ('trip.driver_assigned_next', 'ar', 'وجدنا كابتن قريب منك',
     '{driver_name} يُنهي رحلة قريبة منك وسيأتي لاصطحابك بعدها مباشرة بسيارة {vehicle_model} {vehicle_color}، رقم اللوحة {plate_number}.'),
    ('trip.driver_assigned_next', 'ku', 'کاپتنێکی نزیک دۆزرایەوە',
     '{driver_name} گەشتێکی نزیک لە تۆ تەواو دەکات و دواتر یەکسەر دێت بۆ وەرگرتنت، بە ئۆتۆمبێلی {vehicle_model}ی {vehicle_color}، ژمارەی پلێت {plate_number}.'),

    ('trip.driver_heading', 'en', 'Your captain is on the way',
     '{driver_name} has finished their previous ride and is now coming to pick you up.'),
    ('trip.driver_heading', 'ar', 'الكابتن في الطريق إليك',
     '{driver_name} أنهى رحلته السابقة وهو الآن في طريقه إليك.'),
    ('trip.driver_heading', 'ku', 'کاپتنەکەت لە ڕێگادایە',
     '{driver_name} گەشتی پێشووی تەواو کرد و ئێستا دێت بۆ وەرگرتنت.');

-- +goose Down

DELETE FROM notification_template_translations WHERE event_key IN ('trip.driver_assigned_next', 'trip.driver_heading');
DELETE FROM notification_templates WHERE event_key IN ('trip.driver_assigned_next', 'trip.driver_heading');
