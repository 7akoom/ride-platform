package notification

import (
	"context"
	"reflect"
	"testing"
)

func TestTheInstanceLanguageComesBeforeEnglish(t *testing.T) {
	cases := map[string]struct {
		requested string
		devices   []Device
		instance  string
		want      []string
	}{
		"nothing known: the instance's language, then English": {"", nil, "ar", []string{"ar", "en"}},
		"a device's language first":                            {"", []Device{{Locale: "ku"}}, "ar", []string{"ku", "ar", "en"}},
		"an asked language first":                              {"en", []Device{{Locale: "ku"}}, "ar", []string{"en", "ku", "ar"}},
		"an English instance":                                  {"", nil, "en", []string{"en"}},
	}

	for name, c := range cases {
		if got := localeChain(c.requested, c.devices, c.instance); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

func TestWithDefaultLocale(t *testing.T) {
	if s := NewService(&translations{}, noPush{}, noSMS{}).(*service); s.defaultLocale != DefaultLocale {
		t.Fatalf("without the option: %q", s.defaultLocale)
	}

	if s := NewService(&translations{}, noPush{}, noSMS{}, WithDefaultLocale(" ar ")).(*service); s.defaultLocale != "ar" {
		t.Fatalf("with ar: %q", s.defaultLocale)
	}

	if s := NewService(&translations{}, noPush{}, noSMS{}, WithDefaultLocale("")).(*service); s.defaultLocale != DefaultLocale {
		t.Fatalf("an empty one keeps the default: %q", s.defaultLocale)
	}
}

type noPush struct{}

func (noPush) Send(context.Context, []Device, string, string, map[string]string) (PushResult, error) {
	return PushResult{}, nil
}

type noSMS struct{}

func (noSMS) Send(context.Context, RecipientType, string, string) error { return nil }
