// Package erasure erases what this service keeps about a person whose account
// is deleted. identity-service publishes identity.deletion_requested when the
// person confirms (the grace period starts; nothing is erased yet) and
// identity.deleted once the grace period is over and it erased its own data.
// Trips, money movements and tickets stay, without pointing at anyone.
package erasure

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"time"
)

// Subjects identity-service publishes on its IDENTITY_EVENTS stream.
const (
	SubjectDeletionRequested = "identity.deletion_requested"
	SubjectDeleted           = "identity.deleted"
)

// Subjects is what this service's durable consumer listens to.
var Subjects = []string{SubjectDeletionRequested, SubjectDeleted}

// retryDelay is how long a failed erasure waits before it is delivered again.
const retryDelay = 10 * time.Second

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Account is whose data to erase: the identity and its profiles (empty when
// the person had none).
type Account struct {
	IdentityID string
	RiderID    string
	DriverID   string
}

// Eraser is what this service does with the two events. Both must be safe to
// run more than once.
type Eraser interface {
	// Requested: the account stopped (sessions ended) and waits for its grace
	// period.
	Requested(ctx context.Context, account Account) error
	// Erase: the grace period is over; erase the personal data.
	Erase(ctx context.Context, account Account) error
}

type retryLaterError struct{ cause error }

func (e *retryLaterError) Error() string { return fmt.Sprintf("retry in %s: %v", retryDelay, e.cause) }

func (e *retryLaterError) Unwrap() error { return e.cause }

func (e *retryLaterError) RetryDelay() time.Duration { return retryDelay }

type envelope struct {
	EventID string          `json:"event_id"`
	Payload json.RawMessage `json:"payload"`
}

type payload struct {
	IdentityID string `json:"identity_id"`
	RiderID    string `json:"rider_id"`
	DriverID   string `json:"driver_id"`
}

// Handler turns the events into calls to the Eraser.
type Handler struct {
	eraser Eraser
	logger *slog.Logger
}

func NewHandler(eraser Eraser, logger *slog.Logger) *Handler {
	if eraser == nil || logger == nil {
		panic("erasure handler dependencies are required")
	}

	return &Handler{eraser: eraser, logger: logger}
}

// Handle is a messaging handler: nil acks, an error redelivers later. A
// malformed event is logged and dropped (redelivering it cannot fix it).
func (h *Handler) Handle(ctx context.Context, subject string, data []byte) error {
	var (
		message envelope
		body    payload
	)

	if err := json.Unmarshal(data, &message); err != nil {
		h.logger.WarnContext(ctx, "dropping an identity event that is not an envelope", "subject", subject, "error", err)

		return nil
	}

	if err := json.Unmarshal(message.Payload, &body); err != nil {
		h.logger.WarnContext(ctx, "dropping an identity event with a bad payload", "subject", subject, "error", err)

		return nil
	}

	account := Account{IdentityID: body.IdentityID, RiderID: body.RiderID, DriverID: body.DriverID}

	if !uuidShape.MatchString(account.IdentityID) ||
		(account.RiderID != "" && !uuidShape.MatchString(account.RiderID)) ||
		(account.DriverID != "" && !uuidShape.MatchString(account.DriverID)) {
		h.logger.WarnContext(ctx, "dropping an identity event with bad ids", "subject", subject, "event_id", message.EventID)

		return nil
	}

	var err error

	switch subject {
	case SubjectDeletionRequested:
		err = h.eraser.Requested(ctx, account)
	case SubjectDeleted:
		err = h.eraser.Erase(ctx, account)
	default:
		return nil
	}

	if err != nil {
		return &retryLaterError{cause: err}
	}

	if subject == SubjectDeleted {
		h.logger.InfoContext(ctx, "erased a deleted account's data", "identity_id", account.IdentityID)
	}

	return nil
}
