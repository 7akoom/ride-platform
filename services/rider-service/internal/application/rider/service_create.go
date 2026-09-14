package rider

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func (s *service) CreateRider(
	ctx context.Context,
	input CreateRiderInput,
) (Rider, error) {
	identityID := strings.TrimSpace(input.IdentityID)
	if identityID == "" {
		return Rider{}, ErrIdentityIDRequired
	}

	displayName, err := NewDisplayName(input.DisplayName)
	if err != nil {
		return Rider{}, err
	}

	existing, err := s.repository.FindByIdentityID(ctx, identityID)
	switch {
	case err == nil:
		_ = existing

		return Rider{}, ErrRiderAlreadyExists
	case errors.Is(err, ErrRiderNotFound):
		// Expected path: no rider yet for this identity.
	default:
		return Rider{}, fmt.Errorf("check existing rider for identity: %w", err)
	}

	created, err := s.repository.Create(
		ctx,
		CreateInput{
			ID:          s.idGenerator.NewID(),
			IdentityID:  identityID,
			DisplayName: displayName.String(),
		},
	)
	if err != nil {
		return Rider{}, fmt.Errorf("create rider: %w", err)
	}

	return created, nil
}
