package clients

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ServiceAuthUnaryClientInterceptor attaches the shared internal-service
// token to every outgoing call made over the connection it's installed
// on. This service is a machine caller of location-service, not an end
// user acting on its own behalf, so it authenticates as a service
// rather than forwarding a token it doesn't have.
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
