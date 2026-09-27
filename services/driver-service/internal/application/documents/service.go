package documents

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/driver"
)

var (
	uuidShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	codeShape = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
)

const (
	defaultPageSize = 20
	maxPageSize     = 100
	pageTokenPrefix = "q1:"
)

// Config is how documents are judged in time.
type Config struct {
	// Location is the deployment's time zone: a document is valid through the
	// whole of its last day there.
	Location *time.Location
	// ReminderDays are the days before expiry a reminder goes out, e.g. 30, 7, 1.
	ReminderDays []int
}

type Service struct {
	repository Repository
	drivers    Drivers
	media      Media
	ids        IDGenerator
	clock      Clock
	config     Config
	logger     *slog.Logger
}

var _ driver.ComplianceChecker = (*Service)(nil)

func NewService(
	repository Repository,
	drivers Drivers,
	media Media,
	ids IDGenerator,
	clock Clock,
	config Config,
	logger *slog.Logger,
) *Service {
	switch {
	case repository == nil, drivers == nil, media == nil, ids == nil, clock == nil, logger == nil:
		panic("document service dependencies are required")
	case config.Location == nil:
		panic("document time zone is required")
	}

	days := slices.Clone(config.ReminderDays)
	slices.Sort(days)
	config.ReminderDays = slices.Compact(days)

	return &Service{
		repository: repository,
		drivers:    drivers,
		media:      media,
		ids:        ids,
		clock:      clock,
		config:     config,
		logger:     logger,
	}
}

// Today is the calendar day in the deployment's time zone.
func (s *Service) Today() Date {
	return DateOf(s.clock.Now(), s.config.Location)
}

// --- types -----------------------------------------------------------------

// ListTypes returns the active types, or every type for staff.
func (s *Service) ListTypes(ctx context.Context, includeInactive bool) ([]Type, error) {
	types, err := s.repository.ListTypes(ctx, includeInactive)
	if err != nil {
		return nil, fmt.Errorf("list document types: %w", err)
	}

	return types, nil
}

// UpsertType creates or changes a type.
func (s *Service) UpsertType(ctx context.Context, t Type) (Type, error) {
	t.Code = strings.TrimSpace(t.Code)
	if !codeShape.MatchString(t.Code) {
		return Type{}, ErrInvalidTypeCode
	}

	t.NameEN = strings.TrimSpace(t.NameEN)
	t.NameAR = strings.TrimSpace(t.NameAR)
	t.NameKU = strings.TrimSpace(t.NameKU)

	for _, name := range []string{t.NameEN, t.NameAR, t.NameKU} {
		if name == "" || utf8.RuneCountInString(name) > maxNameLength {
			return Type{}, ErrInvalidType
		}
	}

	if !t.MediaPurpose.Valid() || t.SortOrder < 0 || t.SortOrder > maxSortOrder {
		return Type{}, ErrInvalidType
	}

	saved, err := s.repository.UpsertType(ctx, t)
	if err != nil {
		return Type{}, fmt.Errorf("save document type: %w", err)
	}

	return saved, nil
}

// --- the driver's side -----------------------------------------------------

type SubmitInput struct {
	DriverID  string
	TypeCode  string
	MediaID   string
	Number    string
	ExpiresOn string
}

// Submit hands an uploaded file in as a document of a type, to be reviewed.
// The file is held in media-service first (which checks it is a READY upload
// of this driver, of the type's purpose) and let go again if it cannot be saved.
func (s *Service) Submit(ctx context.Context, in SubmitInput) (Document, error) {
	driverID := strings.ToLower(strings.TrimSpace(in.DriverID))
	if !uuidShape.MatchString(driverID) {
		return Document{}, ErrDriverNotFound
	}

	docType, err := s.activeType(ctx, in.TypeCode)
	if err != nil {
		return Document{}, err
	}

	mediaID, err := checkMediaID(in.MediaID)
	if err != nil {
		return Document{}, err
	}

	today := s.Today()

	number, expiresOn, err := s.details(docType, in.Number, in.ExpiresOn, today)
	if err != nil {
		return Document{}, err
	}

	if docType.RequiresNumber && number == "" {
		return Document{}, ErrNumberRequired
	}

	if docType.RequiresExpiry && expiresOn.IsZero() {
		return Document{}, ErrExpiryRequired
	}

	identityID, err := s.drivers.IdentityOf(ctx, driverID)
	if err != nil {
		return Document{}, err
	}

	if err := s.media.Hold(ctx, mediaID, identityID, docType.MediaPurpose); err != nil {
		return Document{}, err
	}

	created, superseded, err := s.repository.Submit(ctx, Document{
		ID:        s.ids.NewID(),
		DriverID:  driverID,
		TypeCode:  docType.Code,
		MediaID:   mediaID,
		Number:    number,
		ExpiresOn: expiresOn,
		Status:    StatusPending,
	})
	if err != nil {
		// A file already backing another document stays held for that one.
		if !errors.Is(err, ErrMediaAlreadyUsed) {
			s.release(ctx, mediaID)
		}

		return Document{}, fmt.Errorf("submit document: %w", err)
	}

	s.discardAll(ctx, superseded)

	return created, nil
}

// details checks the number and expiry a type asks for; what it does not ask
// for is dropped.
func (s *Service) details(docType Type, rawNumber, rawExpiry string, today Date) (string, Date, error) {
	number := ""

	if docType.RequiresNumber && strings.TrimSpace(rawNumber) != "" {
		normalized, err := NormalizeNumber(rawNumber)
		if err != nil {
			return "", Date{}, err
		}

		number = normalized
	}

	if !docType.RequiresExpiry {
		return number, Date{}, nil
	}

	expiresOn, err := ParseDate(rawExpiry)
	if err != nil {
		return "", Date{}, err
	}

	if !expiresOn.IsZero() {
		if err := checkExpiry(expiresOn, today); err != nil {
			return "", Date{}, err
		}
	}

	return number, expiresOn, nil
}

func checkExpiry(expiresOn, today Date) error {
	switch {
	case !expiresOn.After(today):
		return ErrExpiryNotInFuture
	case expiresOn.After(DateFromTime(today.Time().AddDate(maxExpiryAheadYrs, 0, 0))):
		return ErrExpiryTooFar
	}

	return nil
}

// State is where a driver stands on one document type.
type State string

const (
	StateMissing       State = "missing"
	StatePendingReview State = "pending_review"
	StateRejected      State = "rejected"
	StateApproved      State = "approved"
	StateExpiringSoon  State = "expiring_soon"
	StateExpired       State = "expired"
)

// Requirement is one active type and the driver's documents for it.
type Requirement struct {
	Type     Type
	State    State
	Approved *Document
	Pending  *Document
	Rejected *Document
}

// Overview is everything the driver (or a reviewer) needs about their documents.
type Overview struct {
	Requirements []Requirement
	Compliant    bool
	Missing      []string
	History      []Document
	Today        Date
}

// Overview shows, per active type, where the driver stands.
func (s *Service) Overview(ctx context.Context, driverID string, includeHistory bool) (Overview, error) {
	driverID = strings.ToLower(strings.TrimSpace(driverID))
	if !uuidShape.MatchString(driverID) {
		return Overview{}, ErrDriverNotFound
	}

	if _, err := s.drivers.IdentityOf(ctx, driverID); err != nil {
		return Overview{}, err
	}

	return s.overview(ctx, driverID, includeHistory)
}

func (s *Service) overview(ctx context.Context, driverID string, includeHistory bool) (Overview, error) {
	types, err := s.repository.ListTypes(ctx, false)
	if err != nil {
		return Overview{}, fmt.Errorf("list document types: %w", err)
	}

	docs, err := s.repository.ListByDriver(ctx, driverID, includeHistory)
	if err != nil {
		return Overview{}, fmt.Errorf("list documents: %w", err)
	}

	today := s.Today()
	overview := Overview{Compliant: true, Today: today}

	for _, t := range types {
		req := s.requirement(t, docs, today)
		overview.Requirements = append(overview.Requirements, req)

		if t.Required && req.State != StateApproved && req.State != StateExpiringSoon {
			overview.Compliant = false
			overview.Missing = append(overview.Missing, t.Code)
		}
	}

	if includeHistory {
		overview.History = docs
	}

	return overview, nil
}

// requirement picks, from docs (newest first), the ones in force for t.
func (s *Service) requirement(t Type, docs []Document, today Date) Requirement {
	req := Requirement{Type: t}

	for i := range docs {
		d := &docs[i]
		if d.TypeCode != t.Code {
			continue
		}

		switch {
		case d.Status == StatusApproved && req.Approved == nil:
			req.Approved = d
		case d.Status == StatusPending && req.Pending == nil:
			req.Pending = d
		case d.Status == StatusRejected && req.Rejected == nil:
			req.Rejected = d
		}
	}

	switch {
	case req.Approved != nil && req.Approved.ExpiredOn(today):
		req.State = StateExpired
	case req.Approved != nil && !req.Approved.ExpiresOn.IsZero() &&
		today.DaysUntil(req.Approved.ExpiresOn) <= s.reminderWindow():
		req.State = StateExpiringSoon
	case req.Approved != nil:
		req.State = StateApproved
	case req.Pending != nil:
		req.State = StatePendingReview
	case req.Rejected != nil:
		req.State = StateRejected
	default:
		req.State = StateMissing
	}

	// A rejection older than what is in force or waiting is history.
	if req.Rejected != nil && req.Pending != nil && req.Rejected.CreatedAt.Before(req.Pending.CreatedAt) {
		req.Rejected = nil
	}

	return req
}

func (s *Service) reminderWindow() int {
	if len(s.config.ReminderDays) == 0 {
		return 0
	}

	return s.config.ReminderDays[len(s.config.ReminderDays)-1]
}

// CheckCompliance tells driver-service whether the driver may be approved or go online.
func (s *Service) CheckCompliance(ctx context.Context, driverID string) (driver.Compliance, error) {
	driverID = strings.ToLower(strings.TrimSpace(driverID))
	if !uuidShape.MatchString(driverID) {
		return driver.Compliance{}, driver.ErrDriverNotFound
	}

	overview, err := s.overview(ctx, driverID, false)
	if err != nil {
		return driver.Compliance{}, err
	}

	return driver.Compliance{Compliant: overview.Compliant, Missing: overview.Missing}, nil
}

// --- review ------------------------------------------------------------------

type ApproveInput struct {
	DocumentID string
	Number     string
	ExpiresOn  string
	ReviewedBy string
}

// Approve approves a pending document, with the reviewer's corrections.
func (s *Service) Approve(ctx context.Context, in ApproveInput) (Document, error) {
	doc, docType, err := s.load(ctx, in.DocumentID)
	if err != nil {
		return Document{}, err
	}

	if doc.Status != StatusPending {
		return Document{}, ErrDocumentNotPending
	}

	today := s.Today()

	number, expiresOn, err := s.details(docType, in.Number, in.ExpiresOn, today)
	if err != nil {
		return Document{}, err
	}

	if number == "" {
		number = doc.Number
	}

	if expiresOn.IsZero() {
		expiresOn = doc.ExpiresOn
	}

	if docType.RequiresNumber && number == "" {
		return Document{}, ErrNumberRequired
	}

	if docType.RequiresExpiry {
		if expiresOn.IsZero() {
			return Document{}, ErrExpiryRequired
		}

		// Checked again: the driver's date may have passed while it waited.
		if err := checkExpiry(expiresOn, today); err != nil {
			return Document{}, err
		}
	}

	approved, superseded, err := s.repository.Approve(ctx, ApproveRecord{
		DocumentID: doc.ID,
		Number:     number,
		ExpiresOn:  expiresOn,
		ReviewedBy: reviewer(in.ReviewedBy),
		Type:       docType,
	})
	if err != nil {
		return Document{}, fmt.Errorf("approve document: %w", err)
	}

	s.discardAll(ctx, superseded)

	return approved, nil
}

type RejectInput struct {
	DocumentID string
	Reason     string
	ReviewedBy string
}

// Reject turns down a pending document, or withdraws an approved one.
func (s *Service) Reject(ctx context.Context, in RejectInput) (Document, error) {
	reason := strings.TrimSpace(in.Reason)

	switch {
	case reason == "":
		return Document{}, ErrReasonRequired
	case utf8.RuneCountInString(reason) > maxReasonLength:
		return Document{}, ErrReasonTooLong
	}

	doc, docType, err := s.load(ctx, in.DocumentID)
	if err != nil {
		return Document{}, err
	}

	if doc.Status != StatusPending && doc.Status != StatusApproved {
		return Document{}, ErrDocumentNotReviewable
	}

	rejected, err := s.repository.Reject(ctx, RejectRecord{
		DocumentID:     doc.ID,
		ExpectedStatus: doc.Status,
		Reason:         reason,
		ReviewedBy:     reviewer(in.ReviewedBy),
		TakeOffline:    doc.Status == StatusApproved && docType.Required && docType.Active,
		Type:           docType,
	})
	if err != nil {
		return Document{}, fmt.Errorf("reject document: %w", err)
	}

	return rejected, nil
}

type PendingPage struct {
	Items         []PendingItem
	NextPageToken string
}

// Pending is the review queue, oldest first.
func (s *Service) Pending(ctx context.Context, pageSize int, pageToken string) (PendingPage, error) {
	switch {
	case pageSize < 0:
		return PendingPage{}, ErrInvalidPageSize
	case pageSize == 0:
		pageSize = defaultPageSize
	case pageSize > maxPageSize:
		pageSize = maxPageSize
	}

	query := PendingQuery{Limit: pageSize + 1}

	if pageToken != "" {
		after, id, err := decodePageToken(pageToken)
		if err != nil {
			return PendingPage{}, err
		}

		query.AfterCreatedAt, query.AfterID = after, id
	}

	items, err := s.repository.ListPending(ctx, query)
	if err != nil {
		return PendingPage{}, fmt.Errorf("list pending documents: %w", err)
	}

	page := PendingPage{Items: items}

	if len(items) > pageSize {
		last := items[pageSize-1].Document
		page.Items = items[:pageSize]
		page.NextPageToken = encodePageToken(last.CreatedAt, last.ID)
	}

	return page, nil
}

// --- expiry ------------------------------------------------------------------

// RunExpiry does one pass of reminders and expiries for today.
func (s *Service) RunExpiry(ctx context.Context, limit int) (ExpiryRound, error) {
	round, err := s.repository.RunExpiry(ctx, s.Today(), s.config.ReminderDays, limit)
	if err != nil {
		return ExpiryRound{}, fmt.Errorf("run document expiry: %w", err)
	}

	return round, nil
}

// ReminderThreshold is the reminder a document daysLeft from expiry is due:
// the smallest threshold not below daysLeft. ok is false outside every window.
func ReminderThreshold(daysLeft int, thresholds []int) (int, bool) {
	best, ok := 0, false

	for _, t := range thresholds {
		if t >= daysLeft && (!ok || t < best) {
			best, ok = t, true
		}
	}

	return best, ok
}

// --- helpers -------------------------------------------------------------------

func (s *Service) activeType(ctx context.Context, code string) (Type, error) {
	code = strings.TrimSpace(code)
	if !codeShape.MatchString(code) {
		return Type{}, ErrTypeNotFound
	}

	t, err := s.repository.GetType(ctx, code)
	if err != nil {
		return Type{}, err
	}

	if !t.Active {
		return Type{}, ErrTypeNotFound
	}

	return t, nil
}

func (s *Service) load(ctx context.Context, rawID string) (Document, Type, error) {
	id := strings.ToLower(strings.TrimSpace(rawID))
	if !uuidShape.MatchString(id) {
		return Document{}, Type{}, ErrDocumentNotFound
	}

	doc, err := s.repository.Get(ctx, id)
	if err != nil {
		return Document{}, Type{}, err
	}

	docType, err := s.repository.GetType(ctx, doc.TypeCode)
	if err != nil {
		return Document{}, Type{}, fmt.Errorf("load the document's type: %w", err)
	}

	return doc, docType, nil
}

func checkMediaID(raw string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(raw))

	switch {
	case id == "":
		return "", ErrMediaIDRequired
	case !uuidShape.MatchString(id):
		return "", ErrInvalidMediaID
	}

	return id, nil
}

func reviewer(identityID string) string {
	id := strings.ToLower(strings.TrimSpace(identityID))
	if !uuidShape.MatchString(id) {
		return ""
	}

	return id
}

// release and discard only log a failure: the document is saved (or not), and
// a file left behind is a stray file, not a broken document.
func (s *Service) release(ctx context.Context, mediaID string) {
	if err := s.media.Release(ctx, mediaID); err != nil {
		s.logger.Warn("failed to release a document file", "media_id", mediaID, "error", err)
	}
}

func (s *Service) discardAll(ctx context.Context, docs []Document) {
	for _, d := range docs {
		if err := s.media.Discard(ctx, d.MediaID); err != nil {
			s.logger.Warn("failed to delete a superseded document file", "media_id", d.MediaID, "error", err)
		}
	}
}

func encodePageToken(createdAt time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(pageTokenPrefix + createdAt.UTC().Format(time.RFC3339Nano) + "|" + id))
}

func decodePageToken(token string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, "", ErrInvalidPageToken
	}

	rest, ok := strings.CutPrefix(string(raw), pageTokenPrefix)
	if !ok {
		return time.Time{}, "", ErrInvalidPageToken
	}

	at, id, ok := strings.Cut(rest, "|")
	if !ok || !uuidShape.MatchString(id) {
		return time.Time{}, "", ErrInvalidPageToken
	}

	createdAt, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, "", ErrInvalidPageToken
	}

	return createdAt, id, nil
}
