package profile

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Gender values stored; empty means not given.
const (
	GenderMale   = "male"
	GenderFemale = "female"
)

// MinimumAge is how old someone must be, by the date of birth they give.
const MinimumAge = 18

const maximumAge = 120

var (
	ErrInvalidGender      = errors.New("gender is male or female")
	ErrInvalidDateOfBirth = errors.New("date_of_birth is YYYY-MM-DD, 18 to 120 years ago")
	ErrInvalidNationality = errors.New("nationality is an ISO 3166-1 alpha-2 country code")
)

// Fields are the optional personal details a person gives about themselves.
type Fields struct {
	Gender      string
	DateOfBirth string
	Nationality string
}

// Patch is what an update sends: nil leaves a field as it is, "" clears it.
type Patch struct {
	Gender      *string
	DateOfBirth *string
	Nationality *string
}

// Apply validates the patch and returns the fields after it.
func (p Patch) Apply(current Fields, today time.Time) (Fields, error) {
	next := current

	if p.Gender != nil {
		g := strings.ToLower(strings.TrimSpace(*p.Gender))
		if g != "" && g != GenderMale && g != GenderFemale {
			return Fields{}, ErrInvalidGender
		}

		next.Gender = g
	}

	if p.DateOfBirth != nil {
		d := strings.TrimSpace(*p.DateOfBirth)
		if d != "" {
			if err := validDateOfBirth(d, today); err != nil {
				return Fields{}, err
			}
		}

		next.DateOfBirth = d
	}

	if p.Nationality != nil {
		n := strings.ToUpper(strings.TrimSpace(*p.Nationality))
		if n != "" {
			if _, ok := countries[n]; !ok {
				return Fields{}, ErrInvalidNationality
			}
		}

		next.Nationality = n
	}

	return next, nil
}

func validDateOfBirth(value string, today time.Time) error {
	born, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return ErrInvalidDateOfBirth
	}

	y, m, d := today.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)

	if born.After(day.AddDate(-MinimumAge, 0, 0)) || born.Before(day.AddDate(-maximumAge, 0, 0)) {
		return fmt.Errorf("%w (at least %d years old)", ErrInvalidDateOfBirth, MinimumAge)
	}

	return nil
}

// countries are the ISO 3166-1 alpha-2 codes.
var countries = map[string]struct{}{
	"AD": {}, "AE": {}, "AF": {}, "AG": {}, "AI": {}, "AL": {}, "AM": {}, "AO": {}, "AQ": {}, "AR": {}, "AS": {}, "AT": {}, "AU": {}, "AW": {}, "AX": {}, "AZ": {},
	"BA": {}, "BB": {}, "BD": {}, "BE": {}, "BF": {}, "BG": {}, "BH": {}, "BI": {}, "BJ": {}, "BL": {}, "BM": {}, "BN": {}, "BO": {}, "BQ": {}, "BR": {}, "BS": {},
	"BT": {}, "BV": {}, "BW": {}, "BY": {}, "BZ": {}, "CA": {}, "CC": {}, "CD": {}, "CF": {}, "CG": {}, "CH": {}, "CI": {}, "CK": {}, "CL": {}, "CM": {}, "CN": {},
	"CO": {}, "CR": {}, "CU": {}, "CV": {}, "CW": {}, "CX": {}, "CY": {}, "CZ": {}, "DE": {}, "DJ": {}, "DK": {}, "DM": {}, "DO": {}, "DZ": {}, "EC": {}, "EE": {},
	"EG": {}, "EH": {}, "ER": {}, "ES": {}, "ET": {}, "FI": {}, "FJ": {}, "FK": {}, "FM": {}, "FO": {}, "FR": {}, "GA": {}, "GB": {}, "GD": {}, "GE": {}, "GF": {},
	"GG": {}, "GH": {}, "GI": {}, "GL": {}, "GM": {}, "GN": {}, "GP": {}, "GQ": {}, "GR": {}, "GS": {}, "GT": {}, "GU": {}, "GW": {}, "GY": {}, "HK": {}, "HM": {},
	"HN": {}, "HR": {}, "HT": {}, "HU": {}, "ID": {}, "IE": {}, "IL": {}, "IM": {}, "IN": {}, "IO": {}, "IQ": {}, "IR": {}, "IS": {}, "IT": {}, "JE": {}, "JM": {},
	"JO": {}, "JP": {}, "KE": {}, "KG": {}, "KH": {}, "KI": {}, "KM": {}, "KN": {}, "KP": {}, "KR": {}, "KW": {}, "KY": {}, "KZ": {}, "LA": {}, "LB": {}, "LC": {},
	"LI": {}, "LK": {}, "LR": {}, "LS": {}, "LT": {}, "LU": {}, "LV": {}, "LY": {}, "MA": {}, "MC": {}, "MD": {}, "ME": {}, "MF": {}, "MG": {}, "MH": {}, "MK": {},
	"ML": {}, "MM": {}, "MN": {}, "MO": {}, "MP": {}, "MQ": {}, "MR": {}, "MS": {}, "MT": {}, "MU": {}, "MV": {}, "MW": {}, "MX": {}, "MY": {}, "MZ": {}, "NA": {},
	"NC": {}, "NE": {}, "NF": {}, "NG": {}, "NI": {}, "NL": {}, "NO": {}, "NP": {}, "NR": {}, "NU": {}, "NZ": {}, "OM": {}, "PA": {}, "PE": {}, "PF": {}, "PG": {},
	"PH": {}, "PK": {}, "PL": {}, "PM": {}, "PN": {}, "PR": {}, "PS": {}, "PT": {}, "PW": {}, "PY": {}, "QA": {}, "RE": {}, "RO": {}, "RS": {}, "RU": {}, "RW": {},
	"SA": {}, "SB": {}, "SC": {}, "SD": {}, "SE": {}, "SG": {}, "SH": {}, "SI": {}, "SJ": {}, "SK": {}, "SL": {}, "SM": {}, "SN": {}, "SO": {}, "SR": {}, "SS": {},
	"ST": {}, "SV": {}, "SX": {}, "SY": {}, "SZ": {}, "TC": {}, "TD": {}, "TF": {}, "TG": {}, "TH": {}, "TJ": {}, "TK": {}, "TL": {}, "TM": {}, "TN": {}, "TO": {},
	"TR": {}, "TT": {}, "TV": {}, "TW": {}, "TZ": {}, "UA": {}, "UG": {}, "UM": {}, "US": {}, "UY": {}, "UZ": {}, "VA": {}, "VC": {}, "VE": {}, "VG": {}, "VI": {},
	"VN": {}, "VU": {}, "WF": {}, "WS": {}, "YE": {}, "YT": {}, "ZA": {}, "ZM": {}, "ZW": {},
}
