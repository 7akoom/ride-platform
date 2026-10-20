-- +goose Up

-- The rider's trip cancelled because they did not come to the pickup in time
-- (the captain reported it, or the wait on the rate card ran out): the no-show
-- fee is theirs to pay. Before, they got the plain "trip cancelled" with an
-- English reason.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('trip.no_show', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('trip.no_show', 'en', 'Ride cancelled: you did not come',
     'Your captain waited at the pickup, but you did not come in time, so the ride was cancelled. The no-show fee is taken from your wallet.'),
    ('trip.no_show', 'ar', 'أُلغيت الرحلة لعدم حضورك',
     'انتظرك الكابتن عند نقطة الانطلاق ولم تحضر في الوقت المحدد، فأُلغيت الرحلة. تُخصم رسوم عدم الحضور من محفظتك.'),
    ('trip.no_show', 'ku', 'گەشتەکە هەڵوەشێنرایەوە چونکە نەهاتیت',
     'کاپتنەکەت لە شوێنی سواربوون چاوەڕێی کرد، بەڵام لە کاتی خۆیدا نەهاتیت، بۆیە گەشتەکە هەڵوەشێنرایەوە. کرێی نەهاتن لە جزدانەکەت دەبڕدرێت.');

-- +goose Down

DELETE FROM notification_template_translations WHERE event_key = 'trip.no_show';
DELETE FROM notification_templates WHERE event_key = 'trip.no_show';
