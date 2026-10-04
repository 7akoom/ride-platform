package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/7akoom/ride-platform/services/support-service/internal/application/support"
)

func TestHelpCentreAndMacros(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	rider := support.Caller{IdentityID: uuid.NewString()}

	sections := must(w.service.ListHelpSections(ctx, support.AudienceRider))(t)
	for _, s := range sections {
		if s.Key == "driving" {
			t.Fatal("riders see the drivers' section")
		}
	}

	articles := must(w.service.ListHelpArticles(ctx, support.AudienceRider, "", ""))(t)
	keys := map[string]bool{}

	for _, a := range articles {
		keys[a.Key] = true
		if a.BodyEn != "" {
			t.Fatal("the list carries bodies")
		}
	}

	if !keys["lost-item"] || keys["documents-expiring"] {
		t.Fatalf("rider articles: %v", keys)
	}

	found := must(w.service.ListHelpArticles(ctx, support.AudienceRider, "", "محفظت"))(t)
	if len(found) == 0 {
		t.Fatal("no Arabic search result")
	}

	none := must(w.service.ListHelpArticles(ctx, support.AudienceRider, "", "100%_"))(t)
	if len(none) != 0 {
		t.Fatalf("wildcards matched: %d", len(none))
	}

	_, err := w.service.ListHelpArticles(ctx, support.AudienceRider, "", "x")
	wantErr(t, err, support.ErrInvalidInput)

	article := must(w.service.GetHelpArticle(ctx, rider, "lost-item"))(t)
	if article.BodyAr == "" || article.ContactCategoryKey != "lost_item" || article.Voted {
		t.Fatalf("article: %+v", article)
	}

	rated := must(w.service.RateHelpArticle(ctx, rider, "lost-item", false))(t)
	rated = must(w.service.RateHelpArticle(ctx, rider, "lost-item", true))(t)

	if rated.HelpfulCount != 1 || rated.NotHelpfulCount != 0 || !rated.Voted || !rated.VotedHelpful {
		t.Fatalf("vote: %+v", rated)
	}

	// Drafts are only for staff.
	draft := must(w.service.UpsertHelpArticle(ctx, support.HelpArticle{
		Key: "new-draft", SectionKey: "rides", Audience: support.CategoryBoth,
		TitleEn: "Draft", TitleAr: "مسودة", TitleKu: "ڕەشنووس", BodyEn: "b", BodyAr: "ب", BodyKu: "ب",
	}))(t)
	if draft.Published {
		t.Fatal("published by default")
	}

	_, err = w.service.GetHelpArticle(ctx, rider, "new-draft")
	wantErr(t, err, support.ErrArticleNotFound)

	_, err = w.service.UpsertHelpArticle(ctx, support.HelpArticle{
		Key: "bad", SectionKey: "nope", Audience: support.CategoryBoth,
		TitleEn: "x", TitleAr: "x", TitleKu: "x", BodyEn: "x", BodyAr: "x", BodyKu: "x",
	})
	wantErr(t, err, support.ErrInvalidInput)

	_, err = w.service.UpsertHelpArticle(ctx, support.HelpArticle{
		Key: "bad", SectionKey: "rides", Audience: support.CategoryBoth, ContactCategoryKey: "sos",
		TitleEn: "x", TitleAr: "x", TitleKu: "x", BodyEn: "x", BodyAr: "x", BodyKu: "x",
	})
	wantErr(t, err, support.ErrInvalidInput)

	// A section switched off hides its articles from people.
	must(w.service.UpsertHelpSection(ctx, support.HelpSection{
		Key: "safety", Audience: support.CategoryBoth, NameEn: "Safety", NameAr: "السلامة", NameKu: "سەلامەتی", SortOrder: 40,
	}))(t)

	for _, a := range must(w.service.ListHelpArticles(ctx, support.AudienceRider, "", ""))(t) {
		if a.SectionKey == "safety" {
			t.Fatal("an inactive section's article is listed")
		}
	}

	if len(must(w.service.AdminListHelpArticles(ctx, "safety"))(t)) == 0 {
		t.Fatal("staff do not see the inactive section's articles")
	}

	// Macros: the ticket's category first; inactive ones only for admins.
	macros := must(w.service.ListMacros(ctx, "lost_item"))(t)
	if macros[0].Key != "lost_item_driver" {
		t.Fatalf("first macro: %s", macros[0].Key)
	}

	must(w.service.UpsertMacro(ctx, support.Macro{Key: "checking", Title: "We are checking", BodyEn: "a", BodyAr: "ب", BodyKu: "ب"}))(t)

	for _, m := range must(w.service.ListMacros(ctx, ""))(t) {
		if m.Key == "checking" {
			t.Fatal("an inactive macro is listed")
		}
	}

	if len(must(w.service.AdminListMacros(ctx))(t)) != 6 {
		t.Fatal("admins see every macro")
	}

	_, err = w.service.UpsertMacro(ctx, support.Macro{Key: "x1", Title: "t", BodyEn: "a", BodyAr: "a", BodyKu: "a", CategoryKey: "nope"})
	wantErr(t, err, support.ErrInvalidInput)
}

func TestRatingsSLAAndAutoClose(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	rider := w.rider()
	caller := support.Caller{IdentityID: rider.identity}
	agent := w.staffMember(support.PermissionRead, support.PermissionReply)

	open := func() support.Ticket {
		return must(w.service.CreateTicket(ctx, caller, support.CreateTicketInput{Audience: support.AudienceRider, CategoryKey: "other", Body: "help"}))(t).Ticket
	}

	ticket := open()
	if !ticket.FirstResponseDueAt.Equal(ticket.CreatedAt.Add(2 * time.Hour)) {
		t.Fatalf("normal due: %v after %v", ticket.FirstResponseDueAt, ticket.CreatedAt)
	}

	// Urgent: the default 15 minutes, recomputed from the creation.
	raised := must(w.service.SetPriority(ctx, agent, ticket.ID, support.PriorityUrgent, method))(t).Ticket
	if !raised.FirstResponseDueAt.Equal(ticket.CreatedAt.Add(15 * time.Minute)) {
		t.Fatalf("urgent due: %v", raised.FirstResponseDueAt)
	}

	w.clock.advance(20 * time.Minute)

	if !raised.FirstResponseLate(w.clock.Now()) {
		t.Fatal("not late after the target")
	}

	stats := must(w.service.Stats(ctx, nil, nil))(t)
	if stats.OpenLate != 1 || stats.Opened != 1 || stats.OpenUnassigned != 1 {
		t.Fatalf("stats before an answer: %+v", stats)
	}

	answered := must(w.service.Reply(ctx, agent, support.ReplyInput{TicketID: ticket.ID, Body: "on it"}, method))(t).Ticket
	if !answered.FirstResponseLate(w.clock.Now()) {
		t.Fatal("an answer after the target is late")
	}

	// Not rated while open; waiting 72h resolves it automatically.
	_, err := w.service.RateTicket(ctx, caller, ticket.ID, 5, "")
	wantErr(t, err, support.ErrNotRatable)

	w.clock.advance(71 * time.Hour)

	if n := w.service.AutoResolveAndClose(ctx); n != 0 {
		t.Fatalf("moved early: %d", n)
	}

	w.clock.advance(2 * time.Hour)

	if n := w.service.AutoResolveAndClose(ctx); n != 1 {
		t.Fatalf("auto-resolved: %d", n)
	}

	resolved := must(w.service.GetMyTicket(ctx, caller, ticket.ID))(t).Ticket
	if resolved.Status != support.StatusResolved {
		t.Fatalf("after 72h: %s", resolved.Status)
	}

	if events := strings.Join(w.events(t, ticket.ID), ","); !strings.HasSuffix(events, "support.ticket_resolved:rider:"+rider.profile) {
		t.Fatalf("events: %s", events)
	}

	_, err = w.service.RateTicket(ctx, caller, ticket.ID, 6, "")
	wantErr(t, err, support.ErrInvalidInput)

	stranger := support.Caller{IdentityID: uuid.NewString()}
	_, err = w.service.RateTicket(ctx, stranger, ticket.ID, 5, "")
	wantErr(t, err, support.ErrNotFound)

	rated := must(w.service.RateTicket(ctx, caller, ticket.ID, 4, "  quick  "))(t).Ticket
	if rated.Rating != 4 || rated.RatingComment != "quick" || rated.RatedAt == nil {
		t.Fatalf("rated: %+v", rated)
	}

	_, err = w.service.RateTicket(ctx, caller, ticket.ID, 5, "")
	wantErr(t, err, support.ErrNotRatable)

	// A week resolved: closed for good.
	w.clock.advance(8 * 24 * time.Hour)

	if n := w.service.AutoResolveAndClose(ctx); n != 1 {
		t.Fatalf("auto-closed: %d", n)
	}

	closed := must(w.service.GetMyTicket(ctx, caller, ticket.ID))(t).Ticket
	if closed.Status != support.StatusClosed {
		t.Fatalf("after a week: %s", closed.Status)
	}

	// Too late to rate another one resolved more than 7 days ago.
	late := open()
	must(w.service.SetStatus(ctx, agent, late.ID, support.StatusResolved, method))(t)
	w.clock.advance(8 * 24 * time.Hour)

	_, err = w.service.RateTicket(ctx, caller, late.ID, 5, "")
	wantErr(t, err, support.ErrNotRatable)

	period := w.clock.Now().Add(-30 * 24 * time.Hour)
	stats = must(w.service.Stats(ctx, &period, nil))(t)

	if stats.Opened != 2 || stats.Answered != 1 || stats.AnsweredInTime != 0 || stats.Ratings != 1 ||
		stats.AverageRating != 4 || stats.MedianFirstResponseMins < 19 || stats.Resolved != 2 {
		t.Fatalf("stats: %+v", stats)
	}

	_, err = w.service.Stats(ctx, &period, &period)
	wantErr(t, err, support.ErrInvalidInput)
}
