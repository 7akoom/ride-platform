-- +goose Up

-- What a rider is told when a cancelled trip costs them a fee: they
-- cancelled after the grace minutes, or did not come to the pickup.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('trip.cancellation_fee', ARRAY['in_app', 'push']),
    ('trip.no_show_fee', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('trip.cancellation_fee', 'en', 'Cancellation fee',
     'A cancellation fee of {total} {currency} was charged because the trip was cancelled after the driver was on the way.'),
    ('trip.cancellation_fee', 'ar', 'رسوم إلغاء',
     'تم احتساب رسوم إلغاء {total} {currency} لأن الرحلة أُلغيت بعد انطلاق السائق إليك.'),
    ('trip.cancellation_fee', 'ku', 'کرێی هەڵوەشاندنەوە',
     'کرێی هەڵوەشاندنەوەی {total} {currency} وەرگیرا چونکە گەشتەکە دوای ڕێکەوتنی شۆفێر هەڵوەشێنرایەوە.'),
    ('trip.no_show_fee', 'en', 'No-show fee',
     'A fee of {total} {currency} was charged because the driver waited at the pickup and you did not come.'),
    ('trip.no_show_fee', 'ar', 'رسوم عدم الحضور',
     'تم احتساب رسوم {total} {currency} لأن السائق انتظرك في نقطة الانطلاق ولم تحضر.'),
    ('trip.no_show_fee', 'ku', 'کرێی نەهاتن',
     'کرێی {total} {currency} وەرگیرا چونکە شۆفێر لە خاڵی وەرگرتن چاوەڕێی کردیت و نەهاتیت.');

-- +goose Down

DELETE FROM notification_templates
WHERE event_key IN ('trip.cancellation_fee', 'trip.no_show_fee');
