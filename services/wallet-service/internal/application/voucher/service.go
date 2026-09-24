package voucher

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const (
	maxLabelLength    = 120
	maxSellerLength   = 60
	maxKeyLength      = 120
	minReasonLength   = 3
	maxReasonLength   = 300
	maxQuantity       = 10000
	defaultValidity   = 365 * 24 * time.Hour
	minValidity       = time.Hour
	maxValidity       = 3 * 365 * 24 * time.Hour
	defaultPageSize   = 20
	maxPageSize       = 100
	moneyDecimalScale = 3
)

// Limits is how many wrong codes a rider may type in a window before they
// wait for the oldest of them to leave it.
type Limits struct {
	MaxFailures int
	Window      time.Duration
}

// Service issues, exports and redeems vouchers.
type Service struct {
	store  Store
	codec  *Codec
	limits Limits
	now    func() time.Time
}

func NewService(store Store, codec *Codec, limits Limits) *Service {
	if store == nil {
		panic("voucher store is required")
	}

	if codec == nil {
		panic("voucher codec is required")
	}

	if limits.MaxFailures < 1 || limits.Window <= 0 {
		panic("voucher redeem limits must be positive")
	}

	return &Service{store: store, codec: codec, limits: limits, now: func() time.Time { return time.Now().UTC() }}
}

// CreateInput is a staff member issuing a batch.
type CreateInput struct {
	StaffIdentityID string
	Label           string
	Seller          string
	Amount          wallet.Money
	Quantity        int
	// Zero means a year from now.
	ExpiresAt      time.Time
	IdempotencyKey string
}

// CreateBatch generates the batch's codes and stores them sealed. A retry
// with the same key returns the batch made the first time.
func (s *Service) CreateBatch(ctx context.Context, input CreateInput) (Batch, error) {
	staff := strings.TrimSpace(input.StaffIdentityID)
	label := strings.TrimSpace(input.Label)
	seller := strings.TrimSpace(input.Seller)
	key := strings.TrimSpace(input.IdempotencyKey)
	now := s.now()

	expires := input.ExpiresAt
	if expires.IsZero() {
		expires = now.Add(defaultValidity)
	}

	switch {
	case staff == "":
		return Batch{}, ErrStaffRequired
	case label == "" || utf8.RuneCountInString(label) > maxLabelLength:
		return Batch{}, ErrLabelRequired
	case utf8.RuneCountInString(seller) > maxSellerLength:
		return Batch{}, ErrSellerTooLong
	case !input.Amount.IsPositive() || !input.Amount.Equal(input.Amount.Round(moneyDecimalScale)):
		return Batch{}, ErrInvalidAmount
	case input.Quantity < 1 || input.Quantity > maxQuantity:
		return Batch{}, ErrInvalidQuantity
	case expires.Before(now.Add(minValidity)) || expires.After(now.Add(maxValidity)):
		return Batch{}, ErrInvalidExpiry
	case key == "" || len(key) > maxKeyLength:
		return Batch{}, ErrIdempotencyKey
	}

	replay := func() (Batch, bool, error) {
		done, found, err := s.store.FindBatchByKey(ctx, staff, key)
		if err != nil || !found {
			return Batch{}, found, err
		}

		if done.Label != label || !done.Amount.Equal(input.Amount) || done.Quantity != input.Quantity {
			return Batch{}, true, ErrKeyReused
		}

		return done, true, nil
	}

	if done, found, err := replay(); err != nil || found {
		return done, err
	}

	config, err := s.store.Config(ctx)
	if err != nil {
		return Batch{}, fmt.Errorf("read the wallet config: %w", err)
	}

	batch := Batch{
		Label:          label,
		Seller:         seller,
		Amount:         input.Amount,
		CurrencyCode:   config.CurrencyCode,
		Quantity:       input.Quantity,
		Status:         BatchCreated,
		ExpiresAt:      expires.UTC(),
		CreatedBy:      staff,
		IdempotencyKey: key,
	}

	// A code colliding with one already issued is next to impossible (79
	// bits each); one more try with fresh codes covers it.
	for attempt := 0; ; attempt++ {
		codes, err := s.sealedCodes(input.Quantity)
		if err != nil {
			return Batch{}, err
		}

		created, err := s.store.CreateBatch(ctx, batch, codes)

		switch {
		case err == nil:
			return created, nil
		case errors.Is(err, ErrCodeTaken) && attempt == 0:
			continue
		case errors.Is(err, wallet.ErrDuplicateRequest):
			if done, found, findErr := replay(); findErr != nil || found {
				return done, findErr
			}
		}

		return Batch{}, err
	}
}

func (s *Service) sealedCodes(quantity int) ([]Sealed, error) {
	seen := make(map[string]struct{}, quantity)
	out := make([]Sealed, 0, quantity)

	for len(out) < quantity {
		code, err := s.codec.NewCode()
		if err != nil {
			return nil, err
		}

		if _, dup := seen[code]; dup {
			continue
		}

		seen[code] = struct{}{}
		hash := s.codec.Hash(code)

		sealed, err := s.codec.Seal(code, hash)
		if err != nil {
			return nil, err
		}

		out = append(out, Sealed{Hash: hash, Sealed: sealed})
	}

	return out, nil
}

// BatchPage is one page of batches; NextOffset 0 means there is no next one.
type BatchPage struct {
	Batches    []Batch
	NextOffset int
}

func (s *Service) ListBatches(ctx context.Context, status string, pageSize int, pageToken string) (BatchPage, error) {
	filter := BatchStatus(strings.TrimSpace(status))

	switch filter {
	case "", BatchCreated, BatchExported, BatchCancelled:
	default:
		return BatchPage{}, ErrInvalidStatusFilter
	}

	offset, limit, err := paging(pageSize, pageToken)
	if err != nil {
		return BatchPage{}, err
	}

	batches, err := s.store.ListBatches(ctx, filter, offset, limit+1)
	if err != nil {
		return BatchPage{}, fmt.Errorf("list voucher batches: %w", err)
	}

	if len(batches) > limit {
		return BatchPage{Batches: batches[:limit], NextOffset: offset + limit}, nil
	}

	return BatchPage{Batches: batches}, nil
}

func (s *Service) GetBatch(ctx context.Context, id string) (Batch, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Batch{}, ErrBatchNotFound
	}

	return s.store.GetBatch(ctx, id)
}

// ExportBatch opens a created batch's codes, once: they are wiped as they are
// handed over, and the batch becomes redeemable.
func (s *Service) ExportBatch(ctx context.Context, staffIdentityID, id string) (Batch, []Exported, error) {
	staff := strings.TrimSpace(staffIdentityID)
	id = strings.TrimSpace(id)

	switch {
	case staff == "":
		return Batch{}, nil, ErrStaffRequired
	case id == "":
		return Batch{}, nil, ErrBatchNotFound
	}

	batch, exported, err := s.store.ExportBatch(ctx, id, staff, s.now(), s.codec.Open)
	if err != nil {
		return Batch{}, nil, err
	}

	for i := range exported {
		exported[i].Code = Format(exported[i].Code)
	}

	return batch, exported, nil
}

// CSV is an export for the seller: serial,code,amount,currency,expires_at.
func CSV(batch Batch, exported []Exported) (string, error) {
	var buf bytes.Buffer

	w := csv.NewWriter(&buf)
	if err := w.Write([]string{"serial", "code", "amount", "currency", "expires_at"}); err != nil {
		return "", err
	}

	expires := batch.ExpiresAt.UTC().Format(time.RFC3339)

	for _, e := range exported {
		if err := w.Write([]string{e.Serial, e.Code, batch.Amount.String(), batch.CurrencyCode, expires}); err != nil {
			return "", err
		}
	}

	w.Flush()

	return buf.String(), w.Error()
}

func (s *Service) CancelBatch(ctx context.Context, staffIdentityID, id, reason string) (Batch, error) {
	staff := strings.TrimSpace(staffIdentityID)
	id = strings.TrimSpace(id)
	reason = strings.TrimSpace(reason)

	switch {
	case staff == "":
		return Batch{}, ErrStaffRequired
	case id == "":
		return Batch{}, ErrBatchNotFound
	case !validReason(reason):
		return Batch{}, ErrReasonRequired
	}

	return s.store.CancelBatch(ctx, id, staff, reason, s.now())
}

func (s *Service) GetVoucher(ctx context.Context, serial string) (Voucher, error) {
	serial = strings.ToUpper(strings.TrimSpace(serial))
	if serial == "" {
		return Voucher{}, ErrVoucherNotFound
	}

	return s.store.GetVoucher(ctx, serial)
}

func (s *Service) VoidVoucher(ctx context.Context, staffIdentityID, serial, reason string) (Voucher, error) {
	staff := strings.TrimSpace(staffIdentityID)
	serial = strings.ToUpper(strings.TrimSpace(serial))
	reason = strings.TrimSpace(reason)

	switch {
	case staff == "":
		return Voucher{}, ErrStaffRequired
	case serial == "":
		return Voucher{}, ErrVoucherNotFound
	case !validReason(reason):
		return Voucher{}, ErrReasonRequired
	}

	return s.store.VoidVoucher(ctx, serial, staff, reason, s.now())
}

// Now is the service's clock (a voucher's redeemable flag is read against it).
func (s *Service) Now() time.Time {
	return s.now()
}

// Redeem credits the rider's wallet with the voucher. A code that is not in
// the form of one is refused without counting; every other failed code
// counts toward the rider's limit, checked before the code is looked at.
func (s *Service) Redeem(ctx context.Context, riderID, typed string) (Redemption, error) {
	riderID = strings.TrimSpace(riderID)
	if riderID == "" {
		return Redemption{}, ErrRiderRequired
	}

	code, ok := Normalize(typed)
	if !ok {
		return Redemption{}, ErrInvalidCode
	}

	now := s.now()
	since := now.Add(-s.limits.Window)

	failures, oldest, err := s.store.Failures(ctx, riderID, since)
	if err != nil {
		return Redemption{}, fmt.Errorf("count wrong voucher codes: %w", err)
	}

	if failures >= s.limits.MaxFailures {
		return Redemption{}, &TooManyAttemptsError{Until: oldest.Add(s.limits.Window)}
	}

	redeemed, err := s.store.Redeem(ctx, riderID, s.codec.Hash(code), now)
	if err == nil {
		return redeemed, nil
	}

	if errors.Is(err, ErrCodeNotValid) || errors.Is(err, ErrCodeUsed) ||
		errors.Is(err, ErrCodeCancelled) || errors.Is(err, ErrCodeExpired) {
		if recordErr := s.store.RecordFailure(ctx, riderID, now, since); recordErr != nil {
			return Redemption{}, fmt.Errorf("record a wrong voucher code: %w", recordErr)
		}
	}

	return Redemption{}, err
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
