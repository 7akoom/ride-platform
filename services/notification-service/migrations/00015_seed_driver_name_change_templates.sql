-- +goose Up

-- What a driver is told when staff review a name change they asked for.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('driver.name_change_approved', ARRAY['in_app', 'push']),
    ('driver.name_change_rejected', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('driver.name_change_approved', 'en', 'Name updated',
     'Your name is now {name}. Riders will see it from your next trip.'),
    ('driver.name_change_approved', 'ar', 'تم تحديث الاسم',
     'أصبح اسمك {name}. سيظهر للركاب من رحلتك القادمة.'),
    ('driver.name_change_approved', 'ku', 'ناوەکەت نوێ کرایەوە',
     'ناوت ئێستا {name}ـە. سەرنشینان لە گەشتی داهاتووتەوە دەیبینن.'),

    ('driver.name_change_rejected', 'en', 'Name change not accepted',
     'Your request to change your name to {name} was not accepted: {reason}'),
    ('driver.name_change_rejected', 'ar', 'لم يتم قبول تغيير الاسم',
     'لم يتم قبول طلب تغيير اسمك إلى {name}: {reason}'),
    ('driver.name_change_rejected', 'ku', 'گۆڕینی ناو وەرنەگیرا',
     'داواکاریی گۆڕینی ناوت بۆ {name} وەرنەگیرا: {reason}');

-- +goose Down

DELETE FROM notification_templates
WHERE event_key IN ('driver.name_change_approved', 'driver.name_change_rejected');
