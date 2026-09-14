-- +goose Up

-- Core trip lifecycle templates, seeded in the three languages this
-- platform targets. Add a language by inserting rows here or via
-- UpsertTemplate — no migration needed.
--
-- Placeholders in {braces} are substituted at send time from the
-- caller's variables map. A missing variable is left as-is rather than
-- blanked, so a broken template is visible instead of silently
-- producing half a sentence.

INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('trip.driver_assigned', ARRAY['in_app', 'push']),
    ('trip.driver_arrived',  ARRAY['in_app', 'push']),
    ('trip.started',         ARRAY['in_app']),
    ('trip.completed',       ARRAY['in_app', 'push']),
    ('trip.cancelled',       ARRAY['in_app', 'push']),
    ('wallet.topped_up',     ARRAY['in_app', 'push']),
    ('driver.suspended',     ARRAY['in_app', 'push', 'sms']),
    ('driver.reinstated',    ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    -- trip.driver_assigned
    ('trip.driver_assigned', 'en', 'Driver on the way',
     '{driver_name} is coming to pick you up in a {vehicle_color} {vehicle_model}, plate {plate_number}.'),
    ('trip.driver_assigned', 'ar', 'السائق في الطريق إليك',
     '{driver_name} في طريقه إليك بسيارة {vehicle_model} {vehicle_color}، رقم اللوحة {plate_number}.'),
    ('trip.driver_assigned', 'ku', 'شۆفێر لە ڕێگادایە',
     '{driver_name} بە ئۆتۆمبێلی {vehicle_model}ی {vehicle_color} بۆ لات دێت، ژمارەی پلێت {plate_number}.'),

    -- trip.driver_arrived
    ('trip.driver_arrived', 'en', 'Your driver has arrived',
     '{driver_name} is waiting for you at the pickup point.'),
    ('trip.driver_arrived', 'ar', 'وصل السائق',
     '{driver_name} بانتظارك في نقطة الانطلاق.'),
    ('trip.driver_arrived', 'ku', 'شۆفێرەکەت گەیشت',
     '{driver_name} لە خاڵی وەرگرتن چاوەڕێت دەکات.'),

    -- trip.started
    ('trip.started', 'en', 'Trip started', 'Your trip to {dropoff} has started.'),
    ('trip.started', 'ar', 'بدأت الرحلة', 'بدأت رحلتك إلى {dropoff}.'),
    ('trip.started', 'ku', 'گەشتەکە دەستی پێکرد', 'گەشتەکەت بۆ {dropoff} دەستی پێکرد.'),

    -- trip.completed
    ('trip.completed', 'en', 'Trip completed',
     'Your trip is complete. Total: {total} {currency}.'),
    ('trip.completed', 'ar', 'انتهت الرحلة',
     'انتهت رحلتك. الإجمالي: {total} {currency}.'),
    ('trip.completed', 'ku', 'گەشتەکە تەواو بوو',
     'گەشتەکەت تەواو بوو. کۆی گشتی: {total} {currency}.'),

    -- trip.cancelled
    ('trip.cancelled', 'en', 'Trip cancelled', 'Your trip was cancelled. {reason}'),
    ('trip.cancelled', 'ar', 'أُلغيت الرحلة', 'تم إلغاء رحلتك. {reason}'),
    ('trip.cancelled', 'ku', 'گەشتەکە هەڵوەشێنرایەوە', 'گەشتەکەت هەڵوەشێنرایەوە. {reason}'),

    -- wallet.topped_up
    ('wallet.topped_up', 'en', 'Wallet topped up',
     '{amount} {currency} was added. New balance: {balance} {currency}.'),
    ('wallet.topped_up', 'ar', 'تم شحن المحفظة',
     'تمت إضافة {amount} {currency}. الرصيد الجديد: {balance} {currency}.'),
    ('wallet.topped_up', 'ku', 'جزدان پڕ کرایەوە',
     '{amount} {currency} زیاد کرا. باڵانسی نوێ: {balance} {currency}.'),

    -- driver.suspended
    ('driver.suspended', 'en', 'Account suspended',
     'Your balance has reached the limit. Deposit {amount_due} {currency} to continue receiving trips.'),
    ('driver.suspended', 'ar', 'تم إيقاف الحساب',
     'وصل رصيدك إلى الحد المسموح. أودع {amount_due} {currency} لمتابعة استلام الرحلات.'),
    ('driver.suspended', 'ku', 'هەژمارەکە ڕاگیرا',
     'باڵانسەکەت گەیشتووەتە سنوور. {amount_due} {currency} دابنێ بۆ بەردەوامبوون لە وەرگرتنی گەشت.'),

    -- driver.reinstated
    ('driver.reinstated', 'en', 'Account active again',
     'Your deposit was received. You can start accepting trips.'),
    ('driver.reinstated', 'ar', 'تم تفعيل حسابك',
     'تم استلام إيداعك. يمكنك البدء باستلام الرحلات.'),
    ('driver.reinstated', 'ku', 'هەژمارەکەت چالاک بووەوە',
     'پارەکەت وەرگیرا. دەتوانیت دەست بکەیت بە وەرگرتنی گەشت.');

-- +goose Down

DELETE FROM notification_templates
WHERE event_key IN (
    'trip.driver_assigned',
    'trip.driver_arrived',
    'trip.started',
    'trip.completed',
    'trip.cancelled',
    'wallet.topped_up',
    'driver.suspended',
    'driver.reinstated'
);
