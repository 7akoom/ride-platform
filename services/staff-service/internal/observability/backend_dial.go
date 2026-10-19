package observability

import (
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/resolver"
)

// Every client of another service (and the gateway) dials with these two options
// after GRPCClientOption, so a service that stops and comes back (a restart, a new
// container, a server reboot) is reached again within seconds.
//
// Without them grpc.NewClient keeps its own DNS resolver for the name: while the
// container is gone the lookup fails, and the resolver waits longer after each
// failure, up to two minutes; reconnects back off the same way. After a long outage
// every request failed at once with "unavailable" for minutes after the service
// was back (seen on 19 Oct 2026: the gateway answered 503 until it was restarted).

// LookUpOnEveryConnect makes each connection attempt look the name up again (as
// grpc.Dial did), instead of trusting the last answer. It replaces the "dns"
// resolver for this one client: an address with no scheme uses it.
func LookUpOnEveryConnect() grpc.DialOption {
	return grpc.WithResolvers(lookUpOnConnect{})
}

// QuickReconnect retries a lost connection after half a second, then a little
// later each time, but never more than five seconds apart.
func QuickReconnect() grpc.DialOption {
	return grpc.WithConnectParams(grpc.ConnectParams{
		Backoff: backoff.Config{
			BaseDelay:  500 * time.Millisecond,
			Multiplier: 1.6,
			Jitter:     0.2,
			MaxDelay:   5 * time.Second,
		},
		MinConnectTimeout: 5 * time.Second,
	})
}

// lookUpOnConnect hands gRPC the name as it is ("trip-service:50055"); the dialer
// resolves it on every connection attempt, so a container's new address is found
// at once.
type lookUpOnConnect struct{}

func (lookUpOnConnect) Scheme() string { return "dns" }

func (lookUpOnConnect) Build(
	target resolver.Target,
	cc resolver.ClientConn,
	_ resolver.BuildOptions,
) (resolver.Resolver, error) {
	// As grpc's own passthrough resolver: the address is fixed, so an error here
	// only means the client is closing.
	_ = cc.UpdateState(resolver.State{Addresses: []resolver.Address{{Addr: target.Endpoint()}}})

	return fixedAddress{}, nil
}

type fixedAddress struct{}

func (fixedAddress) ResolveNow(resolver.ResolveNowOptions) {}

func (fixedAddress) Close() {}
