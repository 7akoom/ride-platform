package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Maps is where the routing engine and the place search are, and how long to wait
// for them.
type Maps struct {
	OSRMBaseURL      string
	NominatimBaseURL string

	// CountryCodes restricts place search to these countries (lower case, comma
	// separated ISO 3166-1 alpha-2 codes, e.g. "iq").
	CountryCodes string

	Timeout time.Duration
}

const maxMapsTimeout = 30 * time.Second

// ParseMaps reads OSRM_BASE_URL, NOMINATIM_BASE_URL, MAPS_COUNTRY_CODES and
// MAPS_HTTP_TIMEOUT.
func ParseMaps(cfg Config) (Maps, error) {
	osrm, err := parseBaseURL("OSRM_BASE_URL", cfg.OSRMBaseURL)
	if err != nil {
		return Maps{}, err
	}

	nominatim, err := parseBaseURL("NOMINATIM_BASE_URL", cfg.NominatimBaseURL)
	if err != nil {
		return Maps{}, err
	}

	timeout, err := time.ParseDuration(strings.TrimSpace(cfg.MapsTimeout))
	if err != nil {
		return Maps{}, fmt.Errorf("MAPS_HTTP_TIMEOUT has invalid duration %q: %w", cfg.MapsTimeout, err)
	}

	if timeout <= 0 || timeout > maxMapsTimeout {
		return Maps{}, fmt.Errorf("MAPS_HTTP_TIMEOUT must be greater than zero and at most %s, got %s", maxMapsTimeout, timeout)
	}

	countries, err := parseCountryCodes(cfg.MapsCountryCodes)
	if err != nil {
		return Maps{}, err
	}

	return Maps{OSRMBaseURL: osrm, NominatimBaseURL: nominatim, CountryCodes: countries, Timeout: timeout}, nil
}

func parseBaseURL(name string, value string) (string, error) {
	value = strings.TrimSpace(value)

	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("%s must be an http(s) URL such as http://osrm:5000, got %q", name, value)
	}

	return strings.TrimRight(value, "/"), nil
}

func parseCountryCodes(value string) (string, error) {
	var codes []string

	for _, code := range strings.Split(value, ",") {
		code = strings.ToLower(strings.TrimSpace(code))
		if code == "" {
			continue
		}

		if len(code) != 2 || code[0] < 'a' || code[0] > 'z' || code[1] < 'a' || code[1] > 'z' {
			return "", fmt.Errorf("MAPS_COUNTRY_CODES has invalid country code %q (expected two letters, e.g. iq)", code)
		}

		codes = append(codes, code)
	}

	if len(codes) == 0 {
		return "", fmt.Errorf("MAPS_COUNTRY_CODES needs at least one country code, e.g. iq")
	}

	return strings.Join(codes, ","), nil
}
