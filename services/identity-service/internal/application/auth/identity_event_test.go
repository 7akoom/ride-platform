package auth

import (
	"errors"
	"testing"
	"time"
)

func TestNewIdentityCreatedDomainEventBuildsEvent(
	t *testing.T,
) {
	occurredAt := time.Date(
		2026,
		time.August,
		28,
		16,
		0,
		0,
		0,
		time.FixedZone(
			"UTC+3",
			3*60*60,
		),
	)

	event, err := NewIdentityCreatedDomainEvent(
		"  11111111-1111-1111-1111-111111111111  ",
		IdentityStatusActive,
		occurredAt,
	)
	if err != nil {
		t.Fatalf(
			"NewIdentityCreatedDomainEvent() returned an error: %v",
			err,
		)
	}

	if event.Type != IdentityDomainEventCreated {
		t.Fatalf(
			"event type = %q, want %q",
			event.Type,
			IdentityDomainEventCreated,
		)
	}

	if event.IdentityID !=
		"11111111-1111-1111-1111-111111111111" {
		t.Fatalf(
			"identity ID = %q",
			event.IdentityID,
		)
	}

	if event.Status != IdentityStatusActive {
		t.Fatalf(
			"status = %q, want %q",
			event.Status,
			IdentityStatusActive,
		)
	}

	if !event.OccurredAt.Equal(
		occurredAt.UTC(),
	) {
		t.Fatalf(
			"occurred at = %v, want %v",
			event.OccurredAt,
			occurredAt.UTC(),
		)
	}

	if event.OccurredAt.Location() != time.UTC {
		t.Fatalf(
			"occurred at location = %v, want UTC",
			event.OccurredAt.Location(),
		)
	}

	if event.SchemaVersion !=
		IdentityDomainEventSchemaVersion {
		t.Fatalf(
			"schema version = %d, want %d",
			event.SchemaVersion,
			IdentityDomainEventSchemaVersion,
		)
	}
}

func TestNewIdentityCreatedDomainEventRejectsBlankIdentityID(
	t *testing.T,
) {
	_, err := NewIdentityCreatedDomainEvent(
		"   ",
		IdentityStatusActive,
		time.Now(),
	)

	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf(
			"NewIdentityCreatedDomainEvent() error = %v, want %v",
			err,
			ErrIdentityNotFound,
		)
	}
}

func TestNewIdentityCreatedDomainEventRejectsInvalidStatus(
	t *testing.T,
) {
	_, err := NewIdentityCreatedDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		IdentityStatus("deleted"),
		time.Now(),
	)

	if !errors.Is(err, ErrInvalidIdentityStatus) {
		t.Fatalf(
			"NewIdentityCreatedDomainEvent() error = %v, want %v",
			err,
			ErrInvalidIdentityStatus,
		)
	}
}

func TestNewIdentityCreatedDomainEventRejectsZeroOccurrenceTime(
	t *testing.T,
) {
	_, err := NewIdentityCreatedDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		IdentityStatusActive,
		time.Time{},
	)

	if err == nil {
		t.Fatal(
			"NewIdentityCreatedDomainEvent() did not reject zero occurrence time",
		)
	}
}

func TestNewIdentityIdentifierDomainEventsBuildEvents(
	t *testing.T,
) {
	occurredAt := time.Date(
		2026,
		time.August,
		28,
		18,
		0,
		0,
		0,
		time.FixedZone(
			"UTC+3",
			3*60*60,
		),
	)

	tests := []struct {
		name      string
		build     func() (IdentityIdentifierDomainEvent, error)
		eventType IdentityDomainEventType
	}{
		{
			name: "linked",
			build: func() (IdentityIdentifierDomainEvent, error) {
				return NewIdentityIdentifierLinkedDomainEvent(
					"  11111111-1111-1111-1111-111111111111  ",
					IdentifierTypeEmail,
					occurredAt,
				)
			},
			eventType: IdentityDomainEventIdentifierLinked,
		},
		{
			name: "unlinked",
			build: func() (IdentityIdentifierDomainEvent, error) {
				return NewIdentityIdentifierUnlinkedDomainEvent(
					"  11111111-1111-1111-1111-111111111111  ",
					IdentifierTypeEmail,
					occurredAt,
				)
			},
			eventType: IdentityDomainEventIdentifierUnlinked,
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				event, err := tt.build()
				if err != nil {
					t.Fatalf(
						"build identity identifier domain event: %v",
						err,
					)
				}

				if event.Type != tt.eventType {
					t.Fatalf(
						"event type = %q, want %q",
						event.Type,
						tt.eventType,
					)
				}

				if event.IdentityID !=
					"11111111-1111-1111-1111-111111111111" {
					t.Fatalf(
						"identity ID = %q",
						event.IdentityID,
					)
				}

				if event.IdentifierType != IdentifierTypeEmail {
					t.Fatalf(
						"identifier type = %q, want %q",
						event.IdentifierType,
						IdentifierTypeEmail,
					)
				}

				if !event.OccurredAt.Equal(
					occurredAt.UTC(),
				) {
					t.Fatalf(
						"occurred at = %v, want %v",
						event.OccurredAt,
						occurredAt.UTC(),
					)
				}

				if event.OccurredAt.Location() != time.UTC {
					t.Fatalf(
						"occurred at location = %v, want UTC",
						event.OccurredAt.Location(),
					)
				}

				if event.SchemaVersion !=
					IdentityDomainEventSchemaVersion {
					t.Fatalf(
						"schema version = %d, want %d",
						event.SchemaVersion,
						IdentityDomainEventSchemaVersion,
					)
				}
			},
		)
	}
}

func TestNewIdentityIdentifierDomainEventRejectsBlankIdentityID(
	t *testing.T,
) {
	_, err := NewIdentityIdentifierLinkedDomainEvent(
		"   ",
		IdentifierTypePhone,
		time.Now(),
	)

	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf(
			"NewIdentityIdentifierLinkedDomainEvent() error = %v, want %v",
			err,
			ErrIdentityNotFound,
		)
	}
}

func TestNewIdentityIdentifierDomainEventRejectsInvalidIdentifierType(
	t *testing.T,
) {
	_, err := NewIdentityIdentifierLinkedDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		IdentifierType("username"),
		time.Now(),
	)

	if err == nil {
		t.Fatal(
			"NewIdentityIdentifierLinkedDomainEvent() did not reject invalid identifier type",
		)
	}
}

func TestNewIdentityIdentifierDomainEventRejectsZeroOccurrenceTime(
	t *testing.T,
) {
	_, err := NewIdentityIdentifierUnlinkedDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		IdentifierTypePhone,
		time.Time{},
	)

	if err == nil {
		t.Fatal(
			"NewIdentityIdentifierUnlinkedDomainEvent() did not reject zero occurrence time",
		)
	}
}

func TestNewIdentitySessionRevokedDomainEventBuildsEvent(
	t *testing.T,
) {
	occurredAt := time.Date(
		2026,
		time.August,
		28,
		19,
		0,
		0,
		0,
		time.FixedZone(
			"UTC+3",
			3*60*60,
		),
	)

	event, err := NewIdentitySessionRevokedDomainEvent(
		"  11111111-1111-1111-1111-111111111111  ",
		"  session-123  ",
		occurredAt,
	)
	if err != nil {
		t.Fatalf(
			"NewIdentitySessionRevokedDomainEvent() returned an error: %v",
			err,
		)
	}

	if event.Type != IdentityDomainEventSessionRevoked {
		t.Fatalf(
			"event type = %q, want %q",
			event.Type,
			IdentityDomainEventSessionRevoked,
		)
	}

	if event.IdentityID !=
		"11111111-1111-1111-1111-111111111111" {
		t.Fatalf(
			"identity ID = %q",
			event.IdentityID,
		)
	}

	if event.SessionID != "session-123" {
		t.Fatalf(
			"session ID = %q, want %q",
			event.SessionID,
			"session-123",
		)
	}

	if !event.OccurredAt.Equal(
		occurredAt.UTC(),
	) {
		t.Fatalf(
			"occurred at = %v, want %v",
			event.OccurredAt,
			occurredAt.UTC(),
		)
	}

	if event.OccurredAt.Location() != time.UTC {
		t.Fatalf(
			"occurred at location = %v, want UTC",
			event.OccurredAt.Location(),
		)
	}

	if event.SchemaVersion !=
		IdentityDomainEventSchemaVersion {
		t.Fatalf(
			"schema version = %d, want %d",
			event.SchemaVersion,
			IdentityDomainEventSchemaVersion,
		)
	}
}

func TestNewIdentitySessionRevokedDomainEventRejectsBlankIdentityID(
	t *testing.T,
) {
	_, err := NewIdentitySessionRevokedDomainEvent(
		"   ",
		"session-123",
		time.Now(),
	)

	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf(
			"NewIdentitySessionRevokedDomainEvent() error = %v, want %v",
			err,
			ErrIdentityNotFound,
		)
	}
}

func TestNewIdentitySessionRevokedDomainEventRejectsBlankSessionID(
	t *testing.T,
) {
	_, err := NewIdentitySessionRevokedDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		"   ",
		time.Now(),
	)

	if err == nil {
		t.Fatal(
			"NewIdentitySessionRevokedDomainEvent() did not reject blank session ID",
		)
	}
}

func TestNewIdentitySessionRevokedDomainEventRejectsZeroOccurrenceTime(
	t *testing.T,
) {
	_, err := NewIdentitySessionRevokedDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		"session-123",
		time.Time{},
	)

	if err == nil {
		t.Fatal(
			"NewIdentitySessionRevokedDomainEvent() did not reject zero occurrence time",
		)
	}
}

func TestNewIdentitySessionsRevokedDomainEventBuildsEvent(
	t *testing.T,
) {
	occurredAt := time.Date(
		2026,
		time.August,
		28,
		20,
		0,
		0,
		0,
		time.FixedZone(
			"UTC+3",
			3*60*60,
		),
	)

	event, err := NewIdentitySessionsRevokedDomainEvent(
		"  11111111-1111-1111-1111-111111111111  ",
		[]string{
			"  session-b  ",
			"session-a",
		},
		occurredAt,
	)
	if err != nil {
		t.Fatalf(
			"NewIdentitySessionsRevokedDomainEvent() returned an error: %v",
			err,
		)
	}

	if event.Type != IdentityDomainEventSessionsRevoked {
		t.Fatalf(
			"event type = %q, want %q",
			event.Type,
			IdentityDomainEventSessionsRevoked,
		)
	}

	if event.IdentityID !=
		"11111111-1111-1111-1111-111111111111" {
		t.Fatalf(
			"identity ID = %q",
			event.IdentityID,
		)
	}

	if len(event.SessionIDs) != 2 {
		t.Fatalf(
			"session ID count = %d, want 2",
			len(event.SessionIDs),
		)
	}

	if event.SessionIDs[0] != "session-a" ||
		event.SessionIDs[1] != "session-b" {
		t.Fatalf(
			"session IDs = %v, want [session-a session-b]",
			event.SessionIDs,
		)
	}

	if !event.OccurredAt.Equal(
		occurredAt.UTC(),
	) {
		t.Fatalf(
			"occurred at = %v, want %v",
			event.OccurredAt,
			occurredAt.UTC(),
		)
	}

	if event.OccurredAt.Location() != time.UTC {
		t.Fatalf(
			"occurred at location = %v, want UTC",
			event.OccurredAt.Location(),
		)
	}

	if event.SchemaVersion !=
		IdentityDomainEventSchemaVersion {
		t.Fatalf(
			"schema version = %d, want %d",
			event.SchemaVersion,
			IdentityDomainEventSchemaVersion,
		)
	}
}

func TestNewIdentitySessionsRevokedDomainEventRejectsBlankIdentityID(
	t *testing.T,
) {
	_, err := NewIdentitySessionsRevokedDomainEvent(
		"   ",
		[]string{"session-a"},
		time.Now(),
	)

	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf(
			"NewIdentitySessionsRevokedDomainEvent() error = %v, want %v",
			err,
			ErrIdentityNotFound,
		)
	}
}

func TestNewIdentitySessionsRevokedDomainEventRejectsEmptySessionIDs(
	t *testing.T,
) {
	_, err := NewIdentitySessionsRevokedDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		nil,
		time.Now(),
	)

	if err == nil {
		t.Fatal(
			"NewIdentitySessionsRevokedDomainEvent() did not reject empty session IDs",
		)
	}
}

func TestNewIdentitySessionsRevokedDomainEventRejectsBlankSessionID(
	t *testing.T,
) {
	_, err := NewIdentitySessionsRevokedDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		[]string{
			"session-a",
			"   ",
		},
		time.Now(),
	)

	if err == nil {
		t.Fatal(
			"NewIdentitySessionsRevokedDomainEvent() did not reject blank session ID",
		)
	}
}

func TestNewIdentitySessionsRevokedDomainEventRejectsDuplicateSessionID(
	t *testing.T,
) {
	_, err := NewIdentitySessionsRevokedDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		[]string{
			"session-a",
			"  session-a  ",
		},
		time.Now(),
	)

	if err == nil {
		t.Fatal(
			"NewIdentitySessionsRevokedDomainEvent() did not reject duplicate session ID",
		)
	}
}

func TestNewIdentitySessionsRevokedDomainEventRejectsZeroOccurrenceTime(
	t *testing.T,
) {
	_, err := NewIdentitySessionsRevokedDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		[]string{"session-a"},
		time.Time{},
	)

	if err == nil {
		t.Fatal(
			"NewIdentitySessionsRevokedDomainEvent() did not reject zero occurrence time",
		)
	}
}

func TestNewIdentityRefreshTokenReuseDetectedDomainEventBuildsEvent(
	t *testing.T,
) {
	occurredAt := time.Date(
		2026,
		time.August,
		28,
		22,
		0,
		0,
		0,
		time.FixedZone(
			"UTC+3",
			3*60*60,
		),
	)

	event, err :=
		NewIdentityRefreshTokenReuseDetectedDomainEvent(
			"  11111111-1111-1111-1111-111111111111  ",
			"  session-123  ",
			occurredAt,
		)
	if err != nil {
		t.Fatalf(
			"NewIdentityRefreshTokenReuseDetectedDomainEvent() returned an error: %v",
			err,
		)
	}

	if event.Type !=
		IdentityDomainEventRefreshTokenReuseDetected {
		t.Fatalf(
			"event type = %q, want %q",
			event.Type,
			IdentityDomainEventRefreshTokenReuseDetected,
		)
	}

	if event.IdentityID !=
		"11111111-1111-1111-1111-111111111111" {
		t.Fatalf(
			"identity ID = %q",
			event.IdentityID,
		)
	}

	if event.SessionID != "session-123" {
		t.Fatalf(
			"session ID = %q, want %q",
			event.SessionID,
			"session-123",
		)
	}

	if !event.OccurredAt.Equal(
		occurredAt.UTC(),
	) {
		t.Fatalf(
			"occurred at = %v, want %v",
			event.OccurredAt,
			occurredAt.UTC(),
		)
	}

	if event.OccurredAt.Location() != time.UTC {
		t.Fatalf(
			"occurred at location = %v, want UTC",
			event.OccurredAt.Location(),
		)
	}

	if event.SchemaVersion !=
		IdentityDomainEventSchemaVersion {
		t.Fatalf(
			"schema version = %d, want %d",
			event.SchemaVersion,
			IdentityDomainEventSchemaVersion,
		)
	}
}

func TestNewIdentityRefreshTokenReuseDetectedDomainEventRejectsBlankIdentityID(
	t *testing.T,
) {
	_, err :=
		NewIdentityRefreshTokenReuseDetectedDomainEvent(
			"   ",
			"session-123",
			time.Now(),
		)

	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf(
			"NewIdentityRefreshTokenReuseDetectedDomainEvent() error = %v, want %v",
			err,
			ErrIdentityNotFound,
		)
	}
}

func TestNewIdentityRefreshTokenReuseDetectedDomainEventRejectsBlankSessionID(
	t *testing.T,
) {
	_, err :=
		NewIdentityRefreshTokenReuseDetectedDomainEvent(
			"11111111-1111-1111-1111-111111111111",
			"   ",
			time.Now(),
		)

	if err == nil {
		t.Fatal(
			"NewIdentityRefreshTokenReuseDetectedDomainEvent() did not reject blank session ID",
		)
	}
}

func TestNewIdentityRefreshTokenReuseDetectedDomainEventRejectsZeroOccurrenceTime(
	t *testing.T,
) {
	_, err :=
		NewIdentityRefreshTokenReuseDetectedDomainEvent(
			"11111111-1111-1111-1111-111111111111",
			"session-123",
			time.Time{},
		)

	if err == nil {
		t.Fatal(
			"NewIdentityRefreshTokenReuseDetectedDomainEvent() did not reject zero occurrence time",
		)
	}
}

func TestIdentityLifecycleDomainEventTypeMapsSupportedStatuses(
	t *testing.T,
) {
	tests := []struct {
		name   string
		status IdentityStatus
		want   IdentityDomainEventType
	}{
		{
			name:   "suspended",
			status: IdentityStatusSuspended,
			want:   IdentityDomainEventSuspended,
		},
		{
			name:   "disabled",
			status: IdentityStatusDisabled,
			want:   IdentityDomainEventDisabled,
		},
		{
			name:   "reactivated",
			status: IdentityStatusActive,
			want:   IdentityDomainEventReactivated,
		},
	}

	for _, tt := range tests {
		t.Run(
			tt.name,
			func(t *testing.T) {
				got, err :=
					identityLifecycleDomainEventType(
						tt.status,
					)
				if err != nil {
					t.Fatalf(
						"identityLifecycleDomainEventType() returned an error: %v",
						err,
					)
				}

				if got != tt.want {
					t.Fatalf(
						"event type = %q, want %q",
						got,
						tt.want,
					)
				}
			},
		)
	}
}

func TestIdentityLifecycleDomainEventTypeRejectsInvalidStatus(
	t *testing.T,
) {
	got, err :=
		identityLifecycleDomainEventType(
			IdentityStatus("deleted"),
		)

	if !errors.Is(err, ErrInvalidIdentityStatus) {
		t.Fatalf(
			"identityLifecycleDomainEventType() error = %v, want %v",
			err,
			ErrInvalidIdentityStatus,
		)
	}

	if got != "" {
		t.Fatalf(
			"event type = %q, want empty",
			got,
		)
	}
}

func TestNewIdentityLifecycleDomainEventBuildsEvent(
	t *testing.T,
) {
	occurredAt := time.Date(
		2026,
		time.August,
		28,
		16,
		0,
		0,
		0,
		time.FixedZone(
			"UTC+3",
			3*60*60,
		),
	)

	event, err := NewIdentityLifecycleDomainEvent(
		"  11111111-1111-1111-1111-111111111111  ",
		IdentityStatusActive,
		IdentityStatusSuspended,
		occurredAt,
	)
	if err != nil {
		t.Fatalf(
			"NewIdentityLifecycleDomainEvent() returned an error: %v",
			err,
		)
	}

	if event.Type != IdentityDomainEventSuspended {
		t.Fatalf(
			"event type = %q, want %q",
			event.Type,
			IdentityDomainEventSuspended,
		)
	}

	if event.IdentityID !=
		"11111111-1111-1111-1111-111111111111" {
		t.Fatalf(
			"identity ID = %q",
			event.IdentityID,
		)
	}

	if event.PreviousStatus != IdentityStatusActive {
		t.Fatalf(
			"previous status = %q, want %q",
			event.PreviousStatus,
			IdentityStatusActive,
		)
	}

	if event.CurrentStatus != IdentityStatusSuspended {
		t.Fatalf(
			"current status = %q, want %q",
			event.CurrentStatus,
			IdentityStatusSuspended,
		)
	}

	if !event.OccurredAt.Equal(
		occurredAt.UTC(),
	) {
		t.Fatalf(
			"occurred at = %v, want %v",
			event.OccurredAt,
			occurredAt.UTC(),
		)
	}

	if event.OccurredAt.Location() != time.UTC {
		t.Fatalf(
			"occurred at location = %v, want UTC",
			event.OccurredAt.Location(),
		)
	}

	if event.SchemaVersion !=
		IdentityDomainEventSchemaVersion {
		t.Fatalf(
			"schema version = %d, want %d",
			event.SchemaVersion,
			IdentityDomainEventSchemaVersion,
		)
	}
}

func TestNewIdentityLifecycleDomainEventRejectsBlankIdentityID(
	t *testing.T,
) {
	_, err := NewIdentityLifecycleDomainEvent(
		"   ",
		IdentityStatusActive,
		IdentityStatusSuspended,
		time.Now(),
	)

	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf(
			"NewIdentityLifecycleDomainEvent() error = %v, want %v",
			err,
			ErrIdentityNotFound,
		)
	}
}

func TestNewIdentityLifecycleDomainEventRejectsInvalidPreviousStatus(
	t *testing.T,
) {
	_, err := NewIdentityLifecycleDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		IdentityStatus("deleted"),
		IdentityStatusSuspended,
		time.Now(),
	)

	if !errors.Is(err, ErrInvalidIdentityStatus) {
		t.Fatalf(
			"NewIdentityLifecycleDomainEvent() error = %v, want %v",
			err,
			ErrInvalidIdentityStatus,
		)
	}
}

func TestNewIdentityLifecycleDomainEventRejectsInvalidCurrentStatus(
	t *testing.T,
) {
	_, err := NewIdentityLifecycleDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		IdentityStatusActive,
		IdentityStatus("deleted"),
		time.Now(),
	)

	if !errors.Is(err, ErrInvalidIdentityStatus) {
		t.Fatalf(
			"NewIdentityLifecycleDomainEvent() error = %v, want %v",
			err,
			ErrInvalidIdentityStatus,
		)
	}
}

func TestNewIdentityLifecycleDomainEventRejectsUnchangedStatus(
	t *testing.T,
) {
	_, err := NewIdentityLifecycleDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		IdentityStatusSuspended,
		IdentityStatusSuspended,
		time.Now(),
	)

	if !errors.Is(
		err,
		ErrIdentityLifecycleEventUnchanged,
	) {
		t.Fatalf(
			"NewIdentityLifecycleDomainEvent() error = %v, want %v",
			err,
			ErrIdentityLifecycleEventUnchanged,
		)
	}
}

func TestNewIdentityLifecycleDomainEventRejectsZeroOccurrenceTime(
	t *testing.T,
) {
	_, err := NewIdentityLifecycleDomainEvent(
		"11111111-1111-1111-1111-111111111111",
		IdentityStatusActive,
		IdentityStatusSuspended,
		time.Time{},
	)

	if err == nil {
		t.Fatal(
			"NewIdentityLifecycleDomainEvent() did not reject zero occurrence time",
		)
	}
}

func TestIdentityDomainEventSchemaVersionIsPositive(
	t *testing.T,
) {
	if IdentityDomainEventSchemaVersion <= 0 {
		t.Fatalf(
			"identity domain event schema version = %d, want positive value",
			IdentityDomainEventSchemaVersion,
		)
	}
}
