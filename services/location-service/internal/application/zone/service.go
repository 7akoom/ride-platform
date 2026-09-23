package zone

import "context"

type CreateZoneInput struct {
	CityID   string
	Name     string
	Boundary []Coordinates
}

type UpdateZoneInput struct {
	ZoneID   string
	Name     string
	Boundary []Coordinates
}

type CheckServiceZoneResult struct {
	Served   bool
	ZoneID   string
	CityID   string
	City     string
	TimeZone string
}

type Service interface {
	CreateZone(ctx context.Context, input CreateZoneInput) (Zone, error)

	UpdateZone(ctx context.Context, input UpdateZoneInput) (Zone, error)

	SetZoneActive(ctx context.Context, zoneID string, active bool) (Zone, error)

	GetZone(ctx context.Context, zoneID string) (Zone, error)

	ListZones(ctx context.Context, cityID string) ([]Zone, error)

	// CheckServiceZone is the query every trip request must pass: is
	// this point inside any active service zone?
	CheckServiceZone(ctx context.Context, latitude, longitude float64) (CheckServiceZoneResult, error)
}

type service struct {
	repository  Repository
	idGenerator IDGenerator
}

func NewService(repository Repository, idGenerator IDGenerator) Service {
	if repository == nil {
		panic("zone repository is required")
	}

	if idGenerator == nil {
		panic("zone id generator is required")
	}

	return &service{
		repository:  repository,
		idGenerator: idGenerator,
	}
}
