-- +goose Up

-- What a driver is told about their account review and their documents.
-- Each translation names the document in its own language.
INSERT INTO notification_templates (event_key, default_channels) VALUES
    ('driver.account_approved', ARRAY['in_app', 'push']),
    ('driver.account_rejected', ARRAY['in_app', 'push']),
    ('driver.document_approved', ARRAY['in_app', 'push']),
    ('driver.document_rejected', ARRAY['in_app', 'push']),
    ('driver.document_withdrawn', ARRAY['in_app', 'push']),
    ('driver.document_expiring', ARRAY['in_app', 'push']),
    ('driver.document_expired', ARRAY['in_app', 'push']);

INSERT INTO notification_template_translations (event_key, locale, title, body) VALUES
    ('driver.account_approved', 'en', 'You are approved',
     'Your account is approved. Go online to start taking trips.'),
    ('driver.account_approved', 'ar', 'تمت الموافقة على حسابك',
     'تمت الموافقة على حسابك. فعّل الاتصال لتبدأ باستقبال الرحلات.'),
    ('driver.account_approved', 'ku', 'هەژمارەکەت پەسەند کرا',
     'هەژمارەکەت پەسەند کرا. ئۆنلاین بە بۆ وەرگرتنی گەشتەکان.'),

    ('driver.account_rejected', 'en', 'Your application needs attention',
     'Your application was not approved: {reason}'),
    ('driver.account_rejected', 'ar', 'طلبك يحتاج مراجعة',
     'لم تتم الموافقة على طلبك: {reason}'),
    ('driver.account_rejected', 'ku', 'داواکارییەکەت پێویستی بە چاککردنە',
     'داواکارییەکەت پەسەند نەکرا: {reason}'),

    ('driver.document_approved', 'en', 'Document approved',
     'Your {document_en} is approved.'),
    ('driver.document_approved', 'ar', 'تم قبول الوثيقة',
     'تم قبول {document_ar}.'),
    ('driver.document_approved', 'ku', 'بەڵگەنامە پەسەند کرا',
     '{document_ku} پەسەند کرا.'),

    ('driver.document_rejected', 'en', 'Document not accepted',
     'Your {document_en} was not accepted: {reason}. Please upload it again.'),
    ('driver.document_rejected', 'ar', 'لم يتم قبول الوثيقة',
     'لم يتم قبول {document_ar}: {reason}. يرجى رفعها من جديد.'),
    ('driver.document_rejected', 'ku', 'بەڵگەنامە وەرنەگیرا',
     '{document_ku} وەرنەگیرا: {reason}. تکایە دووبارە بارى بکەرەوە.'),

    ('driver.document_withdrawn', 'en', 'Document no longer accepted',
     'Your {document_en} is no longer accepted: {reason}. Upload a new one to keep working.'),
    ('driver.document_withdrawn', 'ar', 'الوثيقة لم تعد مقبولة',
     'لم يعد {document_ar} مقبولاً: {reason}. ارفع وثيقة جديدة لتستمر بالعمل.'),
    ('driver.document_withdrawn', 'ku', 'بەڵگەنامە ئیتر وەرناگیرێت',
     '{document_ku} ئیتر وەرناگیرێت: {reason}. بەڵگەنامەیەکی نوێ بار بکە بۆ بەردەوامبوون.'),

    ('driver.document_expiring', 'en', 'Document expiring soon',
     'Your {document_en} expires in {days} day(s), on {date}. Upload the renewed one in time.'),
    ('driver.document_expiring', 'ar', 'وثيقتك ستنتهي قريباً',
     'ينتهي {document_ar} بعد {days} يوم، بتاريخ {date}. ارفع الوثيقة المجددة قبل ذلك.'),
    ('driver.document_expiring', 'ku', 'بەڵگەنامەکەت بەم زووانە بەسەر دەچێت',
     '{document_ku} لە ماوەی {days} ڕۆژدا بەسەر دەچێت، لە {date}. نوێکراوەکەی لە کاتی خۆیدا بار بکە.'),

    ('driver.document_expired', 'en', 'Document expired',
     'Your {document_en} expired on {date}. Upload the renewed one to go online again.'),
    ('driver.document_expired', 'ar', 'انتهت صلاحية الوثيقة',
     'انتهت صلاحية {document_ar} بتاريخ {date}. ارفع الوثيقة المجددة لتعود للعمل.'),
    ('driver.document_expired', 'ku', 'بەڵگەنامە بەسەرچوو',
     '{document_ku} لە {date} بەسەرچوو. نوێکراوەکەی بار بکە بۆ ئەوەی دووبارە ئۆنلاین بیت.');

-- +goose Down

DELETE FROM notification_templates
WHERE event_key IN (
    'driver.account_approved',
    'driver.account_rejected',
    'driver.document_approved',
    'driver.document_rejected',
    'driver.document_withdrawn',
    'driver.document_expiring',
    'driver.document_expired'
);
