package dataexport

import (
	"strings"
	"testing"
	"time"
)

func TestTheReadablePageIsInTheLanguageAskedWithTheFieldsInOrder(t *testing.T) {
	baghdad, err := time.LoadLocation("Asia/Baghdad")
	if err != nil {
		t.Fatal(err)
	}

	export := Export{Locale: "ar", RequestedAt: time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC)}
	sections := []Section{
		{Name: "trips/as_rider.json", Content: []byte(`[
			{"status": "completed", "pickup_address": "القلعة <b>", "requested_at": "2026-10-20T17:54:01.5+00:00", "scheduled": false, "stops": [{"address": "x"}], "quoted_fare": 4500},
			{"status": "cancelled", "pickup_address": "المطار", "extra_field": null}
		]`)},
		{Name: "wallet/rider_wallet.json", Content: []byte(`{"balance": 12000, "currency_code": "IQD"}`)},
		{Name: "rider/saved_addresses.json", Content: []byte(`[]`)},
	}

	got, err := BuildReadable(export, sections, baghdad)
	if err != nil {
		t.Fatal(err)
	}

	html := string(got)

	for _, want := range []string{
		`lang="ar" dir="rtl"`,
		"<title>بياناتك</title>",
		"رحلاتك", "المحفظة", "العناوين المحفوظة",
		"<th>الحالة</th><th>من</th><th>وقت الطلب</th><th>محجوزة مسبقاً</th><th>المحطات</th><th>الأجرة</th><th>extra field</th>",
		"2026-10-20 20:54", // 17:54 UTC in Baghdad
		"<bdi>لا</bdi>",
		"القلعة &lt;b&gt;", // escaped
		`{&#34;address&#34;: &#34;x&#34;}`,
		"<bdi>4500</bdi>",
		"<th>الرصيد</th><td><bdi>12000</bdi></td>",
		"لا توجد بيانات محفوظة.",
		"<bdi>مكتملة</bdi>", "<bdi>ملغاة</bdi>",
		"الأوقات بتوقيت \u2066Asia/Baghdad\u2069.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the page lacks %q", want)
		}
	}

	if strings.Contains(html, "<b>") {
		t.Error("a value went into the page unescaped")
	}
}

func TestAnyOtherLanguageIsEnglishAndOddSectionsAreShownAsText(t *testing.T) {
	got, err := BuildReadable(Export{Locale: "fr"}, []Section{{Name: "x/odd.json", Content: []byte("not json")}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{`lang="en" dir="ltr"`, "Your data", "x/odd.json", "<bdi>not json</bdi>", "Times are in \u2066UTC\u2069."} {
		if !strings.Contains(string(got), want) {
			t.Errorf("the page lacks %q", want)
		}
	}
}

func TestEverySectionTitleHasThreeLanguages(t *testing.T) {
	for name, titles := range sectionTitles {
		for i, title := range titles {
			if strings.TrimSpace(title) == "" {
				t.Errorf("%s: no title in language %d", name, i)
			}
		}
	}
}
