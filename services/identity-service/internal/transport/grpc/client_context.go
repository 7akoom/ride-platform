package grpc

import (
	"context"
	"net"
	"net/netip"
	"strings"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

// Metadata keys the API gateway (grpc-gateway) adds to a request it proxies.
const (
	forwardedForKey          = "x-forwarded-for"
	gatewayUserAgentKey      = "grpcgateway-user-agent"
	gatewayAcceptLanguageKey = "grpcgateway-accept-language"
)

// NewClientContextUnaryInterceptor makes a request that came through the API
// gateway look, to the rest of this service, like it came from the user.
//
// Behind the gateway the connection's address is the gateway's, gRPC's own
// user-agent is "grpc-go/...", and the language header arrives under a
// "grpcgateway-" prefix. Without this, every user shares one source address (so
// the per-source OTP limit either locks everyone out together or protects
// nobody), sessions list "grpc-go" as the device, and OTP messages ignore the
// user's language.
//
// It only acts when the connection itself comes from a trusted proxy: anything
// else could forge these headers. When it does, it
//   - replaces the peer address with the user's address, taken from
//     X-Forwarded-For by walking it from the right and skipping trusted proxies,
//     so a client-supplied prefix cannot choose the result;
//   - sets user-agent from the user's User-Agent header;
//   - sets accept-language from the user's Accept-Language header.
//
// Existing code reads exactly those three things (peer address, "user-agent",
// "accept-language"), so nothing else has to change. With no trusted proxies
// configured it does nothing.
func NewClientContextUnaryInterceptor(trustedProxies []netip.Prefix) googlegrpc.UnaryServerInterceptor {
	trusted := append([]netip.Prefix(nil), trustedProxies...)

	return func(
		ctx context.Context,
		request any,
		info *googlegrpc.UnaryServerInfo,
		handler googlegrpc.UnaryHandler,
	) (any, error) {
		if len(trusted) == 0 {
			return handler(ctx, request)
		}

		peerInfo, ok := peer.FromContext(ctx)
		if !ok || peerInfo == nil || peerInfo.Addr == nil {
			return handler(ctx, request)
		}

		direct, ok := addrOfPeer(peerInfo.Addr)
		if !ok || !isTrustedProxy(trusted, direct) {
			return handler(ctx, request)
		}

		incoming, _ := metadata.FromIncomingContext(ctx)
		updated := incoming.Copy()
		metadataChanged := false

		if client, found := clientAddrFromForwardedFor(incoming.Get(forwardedForKey), trusted); found {
			replaced := *peerInfo
			replaced.Addr = &net.TCPAddr{IP: net.IP(client.AsSlice())}
			ctx = peer.NewContext(ctx, &replaced)
		}

		if userAgent := firstGatewayValue(incoming.Get(gatewayUserAgentKey)); userAgent != "" {
			updated.Set("user-agent", userAgent)

			metadataChanged = true
		}

		if language := firstGatewayValue(incoming.Get(gatewayAcceptLanguageKey)); language != "" {
			updated.Set("accept-language", language)

			metadataChanged = true
		}

		if metadataChanged {
			ctx = metadata.NewIncomingContext(ctx, updated)
		}

		return handler(ctx, request)
	}
}

// clientAddrFromForwardedFor picks the user's address out of X-Forwarded-For.
// Every proxy appends the address it saw the request come from, so the rightmost
// entries are added by our own infrastructure and the first one that is not a
// trusted proxy is the user. Anything to its left is whatever the user (or an
// earlier hop) claimed, and is ignored. If the chain is broken by an entry that
// is not an address, nothing is returned rather than guessing.
func clientAddrFromForwardedFor(values []string, trusted []netip.Prefix) (netip.Addr, bool) {
	var entries []string

	for _, value := range values {
		for _, entry := range strings.Split(value, ",") {
			if entry = strings.TrimSpace(entry); entry != "" {
				entries = append(entries, entry)
			}
		}
	}

	for i := len(entries) - 1; i >= 0; i-- {
		addr, ok := parseForwardedEntry(entries[i])
		if !ok {
			return netip.Addr{}, false
		}

		if isTrustedProxy(trusted, addr) {
			continue
		}

		return addr, true
	}

	return netip.Addr{}, false
}

// parseForwardedEntry reads "203.0.113.7", "203.0.113.7:51234", "2001:db8::1" or
// "[2001:db8::1]:51234".
func parseForwardedEntry(entry string) (netip.Addr, bool) {
	if addr, err := netip.ParseAddr(entry); err == nil {
		return addr.Unmap().WithZone(""), true
	}

	if addrPort, err := netip.ParseAddrPort(entry); err == nil {
		return addrPort.Addr().Unmap().WithZone(""), true
	}

	return netip.Addr{}, false
}

func addrOfPeer(addr net.Addr) (netip.Addr, bool) {
	address := strings.TrimSpace(addr.String())

	host := address
	if splitHost, _, err := net.SplitHostPort(address); err == nil {
		host = splitHost
	}

	parsed, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}

	return parsed.Unmap().WithZone(""), true
}

func isTrustedProxy(trusted []netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range trusted {
		if prefix.Contains(addr) {
			return true
		}
	}

	return false
}

func firstGatewayValue(values []string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}

	return ""
}
