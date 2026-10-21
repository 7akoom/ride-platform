-- +goose Up

-- Offers for riders (pricing-service): the next completed trip gets the loyalty
-- discount, and a featured coupon started running. Both are in the inbox's
-- "offers" tab (event keys promo.*) and ring the phone.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('promo.loyalty_due', ARRAY['in_app', 'push']),
    ('promo.featured_coupon', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('promo.loyalty_due', 'en', 'Your next ride is {percent}% off',
     'Thanks for riding with us. Your next ride gets a {percent}% discount by itself.'),
    ('promo.loyalty_due', 'ar', 'رحلتك القادمة بخصم {percent}%',
     'شكراً لأنك تركب معنا. رحلتك القادمة عليها خصم {percent}% تلقائياً.'),
    ('promo.loyalty_due', 'ku', 'گەشتی داهاتووت {percent}% داشکاندنی هەیە',
     'سوپاس بۆ گەشتکردن لەگەڵمان. گەشتی داهاتووت خۆکارانە {percent}% داشکاندنی هەیە.'),

    ('promo.featured_coupon', 'en', 'New offer: {description}',
     'Use the code {code} on your next ride.'),
    ('promo.featured_coupon', 'ar', 'عرض جديد: {description}',
     'استخدم الكود {code} في رحلتك القادمة.'),
    ('promo.featured_coupon', 'ku', 'ئۆفەری نوێ: {description}',
     'کۆدی {code} لە گەشتی داهاتووتدا بەکاربهێنە.');

-- +goose Down

DELETE FROM notification_template_translations WHERE event_key IN ('promo.loyalty_due', 'promo.featured_coupon');
DELETE FROM notification_templates WHERE event_key IN ('promo.loyalty_due', 'promo.featured_coupon');
