package support

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxArticleBody     = 20000
	maxTitleLength     = 160
	maxMacroBody       = 4000
	maxRatingComment   = 1000
	ratingWindow       = 7 * 24 * time.Hour
	minQueryLength     = 2
	maxQueryLength     = 100
	maxStatsPeriod     = 366 * 24 * time.Hour
	defaultStatsPeriod = 7 * 24 * time.Hour
	autoBatch          = 50
)

type HelpSection struct {
	Key       string
	Audience  CategoryAudience
	NameEn    string
	NameAr    string
	NameKu    string
	SortOrder int
	Active    bool
}

type HelpArticle struct {
	Key                string
	SectionKey         string
	Audience           CategoryAudience
	TitleEn            string
	TitleAr            string
	TitleKu            string
	BodyEn             string
	BodyAr             string
	BodyKu             string
	ContactCategoryKey string
	SortOrder          int
	Published          bool
	HelpfulCount       int
	NotHelpfulCount    int
	UpdatedAt          time.Time
	// A person's own vote, when read for them.
	Voted        bool
	VotedHelpful bool
}

type ArticleQuery struct {
	// Empty audience: every article (staff).
	Audience       Audience
	SectionKey     string
	Query          string
	PublishedOnly  bool
	ActiveSections bool
	WithBodies     bool
}

type Macro struct {
	Key         string
	Title       string
	BodyEn      string
	BodyAr      string
	BodyKu      string
	CategoryKey string
	Active      bool
	SortOrder   int
}

type PriorityCount struct {
	Priority Priority
	Count    int
}

type Stats struct {
	OpenByPriority          []PriorityCount
	OpenUnassigned          int
	OpenLate                int
	Opened                  int
	Resolved                int
	Answered                int
	AnsweredInTime          int
	MedianFirstResponseMins float64
	Ratings                 int
	AverageRating           float64
	From                    time.Time
	To                      time.Time
}

// --- people ---------------------------------------------------------------

func (s *Service) ListHelpSections(ctx context.Context, audience Audience) ([]HelpSection, error) {
	if !audience.Valid() {
		return nil, fmt.Errorf("%w: audience is rider or driver", ErrInvalidInput)
	}

	all, err := s.repository.ListHelpSections(ctx, false)
	if err != nil {
		return nil, err
	}

	out := make([]HelpSection, 0, len(all))

	for _, section := range all {
		if section.Audience.Includes(audience) {
			out = append(out, section)
		}
	}

	return out, nil
}

func (s *Service) ListHelpArticles(ctx context.Context, audience Audience, sectionKey, query string) ([]HelpArticle, error) {
	if !audience.Valid() {
		return nil, fmt.Errorf("%w: audience is rider or driver", ErrInvalidInput)
	}

	query = strings.TrimSpace(query)
	if query != "" {
		if n := utf8.RuneCountInString(query); n < minQueryLength || n > maxQueryLength {
			return nil, fmt.Errorf("%w: a search is %d-%d characters", ErrInvalidInput, minQueryLength, maxQueryLength)
		}
	}

	return s.repository.ListHelpArticles(ctx, ArticleQuery{
		Audience:       audience,
		SectionKey:     strings.TrimSpace(sectionKey),
		Query:          query,
		PublishedOnly:  true,
		ActiveSections: true,
	})
}

// GetHelpArticle reads a published article, with the caller's own vote. An
// unpublished one answers like a missing one.
func (s *Service) GetHelpArticle(ctx context.Context, caller Caller, key string) (HelpArticle, error) {
	article, err := s.repository.GetHelpArticle(ctx, strings.TrimSpace(key), caller.IdentityID)
	if err != nil {
		return HelpArticle{}, err
	}

	if !article.Published {
		return HelpArticle{}, ErrArticleNotFound
	}

	return article, nil
}

func (s *Service) RateHelpArticle(ctx context.Context, caller Caller, key string, helpful bool) (HelpArticle, error) {
	article, err := s.GetHelpArticle(ctx, caller, key)
	if err != nil {
		return HelpArticle{}, err
	}

	if err := s.repository.VoteHelpArticle(ctx, article.Key, caller.IdentityID, helpful, s.clock.Now()); err != nil {
		return HelpArticle{}, err
	}

	return s.repository.GetHelpArticle(ctx, article.Key, caller.IdentityID)
}

// RateTicket records the requester's rating of a resolved or closed ticket,
// once, within 7 days of it being resolved (or closed).
func (s *Service) RateTicket(ctx context.Context, caller Caller, ticketID string, rating int, comment string) (TicketView, error) {
	comment = strings.TrimSpace(comment)
	if rating < 1 || rating > 5 {
		return TicketView{}, fmt.Errorf("%w: a rating is 1 to 5", ErrInvalidInput)
	}

	if utf8.RuneCountInString(comment) > maxRatingComment || !utf8.ValidString(comment) {
		return TicketView{}, fmt.Errorf("%w: a comment is at most %d characters", ErrInvalidInput, maxRatingComment)
	}

	ticket, role, err := s.myTicket(ctx, caller, ticketID)
	if err != nil {
		return TicketView{}, err
	}

	if role != RoleRequester {
		return TicketView{}, ErrPermissionDenied
	}

	updated, err := s.repository.UpdateTicket(ctx, ticket.ID, func(t *Ticket) (Change, error) {
		now := s.clock.Now()

		ended := t.ResolvedAt
		if ended == nil {
			ended = t.ClosedAt
		}

		if t.Status.Active() || t.RatedAt != nil || ended == nil || now.Sub(*ended) > ratingWindow {
			return Change{}, ErrNotRatable
		}

		t.Rating = rating
		t.RatingComment = comment
		t.RatedAt = &now

		return Change{Messages: []Message{s.systemMessage(t.ID, fmt.Sprintf("Rated %d/5 by the requester", rating))}}, nil
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

// --- staff: content ---------------------------------------------------------

func (s *Service) AdminListHelpSections(ctx context.Context) ([]HelpSection, error) {
	return s.repository.ListHelpSections(ctx, true)
}

func (s *Service) UpsertHelpSection(ctx context.Context, section HelpSection) (HelpSection, error) {
	section.Key = strings.TrimSpace(section.Key)
	section.NameEn = strings.TrimSpace(section.NameEn)
	section.NameAr = strings.TrimSpace(section.NameAr)
	section.NameKu = strings.TrimSpace(section.NameKu)

	switch {
	case !validCategoryKey(section.Key):
		return HelpSection{}, fmt.Errorf("%w: a key is 2-40 lowercase letters, digits and underscores", ErrInvalidInput)
	case !section.Audience.Valid():
		return HelpSection{}, fmt.Errorf("%w: audience is rider, driver or both", ErrInvalidInput)
	case !validName(section.NameEn) || !validName(section.NameAr) || !validName(section.NameKu):
		return HelpSection{}, fmt.Errorf("%w: names in en, ar and ku are 1-80 characters", ErrInvalidInput)
	case section.SortOrder < 0 || section.SortOrder > 100000:
		return HelpSection{}, fmt.Errorf("%w: sort_order is 0-100000", ErrInvalidInput)
	}

	return s.repository.UpsertHelpSection(ctx, section)
}

func (s *Service) AdminListHelpArticles(ctx context.Context, sectionKey string) ([]HelpArticle, error) {
	return s.repository.ListHelpArticles(ctx, ArticleQuery{SectionKey: strings.TrimSpace(sectionKey), WithBodies: true})
}

func validArticleKey(key string) bool {
	if len(key) < 2 || len(key) > 80 || key[0] < 'a' || key[0] > 'z' {
		return false
	}

	for _, r := range key {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}

	return true
}

func validText(value string, min, max int) bool {
	n := utf8.RuneCountInString(value)

	return utf8.ValidString(value) && n >= min && n <= max
}

func (s *Service) UpsertHelpArticle(ctx context.Context, article HelpArticle) (HelpArticle, error) {
	article.Key = strings.TrimSpace(article.Key)
	article.SectionKey = strings.TrimSpace(article.SectionKey)
	article.ContactCategoryKey = strings.TrimSpace(article.ContactCategoryKey)

	for _, field := range []*string{&article.TitleEn, &article.TitleAr, &article.TitleKu, &article.BodyEn, &article.BodyAr, &article.BodyKu} {
		*field = strings.TrimSpace(*field)
	}

	switch {
	case !validArticleKey(article.Key):
		return HelpArticle{}, fmt.Errorf("%w: a key is 2-80 lowercase letters, digits and hyphens", ErrInvalidInput)
	case !article.Audience.Valid():
		return HelpArticle{}, fmt.Errorf("%w: audience is rider, driver or both", ErrInvalidInput)
	case !validText(article.TitleEn, 1, maxTitleLength) || !validText(article.TitleAr, 1, maxTitleLength) ||
		!validText(article.TitleKu, 1, maxTitleLength):
		return HelpArticle{}, fmt.Errorf("%w: titles in en, ar and ku are 1-%d characters", ErrInvalidInput, maxTitleLength)
	case !validText(article.BodyEn, 1, maxArticleBody) || !validText(article.BodyAr, 1, maxArticleBody) ||
		!validText(article.BodyKu, 1, maxArticleBody):
		return HelpArticle{}, fmt.Errorf("%w: bodies in en, ar and ku are 1-%d characters", ErrInvalidInput, maxArticleBody)
	case article.SortOrder < 0 || article.SortOrder > 100000:
		return HelpArticle{}, fmt.Errorf("%w: sort_order is 0-100000", ErrInvalidInput)
	}

	if _, err := s.repository.GetHelpSection(ctx, article.SectionKey); err != nil {
		if errors.Is(err, ErrNotFound) {
			return HelpArticle{}, fmt.Errorf("%w: unknown section", ErrInvalidInput)
		}

		return HelpArticle{}, err
	}

	if article.ContactCategoryKey != "" {
		category, err := s.repository.GetCategory(ctx, article.ContactCategoryKey)
		if errors.Is(err, ErrCategoryNotFound) || (err == nil && category.Key == CategorySOS) {
			return HelpArticle{}, fmt.Errorf("%w: unknown contact category", ErrInvalidInput)
		}

		if err != nil {
			return HelpArticle{}, err
		}
	}

	return s.repository.UpsertHelpArticle(ctx, article)
}

func (s *Service) ListMacros(ctx context.Context, categoryKey string) ([]Macro, error) {
	macros, err := s.repository.ListMacros(ctx, false)
	if err != nil {
		return nil, err
	}

	categoryKey = strings.TrimSpace(categoryKey)
	if categoryKey == "" {
		return macros, nil
	}

	first := make([]Macro, 0, len(macros))
	rest := make([]Macro, 0, len(macros))

	for _, m := range macros {
		if m.CategoryKey == categoryKey {
			first = append(first, m)
		} else {
			rest = append(rest, m)
		}
	}

	return append(first, rest...), nil
}

func (s *Service) AdminListMacros(ctx context.Context) ([]Macro, error) {
	return s.repository.ListMacros(ctx, true)
}

func (s *Service) UpsertMacro(ctx context.Context, macro Macro) (Macro, error) {
	macro.Key = strings.TrimSpace(macro.Key)
	macro.CategoryKey = strings.TrimSpace(macro.CategoryKey)

	for _, field := range []*string{&macro.Title, &macro.BodyEn, &macro.BodyAr, &macro.BodyKu} {
		*field = strings.TrimSpace(*field)
	}

	switch {
	case !validCategoryKey(macro.Key):
		return Macro{}, fmt.Errorf("%w: a key is 2-40 lowercase letters, digits and underscores", ErrInvalidInput)
	case !validText(macro.Title, 1, 120):
		return Macro{}, fmt.Errorf("%w: a title is 1-120 characters", ErrInvalidInput)
	case !validText(macro.BodyEn, 1, maxMacroBody) || !validText(macro.BodyAr, 1, maxMacroBody) ||
		!validText(macro.BodyKu, 1, maxMacroBody):
		return Macro{}, fmt.Errorf("%w: bodies in en, ar and ku are 1-%d characters", ErrInvalidInput, maxMacroBody)
	case macro.SortOrder < 0 || macro.SortOrder > 100000:
		return Macro{}, fmt.Errorf("%w: sort_order is 0-100000", ErrInvalidInput)
	}

	if macro.CategoryKey != "" {
		if _, err := s.repository.GetCategory(ctx, macro.CategoryKey); err != nil {
			if errors.Is(err, ErrCategoryNotFound) {
				return Macro{}, fmt.Errorf("%w: unknown category", ErrInvalidInput)
			}

			return Macro{}, err
		}
	}

	return s.repository.UpsertMacro(ctx, macro)
}

func (s *Service) Stats(ctx context.Context, from, to *time.Time) (Stats, error) {
	end := s.clock.Now()
	if to != nil {
		end = *to
	}

	start := end.Add(-defaultStatsPeriod)
	if from != nil {
		start = *from
	}

	if !start.Before(end) || end.Sub(start) > maxStatsPeriod {
		return Stats{}, fmt.Errorf("%w: from is before to, at most 366 days apart", ErrInvalidInput)
	}

	return s.repository.Stats(ctx, start, end, s.clock.Now())
}

// --- automatic resolve and close -------------------------------------------

// AutoResolveAndClose resolves tickets left waiting for the person past
// AutoResolveAfter (telling them), and closes tickets resolved for longer
// than AutoCloseAfter. It returns how many it changed.
func (s *Service) AutoResolveAndClose(ctx context.Context) int {
	changed := 0
	now := s.clock.Now()

	sweep := func(from Status, after time.Duration, to Status, note string) {
		if after <= 0 {
			return
		}

		ids, err := s.repository.ListStaleTickets(ctx, from, now.Add(-after), autoBatch)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to list tickets to move on", "status", from, "error", err)

			return
		}

		for _, id := range ids {
			_, err := s.repository.UpdateTicket(ctx, id, func(t *Ticket) (Change, error) {
				if t.Status != from || t.StatusChangedAt.After(now.Add(-after)) {
					return Change{}, nil
				}

				s.moveTo(t, to, now)
				t.UpdatedAt = now
				change := Change{Messages: []Message{s.systemMessage(t.ID, note)}}

				if to == StatusResolved {
					change.Events = append(change.Events, recipientEvent(EventTicketResolved, *t, t.Audience, t.RequesterProfileID))
				}

				return change, nil
			})
			if err != nil {
				s.logger.WarnContext(ctx, "failed to move a ticket on", "ticket_id", id, "error", err)

				continue
			}

			changed++
		}
	}

	sweep(StatusWaitingUser, s.config.AutoResolveAfter, StatusResolved, "Resolved automatically: no answer from the requester")
	sweep(StatusResolved, s.config.AutoCloseAfter, StatusClosed, "Closed automatically")

	return changed
}
