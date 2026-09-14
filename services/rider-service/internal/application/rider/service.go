package rider

import "context"

type CreateRiderInput struct {
	IdentityID  string
	DisplayName string
}

type UpdateRiderProfileInput struct {
	RiderID     string
	DisplayName string
}

// Service is the application-layer use-case boundary for the rider domain.
// gRPC/HTTP handlers depend on this interface, never on the repository
// directly, so transport stays decoupled from persistence.
type Service interface {
	CreateRider(
		ctx context.Context,
		input CreateRiderInput,
	) (Rider, error)

	GetRider(
		ctx context.Context,
		riderID string,
	) (Rider, error)

	GetRiderByIdentityID(
		ctx context.Context,
		identityID string,
	) (Rider, error)

	UpdateRiderProfile(
		ctx context.Context,
		input UpdateRiderProfileInput,
	) (Rider, error)
}
