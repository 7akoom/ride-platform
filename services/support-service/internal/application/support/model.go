package support

import (
	"errors"
	"time"

	"github.com/shopspring/decimal"
)

type Audience string

const (
	AudienceRider  Audience = "rider"
	AudienceDriver Audience = "driver"
)

func (a Audience) Valid() bool { return a == AudienceRider || a == AudienceDriver }

// Other is the other side of a trip.
func (a Audience) Other() Audience {
	if a == AudienceRider {
		return AudienceDriver
	}

	return AudienceRider
}

type CategoryAudience string

const (
	CategoryRider  CategoryAudience = "rider"
	CategoryDriver CategoryAudience = "driver"
	CategoryBoth   CategoryAudience = "both"
)

func (c CategoryAudience) Valid() bool {
	return c == CategoryRider || c == CategoryDriver || c == CategoryBoth
}

func (c CategoryAudience) Includes(a Audience) bool {
	return c == CategoryBoth || string(c) == string(a)
}

type Status string

const (
	StatusOpen        Status = "open"
	StatusInProgress  Status = "in_progress"
	StatusWaitingUser Status = "waiting_user"
	StatusResolved    Status = "resolved"
	StatusClosed      Status = "closed"
)

func (s Status) Valid() bool {
	switch s {
	case StatusOpen, StatusInProgress, StatusWaitingUser, StatusResolved, StatusClosed:
		return true
	}

	return false
}

// Active: still waiting for someone.
func (s Status) Active() bool { return s != StatusResolved && s != StatusClosed }

type Priority string

const (
	PriorityLow    Priority = "low"
	PriorityNormal Priority = "normal"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

func (p Priority) Valid() bool {
	switch p {
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent:
		return true
	}

	return false
}

const (
	SourceApp = "app"
	SourceSOS = "sos"
)

// CategorySOS is the reserved category of tickets opened by an SOS; nobody
// picks it.
const CategorySOS = "sos"

type Author string

const (
	AuthorRequester   Author = "requester"
	AuthorParticipant Author = "participant"
	AuthorStaff       Author = "staff"
	AuthorSystem      Author = "system"
)

type Role string

const (
	RoleRequester   Role = "requester"
	RoleParticipant Role = "participant"
)

type Category struct {
	Key             string
	Audience        CategoryAudience
	NameEn          string
	NameAr          string
	NameKu          string
	DefaultPriority Priority
	RequiresTrip    bool
	Safety          bool
	LostItem        bool
	Active          bool
	SortOrder       int
}

type Ticket struct {
	ID                   string
	Number               int64
	RequesterIdentityID  string
	Audience             Audience
	RequesterProfileID   string
	CategoryKey          string
	Subject              string
	Status               Status
	Priority             Priority
	Safety               bool
	Source               string
	TripID               string
	TransactionID        string
	CounterpartProfileID string
	ParticipantDriverID  string
	SOSAlertID           string
	AssignedStaffID      string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	LastMessageAt        time.Time
	FirstResponseAt      *time.Time
	ResolvedAt           *time.Time
	ClosedAt             *time.Time
	FirstResponseDueAt   time.Time
	StatusChangedAt      time.Time
	Rating               int
	RatingComment        string
	RatedAt              *time.Time
}

// FirstResponseLate: answered after the target, or not answered and past it.
func (t Ticket) FirstResponseLate(now time.Time) bool {
	if t.FirstResponseAt != nil {
		return t.FirstResponseAt.After(t.FirstResponseDueAt)
	}

	return t.Status.Active() && now.After(t.FirstResponseDueAt)
}

type Message struct {
	ID                 string
	TicketID           string
	Author             Author
	AuthorIdentityID   string
	AuthorStaffID      string
	Body               string
	Internal           bool
	AttachmentMediaIDs []string
	CreatedAt          time.Time
}

type ActionKind string

const (
	ActionRefund       ActionKind = "refund"
	ActionWaiveFee     ActionKind = "waive_fee"
	ActionCompensation ActionKind = "compensation"
	ActionSuspend      ActionKind = "suspend"
	ActionReactivate   ActionKind = "reactivate"
)

func (k ActionKind) Money() bool {
	return k == ActionRefund || k == ActionWaiveFee || k == ActionCompensation
}

func (k ActionKind) Account() bool { return k == ActionSuspend || k == ActionReactivate }

type ActionStatus string

const (
	ActionPendingApproval ActionStatus = "pending_approval"
	ActionProcessing      ActionStatus = "processing"
	ActionCompleted       ActionStatus = "completed"
	ActionRejected        ActionStatus = "rejected"
	ActionFailed          ActionStatus = "failed"
)

type ActionTarget string

const (
	TargetRequester   ActionTarget = "requester"
	TargetCounterpart ActionTarget = "counterpart"
)

type Action struct {
	ID                    string
	TicketID              string
	Kind                  ActionKind
	Status                ActionStatus
	Amount                *decimal.Decimal
	DriverAmount          *decimal.Decimal
	Target                ActionTarget
	TargetType            Audience
	TargetProfileID       string
	TargetIdentityID      string
	SuspendUntil          *time.Time
	ReactivatedAt         *time.Time
	Reason                string
	RequestedByStaffID    string
	RequestedByIdentityID string
	DecidedByStaffID      string
	DecidedByIdentityID   string
	DecisionReason        string
	FailureReason         string
	ActingIdentityID      string
	LeaseUntil            *time.Time
	CreatedAt             time.Time
	DecidedAt             *time.Time
	CompletedAt           *time.Time
}

// Event is a domain event written to the outbox with the change it describes.
type Event struct {
	Type    string
	Payload any
}

const (
	EventTicketCreated    = "support.ticket_created"
	EventReplyReceived    = "support.reply_received"
	EventTicketResolved   = "support.ticket_resolved"
	EventLostItemReported = "support.lost_item_reported"
)

// Staff is a staff member allowed to act, as staff-service answered.
type Staff struct {
	StaffID    string
	IdentityID string
}

// Caller is a signed-in person.
type Caller struct {
	IdentityID string
}

var (
	ErrNotFound            = errors.New("ticket not found")
	ErrActionNotFound      = errors.New("action not found")
	ErrCategoryNotFound    = errors.New("category not found")
	ErrInvalidInput        = errors.New("invalid input")
	ErrNoProfile           = errors.New("the caller has no profile for this audience")
	ErrTripNotYours        = errors.New("the trip is not one of yours")
	ErrTripTooOld          = errors.New("the trip is too old for a ticket")
	ErrTripRequired        = errors.New("this category needs a trip")
	ErrTripNotCompleted    = errors.New("a lost item needs a completed trip with a driver")
	ErrTooManyOpen         = errors.New("too many open tickets")
	ErrTicketClosed        = errors.New("the ticket is closed")
	ErrAlreadyAssigned     = errors.New("the ticket is assigned to someone else")
	ErrMediaNotUsable      = errors.New("an attachment is not a ready support attachment of yours")
	ErrAttachmentInUse     = errors.New("an attachment is already on a ticket")
	ErrSafetyPriority      = errors.New("a safety ticket stays urgent")
	ErrActionNotAllowed    = errors.New("this action does not fit the ticket")
	ErrNothingToWaive      = errors.New("nothing is left of the fee to waive")
	ErrActionNotPending    = errors.New("the action is not waiting for approval")
	ErrOwnApproval         = errors.New("an action is approved by someone other than who asked for it")
	ErrPermissionDenied    = errors.New("permission denied")
	ErrUpstreamUnavailable = errors.New("a service the support desk needs did not answer")
	ErrArticleNotFound     = errors.New("help article not found")
	ErrNotRatable          = errors.New("only a resolved or closed ticket is rated, once, within 7 days")
)
