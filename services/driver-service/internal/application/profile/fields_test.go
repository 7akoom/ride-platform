package profile

import (
	"errors"
	"testing"
	"time"
)

func ptr(s string) *string { return &s }

func TestPatchApply(t *testing.T) {
	today := time.Date(2026, 10, 4, 15, 0, 0, 0, time.UTC)
	current := Fields{Gender: "female", DateOfBirth: "1995-01-02", Nationality: "IQ"}

	cases := []struct {
		name  string
		patch Patch
		want  Fields
		err   error
	}{
		{"nothing", Patch{}, current, nil},
		{"normalises", Patch{Gender: ptr(" MALE "), Nationality: ptr(" sy ")}, Fields{"male", "1995-01-02", "SY"}, nil},
		{"clears", Patch{Gender: ptr(""), DateOfBirth: ptr(""), Nationality: ptr("")}, Fields{}, nil},
		{"exactly 18", Patch{DateOfBirth: ptr("2008-10-04")}, Fields{"female", "2008-10-04", "IQ"}, nil},
		{"under 18", Patch{DateOfBirth: ptr("2008-10-05")}, Fields{}, ErrInvalidDateOfBirth},
		{"too old", Patch{DateOfBirth: ptr("1900-01-01")}, Fields{}, ErrInvalidDateOfBirth},
		{"bad date", Patch{DateOfBirth: ptr("04/10/1990")}, Fields{}, ErrInvalidDateOfBirth},
		{"bad gender", Patch{Gender: ptr("other")}, Fields{}, ErrInvalidGender},
		{"bad country", Patch{Nationality: ptr("XX")}, Fields{}, ErrInvalidNationality},
		{"alpha-3", Patch{Nationality: ptr("IRQ")}, Fields{}, ErrInvalidNationality},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.patch.Apply(current, today)
			if c.err != nil {
				if !errors.Is(err, c.err) {
					t.Fatalf("want %v, got %v", c.err, err)
				}

				return
			}

			if err != nil || got != c.want {
				t.Fatalf("got %+v %v, want %+v", got, err, c.want)
			}
		})
	}
}

func TestCountriesAreAlpha2(t *testing.T) {
	if len(countries) != 249 {
		t.Fatalf("want 249 ISO 3166-1 codes, got %d", len(countries))
	}

	for code := range countries {
		if len(code) != 2 || code[0] < 'A' || code[0] > 'Z' || code[1] < 'A' || code[1] > 'Z' {
			t.Fatalf("bad code %q", code)
		}
	}
}
