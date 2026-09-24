// Package voucher is prepaid wallet codes: the platform issues them in
// batches, a seller (ZainCash, shops) sells them, and a rider redeems one into
// their wallet. The codes are generated here, kept sealed until the batch is
// exported (once), and after that only their HMAC remains.
package voucher

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

var (
	ErrStaffRequired       = errors.New("vouchers are managed by a staff member")
	ErrRiderRequired       = errors.New("the rider is required")
	ErrLabelRequired       = errors.New("label is required (at most 120 characters)")
	ErrSellerTooLong       = errors.New("seller must be at most 60 characters")
	ErrInvalidAmount       = errors.New("amount must be a positive decimal with at most 3 decimal places")
	ErrInvalidQuantity     = errors.New("quantity must be between 1 and 10000")
	ErrInvalidExpiry       = errors.New("expires_at must be at least an hour and at most three years ahead")
	ErrIdempotencyKey      = errors.New("idempotency_key is required (1-120 characters)")
	ErrKeyReused           = errors.New("this idempotency_key was already used for another batch")
	ErrReasonRequired      = errors.New("reason is required (3-300 characters)")
	ErrInvalidStatusFilter = errors.New("status must be created, exported or cancelled")
	ErrInvalidPageToken    = errors.New("page_token is not a token from a previous page")
	ErrInvalidCode         = errors.New("a voucher code is 16 letters and digits, like ABCD-EFGH-JKMN-PQRS")

	ErrBatchNotFound   = errors.New("voucher batch not found")
	ErrVoucherNotFound = errors.New("voucher not found")
	// ErrNotExportable: the batch was exported already, or cancelled.
	ErrNotExportable  = errors.New("only a batch not exported yet can be exported; its codes are shown once")
	ErrNotCancellable = errors.New("the batch is cancelled already")
	ErrNotVoidable    = errors.New("only a voucher not redeemed yet can be voided")

	// What a rider hears about a code.
	ErrCodeNotValid   = errors.New("this voucher code is not valid")
	ErrCodeUsed       = errors.New("this voucher code was already used")
	ErrCodeCancelled  = errors.New("this voucher code was cancelled")
	ErrCodeExpired    = errors.New("this voucher code has expired")
	ErrCurrencyDiffer = errors.New("the voucher's currency is not the wallet's")

	// ErrCodeTaken: a new code collided with an existing one (issue again).
	ErrCodeTaken = errors.New("a generated voucher code is taken")
)

// TooManyAttemptsError is a rider who typed too many wrong codes lately.
type TooManyAttemptsError struct{ Until time.Time }

func (e *TooManyAttemptsError) Error() string {
	return "too many wrong voucher codes: try again after " + e.Until.UTC().Format(time.RFC3339)
}

// BatchStatus is where a batch stands.
type BatchStatus string

const (
	// BatchCreated: the codes exist, sealed; nothing is redeemable yet.
	BatchCreated   BatchStatus = "created"
	BatchExported  BatchStatus = "exported"
	BatchCancelled BatchStatus = "cancelled"
)

// Status is where one voucher stands.
type Status string

const (
	Available Status = "available"
	Redeemed  Status = "redeemed"
	Void      Status = "void"
)

// Batch is vouchers issued together, of one amount.
type Batch struct {
	ID             string
	Number         int64
	Label          string
	Seller         string
	Amount         wallet.Money
	CurrencyCode   string
	Quantity       int
	Status         BatchStatus
	ExpiresAt      time.Time
	CreatedBy      string
	IdempotencyKey string
	CreatedAt      time.Time
	ExportedAt     *time.Time
	CancelledAt    *time.Time
	CancelReason   string

	RedeemedCount  int
	VoidCount      int
	RedeemedAmount wallet.Money
}

// Serial is the number printed next to a voucher's code.
func Serial(batchNumber int64, index int) string {
	return fmt.Sprintf("V%d-%05d", batchNumber, index)
}

// Voucher is one code of a batch, as staff see it (never the code).
type Voucher struct {
	ID                string
	Serial            string
	BatchID           string
	BatchLabel        string
	BatchStatus       BatchStatus
	Amount            wallet.Money
	CurrencyCode      string
	Status            Status
	ExpiresAt         time.Time
	RedeemedByRiderID string
	RedeemedAt        *time.Time
	TransactionID     string
	VoidedAt          *time.Time
	VoidReason        string
}

// RedeemableAt: a rider could redeem it at now.
func (v Voucher) RedeemableAt(now time.Time) bool {
	return v.Status == Available && v.BatchStatus == BatchExported && now.Before(v.ExpiresAt)
}

// Sealed is a new voucher's code as it is stored: its hash, and the code
// sealed until the batch is exported.
type Sealed struct {
	Hash   []byte
	Sealed []byte
}

// Exported is one voucher of an exported batch, with its code.
type Exported struct {
	Serial string
	Code   string
}

// Redemption is a voucher redeemed into a rider's wallet.
type Redemption struct {
	Serial       string
	Amount       wallet.Money
	CurrencyCode string
	Wallet       wallet.Wallet
	Transaction  wallet.Transaction
}

// Store keeps batches and vouchers and moves the money.
type Store interface {
	Config(ctx context.Context) (wallet.Config, error)
	// CreateBatch stores the batch and its vouchers (serials from the batch
	// number, in the codes' order). wallet.ErrDuplicateRequest when the
	// creator's key is used; ErrCodeTaken when a code hash is.
	CreateBatch(ctx context.Context, batch Batch, codes []Sealed) (Batch, error)
	FindBatchByKey(ctx context.Context, createdBy, key string) (Batch, bool, error)
	// GetBatch is the batch with its counts; ErrBatchNotFound.
	GetBatch(ctx context.Context, id string) (Batch, error)
	// ListBatches: newest first; status "" for all.
	ListBatches(ctx context.Context, status BatchStatus, offset, limit int) ([]Batch, error)
	// ExportBatch locks a created batch, opens every sealed code with open,
	// wipes them and marks the batch exported, in one transaction; nothing
	// changes if open fails. ErrNotExportable when it is not created.
	ExportBatch(ctx context.Context, id, by string, now time.Time, open func(hash, sealed []byte) (string, error)) (Batch, []Exported, error)
	// CancelBatch cancels a batch not cancelled yet and wipes codes never
	// exported. ErrNotCancellable otherwise.
	CancelBatch(ctx context.Context, id, by, reason string, now time.Time) (Batch, error)
	// GetVoucher by serial; ErrVoucherNotFound.
	GetVoucher(ctx context.Context, serial string) (Voucher, error)
	// VoidVoucher voids an available voucher; ErrNotVoidable otherwise.
	VoidVoucher(ctx context.Context, serial, by, reason string, now time.Time) (Voucher, error)
	// Redeem credits the rider's wallet with the voucher whose code hashes to
	// hash, in one transaction with the voucher locked (so it is redeemed
	// once). The same rider again gets their redemption back. Errors:
	// ErrCodeNotValid (unknown, or its batch not exported), ErrCodeUsed,
	// ErrCodeCancelled, ErrCodeExpired.
	Redeem(ctx context.Context, riderID string, hash []byte, now time.Time) (Redemption, error)
	// Failures counts the rider's wrong codes since, and the oldest of them.
	Failures(ctx context.Context, riderID string, since time.Time) (int, time.Time, error)
	// RecordFailure records a wrong code (and forgets the rider's older than
	// forgetBefore).
	RecordFailure(ctx context.Context, riderID string, at, forgetBefore time.Time) error
}
