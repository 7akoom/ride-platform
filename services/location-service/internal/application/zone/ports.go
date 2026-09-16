package zone

import "context"

type CreateInput struct {
	ID       string
	City     string
	Name     string
	Boundary []Coordinates
}

type UpdateInput struct {
	ZoneID   string
	Name     string
	Boundary []Coordinates
}

// Repository is the persistence port for zones. Unlike location.Repository
// (ephemeral, Valkey-backed live positions), this is meant to be backed
// by a durable store — zones barely change and must survive a restart.
type Repository interface {
	Create(ctx context.Context, input CreateInput) (Zone, error)

	Update(ctx context.Context, input UpdateInput) (Zone, error)

	SetActive(ctx context.Context, zoneID string, active bool) (Zone, error)

	Get(ctx context.Context, zoneID string) (Zone, error)

	// List returns every zone, optionally filtered to one city (empty
	// string returns all cities).
	List(ctx context.Context, city string) ([]Zone, error)

	// FindContaining returns the first active zone whose boundary
	// covers the given point, and false if no active zone does.
	FindContaining(ctx context.Context, point Coordinates) (Zone, bool, error)
}

type IDGenerator interface {
	NewID() string
}
