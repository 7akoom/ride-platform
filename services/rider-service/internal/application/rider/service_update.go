package rider

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) UpdateRiderProfile(
	ctx context.Context,
	input UpdateRiderProfileInput,
) (Rider, error) {
	riderID := strings.TrimSpace(input.RiderID)
	if riderID == "" {
		return Rider{}, ErrRiderIDRequired
	}

	displayName, err := NewDisplayName(input.DisplayName)
	if err != nil {
		return Rider{}, err
	}

	updated, err := s.repository.UpdateProfile(
		ctx,
		UpdateProfileInput{
			RiderID:     riderID,
			DisplayName: displayName.String(),
		},
	)
	if err != nil {
		return Rider{}, fmt.Errorf("update rider profile: %w", err)
	}

	return updated, nil
}
