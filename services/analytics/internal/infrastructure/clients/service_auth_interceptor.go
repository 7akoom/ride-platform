package clients

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ServiceAuthUnaryClientInterceptor attaches the shared internal-service
// token to every outgoing call made over the connection it's installed
// on. Asking staff-service whether a staff member may act is a
// service-to-service call, so it authenticates as a service.
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
