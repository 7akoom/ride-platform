package grpc

import (
	"context"

	googlegrpc "google.golang.org/grpc"
)

func NewRequestSourceUnaryInterceptor() googlegrpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		info *googlegrpc.UnaryServerInfo,
		handler googlegrpc.UnaryHandler,
	) (any, error) {
		sourceIPAddress := sessionIPAddressFromContext(ctx)

		if sourceIPAddress == "" {
			return handler(
				ctx,
				request,
			)
		}

		sourceContext := contextWithRequestSource(
			ctx,
			requestSource{
				IPAddress: sourceIPAddress,
			},
		)

		return handler(
			sourceContext,
			request,
		)
	}
}
