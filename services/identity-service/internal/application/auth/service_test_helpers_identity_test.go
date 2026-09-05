package auth

import (
	"context"
	"time"
)

type testIdentityReader struct {
	findResult     IdentityDetails
	findFound      bool
	findErr        error
	findCalls      int
	findIdentityID string
}

func (r *testIdentityReader) FindByID(
	ctx context.Context,
	identityID string,
) (IdentityDetails, bool, error) {
	r.findCalls++
	r.findIdentityID = identityID

	if r.findErr != nil {
		return IdentityDetails{}, false, r.findErr
	}

	return r.findResult, r.findFound, nil
}

type testIdentityIdentifierRepository struct {
	findResult     Identity
	findFound      bool
	findErr        error
	findCalls      int
	findIdentifier Identifier

	createResult     Identity
	createErr        error
	createCalls      int
	createIdentifier Identifier
	createVerifiedAt time.Time

	linkCalls      int
	linkIdentityID string
	linkIdentifier Identifier
	linkVerifiedAt time.Time
	linkErr        error
}

func (r *testIdentityIdentifierRepository) FindIdentityByIdentifier(
	ctx context.Context,
	identifier Identifier,
) (Identity, bool, error) {
	r.findCalls++
	r.findIdentifier = identifier

	if r.findErr != nil {
		return Identity{}, false, r.findErr
	}

	return r.findResult, r.findFound, nil
}

func (r *testIdentityIdentifierRepository) CreateIdentityWithIdentifier(
	ctx context.Context,
	identifier Identifier,
	verifiedAt time.Time,
) (Identity, error) {
	r.createCalls++
	r.createIdentifier = identifier
	r.createVerifiedAt = verifiedAt

	if r.createErr != nil {
		return Identity{}, r.createErr
	}

	return r.createResult, nil
}

func (r *testIdentityIdentifierRepository) LinkIdentifier(
	ctx context.Context,
	identityID string,
	identifier Identifier,
	verifiedAt time.Time,
) error {
	r.linkCalls++
	r.linkIdentityID = identityID
	r.linkIdentifier = identifier
	r.linkVerifiedAt = verifiedAt

	return r.linkErr
}

type testIdentifierLinkCompletionStore struct {
	calls int
	input IdentifierLinkCompletionInput
	err   error
}

func (s *testIdentifierLinkCompletionStore) Complete(
	ctx context.Context,
	input IdentifierLinkCompletionInput,
) error {
	s.calls++
	s.input = input

	return s.err
}

type testIdentifierUnlinkRequestStore struct {
	calls int
	input IdentifierUnlinkRequestInput
	err   error
}

func (s *testIdentifierUnlinkRequestStore) Create(
	ctx context.Context,
	input IdentifierUnlinkRequestInput,
) error {
	s.calls++
	s.input = input

	return s.err
}

type testIdentifierUnlinkCompletionStore struct {
	calls int
	input IdentifierUnlinkCompletionInput
	err   error
}

func (s *testIdentifierUnlinkCompletionStore) Complete(
	ctx context.Context,
	input IdentifierUnlinkCompletionInput,
) error {
	s.calls++
	s.input = input

	return s.err
}

type testIdentityLifecycleStore struct {
	calls  int
	input  IdentityLifecycleTransition
	result IdentityLifecycleTransitionResult
	found  bool
	err    error
}

func (s *testIdentityLifecycleStore) Transition(
	ctx context.Context,
	input IdentityLifecycleTransition,
) (IdentityLifecycleTransitionResult, bool, error) {
	s.calls++
	s.input = input

	if s.err != nil {
		return IdentityLifecycleTransitionResult{}, false, s.err
	}

	return s.result, s.found, nil
}
