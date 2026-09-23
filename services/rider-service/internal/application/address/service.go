package address

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Service struct {
	repository Repository
	riders     Riders
	photos     Photos
	ids        IDGenerator
	logger     *slog.Logger
}

func NewService(repository Repository, riders Riders, photos Photos, ids IDGenerator, logger *slog.Logger) *Service {
	if repository == nil || riders == nil || photos == nil || ids == nil || logger == nil {
		panic("address service dependencies are required")
	}

	return &Service{repository: repository, riders: riders, photos: photos, ids: ids, logger: logger}
}

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func checkID(id string, required, notFound error) (string, error) {
	id = strings.ToLower(strings.TrimSpace(id))

	switch {
	case id == "":
		return "", required
	case !uuidShape.MatchString(id):
		return "", notFound
	}

	return id, nil
}

func checkPhotoID(id string) (string, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if id != "" && !uuidShape.MatchString(id) {
		return "", ErrInvalidPhotoID
	}

	return id, nil
}

// Create saves an address. A photo is first held in media-service (which
// checks it is a READY address photo of this rider), and let go again if the
// address cannot be saved.
func (s *Service) Create(ctx context.Context, riderID string, details Details, photoID string) (Address, error) {
	riderID, err := checkID(riderID, ErrRiderNotFound, ErrRiderNotFound)
	if err != nil {
		return Address{}, err
	}

	clean, err := validate(details)
	if err != nil {
		return Address{}, err
	}

	if photoID, err = checkPhotoID(photoID); err != nil {
		return Address{}, err
	}

	if photoID != "" {
		if err := s.hold(ctx, riderID, photoID); err != nil {
			return Address{}, err
		}
	}

	created, err := s.repository.Create(ctx, Address{
		ID:            s.ids.NewID(),
		RiderID:       riderID,
		Kind:          clean.Kind,
		Label:         clean.Label,
		Coordinates:   clean.Coordinates,
		Address:       clean.Address,
		Details:       clean.Details,
		NoteForDriver: clean.NoteForDriver,
		PhotoMediaID:  photoID,
	})
	if err != nil {
		if photoID != "" {
			s.release(ctx, photoID)
		}

		return Address{}, fmt.Errorf("create saved address: %w", err)
	}

	return created, nil
}

// Update replaces the address's details. A new photo is held before the
// change and the old one deleted after it; removePhoto deletes the photo.
func (s *Service) Update(
	ctx context.Context,
	riderID, id string,
	details Details,
	photoID string,
	removePhoto bool,
) (Address, error) {
	current, err := s.Get(ctx, riderID, id)
	if err != nil {
		return Address{}, err
	}

	clean, err := validate(details)
	if err != nil {
		return Address{}, err
	}

	if photoID, err = checkPhotoID(photoID); err != nil {
		return Address{}, err
	}

	if photoID != "" && removePhoto {
		return Address{}, ErrPhotoConflict
	}

	newPhoto, oldPhoto, heldNew := current.PhotoMediaID, "", false

	switch {
	case photoID != "" && photoID != current.PhotoMediaID:
		if err := s.hold(ctx, current.RiderID, photoID); err != nil {
			return Address{}, err
		}

		newPhoto, oldPhoto, heldNew = photoID, current.PhotoMediaID, true

	case removePhoto:
		newPhoto, oldPhoto = "", current.PhotoMediaID
	}

	updated, err := s.repository.Update(ctx, Address{
		ID:            current.ID,
		RiderID:       current.RiderID,
		Kind:          clean.Kind,
		Label:         clean.Label,
		Coordinates:   clean.Coordinates,
		Address:       clean.Address,
		Details:       clean.Details,
		NoteForDriver: clean.NoteForDriver,
		PhotoMediaID:  newPhoto,
	})
	if err != nil {
		if heldNew {
			s.release(ctx, photoID)
		}

		return Address{}, fmt.Errorf("update saved address: %w", err)
	}

	if oldPhoto != "" {
		s.discard(ctx, oldPhoto)
	}

	return updated, nil
}

// Delete removes the address and its photo.
func (s *Service) Delete(ctx context.Context, riderID, id string) error {
	riderID, err := checkID(riderID, ErrRiderNotFound, ErrRiderNotFound)
	if err != nil {
		return err
	}

	id, err = checkID(id, ErrAddressIDRequired, ErrAddressNotFound)
	if err != nil {
		return err
	}

	deleted, err := s.repository.Delete(ctx, riderID, id)
	if err != nil {
		return fmt.Errorf("delete saved address: %w", err)
	}

	if deleted.PhotoMediaID != "" {
		s.discard(ctx, deleted.PhotoMediaID)
	}

	return nil
}

func (s *Service) Get(ctx context.Context, riderID, id string) (Address, error) {
	riderID, err := checkID(riderID, ErrRiderNotFound, ErrRiderNotFound)
	if err != nil {
		return Address{}, err
	}

	id, err = checkID(id, ErrAddressIDRequired, ErrAddressNotFound)
	if err != nil {
		return Address{}, err
	}

	found, err := s.repository.Get(ctx, riderID, id)
	if err != nil {
		return Address{}, fmt.Errorf("get saved address: %w", err)
	}

	return found, nil
}

func (s *Service) List(ctx context.Context, riderID string) ([]Address, error) {
	riderID, err := checkID(riderID, ErrRiderNotFound, ErrRiderNotFound)
	if err != nil {
		return nil, err
	}

	found, err := s.repository.List(ctx, riderID)
	if err != nil {
		return nil, fmt.Errorf("list saved addresses: %w", err)
	}

	return found, nil
}

func (s *Service) hold(ctx context.Context, riderID, photoID string) error {
	identityID, err := s.riders.IdentityOf(ctx, riderID)
	if err != nil {
		return err
	}

	return s.photos.Hold(ctx, photoID, identityID)
}

// release and discard only log a failure: the address is already saved (or
// not), and a photo left behind is a stray file, not a broken address.
func (s *Service) release(ctx context.Context, photoID string) {
	if err := s.photos.Release(ctx, photoID); err != nil {
		s.logger.Warn("failed to release an address photo", "media_id", photoID, "error", err)
	}
}

func (s *Service) discard(ctx context.Context, photoID string) {
	if err := s.photos.Discard(ctx, photoID); err != nil {
		s.logger.Warn("failed to delete an address photo", "media_id", photoID, "error", err)
	}
}

func validate(d Details) (Details, error) {
	if !d.Kind.Valid() {
		return Details{}, ErrInvalidKind
	}

	label := strings.TrimSpace(d.Label)

	switch {
	case d.Kind == KindOther && label == "":
		return Details{}, ErrLabelRequired
	case utf8.RuneCountInString(label) > MaxLabelLength:
		return Details{}, ErrLabelTooLong
	}

	text := []struct {
		value *string
		max   int
		err   error
	}{
		{&d.Address, MaxAddressLength, ErrAddressTooLong},
		{&d.Details, MaxDetailsLength, ErrDetailsTooLong},
		{&d.NoteForDriver, MaxNoteLength, ErrNoteTooLong},
	}

	for _, t := range text {
		*t.value = strings.TrimSpace(*t.value)
		if utf8.RuneCountInString(*t.value) > t.max {
			return Details{}, t.err
		}
	}

	c := d.Coordinates

	switch {
	case c == (Coordinates{}):
		return Details{}, ErrPointRequired
	case c.Latitude < -90 || c.Latitude > 90:
		return Details{}, ErrInvalidLatitude
	case c.Longitude < -180 || c.Longitude > 180:
		return Details{}, ErrInvalidLongitude
	}

	d.Label = label

	return d, nil
}
