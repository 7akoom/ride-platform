package staff

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100

	memberTokenPrefix = "m1:"
	auditTokenPrefix  = "a1:"
)

// AuditListQuery asks for one page of the audit log.
type AuditListQuery struct {
	ActorStaffID   string
	Permission     string
	TargetID       string
	OccurredAfter  *time.Time
	OccurredBefore *time.Time
	PageSize       int
	PageToken      string
}

// AuditPage is one page of audit entries, newest first.
type AuditPage struct {
	Entries       []AuditEntry
	NextPageToken string
}

func (s *Service) ListAudit(ctx context.Context, query AuditListQuery) (AuditPage, error) {
	actor := strings.TrimSpace(query.ActorStaffID)
	if actor != "" && !looksLikeUUID(actor) {
		return AuditPage{}, ErrInvalidID
	}

	if query.OccurredAfter != nil && query.OccurredBefore != nil &&
		!query.OccurredAfter.Before(*query.OccurredBefore) {
		return AuditPage{}, ErrInvalidTimeRange
	}

	size, err := pageSize(query.PageSize)
	if err != nil {
		return AuditPage{}, err
	}

	after := ""
	if query.PageToken != "" {
		if after, err = decodePageToken(auditTokenPrefix, query.PageToken); err != nil {
			return AuditPage{}, err
		}
	}

	entries, err := s.repository.ListAudit(ctx, AuditQuery{
		ActorStaffID:   actor,
		Permission:     strings.TrimSpace(query.Permission),
		TargetID:       strings.TrimSpace(query.TargetID),
		OccurredAfter:  query.OccurredAfter,
		OccurredBefore: query.OccurredBefore,
		AfterID:        after,
		Limit:          size + 1,
	})
	if err != nil {
		return AuditPage{}, fmt.Errorf("list audit entries: %w", err)
	}

	page := AuditPage{Entries: entries}

	if len(entries) > size {
		page.Entries = entries[:size]
		page.NextPageToken = encodePageToken(auditTokenPrefix, entries[size-1].ID)
	}

	return page, nil
}

func pageSize(requested int) (int, error) {
	switch {
	case requested < 0:
		return 0, ErrInvalidPageSize
	case requested == 0:
		return DefaultPageSize, nil
	case requested > MaxPageSize:
		return MaxPageSize, nil
	default:
		return requested, nil
	}
}

// A page token is the id of the last row of the previous page, wrapped so
// callers treat it as opaque.
func encodePageToken(prefix string, lastID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(prefix + lastID))
}

func decodePageToken(prefix string, token string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", ErrInvalidPageToken
	}

	id, ok := strings.CutPrefix(string(raw), prefix)
	if !ok || !looksLikeUUID(id) {
		return "", ErrInvalidPageToken
	}

	return id, nil
}
