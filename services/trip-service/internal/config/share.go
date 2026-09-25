package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Share is the deployment's rules for trip sharing links.
type Share struct {
	// URLBase, when set, is what a link's token is appended to
	// ("https://ride.example/t/" gives "https://ride.example/t/<token>").
	URLBase string
	// MaxAge is how long a link lives at most.
	MaxAge time.Duration
	// AfterEnd is how long a link still shows a trip after it ended.
	AfterEnd time.Duration
}

// ParseShare reads the TRIP_SHARE_* settings.
func ParseShare(cfg Config) (Share, error) {
	out := Share{URLBase: strings.TrimSpace(cfg.ShareURLBase)}

	if out.URLBase != "" {
		parsed, err := url.Parse(out.URLBase)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return Share{}, fmt.Errorf("TRIP_SHARE_URL_BASE must be an http(s) URL, got %q", cfg.ShareURLBase)
		}
	}

	var err error

	if out.MaxAge, err = time.ParseDuration(strings.TrimSpace(cfg.ShareMaxAge)); err != nil || out.MaxAge < time.Hour || out.MaxAge > 48*time.Hour {
		return Share{}, fmt.Errorf("TRIP_SHARE_MAX_AGE must be a duration from 1h to 48h, got %q", cfg.ShareMaxAge)
	}

	if out.AfterEnd, err = time.ParseDuration(strings.TrimSpace(cfg.ShareAfterEnd)); err != nil || out.AfterEnd < 0 || out.AfterEnd > 6*time.Hour {
		return Share{}, fmt.Errorf("TRIP_SHARE_AFTER_END must be a duration from 0 to 6h, got %q", cfg.ShareAfterEnd)
	}

	return out, nil
}
