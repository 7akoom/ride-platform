// Package dataexport is "Download your data", as the big ride apps have it:
// the person asks, a maker gathers what every service keeps about them (each
// answers ExportPersonalData) into a ZIP of JSON files, media-service keeps it
// as the person's file, and they are told it is ready. It is kept for a week
// (KeepFor) and then deleted; one request a day (MinInterval).
package dataexport

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

var (
	// ErrTooSoon: the last request was less than MinInterval ago.
	ErrTooSoon = errors.New("a data export was asked for recently; try again later")
	// ErrNotFound: no such export of this person.
	ErrNotFound = errors.New("data export not found")
	// ErrNotReady: the export is not (or no longer) ready to download.
	ErrNotReady = errors.New("the data export is not ready to download")
)

// Status of an export.
type Status string

const (
	StatusPending Status = "pending"
	StatusReady   Status = "ready"
	StatusFailed  Status = "failed"
	StatusExpired Status = "expired"
)

// Export is one request and, once made, its file.
type Export struct {
	ID          string
	IdentityID  string
	Status      Status
	RequestedAt time.Time
	ReadyAt     *time.Time
	ExpiresAt   *time.Time
	MediaID     string
	SizeBytes   int64
	Attempts    int
}

// Section is one file of the ZIP.
type Section struct {
	Name    string
	Content []byte
}

// Profiles are the person's rider and driver profiles (empty when none).
type Profiles struct {
	RiderID  string
	DriverID string
}

// Source is a service that keeps data about people.
type Source struct {
	Name   string
	Export func(ctx context.Context, identityID string, profiles Profiles) ([]Section, error)
}

// ProfileFinder finds the person's profiles.
type ProfileFinder interface {
	Profiles(ctx context.Context, identityID string) (Profiles, error)
}

// Files keeps the ZIP in media-service.
type Files interface {
	Store(ctx context.Context, ownerIdentityID string, zip []byte) (mediaID string, err error)
	DownloadURL(ctx context.Context, mediaID string) (string, time.Time, error)
	Delete(ctx context.Context, mediaID string) error
}

// ReadyInput marks an export made.
type ReadyInput struct {
	ID        string
	MediaID   string
	SizeBytes int64
	ReadyAt   time.Time
	ExpiresAt time.Time
	Profiles  Profiles
}

// Store keeps exports.
type Store interface {
	Create(ctx context.Context, export Export) error
	// List returns the person's latest exports, newest first.
	List(ctx context.Context, identityID string, limit int) ([]Export, error)
	Find(ctx context.Context, identityID, exportID string) (Export, error)
	// ClaimPending takes up to limit pending exports due now, holding each
	// until now+lease.
	ClaimPending(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Export, error)
	// MarkReady also writes identity.data_export_ready.
	MarkReady(ctx context.Context, input ReadyInput) error
	// MarkAttemptFailed counts a failure; with retryAt nil the export fails.
	MarkAttemptFailed(ctx context.Context, id string, retryAt *time.Time, reason string) error
	ListExpired(ctx context.Context, now time.Time, limit int) ([]Export, error)
	MarkExpired(ctx context.Context, id string) error
}

// IDs makes export ids.
type IDs interface{ NewID() string }

// Clock is the time source.
type Clock interface{ Now() time.Time }

// Settings are the limits and pace.
type Settings struct {
	MinInterval time.Duration
	KeepFor     time.Duration
	MaxAttempts int
	RetryAfter  time.Duration
}

type Service struct {
	store    Store
	profiles ProfileFinder
	sources  []Source
	files    Files
	ids      IDs
	clock    Clock
	settings Settings
	logger   *slog.Logger
}

func NewService(store Store, profiles ProfileFinder, sources []Source, files Files, ids IDs, clock Clock, settings Settings, logger *slog.Logger) *Service {
	if store == nil || profiles == nil || files == nil || ids == nil || clock == nil || logger == nil {
		panic("data export dependencies are required")
	}

	if settings.MinInterval <= 0 || settings.KeepFor <= 0 || settings.MaxAttempts <= 0 || settings.RetryAfter <= 0 {
		panic("data export settings are required")
	}

	return &Service{store: store, profiles: profiles, sources: sources, files: files, ids: ids, clock: clock, settings: settings, logger: logger}
}

// NextRequestAt is when the person may ask again (zero: now).
func (s *Service) nextRequestAt(latest []Export) time.Time {
	for _, e := range latest {
		if e.Status == StatusFailed {
			continue
		}

		next := e.RequestedAt.Add(s.settings.MinInterval)
		if next.After(s.clock.Now()) {
			return next
		}

		return time.Time{}
	}

	return time.Time{}
}

// Request asks for a new export.
func (s *Service) Request(ctx context.Context, identityID string) (Export, error) {
	identityID = strings.TrimSpace(identityID)

	latest, err := s.store.List(ctx, identityID, 5)
	if err != nil {
		return Export{}, err
	}

	if !s.nextRequestAt(latest).IsZero() {
		return Export{}, ErrTooSoon
	}

	now := s.clock.Now()
	created := Export{ID: s.ids.NewID(), IdentityID: identityID, Status: StatusPending, RequestedAt: now}

	if err := s.store.Create(ctx, created); err != nil {
		return Export{}, err
	}

	return created, nil
}

// List returns the person's latest exports and when they may ask again.
func (s *Service) List(ctx context.Context, identityID string) ([]Export, time.Time, error) {
	latest, err := s.store.List(ctx, strings.TrimSpace(identityID), 5)
	if err != nil {
		return nil, time.Time{}, err
	}

	return latest, s.nextRequestAt(latest), nil
}

// Download is a short-lived link to a ready export of the person.
func (s *Service) Download(ctx context.Context, identityID, exportID string) (string, time.Time, error) {
	found, err := s.store.Find(ctx, strings.TrimSpace(identityID), strings.TrimSpace(exportID))
	if err != nil {
		return "", time.Time{}, err
	}

	if found.Status != StatusReady || found.ExpiresAt == nil || !found.ExpiresAt.After(s.clock.Now()) {
		return "", time.Time{}, ErrNotReady
	}

	return s.files.DownloadURL(ctx, found.MediaID)
}

const (
	makeBatch = 5
	makeLease = 10 * time.Minute
)

// RunMaker makes pending exports and removes expired ones, every interval.
func (s *Service) RunMaker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		s.MakePending(ctx)
		s.RemoveExpired(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// MakePending makes the exports due now; it returns how many it finished.
func (s *Service) MakePending(ctx context.Context) int {
	claimed, err := s.store.ClaimPending(ctx, s.clock.Now(), makeLease, makeBatch)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to claim data exports", "error", err)

		return 0
	}

	made := 0

	for _, export := range claimed {
		if ctx.Err() != nil {
			return made
		}

		if err := s.make(ctx, export); err != nil {
			s.logger.WarnContext(ctx, "a data export could not be made", "export_id", export.ID, "attempt", export.Attempts+1, "error", err)

			var retryAt *time.Time
			if export.Attempts+1 < s.settings.MaxAttempts {
				at := s.clock.Now().Add(s.settings.RetryAfter)
				retryAt = &at
			}

			if markErr := s.store.MarkAttemptFailed(ctx, export.ID, retryAt, err.Error()); markErr != nil {
				s.logger.ErrorContext(ctx, "failed to record a data export failure", "export_id", export.ID, "error", markErr)
			}

			continue
		}

		made++
	}

	return made
}

func (s *Service) make(ctx context.Context, export Export) error {
	profiles, err := s.profiles.Profiles(ctx, export.IdentityID)
	if err != nil {
		return fmt.Errorf("find the profiles: %w", err)
	}

	var sections []Section

	for _, source := range s.sources {
		got, err := source.Export(ctx, export.IdentityID, profiles)
		if err != nil {
			return fmt.Errorf("%s: %w", source.Name, err)
		}

		sections = append(sections, got...)
	}

	archive, err := BuildZIP(export, sections)
	if err != nil {
		return err
	}

	mediaID, err := s.files.Store(ctx, export.IdentityID, archive)
	if err != nil {
		return fmt.Errorf("store the file: %w", err)
	}

	now := s.clock.Now()

	if err := s.store.MarkReady(ctx, ReadyInput{
		ID: export.ID, MediaID: mediaID, SizeBytes: int64(len(archive)),
		ReadyAt: now, ExpiresAt: now.Add(s.settings.KeepFor), Profiles: profiles,
	}); err != nil {
		if deleteErr := s.files.Delete(context.WithoutCancel(ctx), mediaID); deleteErr != nil {
			s.logger.WarnContext(ctx, "failed to delete an unused data export file", "media_id", mediaID, "error", deleteErr)
		}

		return fmt.Errorf("mark ready: %w", err)
	}

	s.logger.InfoContext(ctx, "data export ready", "export_id", export.ID, "size_bytes", len(archive))

	return nil
}

// RemoveExpired deletes the files of exports past their time.
func (s *Service) RemoveExpired(ctx context.Context) int {
	expired, err := s.store.ListExpired(ctx, s.clock.Now(), 20)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to list expired data exports", "error", err)

		return 0
	}

	removed := 0

	for _, export := range expired {
		if err := s.files.Delete(ctx, export.MediaID); err != nil {
			s.logger.WarnContext(ctx, "failed to delete an expired data export file", "export_id", export.ID, "error", err)

			continue
		}

		if err := s.store.MarkExpired(ctx, export.ID); err != nil {
			s.logger.ErrorContext(ctx, "failed to mark a data export expired", "export_id", export.ID, "error", err)

			continue
		}

		removed++
	}

	return removed
}

const readme = `YOUR DATA / بياناتك
====================

This archive holds what the service keeps about your account, as JSON files
(one per subject), readable by people and by other programs. It was made on
%s for account %s.

يحتوي هذا الملف على ما يحتفظ به التطبيق عن حسابك، بصيغة JSON (ملف لكل
موضوع)، يمكن قراءتها مباشرة أو بأي برنامج. أُنشئ بتاريخ %s للحساب %s.

Files / الملفات:
%s`

// BuildZIP puts the sections in a ZIP with a README listing them.
func BuildZIP(export Export, sections []Section) ([]byte, error) {
	var (
		buffer bytes.Buffer
		names  strings.Builder
	)

	for _, section := range sections {
		names.WriteString("  - " + section.Name + "\n")
	}

	writer := zip.NewWriter(&buffer)
	stamp := export.RequestedAt.UTC().Format(time.RFC3339)

	files := append([]Section{{
		Name:    "README.txt",
		Content: []byte(fmt.Sprintf(readme, stamp, export.IdentityID, stamp, export.IdentityID, names.String())),
	}}, sections...)

	for _, file := range files {
		header := &zip.FileHeader{Name: file.Name, Method: zip.Deflate, Modified: export.RequestedAt.UTC()}

		w, err := writer.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("add %s to the ZIP: %w", file.Name, err)
		}

		if _, err := w.Write(file.Content); err != nil {
			return nil, fmt.Errorf("write %s to the ZIP: %w", file.Name, err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close the ZIP: %w", err)
	}

	return buffer.Bytes(), nil
}
