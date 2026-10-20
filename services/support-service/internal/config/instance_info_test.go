package config

import (
	"reflect"
	"testing"
)

func TestParseInstanceInfo(t *testing.T) {
	info, err := ParseInstanceInfo(Config{
		TermsURL:         " https://taxi.example.com/terms ",
		EmergencyNumbers: "general=112, police=104,ambulance=122,fire=+9647700",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := InstanceInfo{
		TermsURL: "https://taxi.example.com/terms",
		Emergency: []EmergencyNumber{
			{Kind: "general", Number: "112"},
			{Kind: "police", Number: "104"},
			{Kind: "ambulance", Number: "122"},
			{Kind: "fire", Number: "+9647700"},
		},
	}
	if !reflect.DeepEqual(info, want) {
		t.Fatalf("got %+v, want %+v", info, want)
	}

	if empty, err := ParseInstanceInfo(Config{}); err != nil || !reflect.DeepEqual(empty, InstanceInfo{}) {
		t.Fatalf("nothing set: %+v, %v", empty, err)
	}
}

func TestParseInstanceInfoRefusesBadValues(t *testing.T) {
	for name, cfg := range map[string]Config{
		"http terms":   {TermsURL: "http://taxi.example.com/terms"},
		"no host":      {PrivacyURL: "https://"},
		"unknown kind": {EmergencyNumbers: "coastguard=199"},
		"twice":        {EmergencyNumbers: "police=104,police=105"},
		"letters":      {EmergencyNumbers: "police=10A"},
		"no number":    {EmergencyNumbers: "police"},
		"too short":    {EmergencyNumbers: "police=1"},
	} {
		if _, err := ParseInstanceInfo(cfg); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
