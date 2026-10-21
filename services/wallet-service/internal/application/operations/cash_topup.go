package operations

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const (
	maxReceiptLength    = 60
	maxNoteLength       = 300
	maxCashTopUpRange   = 31 * 24 * time.Hour
	maxCashTopUpsListed = 500
)

// CashTopUpInput is cash paid at the office, credited by a staff member.
type CashTopUpInput struct {
	StaffIdentityID string
	OwnerType       wallet.OwnerType
	OwnerID         string
	Amount          wallet.Money
	ReceiptNumber   string
	Note            string
	IdempotencyKey  string
}

// CashTopUpRecord is what the store writes.
type CashTopUpRecord struct {
	OwnerType      wallet.OwnerType
	OwnerID        string
	Amount         wallet.Money
	ReceiptNumber  string
	Note           string
	CreatedBy      string
	IdempotencyKey string
}

// CashTopUpQuery is a time range of the office's cash, and optionally one staff
// member's.
type CashTopUpQuery struct {
	From, To  time.Time
	CreatedBy string
	Limit     int
}

// CashTopUps is the cash in a range, newest first (at most the limit), and the
// count and total of all of it.
type CashTopUps struct {
	TopUps       []Adjustment
	Count        int
	Total        wallet.Money
	CurrencyCode string
}

// CashTopUpStore is the store of cash top-ups.
type CashTopUpStore interface {
	// CashTopUp credits the wallet with a cash_top_up row and records the
	// receipt; ErrReceiptUsed when the receipt was credited before,
	// wallet.ErrDuplicateRequest when the key was taken meanwhile.
	CashTopUp(ctx context.Context, record CashTopUpRecord) (Adjustment, wallet.Wallet, error)
	ListCashTopUps(ctx context.Context, query CashTopUpQuery) (CashTopUps, error)
}

// CashTopUp credits the owner's wallet with cash paid at the office.
func (s *Service) CashTopUp(ctx context.Context, input CashTopUpInput) (Adjustment, wallet.Wallet, error) {
	store, ok := s.store.(CashTopUpStore)
	if !ok {
		return Adjustment{}, wallet.Wallet{}, errors.New("cash top-ups are not configured")
	}

	staff := strings.TrimSpace(input.StaffIdentityID)
	owner := strings.TrimSpace(input.OwnerID)
	receipt := strings.TrimSpace(input.ReceiptNumber)
	note := strings.TrimSpace(input.Note)
	key := strings.TrimSpace(input.IdempotencyKey)

	switch {
	case staff == "":
		return Adjustment{}, wallet.Wallet{}, ErrStaffRequired
	case !input.OwnerType.Valid() || !isUUID(owner):
		return Adjustment{}, wallet.Wallet{}, ErrOwnerRequired
	case !input.Amount.IsPositive() || !input.Amount.Equal(input.Amount.Round(moneyDecimalScale)):
		return Adjustment{}, wallet.Wallet{}, ErrInvalidCashAmount
	case receipt == "" || utf8.RuneCountInString(receipt) > maxReceiptLength:
		return Adjustment{}, wallet.Wallet{}, ErrReceiptRequired
	case utf8.RuneCountInString(note) > maxNoteLength:
		return Adjustment{}, wallet.Wallet{}, ErrNoteTooLong
	case key == "" || len(key) > maxKeyLength:
		return Adjustment{}, wallet.Wallet{}, ErrIdempotencyKey
	}

	same := func(a Adjustment) bool {
		return a.Kind == KindCashTopUp && a.OwnerType == input.OwnerType && a.OwnerID == owner &&
			a.Amount.Equal(input.Amount) && a.ReceiptNumber == receipt
	}

	if done, found, err := s.replay(ctx, staff, key, same); err != nil || found {
		return done, wallet.Wallet{}, err
	}

	made, balance, err := store.CashTopUp(ctx, CashTopUpRecord{
		OwnerType: input.OwnerType, OwnerID: owner, Amount: input.Amount,
		ReceiptNumber: receipt, Note: note, CreatedBy: staff, IdempotencyKey: key,
	})
	if errors.Is(err, wallet.ErrDuplicateRequest) {
		if done, found, findErr := s.replay(ctx, staff, key, same); findErr != nil || found {
			return done, wallet.Wallet{}, findErr
		}
	}

	return made, balance, err
}

// ListCashTopUps is the office's cash in [from, to): the last 24 hours when
// both are zero, at most 31 days.
func (s *Service) ListCashTopUps(ctx context.Context, from, to time.Time, createdBy string) (CashTopUps, error) {
	store, ok := s.store.(CashTopUpStore)
	if !ok {
		return CashTopUps{}, errors.New("cash top-ups are not configured")
	}

	if to.IsZero() {
		to = s.now()
	}

	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}

	if !to.After(from) || to.Sub(from) > maxCashTopUpRange {
		return CashTopUps{}, ErrInvalidRange
	}

	createdBy = strings.TrimSpace(createdBy)
	if createdBy != "" && !isUUID(createdBy) {
		return CashTopUps{}, ErrStaffRequired
	}

	return store.ListCashTopUps(ctx, CashTopUpQuery{From: from, To: to, CreatedBy: createdBy, Limit: maxCashTopUpsListed})
}
