-- +goose Up

-- What riders and drivers are told about their support tickets. The push
-- never carries the message itself, only that there is something new.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('support.reply_received', ARRAY['in_app', 'push']),
    ('support.ticket_resolved', ARRAY['in_app', 'push']),
    ('support.lost_item_reported', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('support.reply_received', 'en', 'New reply from support',
     'There is a new reply on your ticket {ticket}.'),
    ('support.reply_received', 'ar', 'رد جديد من الدعم',
     'يوجد رد جديد على تذكرتك {ticket}.'),
    ('support.reply_received', 'ku', 'وەڵامێکی نوێ لە پشتگیری',
     'وەڵامێکی نوێ هەیە لەسەر تیکێتەکەت {ticket}.'),

    ('support.ticket_resolved', 'en', 'Your ticket is resolved',
     'Support marked your ticket {ticket} as resolved. Reply if you still need help.'),
    ('support.ticket_resolved', 'ar', 'تم حل تذكرتك',
     'أغلق فريق الدعم تذكرتك {ticket} كمحلولة. رد عليها إذا كنت ما زلت بحاجة إلى مساعدة.'),
    ('support.ticket_resolved', 'ku', 'تیکێتەکەت چارەسەر کرا',
     'پشتگیری تیکێتەکەت {ticket} وەک چارەسەرکراو دیاری کرد. ئەگەر هێشتا یارمەتیت پێویستە وەڵام بدەرەوە.'),

    ('support.lost_item_reported', 'en', 'A rider lost an item',
     'A rider reported an item left in your car (ticket {ticket}). Please check and answer in the ticket.'),
    ('support.lost_item_reported', 'ar', 'راكب نسي غرضاً',
     'أبلغ راكب عن غرض نسيه في سيارتك (تذكرة {ticket}). يرجى التحقق والرد داخل التذكرة.'),
    ('support.lost_item_reported', 'ku', 'سەرنشینێک شتێکی بەجێهێشت',
     'سەرنشینێک ڕایگەیاند شتێکی لە ئۆتۆمبێلەکەت بەجێهێشتووە (تیکێت {ticket}). تکایە بپشکنە و لە تیکێتەکەدا وەڵام بدەرەوە.');

-- +goose Down

DELETE FROM notification_templates
WHERE event_key IN ('support.reply_received', 'support.ticket_resolved', 'support.lost_item_reported');
