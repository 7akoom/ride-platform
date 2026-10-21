package config

import "testing"

func TestTheDefaultLocaleIsOneTheTemplatesAreWrittenIn(t *testing.T) {
	for given, want := range map[string]string{"": "en", "en": "en", " AR ": "ar", "ku": "ku"} {
		got, err := ParseDefaultLocale(Config{DefaultLocale: given})
		if err != nil || got != want {
			t.Errorf("%q: %q, %v", given, got, err)
		}
	}

	for _, bad := range []string{"fr", "ar-IQ", "arabic"} {
		if _, err := ParseDefaultLocale(Config{DefaultLocale: bad}); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
