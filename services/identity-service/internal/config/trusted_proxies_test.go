package config

import (
	"net/netip"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		want    []string
		wantErr bool
	}{
		{"empty trusts nobody", "", nil, false},
		{"only blanks and commas", " , ,, ", nil, false},
		{"one network", "172.16.0.0/12", []string{"172.16.0.0/12"}, false},
		{"several, with spaces", " 10.0.0.0/8 , 192.168.0.0/16,127.0.0.0/8", []string{"10.0.0.0/8", "192.168.0.0/16", "127.0.0.0/8"}, false},
		{"a single address becomes a /32", "203.0.113.7", []string{"203.0.113.7/32"}, false},
		{"a single IPv6 address becomes a /128", "::1", []string{"::1/128"}, false},
		{"an IPv4-mapped address is unmapped", "::ffff:10.1.2.3", []string{"10.1.2.3/32"}, false},
		{"host bits are masked off", "172.18.5.9/16", []string{"172.18.0.0/16"}, false},
		{"garbage", "not-an-address", nil, true},
		{"garbage among good entries", "10.0.0.0/8,oops", nil, true},
		{"a bad prefix length", "10.0.0.0/33", nil, true},
		{"every IPv4 address", "0.0.0.0/0", nil, true},
		{"every IPv6 address", "::/0", nil, true},
	}

	for _, tc := range cases {
		got, err := ParseTrustedProxies(Config{TrustedProxyCIDRs: tc.value})

		if tc.wantErr {
			if err == nil {
				t.Errorf("%s: expected an error, got %v", tc.name, got)
			}

			continue
		}

		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)

			continue
		}

		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)

			continue
		}

		for i := range got {
			if got[i] != netip.MustParsePrefix(tc.want[i]) {
				t.Errorf("%s: entry %d: got %v, want %s", tc.name, i, got[i], tc.want[i])
			}
		}
	}
}
