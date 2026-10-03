-- +goose Up

-- What a driver is told when an incentive campaign paid them.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('driver.incentive_earned', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('driver.incentive_earned', 'en', 'Bonus earned',
     'You completed {trips} trips in {campaign}: {amount} {currency} was added to your wallet.'),
    ('driver.incentive_earned', 'ar', 'حصلت على مكافأة',
     'أكملت {trips} رحلة ضمن {campaign}: أُضيف {amount} {currency} إلى محفظتك.'),
    ('driver.incentive_earned', 'ku', 'پاداشتت وەرگرت',
     '{trips} گەشتت تەواو کرد لە {campaign}: {amount} {currency} زیادکرا بۆ جزدانەکەت.');

-- +goose Down

DELETE FROM notification_templates
WHERE event_key = 'driver.incentive_earned';
