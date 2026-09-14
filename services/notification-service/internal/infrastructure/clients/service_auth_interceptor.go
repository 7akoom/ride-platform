package clients

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ServiceAuthUnaryClientInterceptor attaches the shared internal-service
// token to every outgoing call made over the connection it's installed
// on. This service's outbound calls to trip-service/driver-service
// happen from a background NATS event handler, not from within an
// incoming end-user request, so there is no end-user token to forward —
// it authenticates as a service instead.
func ServiceAuthUnaryClientInterceptor(internalServiceToken string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		cc *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		ctx = metadata.AppendToOutgoingContext(
			ctx,
			"authorization",
			"Bearer "+internalServiceToken,
		)

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
