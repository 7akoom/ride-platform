package grpc

import (
	"context"
	"strconv"
	"strings"

	"google.golang.org/grpc/metadata"
)

const defaultRequestLocale = "en"

func requestLocaleFromIncomingContext(
	ctx context.Context,
) string {
	incomingMetadata, ok :=
		metadata.FromIncomingContext(ctx)
	if !ok {
		return defaultRequestLocale
	}

	values := incomingMetadata.Get(
		"accept-language",
	)
	if len(values) == 0 {
		return defaultRequestLocale
	}

	bestLocale := ""
	bestQuality := -1.0

	for _, value := range values {
		for _, candidate := range strings.Split(
			value,
			",",
		) {
			locale, quality, ok :=
				parseRequestLocaleCandidate(
					candidate,
				)
			if !ok {
				continue
			}

			if quality > bestQuality {
				bestLocale = locale
				bestQuality = quality
			}
		}
	}

	if bestLocale == "" {
		return defaultRequestLocale
	}

	return bestLocale
}

func parseRequestLocaleCandidate(
	value string,
) (string, float64, bool) {
	parts := strings.Split(
		value,
		";",
	)

	tag := strings.ToLower(
		strings.TrimSpace(parts[0]),
	)

	tag = strings.ReplaceAll(
		tag,
		"_",
		"-",
	)

	locale := supportedRequestLocale(
		tag,
	)
	if locale == "" {
		return "", 0, false
	}

	quality := 1.0

	for _, parameter := range parts[1:] {
		keyValue := strings.SplitN(
			strings.TrimSpace(parameter),
			"=",
			2,
		)

		if len(keyValue) != 2 ||
			!strings.EqualFold(
				strings.TrimSpace(keyValue[0]),
				"q",
			) {
			continue
		}

		parsedQuality, err := strconv.ParseFloat(
			strings.TrimSpace(keyValue[1]),
			64,
		)
		if err != nil ||
			parsedQuality <= 0 ||
			parsedQuality > 1 {
			return "", 0, false
		}

		quality = parsedQuality
	}

	return locale, quality, true
}

func supportedRequestLocale(
	tag string,
) string {
	switch {
	case tag == "ar" ||
		strings.HasPrefix(tag, "ar-"):
		return "ar"

	case tag == "ku" ||
		strings.HasPrefix(tag, "ku-"):
		return "ku"

	case tag == "en" ||
		strings.HasPrefix(tag, "en-"):
		return "en"

	default:
		return ""
	}
}
