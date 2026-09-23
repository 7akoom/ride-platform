package trip_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

const (
	homeID  = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	workID  = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	photoID = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

type fakeAddressBook struct {
	addresses map[string]trip.SavedAddress
	err       error
	asked     []string
}

func (b *fakeAddressBook) SavedAddress(_ context.Context, riderID, addressID string) (trip.SavedAddress, error) {
	b.asked = append(b.asked, riderID+"/"+addressID)

	if b.err != nil {
		return trip.SavedAddress{}, b.err
	}

	found, ok := b.addresses[addressID]
	if !ok {
		return trip.SavedAddress{}, trip.ErrSavedAddressNotFound
	}

	return found, nil
}

func savedRig() (trip.Service, *fakeRepository, *fakeAddressBook) {
	repo := &fakeRepository{findActiveByRiderIDErr: trip.ErrTripNotFound}
	book := &fakeAddressBook{addresses: map[string]trip.SavedAddress{
		homeID: {
			Coordinates: trip.Coordinates{Latitude: 36.1, Longitude: 44.1},
			Address:     "Home, Gulan Street", Details: "Floor 2", Note: "Blue gate", PhotoMediaID: photoID,
		},
		workID: {Coordinates: trip.Coordinates{Latitude: 36.3, Longitude: 44.3}, Address: "Work, 100m Street", Note: "ignored"},
	}}

	return trip.WithSavedAddresses(newService(repo), book), repo, book
}

func TestATripFromASavedAddressCopiesItsPointAndWhatHelpsTheCaptain(t *testing.T) {
	svc, repo, book := savedRig()

	input := validRequestInput()
	input.PickupSavedAddressID = homeID
	input.DropoffSavedAddressID = workID
	input.PickupAddress = "typed by the app, replaced"

	if _, err := svc.RequestTrip(context.Background(), input); err != nil {
		t.Fatal(err)
	}

	got := repo.createCalls[0]

	if got.Pickup != (trip.Coordinates{Latitude: 36.1, Longitude: 44.1}) || got.Dropoff != (trip.Coordinates{Latitude: 36.3, Longitude: 44.3}) {
		t.Fatalf("points: %+v %+v", got.Pickup, got.Dropoff)
	}

	if got.PickupAddress != "Home, Gulan Street" || got.DropoffAddress != "Work, 100m Street" ||
		got.PickupDetails != "Floor 2" || got.PickupNote != "Blue gate" || got.PickupPhotoMediaID != photoID {
		t.Fatalf("texts: %+v", got)
	}

	if len(book.asked) != 2 || book.asked[0] != "rider-1/"+homeID {
		t.Fatalf("asked %v", book.asked)
	}
}

func TestOnlyASavedPickupBringsANoteOrAPhoto(t *testing.T) {
	svc, repo, _ := savedRig()

	input := validRequestInput()
	input.PickupAddress = "  Family Mall  "
	input.PickupNote = "smuggled in"
	input.PickupPhotoMediaID = photoID

	if _, err := svc.RequestTrip(context.Background(), input); err != nil {
		t.Fatal(err)
	}

	got := repo.createCalls[0]
	if got.PickupAddress != "Family Mall" || got.PickupNote != "" || got.PickupPhotoMediaID != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestSavedAddressRefusals(t *testing.T) {
	cases := map[string]struct {
		id   string
		err  error
		want error
	}{
		"someone else's":     {workID + "0", nil, trip.ErrSavedAddressNotFound},
		"malformed":          {"home", nil, trip.ErrSavedAddressNotFound},
		"unknown":            {"dddddddd-dddd-4ddd-8ddd-dddddddddddd", nil, trip.ErrSavedAddressNotFound},
		"rider-service down": {homeID, trip.ErrUpstreamUnavailable, trip.ErrUpstreamUnavailable},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			svc, repo, book := savedRig()
			book.err = tc.err

			input := validRequestInput()
			input.PickupSavedAddressID = tc.id

			if _, err := svc.RequestTrip(context.Background(), input); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}

			if len(repo.createCalls) != 0 {
				t.Fatal("no trip may be created")
			}
		})
	}
}

func TestWithoutTheDecoratorSavedAddressesAreRefused(t *testing.T) {
	repo := &fakeRepository{findActiveByRiderIDErr: trip.ErrTripNotFound}
	input := validRequestInput()
	input.DropoffSavedAddressID = workID

	if _, err := newService(repo).RequestTrip(context.Background(), input); !errors.Is(err, trip.ErrSavedAddressesUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func TestTooLongAnAddressIsRefused(t *testing.T) {
	repo := &fakeRepository{findActiveByRiderIDErr: trip.ErrTripNotFound}
	input := validRequestInput()
	input.DropoffAddress = strings.Repeat("ش", 301)

	if _, err := newService(repo).RequestTrip(context.Background(), input); !errors.Is(err, trip.ErrAddressTooLong) {
		t.Fatalf("got %v", err)
	}
}

// --- pickup photo ------------------------------------------------------------

type photoBase struct {
	trip.Service

	trip trip.Trip
}

func (b *photoBase) GetTrip(context.Context, string) (trip.Trip, error) { return b.trip, nil }

type fakeLinks struct {
	asked []string
	err   error
}

func (l *fakeLinks) DownloadURL(_ context.Context, mediaID string) (string, time.Time, error) {
	l.asked = append(l.asked, mediaID)

	return "https://store.test/" + mediaID, time.Unix(1, 0), l.err
}

type photoGetter interface {
	PickupPhoto(ctx context.Context, tripID string) (string, time.Time, error)
}

func TestPickupPhotoOnlyWhileTheTripIsUnderway(t *testing.T) {
	for status, want := range map[trip.Status]error{
		trip.StatusRequested:  trip.ErrPickupPhotoNotAvailableNow,
		trip.StatusAccepted:   nil,
		trip.StatusInProgress: nil,
		trip.StatusCompleted:  trip.ErrPickupPhotoNotAvailableNow,
		trip.StatusCancelled:  trip.ErrPickupPhotoNotAvailableNow,
	} {
		links := &fakeLinks{}
		svc := trip.WithPickupPhotos(&photoBase{trip: trip.Trip{ID: "t", Status: status, PickupPhotoMediaID: photoID}}, links)

		getter, ok := trip.As[photoGetter](svc)
		if !ok {
			t.Fatal("no PickupPhoto")
		}

		url, _, err := getter.PickupPhoto(context.Background(), "t")
		if !errors.Is(err, want) {
			t.Fatalf("%s: got %v, want %v", status, err, want)
		}

		if want == nil && url != "https://store.test/"+photoID {
			t.Fatalf("%s: url %q", status, url)
		}

		if want != nil && len(links.asked) != 0 {
			t.Fatalf("%s: media-service asked anyway", status)
		}
	}
}

func TestNoPhotoNoLink(t *testing.T) {
	svc := trip.WithPickupPhotos(&photoBase{trip: trip.Trip{Status: trip.StatusAccepted}}, &fakeLinks{})
	getter, _ := trip.As[photoGetter](svc)

	if _, _, err := getter.PickupPhoto(context.Background(), "t"); !errors.Is(err, trip.ErrNoPickupPhoto) {
		t.Fatalf("got %v", err)
	}
}

// --- recent destinations -------------------------------------------------------

type fakeDestinations struct {
	limit int
}

func (f *fakeDestinations) RecentDestinations(_ context.Context, _ string, limit int) ([]trip.Destination, error) {
	f.limit = limit

	return []trip.Destination{{Address: "Family Mall"}}, nil
}

type destinationLister interface {
	RecentDestinations(ctx context.Context, riderID string, limit int) ([]trip.Destination, error)
}

func TestRecentDestinationLimits(t *testing.T) {
	store := &fakeDestinations{}
	lister, ok := trip.As[destinationLister](trip.WithRecentDestinations(newService(&fakeRepository{}), store))
	if !ok {
		t.Fatal("no RecentDestinations")
	}

	for asked, want := range map[int]int{0: trip.DefaultRecentDestinations, 3: 3, 50: trip.MaxRecentDestinations} {
		if _, err := lister.RecentDestinations(context.Background(), "rider-1", asked); err != nil || store.limit != want {
			t.Fatalf("asked %d: limit %d, %v", asked, store.limit, err)
		}
	}

	if _, err := lister.RecentDestinations(context.Background(), "rider-1", -1); !errors.Is(err, trip.ErrInvalidLimit) {
		t.Fatalf("negative: %v", err)
	}

	if _, err := lister.RecentDestinations(context.Background(), " ", 1); !errors.Is(err, trip.ErrRiderIDRequired) {
		t.Fatalf("no rider: %v", err)
	}
}
