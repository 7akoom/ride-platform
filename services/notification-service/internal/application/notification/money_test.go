package notification

import "testing"

func TestAmountsAreWrittenForPeople(t *testing.T) {
	for in, want := range map[string]string{
		"2000.00":    "2,000",
		"2000":       "2,000",
		"1500.5":     "1,500.50",
		"1234567.25": "1,234,567.25",
		"0.00":       "0",
		"-3000.00":   "-3,000",
		"999":        "999",
		"abc":        "abc",
		"":           "",
	} {
		if got := formatAmount(in); got != want {
			t.Errorf("formatAmount(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMoneyInATemplateReadsInTheLanguage(t *testing.T) {
	vars := map[string]string{"amount": "2000.00", "currency": "IQD", "trip_id": "1234"}

	if got := RenderFor("أُضيف {amount} {currency} ({trip_id})", vars, "ar"); got != "أُضيف 2,000 د.ع (1234)" {
		t.Errorf("ar: %q", got)
	}

	if got := RenderFor("{amount} {currency}", vars, "en"); got != "2,000 IQD" {
		t.Errorf("en: %q", got)
	}

	if got := RenderFor("{total} {currency}", map[string]string{"total": "500.00", "currency": "USD"}, "ar"); got != "500 USD" {
		t.Errorf("other currency: %q", got)
	}

	// What is stored is left as it came.
	if vars["amount"] != "2000.00" {
		t.Errorf("variables changed: %v", vars)
	}
}
