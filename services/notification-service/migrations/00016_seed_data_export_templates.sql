-- +goose Up

-- What a person is told when their "Download your data" file is ready.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('account.data_export_ready', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('account.data_export_ready', 'en', 'Your data is ready',
     'The copy of your data you asked for is ready. Download it from your account settings before {date}.'),
    ('account.data_export_ready', 'ar', 'بياناتك جاهزة',
     'نسخة بياناتك التي طلبتها جاهزة. نزّلها من إعدادات حسابك قبل {date}.'),
    ('account.data_export_ready', 'ku', 'زانیارییەکانت ئامادەن',
     'ئەو کۆپییەی زانیارییەکانت کە داوات کردبوو ئامادەیە. پێش {date} لە ڕێکخستنەکانی هەژمارەکەت دایبەزێنە.');

-- +goose Down

DELETE FROM notification_templates WHERE event_key = 'account.data_export_ready';
