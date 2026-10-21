package dataexport

// The words of the readable page (index.html), in each language a person may ask
// in. Field names without a label here are shown as they are, with spaces.

type pageWords struct {
	dir, title, intro, files, times, empty, yes, no string
}

var pageText = map[string]pageWords{
	"en": {
		dir: "ltr", title: "Your data",
		intro: "What the service keeps about your account, made on %s. The same data is in the JSON files beside this page, for other programs.",
		files: "Contents", times: "Times are in %s.", empty: "Nothing kept.", yes: "Yes", no: "No",
	},
	"ar": {
		dir: "rtl", title: "بياناتك",
		intro: "ما يحتفظ به التطبيق عن حسابك، أُنشئ بتاريخ %s. نفس البيانات موجودة في ملفات JSON بجانب هذه الصفحة، لاستخدامها في برامج أخرى.",
		files: "المحتويات", times: "الأوقات بتوقيت %s.", empty: "لا توجد بيانات محفوظة.", yes: "نعم", no: "لا",
	},
	"ku": {
		dir: "rtl", title: "زانیارییەکانت",
		intro: "ئەوەی خزمەتگوزارییەکە دەربارەی هەژمارەکەت دەیپارێزێت، لە %s دروستکراوە. هەمان زانیاری لە فایلەکانی JSON لە تەنیشت ئەم پەڕەیە هەیە، بۆ بەرنامەکانی تر.",
		files: "ناوەڕۆک", times: "کاتەکان بە کاتی %s نیشان دراون.", empty: "هیچ شتێک نەپارێزراوە.", yes: "بەڵێ", no: "نەخێر",
	},
}

// sectionTitles names each file of the export: en, ar, ku.
var sectionTitles = map[string][3]string{
	"identity/account.json":              {"Account", "الحساب", "هەژمار"},
	"identity/sessions.json":             {"Signed-in devices", "الأجهزة المسجّل دخولها", "ئامێرە چوونەژوورەوەکان"},
	"identity/sign_in_methods.json":      {"Phone and email", "الهاتف والبريد", "تەلەفۆن و ئیمەیڵ"},
	"identity/wallet_pin.json":           {"Wallet PIN", "رمز المحفظة", "کۆدی جزدان"},
	"rider/profile.json":                 {"Rider profile", "ملف الراكب", "پرۆفایلی سەرنشین"},
	"rider/details.json":                 {"Personal details", "المعلومات الشخصية", "زانیاری کەسی"},
	"rider/saved_addresses.json":         {"Saved addresses", "العناوين المحفوظة", "ناونیشانە پاشەکەوتکراوەکان"},
	"driver/profile.json":                {"Captain profile", "ملف الكابتن", "پرۆفایلی کاپتن"},
	"driver/details.json":                {"Captain details", "معلومات الكابتن", "زانیاری کاپتن"},
	"driver/documents.json":              {"Documents", "الوثائق", "بەڵگەنامەکان"},
	"driver/vehicles.json":               {"Vehicles", "المركبات", "ئۆتۆمبێلەکان"},
	"driver/name_changes.json":           {"Name changes", "طلبات تغيير الاسم", "گۆڕینی ناو"},
	"trips/as_rider.json":                {"Your trips", "رحلاتك", "گەشتەکانت"},
	"trips/as_driver.json":               {"Trips you drove", "الرحلات التي قدتها", "ئەو گەشتانەی لێتخوڕیون"},
	"trips/scheduled.json":               {"Booked rides", "الرحلات المحجوزة", "گەشتە پێشوەختەکان"},
	"trips/ratings_given_as_rider.json":  {"Your ratings", "تقييماتك", "هەڵسەنگاندنەکانت"},
	"trips/ratings_given_as_driver.json": {"Your ratings as a captain", "تقييماتك ككابتن", "هەڵسەنگاندنەکانت وەک کاپتن"},
	"wallet/rider_wallet.json":           {"Wallet", "المحفظة", "جزدان"},
	"wallet/rider_transactions.json":     {"Wallet transactions", "حركات المحفظة", "مامەڵەکانی جزدان"},
	"wallet/driver_wallet.json":          {"Captain wallet", "محفظة الكابتن", "جزدانی کاپتن"},
	"wallet/driver_transactions.json":    {"Captain wallet transactions", "حركات محفظة الكابتن", "مامەڵەکانی جزدانی کاپتن"},
	"wallet/transfers.json":              {"Money sent and received", "التحويلات", "پارە ناردن و وەرگرتن"},
	"wallet/money_requests.json":         {"Money requests", "طلبات المال", "داواکاری پارە"},
	"wallet/payouts.json":                {"Payouts", "السحوبات", "پارە ڕاکێشان"},
	"support/tickets.json":               {"Support tickets", "تذاكر الدعم", "تکتەکانی پشتگیری"},
	"notifications/rider_inbox.json":     {"Notifications", "الإشعارات", "ئاگادارکردنەوەکان"},
	"notifications/driver_inbox.json":    {"Captain notifications", "إشعارات الكابتن", "ئاگادارکردنەوەکانی کاپتن"},
	"notifications/rider_devices.json":   {"Phones for notifications", "الأجهزة المستقبلة للإشعارات", "ئامێرەکانی ئاگادارکردنەوە"},
	"notifications/driver_devices.json":  {"Captain phones for notifications", "أجهزة الكابتن للإشعارات", "ئامێرەکانی کاپتن بۆ ئاگادارکردنەوە"},
}

// fieldLabels names the fields that come up most: en, ar, ku.
var fieldLabels = map[string][3]string{
	"id":                  {"Id", "المعرّف", "ناسنامە"},
	"number":              {"Number", "الرقم", "ژمارە"},
	"status":              {"Status", "الحالة", "دۆخ"},
	"type":                {"Type", "النوع", "جۆر"},
	"kind":                {"Kind", "النوع", "جۆر"},
	"label":               {"Name", "الاسم", "ناو"},
	"display_name":        {"Name", "الاسم", "ناو"},
	"title":               {"Title", "العنوان", "سەردێڕ"},
	"subject":             {"Subject", "الموضوع", "بابەت"},
	"body":                {"Message", "النص", "دەق"},
	"description":         {"Description", "الوصف", "وەسف"},
	"note":                {"Note", "ملاحظة", "تێبینی"},
	"comment":             {"Comment", "التعليق", "لێدوان"},
	"reason":              {"Reason", "السبب", "هۆکار"},
	"value":               {"Value", "القيمة", "بەها"},
	"phone":               {"Phone", "الهاتف", "تەلەفۆن"},
	"email":               {"Email", "البريد", "ئیمەیڵ"},
	"gender":              {"Gender", "الجنس", "ڕەگەز"},
	"date_of_birth":       {"Date of birth", "تاريخ الميلاد", "بەرواری لەدایکبوون"},
	"nationality":         {"Nationality", "الجنسية", "نەتەوە"},
	"address":             {"Address", "العنوان", "ناونیشان"},
	"details":             {"Details", "التفاصيل", "وردەکاری"},
	"pickup_address":      {"From", "من", "لە"},
	"dropoff_address":     {"To", "إلى", "بۆ"},
	"pickup_details":      {"Pickup details", "تفاصيل الانطلاق", "وردەکاری سواربوون"},
	"pickup_note":         {"Note for the captain", "ملاحظة للكابتن", "تێبینی بۆ کاپتن"},
	"stops":               {"Stops", "المحطات", "وەستانەکان"},
	"vehicle_class":       {"Class", "الفئة", "پۆل"},
	"payment_method":      {"Payment", "طريقة الدفع", "شێوازی پارەدان"},
	"quoted_fare":         {"Fare", "الأجرة", "کرێ"},
	"amount":              {"Amount", "المبلغ", "بڕ"},
	"balance":             {"Balance", "الرصيد", "باڵانس"},
	"balance_after":       {"Balance after", "الرصيد بعدها", "باڵانس دوای"},
	"currency_code":       {"Currency", "العملة", "دراو"},
	"direction":           {"Direction", "الاتجاه", "ئاراستە"},
	"stars":               {"Stars", "النجوم", "ئەستێرە"},
	"passenger_name":      {"Passenger", "الراكب", "سەرنشین"},
	"passenger_phone":     {"Passenger phone", "هاتف الراكب", "تەلەفۆنی سەرنشین"},
	"scheduled":           {"Booked ahead", "محجوزة مسبقاً", "پێشوەخت"},
	"scheduled_at":        {"Booked for", "موعد الحجز", "کاتی گەشتی پێشوەخت"},
	"cancelled_by":        {"Cancelled by", "ألغاها", "هەڵیوەشاندەوە"},
	"cancellation_reason": {"Why cancelled", "سبب الإلغاء", "هۆکاری هەڵوەشاندنەوە"},
	"rider_no_show":       {"Rider did not come", "لم يحضر الراكب", "سەرنشین نەهات"},
	"requested_at":        {"Requested", "وقت الطلب", "کاتی داواکردن"},
	"accepted_at":         {"Accepted", "وقت القبول", "کاتی وەرگرتن"},
	"arrived_at":          {"Captain arrived", "وصول الكابتن", "گەیشتنی کاپتن"},
	"started_at":          {"Started", "البداية", "دەستپێک"},
	"completed_at":        {"Finished", "النهاية", "کۆتایی"},
	"cancelled_at":        {"Cancelled", "وقت الإلغاء", "کاتی هەڵوەشاندنەوە"},
	"created_at":          {"Created", "التاريخ", "بەروار"},
	"updated_at":          {"Updated", "آخر تعديل", "دوایین گۆڕانکاری"},
	"read_at":             {"Read", "قُرئ", "خوێندرایەوە"},
	"expires_at":          {"Expires", "ينتهي", "بەسەردەچێت"},
	"expires_on":          {"Expires", "ينتهي", "بەسەردەچێت"},
	"last_seen_at":        {"Last seen", "آخر ظهور", "دوایین بینین"},
	"platform":            {"Platform", "النظام", "سیستەم"},
	"locale":              {"Language", "اللغة", "زمان"},
	"device_name":         {"Device", "الجهاز", "ئامێر"},
	"ip_address":          {"Address (IP)", "عنوان IP", "ناونیشانی IP"},
	"make":                {"Make", "الشركة", "کۆمپانیا"},
	"model":               {"Model", "الطراز", "مۆدێل"},
	"color":               {"Colour", "اللون", "ڕەنگ"},
	"year":                {"Year", "السنة", "ساڵ"},
	"plate_number":        {"Plate", "رقم اللوحة", "ژمارەی پلێت"},
	"document_number":     {"Document number", "رقم الوثيقة", "ژمارەی بەڵگەنامە"},
	"messages":            {"Messages", "الرسائل", "نامەکان"},
	"trip_id":             {"Trip", "الرحلة", "گەشت"},
	"blocked":             {"Blocked", "موقوفة", "ڕاگیراو"},
	"active":              {"Active", "فعّال", "چالاک"},
	"rating":              {"Rating", "التقييم", "هەڵسەنگاندن"},
}

func languageIndex(locale string) int {
	switch locale {
	case "ar":
		return 1
	case "ku":
		return 2
	default:
		return 0
	}
}

// enumFields are the fields whose values are words of the system, shown through
// valueLabels when one is known.
var enumFields = map[string]bool{
	"status": true, "payment_method": true, "vehicle_class": true, "cancelled_by": true,
	"direction": true, "platform": true, "type": true, "kind": true,
}

// valueLabels: en, ar, ku.
var valueLabels = map[string][3]string{
	"requested":   {"Requested", "مطلوبة", "داواکراو"},
	"accepted":    {"Accepted", "مقبولة", "وەرگیراو"},
	"in_progress": {"In progress", "جارية", "لە ڕێگادا"},
	"completed":   {"Completed", "مكتملة", "تەواوبوو"},
	"cancelled":   {"Cancelled", "ملغاة", "هەڵوەشاوە"},
	"pending":     {"Pending", "قيد الانتظار", "چاوەڕوان"},
	"approved":    {"Approved", "مقبول", "پەسەندکراو"},
	"rejected":    {"Rejected", "مرفوض", "ڕەتکراوە"},
	"active":      {"Active", "فعّال", "چالاک"},
	"expired":     {"Expired", "منتهي", "بەسەرچوو"},
	"open":        {"Open", "مفتوحة", "کراوە"},
	"resolved":    {"Resolved", "محلولة", "چارەسەرکراو"},
	"closed":      {"Closed", "مغلقة", "داخراو"},
	"paid":        {"Paid", "مدفوع", "دراوە"},
	"declined":    {"Declined", "مرفوض", "ڕەتکراوە"},
	"cash":        {"Cash", "نقداً", "کاش"},
	"wallet":      {"Wallet", "المحفظة", "جزدان"},
	"economy":     {"Economy", "اقتصادي", "ئابووری"},
	"comfort":     {"Comfort", "مريح", "ئاسوودە"},
	"rider":       {"Rider", "الراكب", "سەرنشین"},
	"driver":      {"Captain", "الكابتن", "کاپتن"},
	"system":      {"System", "النظام", "سیستەم"},
	"credit":      {"In", "إضافة", "زیادکردن"},
	"debit":       {"Out", "خصم", "کەمکردنەوە"},
	"android":     {"Android", "أندرويد", "ئەندرۆید"},
	"ios":         {"iPhone", "آيفون", "ئایفۆن"},
}
