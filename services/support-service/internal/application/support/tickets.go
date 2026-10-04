package support

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type CreateTicketInput struct {
	Audience      Audience
	CategoryKey   string
	Subject       string
	Body          string
	TripID        string
	TransactionID string
	AttachmentIDs []string
}

type TicketView struct {
	Ticket   Ticket
	Role     Role
	Messages []Message
	Existing bool
}

// ListCategories lists what riders or drivers may pick.
func (s *Service) ListCategories(ctx context.Context, audience Audience) ([]Category, error) {
	if !audience.Valid() {
		return nil, fmt.Errorf("%w: audience is rider or driver", ErrInvalidInput)
	}

	all, err := s.repository.ListCategories(ctx, false)
	if err != nil {
		return nil, err
	}

	out := make([]Category, 0, len(all))

	for _, category := range all {
		if category.Key != CategorySOS && category.Audience.Includes(audience) {
			out = append(out, category)
		}
	}

	return out, nil
}

func (s *Service) CreateTicket(ctx context.Context, caller Caller, input CreateTicketInput) (TicketView, error) {
	if !input.Audience.Valid() {
		return TicketView{}, fmt.Errorf("%w: audience is rider or driver", ErrInvalidInput)
	}

	category, err := s.repository.GetCategory(ctx, strings.TrimSpace(input.CategoryKey))
	if errors.Is(err, ErrCategoryNotFound) || (err == nil && (!category.Active || category.Key == CategorySOS ||
		!category.Audience.Includes(input.Audience))) {
		return TicketView{}, fmt.Errorf("%w: unknown category", ErrInvalidInput)
	}

	if err != nil {
		return TicketView{}, err
	}

	subject := strings.TrimSpace(input.Subject)
	if utf8.RuneCountInString(subject) > maxSubjectLength || !utf8.ValidString(subject) {
		return TicketView{}, fmt.Errorf("%w: a subject is at most %d characters", ErrInvalidInput, maxSubjectLength)
	}

	body, err := cleanBody(input.Body)
	if err != nil {
		return TicketView{}, err
	}

	attachments, err := cleanAttachmentIDs(input.AttachmentIDs)
	if err != nil {
		return TicketView{}, err
	}

	transactionID := strings.TrimSpace(input.TransactionID)
	if transactionID != "" && !validID(transactionID) {
		return TicketView{}, fmt.Errorf("%w: transaction_id is a uuid", ErrInvalidInput)
	}

	profileID, err := s.profiles.ProfileIDByIdentity(ctx, input.Audience, caller.IdentityID)
	if err != nil {
		return TicketView{}, err
	}

	tripID := strings.TrimSpace(input.TripID)
	if tripID == "" && category.RequiresTrip {
		return TicketView{}, ErrTripRequired
	}

	var trip TripInfo

	if tripID != "" {
		if trip, err = s.ownTrip(ctx, input.Audience, profileID, tripID); err != nil {
			return TicketView{}, err
		}

		if category.LostItem && (trip.Status != "completed" || trip.DriverID == "") {
			return TicketView{}, ErrTripNotCompleted
		}

		existing, found, err := s.repository.FindOpenDuplicate(ctx, caller.IdentityID, input.Audience, category.Key, tripID)
		if err != nil {
			return TicketView{}, err
		}

		if found {
			view, err := s.addMessage(ctx, caller, existing, RoleRequester, body, attachments)
			view.Existing = true

			return view, err
		}
	}

	release, err := s.holdAttachments(ctx, caller.IdentityID, attachments)
	if err != nil {
		return TicketView{}, err
	}

	now := s.clock.Now()
	ticket := Ticket{
		ID:                  s.ids.NewID(),
		RequesterIdentityID: caller.IdentityID,
		Audience:            input.Audience,
		RequesterProfileID:  profileID,
		CategoryKey:         category.Key,
		Subject:             subject,
		Status:              StatusOpen,
		Priority:            category.DefaultPriority,
		Safety:              category.Safety,
		Source:              SourceApp,
		TripID:              tripID,
		TransactionID:       transactionID,
		CreatedAt:           now,
		UpdatedAt:           now,
		LastMessageAt:       now,
	}

	if category.Safety {
		ticket.Priority = PriorityUrgent
	}

	ticket.FirstResponseDueAt = s.firstResponseDue(now, ticket.Priority)
	ticket.StatusChangedAt = now

	if tripID != "" {
		ticket.CounterpartProfileID = counterpartOf(input.Audience, trip)
	}

	if category.LostItem {
		ticket.ParticipantDriverID = trip.DriverID
	}

	message := Message{
		ID:                 s.ids.NewID(),
		TicketID:           ticket.ID,
		Author:             AuthorRequester,
		AuthorIdentityID:   caller.IdentityID,
		Body:               body,
		AttachmentMediaIDs: attachments,
		CreatedAt:          now,
	}

	lostItem := category.LostItem

	created, err := s.repository.CreateTicket(ctx, NewTicket{
		Ticket:  ticket,
		Message: message,
		BuildEvents: func(t Ticket) []Event {
			events := []Event{ticketCreatedEvent(t)}
			if lostItem {
				events = append(events, recipientEvent(EventLostItemReported, t, AudienceDriver, t.ParticipantDriverID))
			}

			return events
		},
		MaxOpen: s.config.MaxOpenTickets,
	})
	if err != nil {
		release()

		return TicketView{}, err
	}

	return TicketView{Ticket: created, Role: RoleRequester, Messages: []Message{message}}, nil
}

// ownTrip reads the trip and checks it is the caller's and recent enough.
func (s *Service) ownTrip(ctx context.Context, audience Audience, profileID, tripID string) (TripInfo, error) {
	if !validID(tripID) {
		return TripInfo{}, fmt.Errorf("%w: trip_id is a uuid", ErrInvalidInput)
	}

	trip, err := s.trips.GetTrip(ctx, tripID)
	if errors.Is(err, ErrNotFound) {
		return TripInfo{}, ErrTripNotYours
	}

	if err != nil {
		return TripInfo{}, err
	}

	mine := (audience == AudienceRider && trip.RiderID == profileID) ||
		(audience == AudienceDriver && trip.DriverID == profileID)
	if !mine {
		return TripInfo{}, ErrTripNotYours
	}

	if s.clock.Now().Sub(trip.RequestedAt) > s.config.TripMaxAge {
		return TripInfo{}, ErrTripTooOld
	}

	return trip, nil
}

func ticketCreatedEvent(t Ticket) Event {
	return Event{Type: EventTicketCreated, Payload: map[string]any{
		"ticket_id":     t.ID,
		"ticket_number": TicketNumber(t.Number),
		"audience":      string(t.Audience),
		"category_key":  t.CategoryKey,
		"priority":      string(t.Priority),
		"safety":        t.Safety,
		"source":        t.Source,
	}}
}

func counterpartOf(audience Audience, trip TripInfo) string {
	if audience == AudienceRider {
		return trip.DriverID
	}

	return trip.RiderID
}

// roleOf says how the caller is in the ticket; ErrNotFound if not at all.
func (s *Service) roleOf(ctx context.Context, caller Caller, ticket Ticket) (Role, error) {
	if ticket.RequesterIdentityID == caller.IdentityID {
		return RoleRequester, nil
	}

	if ticket.ParticipantDriverID == "" {
		return "", ErrNotFound
	}

	driverID, err := s.profiles.ProfileIDByIdentity(ctx, AudienceDriver, caller.IdentityID)
	if errors.Is(err, ErrNoProfile) {
		return "", ErrNotFound
	}

	if err != nil {
		return "", err
	}

	if driverID != ticket.ParticipantDriverID {
		return "", ErrNotFound
	}

	return RoleParticipant, nil
}

func (s *Service) myTicket(ctx context.Context, caller Caller, ticketID string) (Ticket, Role, error) {
	if !validID(ticketID) {
		return Ticket{}, "", ErrNotFound
	}

	ticket, err := s.repository.GetTicket(ctx, ticketID)
	if err != nil {
		return Ticket{}, "", err
	}

	role, err := s.roleOf(ctx, caller, ticket)
	if err != nil {
		return Ticket{}, "", err
	}

	return ticket, role, nil
}

func (s *Service) GetMyTicket(ctx context.Context, caller Caller, ticketID string) (TicketView, error) {
	ticket, role, err := s.myTicket(ctx, caller, ticketID)
	if err != nil {
		return TicketView{}, err
	}

	messages, err := s.repository.ListMessages(ctx, ticket.ID, false)
	if err != nil {
		return TicketView{}, err
	}

	return TicketView{Ticket: ticket, Role: role, Messages: messages}, nil
}

func (s *Service) ListMyTickets(ctx context.Context, caller Caller, query MyTicketsQuery) ([]Ticket, string, error) {
	if !query.Audience.Valid() {
		return nil, "", fmt.Errorf("%w: audience is rider or driver", ErrInvalidInput)
	}

	query.IdentityID = caller.IdentityID
	query.ParticipantDriverID = ""

	if query.Audience == AudienceDriver {
		driverID, err := s.profiles.ProfileIDByIdentity(ctx, AudienceDriver, caller.IdentityID)
		if err != nil && !errors.Is(err, ErrNoProfile) {
			return nil, "", err
		}

		query.ParticipantDriverID = driverID
	}

	return s.repository.ListMyTickets(ctx, query)
}

func (s *Service) AddTicketMessage(ctx context.Context, caller Caller, ticketID, body string, attachmentIDs []string) (TicketView, error) {
	ticket, role, err := s.myTicket(ctx, caller, ticketID)
	if err != nil {
		return TicketView{}, err
	}

	cleaned, err := cleanBody(body)
	if err != nil {
		return TicketView{}, err
	}

	attachments, err := cleanAttachmentIDs(attachmentIDs)
	if err != nil {
		return TicketView{}, err
	}

	return s.addMessage(ctx, caller, ticket, role, cleaned, attachments)
}

// addMessage writes a requester's or participant's message. It reopens a
// resolved or waiting ticket, and tells the other person of a lost-item
// ticket.
func (s *Service) addMessage(ctx context.Context, caller Caller, ticket Ticket, role Role, body string, attachments []string) (TicketView, error) {
	if ticket.Status == StatusClosed {
		return TicketView{}, ErrTicketClosed
	}

	release, err := s.holdAttachments(ctx, caller.IdentityID, attachments)
	if err != nil {
		return TicketView{}, err
	}

	author := AuthorRequester
	if role == RoleParticipant {
		author = AuthorParticipant
	}

	updated, err := s.repository.UpdateTicket(ctx, ticket.ID, func(t *Ticket) (Change, error) {
		if t.Status == StatusClosed {
			return Change{}, ErrTicketClosed
		}

		now := s.clock.Now()

		switch t.Status {
		case StatusResolved, StatusWaitingUser:
			next := StatusOpen
			if t.AssignedStaffID != "" {
				next = StatusInProgress
			}

			s.moveTo(t, next, now)
		}

		t.LastMessageAt = now
		t.UpdatedAt = now

		change := Change{Messages: []Message{{
			ID:                 s.ids.NewID(),
			TicketID:           t.ID,
			Author:             author,
			AuthorIdentityID:   caller.IdentityID,
			Body:               body,
			AttachmentMediaIDs: attachments,
			CreatedAt:          now,
		}}}

		if t.ParticipantDriverID != "" {
			if role == RoleRequester {
				change.Events = append(change.Events, recipientEvent(EventReplyReceived, *t, AudienceDriver, t.ParticipantDriverID))
			} else {
				change.Events = append(change.Events, recipientEvent(EventReplyReceived, *t, t.Audience, t.RequesterProfileID))
			}
		}

		return change, nil
	})
	if err != nil {
		release()

		return TicketView{}, err
	}

	messages, err := s.repository.ListMessages(ctx, updated.ID, false)
	if err != nil {
		return TicketView{}, err
	}

	return TicketView{Ticket: updated, Role: role, Messages: messages}, nil
}

func (s *Service) CloseMyTicket(ctx context.Context, caller Caller, ticketID string) (TicketView, error) {
	ticket, role, err := s.myTicket(ctx, caller, ticketID)
	if err != nil {
		return TicketView{}, err
	}

	if role != RoleRequester {
		return TicketView{}, ErrPermissionDenied
	}

	updated, err := s.repository.UpdateTicket(ctx, ticket.ID, func(t *Ticket) (Change, error) {
		if t.Status == StatusClosed {
			return Change{}, nil
		}

		now := s.clock.Now()
		s.moveTo(t, StatusClosed, now)
		t.UpdatedAt = now

		return Change{Messages: []Message{s.systemMessage(t.ID, "Closed by the requester")}}, nil
	})
	if err != nil {
		return TicketView{}, err
	}

	messages, err := s.repository.ListMessages(ctx, updated.ID, false)
	if err != nil {
		return TicketView{}, err
	}

	return TicketView{Ticket: updated, Role: role, Messages: messages}, nil
}

// AttachmentURL links a ticket's file for the people in the ticket (not
// files of internal notes), or for staff allowed to see the ticket.
func (s *Service) AttachmentURL(ctx context.Context, caller Caller, ticketID, mediaID, method string) (string, time.Time, error) {
	if !validID(ticketID) || !validID(mediaID) {
		return "", time.Time{}, ErrNotFound
	}

	ticket, err := s.repository.GetTicket(ctx, ticketID)
	if err != nil {
		return "", time.Time{}, err
	}

	internal, found, err := s.repository.TicketHasAttachment(ctx, ticketID, mediaID)
	if err != nil {
		return "", time.Time{}, err
	}

	if !found {
		return "", time.Time{}, ErrNotFound
	}

	_, err = s.roleOf(ctx, caller, ticket)

	switch {
	case err == nil:
		if internal {
			return "", time.Time{}, ErrNotFound
		}
	case errors.Is(err, ErrNotFound):
		if err := s.staffMaySee(ctx, caller.IdentityID, ticket, method); err != nil {
			return "", time.Time{}, err
		}
	default:
		return "", time.Time{}, err
	}

	return s.media.DownloadURL(ctx, mediaID)
}

// staffMaySee asks staff-service whether someone who is not in the ticket
// may see it as staff.
func (s *Service) staffMaySee(ctx context.Context, identityID string, ticket Ticket, method string) error {
	permissions := []string{PermissionRead}
	if ticket.Safety {
		permissions = append(permissions, PermissionSafety)
	}

	for _, permission := range permissions {
		allowed, err := s.staff.Check(ctx, identityID, permission, method, ticket.ID)
		if err != nil {
			return err
		}

		if !allowed {
			return ErrNotFound
		}
	}

	return nil
}
