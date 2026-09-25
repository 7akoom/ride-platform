package grpc

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// rateLimiterEntry pairs a client's token bucket with when it was last
// used, so idle entries can be evicted instead of accumulating forever
// (one entry per distinct caller identity seen since the process
// started).
type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// perClientRateLimiter holds one token bucket per caller identity.
type perClientRateLimiter struct {
	mu                sync.Mutex
	clients           map[string]*rateLimiterEntry
	requestsPerSecond float64
	burst             int
}

func newPerClientRateLimiter(
	requestsPerSecond float64,
	burst int,
) *perClientRateLimiter {
	return &perClientRateLimiter{
		clients:           make(map[string]*rateLimiterEntry),
		requestsPerSecond: requestsPerSecond,
		burst:             burst,
	}
}

func (r *perClientRateLimiter) allow(key string) bool {
	r.mu.Lock()

	entry, ok := r.clients[key]
	if !ok {
		entry = &rateLimiterEntry{
			limiter: rate.NewLimiter(
				rate.Limit(r.requestsPerSecond),
				r.burst,
			),
		}
		r.clients[key] = entry
	}

	entry.lastSeen = time.Now()

	limiter := entry.limiter

	r.mu.Unlock()

	return limiter.Allow()
}

func (r *perClientRateLimiter) evictIdleSince(cutoff time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for key, entry := range r.clients {
		if entry.lastSeen.Before(cutoff) {
			delete(r.clients, key)
		}
	}
}

const (
	rateLimiterEvictionInterval = 5 * time.Minute
	rateLimiterMaxIdle          = 10 * time.Minute
)

// NewRateLimitUnaryInterceptor rejects requests beyond a per-caller
// token-bucket rate, identifying the caller by the authenticated
// principal set by NewAuthenticationUnaryInterceptor (so this
// interceptor must run after it in the chain). Exempt: the health
// check, and internal service-to-service calls (the shared-token
// principal, internalServicePrincipalID) — those are trusted and
// already volume-bounded by the calling service's own logic, not by an
// external actor that needs throttling.
//
// This is a per-process, in-memory limiter: fine for this project's
// single-instance-per-deployment model (see the architecture doc), but
// it resets on restart and doesn't coordinate across replicas. Revisit
// with a shared store (e.g. Valkey) if a service is ever horizontally
// scaled.
func NewRateLimitUnaryInterceptor(
	requestsPerSecond float64,
	burst int,
) googlegrpc.UnaryServerInterceptor {
	if requestsPerSecond <= 0 {
		panic("rate limit requests per second must be positive")
	}

	if burst <= 0 {
		panic("rate limit burst must be positive")
	}

	limiter := newPerClientRateLimiter(requestsPerSecond, burst)

	go func() {
		ticker := time.NewTicker(rateLimiterEvictionInterval)
		defer ticker.Stop()

		for range ticker.C {
			limiter.evictIdleSince(
				time.Now().Add(-rateLimiterMaxIdle),
			)
		}
	}()

	return func(
		ctx context.Context,
		request any,
		info *googlegrpc.UnaryServerInfo,
		handler googlegrpc.UnaryHandler,
	) (any, error) {
		if info != nil &&
			info.FullMethod == healthv1.Health_Check_FullMethodName {
			return handler(ctx, request)
		}

		key := rateLimitKey(ctx)

		if key != internalServicePrincipalID && !limiter.allow(key) {
			return nil, status.Error(
				codes.ResourceExhausted,
				"rate limit exceeded, slow down and try again",
			)
		}

		return handler(ctx, request)
	}
}

// rateLimitKey identifies the caller: the authenticated end-user's
// identity id when available, otherwise the shared internal-service
// marker. This interceptor is expected to run after authentication, so
// one or the other is always set for any request reaching a real
// handler; the fallback only guards against a future reordering
// mistake, not a real unauthenticated path (auth already rejects that
// before this interceptor runs).
func rateLimitKey(ctx context.Context) string {
	if principal, ok := authenticatedPrincipalFromContext(ctx); ok {
		return principal.IdentityID
	}

	// A call without a credential (a shared trip's link) is counted by the
	// address it came from: the last x-forwarded-for entry is the one the
	// gateway added, then the connection's own peer.
	if forwarded := metadata.ValueFromIncomingContext(ctx, "x-forwarded-for"); len(forwarded) > 0 {
		hops := strings.Split(forwarded[len(forwarded)-1], ",")
		if last := strings.TrimSpace(hops[len(hops)-1]); last != "" {
			return "anonymous:" + last
		}
	}

	if p, ok := peer.FromContext(ctx); ok && p.Addr != nil {
		host := p.Addr.String()
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}

		return "anonymous:" + host
	}

	return "anonymous"
}
