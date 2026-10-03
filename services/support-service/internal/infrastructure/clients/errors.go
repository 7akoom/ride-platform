package clients

import (
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/support-service/internal/application/support"
)

const callTimeout = 5 * time.Second

func unavailable(what string, err error) error {
	return fmt.Errorf("%w: %s: %v", support.ErrUpstreamUnavailable, what, err)
}

// refusalOrUnavailable tells a definite answer (the action will not work)
// from a service that did not answer (try again later).
func refusalOrUnavailable(what string, err error) error {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.NotFound, codes.PermissionDenied,
		codes.AlreadyExists, codes.OutOfRange, codes.Unauthenticated, codes.Unimplemented:
		return fmt.Errorf("%w: %s: %s", support.ErrRefused, what, status.Convert(err).Message())
	default:
		return unavailable(what, err)
	}
}
