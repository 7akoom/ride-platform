package config

import (
	"testing"
	"time"
)

func validMapsConfig() Config {
	return Config{
		OSRMBaseURL:      "http://osrm:5000",
		NominatimBaseURL: "http://nominatim:8080",
		MapsTimeout:      "5s",
		MapsCountryCodes: "iq",
	}
}

func TestParseMapsReadsTheDefaults(t *testing.T) {
	got, err := ParseMaps(validMapsConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.OSRMBaseURL != "http://osrm:5000" || got.NominatimBaseURL != "http://nominatim:8080" || got.CountryCodes != "iq" || got.Timeout != 5*time.Second {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestParseMapsCleansWhatItReads(t *testing.T) {
	cfg := validMapsConfig()
	cfg.OSRMBaseURL = "  https://osrm.example.org/  "
	cfg.MapsCountryCodes = " IQ , sy,, TR "

	got, err := ParseMaps(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.OSRMBaseURL != "https://osrm.example.org" || got.CountryCodes != "iq,sy,tr" {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestParseMapsRefusesWhatCannotWork(t *testing.T) {
	cases := map[string]func(*Config){
		"an OSRM URL with no scheme":      func(c *Config) { c.OSRMBaseURL = "osrm:5000" },
		"an OSRM URL that is not http":    func(c *Config) { c.OSRMBaseURL = "ftp://osrm:5000" },
		"an empty OSRM URL":               func(c *Config) { c.OSRMBaseURL = "" },
		"a Nominatim URL with no host":    func(c *Config) { c.NominatimBaseURL = "http://" },
		"an invalid timeout":              func(c *Config) { c.MapsTimeout = "soon" },
		"a timeout of zero":               func(c *Config) { c.MapsTimeout = "0s" },
		"a negative timeout":              func(c *Config) { c.MapsTimeout = "-1s" },
		"a timeout of a minute":           func(c *Config) { c.MapsTimeout = "1m" },
		"no country codes":                func(c *Config) { c.MapsCountryCodes = " , " },
		"a country code of three letters": func(c *Config) { c.MapsCountryCodes = "irq" },
		"a country code with a digit":     func(c *Config) { c.MapsCountryCodes = "i1" },
		"one bad code among good ones":    func(c *Config) { c.MapsCountryCodes = "iq,xx1" },
	}

	for name, mutate := range cases {
		cfg := validMapsConfig()
		mutate(&cfg)

		if got, err := ParseMaps(cfg); err == nil {
			t.Errorf("%s: expected an error, got %+v", name, got)
		}
	}
}
