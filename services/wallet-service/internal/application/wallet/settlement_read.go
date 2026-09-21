package wallet

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrSettlementNotFound covers three cases the caller must not be able to tell
// apart: the trip has no settlement yet, there is no such trip, and the trip
// exists but the caller is not one of the two people on it.
var ErrSettlementNotFound = errors.New("trip settlement not found")

// uuidShape is the canonical form every trip id has. Anything else cannot
// exist, and must not reach the database (which would answer with an error).
var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// GetTripSettlement returns how a settled trip's fare was paid. Only the rider
// and the driver of that trip may see it: the settlement is looked up by trip,
// and the owner asking must be the rider or the driver recorded on it.
func (s *service) GetTripSettlement(
	ctx context.Context,
	ownerType OwnerType,
	ownerID string,
	tripID string,
) (Settlement, error) {
	if !ownerType.Valid() {
		return Settlement{}, ErrInvalidOwnerType
	}

	trimmedOwner := strings.TrimSpace(ownerID)
	if trimmedOwner == "" {
		return Settlement{}, ErrOwnerIDRequired
	}

	trimmedTrip := strings.TrimSpace(tripID)
	if trimmedTrip == "" {
		return Settlement{}, ErrTripIDRequired
	}

	if !uuidShape.MatchString(trimmedTrip) {
		return Settlement{}, ErrSettlementNotFound
	}

	found, exists, err := s.repository.FindSettlement(ctx, trimmedTrip)
	if err != nil {
		return Settlement{}, fmt.Errorf("find trip settlement: %w", err)
	}

	if !exists {
		return Settlement{}, ErrSettlementNotFound
	}

	participant := false

	switch ownerType {
	case OwnerRider:
		participant = strings.EqualFold(found.RiderID, trimmedOwner)
	case OwnerDriver:
		participant = strings.EqualFold(found.DriverID, trimmedOwner)
	}

	if !participant {
		return Settlement{}, ErrSettlementNotFound
	}

	return found, nil
}
