package config

import (
	"fmt"
	"net/netip"
	"strings"
)

// ParseTrustedProxies reads TRUSTED_PROXY_CIDRS: a comma-separated list of the
// networks (or single addresses) of the reverse proxies and the API gateway that
// sit between the internet and this service.
//
// Behind them every connection this service accepts comes from the gateway, so
// the address it sees is not the user's. When (and only when) the connection comes
// from a trusted proxy, the gRPC layer takes the user's address, user agent and
// language from the headers the gateway forwards. An empty list trusts nobody:
// those headers are ignored and the connection's own address is used.
//
// A prefix that covers every address is refused: it would make the forwarded
// address something any caller can forge, which defeats the per-source OTP limit.
func ParseTrustedProxies(cfg Config) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix

	for _, entry := range strings.Split(cfg.TrustedProxyCIDRs, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		prefix, err := parseTrustedProxy(entry)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS has an invalid entry %q: %w", entry, err)
		}

		if prefix.Bits() == 0 {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS entry %q would trust every address", entry)
		}

		prefixes = append(prefixes, prefix)
	}

	return prefixes, nil
}

func parseTrustedProxy(entry string) (netip.Prefix, error) {
	if strings.Contains(entry, "/") {
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			return netip.Prefix{}, err
		}

		return prefix.Masked(), nil
	}

	addr, err := netip.ParseAddr(entry)
	if err != nil {
		return netip.Prefix{}, err
	}

	addr = addr.Unmap().WithZone("")

	return netip.PrefixFrom(addr, addr.BitLen()), nil
}
