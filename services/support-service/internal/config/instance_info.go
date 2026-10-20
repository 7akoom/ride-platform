package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// InstanceInfo is what the apps show about this copy of the platform: its legal
// pages and the emergency numbers of its country.
type InstanceInfo struct {
	TermsURL   string
	PrivacyURL string
	Emergency  []EmergencyNumber
}

// EmergencyNumber is one number of the safety center; Kind is general, police,
// ambulance or fire.
type EmergencyNumber struct {
	Kind   string
	Number string
}

var dialable = regexp.MustCompile(`^\+?[0-9]{2,15}$`)

// ParseInstanceInfo reads SUPPORT_TERMS_URL and SUPPORT_PRIVACY_URL (https, or
// empty) and SUPPORT_EMERGENCY_NUMBERS ("general=112,police=104,ambulance=122,
// fire=115", or empty), each kind at most once.
func ParseInstanceInfo(cfg Config) (InstanceInfo, error) {
	terms, err := parsePageURL("SUPPORT_TERMS_URL", cfg.TermsURL)
	if err != nil {
		return InstanceInfo{}, err
	}

	privacy, err := parsePageURL("SUPPORT_PRIVACY_URL", cfg.PrivacyURL)
	if err != nil {
		return InstanceInfo{}, err
	}

	info := InstanceInfo{TermsURL: terms, PrivacyURL: privacy}
	seen := map[string]bool{}

	for _, part := range strings.Split(cfg.EmergencyNumbers, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		kind, number, ok := strings.Cut(part, "=")
		kind = strings.TrimSpace(kind)
		number = strings.TrimSpace(number)

		switch kind {
		case "general", "police", "ambulance", "fire":
		default:
			ok = false
		}

		if !ok || seen[kind] || !dialable.MatchString(number) {
			return InstanceInfo{}, fmt.Errorf(
				"SUPPORT_EMERGENCY_NUMBERS is kind=number pairs (general, police, ambulance, fire; each once), got %q", part)
		}

		seen[kind] = true
		info.Emergency = append(info.Emergency, EmergencyNumber{Kind: kind, Number: number})
	}

	return info, nil
}

func parsePageURL(name, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", fmt.Errorf("%s must be an https address, got %q", name, value)
	}

	return value, nil
}
