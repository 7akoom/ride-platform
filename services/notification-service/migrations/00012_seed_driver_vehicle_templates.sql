-- +goose Up

-- What a driver is told when staff review a car they added.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('driver.vehicle_approved', ARRAY['in_app', 'push']),
    ('driver.vehicle_rejected', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('driver.vehicle_approved', 'en', 'Car approved',
     'Your {car} ({plate}) is approved. You can switch to it while offline.'),
    ('driver.vehicle_approved', 'ar', 'تمت الموافقة على السيارة',
     'تمت الموافقة على {car} ({plate}). يمكنك التبديل إليها وأنت غير متصل.'),
    ('driver.vehicle_approved', 'ku', 'ئۆتۆمبێلەکە پەسەند کرا',
     '{car} ({plate}) پەسەند کرا. کاتێک ئۆفلاینیت دەتوانیت بیگۆڕیت بۆی.'),

    ('driver.vehicle_rejected', 'en', 'Car not accepted',
     'Your {car} ({plate}) was not accepted: {reason}'),
    ('driver.vehicle_rejected', 'ar', 'لم يتم قبول السيارة',
     'لم يتم قبول {car} ({plate}): {reason}'),
    ('driver.vehicle_rejected', 'ku', 'ئۆتۆمبێلەکە وەرنەگیرا',
     '{car} ({plate}) وەرنەگیرا: {reason}');

-- +goose Down

DELETE FROM notification_templates
WHERE event_key IN ('driver.vehicle_approved', 'driver.vehicle_rejected');
