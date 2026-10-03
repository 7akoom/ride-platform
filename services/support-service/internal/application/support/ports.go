package support

import (
	"context"
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

// Change is what a ticket mutation adds besides the ticket's own fields.
type Change struct {
	Messages []Message
	Events   []Event
}

type MyTicketsQuery struct {
	IdentityID string
	Audience   Audience
	// The caller's driver profile, so lost-item tickets about their trips
	// show too; empty for riders.
	ParticipantDriverID string
	OpenOnly            bool
	Limit               int
	Cursor              string
}

type QueueQuery struct {
	Safety          bool
	Status          Status
	CategoryKey     string
	Priority        Priority
	AssignedStaffID string
	UnassignedOnly  bool
	Audience        Audience
	Limit           int
	Cursor          string
}

type NewTicket struct {
	Ticket  Ticket
	Message Message
	// BuildEvents makes the events once the ticket has its number.
	BuildEvents func(ticket Ticket) []Event
	// Open-ticket limit for the requester (0: none).
	MaxOpen int
}

type Repository interface {
	ListCategories(ctx context.Context, includeInactive bool) ([]Category, error)
	GetCategory(ctx context.Context, key string) (Category, error)
	UpsertCategory(ctx context.Context, category Category) (Category, error)

	// CreateTicket inserts the ticket, its first message and events. It
	// returns ErrTooManyOpen past MaxOpen, and ErrAttachmentInUse.
	CreateTicket(ctx context.Context, input NewTicket) (Ticket, error)
	// FindOpenDuplicate finds an active ticket of the requester in the same
	// category about the same trip.
	FindOpenDuplicate(ctx context.Context, identityID string, audience Audience, categoryKey, tripID string) (Ticket, bool, error)
	FindBySOSAlert(ctx context.Context, alertID string) (Ticket, bool, error)
	GetTicket(ctx context.Context, id string) (Ticket, error)
	ListMessages(ctx context.Context, ticketID string, includeInternal bool) ([]Message, error)
	TicketHasAttachment(ctx context.Context, ticketID, mediaID string) (internal bool, found bool, err error)
	ListMyTickets(ctx context.Context, query MyTicketsQuery) ([]Ticket, string, error)
	ListQueue(ctx context.Context, query QueueQuery) ([]Ticket, string, error)
	// UpdateTicket locks the ticket, lets apply change it and saves the
	// ticket with what apply adds, in one transaction. apply's error aborts.
	UpdateTicket(ctx context.Context, ticketID string, apply func(ticket *Ticket) (Change, error)) (Ticket, error)

	// CreateAction locks the ticket and inserts the action decide returns,
	// with its system message. decide sees the ticket's money actions so far.
	CreateAction(ctx context.Context, ticketID string, decide func(ticket Ticket, moneySoFar decimal.Decimal) (Action, Message, error)) (Action, error)
	GetAction(ctx context.Context, id string) (Action, error)
	ListActions(ctx context.Context, ticketID string) ([]Action, error)
	ListPendingActions(ctx context.Context, limit int, cursor string) ([]Action, string, error)
	// DecideAction locks the action, lets apply change it and saves it with
	// the system message apply returns.
	DecideAction(ctx context.Context, actionID string, apply func(action *Action) (*Message, error)) (Action, error)
	// FinishAction records how a processing action ended, with a message.
	FinishAction(ctx context.Context, actionID string, status ActionStatus, failureReason string, at time.Time, message Message) (Action, error)
	// ClaimProcessingActions leases processing actions whose lease ran out.
	ClaimProcessingActions(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Action, error)
	// ClaimDueSuspensions leases completed suspensions whose time is up.
	ClaimDueSuspensions(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Action, error)
	// MarkReactivated records a suspension lifted, with a message on its
	// ticket. Every completed suspension of that identity counts as lifted.
	MarkReactivated(ctx context.Context, identityID string, at time.Time, message *Message) error
}

type TripInfo struct {
	ID          string
	RiderID     string
	DriverID    string
	Status      string // requested, accepted, in_progress, completed, cancelled
	RequestedAt time.Time
}

type Trips interface {
	GetTrip(ctx context.Context, tripID string) (TripInfo, error)
}

// Profiles finds rider and driver profiles (as a service).
type Profiles interface {
	// ProfileIDByIdentity returns the identity's profile id for the
	// audience, ErrNoProfile if it has none.
	ProfileIDByIdentity(ctx context.Context, audience Audience, identityID string) (string, error)
	IdentityByProfile(ctx context.Context, audience Audience, profileID string) (string, error)
	SetDriverOffline(ctx context.Context, driverID string) error
}

type Media interface {
	// Hold keeps a READY support attachment of the identity; ErrMediaNotUsable otherwise.
	Hold(ctx context.Context, mediaID, ownerIdentityID string) error
	Release(ctx context.Context, mediaID string) error
	DownloadURL(ctx context.Context, mediaID string) (string, time.Time, error)
}

// ErrRefused wraps a definite refusal by another service: the action
// failed and will not be retried.
var ErrRefused = errors.New("refused")

type Wallet interface {
	RefundTrip(ctx context.Context, actingIdentityID, tripID string, amount, driverAmount decimal.Decimal, reason, idempotencyKey string) error
	Credit(ctx context.Context, actingIdentityID string, ownerType Audience, ownerID string, amount decimal.Decimal, reason, idempotencyKey string) error
	// Refundable is what a trip's fare (or fee) still allows to refund.
	Refundable(ctx context.Context, tripID string) (decimal.Decimal, error)
}

type Accounts interface {
	Suspend(ctx context.Context, identityID string) error
	Reactivate(ctx context.Context, identityID string) error
}

// StaffGate asks staff-service whether a staff member may use a
// permission (it records the attempt). allowed false with no error is a
// definite no.
type StaffGate interface {
	Check(ctx context.Context, identityID, permission, method, targetID string) (allowed bool, err error)
}

type IDGenerator interface{ NewID() string }

type Clock interface{ Now() time.Time }
