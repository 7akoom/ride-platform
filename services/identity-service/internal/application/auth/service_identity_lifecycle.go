package auth

import (
	"context"
	"fmt"
	"strings"
)

func (s *service) SuspendIdentity(
	ctx context.Context,
	input IdentityLifecycleInput,
) (IdentityLifecycleResult, error) {
	return s.transitionIdentityStatus(
		ctx,
		input,
		IdentityStatusSuspended,
	)
}

func (s *service) DisableIdentity(
	ctx context.Context,
	input IdentityLifecycleInput,
) (IdentityLifecycleResult, error) {
	return s.transitionIdentityStatus(
		ctx,
		input,
		IdentityStatusDisabled,
	)
}

func (s *service) ReactivateIdentity(
	ctx context.Context,
	input IdentityLifecycleInput,
) (IdentityLifecycleResult, error) {
	return s.transitionIdentityStatus(
		ctx,
		input,
		IdentityStatusActive,
	)
}

func (s *service) transitionIdentityStatus(
	ctx context.Context,
	input IdentityLifecycleInput,
	targetStatus IdentityStatus,
) (IdentityLifecycleResult, error) {
	identityID := strings.TrimSpace(input.IdentityID)
	if identityID == "" {
		return IdentityLifecycleResult{}, ErrIdentityNotFound
	}

	if s.identityLifecycleStore == nil {
		return IdentityLifecycleResult{},
			fmt.Errorf("identity lifecycle store is not configured")
	}

	result, found, err := s.identityLifecycleStore.Transition(
		ctx,
		IdentityLifecycleTransition{
			IdentityID:     identityID,
			TargetStatus:   targetStatus,
			TransitionedAt: s.clock.Now(),
		},
	)
	if err != nil {
		return IdentityLifecycleResult{}, fmt.Errorf(
			"transition identity lifecycle: %w",
			err,
		)
	}

	if !found {
		return IdentityLifecycleResult{}, ErrIdentityNotFound
	}

	return IdentityLifecycleResult{
		PreviousStatus: result.PreviousStatus,
		CurrentStatus:  result.CurrentStatus,
		Changed:        result.Changed,
	}, nil
}
