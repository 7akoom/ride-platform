package grpc

import (
	"context"
	"net"
	"net/netip"
	"testing"

	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

var clientContextTrusted = []netip.Prefix{
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("127.0.0.0/8"),
}

func TestClientAddrFromForwardedFor(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		want   string // empty: no address found
	}{
		{"the user, then the gateway's own view of its caller", []string{"203.0.113.7, 172.18.0.1"}, "203.0.113.7"},
		{"the user alone", []string{"203.0.113.7"}, "203.0.113.7"},
		{"a forged prefix cannot choose the result", []string{"1.2.3.4, 203.0.113.7, 172.18.0.1"}, "203.0.113.7"},
		{"several trusted hops", []string{"203.0.113.7, 10.0.0.5, 172.18.0.1"}, "203.0.113.7"},
		{"the header split over two values", []string{"203.0.113.7", "172.18.0.1"}, "203.0.113.7"},
		{"an IPv6 user", []string{"2001:db8::7, 172.18.0.1"}, "2001:db8::7"},
		{"an entry with a port", []string{"203.0.113.7:51234, 172.18.0.1"}, "203.0.113.7"},
		{"a bracketed IPv6 entry with a port", []string{"[2001:db8::7]:51234, 172.18.0.1"}, "2001:db8::7"},
		{"an IPv4-mapped address is unmapped", []string{"::ffff:203.0.113.7, 172.18.0.1"}, "203.0.113.7"},
		{"only trusted addresses: nobody to name", []string{"172.18.0.1, 10.0.0.2"}, ""},
		{"an entry that is not an address breaks the chain", []string{"203.0.113.7, unknown, 172.18.0.1"}, ""},
		{"garbage on the right breaks the chain", []string{"203.0.113.7, <script>"}, ""},
		{"empty header", []string{""}, ""},
		{"no header", nil, ""},
	}

	for _, tc := range cases {
		got, ok := clientAddrFromForwardedFor(tc.values, clientContextTrusted)

		if tc.want == "" {
			if ok {
				t.Errorf("%s: expected no address, got %s", tc.name, got)
			}

			continue
		}

		if !ok || got != netip.MustParseAddr(tc.want) {
			t.Errorf("%s: got %s (%v), want %s", tc.name, got, ok, tc.want)
		}
	}
}

// seen is what the wrapped handler observed: the session address and device
// through the same functions the service uses, and the language header as it
// arrives in the metadata.
type clientContextSeen struct {
	ip        string
	userAgent string
	locale    string
}

func runClientContext(t *testing.T, trusted []netip.Prefix, peerAddr string, md metadata.MD) clientContextSeen {
	t.Helper()

	ctx := context.Background()

	if peerAddr != "" {
		ctx = peer.NewContext(ctx, &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP(peerAddr), Port: 50051}})
	}

	if md != nil {
		ctx = metadata.NewIncomingContext(ctx, md)
	}

	var seen clientContextSeen

	_, err := NewClientContextUnaryInterceptor(trusted)(ctx, nil, &googlegrpc.UnaryServerInfo{FullMethod: "/test/Method"}, func(ctx context.Context, _ any) (any, error) {
		seen = clientContextSeen{
			ip:        sessionIPAddressFromContext(ctx),
			userAgent: sessionUserAgentFromContext(ctx),
			locale:    incomingAcceptLanguage(ctx),
		}

		return nil, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return seen
}

func incomingAcceptLanguage(ctx context.Context) string {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ""
	}

	return firstGatewayValue(incoming.Get("accept-language"))
}

func gatewayHeaders() metadata.MD {
	return metadata.Pairs(
		"user-agent", "grpc-go/1.70.0",
		"x-forwarded-for", "203.0.113.7, 172.18.0.1",
		"grpcgateway-user-agent", "RideApp/1.4 (Android 14)",
		"grpcgateway-accept-language", "ar-IQ,ar;q=0.9,en;q=0.5",
	)
}

func TestARequestThroughTheGatewayLooksLikeTheUser(t *testing.T) {
	seen := runClientContext(t, clientContextTrusted, "172.18.0.9", gatewayHeaders())

	if seen.ip != "203.0.113.7" {
		t.Errorf("the source address is %q, expected the user's", seen.ip)
	}

	if seen.userAgent != "RideApp/1.4 (Android 14)" {
		t.Errorf("the user agent is %q, expected the app's", seen.userAgent)
	}

	if seen.locale != "ar-IQ,ar;q=0.9,en;q=0.5" {
		t.Errorf("the language is %q, expected the user's Accept-Language", seen.locale)
	}
}

func TestAConnectionFromAnUntrustedPeerIsTakenAtFaceValue(t *testing.T) {
	// Anyone who can reach the service directly could forge these headers.
	seen := runClientContext(t, clientContextTrusted, "198.51.100.20", gatewayHeaders())

	if seen.ip != "198.51.100.20" {
		t.Errorf("the source address is %q, expected the connection's own", seen.ip)
	}

	if seen.userAgent != "grpc-go/1.70.0" {
		t.Errorf("the user agent is %q, expected the transport's own", seen.userAgent)
	}

	if seen.locale != "" {
		t.Errorf("the language is %q, expected none (the forwarded one is not trusted)", seen.locale)
	}
}

func TestWithNoTrustedProxiesNothingChanges(t *testing.T) {
	seen := runClientContext(t, nil, "172.18.0.9", gatewayHeaders())

	if seen.ip != "172.18.0.9" || seen.userAgent != "grpc-go/1.70.0" || seen.locale != "" {
		t.Errorf("an empty trust list must change nothing, got %+v", seen)
	}
}

func TestAForgedPrefixDoesNotChooseTheAddress(t *testing.T) {
	md := gatewayHeaders()
	md.Set("x-forwarded-for", "1.1.1.1, 203.0.113.7, 172.18.0.1")

	if seen := runClientContext(t, clientContextTrusted, "172.18.0.9", md); seen.ip != "203.0.113.7" {
		t.Errorf("the source address is %q, expected the first untrusted from the right", seen.ip)
	}
}

func TestWithoutAForwardedAddressTheGatewayIsTheSource(t *testing.T) {
	md := metadata.Pairs("grpcgateway-user-agent", "RideApp/1.4", "grpcgateway-accept-language", "ku")

	seen := runClientContext(t, clientContextTrusted, "172.18.0.9", md)

	if seen.ip != "172.18.0.9" {
		t.Errorf("the source address is %q, expected the connection's own", seen.ip)
	}

	if seen.userAgent != "RideApp/1.4" || seen.locale != "ku" {
		t.Errorf("device and language must still come through: %+v", seen)
	}
}

func TestAMissingUserAgentOrLanguageIsNotInvented(t *testing.T) {
	md := metadata.Pairs("user-agent", "grpc-go/1.70.0", "x-forwarded-for", "203.0.113.7, 172.18.0.1")

	seen := runClientContext(t, clientContextTrusted, "172.18.0.9", md)

	if seen.userAgent != "grpc-go/1.70.0" || seen.locale != "" {
		t.Errorf("without gateway headers nothing may be invented: %+v", seen)
	}
}

func TestARequestWithoutAPeerOrMetadataPassesThrough(t *testing.T) {
	seen := runClientContext(t, clientContextTrusted, "", nil)

	if seen.ip != "" || seen.userAgent != "" || seen.locale != "" {
		t.Errorf("nothing to rewrite, got %+v", seen)
	}
}

func TestTheInterceptorDoesNotShareItsTrustListWithTheCaller(t *testing.T) {
	trusted := append([]netip.Prefix(nil), clientContextTrusted...)
	interceptor := NewClientContextUnaryInterceptor(trusted)

	// Emptying the caller's slice afterwards must not switch the check off.
	for i := range trusted {
		trusted[i] = netip.Prefix{}
	}

	ctx := peer.NewContext(context.Background(), &peer.Peer{Addr: &net.TCPAddr{IP: net.ParseIP("172.18.0.9"), Port: 1}})
	ctx = metadata.NewIncomingContext(ctx, gatewayHeaders())

	var ip string

	_, _ = interceptor(ctx, nil, &googlegrpc.UnaryServerInfo{}, func(ctx context.Context, _ any) (any, error) {
		ip = sessionIPAddressFromContext(ctx)

		return nil, nil
	})

	if ip != "203.0.113.7" {
		t.Errorf("the interceptor's trust list changed with the caller's slice: %q", ip)
	}
}
