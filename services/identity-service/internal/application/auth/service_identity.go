package auth

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) GetMyIdentity(
	ctx context.Context,
	input GetMyIdentityInput,
) (IdentityDetails, error) {
	identityID := strings.TrimSpace(input.IdentityID)
	if identityID == "" {
		return IdentityDetails{}, ErrIdentityNotFound
	}

	details, found, err := s.identityReader.FindByID(
		ctx,
		identityID,
	)
	if err != nil {
		return IdentityDetails{}, fmt.Errorf(
			"get identity details: %w",
			err,
		)
	}

	if !found {
		return IdentityDetails{}, ErrIdentityNotFound
	}

	return details, nil
}
