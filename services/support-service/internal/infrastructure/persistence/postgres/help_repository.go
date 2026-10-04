package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/7akoom/ride-platform/services/support-service/internal/application/support"
)

// --- stale tickets and stats ------------------------------------------------

func (r *SupportRepository) ListStaleTickets(ctx context.Context, status support.Status, before time.Time, limit int) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id FROM support_tickets
		 WHERE status = $1 AND status_changed_at < $2
		 ORDER BY status_changed_at
		 LIMIT $3`, string(status), before, limit)
	if err != nil {
		return nil, fmt.Errorf("list stale tickets: %w", err)
	}
	defer rows.Close()

	var ids []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan stale ticket: %w", err)
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}

func (r *SupportRepository) Stats(ctx context.Context, from, to, now time.Time) (support.Stats, error) {
	stats := support.Stats{From: from, To: to}

	rows, err := r.pool.Query(ctx,
		`SELECT priority, count(*) FROM support_tickets
		 WHERE status NOT IN ('resolved', 'closed')
		 GROUP BY priority`)
	if err != nil {
		return support.Stats{}, fmt.Errorf("count open tickets: %w", err)
	}

	counts := map[support.Priority]int{}

	for rows.Next() {
		var priority string
		var count int

		if err := rows.Scan(&priority, &count); err != nil {
			rows.Close()

			return support.Stats{}, fmt.Errorf("scan open tickets: %w", err)
		}

		counts[support.Priority(priority)] = count
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return support.Stats{}, err
	}

	for _, p := range []support.Priority{support.PriorityUrgent, support.PriorityHigh, support.PriorityNormal, support.PriorityLow} {
		stats.OpenByPriority = append(stats.OpenByPriority, support.PriorityCount{Priority: p, Count: counts[p]})
	}

	err = r.pool.QueryRow(ctx,
		`SELECT
		    count(*) FILTER (WHERE status NOT IN ('resolved', 'closed') AND assigned_staff_id IS NULL),
		    count(*) FILTER (WHERE status NOT IN ('resolved', 'closed') AND first_response_at IS NULL
		                       AND first_response_due_at < $3),
		    count(*) FILTER (WHERE created_at >= $1 AND created_at < $2),
		    count(*) FILTER (WHERE resolved_at >= $1 AND resolved_at < $2),
		    count(*) FILTER (WHERE created_at >= $1 AND created_at < $2 AND first_response_at IS NOT NULL),
		    count(*) FILTER (WHERE created_at >= $1 AND created_at < $2 AND first_response_at IS NOT NULL
		                       AND first_response_at <= first_response_due_at),
		    COALESCE(percentile_cont(0.5) WITHIN GROUP (
		        ORDER BY extract(epoch FROM first_response_at - created_at) / 60)
		        FILTER (WHERE created_at >= $1 AND created_at < $2 AND first_response_at IS NOT NULL), 0),
		    count(*) FILTER (WHERE rated_at >= $1 AND rated_at < $2),
		    COALESCE(avg(rating) FILTER (WHERE rated_at >= $1 AND rated_at < $2), 0)
		 FROM support_tickets`, from, to, now).Scan(
		&stats.OpenUnassigned, &stats.OpenLate, &stats.Opened, &stats.Resolved,
		&stats.Answered, &stats.AnsweredInTime, &stats.MedianFirstResponseMins,
		&stats.Ratings, &stats.AverageRating)
	if err != nil {
		return support.Stats{}, fmt.Errorf("support stats: %w", err)
	}

	return stats, nil
}

// --- help sections ----------------------------------------------------------

const sectionColumns = `key, audience, name_en, name_ar, name_ku, sort_order, active`

func scanSection(row pgx.Row) (support.HelpSection, error) {
	var s support.HelpSection
	var audience string

	err := row.Scan(&s.Key, &audience, &s.NameEn, &s.NameAr, &s.NameKu, &s.SortOrder, &s.Active)
	s.Audience = support.CategoryAudience(audience)

	return s, err
}

func (r *SupportRepository) ListHelpSections(ctx context.Context, includeInactive bool) ([]support.HelpSection, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+sectionColumns+` FROM support_help_sections WHERE $1 OR active ORDER BY sort_order, key`, includeInactive)
	if err != nil {
		return nil, fmt.Errorf("list help sections: %w", err)
	}
	defer rows.Close()

	var out []support.HelpSection

	for rows.Next() {
		section, err := scanSection(rows)
		if err != nil {
			return nil, fmt.Errorf("scan help section: %w", err)
		}

		out = append(out, section)
	}

	return out, rows.Err()
}

func (r *SupportRepository) GetHelpSection(ctx context.Context, key string) (support.HelpSection, error) {
	section, err := scanSection(r.pool.QueryRow(ctx, `SELECT `+sectionColumns+` FROM support_help_sections WHERE key = $1`, key))
	if errors.Is(err, pgx.ErrNoRows) {
		return support.HelpSection{}, support.ErrNotFound
	}

	if err != nil {
		return support.HelpSection{}, fmt.Errorf("get help section: %w", err)
	}

	return section, nil
}

func (r *SupportRepository) UpsertHelpSection(ctx context.Context, s support.HelpSection) (support.HelpSection, error) {
	saved, err := scanSection(r.pool.QueryRow(ctx,
		`INSERT INTO support_help_sections (key, audience, name_en, name_ar, name_ku, sort_order, active)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (key) DO UPDATE SET
		    audience = EXCLUDED.audience, name_en = EXCLUDED.name_en, name_ar = EXCLUDED.name_ar,
		    name_ku = EXCLUDED.name_ku, sort_order = EXCLUDED.sort_order, active = EXCLUDED.active,
		    updated_at = CURRENT_TIMESTAMP
		 RETURNING `+sectionColumns,
		s.Key, string(s.Audience), s.NameEn, s.NameAr, s.NameKu, s.SortOrder, s.Active))
	if err != nil {
		return support.HelpSection{}, fmt.Errorf("upsert help section: %w", err)
	}

	return saved, nil
}

// --- help articles ----------------------------------------------------------

func articleColumns(withBodies bool) string {
	bodies := `'', '', ''`
	if withBodies {
		bodies = `a.body_en, a.body_ar, a.body_ku`
	}

	return `a.key, a.section_key, a.audience, a.title_en, a.title_ar, a.title_ku, ` + bodies + `,
		COALESCE(a.contact_category_key, ''), a.sort_order, a.published,
		(SELECT count(*) FROM support_help_votes v WHERE v.article_key = a.key AND v.helpful),
		(SELECT count(*) FROM support_help_votes v WHERE v.article_key = a.key AND NOT v.helpful),
		a.updated_at`
}

func scanArticle(row pgx.Row, extra ...any) (support.HelpArticle, error) {
	var a support.HelpArticle
	var audience string

	dest := []any{&a.Key, &a.SectionKey, &audience, &a.TitleEn, &a.TitleAr, &a.TitleKu,
		&a.BodyEn, &a.BodyAr, &a.BodyKu, &a.ContactCategoryKey, &a.SortOrder, &a.Published,
		&a.HelpfulCount, &a.NotHelpfulCount, &a.UpdatedAt}

	err := row.Scan(append(dest, extra...)...)
	a.Audience = support.CategoryAudience(audience)

	return a, err
}

func likePattern(query string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(query)

	return "%" + escaped + "%"
}

func (r *SupportRepository) ListHelpArticles(ctx context.Context, q support.ArticleQuery) ([]support.HelpArticle, error) {
	conditions := []string{"TRUE"}
	args := []any{}

	add := func(condition string, value any) {
		args = append(args, value)
		conditions = append(conditions, strings.ReplaceAll(condition, "?", "$"+strconv.Itoa(len(args))))
	}

	if q.Audience != "" {
		add("a.audience IN ('both', ?)", string(q.Audience))
	}

	if q.SectionKey != "" {
		add("a.section_key = ?", q.SectionKey)
	}

	if q.PublishedOnly {
		conditions = append(conditions, "a.published")
	}

	if q.ActiveSections {
		conditions = append(conditions, "s.active")
		if q.Audience != "" {
			add("s.audience IN ('both', ?)", string(q.Audience))
		}
	}

	if q.Query != "" {
		add(`(a.title_en ILIKE ? OR a.title_ar ILIKE ? OR a.title_ku ILIKE ?
		      OR a.body_en ILIKE ? OR a.body_ar ILIKE ? OR a.body_ku ILIKE ?)`, likePattern(q.Query))
	}

	rows, err := r.pool.Query(ctx,
		`SELECT `+articleColumns(q.WithBodies)+`
		 FROM support_help_articles a
		 JOIN support_help_sections s ON s.key = a.section_key
		 WHERE `+strings.Join(conditions, " AND ")+`
		 ORDER BY s.sort_order, a.sort_order, a.key
		 LIMIT 200`, args...)
	if err != nil {
		return nil, fmt.Errorf("list help articles: %w", err)
	}
	defer rows.Close()

	var out []support.HelpArticle

	for rows.Next() {
		article, err := scanArticle(rows)
		if err != nil {
			return nil, fmt.Errorf("scan help article: %w", err)
		}

		out = append(out, article)
	}

	return out, rows.Err()
}

func (r *SupportRepository) GetHelpArticle(ctx context.Context, key, identityID string) (support.HelpArticle, error) {
	var voted *bool

	article, err := scanArticle(r.pool.QueryRow(ctx,
		`SELECT `+articleColumns(true)+`,
		    (SELECT v.helpful FROM support_help_votes v WHERE v.article_key = a.key AND v.identity_id = $2::uuid)
		 FROM support_help_articles a
		 WHERE a.key = $1`, key, nullable(identityID)), &voted)
	if errors.Is(err, pgx.ErrNoRows) {
		return support.HelpArticle{}, support.ErrArticleNotFound
	}

	if err != nil {
		return support.HelpArticle{}, fmt.Errorf("get help article: %w", err)
	}

	if voted != nil {
		article.Voted = true
		article.VotedHelpful = *voted
	}

	return article, nil
}

func (r *SupportRepository) UpsertHelpArticle(ctx context.Context, a support.HelpArticle) (support.HelpArticle, error) {
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO support_help_articles
		    (key, section_key, audience, title_en, title_ar, title_ku, body_en, body_ar, body_ku,
		     contact_category_key, sort_order, published)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 ON CONFLICT (key) DO UPDATE SET
		    section_key = EXCLUDED.section_key, audience = EXCLUDED.audience,
		    title_en = EXCLUDED.title_en, title_ar = EXCLUDED.title_ar, title_ku = EXCLUDED.title_ku,
		    body_en = EXCLUDED.body_en, body_ar = EXCLUDED.body_ar, body_ku = EXCLUDED.body_ku,
		    contact_category_key = EXCLUDED.contact_category_key, sort_order = EXCLUDED.sort_order,
		    published = EXCLUDED.published, updated_at = CURRENT_TIMESTAMP`,
		a.Key, a.SectionKey, string(a.Audience), a.TitleEn, a.TitleAr, a.TitleKu, a.BodyEn, a.BodyAr, a.BodyKu,
		nullable(a.ContactCategoryKey), a.SortOrder, a.Published); err != nil {
		return support.HelpArticle{}, fmt.Errorf("upsert help article: %w", err)
	}

	return r.GetHelpArticle(ctx, a.Key, "")
}

func (r *SupportRepository) VoteHelpArticle(ctx context.Context, key, identityID string, helpful bool, at time.Time) error {
	if _, err := r.pool.Exec(ctx,
		`INSERT INTO support_help_votes (article_key, identity_id, helpful, voted_at)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (article_key, identity_id) DO UPDATE SET helpful = EXCLUDED.helpful, voted_at = EXCLUDED.voted_at`,
		key, identityID, helpful, at); err != nil {
		return fmt.Errorf("vote help article: %w", err)
	}

	return nil
}

// --- macros -----------------------------------------------------------------

const macroColumns = `key, title, body_en, body_ar, body_ku, COALESCE(category_key, ''), active, sort_order`

func scanMacro(row pgx.Row) (support.Macro, error) {
	var m support.Macro

	err := row.Scan(&m.Key, &m.Title, &m.BodyEn, &m.BodyAr, &m.BodyKu, &m.CategoryKey, &m.Active, &m.SortOrder)

	return m, err
}

func (r *SupportRepository) ListMacros(ctx context.Context, includeInactive bool) ([]support.Macro, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+macroColumns+` FROM support_macros WHERE $1 OR active ORDER BY sort_order, key`, includeInactive)
	if err != nil {
		return nil, fmt.Errorf("list macros: %w", err)
	}
	defer rows.Close()

	var out []support.Macro

	for rows.Next() {
		m, err := scanMacro(rows)
		if err != nil {
			return nil, fmt.Errorf("scan macro: %w", err)
		}

		out = append(out, m)
	}

	return out, rows.Err()
}

func (r *SupportRepository) UpsertMacro(ctx context.Context, m support.Macro) (support.Macro, error) {
	saved, err := scanMacro(r.pool.QueryRow(ctx,
		`INSERT INTO support_macros (key, title, body_en, body_ar, body_ku, category_key, active, sort_order)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (key) DO UPDATE SET
		    title = EXCLUDED.title, body_en = EXCLUDED.body_en, body_ar = EXCLUDED.body_ar,
		    body_ku = EXCLUDED.body_ku, category_key = EXCLUDED.category_key, active = EXCLUDED.active,
		    sort_order = EXCLUDED.sort_order, updated_at = CURRENT_TIMESTAMP
		 RETURNING `+macroColumns,
		m.Key, m.Title, m.BodyEn, m.BodyAr, m.BodyKu, nullable(m.CategoryKey), m.Active, m.SortOrder))
	if err != nil {
		return support.Macro{}, fmt.Errorf("upsert macro: %w", err)
	}

	return saved, nil
}
