-- +goose Up

-- A rider handed the driver more than the cash due and the driver had no change:
-- the difference went to the rider's wallet (wallet.change_credited). The rider is
-- told, so it does not wait to be found in the wallet.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('wallet.change_credited', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('wallet.change_credited', 'en', 'Your change is in your wallet',
     'Your captain had no change: {amount} {currency} from your trip was added to your wallet.'),
    ('wallet.change_credited', 'ar', 'أُضيف الباقي إلى محفظتك',
     'لم يكن مع الكابتن باقٍ: أُضيف {amount} {currency} من رحلتك إلى محفظتك.'),
    ('wallet.change_credited', 'ku', 'باقییەکەت خرایە جزدانەکەت',
     'کاپتن باقیی پێ نەبوو: {amount} {currency} لە گەشتەکەت خرایە سەر جزدانەکەت.');

-- +goose Down

DELETE FROM notification_template_translations WHERE event_key = 'wallet.change_credited';
DELETE FROM notification_templates WHERE event_key = 'wallet.change_credited';
