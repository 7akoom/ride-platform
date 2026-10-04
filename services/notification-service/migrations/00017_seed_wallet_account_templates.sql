-- +goose Up

-- What a person is told about their own money (wallet-service events), and a
-- rider whose ride booked ahead could not be made. wallet.topped_up,
-- driver.suspended and driver.reinstated were seeded in 00005 but nothing sent
-- them until now.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('wallet.tip_received',     ARRAY['in_app', 'push']),
    ('wallet.payout_paid',      ARRAY['in_app', 'push']),
    ('wallet.payout_rejected',  ARRAY['in_app', 'push']),
    ('wallet.refund_issued',    ARRAY['in_app', 'push']),
    ('trip.schedule_failed',    ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    -- wallet.tip_received (driver)
    ('wallet.tip_received', 'en', 'You got a tip',
     'Your rider tipped you {amount} {currency}. It is in your wallet.'),
    ('wallet.tip_received', 'ar', 'وصلتك إكرامية',
     'أعطاك الراكب إكرامية بقيمة {amount} {currency}، وأُضيفت إلى محفظتك.'),
    ('wallet.tip_received', 'ku', 'دیارییەکت پێگەیشت',
     'سەرنشینەکەت {amount} {currency} دیاری پێدایت، خرایە سەر جزدانەکەت.'),

    -- wallet.payout_paid (driver)
    ('wallet.payout_paid', 'en', 'Payout sent',
     'Your payout of {amount} {currency} has been sent.'),
    ('wallet.payout_paid', 'ar', 'تم تحويل أرباحك',
     'تم تحويل مبلغ السحب {amount} {currency}.'),
    ('wallet.payout_paid', 'ku', 'پارەکەت نێردرا',
     'پارەی ڕاکێشانەکەت {amount} {currency} نێردرا.'),

    -- wallet.payout_rejected (driver)
    ('wallet.payout_rejected', 'en', 'Payout not sent',
     'Your payout of {amount} {currency} could not be sent: {reason}. The amount is back in your wallet.'),
    ('wallet.payout_rejected', 'ar', 'تعذّر تحويل السحب',
     'تعذّر تحويل مبلغ السحب {amount} {currency}: {reason}. أُعيد المبلغ إلى محفظتك.'),
    ('wallet.payout_rejected', 'ku', 'پارەکە نەنێردرا',
     'پارەی ڕاکێشانەکەت {amount} {currency} نەنێردرا: {reason}. بڕەکە گەڕایەوە سەر جزدانەکەت.'),

    -- wallet.refund_issued (rider)
    ('wallet.refund_issued', 'en', 'Refund issued',
     '{amount} {currency} was refunded for your trip.'),
    ('wallet.refund_issued', 'ar', 'تم استرداد مبلغ',
     'تم استرداد {amount} {currency} عن رحلتك.'),
    ('wallet.refund_issued', 'ku', 'پارە گەڕێنرایەوە',
     '{amount} {currency} بۆ گەشتەکەت گەڕێنرایەوە.'),

    -- trip.schedule_failed (rider)
    ('trip.schedule_failed', 'en', 'We could not book your ride',
     'We could not find a driver for the ride you booked. Please request a ride now.'),
    ('trip.schedule_failed', 'ar', 'تعذّر تأمين رحلتك المحجوزة',
     'لم نتمكن من إيجاد سائق للرحلة التي حجزتها. يرجى طلب رحلة الآن.'),
    ('trip.schedule_failed', 'ku', 'نەمانتوانی گەشتەکەت دابین بکەین',
     'نەمانتوانی شۆفێرێک بۆ ئەو گەشتەی حیجزت کردبوو بدۆزینەوە. تکایە ئێستا داوای گەشت بکە.');

-- A driver is reinstated by any money that lifts their balance back above the
-- limit (a tip, a refund, an adjustment), not only a deposit.
UPDATE notification_template_translations SET title = 'You can take trips again',
       body = 'Your balance is back above the limit. You can accept trips again.'
 WHERE event_key = 'driver.reinstated' AND locale = 'en';
UPDATE notification_template_translations SET title = 'يمكنك استلام الرحلات مجدداً',
       body = 'عاد رصيدك فوق الحد المسموح. يمكنك استلام الرحلات مجدداً.'
 WHERE event_key = 'driver.reinstated' AND locale = 'ar';
UPDATE notification_template_translations SET title = 'دەتوانیت دووبارە گەشت وەربگریت',
       body = 'باڵانسەکەت گەڕایەوە سەرووی سنوور. دەتوانیت دووبارە گەشت وەربگریت.'
 WHERE event_key = 'driver.reinstated' AND locale = 'ku';

-- +goose Down

UPDATE notification_template_translations SET title = 'Account active again',
       body = 'Your deposit was received. You can start accepting trips.'
 WHERE event_key = 'driver.reinstated' AND locale = 'en';
UPDATE notification_template_translations SET title = 'تم تفعيل حسابك',
       body = 'تم استلام إيداعك. يمكنك البدء باستلام الرحلات.'
 WHERE event_key = 'driver.reinstated' AND locale = 'ar';
UPDATE notification_template_translations SET title = 'هەژمارەکەت چالاک بووەوە',
       body = 'پارەکەت وەرگیرا. دەتوانیت دەست بکەیت بە وەرگرتنی گەشت.'
 WHERE event_key = 'driver.reinstated' AND locale = 'ku';

DELETE FROM notification_templates
WHERE event_key IN (
    'wallet.tip_received',
    'wallet.payout_paid',
    'wallet.payout_rejected',
    'wallet.refund_issued',
    'trip.schedule_failed'
);
