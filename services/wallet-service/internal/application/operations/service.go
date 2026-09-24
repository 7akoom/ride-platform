package operations

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const (
	maxKeyLength         = 120
	minReasonLength      = 3
	maxReasonLength      = 300
	maxDestinationLength = 120
	maxReferenceLength   = 120
	recentTransactions   = 20
	defaultPageSize      = 20
	maxPageSize          = 100
	moneyDecimalScale    = 3
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func isUUID(id string) bool { return uuidPattern.MatchString(id) }

// Service runs staff money operations and drivers' payouts.
type Service struct {
	store   Store
	wallets wallet.Service
	now     func() time.Time
}

func NewService(store Store, wallets wallet.Service) *Service {
	if store == nil {
		panic("operations store is required")
	}

	if wallets == nil {
		panic("wallet service is required")
	}

	return &Service{store: store, wallets: wallets, now: func() time.Time { return time.Now().UTC() }}
}

// Inspect is a wallet with its latest rows, and what the owner owes (a
// rider) or has asked to be paid (a driver).
func (s *Service) Inspect(ctx context.Context, ownerType wallet.OwnerType, ownerID string) (Inspection, error) {
	ownerID = strings.TrimSpace(ownerID)
	if !ownerType.Valid() || !isUUID(ownerID) {
		return Inspection{}, ErrOwnerRequired
	}

	found, err := s.wallets.GetWallet(ctx, ownerType, ownerID)
	if err != nil {
		return Inspection{}, err
	}

	recent, err := s.wallets.ListTransactions(ctx, ownerType, ownerID, recentTransactions)
	if err != nil {
		return Inspection{}, err
	}

	out := Inspection{Wallet: found, Recent: recent, OutstandingDues: wallet.Money{}}

	switch ownerType {
	case wallet.OwnerRider:
		dues, err := s.wallets.RiderDues(ctx, ownerID)
		if err != nil {
			return Inspection{}, err
		}

		out.OutstandingDues = dues.Outstanding

	case wallet.OwnerDriver:
		for _, status := range []PayoutStatus{PayoutPending, PayoutApproved} {
			open, err := s.store.ListPayouts(ctx, ownerID, status, 0, maxPageSize)
			if err != nil {
				return Inspection{}, fmt.Errorf("list open payouts: %w", err)
			}

			out.OpenPayouts = append(out.OpenPayouts, open...)
		}
	}

	return out, nil
}

// AdjustInput is a staff member correcting a balance.
type AdjustInput struct {
	StaffIdentityID string
	OwnerType       wallet.OwnerType
	OwnerID         string
	Amount          wallet.Money
	Reason          string
	IdempotencyKey  string
}

func (s *Service) Adjust(ctx context.Context, input AdjustInput) (Adjustment, wallet.Wallet, error) {
	staff := strings.TrimSpace(input.StaffIdentityID)
	owner := strings.TrimSpace(input.OwnerID)
	reason := strings.TrimSpace(input.Reason)
	key := strings.TrimSpace(input.IdempotencyKey)

	switch {
	case staff == "":
		return Adjustment{}, wallet.Wallet{}, ErrStaffRequired
	case !input.OwnerType.Valid() || !isUUID(owner):
		return Adjustment{}, wallet.Wallet{}, ErrOwnerRequired
	case input.Amount.IsZero() || !input.Amount.Equal(input.Amount.Round(moneyDecimalScale)):
		return Adjustment{}, wallet.Wallet{}, ErrInvalidAmount
	case !validReason(reason):
		return Adjustment{}, wallet.Wallet{}, ErrReasonRequired
	case key == "" || len(key) > maxKeyLength:
		return Adjustment{}, wallet.Wallet{}, ErrIdempotencyKey
	}

	same := func(a Adjustment) bool {
		return a.Kind == KindAdjustment && a.OwnerType == input.OwnerType && a.OwnerID == owner && a.Amount.Equal(input.Amount)
	}

	if done, found, err := s.replay(ctx, staff, key, same); err != nil || found {
		return done, wallet.Wallet{}, err
	}

	made, balance, err := s.store.Adjust(ctx, AdjustRecord{
		OwnerType: input.OwnerType, OwnerID: owner, Amount: input.Amount,
		Reason: reason, CreatedBy: staff, IdempotencyKey: key,
	})
	if errors.Is(err, wallet.ErrDuplicateRequest) {
		if done, found, findErr := s.replay(ctx, staff, key, same); findErr != nil || found {
			return done, wallet.Wallet{}, findErr
		}
	}

	return made, balance, err
}

// RefundInput is a staff member giving a rider money back for a trip.
type RefundInput struct {
	StaffIdentityID string
	TripID          string
	Amount          wallet.Money
	DriverAmount    wallet.Money
	Reason          string
	IdempotencyKey  string
}

func (s *Service) Refund(ctx context.Context, input RefundInput) (Adjustment, wallet.Wallet, error) {
	staff := strings.TrimSpace(input.StaffIdentityID)
	trip := strings.TrimSpace(input.TripID)
	reason := strings.TrimSpace(input.Reason)
	key := strings.TrimSpace(input.IdempotencyKey)

	switch {
	case staff == "":
		return Adjustment{}, wallet.Wallet{}, ErrStaffRequired
	case !isUUID(trip):
		return Adjustment{}, wallet.Wallet{}, ErrTripRequired
	case !input.Amount.IsPositive() || !input.Amount.Equal(input.Amount.Round(moneyDecimalScale)),
		input.DriverAmount.IsNegative() || input.DriverAmount.GreaterThan(input.Amount),
		!input.DriverAmount.Equal(input.DriverAmount.Round(moneyDecimalScale)):
		return Adjustment{}, wallet.Wallet{}, ErrInvalidRefund
	case !validReason(reason):
		return Adjustment{}, wallet.Wallet{}, ErrReasonRequired
	case key == "" || len(key) > maxKeyLength:
		return Adjustment{}, wallet.Wallet{}, ErrIdempotencyKey
	}

	same := func(a Adjustment) bool {
		return a.Kind == KindRefund && a.TripID == trip && a.Amount.Equal(input.Amount) && a.DriverAmount.Equal(input.DriverAmount)
	}

	if done, found, err := s.replay(ctx, staff, key, same); err != nil || found {
		return done, wallet.Wallet{}, err
	}

	made, balance, err := s.store.Refund(ctx, RefundRecord{
		TripID: trip, Amount: input.Amount, DriverAmount: input.DriverAmount,
		Reason: reason, CreatedBy: staff, IdempotencyKey: key,
	})
	if errors.Is(err, wallet.ErrDuplicateRequest) {
		if done, found, findErr := s.replay(ctx, staff, key, same); findErr != nil || found {
			return done, wallet.Wallet{}, findErr
		}
	}

	return made, balance, err
}

// replay returns the operation already made with the staff member's key: the
// same one again, or ErrKeyReused for another. The wallet is left empty (the
// caller reads it as it is now).
func (s *Service) replay(ctx context.Context, staff, key string, same func(Adjustment) bool) (Adjustment, bool, error) {
	done, found, err := s.store.FindAdjustmentByKey(ctx, staff, key)
	if err != nil {
		return Adjustment{}, false, fmt.Errorf("look up the idempotency key: %w", err)
	}

	if !found {
		return Adjustment{}, false, nil
	}

	if !same(done) {
		return Adjustment{}, true, ErrKeyReused
	}

	return done, true, nil
}

func (s *Service) TripRefunds(ctx context.Context, tripID string) (TripRefunds, error) {
	tripID = strings.TrimSpace(tripID)
	if !isUUID(tripID) {
		return TripRefunds{}, ErrTripRequired
	}

	return s.store.TripRefunds(ctx, tripID)
}

// PayoutInput is a driver asking for their money.
type PayoutInput struct {
	DriverID       string
	Amount         wallet.Money
	Destination    string
	IdempotencyKey string
}

// RequestPayout holds the amount and opens a request. A retry with the same
// key returns the request made the first time (the wallet empty: the caller
// reads it as it is now).
func (s *Service) RequestPayout(ctx context.Context, input PayoutInput) (Payout, wallet.Wallet, wallet.Transaction, error) {
	driver := strings.TrimSpace(input.DriverID)
	destination := strings.TrimSpace(input.Destination)
	key := strings.TrimSpace(input.IdempotencyKey)

	switch {
	case !isUUID(driver):
		return Payout{}, wallet.Wallet{}, wallet.Transaction{}, ErrDriverRequired
	case !input.Amount.IsPositive() || !input.Amount.Equal(input.Amount.Round(moneyDecimalScale)):
		return Payout{}, wallet.Wallet{}, wallet.Transaction{}, wallet.ErrInvalidAmount
	case utf8.RuneCountInString(destination) > maxDestinationLength:
		return Payout{}, wallet.Wallet{}, wallet.Transaction{}, ErrDestinationTooLong
	case key == "" || len(key) > maxKeyLength:
		return Payout{}, wallet.Wallet{}, wallet.Transaction{}, ErrIdempotencyKey
	}

	replay := func() (Payout, bool, error) {
		done, found, err := s.store.FindPayoutByKey(ctx, driver, key)
		if err != nil || !found {
			return Payout{}, found, err
		}

		if !done.Amount.Equal(input.Amount) {
			return Payout{}, true, ErrKeyReused
		}

		return done, true, nil
	}

	if done, found, err := replay(); err != nil || found {
		return done, wallet.Wallet{}, wallet.Transaction{}, err
	}

	config, err := s.store.Config(ctx)
	if err != nil {
		return Payout{}, wallet.Wallet{}, wallet.Transaction{}, fmt.Errorf("read the wallet config: %w", err)
	}

	if input.Amount.LessThan(config.MinimumPayoutAmount) {
		return Payout{}, wallet.Wallet{}, wallet.Transaction{}, wallet.ErrBelowMinimumPayout
	}

	made, balance, hold, err := s.store.RequestPayout(ctx, PayoutRecord{
		DriverID: driver, Amount: input.Amount, Destination: destination, IdempotencyKey: key,
	})
	if errors.Is(err, wallet.ErrDuplicateRequest) {
		if done, found, findErr := replay(); findErr != nil || found {
			return done, wallet.Wallet{}, wallet.Transaction{}, findErr
		}
	}

	return made, balance, hold, err
}

// PayoutPage is one page of payout requests; NextOffset 0 means no next one.
type PayoutPage struct {
	Payouts    []Payout
	NextOffset int
}

// ListPayouts is a driver's requests (driverID set) or the staff's queue.
func (s *Service) ListPayouts(ctx context.Context, driverID, status string, pageSize int, pageToken string) (PayoutPage, error) {
	filter := PayoutStatus(strings.TrimSpace(status))

	switch filter {
	case "", PayoutPending, PayoutApproved, PayoutPaid, PayoutRejected:
	default:
		return PayoutPage{}, ErrInvalidStatus
	}

	offset, limit, err := paging(pageSize, pageToken)
	if err != nil {
		return PayoutPage{}, err
	}

	payouts, err := s.store.ListPayouts(ctx, strings.TrimSpace(driverID), filter, offset, limit+1)
	if err != nil {
		return PayoutPage{}, fmt.Errorf("list payouts: %w", err)
	}

	if len(payouts) > limit {
		return PayoutPage{Payouts: payouts[:limit], NextOffset: offset + limit}, nil
	}

	return PayoutPage{Payouts: payouts}, nil
}

func (s *Service) ApprovePayout(ctx context.Context, staffIdentityID, id string) (Payout, error) {
	staff, id := strings.TrimSpace(staffIdentityID), strings.TrimSpace(id)

	switch {
	case staff == "":
		return Payout{}, ErrStaffRequired
	case !isUUID(id):
		return Payout{}, ErrPayoutNotFound
	}

	return s.store.SetPayoutStatus(ctx, id, PayoutApproved, staff, "", s.now())
}

func (s *Service) MarkPayoutPaid(ctx context.Context, staffIdentityID, id, reference string) (Payout, error) {
	staff, id, reference := strings.TrimSpace(staffIdentityID), strings.TrimSpace(id), strings.TrimSpace(reference)

	switch {
	case staff == "":
		return Payout{}, ErrStaffRequired
	case !isUUID(id):
		return Payout{}, ErrPayoutNotFound
	case reference == "" || utf8.RuneCountInString(reference) > maxReferenceLength:
		return Payout{}, ErrReferenceRequired
	}

	return s.store.SetPayoutStatus(ctx, id, PayoutPaid, staff, reference, s.now())
}

func (s *Service) RejectPayout(ctx context.Context, staffIdentityID, id, reason string) (Payout, error) {
	staff, id, reason := strings.TrimSpace(staffIdentityID), strings.TrimSpace(id), strings.TrimSpace(reason)

	switch {
	case staff == "":
		return Payout{}, ErrStaffRequired
	case !isUUID(id):
		return Payout{}, ErrPayoutNotFound
	case !validReason(reason):
		return Payout{}, ErrReasonRequired
	}

	return s.store.RejectPayout(ctx, id, staff, reason, s.now())
}

func validReason(reason string) bool {
	n := utf8.RuneCountInString(reason)

	return n >= minReasonLength && n <= maxReasonLength
}

func paging(pageSize int, pageToken string) (int, int, error) {
	limit := pageSize

	switch {
	case limit <= 0:
		limit = defaultPageSize
	case limit > maxPageSize:
		limit = maxPageSize
	}

	offset := 0

	if pageToken != "" {
		n, err := strconv.Atoi(pageToken)
		if err != nil || n < 0 {
			return 0, 0, ErrInvalidPageToken
		}

		offset = n
	}

	return offset, limit, nil
}
