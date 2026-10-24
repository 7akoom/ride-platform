package notification

import (
	"strings"
)

// moneyVariables are the template variables that hold an amount of money.
var moneyVariables = map[string]bool{"amount": true, "amount_due": true, "balance": true, "total": true}

// currencyNames is how a currency reads in each language; a code not here stays as
// it is ("USD"). One currency per instance, so the list stays short.
var currencyNames = map[string]map[string]string{
	"IQD": {"ar": "د.ع", "ku": "د.ع", "en": "IQD"},
}

// RenderFor is Render with money written for people in [locale]: amounts with
// thousands separators and no ".00" ("2,000" not "2000.00"), in Western digits in
// every language, and the currency by its local name ("د.ع" in Arabic and Kurdish).
// The stored variables stay as they came, so the same notification can be written
// again in another language.
func RenderFor(text string, variables map[string]string, locale string) string {
	if len(variables) == 0 {
		return text
	}

	shown := make(map[string]string, len(variables))

	for key, value := range variables {
		switch {
		case moneyVariables[key]:
			shown[key] = formatAmount(value)
		case key == "currency":
			shown[key] = currencyName(value, locale)
		default:
			shown[key] = value
		}
	}

	return Render(text, shown)
}

// formatAmount turns "2000.00" into "2,000" and "1500.5" into "1,500.50"; anything
// that is not a plain decimal number is left as it is.
func formatAmount(value string) string {
	text := strings.TrimSpace(value)
	sign := ""

	if strings.HasPrefix(text, "-") {
		sign, text = "-", text[1:]
	}

	whole, fraction, _ := strings.Cut(text, ".")

	if whole == "" || !digitsOnly(whole) || !digitsOnly(fraction) {
		return value
	}

	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}

	var grouped strings.Builder

	for i, digit := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			grouped.WriteByte(',')
		}

		grouped.WriteRune(digit)
	}

	fraction = strings.TrimRight(fraction, "0")
	if fraction != "" {
		if len(fraction) == 1 {
			fraction += "0"
		}

		return sign + grouped.String() + "." + fraction
	}

	return sign + grouped.String()
}

func currencyName(code, locale string) string {
	if names, ok := currencyNames[strings.ToUpper(strings.TrimSpace(code))]; ok {
		if name, ok := names[locale]; ok {
			return name
		}
	}

	return code
}

func digitsOnly(text string) bool {
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}
