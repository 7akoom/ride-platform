package support

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Permissions this service asks staff-service about.
const (
	PermissionRead      = "support.read"
	PermissionReply     = "support.reply"
	PermissionManage    = "support.manage"
	PermissionRefund    = "support.refund"
	PermissionApprove   = "support.approve"
	PermissionSuspend   = "support.suspend"
	PermissionSafety    = "support.safety"
	PermissionConfigure = "support.configure"
)

type StaffTicketView struct {
	Ticket   Ticket
	Messages []Message
	Actions  []Action
}

// staffTicket reads a ticket for a staff member already allowed the
// method's permission; a safety ticket also needs support.safety.
func (s *Service) staffTicket(ctx context.Context, staff Staff, ticketID, method string) (Ticket, error) {
	if !validID(ticketID) {
		return Ticket{}, ErrNotFound
	}

	ticket, err := s.repository.GetTicket(ctx, ticketID)
	if err != nil {
		return Ticket{}, err
	}

	if err := s.requireSafety(ctx, staff, ticket, method); err != nil {
		return Ticket{}, err
	}

	return ticket, nil
}

func (s *Service) requireSafety(ctx context.Context, staff Staff, ticket Ticket, method string) error {
	if !ticket.Safety {
		return nil
	}

	allowed, err := s.staff.Check(ctx, staff.IdentityID, PermissionSafety, method, ticket.ID)
	if err != nil {
		return err
	}

	if !allowed {
		return ErrPermissionDenied
	}

	return nil
}

func (s *Service) staffView(ctx context.Context, ticket Ticket) (StaffTicketView, error) {
	messages, err := s.repository.ListMessages(ctx, ticket.ID, true)
	if err != nil {
		return StaffTicketView{}, err
	}

	actions, err := s.repository.ListActions(ctx, ticket.ID)
	if err != nil {
		return StaffTicketView{}, err
	}

	return StaffTicketView{Ticket: ticket, Messages: messages, Actions: actions}, nil
}

func (s *Service) ListQueue(ctx context.Context, query QueueQuery) ([]Ticket, string, error) {
	if query.Status != "" && !query.Status.Valid() {
		return nil, "", fmt.Errorf("%w: unknown status", ErrInvalidInput)
	}

	if query.Priority != "" && !query.Priority.Valid() {
		return nil, "", fmt.Errorf("%w: unknown priority", ErrInvalidInput)
	}

	if query.Audience != "" && !query.Audience.Valid() {
		return nil, "", fmt.Errorf("%w: unknown audience", ErrInvalidInput)
	}

	if query.AssignedStaffID != "" && !validID(query.AssignedStaffID) {
		return nil, "", fmt.Errorf("%w: assigned_staff_id is a uuid", ErrInvalidInput)
	}

	return s.repository.ListQueue(ctx, query)
}

func (s *Service) GetTicketForStaff(ctx context.Context, staff Staff, ticketID, method string) (StaffTicketView, error) {
	ticket, err := s.staffTicket(ctx, staff, ticketID, method)
	if err != nil {
		return StaffTicketView{}, err
	}

	return s.staffView(ctx, ticket)
}

func (s *Service) ClaimTicket(ctx context.Context, staff Staff, ticketID, method string) (StaffTicketView, error) {
	if _, err := s.staffTicket(ctx, staff, ticketID, method); err != nil {
		return StaffTicketView{}, err
	}

	updated, err := s.repository.UpdateTicket(ctx, ticketID, func(t *Ticket) (Change, error) {
		if t.Status == StatusClosed {
			return Change{}, ErrTicketClosed
		}

		if t.AssignedStaffID == staff.StaffID {
			return Change{}, nil
		}

		if t.AssignedStaffID != "" {
			return Change{}, ErrAlreadyAssigned
		}

		now := s.clock.Now()
		t.AssignedStaffID = staff.StaffID
		t.UpdatedAt = now

		if t.Status == StatusOpen {
			s.moveTo(t, StatusInProgress, now)
		}

		return Change{Messages: []Message{s.systemMessage(t.ID, "Claimed by staff "+staff.StaffID)}}, nil
	})
	if err != nil {
		return StaffTicketView{}, err
	}

	return s.staffView(ctx, updated)
}

func (s *Service) AssignTicket(ctx context.Context, staff Staff, ticketID, assigneeStaffID, method string) (StaffTicketView, error) {
	assigneeStaffID = strings.TrimSpace(assigneeStaffID)
	if assigneeStaffID != "" && !validID(assigneeStaffID) {
		return StaffTicketView{}, fmt.Errorf("%w: staff_id is a uuid", ErrInvalidInput)
	}

	if _, err := s.staffTicket(ctx, staff, ticketID, method); err != nil {
		return StaffTicketView{}, err
	}

	updated, err := s.repository.UpdateTicket(ctx, ticketID, func(t *Ticket) (Change, error) {
		if t.Status == StatusClosed {
			return Change{}, ErrTicketClosed
		}

		if t.AssignedStaffID == assigneeStaffID {
			return Change{}, nil
		}

		now := s.clock.Now()
		t.AssignedStaffID = assigneeStaffID
		t.UpdatedAt = now

		note := "Assigned to staff " + assigneeStaffID + " by " + staff.StaffID

		switch {
		case assigneeStaffID == "":
			note = "Returned to the queue by staff " + staff.StaffID
			if t.Status == StatusInProgress {
				s.moveTo(t, StatusOpen, now)
			}
		case t.Status == StatusOpen:
			s.moveTo(t, StatusInProgress, now)
		}

		return Change{Messages: []Message{s.systemMessage(t.ID, note)}}, nil
	})
	if err != nil {
		return StaffTicketView{}, err
	}

	return s.staffView(ctx, updated)
}

type ReplyInput struct {
	TicketID      string
	Body          string
	Internal      bool
	AttachmentIDs []string
	SetStatus     Status
}

// Reply answers the person (and the driver of a lost-item ticket), or
// writes an internal note.
func (s *Service) Reply(ctx context.Context, staff Staff, input ReplyInput, method string) (StaffTicketView, error) {
	body, err := cleanBody(input.Body)
	if err != nil {
		return StaffTicketView{}, err
	}

	attachments, err := cleanAttachmentIDs(input.AttachmentIDs)
	if err != nil {
		return StaffTicketView{}, err
	}

	if input.SetStatus != "" && (!input.SetStatus.Valid() || input.SetStatus == StatusOpen) {
		return StaffTicketView{}, fmt.Errorf("%w: a reply sets in_progress, waiting_user, resolved or closed", ErrInvalidInput)
	}

	ticket, err := s.staffTicket(ctx, staff, input.TicketID, method)
	if err != nil {
		return StaffTicketView{}, err
	}

	if ticket.Status == StatusClosed {
		return StaffTicketView{}, ErrTicketClosed
	}

	release, err := s.holdAttachments(ctx, staff.IdentityID, attachments)
	if err != nil {
		return StaffTicketView{}, err
	}

	updated, err := s.repository.UpdateTicket(ctx, ticket.ID, func(t *Ticket) (Change, error) {
		if t.Status == StatusClosed {
			return Change{}, ErrTicketClosed
		}

		now := s.clock.Now()
		t.UpdatedAt = now

		if t.AssignedStaffID == "" {
			t.AssignedStaffID = staff.StaffID
		}

		next := input.SetStatus
		if next == "" && !input.Internal {
			next = StatusWaitingUser
		}

		change := Change{Messages: []Message{{
			ID:                 s.ids.NewID(),
			TicketID:           t.ID,
			Author:             AuthorStaff,
			AuthorIdentityID:   staff.IdentityID,
			AuthorStaffID:      staff.StaffID,
			Body:               body,
			Internal:           input.Internal,
			AttachmentMediaIDs: attachments,
			CreatedAt:          now,
		}}}

		if !input.Internal {
			t.LastMessageAt = now
			if t.FirstResponseAt == nil {
				t.FirstResponseAt = &now
			}
		}

		if next == "" && t.Status == StatusOpen {
			next = StatusInProgress
		}

		if next != "" {
			s.moveTo(t, next, now)
		}

		if !input.Internal {
			eventType := EventReplyReceived
			if t.Status == StatusResolved {
				eventType = EventTicketResolved
			}

			change.Events = append(change.Events, recipientEvent(eventType, *t, t.Audience, t.RequesterProfileID))

			if t.ParticipantDriverID != "" {
				change.Events = append(change.Events, recipientEvent(EventReplyReceived, *t, AudienceDriver, t.ParticipantDriverID))
			}
		}

		return change, nil
	})
	if err != nil {
		release()

		return StaffTicketView{}, err
	}

	return s.staffView(ctx, updated)
}

// moveTo sets a status and the times that go with it.
func (s *Service) moveTo(t *Ticket, next Status, now time.Time) {
	if t.Status == next {
		return
	}

	t.Status = next
	t.StatusChangedAt = now

	switch next {
	case StatusResolved:
		at := now
		t.ResolvedAt = &at
	case StatusClosed:
		at := now
		t.ClosedAt = &at
	default:
		t.ResolvedAt = nil
	}
}

func (s *Service) SetStatus(ctx context.Context, staff Staff, ticketID string, next Status, method string) (StaffTicketView, error) {
	if !next.Valid() || next == StatusOpen {
		return StaffTicketView{}, fmt.Errorf("%w: status is in_progress, waiting_user, resolved or closed", ErrInvalidInput)
	}

	if _, err := s.staffTicket(ctx, staff, ticketID, method); err != nil {
		return StaffTicketView{}, err
	}

	updated, err := s.repository.UpdateTicket(ctx, ticketID, func(t *Ticket) (Change, error) {
		if t.Status == StatusClosed {
			return Change{}, ErrTicketClosed
		}

		if t.Status == next {
			return Change{}, nil
		}

		now := s.clock.Now()
		t.UpdatedAt = now
		s.moveTo(t, next, now)

		if next == StatusInProgress && t.AssignedStaffID == "" {
			t.AssignedStaffID = staff.StaffID
		}

		change := Change{Messages: []Message{s.systemMessage(t.ID, fmt.Sprintf("Status set to %s by staff %s", next, staff.StaffID))}}

		if next == StatusResolved {
			change.Events = append(change.Events, recipientEvent(EventTicketResolved, *t, t.Audience, t.RequesterProfileID))
		}

		return change, nil
	})
	if err != nil {
		return StaffTicketView{}, err
	}

	return s.staffView(ctx, updated)
}

func (s *Service) SetPriority(ctx context.Context, staff Staff, ticketID string, priority Priority, method string) (StaffTicketView, error) {
	if !priority.Valid() {
		return StaffTicketView{}, fmt.Errorf("%w: unknown priority", ErrInvalidInput)
	}

	if _, err := s.staffTicket(ctx, staff, ticketID, method); err != nil {
		return StaffTicketView{}, err
	}

	updated, err := s.repository.UpdateTicket(ctx, ticketID, func(t *Ticket) (Change, error) {
		if t.Safety && priority != PriorityUrgent {
			return Change{}, ErrSafetyPriority
		}

		if t.Priority == priority {
			return Change{}, nil
		}

		t.Priority = priority
		t.FirstResponseDueAt = s.firstResponseDue(t.CreatedAt, priority)
		t.UpdatedAt = s.clock.Now()

		return Change{Messages: []Message{s.systemMessage(t.ID, fmt.Sprintf("Priority set to %s by staff %s", priority, staff.StaffID))}}, nil
	})
	if err != nil {
		return StaffTicketView{}, err
	}

	return s.staffView(ctx, updated)
}

func (s *Service) AdminListCategories(ctx context.Context) ([]Category, error) {
	return s.repository.ListCategories(ctx, true)
}

func (s *Service) UpsertCategory(ctx context.Context, category Category) (Category, error) {
	category.Key = strings.TrimSpace(category.Key)
	category.NameEn = strings.TrimSpace(category.NameEn)
	category.NameAr = strings.TrimSpace(category.NameAr)
	category.NameKu = strings.TrimSpace(category.NameKu)

	switch {
	case !validCategoryKey(category.Key):
		return Category{}, fmt.Errorf("%w: a key is 2-40 lowercase letters, digits and underscores", ErrInvalidInput)
	case category.Key == CategorySOS:
		return Category{}, fmt.Errorf("%w: the sos category cannot change", ErrInvalidInput)
	case !category.Audience.Valid():
		return Category{}, fmt.Errorf("%w: audience is rider, driver or both", ErrInvalidInput)
	case !validName(category.NameEn) || !validName(category.NameAr) || !validName(category.NameKu):
		return Category{}, fmt.Errorf("%w: names in en, ar and ku are 1-80 characters", ErrInvalidInput)
	case !category.DefaultPriority.Valid():
		return Category{}, fmt.Errorf("%w: unknown priority", ErrInvalidInput)
	case category.LostItem && !category.RequiresTrip:
		return Category{}, fmt.Errorf("%w: a lost-item category needs a trip", ErrInvalidInput)
	case category.LostItem && category.Audience != CategoryRider:
		return Category{}, fmt.Errorf("%w: lost items are reported by riders", ErrInvalidInput)
	case category.SortOrder < 0 || category.SortOrder > 100000:
		return Category{}, fmt.Errorf("%w: sort_order is 0-100000", ErrInvalidInput)
	}

	if category.Safety {
		category.DefaultPriority = PriorityUrgent
	}

	return s.repository.UpsertCategory(ctx, category)
}

func validCategoryKey(key string) bool {
	if len(key) < 2 || len(key) > 40 || key[0] < 'a' || key[0] > 'z' {
		return false
	}

	for _, r := range key {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' {
			return false
		}
	}

	return true
}

func validName(name string) bool {
	n := len([]rune(name))

	return n >= 1 && n <= 80
}
