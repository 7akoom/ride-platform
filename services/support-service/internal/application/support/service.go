package support

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	maxBodyLength       = 4000
	maxSubjectLength    = 120
	minReasonLength     = 3
	maxReasonLength     = 300
	maxAttachments      = 5
	actionLease         = time.Minute
	minSuspension       = time.Hour
	maxSuspension       = 90 * 24 * time.Hour
	maxFailureReasonLen = 300
)

// Config holds the desk's limits.
type Config struct {
	// Money actions one ticket may carry out without a second staff member.
	RefundLimit decimal.Decimal
	// Open tickets one person may have per audience.
	MaxOpenTickets int
	// How old a trip may be for a ticket about it.
	TripMaxAge time.Duration
	// First-response targets by priority (SLA). Missing ones use the defaults.
	FirstResponse map[Priority]time.Duration
	// A ticket waiting for the person this long is resolved; a resolved one
	// this long is closed. Zero: never.
	AutoResolveAfter time.Duration
	AutoCloseAfter   time.Duration
}

// DefaultFirstResponse are the first-response targets unless configured.
var DefaultFirstResponse = map[Priority]time.Duration{
	PriorityUrgent: 15 * time.Minute,
	PriorityHigh:   time.Hour,
	PriorityNormal: 4 * time.Hour,
	PriorityLow:    24 * time.Hour,
}

func (s *Service) firstResponseDue(created time.Time, priority Priority) time.Time {
	if d, ok := s.config.FirstResponse[priority]; ok && d > 0 {
		return created.Add(d)
	}

	return created.Add(DefaultFirstResponse[priority])
}

type Service struct {
	repository Repository
	trips      Trips
	profiles   Profiles
	media      Media
	wallet     Wallet
	accounts   Accounts
	staff      StaffGate
	ids        IDGenerator
	clock      Clock
	config     Config
	logger     *slog.Logger
}

type Dependencies struct {
	Repository Repository
	Trips      Trips
	Profiles   Profiles
	Media      Media
	Wallet     Wallet
	Accounts   Accounts
	Staff      StaffGate
	IDs        IDGenerator
	Clock      Clock
}

func NewService(deps Dependencies, config Config, logger *slog.Logger) *Service {
	switch {
	case deps.Repository == nil:
		panic("support repository is required")
	case deps.Trips == nil, deps.Profiles == nil, deps.Media == nil,
		deps.Wallet == nil, deps.Accounts == nil, deps.Staff == nil:
		panic("support service clients are required")
	case deps.IDs == nil || deps.Clock == nil:
		panic("id generator and clock are required")
	case logger == nil:
		panic("logger is required")
	case config.RefundLimit.IsNegative() || config.MaxOpenTickets <= 0 || config.TripMaxAge <= 0:
		panic("invalid support configuration")
	}

	return &Service{
		repository: deps.Repository,
		trips:      deps.Trips,
		profiles:   deps.Profiles,
		media:      deps.Media,
		wallet:     deps.Wallet,
		accounts:   deps.Accounts,
		staff:      deps.Staff,
		ids:        deps.IDs,
		clock:      deps.Clock,
		config:     config,
		logger:     logger,
	}
}

// TicketNumber is the human reference of a ticket.
func TicketNumber(number int64) string {
	return fmt.Sprintf("S-%06d", number)
}

func validID(value string) bool {
	_, err := uuid.Parse(value)

	return err == nil && len(value) == 36
}

func cleanBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > maxBodyLength || !utf8.ValidString(body) {
		return "", fmt.Errorf("%w: a message is 1-%d characters", ErrInvalidInput, maxBodyLength)
	}

	return body, nil
}

func cleanReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	n := utf8.RuneCountInString(reason)

	if n < minReasonLength || n > maxReasonLength {
		return "", fmt.Errorf("%w: a reason is %d-%d characters", ErrInvalidInput, minReasonLength, maxReasonLength)
	}

	return reason, nil
}

func cleanAttachmentIDs(ids []string) ([]string, error) {
	if len(ids) > maxAttachments {
		return nil, fmt.Errorf("%w: at most %d attachments", ErrInvalidInput, maxAttachments)
	}

	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))

	for _, id := range ids {
		id = strings.TrimSpace(id)
		if !validID(id) {
			return nil, fmt.Errorf("%w: attachment ids are uuids", ErrInvalidInput)
		}

		if _, dup := seen[id]; dup {
			continue
		}

		seen[id] = struct{}{}
		out = append(out, id)
	}

	return out, nil
}

// holdAttachments keeps the files for the ticket; the returned function
// lets them go again when the ticket change does not happen.
func (s *Service) holdAttachments(ctx context.Context, ownerIdentityID string, ids []string) (func(), error) {
	held := make([]string, 0, len(ids))

	release := func() {
		for _, id := range held {
			if err := s.media.Release(context.WithoutCancel(ctx), id); err != nil {
				s.logger.WarnContext(ctx, "failed to release a support attachment", "media_id", id, "error", err)
			}
		}
	}

	for _, id := range ids {
		if err := s.media.Hold(ctx, id, ownerIdentityID); err != nil {
			release()

			return func() {}, err
		}

		held = append(held, id)
	}

	return release, nil
}

func (s *Service) systemMessage(ticketID, body string) Message {
	return Message{
		ID:        s.ids.NewID(),
		TicketID:  ticketID,
		Author:    AuthorSystem,
		Body:      body,
		Internal:  true,
		CreatedAt: s.clock.Now(),
	}
}

func recipientEvent(eventType string, ticket Ticket, recipientType Audience, recipientID string) Event {
	return Event{
		Type: eventType,
		Payload: map[string]any{
			"ticket_id":      ticket.ID,
			"ticket_number":  TicketNumber(ticket.Number),
			"recipient_type": string(recipientType),
			"recipient_id":   recipientID,
		},
	}
}

func isUpstream(err error) bool {
	return errors.Is(err, ErrUpstreamUnavailable)
}

func truncate(value string, max int) string {
	if utf8.RuneCountInString(value) <= max {
		return value
	}

	return string([]rune(value)[:max])
}
