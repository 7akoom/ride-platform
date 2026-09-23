package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

// Settings are the service's timings and limits.
type Settings struct {
	// How long an upload URL accepts the bytes.
	UploadURLTTL time.Duration
	// How long a download URL works.
	DownloadURLTTL time.Duration
	// A reservation not completed within this is dropped.
	PendingTTL time.Duration
	// Reservations one person may have open at once.
	MaxPendingPerOwner int
	// Files checked at the same time (each can take a few hundred MB decoded).
	MaxConcurrentInspections int
}

// clockSkew is added to the upload URL lifetime before an incoming object is
// considered safe to clear: the object store's clock may run a little behind.
const clockSkew = 2 * time.Minute

type Service struct {
	repository  Repository
	store       ObjectStore
	idGenerator IDGenerator
	clock       Clock
	settings    Settings
	inspections chan struct{}
	logger      *slog.Logger
}

func NewService(
	repository Repository,
	store ObjectStore,
	idGenerator IDGenerator,
	clock Clock,
	settings Settings,
	logger *slog.Logger,
) *Service {
	if repository == nil || store == nil || idGenerator == nil || clock == nil || logger == nil {
		panic("media service dependencies are required")
	}

	if settings.UploadURLTTL <= 0 || settings.DownloadURLTTL <= 0 || settings.PendingTTL <= settings.UploadURLTTL ||
		settings.MaxPendingPerOwner <= 0 || settings.MaxConcurrentInspections <= 0 {
		panic("media service settings are not valid")
	}

	return &Service{
		repository:  repository,
		store:       store,
		idGenerator: idGenerator,
		clock:       clock,
		settings:    settings,
		inspections: make(chan struct{}, settings.MaxConcurrentInspections),
		logger:      logger,
	}
}

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// CreateUploadInput reserves a file.
type CreateUploadInput struct {
	OwnerIdentityID string
	Purpose         Purpose
	ContentType     string
	SizeBytes       int64
}

// CreateUpload reserves a file and returns where to send it. The URL accepts
// exactly the declared type and size, and points at the incoming object, not
// at the one downloads read.
func (s *Service) CreateUpload(ctx context.Context, input CreateUploadInput) (Media, UploadTicket, error) {
	owner := strings.ToLower(strings.TrimSpace(input.OwnerIdentityID))
	if !uuidShape.MatchString(owner) {
		return Media{}, UploadTicket{}, ErrOwnerRequired
	}

	policy, ok := PolicyFor(input.Purpose)
	if !ok {
		return Media{}, UploadTicket{}, ErrInvalidPurpose
	}

	contentType := strings.ToLower(strings.TrimSpace(input.ContentType))
	if !policy.allows(contentType) {
		return Media{}, UploadTicket{}, ErrTypeNotAllowed
	}

	if input.SizeBytes <= 0 || input.SizeBytes > policy.MaxBytes {
		return Media{}, UploadTicket{}, ErrInvalidSize
	}

	pending, err := s.repository.CountPending(ctx, owner)
	if err != nil {
		return Media{}, UploadTicket{}, fmt.Errorf("count pending uploads: %w", err)
	}

	if pending >= s.settings.MaxPendingPerOwner {
		return Media{}, UploadTicket{}, ErrTooManyPending
	}

	now := s.clock.Now().UTC()
	id := s.idGenerator.NewID()

	created := Media{
		ID:                  id,
		OwnerIdentityID:     owner,
		Purpose:             input.Purpose,
		Status:              StatusPending,
		DeclaredContentType: contentType,
		DeclaredSize:        input.SizeBytes,
		ObjectKey:           fmt.Sprintf("%s/%s/%s", input.Purpose, now.Format("2006/01"), id),
		CreatedAt:           now,
	}

	if err := s.repository.Create(ctx, created); err != nil {
		return Media{}, UploadTicket{}, fmt.Errorf("create media record: %w", err)
	}

	url, headers, expires := s.store.PresignPut(created.UploadKey(), contentType, input.SizeBytes, s.settings.UploadURLTTL)

	return created, UploadTicket{URL: url, Method: "PUT", Headers: headers, ExpiresAt: expires}, nil
}

// CompleteUpload checks the uploaded bytes. A file that passes is copied (and
// re-encoded when it is an image) to where downloads read it, and is READY;
// one that fails is deleted and REJECTED. Completing a READY file again
// returns it unchanged.
func (s *Service) CompleteUpload(ctx context.Context, id string) (Media, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return Media{}, err
	}

	switch current.Status {
	case StatusReady:
		return current, nil
	case StatusPending:
	default:
		return Media{}, ErrInvalidState
	}

	info, err := s.store.Stat(ctx, current.UploadKey())
	if err != nil {
		return Media{}, err
	}

	policy, _ := PolicyFor(current.Purpose)

	if info.Size != current.DeclaredSize || info.Size > policy.MaxBytes {
		return s.reject(ctx, current, "the file size does not match the declared size")
	}

	select {
	case s.inspections <- struct{}{}:
		defer func() { <-s.inspections }()
	case <-ctx.Done():
		return Media{}, ctx.Err()
	}

	data, err := s.store.Read(ctx, current.UploadKey(), policy.MaxBytes)
	if errors.Is(err, ErrObjectTooLarge) {
		return s.reject(ctx, current, "the file is larger than allowed")
	}

	if err != nil {
		return Media{}, err
	}

	result := inspect(current.DeclaredContentType, data)
	if result.rejection != "" {
		return s.reject(ctx, current, result.rejection)
	}

	// Only the bytes that were checked are kept.
	if err := s.store.Put(ctx, current.ObjectKey, result.contentType, result.data); err != nil {
		return Media{}, fmt.Errorf("store the checked file: %w", err)
	}

	s.deleteIncoming(ctx, current)

	ready, err := s.repository.MarkReady(ctx, current.ID, ReadyInput{
		ContentType: result.contentType,
		SizeBytes:   int64(len(result.data)),
		SHA256:      result.sha256,
		Width:       result.width,
		Height:      result.height,
		At:          s.clock.Now().UTC(),
	})
	if errors.Is(err, ErrInvalidState) {
		// A concurrent call decided first; report what it decided, and do not
		// leave behind the copy just written if the file was deleted meanwhile.
		latest, findErr := s.repository.FindByID(ctx, current.ID)
		if findErr != nil {
			return Media{}, findErr
		}

		if latest.Status == StatusDeleted || latest.Status == StatusExpired {
			if err := s.store.Delete(ctx, current.ObjectKey); err != nil {
				s.logger.Warn("failed to delete the copy of a file deleted meanwhile", "media_id", current.ID, "error", err)
			}
		}

		return latest, nil
	}

	return ready, err
}

func (s *Service) reject(ctx context.Context, current Media, reason string) (Media, error) {
	if err := s.store.Delete(ctx, current.UploadKey()); err != nil {
		return Media{}, fmt.Errorf("delete a rejected file: %w", err)
	}

	rejected, err := s.repository.MarkRejected(ctx, current.ID, reason, s.clock.Now().UTC())
	if errors.Is(err, ErrInvalidState) {
		return s.repository.FindByID(ctx, current.ID)
	}

	return rejected, err
}

// deleteIncoming removes the uploaded original once it is no longer needed.
// A failure is only logged: the clearing sweep deletes it later anyway.
func (s *Service) deleteIncoming(ctx context.Context, current Media) {
	if err := s.store.Delete(ctx, current.UploadKey()); err != nil {
		s.logger.Warn("failed to delete an incoming object", "media_id", current.ID, "error", err)
	}
}

// Get reads a media record.
func (s *Service) Get(ctx context.Context, id string) (Media, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !uuidShape.MatchString(id) {
		return Media{}, ErrInvalidID
	}

	return s.repository.FindByID(ctx, id)
}

// DownloadURL returns a short-lived URL to read a READY file.
func (s *Service) DownloadURL(ctx context.Context, id string) (string, time.Time, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return "", time.Time{}, err
	}

	if current.Status != StatusReady {
		return "", time.Time{}, ErrNotReady
	}

	url, expires := s.store.PresignGet(current.ObjectKey, s.settings.DownloadURLTTL)

	return url, expires, nil
}

// Delete removes a file its owner no longer wants, unless a service holds it.
// Deleting a file already deleted is not an error, and removes its objects
// again in case an earlier attempt stopped half way.
func (s *Service) Delete(ctx context.Context, id string) error {
	current, err := s.Get(ctx, id)
	if err != nil {
		return err
	}

	switch {
	case current.Status == StatusExpired:
		return nil
	case current.Held:
		return ErrHeld
	case current.Status != StatusDeleted:
		// Mark first: once deleted, nothing hands out a URL to it, and a
		// failed object removal below is retried by calling Delete again.
		if _, err := s.repository.MarkDeleted(ctx, current.ID, s.clock.Now().UTC()); err != nil {
			if errors.Is(err, ErrInvalidState) {
				return s.deletedOrHeld(ctx, current.ID)
			}

			return err
		}
	}

	if err := s.store.Delete(ctx, current.ObjectKey); err != nil {
		return fmt.Errorf("delete the object: %w", err)
	}

	s.deleteIncoming(ctx, current)

	return nil
}

// deletedOrHeld explains a lost race in Delete: a concurrent Hold wins with
// ErrHeld, a concurrent Delete or expiry is success.
func (s *Service) deletedOrHeld(ctx context.Context, id string) error {
	latest, err := s.repository.FindByID(ctx, id)
	if err != nil {
		return err
	}

	switch {
	case latest.Status == StatusDeleted || latest.Status == StatusExpired:
		return nil
	case latest.Held:
		return ErrHeld
	default:
		return ErrInvalidState
	}
}

// Hold marks a READY file of that owner and purpose as used by a service, so
// its owner cannot delete it.
func (s *Service) Hold(ctx context.Context, id string, ownerIdentityID string, purpose Purpose) (Media, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return Media{}, err
	}

	if current.OwnerIdentityID != strings.ToLower(strings.TrimSpace(ownerIdentityID)) || current.Purpose != purpose {
		return Media{}, ErrHoldMismatch
	}

	if current.Status != StatusReady {
		return Media{}, ErrNotReady
	}

	if current.Held {
		return current, nil
	}

	return s.repository.SetHeld(ctx, current.ID, true)
}

// Release lets the owner delete a held file again.
func (s *Service) Release(ctx context.Context, id string) (Media, error) {
	current, err := s.Get(ctx, id)
	if err != nil {
		return Media{}, err
	}

	if !current.Held {
		return current, nil
	}

	return s.repository.SetHeld(ctx, current.ID, false)
}

// ExpireStale drops reservations never completed in time, with whatever
// bytes did arrive. It returns how many it dropped.
func (s *Service) ExpireStale(ctx context.Context, limit int) (int, error) {
	stale, err := s.repository.ListStalePending(ctx, s.clock.Now().Add(-s.settings.PendingTTL), limit)
	if err != nil {
		return 0, fmt.Errorf("list stale uploads: %w", err)
	}

	expired := 0

	for _, record := range stale {
		if err := s.store.Delete(ctx, record.UploadKey()); err != nil {
			s.logger.Warn("failed to delete the object of a stale upload", "media_id", record.ID, "error", err)

			continue
		}

		if err := s.repository.MarkExpired(ctx, record.ID, s.clock.Now().UTC()); err != nil && !errors.Is(err, ErrInvalidState) {
			return expired, fmt.Errorf("expire upload %s: %w", record.ID, err)
		}

		expired++
	}

	return expired, nil
}

// ClearUploads deletes the incoming objects of completed, rejected and
// deleted files once their upload URL can no longer write there. A client
// may PUT again with a still-valid URL after completing; this removes that
// copy. It returns how many it cleared.
func (s *Service) ClearUploads(ctx context.Context, limit int) (int, error) {
	cutoff := s.clock.Now().Add(-s.settings.UploadURLTTL - clockSkew)

	records, err := s.repository.ListUploadsToClear(ctx, cutoff, limit)
	if err != nil {
		return 0, fmt.Errorf("list uploads to clear: %w", err)
	}

	cleared := 0

	for _, record := range records {
		if err := s.store.Delete(ctx, record.UploadKey()); err != nil {
			s.logger.Warn("failed to clear an incoming object", "media_id", record.ID, "error", err)

			continue
		}

		if err := s.repository.MarkUploadCleared(ctx, record.ID); err != nil {
			return cleared, fmt.Errorf("mark upload %s cleared: %w", record.ID, err)
		}

		cleared++
	}

	return cleared, nil
}

// RunMaintenance expires stale reservations and clears incoming objects
// every interval until ctx ends.
func (s *Service) RunMaintenance(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	const batch = 100

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if count, err := s.ExpireStale(ctx, batch); err != nil {
				s.logger.Error("expiring stale uploads failed", "error", err)
			} else if count > 0 {
				s.logger.Info("expired stale uploads", "count", count)
			}

			if count, err := s.ClearUploads(ctx, batch); err != nil {
				s.logger.Error("clearing incoming objects failed", "error", err)
			} else if count > 0 {
				s.logger.Info("cleared incoming objects", "count", count)
			}
		}
	}
}
