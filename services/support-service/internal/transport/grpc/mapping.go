package grpc

import (
	"time"

	supportv1 "github.com/7akoom/ride-platform/gen/go/ride/support/v1"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/support-service/internal/application/support"
)

func timestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}

	return timestamppb.New(*t)
}

func audienceFromProto(a supportv1.Audience) support.Audience {
	switch a {
	case supportv1.Audience_AUDIENCE_RIDER:
		return support.AudienceRider
	case supportv1.Audience_AUDIENCE_DRIVER:
		return support.AudienceDriver
	}

	return ""
}

func audienceToProto(a support.Audience) supportv1.Audience {
	switch a {
	case support.AudienceRider:
		return supportv1.Audience_AUDIENCE_RIDER
	case support.AudienceDriver:
		return supportv1.Audience_AUDIENCE_DRIVER
	}

	return supportv1.Audience_AUDIENCE_UNSPECIFIED
}

var categoryAudiences = map[support.CategoryAudience]supportv1.CategoryAudience{
	support.CategoryRider:  supportv1.CategoryAudience_CATEGORY_AUDIENCE_RIDER,
	support.CategoryDriver: supportv1.CategoryAudience_CATEGORY_AUDIENCE_DRIVER,
	support.CategoryBoth:   supportv1.CategoryAudience_CATEGORY_AUDIENCE_BOTH,
}

func categoryAudienceFromProto(a supportv1.CategoryAudience) support.CategoryAudience {
	for domain, proto := range categoryAudiences {
		if proto == a {
			return domain
		}
	}

	return ""
}

var statuses = map[support.Status]supportv1.TicketStatus{
	support.StatusOpen:        supportv1.TicketStatus_TICKET_STATUS_OPEN,
	support.StatusInProgress:  supportv1.TicketStatus_TICKET_STATUS_IN_PROGRESS,
	support.StatusWaitingUser: supportv1.TicketStatus_TICKET_STATUS_WAITING_USER,
	support.StatusResolved:    supportv1.TicketStatus_TICKET_STATUS_RESOLVED,
	support.StatusClosed:      supportv1.TicketStatus_TICKET_STATUS_CLOSED,
}

func statusFromProto(s supportv1.TicketStatus) support.Status {
	for domain, proto := range statuses {
		if proto == s {
			return domain
		}
	}

	return ""
}

var priorities = map[support.Priority]supportv1.TicketPriority{
	support.PriorityLow:    supportv1.TicketPriority_TICKET_PRIORITY_LOW,
	support.PriorityNormal: supportv1.TicketPriority_TICKET_PRIORITY_NORMAL,
	support.PriorityHigh:   supportv1.TicketPriority_TICKET_PRIORITY_HIGH,
	support.PriorityUrgent: supportv1.TicketPriority_TICKET_PRIORITY_URGENT,
}

func priorityFromProto(p supportv1.TicketPriority) support.Priority {
	for domain, proto := range priorities {
		if proto == p {
			return domain
		}
	}

	return ""
}

var authors = map[support.Author]supportv1.MessageAuthor{
	support.AuthorRequester:   supportv1.MessageAuthor_MESSAGE_AUTHOR_REQUESTER,
	support.AuthorParticipant: supportv1.MessageAuthor_MESSAGE_AUTHOR_PARTICIPANT,
	support.AuthorStaff:       supportv1.MessageAuthor_MESSAGE_AUTHOR_STAFF,
	support.AuthorSystem:      supportv1.MessageAuthor_MESSAGE_AUTHOR_SYSTEM,
}

var actionKinds = map[support.ActionKind]supportv1.ActionKind{
	support.ActionRefund:       supportv1.ActionKind_ACTION_KIND_REFUND,
	support.ActionWaiveFee:     supportv1.ActionKind_ACTION_KIND_WAIVE_FEE,
	support.ActionCompensation: supportv1.ActionKind_ACTION_KIND_COMPENSATION,
	support.ActionSuspend:      supportv1.ActionKind_ACTION_KIND_SUSPEND_ACCOUNT,
	support.ActionReactivate:   supportv1.ActionKind_ACTION_KIND_REACTIVATE_ACCOUNT,
}

func actionKindFromProto(k supportv1.ActionKind) support.ActionKind {
	for domain, proto := range actionKinds {
		if proto == k {
			return domain
		}
	}

	return ""
}

var actionStatuses = map[support.ActionStatus]supportv1.ActionStatus{
	support.ActionPendingApproval: supportv1.ActionStatus_ACTION_STATUS_PENDING_APPROVAL,
	support.ActionProcessing:      supportv1.ActionStatus_ACTION_STATUS_PROCESSING,
	support.ActionCompleted:       supportv1.ActionStatus_ACTION_STATUS_COMPLETED,
	support.ActionRejected:        supportv1.ActionStatus_ACTION_STATUS_REJECTED,
	support.ActionFailed:          supportv1.ActionStatus_ACTION_STATUS_FAILED,
}

func actionTargetFromProto(t supportv1.ActionTarget) support.ActionTarget {
	switch t {
	case supportv1.ActionTarget_ACTION_TARGET_REQUESTER:
		return support.TargetRequester
	case supportv1.ActionTarget_ACTION_TARGET_COUNTERPART:
		return support.TargetCounterpart
	}

	return ""
}

func actionTargetToProto(t support.ActionTarget) supportv1.ActionTarget {
	switch t {
	case support.TargetRequester:
		return supportv1.ActionTarget_ACTION_TARGET_REQUESTER
	case support.TargetCounterpart:
		return supportv1.ActionTarget_ACTION_TARGET_COUNTERPART
	}

	return supportv1.ActionTarget_ACTION_TARGET_UNSPECIFIED
}

func categoryToProto(c support.Category) *supportv1.SupportCategory {
	return &supportv1.SupportCategory{
		Key:             c.Key,
		Audience:        categoryAudiences[c.Audience],
		NameEn:          c.NameEn,
		NameAr:          c.NameAr,
		NameKu:          c.NameKu,
		DefaultPriority: priorities[c.DefaultPriority],
		RequiresTrip:    c.RequiresTrip,
		Safety:          c.Safety,
		LostItem:        c.LostItem,
		Active:          c.Active,
		SortOrder:       int32(c.SortOrder),
	}
}

func categoriesToProto(categories []support.Category) []*supportv1.SupportCategory {
	out := make([]*supportv1.SupportCategory, 0, len(categories))
	for _, c := range categories {
		out = append(out, categoryToProto(c))
	}

	return out
}

func sourceToProto(source string) supportv1.TicketSource {
	if source == support.SourceSOS {
		return supportv1.TicketSource_TICKET_SOURCE_SOS
	}

	return supportv1.TicketSource_TICKET_SOURCE_APP
}

// ticketToProto is the ticket as the person in it sees it. The driver of a
// lost-item ticket does not see the rider's transaction.
func ticketToProto(t support.Ticket, role support.Role) *supportv1.Ticket {
	out := &supportv1.Ticket{
		Id:            t.ID,
		Number:        support.TicketNumber(t.Number),
		Audience:      audienceToProto(t.Audience),
		CategoryKey:   t.CategoryKey,
		Subject:       t.Subject,
		Status:        statuses[t.Status],
		Priority:      priorities[t.Priority],
		Safety:        t.Safety,
		Source:        sourceToProto(t.Source),
		TripId:        t.TripID,
		TransactionId: t.TransactionID,
		CreatedAt:     timestamppb.New(t.CreatedAt),
		UpdatedAt:     timestamppb.New(t.UpdatedAt),
		LastMessageAt: timestamppb.New(t.LastMessageAt),
		ResolvedAt:    timestamp(t.ResolvedAt),
		ClosedAt:      timestamp(t.ClosedAt),

		FirstResponseDueAt: timestamppb.New(t.FirstResponseDueAt),
		FirstResponseLate:  t.FirstResponseLate(time.Now()),
		Rating:             int32(t.Rating),
		RatingComment:      t.RatingComment,
		RatedAt:            timestamp(t.RatedAt),
	}

	switch role {
	case support.RoleRequester:
		out.Role = supportv1.TicketRole_TICKET_ROLE_REQUESTER
	case support.RoleParticipant:
		out.Role = supportv1.TicketRole_TICKET_ROLE_PARTICIPANT
		out.TransactionId = ""
	}

	return out
}

func staffTicketToProto(t support.Ticket) *supportv1.StaffTicket {
	return &supportv1.StaffTicket{
		Ticket:               ticketToProto(t, ""),
		RequesterIdentityId:  t.RequesterIdentityID,
		RequesterProfileId:   t.RequesterProfileID,
		CounterpartProfileId: t.CounterpartProfileID,
		ParticipantDriverId:  t.ParticipantDriverID,
		AssignedStaffId:      t.AssignedStaffID,
		FirstResponseAt:      timestamp(t.FirstResponseAt),
		SosAlertId:           t.SOSAlertID,
	}
}

func messagesToProto(messages []support.Message, staffView bool) []*supportv1.TicketMessage {
	out := make([]*supportv1.TicketMessage, 0, len(messages))

	for _, m := range messages {
		if m.Internal && !staffView {
			continue
		}

		message := &supportv1.TicketMessage{
			Id:                 m.ID,
			Author:             authors[m.Author],
			Body:               m.Body,
			Internal:           m.Internal,
			AttachmentMediaIds: m.AttachmentMediaIDs,
			CreatedAt:          timestamppb.New(m.CreatedAt),
		}

		if staffView {
			message.StaffId = m.AuthorStaffID
		}

		out = append(out, message)
	}

	return out
}

func actionToProto(a support.Action) *supportv1.TicketAction {
	out := &supportv1.TicketAction{
		Id:                 a.ID,
		TicketId:           a.TicketID,
		Kind:               actionKinds[a.Kind],
		Status:             actionStatuses[a.Status],
		Target:             actionTargetToProto(a.Target),
		TargetProfileId:    a.TargetProfileID,
		Reason:             a.Reason,
		RequestedByStaffId: a.RequestedByStaffID,
		DecidedByStaffId:   a.DecidedByStaffID,
		DecisionReason:     a.DecisionReason,
		FailureReason:      a.FailureReason,
		SuspendUntil:       timestamp(a.SuspendUntil),
		CreatedAt:          timestamppb.New(a.CreatedAt),
		DecidedAt:          timestamp(a.DecidedAt),
		CompletedAt:        timestamp(a.CompletedAt),
	}

	if a.Amount != nil {
		out.Amount = a.Amount.StringFixed(2)
	}

	if a.DriverAmount != nil {
		out.DriverAmount = a.DriverAmount.StringFixed(2)
	}

	return out
}

func actionsToProto(actions []support.Action) []*supportv1.TicketAction {
	out := make([]*supportv1.TicketAction, 0, len(actions))
	for _, a := range actions {
		out = append(out, actionToProto(a))
	}

	return out
}

func staffDetail(view support.StaffTicketView) *supportv1.StaffTicketDetailResponse {
	return &supportv1.StaffTicketDetailResponse{
		Ticket:   staffTicketToProto(view.Ticket),
		Messages: messagesToProto(view.Messages, true),
		Actions:  actionsToProto(view.Actions),
	}
}

func userDetail(view support.TicketView) *supportv1.TicketDetailResponse {
	return &supportv1.TicketDetailResponse{
		Ticket:   ticketToProto(view.Ticket, view.Role),
		Messages: messagesToProto(view.Messages, false),
	}
}

func sectionToProto(s support.HelpSection) *supportv1.HelpSection {
	return &supportv1.HelpSection{
		Key:       s.Key,
		Audience:  categoryAudiences[s.Audience],
		NameEn:    s.NameEn,
		NameAr:    s.NameAr,
		NameKu:    s.NameKu,
		SortOrder: int32(s.SortOrder),
		Active:    s.Active,
	}
}

func sectionsToProto(sections []support.HelpSection) []*supportv1.HelpSection {
	out := make([]*supportv1.HelpSection, 0, len(sections))
	for _, s := range sections {
		out = append(out, sectionToProto(s))
	}

	return out
}

func articleToProto(a support.HelpArticle) *supportv1.HelpArticle {
	return &supportv1.HelpArticle{
		Key:                a.Key,
		SectionKey:         a.SectionKey,
		Audience:           categoryAudiences[a.Audience],
		TitleEn:            a.TitleEn,
		TitleAr:            a.TitleAr,
		TitleKu:            a.TitleKu,
		BodyEn:             a.BodyEn,
		BodyAr:             a.BodyAr,
		BodyKu:             a.BodyKu,
		ContactCategoryKey: a.ContactCategoryKey,
		SortOrder:          int32(a.SortOrder),
		Published:          a.Published,
		HelpfulCount:       int32(a.HelpfulCount),
		NotHelpfulCount:    int32(a.NotHelpfulCount),
		UpdatedAt:          timestamppb.New(a.UpdatedAt),
		Voted:              a.Voted,
		VotedHelpful:       a.VotedHelpful,
	}
}

func articlesToProto(articles []support.HelpArticle) []*supportv1.HelpArticle {
	out := make([]*supportv1.HelpArticle, 0, len(articles))
	for _, a := range articles {
		out = append(out, articleToProto(a))
	}

	return out
}

func macroToProto(m support.Macro) *supportv1.Macro {
	return &supportv1.Macro{
		Key:         m.Key,
		Title:       m.Title,
		BodyEn:      m.BodyEn,
		BodyAr:      m.BodyAr,
		BodyKu:      m.BodyKu,
		CategoryKey: m.CategoryKey,
		Active:      m.Active,
		SortOrder:   int32(m.SortOrder),
	}
}

func macrosToProto(macros []support.Macro) []*supportv1.Macro {
	out := make([]*supportv1.Macro, 0, len(macros))
	for _, m := range macros {
		out = append(out, macroToProto(m))
	}

	return out
}
