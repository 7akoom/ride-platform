package auth

import (
	"errors"
	"sort"
	"strings"
	"time"
)

type IdentityDomainEventType string

const (
	IdentityDomainEventCreated IdentityDomainEventType = "identity.created"

	IdentityDomainEventIdentifierLinked IdentityDomainEventType = "identity.identifier_linked"

	IdentityDomainEventIdentifierUnlinked IdentityDomainEventType = "identity.identifier_unlinked"

	IdentityDomainEventSessionRevoked IdentityDomainEventType = "identity.session_revoked"

	IdentityDomainEventSessionsRevoked IdentityDomainEventType = "identity.sessions_revoked"

	IdentityDomainEventRefreshTokenReuseDetected IdentityDomainEventType = "identity.refresh_token_reuse_detected"

	IdentityDomainEventSuspended IdentityDomainEventType = "identity.suspended"

	IdentityDomainEventDisabled IdentityDomainEventType = "identity.disabled"

	IdentityDomainEventReactivated IdentityDomainEventType = "identity.reactivated"
)

const IdentityDomainEventSchemaVersion int16 = 1

var ErrIdentityLifecycleEventUnchanged = errors.New(
	"identity lifecycle event requires a status change",
)

type IdentityCreatedDomainEvent struct {
	Type          IdentityDomainEventType
	IdentityID    string
	Status        IdentityStatus
	OccurredAt    time.Time
	SchemaVersion int16
}

func NewIdentityCreatedDomainEvent(
	identityID string,
	status IdentityStatus,
	occurredAt time.Time,
) (IdentityCreatedDomainEvent, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return IdentityCreatedDomainEvent{},
			ErrIdentityNotFound
	}

	status, err := ParseIdentityStatus(
		string(status),
	)
	if err != nil {
		return IdentityCreatedDomainEvent{},
			err
	}

	if occurredAt.IsZero() {
		return IdentityCreatedDomainEvent{},
			errors.New(
				"identity created event occurrence time cannot be zero",
			)
	}

	return IdentityCreatedDomainEvent{
		Type:          IdentityDomainEventCreated,
		IdentityID:    identityID,
		Status:        status,
		OccurredAt:    occurredAt.UTC(),
		SchemaVersion: IdentityDomainEventSchemaVersion,
	}, nil
}

type IdentityIdentifierDomainEvent struct {
	Type           IdentityDomainEventType
	IdentityID     string
	IdentifierType IdentifierType
	OccurredAt     time.Time
	SchemaVersion  int16
}

func NewIdentityIdentifierLinkedDomainEvent(
	identityID string,
	identifierType IdentifierType,
	occurredAt time.Time,
) (IdentityIdentifierDomainEvent, error) {
	return newIdentityIdentifierDomainEvent(
		IdentityDomainEventIdentifierLinked,
		identityID,
		identifierType,
		occurredAt,
	)
}

func NewIdentityIdentifierUnlinkedDomainEvent(
	identityID string,
	identifierType IdentifierType,
	occurredAt time.Time,
) (IdentityIdentifierDomainEvent, error) {
	return newIdentityIdentifierDomainEvent(
		IdentityDomainEventIdentifierUnlinked,
		identityID,
		identifierType,
		occurredAt,
	)
}

func newIdentityIdentifierDomainEvent(
	eventType IdentityDomainEventType,
	identityID string,
	identifierType IdentifierType,
	occurredAt time.Time,
) (IdentityIdentifierDomainEvent, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return IdentityIdentifierDomainEvent{},
			ErrIdentityNotFound
	}

	identifierType, err := ParseIdentifierType(
		string(identifierType),
	)
	if err != nil {
		return IdentityIdentifierDomainEvent{},
			err
	}

	if occurredAt.IsZero() {
		return IdentityIdentifierDomainEvent{},
			errors.New(
				"identity identifier event occurrence time cannot be zero",
			)
	}

	switch eventType {
	case IdentityDomainEventIdentifierLinked,
		IdentityDomainEventIdentifierUnlinked:
	default:
		return IdentityIdentifierDomainEvent{},
			errors.New(
				"invalid identity identifier domain event type",
			)
	}

	return IdentityIdentifierDomainEvent{
		Type:           eventType,
		IdentityID:     identityID,
		IdentifierType: identifierType,
		OccurredAt:     occurredAt.UTC(),
		SchemaVersion:  IdentityDomainEventSchemaVersion,
	}, nil
}

type IdentitySessionRevokedDomainEvent struct {
	Type          IdentityDomainEventType
	IdentityID    string
	SessionID     string
	OccurredAt    time.Time
	SchemaVersion int16
}

func NewIdentitySessionRevokedDomainEvent(
	identityID string,
	sessionID string,
	occurredAt time.Time,
) (IdentitySessionRevokedDomainEvent, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return IdentitySessionRevokedDomainEvent{},
			ErrIdentityNotFound
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return IdentitySessionRevokedDomainEvent{},
			errors.New(
				"session revoked event session ID cannot be blank",
			)
	}

	if occurredAt.IsZero() {
		return IdentitySessionRevokedDomainEvent{},
			errors.New(
				"session revoked event occurrence time cannot be zero",
			)
	}

	return IdentitySessionRevokedDomainEvent{
		Type:          IdentityDomainEventSessionRevoked,
		IdentityID:    identityID,
		SessionID:     sessionID,
		OccurredAt:    occurredAt.UTC(),
		SchemaVersion: IdentityDomainEventSchemaVersion,
	}, nil
}

type IdentitySessionsRevokedDomainEvent struct {
	Type          IdentityDomainEventType
	IdentityID    string
	SessionIDs    []string
	OccurredAt    time.Time
	SchemaVersion int16
}

func NewIdentitySessionsRevokedDomainEvent(
	identityID string,
	sessionIDs []string,
	occurredAt time.Time,
) (IdentitySessionsRevokedDomainEvent, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return IdentitySessionsRevokedDomainEvent{},
			ErrIdentityNotFound
	}

	if len(sessionIDs) == 0 {
		return IdentitySessionsRevokedDomainEvent{},
			errors.New(
				"sessions revoked event requires at least one session ID",
			)
	}

	normalizedSessionIDs := make(
		[]string,
		0,
		len(sessionIDs),
	)

	seenSessionIDs := make(
		map[string]struct{},
		len(sessionIDs),
	)

	for _, sessionID := range sessionIDs {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			return IdentitySessionsRevokedDomainEvent{},
				errors.New(
					"sessions revoked event contains blank session ID",
				)
		}

		if _, exists := seenSessionIDs[sessionID]; exists {
			return IdentitySessionsRevokedDomainEvent{},
				errors.New(
					"sessions revoked event contains duplicate session ID",
				)
		}

		seenSessionIDs[sessionID] = struct{}{}

		normalizedSessionIDs = append(
			normalizedSessionIDs,
			sessionID,
		)
	}

	if occurredAt.IsZero() {
		return IdentitySessionsRevokedDomainEvent{},
			errors.New(
				"sessions revoked event occurrence time cannot be zero",
			)
	}

	sort.Strings(normalizedSessionIDs)

	return IdentitySessionsRevokedDomainEvent{
		Type:          IdentityDomainEventSessionsRevoked,
		IdentityID:    identityID,
		SessionIDs:    normalizedSessionIDs,
		OccurredAt:    occurredAt.UTC(),
		SchemaVersion: IdentityDomainEventSchemaVersion,
	}, nil
}

type IdentityRefreshTokenReuseDetectedDomainEvent struct {
	Type          IdentityDomainEventType
	IdentityID    string
	SessionID     string
	OccurredAt    time.Time
	SchemaVersion int16
}

func NewIdentityRefreshTokenReuseDetectedDomainEvent(
	identityID string,
	sessionID string,
	occurredAt time.Time,
) (IdentityRefreshTokenReuseDetectedDomainEvent, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return IdentityRefreshTokenReuseDetectedDomainEvent{},
			ErrIdentityNotFound
	}

	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return IdentityRefreshTokenReuseDetectedDomainEvent{},
			errors.New(
				"refresh token reuse detected event session ID cannot be blank",
			)
	}

	if occurredAt.IsZero() {
		return IdentityRefreshTokenReuseDetectedDomainEvent{},
			errors.New(
				"refresh token reuse detected event occurrence time cannot be zero",
			)
	}

	return IdentityRefreshTokenReuseDetectedDomainEvent{
		Type:          IdentityDomainEventRefreshTokenReuseDetected,
		IdentityID:    identityID,
		SessionID:     sessionID,
		OccurredAt:    occurredAt.UTC(),
		SchemaVersion: IdentityDomainEventSchemaVersion,
	}, nil
}

type IdentityLifecycleDomainEvent struct {
	Type           IdentityDomainEventType
	IdentityID     string
	PreviousStatus IdentityStatus
	CurrentStatus  IdentityStatus
	OccurredAt     time.Time
	SchemaVersion  int16
}

func NewIdentityLifecycleDomainEvent(
	identityID string,
	previousStatus IdentityStatus,
	currentStatus IdentityStatus,
	occurredAt time.Time,
) (IdentityLifecycleDomainEvent, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return IdentityLifecycleDomainEvent{},
			ErrIdentityNotFound
	}

	previousStatus, err := ParseIdentityStatus(
		string(previousStatus),
	)
	if err != nil {
		return IdentityLifecycleDomainEvent{},
			err
	}

	currentStatus, err = ParseIdentityStatus(
		string(currentStatus),
	)
	if err != nil {
		return IdentityLifecycleDomainEvent{},
			err
	}

	if previousStatus == currentStatus {
		return IdentityLifecycleDomainEvent{},
			ErrIdentityLifecycleEventUnchanged
	}

	if occurredAt.IsZero() {
		return IdentityLifecycleDomainEvent{},
			errors.New(
				"identity lifecycle event occurrence time cannot be zero",
			)
	}

	eventType, err :=
		identityLifecycleDomainEventType(
			currentStatus,
		)
	if err != nil {
		return IdentityLifecycleDomainEvent{},
			err
	}

	return IdentityLifecycleDomainEvent{
		Type:           eventType,
		IdentityID:     identityID,
		PreviousStatus: previousStatus,
		CurrentStatus:  currentStatus,
		OccurredAt:     occurredAt.UTC(),
		SchemaVersion:  IdentityDomainEventSchemaVersion,
	}, nil
}

func identityLifecycleDomainEventType(
	targetStatus IdentityStatus,
) (IdentityDomainEventType, error) {
	switch targetStatus {
	case IdentityStatusSuspended:
		return IdentityDomainEventSuspended, nil

	case IdentityStatusDisabled:
		return IdentityDomainEventDisabled, nil

	case IdentityStatusActive:
		return IdentityDomainEventReactivated, nil

	default:
		return "", ErrInvalidIdentityStatus
	}
}
