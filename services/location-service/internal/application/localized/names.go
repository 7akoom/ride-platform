// Package localized holds what cities and curated places share: a default
// name, and the same name in the languages the apps speak.
package localized

import (
	"errors"
	"strings"
	"unicode/utf8"
)

// Languages the apps are written in.
var Languages = []string{"ar", "ku", "en"}

const MaxNameLength = 120

var (
	ErrNameRequired    = errors.New("name is required")
	ErrNameTooLong     = errors.New("a name is longer than 120 characters")
	ErrUnknownLanguage = errors.New("names may only be given in ar, ku or en")
)

// Name validates a default name: trimmed, not empty, not too long.
func Name(raw string) (string, error) {
	name := strings.TrimSpace(raw)

	switch {
	case name == "":
		return "", ErrNameRequired
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", ErrNameTooLong
	}

	return name, nil
}

// Names validates translations: known languages only, each trimmed and not too
// long. Empty translations are dropped.
func Names(raw map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(raw))

	for language, value := range raw {
		language = strings.ToLower(strings.TrimSpace(language))
		if !known(language) {
			return nil, ErrUnknownLanguage
		}

		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		if utf8.RuneCountInString(value) > MaxNameLength {
			return nil, ErrNameTooLong
		}

		out[language] = value
	}

	return out, nil
}

// Pick returns the name in the first of the preferred languages that has one,
// or the default name. preferred is ordered, for example ["ku", "ar", "en"].
func Pick(fallback string, names map[string]string, preferred []string) string {
	for _, language := range preferred {
		if name, ok := names[language]; ok && name != "" {
			return name
		}
	}

	return fallback
}

func known(language string) bool {
	for _, l := range Languages {
		if l == language {
			return true
		}
	}

	return false
}
