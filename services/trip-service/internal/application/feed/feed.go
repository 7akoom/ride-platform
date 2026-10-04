// Package feed is the activity page: a person's trips and the wallet
// movements that are not part of a trip (top-ups, transfers, refunds,
// vouchers, payouts...) in one list, newest first, paged with an opaque
// token. It reads both sources at the moment of the request, so it is always
// in step with them.
package feed

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/trip-service/internal/application/trip"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 50
)

var (
	// ErrInvalidQuery: exactly one of the rider and the driver is needed.
	ErrInvalidQuery = errors.New("exactly one of rider_id and driver_id is required")
	// ErrInvalidPageToken: the token is not one this service gave out.
	ErrInvalidPageToken = errors.New("invalid page token")
	// ErrWalletUnavailable: wallet-service did not answer.
	ErrWalletUnavailable = errors.New("the wallet could not be read, try again")
)

// Owner is whose activity: rider or driver.
type Owner string

const (
	OwnerRider  Owner = "rider"
	OwnerDriver Owner = "driver"
)

// Cursor is a position in the list: (time, id), newest first.
type Cursor struct {
	At time.Time
	ID string
}

func (c Cursor) after(at time.Time, id string) bool {
	return at.Before(c.At) || (at.Equal(c.At) && id < c.ID)
}

// Movement is a wallet movement not tied to a trip.
type Movement struct {
	ID           string
	Type         string
	Amount       string
	BalanceAfter string
	Currency     string
	Description  string
	TransferID   string
	At           time.Time
}

// Kind of an item.
type Kind string

const (
	KindTrip   Kind = "trip"
	KindWallet Kind = "wallet"
)

// Item is one line of the page.
type Item struct {
	Kind     Kind
	ID       string
	At       time.Time
	Trip     *trip.Trip
	Movement *Movement
}

// Page is one page; NextPageToken is empty on the last.
type Page struct {
	Items         []Item
	NextPageToken string
}

// Trips lists a profile's trips older than before (nil: the newest), by
// (requested_at, id), newest first.
type Trips interface {
	ListTripsBefore(ctx context.Context, owner Owner, ownerID string, before *Cursor, limit int) ([]trip.Trip, error)
}

// Wallet lists a profile's wallet movements that are not tied to a trip, the
// same way. It fails (wrapping ErrWalletUnavailable) rather than return less.
type Wallet interface {
	MovementsBefore(ctx context.Context, owner Owner, ownerID string, before *Cursor, limit int) ([]Movement, error)
}

type Service struct {
	trips  Trips
	wallet Wallet
}

func NewService(trips Trips, wallet Wallet) *Service {
	if trips == nil || wallet == nil {
		panic("feed dependencies are required")
	}

	return &Service{trips: trips, wallet: wallet}
}

// List returns one page of the profile's activity.
func (s *Service) List(ctx context.Context, riderID, driverID string, pageSize int, pageToken string) (Page, error) {
	riderID, driverID = strings.TrimSpace(riderID), strings.TrimSpace(driverID)

	var (
		owner   Owner
		ownerID string
	)

	switch {
	case riderID != "" && driverID == "":
		owner, ownerID = OwnerRider, riderID
	case driverID != "" && riderID == "":
		owner, ownerID = OwnerDriver, driverID
	default:
		return Page{}, ErrInvalidQuery
	}

	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}

	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}

	before, err := decodeToken(pageToken)
	if err != nil {
		return Page{}, err
	}

	// Each source gives one more than the page, so the merge knows whether
	// anything follows.
	trips, err := s.trips.ListTripsBefore(ctx, owner, ownerID, before, pageSize+1)
	if err != nil {
		return Page{}, fmt.Errorf("list trips: %w", err)
	}

	movements, err := s.wallet.MovementsBefore(ctx, owner, ownerID, before, pageSize+1)
	if err != nil {
		return Page{}, err
	}

	items := make([]Item, 0, len(trips)+len(movements))

	for i := range trips {
		t := trips[i]
		items = append(items, Item{Kind: KindTrip, ID: t.ID, At: t.RequestedAt, Trip: &t})
	}

	for i := range movements {
		m := movements[i]
		items = append(items, Item{Kind: KindWallet, ID: m.ID, At: m.At, Movement: &m})
	}

	// Newest first; ties by id, the same order both sources page in.
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].At.Equal(items[j].At) {
			return items[i].At.After(items[j].At)
		}

		return items[i].ID > items[j].ID
	})

	if before != nil {
		kept := items[:0]

		for _, it := range items {
			if before.after(it.At, it.ID) {
				kept = append(kept, it)
			}
		}

		items = kept
	}

	page := Page{Items: items}

	if len(items) > pageSize {
		page.Items = items[:pageSize]
		last := page.Items[pageSize-1]
		page.NextPageToken = encodeToken(Cursor{At: last.At, ID: last.ID})
	}

	return page, nil
}

func encodeToken(c Cursor) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(c.At.UnixNano(), 10) + ":" + c.ID))
}

func decodeToken(token string) (*Cursor, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, nil
	}

	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrInvalidPageToken
	}

	nanos, id, found := strings.Cut(string(raw), ":")
	if !found || id == "" || len(id) > 64 {
		return nil, ErrInvalidPageToken
	}

	value, err := strconv.ParseInt(nanos, 10, 64)
	if err != nil || value <= 0 {
		return nil, ErrInvalidPageToken
	}

	return &Cursor{At: time.Unix(0, value).UTC(), ID: id}, nil
}
