package config

import (
	"fmt"
	"strings"
)

// ParseDefaultLocale reads NOTIFICATION_DEFAULT_LOCALE: en (the default), ar or ku,
// the languages the templates are written in.
func ParseDefaultLocale(cfg Config) (string, error) {
	locale := strings.ToLower(strings.TrimSpace(cfg.DefaultLocale))

	switch locale {
	case "":
		return "en", nil
	case "en", "ar", "ku":
		return locale, nil
	default:
		return "", fmt.Errorf("NOTIFICATION_DEFAULT_LOCALE must be en, ar or ku, got %q", cfg.DefaultLocale)
	}
}
