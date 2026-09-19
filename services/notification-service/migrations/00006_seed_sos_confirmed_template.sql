-- +goose Up

-- Confirmation sent to whoever pressed the SOS button. The wording only
-- claims the alert was recorded: no operator alerting exists yet, so the
-- text tells the user to call local emergency services themselves.

INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('trip.sos_confirmed', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('trip.sos_confirmed', 'en', 'SOS alert received',
     'Your emergency alert was recorded with your trip details. If you are in danger, call your local emergency number now.'),
    ('trip.sos_confirmed', 'ar', 'تم استلام تنبيه الطوارئ',
     'تم تسجيل تنبيه الطوارئ الخاص بك مع تفاصيل رحلتك. إذا كنت في خطر، اتصل برقم الطوارئ المحلي فوراً.'),
    ('trip.sos_confirmed', 'ku', 'ئاگادارکردنەوەی فریاکەوتن تۆمار کرا',
     'ئاگادارکردنەوەی فریاکەوتنەکەت لەگەڵ زانیاری گەشتەکەت تۆمار کرا. ئەگەر لە مەترسیدایت، ئێستا پەیوەندی بە ژمارەی فریاکەوتنی ناوخۆییەوە بکە.');

-- +goose Down

DELETE FROM notification_templates
WHERE event_key = 'trip.sos_confirmed';
