package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIdentityLifecycleTransitions(
	t *testing.T,
) {
	transitionedAt := time.Date(
		2026,
		time.August,
		28,
		15,
		30,
		0,
		0,
		time.UTC,
	)

	const identityID = "11111111-1111-1111-1111-111111111111"

	tests := []struct {
		name           string
		targetStatus   IdentityStatus
		previousStatus IdentityStatus
		call           func(
			ServiceWithIdentityLifecycle,
			context.Context,
			IdentityLifecycleInput,
		) (IdentityLifecycleResult, error)
	}{
		{
			name:           "suspend",
			targetStatus:   IdentityStatusSuspended,
			previousStatus: IdentityStatusActive,
			call: func(
				service ServiceWithIdentityLifecycle,
				ctx context.Context,
				input IdentityLifecycleInput,
			) (IdentityLifecycleResult, error) {
				return service.SuspendIdentity(
					ctx,
					input,
				)
			},
		},
		{
			name:           "disable",
			targetStatus:   IdentityStatusDisabled,
			previousStatus: IdentityStatusActive,
			call: func(
				service ServiceWithIdentityLifecycle,
				ctx context.Context,
				input IdentityLifecycleInput,
			) (IdentityLifecycleResult, error) {
				return service.DisableIdentity(
					ctx,
					input,
				)
			},
		},
		{
			name:           "reactivate",
			targetStatus:   IdentityStatusActive,
			previousStatus: IdentityStatusSuspended,
			call: func(
				service ServiceWithIdentityLifecycle,
				ctx context.Context,
				input IdentityLifecycleInput,
			) (IdentityLifecycleResult, error) {
				return service.ReactivateIdentity(
					ctx,
					input,
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				lifecycleStore := &testIdentityLifecycleStore{
					result: IdentityLifecycleTransitionResult{
						PreviousStatus: tt.previousStatus,
						CurrentStatus:  tt.targetStatus,
						Changed:        true,
					},
					found: true,
				}

				clock := &testClock{
					now: transitionedAt,
				}

				dependencies :=
					newValidServiceConstructorTestDependencies()

				dependencies.identityLifecycleStore =
					lifecycleStore
				dependencies.clock = clock

				service :=
					newServiceFromConstructorTestDependencies(
						dependencies,
					)

				result, err := tt.call(
					service,
					context.Background(),
					IdentityLifecycleInput{
						IdentityID: "  " + identityID + "  ",
					},
				)
				if err != nil {
					t.Fatalf(
						"identity lifecycle transition returned an error: %v",
						err,
					)
				}

				if lifecycleStore.calls != 1 {
					t.Fatalf(
						"identity lifecycle store calls = %d, want 1",
						lifecycleStore.calls,
					)
				}

				if lifecycleStore.input.IdentityID != identityID {
					t.Fatalf(
						"identity lifecycle store identity ID = %q, want %q",
						lifecycleStore.input.IdentityID,
						identityID,
					)
				}

				if lifecycleStore.input.TargetStatus !=
					tt.targetStatus {
					t.Fatalf(
						"target status = %q, want %q",
						lifecycleStore.input.TargetStatus,
						tt.targetStatus,
					)
				}

				if !lifecycleStore.input.TransitionedAt.Equal(
					transitionedAt,
				) {
					t.Fatalf(
						"transitioned at = %v, want %v",
						lifecycleStore.input.TransitionedAt,
						transitionedAt,
					)
				}

				if result.PreviousStatus !=
					tt.previousStatus {
					t.Fatalf(
						"previous status = %q, want %q",
						result.PreviousStatus,
						tt.previousStatus,
					)
				}

				if result.CurrentStatus !=
					tt.targetStatus {
					t.Fatalf(
						"current status = %q, want %q",
						result.CurrentStatus,
						tt.targetStatus,
					)
				}

				if !result.Changed {
					t.Fatal(
						"identity lifecycle result reported unchanged transition",
					)
				}
			},
		)
	}
}

func TestIdentityLifecycleRejectsBlankIdentityID(
	t *testing.T,
) {
	lifecycleStore := &testIdentityLifecycleStore{}

	dependencies :=
		newValidServiceConstructorTestDependencies()

	dependencies.identityLifecycleStore =
		lifecycleStore

	service :=
		newServiceFromConstructorTestDependencies(
			dependencies,
		)

	_, err := service.SuspendIdentity(
		context.Background(),
		IdentityLifecycleInput{
			IdentityID: "   ",
		},
	)

	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf(
			"SuspendIdentity() error = %v, want %v",
			err,
			ErrIdentityNotFound,
		)
	}

	if lifecycleStore.calls != 0 {
		t.Fatalf(
			"identity lifecycle store calls = %d, want 0",
			lifecycleStore.calls,
		)
	}
}

func TestIdentityLifecycleReturnsNotFoundForUnknownIdentity(
	t *testing.T,
) {
	lifecycleStore := &testIdentityLifecycleStore{
		found: false,
	}

	dependencies :=
		newValidServiceConstructorTestDependencies()

	dependencies.identityLifecycleStore =
		lifecycleStore

	service :=
		newServiceFromConstructorTestDependencies(
			dependencies,
		)

	_, err := service.DisableIdentity(
		context.Background(),
		IdentityLifecycleInput{
			IdentityID: "22222222-2222-2222-2222-222222222222",
		},
	)

	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf(
			"DisableIdentity() error = %v, want %v",
			err,
			ErrIdentityNotFound,
		)
	}

	if lifecycleStore.calls != 1 {
		t.Fatalf(
			"identity lifecycle store calls = %d, want 1",
			lifecycleStore.calls,
		)
	}
}

func TestIdentityLifecyclePropagatesStoreError(
	t *testing.T,
) {
	storeError := errors.New(
		"identity lifecycle store unavailable",
	)

	lifecycleStore := &testIdentityLifecycleStore{
		err: storeError,
	}

	dependencies :=
		newValidServiceConstructorTestDependencies()

	dependencies.identityLifecycleStore =
		lifecycleStore

	service :=
		newServiceFromConstructorTestDependencies(
			dependencies,
		)

	_, err := service.ReactivateIdentity(
		context.Background(),
		IdentityLifecycleInput{
			IdentityID: "33333333-3333-3333-3333-333333333333",
		},
	)

	if !errors.Is(err, storeError) {
		t.Fatalf(
			"ReactivateIdentity() error = %v, want wrapped %v",
			err,
			storeError,
		)
	}

	if lifecycleStore.calls != 1 {
		t.Fatalf(
			"identity lifecycle store calls = %d, want 1",
			lifecycleStore.calls,
		)
	}
}

func TestIdentityLifecycleReturnsUnchangedTransition(
	t *testing.T,
) {
	lifecycleStore := &testIdentityLifecycleStore{
		result: IdentityLifecycleTransitionResult{
			PreviousStatus: IdentityStatusSuspended,
			CurrentStatus:  IdentityStatusSuspended,
			Changed:        false,
		},
		found: true,
	}

	dependencies :=
		newValidServiceConstructorTestDependencies()

	dependencies.identityLifecycleStore =
		lifecycleStore

	service :=
		newServiceFromConstructorTestDependencies(
			dependencies,
		)

	result, err := service.SuspendIdentity(
		context.Background(),
		IdentityLifecycleInput{
			IdentityID: "44444444-4444-4444-4444-444444444444",
		},
	)
	if err != nil {
		t.Fatalf(
			"SuspendIdentity() returned an error: %v",
			err,
		)
	}

	if result.PreviousStatus !=
		IdentityStatusSuspended {
		t.Fatalf(
			"previous status = %q, want %q",
			result.PreviousStatus,
			IdentityStatusSuspended,
		)
	}

	if result.CurrentStatus !=
		IdentityStatusSuspended {
		t.Fatalf(
			"current status = %q, want %q",
			result.CurrentStatus,
			IdentityStatusSuspended,
		)
	}

	if result.Changed {
		t.Fatal(
			"same-status lifecycle transition was reported as changed",
		)
	}
}
