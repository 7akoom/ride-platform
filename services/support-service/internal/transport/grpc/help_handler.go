package grpc

import (
	"context"
	"errors"
	"time"

	supportv1 "github.com/7akoom/ride-platform/gen/go/ride/support/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/support-service/internal/application/support"
)

func (h *SupportHandler) helpError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, support.ErrArticleNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, support.ErrNotRatable):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return h.mapError(ctx, err)
	}
}

func (h *SupportHandler) ListHelpSections(ctx context.Context, request *supportv1.ListHelpSectionsRequest) (*supportv1.ListHelpSectionsResponse, error) {
	if _, err := caller(ctx); err != nil {
		return nil, err
	}

	sections, err := h.service.ListHelpSections(ctx, audienceFromProto(request.GetAudience()))
	if err != nil {
		return nil, h.helpError(ctx, err)
	}

	return &supportv1.ListHelpSectionsResponse{Sections: sectionsToProto(sections)}, nil
}

func (h *SupportHandler) ListHelpArticles(ctx context.Context, request *supportv1.ListHelpArticlesRequest) (*supportv1.ListHelpArticlesResponse, error) {
	if _, err := caller(ctx); err != nil {
		return nil, err
	}

	articles, err := h.service.ListHelpArticles(ctx, audienceFromProto(request.GetAudience()), request.GetSectionKey(), request.GetQuery())
	if err != nil {
		return nil, h.helpError(ctx, err)
	}

	return &supportv1.ListHelpArticlesResponse{Articles: articlesToProto(articles)}, nil
}

func (h *SupportHandler) GetHelpArticle(ctx context.Context, request *supportv1.GetHelpArticleRequest) (*supportv1.HelpArticleResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	article, err := h.service.GetHelpArticle(ctx, who, request.GetKey())
	if err != nil {
		return nil, h.helpError(ctx, err)
	}

	return &supportv1.HelpArticleResponse{Article: articleToProto(article)}, nil
}

func (h *SupportHandler) RateHelpArticle(ctx context.Context, request *supportv1.RateHelpArticleRequest) (*supportv1.HelpArticleResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	article, err := h.service.RateHelpArticle(ctx, who, request.GetKey(), request.GetHelpful())
	if err != nil {
		return nil, h.helpError(ctx, err)
	}

	return &supportv1.HelpArticleResponse{Article: articleToProto(article)}, nil
}

func (h *SupportHandler) RateTicket(ctx context.Context, request *supportv1.RateTicketRequest) (*supportv1.TicketDetailResponse, error) {
	who, err := caller(ctx)
	if err != nil {
		return nil, err
	}

	view, err := h.service.RateTicket(ctx, who, request.GetTicketId(), int(request.GetRating()), request.GetComment())
	if err != nil {
		return nil, h.helpError(ctx, err)
	}

	return userDetail(view), nil
}

func (h *SupportHandler) AdminListHelpSections(ctx context.Context, _ *supportv1.AdminListHelpSectionsRequest) (*supportv1.ListHelpSectionsResponse, error) {
	if _, err := staffCaller(ctx); err != nil {
		return nil, err
	}

	sections, err := h.service.AdminListHelpSections(ctx)
	if err != nil {
		return nil, h.helpError(ctx, err)
	}

	return &supportv1.ListHelpSectionsResponse{Sections: sectionsToProto(sections)}, nil
}

func (h *SupportHandler) UpsertHelpSection(ctx context.Context, request *supportv1.UpsertHelpSectionRequest) (*supportv1.HelpSectionResponse, error) {
	if _, err := staffCaller(ctx); err != nil {
		return nil, err
	}

	s := request.GetSection()

	saved, err := h.service.UpsertHelpSection(ctx, support.HelpSection{
		Key:       s.GetKey(),
		Audience:  categoryAudienceFromProto(s.GetAudience()),
		NameEn:    s.GetNameEn(),
		NameAr:    s.GetNameAr(),
		NameKu:    s.GetNameKu(),
		SortOrder: int(s.GetSortOrder()),
		Active:    s.GetActive(),
	})
	if err != nil {
		return nil, h.helpError(ctx, err)
	}

	return &supportv1.HelpSectionResponse{Section: sectionToProto(saved)}, nil
}

func (h *SupportHandler) AdminListHelpArticles(ctx context.Context, request *supportv1.AdminListHelpArticlesRequest) (*supportv1.ListHelpArticlesResponse, error) {
	if _, err := staffCaller(ctx); err != nil {
		return nil, err
	}

	articles, err := h.service.AdminListHelpArticles(ctx, request.GetSectionKey())
	if err != nil {
		return nil, h.helpError(ctx, err)
	}

	return &supportv1.ListHelpArticlesResponse{Articles: articlesToProto(articles)}, nil
}

func (h *SupportHandler) UpsertHelpArticle(ctx context.Context, request *supportv1.UpsertHelpArticleRequest) (*supportv1.HelpArticleResponse, error) {
	if _, err := staffCaller(ctx); err != nil {
		return nil, err
	}

	a := request.GetArticle()

	saved, err := h.service.UpsertHelpArticle(ctx, support.HelpArticle{
		Key:                a.GetKey(),
		SectionKey:         a.GetSectionKey(),
		Audience:           categoryAudienceFromProto(a.GetAudience()),
		TitleEn:            a.GetTitleEn(),
		TitleAr:            a.GetTitleAr(),
		TitleKu:            a.GetTitleKu(),
		BodyEn:             a.GetBodyEn(),
		BodyAr:             a.GetBodyAr(),
		BodyKu:             a.GetBodyKu(),
		ContactCategoryKey: a.GetContactCategoryKey(),
		SortOrder:          int(a.GetSortOrder()),
		Published:          a.GetPublished(),
	})
	if err != nil {
		return nil, h.helpError(ctx, err)
	}

	return &supportv1.HelpArticleResponse{Article: articleToProto(saved)}, nil
}

func (h *SupportHandler) ListMacros(ctx context.Context, request *supportv1.ListMacrosRequest) (*supportv1.ListMacrosResponse, error) {
	if _, err := staffCaller(ctx); err != nil {
		return nil, err
	}

	macros, err := h.service.ListMacros(ctx, request.GetCategoryKey())
	if err != nil {
		return nil, h.helpError(ctx, err)
	}

	return &supportv1.ListMacrosResponse{Macros: macrosToProto(macros)}, nil
}

func (h *SupportHandler) AdminListMacros(ctx context.Context, _ *supportv1.AdminListMacrosRequest) (*supportv1.ListMacrosResponse, error) {
	if _, err := staffCaller(ctx); err != nil {
		return nil, err
	}

	macros, err := h.service.AdminListMacros(ctx)
	if err != nil {
		return nil, h.helpError(ctx, err)
	}

	return &supportv1.ListMacrosResponse{Macros: macrosToProto(macros)}, nil
}

func (h *SupportHandler) UpsertMacro(ctx context.Context, request *supportv1.UpsertMacroRequest) (*supportv1.MacroResponse, error) {
	if _, err := staffCaller(ctx); err != nil {
		return nil, err
	}

	m := request.GetMacro()

	saved, err := h.service.UpsertMacro(ctx, support.Macro{
		Key:         m.GetKey(),
		Title:       m.GetTitle(),
		BodyEn:      m.GetBodyEn(),
		BodyAr:      m.GetBodyAr(),
		BodyKu:      m.GetBodyKu(),
		CategoryKey: m.GetCategoryKey(),
		Active:      m.GetActive(),
		SortOrder:   int(m.GetSortOrder()),
	})
	if err != nil {
		return nil, h.helpError(ctx, err)
	}

	return &supportv1.MacroResponse{Macro: macroToProto(saved)}, nil
}

func (h *SupportHandler) GetSupportStats(ctx context.Context, request *supportv1.GetSupportStatsRequest) (*supportv1.GetSupportStatsResponse, error) {
	if _, err := staffCaller(ctx); err != nil {
		return nil, err
	}

	var from, to *time.Time

	if request.GetFrom() != nil {
		t := request.GetFrom().AsTime()
		from = &t
	}

	if request.GetTo() != nil {
		t := request.GetTo().AsTime()
		to = &t
	}

	stats, err := h.service.Stats(ctx, from, to)
	if err != nil {
		return nil, h.helpError(ctx, err)
	}

	out := &supportv1.GetSupportStatsResponse{
		OpenUnassigned:             int32(stats.OpenUnassigned),
		OpenLate:                   int32(stats.OpenLate),
		Opened:                     int32(stats.Opened),
		Resolved:                   int32(stats.Resolved),
		Answered:                   int32(stats.Answered),
		AnsweredInTime:             int32(stats.AnsweredInTime),
		MedianFirstResponseMinutes: stats.MedianFirstResponseMins,
		Ratings:                    int32(stats.Ratings),
		AverageRating:              stats.AverageRating,
		From:                       timestamppb.New(stats.From),
		To:                         timestamppb.New(stats.To),
	}

	for _, c := range stats.OpenByPriority {
		out.OpenByPriority = append(out.OpenByPriority, &supportv1.PriorityCount{Priority: priorities[c.Priority], Count: int32(c.Count)})
	}

	return out, nil
}
