package transfer

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

const (
	maxNoteLength     = 140
	maxKeyLength      = 120
	defaultPageSize   = 20
	maxPageSize       = 100
	moneyDecimalScale = 3
)

var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{1,14}$`)

// Service sends and lists transfers.
type Service struct {
	store    Store
	identity Identity
	riders   Riders
}

func NewService(store Store, identity Identity, riders Riders) *Service {
	if store == nil {
		panic("transfer store is required")
	}

	if identity == nil {
		panic("identity client is required")
	}

	if riders == nil {
		panic("rider resolver is required")
	}

	return &Service{store: store, identity: identity, riders: riders}
}

// SendInput is a rider sending money. SenderIdentityID is the person
// sending (from their access token): their PIN is checked.
type SendInput struct {
	SenderRiderID    string
	SenderIdentityID string
	RecipientPhone   string
	Amount           wallet.Money
	Note             string
	PIN              string
	IdempotencyKey   string
}

// Send moves the amount to the recipient. A retry with the same key returns
// the transfer already made.
func (s *Service) Send(ctx context.Context, input SendInput) (Transfer, wallet.Wallet, error) {
	sender := strings.TrimSpace(input.SenderRiderID)
	identityID := strings.TrimSpace(input.SenderIdentityID)
	key := strings.TrimSpace(input.IdempotencyKey)
	note := strings.TrimSpace(input.Note)
	phone := strings.TrimSpace(input.RecipientPhone)

	switch {
	case sender == "":
		return Transfer{}, wallet.Wallet{}, ErrSenderRequired
	case identityID == "":
		return Transfer{}, wallet.Wallet{}, ErrIdentityRequired
	case key == "" || len(key) > maxKeyLength:
		return Transfer{}, wallet.Wallet{}, ErrIdempotencyKeyNeeded
	case !input.Amount.IsPositive() || !input.Amount.Equal(input.Amount.Round(moneyDecimalScale)):
		return Transfer{}, wallet.Wallet{}, ErrInvalidAmount
	case utf8.RuneCountInString(note) > maxNoteLength:
		return Transfer{}, wallet.Wallet{}, ErrNoteTooLong
	case !phonePattern.MatchString(phone):
		return Transfer{}, wallet.Wallet{}, ErrInvalidPhone
	}

	// A retry: the transfer is made already. Nothing is checked again (the
	// PIN was, the first time), nothing moves again.
	if done, found, err := s.store.FindByKey(ctx, sender, key); err != nil {
		return Transfer{}, wallet.Wallet{}, fmt.Errorf("look up the idempotency key: %w", err)
	} else if found {
		return s.replay(done, phone, input.Amount)
	}

	config, err := s.store.Config(ctx)
	if err != nil {
		return Transfer{}, wallet.Wallet{}, fmt.Errorf("read the wallet config: %w", err)
	}

	switch {
	case input.Amount.LessThan(config.TransferMinAmount):
		return Transfer{}, wallet.Wallet{}, ErrBelowMin
	case input.Amount.GreaterThan(config.TransferMaxAmount):
		return Transfer{}, wallet.Wallet{}, ErrAboveMax
	}

	// The PIN first: without it, nobody learns whether a phone belongs to a
	// rider.
	if err := s.checkPIN(ctx, identityID, input.PIN); err != nil {
		return Transfer{}, wallet.Wallet{}, err
	}

	recipientIdentity, found, err := s.identity.FindByPhone(ctx, phone)
	if err != nil {
		return Transfer{}, wallet.Wallet{}, err
	}

	if !found {
		return Transfer{}, wallet.Wallet{}, ErrRecipientNotFound
	}

	recipient, err := s.riders.RiderID(ctx, recipientIdentity)
	if err != nil {
		return Transfer{}, wallet.Wallet{}, fmt.Errorf("find the recipient's rider profile: %w", err)
	}

	if recipient == "" {
		return Transfer{}, wallet.Wallet{}, ErrRecipientNotFound
	}

	if recipient == sender || recipientIdentity == identityID {
		return Transfer{}, wallet.Wallet{}, ErrToSelf
	}

	senderPhone, err := s.identity.PhoneOf(ctx, identityID)
	if err != nil {
		return Transfer{}, wallet.Wallet{}, fmt.Errorf("read the sender's phone: %w", err)
	}

	sent, balance, err := s.store.Send(ctx, Record{
		Transfer: Transfer{
			SenderRiderID:    sender,
			RecipientRiderID: recipient,
			SenderPhone:      senderPhone,
			RecipientPhone:   phone,
			CurrencyCode:     config.CurrencyCode,
			Amount:           input.Amount,
			Note:             note,
			IdempotencyKey:   key,
		},
		DailyAmount: config.TransferDailyAmount,
		DailyCount:  config.TransferDailyCount,
	})

	if errors.Is(err, wallet.ErrDuplicateRequest) {
		// The same key raced this send and won: that one is the transfer.
		done, found, findErr := s.store.FindByKey(ctx, sender, key)
		if findErr != nil {
			return Transfer{}, wallet.Wallet{}, fmt.Errorf("read the transfer made meanwhile: %w", findErr)
		}

		if found {
			return s.replay(done, phone, input.Amount)
		}
	}

	if err != nil {
		return Transfer{}, wallet.Wallet{}, err
	}

	return sent, balance, nil
}

func (s *Service) replay(done Transfer, phone string, amount wallet.Money) (Transfer, wallet.Wallet, error) {
	if done.RecipientPhone != phone || !done.Amount.Equal(amount) {
		return Transfer{}, wallet.Wallet{}, ErrKeyReused
	}

	return done, wallet.Wallet{}, nil
}

func (s *Service) checkPIN(ctx context.Context, identityID, pin string) error {
	result, err := s.identity.VerifyPIN(ctx, identityID, pin)
	if err != nil {
		return fmt.Errorf("verify the wallet PIN: %w", err)
	}

	switch result.Check {
	case PINOK:
		return nil
	case PINNotSet:
		return ErrPINNotSet
	case PINLocked:
		return &LockedPINError{Until: result.LockedUntil}
	default:
		return &WrongPINError{AttemptsLeft: result.AttemptsLeft}
	}
}

// Page is one page of transfers; NextOffset 0 means there is no next one.
type Page struct {
	Transfers  []Transfer
	NextOffset int
}

// ErrInvalidPageToken: a page token that no listing gave.
var ErrInvalidPageToken = errors.New("page_token is not a token from a previous page")

// List pages a rider's transfers, newest first.
func (s *Service) List(ctx context.Context, riderID string, pageSize int, pageToken string) (Page, error) {
	riderID = strings.TrimSpace(riderID)
	if riderID == "" {
		return Page{}, ErrSenderRequired
	}

	limit := pageSize

	switch {
	case limit <= 0:
		limit = defaultPageSize
	case limit > maxPageSize:
		limit = maxPageSize
	}

	offset := 0

	if token := strings.TrimSpace(pageToken); token != "" {
		value, err := strconv.Atoi(token)
		if err != nil || value < 0 || strconv.Itoa(value) != token {
			return Page{}, ErrInvalidPageToken
		}

		offset = value
	}

	transfers, err := s.store.List(ctx, riderID, offset, limit+1)
	if err != nil {
		return Page{}, fmt.Errorf("list transfers: %w", err)
	}

	if len(transfers) > limit {
		return Page{Transfers: transfers[:limit], NextOffset: offset + limit}, nil
	}

	return Page{Transfers: transfers}, nil
}

// MaskPhone hides the middle of a phone for a notification: the country and
// network prefix and the last four digits stay.
func MaskPhone(phone string) string {
	if len(phone) < 10 {
		return phone
	}

	return phone[:len(phone)-7] + "***" + phone[len(phone)-4:]
}
