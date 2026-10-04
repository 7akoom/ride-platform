-- +goose Up

-- First-response time (SLA), when the status last changed (auto-resolve and
-- auto-close count from it), and the requester's rating.
ALTER TABLE support_tickets
    ADD COLUMN first_response_due_at TIMESTAMPTZ NULL,
    ADD COLUMN status_changed_at TIMESTAMPTZ NULL,
    ADD COLUMN rating SMALLINT NULL,
    ADD COLUMN rating_comment VARCHAR(1000) NOT NULL DEFAULT '',
    ADD COLUMN rated_at TIMESTAMPTZ NULL,
    ADD CONSTRAINT support_tickets_rating_check CHECK (rating IS NULL OR rating BETWEEN 1 AND 5),
    ADD CONSTRAINT support_tickets_rated_check CHECK ((rating IS NULL) = (rated_at IS NULL));

-- Tickets from before: the default targets (urgent 15m, high 1h, normal 4h,
-- low 24h), and their last change as the status time.
UPDATE support_tickets SET
    first_response_due_at = created_at + CASE priority
        WHEN 'urgent' THEN interval '15 minutes'
        WHEN 'high' THEN interval '1 hour'
        WHEN 'normal' THEN interval '4 hours'
        ELSE interval '24 hours' END,
    status_changed_at = updated_at;

ALTER TABLE support_tickets
    ALTER COLUMN first_response_due_at SET NOT NULL,
    ALTER COLUMN status_changed_at SET NOT NULL;

CREATE INDEX support_tickets_status_changed_idx
    ON support_tickets (status, status_changed_at)
    WHERE status IN ('waiting_user', 'resolved');

CREATE INDEX support_tickets_created_idx ON support_tickets (created_at);

CREATE TABLE support_help_sections (
    key VARCHAR(40) PRIMARY KEY,
    audience VARCHAR(10) NOT NULL,
    name_en VARCHAR(80) NOT NULL,
    name_ar VARCHAR(80) NOT NULL,
    name_ku VARCHAR(80) NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT support_help_sections_key_check CHECK (key ~ '^[a-z][a-z0-9_]{1,39}$'),
    CONSTRAINT support_help_sections_audience_check CHECK (audience IN ('rider', 'driver', 'both'))
);

CREATE TABLE support_help_articles (
    key VARCHAR(80) PRIMARY KEY,
    section_key VARCHAR(40) NOT NULL REFERENCES support_help_sections (key),
    audience VARCHAR(10) NOT NULL,
    title_en VARCHAR(160) NOT NULL,
    title_ar VARCHAR(160) NOT NULL,
    title_ku VARCHAR(160) NOT NULL,
    body_en TEXT NOT NULL,
    body_ar TEXT NOT NULL,
    body_ku TEXT NOT NULL,
    contact_category_key VARCHAR(40) NULL REFERENCES support_categories (key),
    sort_order INTEGER NOT NULL DEFAULT 0,
    published BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT support_help_articles_key_check CHECK (key ~ '^[a-z][a-z0-9-]{1,79}$'),
    CONSTRAINT support_help_articles_audience_check CHECK (audience IN ('rider', 'driver', 'both')),
    CONSTRAINT support_help_articles_body_check
        CHECK (length(body_en) <= 20000 AND length(body_ar) <= 20000 AND length(body_ku) <= 20000)
);

CREATE INDEX support_help_articles_section_idx ON support_help_articles (section_key, sort_order, key);

CREATE TABLE support_help_votes (
    article_key VARCHAR(80) NOT NULL REFERENCES support_help_articles (key) ON DELETE CASCADE,
    identity_id UUID NOT NULL,
    helpful BOOLEAN NOT NULL,
    voted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (article_key, identity_id)
);

CREATE TABLE support_macros (
    key VARCHAR(40) PRIMARY KEY,
    title VARCHAR(120) NOT NULL,
    body_en TEXT NOT NULL,
    body_ar TEXT NOT NULL,
    body_ku TEXT NOT NULL,
    category_key VARCHAR(40) NULL REFERENCES support_categories (key),
    active BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT support_macros_key_check CHECK (key ~ '^[a-z][a-z0-9_]{1,39}$'),
    CONSTRAINT support_macros_body_check
        CHECK (length(body_en) BETWEEN 1 AND 4000 AND length(body_ar) BETWEEN 1 AND 4000
               AND length(body_ku) BETWEEN 1 AND 4000)
);

-- Starting content. Every instance edits it from the Admin (the Kurdish needs
-- a native review).
INSERT INTO support_help_sections (key, audience, name_en, name_ar, name_ku, sort_order) VALUES
    ('rides', 'both', 'Rides', 'الرحلات', 'گەشتەکان', 10),
    ('payments', 'both', 'Payments and wallet', 'الدفع والمحفظة', 'پارەدان و جزدان', 20),
    ('account', 'both', 'Account', 'الحساب', 'هەژمار', 30),
    ('safety', 'both', 'Safety', 'السلامة', 'سەلامەتی', 40),
    ('driving', 'driver', 'Driving and earnings', 'القيادة والأرباح', 'شۆفێری و داهات', 50);

INSERT INTO support_help_articles
    (key, section_key, audience, title_en, title_ar, title_ku, body_en, body_ar, body_ku, contact_category_key, sort_order, published)
VALUES
    ('lost-item', 'rides', 'rider',
     'I left something in the car',
     'نسيت شيئاً في السيارة',
     'شتێکم لە ئۆتۆمبێلەکە بەجێهێشت',
     'Open the trip in your history and report a lost item. The captain is told at once and can answer you inside the ticket; your phone number stays private.',
     'افتح الرحلة من سجل رحلاتك وأبلغ عن غرض مفقود. يصل الإشعار إلى الكابتن فوراً ويمكنه الرد عليك داخل التذكرة، ويبقى رقم هاتفك خاصاً.',
     'گەشتەکە لە مێژووی گەشتەکانت بکەرەوە و ڕاپۆرتی شتی ونبوو بدە. کاپتن یەکسەر ئاگادار دەکرێتەوە و دەتوانێت لەناو تیکێتەکەدا وەڵامت بداتەوە؛ ژمارەی تەلەفۆنەکەت نهێنی دەمێنێتەوە.',
     'lost_item', 10, TRUE),
    ('fare-higher-than-expected', 'rides', 'rider',
     'My fare was higher than expected',
     'الأجرة أعلى من المتوقع',
     'کرێکە لە چاوەڕوانی زیاتر بوو',
     'A fare quoted before the trip is the fare you pay. Without a quote it is worked out at the end from the distance and time, waiting time and any surge. If something looks wrong, contact us from the trip and we will check it.',
     'الأجرة المعروضة قبل الرحلة هي ما تدفعه. وبدون عرض سعر تُحسب في النهاية حسب المسافة والوقت ووقت الانتظار والازدحام. إذا بدا لك شيء غير صحيح تواصل معنا من الرحلة وسنتحقق منه.',
     'ئەو کرێیەی پێش گەشت پیشان دەدرێت ئەوەیە کە دەیدەیت. بەبێ نرخی پێشوەختە لە کۆتاییدا بەپێی دووری و کات و کاتی چاوەڕوانی و قەرەباڵغی هەژمار دەکرێت. ئەگەر شتێک هەڵە دەرکەوت لە گەشتەکەوە پەیوەندیمان پێوە بکە.',
     'trip_fare', 20, TRUE),
    ('cancellation-fee', 'rides', 'both',
     'Why was I charged a cancellation fee?',
     'لماذا دفعت رسوم إلغاء؟',
     'بۆچی کرێی هەڵوەشاندنەوەم لێ وەرگیرا؟',
     'A fee applies when a trip is cancelled after the free cancellation time, or when the rider does not show up. It is not charged if the captain was late. If you think it was charged by mistake, contact us and we can waive it.',
     'تُفرض الرسوم عند إلغاء الرحلة بعد مهلة الإلغاء المجاني، أو عند عدم حضور الراكب. ولا تُفرض إذا تأخر الكابتن. إذا كنت تعتقد أنها فُرضت بالخطأ تواصل معنا ويمكننا إلغاؤها.',
     'کرێ دەسەپێنرێت کاتێک گەشت دوای ماوەی هەڵوەشاندنەوەی بێبەرامبەر هەڵدەوەشێتەوە، یان سەرنشین نایەت. ئەگەر کاپتن دواکەوتبێت وەرناگیرێت. ئەگەر پێتوایە بە هەڵە وەرگیراوە پەیوەندیمان پێوە بکە.',
     'trip_fare', 30, TRUE),
    ('top-up-wallet', 'payments', 'both',
     'How do I top up my wallet?',
     'كيف أشحن محفظتي؟',
     'چۆن جزدانەکەم پڕ بکەمەوە؟',
     'Open the wallet and choose top up: you pay on the provider''s own page, so your card or PIN never reaches us. You can also redeem a voucher code. Money in the wallet pays trips first, then any fee still owed.',
     'افتح المحفظة واختر الشحن: تدفع في صفحة مزوّد الدفع نفسه، فلا تصلنا بيانات بطاقتك أو رمزك السري. ويمكنك أيضاً استخدام رمز قسيمة. يُستخدم رصيد المحفظة للرحلات، ويسدد أولاً أي رسوم متأخرة.',
     'جزدانەکە بکەرەوە و پڕکردنەوە هەڵبژێرە: لە پەڕەی دابینکەری پارەدان خۆیدا دەدەیت، بۆیە کارت یان ژمارەی نهێنیت ناگاتە ئێمە. هەروەها دەتوانیت کۆدی کارتی پێشوەختە بەکاربهێنیت.',
     'payment_wallet', 10, TRUE),
    ('wallet-pin', 'payments', 'rider',
     'I forgot my wallet PIN',
     'نسيت الرمز السري للمحفظة',
     'ژمارەی نهێنی جزدانەکەم لەبیرچوو',
     'Sign in again with the code sent to your phone, then set a new PIN within 10 minutes; the old one is not needed. Several wrong PINs lock it for a while.',
     'سجّل الدخول مجدداً بالرمز المرسل إلى هاتفك، ثم عيّن رمزاً جديداً خلال 10 دقائق دون الحاجة إلى القديم. إدخال رمز خاطئ عدة مرات يقفل المحفظة لفترة.',
     'دووبارە بە کۆدی نێردراو بۆ تەلەفۆنەکەت بچۆرە ژوورەوە، پاشان لە ماوەی ١٠ خولەکدا ژمارەیەکی نهێنی نوێ دابنێ.',
     'payment_wallet', 20, TRUE),
    ('account-suspended', 'account', 'both',
     'My account was suspended',
     'تم إيقاف حسابي',
     'هەژمارەکەم ڕاگیرا',
     'An account can be suspended while a complaint is looked into. You cannot sign in until it is lifted. Contact us and we will tell you what happens next.',
     'قد يُوقف الحساب أثناء التحقق من شكوى، ولا يمكنك تسجيل الدخول حتى يُرفع الإيقاف. تواصل معنا وسنخبرك بالخطوات التالية.',
     'لەوانەیە هەژمار ڕابگیرێت لە کاتی لێکۆڵینەوە لە سکاڵایەک. تا ڕادەگیرێت ناتوانیت بچیتە ژوورەوە. پەیوەندیمان پێوە بکە.',
     'account', 10, TRUE),
    ('safety-during-trip', 'safety', 'both',
     'Staying safe during a trip',
     'سلامتك أثناء الرحلة',
     'سەلامەتی لە کاتی گەشتدا',
     'Check the car and plate before getting in, and share your trip with someone you trust. In danger, press SOS in the app: our team is alerted at once. SOS does not replace calling the emergency services.',
     'تأكد من السيارة ورقم اللوحة قبل الصعود، وشارك رحلتك مع شخص تثق به. عند الخطر اضغط زر الاستغاثة في التطبيق فيصل التنبيه إلى فريقنا فوراً. زر الاستغاثة لا يغني عن الاتصال بالطوارئ.',
     'پێش سواربوون ئۆتۆمبێل و ژمارەی تابلۆکە بپشکنە، و گەشتەکەت لەگەڵ کەسێکی متمانەپێکراو هاوبەش بکە. لە کاتی مەترسیدا دوگمەی فریاکەوتن دابگرە. ئەمە جێگەی پەیوەندی بە فریاکەوتنەوە ناگرێتەوە.',
     'safety', 10, TRUE),
    ('documents-expiring', 'driving', 'driver',
     'My documents are about to expire',
     'وثائقي على وشك الانتهاء',
     'بەڵگەنامەکانم خەریکە بەسەردەچن',
     'You are reminded 30, 7 and 1 days before a document runs out. Upload the renewed one from Documents; the old one stays valid until the new one is approved. On the day it expires you cannot go online.',
     'يصلك تذكير قبل انتهاء الوثيقة بـ 30 و7 ويوم واحد. ارفع الوثيقة المجددة من قسم الوثائق، وتبقى القديمة سارية حتى تُقبل الجديدة. في يوم انتهائها لا يمكنك العمل.',
     'پێش بەسەرچوونی بەڵگەنامە ٣٠ و ٧ و ١ ڕۆژ بیرت دەخرێتەوە. نوێکراوەکە لە بەشی بەڵگەنامەکان باربکە؛ کۆنەکە تا پەسەندکردنی نوێکە کاردەکات.',
     'documents_vehicle', 10, TRUE),
    ('how-payouts-work', 'driving', 'driver',
     'How do payouts work?',
     'كيف يتم سحب الأرباح؟',
     'وەرگرتنی داهات چۆن کاردەکات؟',
     'Ask for a payout from your wallet: the amount is held at once and our team pays it and marks it paid with a reference. A rejected request gives the money back to your wallet.',
     'اطلب السحب من محفظتك: يُحجز المبلغ فوراً ثم يدفعه فريقنا ويعلّمه مدفوعاً مع رقم مرجعي. وإذا رُفض الطلب يعود المبلغ إلى محفظتك.',
     'داوای وەرگرتنی پارە لە جزدانەکەت بکە: بڕەکە یەکسەر دەگیرێت و تیمەکەمان دەیدات. ئەگەر داواکە ڕەتکرایەوە پارەکە دەگەڕێتەوە بۆ جزدانەکەت.',
     'earnings_payout', 20, TRUE);

INSERT INTO support_macros (key, title, body_en, body_ar, body_ku, category_key, sort_order) VALUES
    ('checking', 'We are checking',
     'Thank you for telling us. We are checking this and will get back to you here shortly.',
     'شكراً لإبلاغنا. نحن نتحقق من الأمر وسنعود إليك هنا قريباً.',
     'سوپاس بۆ ئاگادارکردنەوەمان. ئێمە سەیری دەکەین و بەم زووانە لێرە وەڵامت دەدەینەوە.',
     NULL, 10),
    ('refunded', 'Refund made',
     'We have refunded the amount to your wallet. Sorry for the trouble.',
     'أعدنا المبلغ إلى محفظتك. نعتذر عن الإزعاج.',
     'بڕەکەمان گەڕاندەوە بۆ جزدانەکەت. ببورە بۆ ئەم کێشەیە.',
     'trip_fare', 20),
    ('fee_waived', 'Fee waived',
     'We have waived the fee on this trip.',
     'ألغينا الرسوم المفروضة على هذه الرحلة.',
     'کرێی ئەم گەشتەمان هەڵوەشاندەوە.',
     'trip_fare', 30),
    ('lost_item_driver', 'Lost item: asking the captain',
     'We have asked the captain to check the car. You will see the answer in this ticket.',
     'طلبنا من الكابتن التحقق من السيارة، وسترى الرد في هذه التذكرة.',
     'داوامان لە کاپتن کرد ئۆتۆمبێلەکە بپشکنێت. وەڵامەکە لەم تیکێتەدا دەبینیت.',
     'lost_item', 40),
    ('need_details', 'Need more details',
     'Could you tell us a little more, and attach a screenshot if you can?',
     'هل يمكنك إخبارنا بمزيد من التفاصيل وإرفاق صورة للشاشة إن أمكن؟',
     'دەتوانیت کەمێکی تر زانیاریمان پێبدەیت و ئەگەر بکرێت وێنەی شاشە هاوپێچ بکەیت؟',
     NULL, 50),
    ('resolved_followup', 'Resolved',
     'We are marking this as resolved. If anything is still wrong, just reply here.',
     'سنعتبر هذه التذكرة محلولة. إذا كان هناك أي مشكلة بعد، رد هنا فقط.',
     'ئەم تیکێتە وەک چارەسەرکراو دیاری دەکەین. ئەگەر هێشتا کێشەیەک هەیە تەنها لێرە وەڵام بدەرەوە.',
     NULL, 60);

-- +goose Down

DROP TABLE support_macros;
DROP TABLE support_help_votes;
DROP TABLE support_help_articles;
DROP TABLE support_help_sections;

DROP INDEX support_tickets_created_idx;
DROP INDEX support_tickets_status_changed_idx;

ALTER TABLE support_tickets
    DROP CONSTRAINT support_tickets_rated_check,
    DROP CONSTRAINT support_tickets_rating_check,
    DROP COLUMN rated_at,
    DROP COLUMN rating_comment,
    DROP COLUMN rating,
    DROP COLUMN status_changed_at,
    DROP COLUMN first_response_due_at;
