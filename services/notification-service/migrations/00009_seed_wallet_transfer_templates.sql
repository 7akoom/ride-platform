-- +goose Up

-- What a rider is told when another rider sent them money.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('wallet.transfer_received', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('wallet.transfer_received', 'en', 'Money received',
     '{amount} {currency} arrived in your wallet from {sender}.'),
    ('wallet.transfer_received', 'ar', 'وصلك مبلغ',
     'وصل {amount} {currency} إلى محفظتك من {sender}.'),
    ('wallet.transfer_received', 'ku', 'پارە گەیشت',
     '{amount} {currency} لە {sender}ەوە گەیشتە جزدانەکەت.');

-- +goose Down

DELETE FROM notification_templates
WHERE event_key = 'wallet.transfer_received';
