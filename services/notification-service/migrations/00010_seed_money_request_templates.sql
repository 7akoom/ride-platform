-- +goose Up

-- What a rider is told when another rider asked them for money.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('wallet.money_requested', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('wallet.money_requested', 'en', 'Money request',
     '{requester} asked you for {amount} {currency}.'),
    ('wallet.money_requested', 'ar', 'طلب مبلغ',
     '{requester} طلب منك {amount} {currency}.'),
    ('wallet.money_requested', 'ku', 'داواکاری پارە',
     '{requester} داوای {amount} {currency}ی لێکردیت.');

-- +goose Down

DELETE FROM notification_templates
WHERE event_key = 'wallet.money_requested';
