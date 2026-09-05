package grpc

import "context"

type requestSource struct {
	IPAddress string
}

type requestSourceContextKey struct{}

func contextWithRequestSource(
	ctx context.Context,
	source requestSource,
) context.Context {
	return context.WithValue(
		ctx,
		requestSourceContextKey{},
		source,
	)
}

func requestSourceFromContext(
	ctx context.Context,
) (requestSource, bool) {
	source, ok := ctx.Value(
		requestSourceContextKey{},
	).(requestSource)

	if !ok || source.IPAddress == "" {
		return requestSource{}, false
	}

	return source, true
}
