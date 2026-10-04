package accounts

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Dial connects to another service and sends the internal service token on
// every call.
func Dial(address, internalServiceToken string) (*grpc.ClientConn, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil, fmt.Errorf("service address is required")
	}

	token := internalServiceToken

	return grpc.NewClient(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(func(
			ctx context.Context,
			method string,
			req, reply any,
			cc *grpc.ClientConn,
			invoker grpc.UnaryInvoker,
			opts ...grpc.CallOption,
		) error {
			ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)

			return invoker(ctx, method, req, reply, cc, opts...)
		}),
	)
}
