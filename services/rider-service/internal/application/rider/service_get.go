package rider

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) GetRider(
	ctx context.Context,
	riderID string,
) (Rider, error) {
	trimmedID := strings.TrimSpace(riderID)
	if trimmedID == "" {
		return Rider{}, ErrRiderIDRequired
	}

	found, err := s.repository.FindByID(ctx, trimmedID)
	if err != nil {
		return Rider{}, fmt.Errorf("find rider by id: %w", err)
	}

	return found, nil
}

func (s *service) GetRiderByIdentityID(
	ctx context.Context,
	identityID string,
) (Rider, error) {
	trimmedID := strings.TrimSpace(identityID)
	if trimmedID == "" {
		return Rider{}, ErrIdentityIDRequired
	}

	found, err := s.repository.FindByIdentityID(ctx, trimmedID)
	if err != nil {
		return Rider{}, fmt.Errorf("find rider by identity id: %w", err)
	}

	return found, nil
}
