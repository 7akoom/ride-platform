package address

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

const (
	riderID    = "11111111-1111-4111-8111-111111111111"
	otherRider = "22222222-2222-4222-8222-222222222222"
	identityID = "33333333-3333-4333-8333-333333333333"
	photoA     = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	photoB     = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

type memoryRepository struct {
	byID      map[string]Address
	failWrite error
}

func (m *memoryRepository) Create(_ context.Context, a Address) (Address, error) {
	if m.failWrite != nil {
		return Address{}, m.failWrite
	}

	count := 0

	for _, existing := range m.byID {
		if existing.RiderID != a.RiderID {
			continue
		}

		count++

		if a.Kind != KindOther && existing.Kind == a.Kind {
			return Address{}, ErrKindTaken
		}
	}

	if count >= MaxPerRider {
		return Address{}, ErrTooManyAddresses
	}

	m.byID[a.ID] = a

	return a, nil
}

func (m *memoryRepository) Update(_ context.Context, a Address) (Address, error) {
	if m.failWrite != nil {
		return Address{}, m.failWrite
	}

	current, ok := m.byID[a.ID]
	if !ok || current.RiderID != a.RiderID {
		return Address{}, ErrAddressNotFound
	}

	m.byID[a.ID] = a

	return a, nil
}

func (m *memoryRepository) Get(_ context.Context, rider, id string) (Address, error) {
	found, ok := m.byID[id]
	if !ok || found.RiderID != rider {
		return Address{}, ErrAddressNotFound
	}

	return found, nil
}

func (m *memoryRepository) List(_ context.Context, rider string) ([]Address, error) {
	out := []Address{}

	for _, a := range m.byID {
		if a.RiderID == rider {
			out = append(out, a)
		}
	}

	return out, nil
}

func (m *memoryRepository) Delete(_ context.Context, rider, id string) (Address, error) {
	found, ok := m.byID[id]
	if !ok || found.RiderID != rider {
		return Address{}, ErrAddressNotFound
	}

	delete(m.byID, id)

	return found, nil
}

type fakeRiders struct{}

func (fakeRiders) IdentityOf(_ context.Context, id string) (string, error) {
	if id == riderID {
		return identityID, nil
	}

	return "", ErrRiderNotFound
}

type fakePhotos struct {
	usable    map[string]string // media id -> owner identity
	held      map[string]bool
	discarded []string
	down      bool
}

func (p *fakePhotos) Hold(_ context.Context, mediaID, owner string) error {
	if p.down {
		return ErrPhotoUnavailable
	}

	if p.usable[mediaID] != owner {
		return ErrPhotoNotUsable
	}

	p.held[mediaID] = true

	return nil
}

func (p *fakePhotos) Release(_ context.Context, mediaID string) error {
	delete(p.held, mediaID)

	return nil
}

func (p *fakePhotos) Discard(_ context.Context, mediaID string) error {
	delete(p.held, mediaID)
	p.discarded = append(p.discarded, mediaID)

	return nil
}

type counterIDs struct{ n int }

func (c *counterIDs) NewID() string {
	c.n++

	return "00000000-0000-4000-8000-" + strings.Repeat("0", 11) + string(rune('0'+c.n%10))
}

type rig struct {
	service *Service
	repo    *memoryRepository
	photos  *fakePhotos
}

func newRig() rig {
	r := rig{
		repo: &memoryRepository{byID: map[string]Address{}},
		photos: &fakePhotos{
			usable: map[string]string{photoA: identityID, photoB: identityID},
			held:   map[string]bool{},
		},
	}
	r.service = NewService(r.repo, fakeRiders{}, r.photos, &counterIDs{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	return r
}

func home() Details {
	return Details{
		Kind:          KindHome,
		Coordinates:   Coordinates{Latitude: 36.19, Longitude: 44.01},
		Address:       " Gulan Street, Erbil ",
		Details:       "Building 4, floor 2",
		NoteForDriver: "Blue gate",
	}
}

func TestCreateHoldsThePhotoAndTrims(t *testing.T) {
	r := newRig()

	created, err := r.service.Create(context.Background(), riderID, home(), strings.ToUpper(photoA))
	if err != nil {
		t.Fatal(err)
	}

	if created.Address != "Gulan Street, Erbil" || created.PhotoMediaID != photoA || !r.photos.held[photoA] {
		t.Fatalf("got %+v held=%v", created, r.photos.held)
	}
}

func TestCreateValidates(t *testing.T) {
	cases := map[string]struct {
		change func(*Details)
		want   error
	}{
		"no kind":           {func(d *Details) { d.Kind = "" }, ErrInvalidKind},
		"other needs label": {func(d *Details) { d.Kind = KindOther }, ErrLabelRequired},
		"long label":        {func(d *Details) { d.Label = strings.Repeat("a", 61) }, ErrLabelTooLong},
		"long address":      {func(d *Details) { d.Address = strings.Repeat("ا", 301) }, ErrAddressTooLong},
		"long details":      {func(d *Details) { d.Details = strings.Repeat("a", 201) }, ErrDetailsTooLong},
		"long note":         {func(d *Details) { d.NoteForDriver = strings.Repeat("a", 301) }, ErrNoteTooLong},
		"no point":          {func(d *Details) { d.Coordinates = Coordinates{} }, ErrPointRequired},
		"bad latitude":      {func(d *Details) { d.Coordinates.Latitude = 100 }, ErrInvalidLatitude},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig()
			d := home()
			tc.change(&d)

			if _, err := r.service.Create(context.Background(), riderID, d, photoA); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}

			if len(r.photos.held) != 0 || len(r.repo.byID) != 0 {
				t.Fatal("a refused address must hold nothing and store nothing")
			}
		})
	}
}

func TestCreateRefusesSomeoneElsesOrAnUnreadyPhoto(t *testing.T) {
	r := newRig()
	r.photos.usable[photoB] = "someone-else"

	if _, err := r.service.Create(context.Background(), riderID, home(), photoB); !errors.Is(err, ErrPhotoNotUsable) {
		t.Fatalf("got %v", err)
	}

	if _, err := r.service.Create(context.Background(), riderID, home(), "not-a-uuid"); !errors.Is(err, ErrInvalidPhotoID) {
		t.Fatalf("got %v", err)
	}

	r.photos.down = true
	if _, err := r.service.Create(context.Background(), riderID, home(), photoA); !errors.Is(err, ErrPhotoUnavailable) {
		t.Fatalf("got %v", err)
	}

	if len(r.repo.byID) != 0 {
		t.Fatal("nothing may be saved when the photo is refused")
	}
}

func TestAFailedCreateLetsThePhotoGo(t *testing.T) {
	r := newRig()

	if _, err := r.service.Create(context.Background(), riderID, home(), ""); err != nil {
		t.Fatal(err)
	}

	// A second home is refused after the photo was held: it is let go again.
	if _, err := r.service.Create(context.Background(), riderID, home(), photoA); !errors.Is(err, ErrKindTaken) {
		t.Fatalf("got %v", err)
	}

	if r.photos.held[photoA] {
		t.Fatal("the photo is still held")
	}

	if len(r.photos.discarded) != 0 {
		t.Fatal("the rider's photo must not be deleted, only let go")
	}
}

func TestUpdateReplacesAndRemovesThePhoto(t *testing.T) {
	r := newRig()
	ctx := context.Background()

	created, _ := r.service.Create(ctx, riderID, home(), photoA)

	d := home()
	d.NoteForDriver = "Green gate"

	kept, err := r.service.Update(ctx, riderID, created.ID, d, "", false)
	if err != nil || kept.PhotoMediaID != photoA || kept.NoteForDriver != "Green gate" {
		t.Fatalf("keep: %+v %v", kept, err)
	}

	replaced, err := r.service.Update(ctx, riderID, created.ID, d, photoB, false)
	if err != nil || replaced.PhotoMediaID != photoB || !r.photos.held[photoB] {
		t.Fatalf("replace: %+v %v", replaced, err)
	}

	if len(r.photos.discarded) != 1 || r.photos.discarded[0] != photoA {
		t.Fatalf("the old photo must be deleted: %v", r.photos.discarded)
	}

	removed, err := r.service.Update(ctx, riderID, created.ID, d, "", true)
	if err != nil || removed.PhotoMediaID != "" || r.photos.discarded[1] != photoB {
		t.Fatalf("remove: %+v %v %v", removed, err, r.photos.discarded)
	}

	if _, err := r.service.Update(ctx, riderID, created.ID, d, photoA, true); !errors.Is(err, ErrPhotoConflict) {
		t.Fatalf("both: %v", err)
	}
}

func TestAFailedUpdateKeepsTheOldPhoto(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	created, _ := r.service.Create(ctx, riderID, home(), photoA)

	r.repo.failWrite = errors.New("db down")

	if _, err := r.service.Update(ctx, riderID, created.ID, home(), photoB, false); err == nil {
		t.Fatal("expected an error")
	}

	if r.photos.held[photoB] || !r.photos.held[photoA] || len(r.photos.discarded) != 0 {
		t.Fatalf("held %v discarded %v", r.photos.held, r.photos.discarded)
	}
}

func TestDeleteDeletesThePhoto(t *testing.T) {
	r := newRig()
	ctx := context.Background()
	created, _ := r.service.Create(ctx, riderID, home(), photoA)

	if err := r.service.Delete(ctx, otherRider, created.ID); !errors.Is(err, ErrAddressNotFound) {
		t.Fatalf("another rider: %v", err)
	}

	if err := r.service.Delete(ctx, riderID, created.ID); err != nil {
		t.Fatal(err)
	}

	if len(r.photos.discarded) != 1 || r.photos.discarded[0] != photoA {
		t.Fatalf("discarded %v", r.photos.discarded)
	}

	if _, err := r.service.Get(ctx, riderID, created.ID); !errors.Is(err, ErrAddressNotFound) {
		t.Fatalf("after delete: %v", err)
	}
}

func TestIDsAreChecked(t *testing.T) {
	r := newRig()

	if _, err := r.service.Get(context.Background(), riderID, "x"); !errors.Is(err, ErrAddressNotFound) {
		t.Fatalf("got %v", err)
	}

	if _, err := r.service.List(context.Background(), "x"); !errors.Is(err, ErrRiderNotFound) {
		t.Fatalf("got %v", err)
	}
}
