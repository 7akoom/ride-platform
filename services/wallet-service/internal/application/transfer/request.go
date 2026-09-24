package transfer

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

var (
	ErrRequestNotFound = errors.New("money request not found")
	// ErrRequestNotPending: it was paid, declined, cancelled or it expired.
	ErrRequestNotPending   = errors.New("the money request is no longer pending")
	ErrRequestExpired      = errors.New("the money request has expired")
	ErrNotYourRequest      = errors.New("only the rider it is for may do this")
	ErrOwnRequest          = errors.New("a rider cannot pay their own request")
	ErrInvalidExpiry       = errors.New("expires_in_hours must be between 1 and 168")
	ErrInvalidRequestRole  = errors.New("role must be incoming or outgoing")
	ErrInvalidStatusFilter = errors.New("status must be pending, paid, declined, cancelled or expired")
	// ErrCodeTaken: a new code collided with an existing one (try another).
	ErrCodeTaken = errors.New("the request code is taken")
)

// RequestStatus is where a money request stands.
type RequestStatus string

const (
	RequestPending   RequestStatus = "pending"
	RequestPaid      RequestStatus = "paid"
	RequestDeclined  RequestStatus = "declined"
	RequestCancelled RequestStatus = "cancelled"
	// RequestExpired is a pending request past its end; it is never stored.
	RequestExpired RequestStatus = "expired"
)

const (
	defaultRequestHours = 72
	maxRequestHours     = 168
	codeLength          = 10
	// codeAlphabet leaves out 0/O and 1/I/L, so a code read aloud or typed
	// from a screen is not mistaken.
	codeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
)

// MoneyRequest is a rider asking for money.
type MoneyRequest struct {
	ID               string
	Code             string
	RequesterRiderID string
	RequesterPhone   string
	// PayerRiderID is the rider it is for (Open false), or for an open
	// request whoever paid it.
	PayerRiderID   string
	PayerPhone     string
	Open           bool
	CurrencyCode   string
	Amount         wallet.Money
	Note           string
	Status         RequestStatus
	TransferID     string
	IdempotencyKey string
	ExpiresAt      time.Time
	CreatedAt      time.Time
	ClosedAt       *time.Time
}

// StatusAt is the status, with a pending request past its end expired.
func (r MoneyRequest) StatusAt(now time.Time) RequestStatus {
	if r.Status == RequestPending && !now.Before(r.ExpiresAt) {
		return RequestExpired
	}

	return r.Status
}

// Role is how the rider stands to the request.
func (r MoneyRequest) Role(riderID string) string {
	switch {
	case r.RequesterRiderID == riderID:
		return "requester"
	case r.PayerRiderID == riderID:
		return "payer"
	default:
		return "viewer"
	}
}

// visibleTo: an open request is seen by anyone with its code; one for a
// rider only by that rider and the requester.
func (r MoneyRequest) visibleTo(riderID string) bool {
	return r.Open || r.RequesterRiderID == riderID || r.PayerRiderID == riderID
}

// Requests stores money requests.
type Requests interface {
	// CreateRequest stores a new request (with the wallet.money_requested
	// event when it is for a rider). ErrCodeTaken when the code is used;
	// wallet.ErrDuplicateRequest when the requester's key is.
	CreateRequest(ctx context.Context, request MoneyRequest) (MoneyRequest, error)
	FindRequestByCode(ctx context.Context, code string) (MoneyRequest, bool, error)
	FindRequestByKey(ctx context.Context, requesterRiderID, key string) (MoneyRequest, bool, error)
	// ListRequests: role outgoing (asked by the rider) or incoming (for the
	// rider or paid by them); status filters (expired: pending past the
	// end). Newest first.
	ListRequests(ctx context.Context, riderID, role string, status RequestStatus, now time.Time, offset, limit int) ([]MoneyRequest, error)
	// CloseRequest moves a pending request that has not expired at now to
	// declined or cancelled; ErrRequestNotPending otherwise.
	CloseRequest(ctx context.Context, requestID string, status RequestStatus, now time.Time) (MoneyRequest, error)
	// PayRequest pays a pending request that has not expired, with the
	// transfer in the record, in one transaction: the request is locked, so
	// it is paid once. ErrRequestNotPending otherwise, and the transfer's
	// errors.
	PayRequest(ctx context.Context, requestID string, payment Record, now time.Time) (MoneyRequest, Transfer, wallet.Wallet, error)
}

// WithRequests gives the service money requests.
func (s *Service) WithRequests(requests Requests) *Service {
	s.requests = requests

	return s
}

// RequestInput is a rider asking for money.
type RequestInput struct {
	RequesterRiderID    string
	RequesterIdentityID string
	// PayerPhone is empty for an open request.
	PayerPhone     string
	Amount         wallet.Money
	Note           string
	ExpiresInHours int
	IdempotencyKey string
}

func (s *Service) requestsOrFail() error {
	if s.requests == nil {
		return errors.New("money requests are not configured")
	}

	return nil
}

// CreateRequest asks for money. A retry with the same key returns the
// request already made.
func (s *Service) CreateRequest(ctx context.Context, input RequestInput) (MoneyRequest, error) {
	if err := s.requestsOrFail(); err != nil {
		return MoneyRequest{}, err
	}

	requester := strings.TrimSpace(input.RequesterRiderID)
	identityID := strings.TrimSpace(input.RequesterIdentityID)
	key := strings.TrimSpace(input.IdempotencyKey)
	note := strings.TrimSpace(input.Note)
	phone := strings.TrimSpace(input.PayerPhone)

	hours := input.ExpiresInHours
	if hours == 0 {
		hours = defaultRequestHours
	}

	switch {
	case requester == "":
		return MoneyRequest{}, ErrSenderRequired
	case identityID == "":
		return MoneyRequest{}, ErrIdentityRequired
	case key == "" || len(key) > maxKeyLength:
		return MoneyRequest{}, ErrIdempotencyKeyNeeded
	case !input.Amount.IsPositive() || !input.Amount.Equal(input.Amount.Round(moneyDecimalScale)):
		return MoneyRequest{}, ErrInvalidAmount
	case utf8.RuneCountInString(note) > maxNoteLength:
		return MoneyRequest{}, ErrNoteTooLong
	case phone != "" && !phonePattern.MatchString(phone):
		return MoneyRequest{}, ErrInvalidPhone
	case hours < 1 || hours > maxRequestHours:
		return MoneyRequest{}, ErrInvalidExpiry
	}

	if done, found, err := s.requests.FindRequestByKey(ctx, requester, key); err != nil {
		return MoneyRequest{}, fmt.Errorf("look up the idempotency key: %w", err)
	} else if found {
		return replayRequest(done, phone, input.Amount)
	}

	config, err := s.store.Config(ctx)
	if err != nil {
		return MoneyRequest{}, fmt.Errorf("read the wallet config: %w", err)
	}

	// What may be asked is what may be sent.
	switch {
	case input.Amount.LessThan(config.TransferMinAmount):
		return MoneyRequest{}, ErrBelowMin
	case input.Amount.GreaterThan(config.TransferMaxAmount):
		return MoneyRequest{}, ErrAboveMax
	}

	request := MoneyRequest{
		RequesterRiderID: requester,
		Open:             phone == "",
		CurrencyCode:     config.CurrencyCode,
		Amount:           input.Amount,
		Note:             note,
		Status:           RequestPending,
		IdempotencyKey:   key,
		ExpiresAt:        s.now().Add(time.Duration(hours) * time.Hour),
	}

	if phone != "" {
		payer, err := s.riderByPhone(ctx, phone)
		if err != nil {
			return MoneyRequest{}, err
		}

		if payer == requester {
			return MoneyRequest{}, ErrToSelf
		}

		request.PayerRiderID, request.PayerPhone = payer, phone
	}

	if request.RequesterPhone, err = s.identity.PhoneOf(ctx, identityID); err != nil {
		return MoneyRequest{}, fmt.Errorf("read the requester's phone: %w", err)
	}

	for attempt := 0; attempt < 5; attempt++ {
		if request.Code, err = newCode(); err != nil {
			return MoneyRequest{}, err
		}

		created, err := s.requests.CreateRequest(ctx, request)

		switch {
		case errors.Is(err, ErrCodeTaken):
			continue
		case errors.Is(err, wallet.ErrDuplicateRequest):
			done, found, findErr := s.requests.FindRequestByKey(ctx, requester, key)
			if findErr != nil || !found {
				return MoneyRequest{}, fmt.Errorf("read the request made meanwhile: %w", findErr)
			}

			return replayRequest(done, phone, input.Amount)
		case err != nil:
			return MoneyRequest{}, err
		}

		return created, nil
	}

	return MoneyRequest{}, errors.New("could not find a free request code")
}

func replayRequest(done MoneyRequest, phone string, amount wallet.Money) (MoneyRequest, error) {
	if done.Open != (phone == "") || (!done.Open && done.PayerPhone != phone) || !done.Amount.Equal(amount) {
		return MoneyRequest{}, ErrKeyReused
	}

	return done, nil
}

// GetRequest opens a request by its code for the rider.
func (s *Service) GetRequest(ctx context.Context, riderID, code string) (MoneyRequest, error) {
	if err := s.requestsOrFail(); err != nil {
		return MoneyRequest{}, err
	}

	riderID = strings.TrimSpace(riderID)

	request, found, err := s.requests.FindRequestByCode(ctx, normalizeCode(code))
	if err != nil {
		return MoneyRequest{}, fmt.Errorf("read the money request: %w", err)
	}

	if !found || !request.visibleTo(riderID) {
		return MoneyRequest{}, ErrRequestNotFound
	}

	return request, nil
}

// RequestPage is one page of requests; NextOffset 0 means no next one.
type RequestPage struct {
	Requests   []MoneyRequest
	NextOffset int
}

// ListRequests pages the rider's requests.
func (s *Service) ListRequests(ctx context.Context, riderID, role, status string, pageSize int, pageToken string) (RequestPage, error) {
	if err := s.requestsOrFail(); err != nil {
		return RequestPage{}, err
	}

	riderID = strings.TrimSpace(riderID)
	if riderID == "" {
		return RequestPage{}, ErrSenderRequired
	}

	role = strings.ToLower(strings.TrimSpace(role))
	if role != "incoming" && role != "outgoing" {
		return RequestPage{}, ErrInvalidRequestRole
	}

	filter := RequestStatus(strings.ToLower(strings.TrimSpace(status)))

	switch filter {
	case "", RequestPending, RequestPaid, RequestDeclined, RequestCancelled, RequestExpired:
	default:
		return RequestPage{}, ErrInvalidStatusFilter
	}

	offset, limit, err := paging(pageSize, pageToken)
	if err != nil {
		return RequestPage{}, err
	}

	requests, err := s.requests.ListRequests(ctx, riderID, role, filter, s.now(), offset, limit+1)
	if err != nil {
		return RequestPage{}, fmt.Errorf("list money requests: %w", err)
	}

	if len(requests) > limit {
		return RequestPage{Requests: requests[:limit], NextOffset: offset + limit}, nil
	}

	return RequestPage{Requests: requests}, nil
}

// PayInput is a rider paying a request with their PIN.
type PayInput struct {
	PayerRiderID    string
	PayerIdentityID string
	Code            string
	PIN             string
}

// PayRequest pays a request: a transfer from the payer to the requester. Paying
// again what the rider already paid returns that payment.
func (s *Service) PayRequest(ctx context.Context, input PayInput) (MoneyRequest, Transfer, wallet.Wallet, error) {
	payer := strings.TrimSpace(input.PayerRiderID)
	identityID := strings.TrimSpace(input.PayerIdentityID)

	if identityID == "" {
		return MoneyRequest{}, Transfer{}, wallet.Wallet{}, ErrIdentityRequired
	}

	request, err := s.GetRequest(ctx, payer, input.Code)
	if err != nil {
		return MoneyRequest{}, Transfer{}, wallet.Wallet{}, err
	}

	key := "request:" + request.ID

	// A retry of a payment that went through.
	if request.Status == RequestPaid && request.PayerRiderID == payer {
		done, found, err := s.store.FindByKey(ctx, payer, key)
		if err != nil {
			return MoneyRequest{}, Transfer{}, wallet.Wallet{}, fmt.Errorf("read the payment: %w", err)
		}

		if found {
			return request, done, wallet.Wallet{}, nil
		}
	}

	switch {
	case request.RequesterRiderID == payer:
		return MoneyRequest{}, Transfer{}, wallet.Wallet{}, ErrOwnRequest
	case request.StatusAt(s.now()) == RequestExpired:
		return MoneyRequest{}, Transfer{}, wallet.Wallet{}, ErrRequestExpired
	case request.Status != RequestPending:
		return MoneyRequest{}, Transfer{}, wallet.Wallet{}, ErrRequestNotPending
	}

	config, err := s.store.Config(ctx)
	if err != nil {
		return MoneyRequest{}, Transfer{}, wallet.Wallet{}, fmt.Errorf("read the wallet config: %w", err)
	}

	// The limits may have changed since it was asked.
	switch {
	case request.Amount.LessThan(config.TransferMinAmount):
		return MoneyRequest{}, Transfer{}, wallet.Wallet{}, ErrBelowMin
	case request.Amount.GreaterThan(config.TransferMaxAmount):
		return MoneyRequest{}, Transfer{}, wallet.Wallet{}, ErrAboveMax
	}

	if err := s.checkPIN(ctx, identityID, input.PIN); err != nil {
		return MoneyRequest{}, Transfer{}, wallet.Wallet{}, err
	}

	payerPhone, err := s.identity.PhoneOf(ctx, identityID)
	if err != nil {
		return MoneyRequest{}, Transfer{}, wallet.Wallet{}, fmt.Errorf("read the payer's phone: %w", err)
	}

	paid, sent, balance, err := s.requests.PayRequest(ctx, request.ID, Record{
		Transfer: Transfer{
			SenderRiderID:    payer,
			RecipientRiderID: request.RequesterRiderID,
			SenderPhone:      payerPhone,
			RecipientPhone:   request.RequesterPhone,
			CurrencyCode:     request.CurrencyCode,
			Amount:           request.Amount,
			Note:             request.Note,
			IdempotencyKey:   key,
		},
		DailyAmount: config.TransferDailyAmount,
		DailyCount:  config.TransferDailyCount,
	}, s.now())
	if err != nil {
		return MoneyRequest{}, Transfer{}, wallet.Wallet{}, err
	}

	return paid, sent, balance, nil
}

// DeclineRequest is the rider a request is for saying no.
func (s *Service) DeclineRequest(ctx context.Context, riderID, code string) (MoneyRequest, error) {
	request, err := s.GetRequest(ctx, riderID, code)
	if err != nil {
		return MoneyRequest{}, err
	}

	if request.Open || request.PayerRiderID != strings.TrimSpace(riderID) {
		return MoneyRequest{}, ErrNotYourRequest
	}

	return s.close(ctx, request, RequestDeclined)
}

// CancelRequest is the requester taking it back.
func (s *Service) CancelRequest(ctx context.Context, riderID, code string) (MoneyRequest, error) {
	request, err := s.GetRequest(ctx, riderID, code)
	if err != nil {
		return MoneyRequest{}, err
	}

	if request.RequesterRiderID != strings.TrimSpace(riderID) {
		return MoneyRequest{}, ErrNotYourRequest
	}

	return s.close(ctx, request, RequestCancelled)
}

func (s *Service) close(ctx context.Context, request MoneyRequest, status RequestStatus) (MoneyRequest, error) {
	now := s.now()

	switch request.StatusAt(now) {
	case RequestExpired:
		return MoneyRequest{}, ErrRequestExpired
	case RequestPending:
	default:
		return MoneyRequest{}, ErrRequestNotPending
	}

	return s.requests.CloseRequest(ctx, request.ID, status, now)
}

// riderByPhone is the registered rider who signs in with the phone.
func (s *Service) riderByPhone(ctx context.Context, phone string) (string, error) {
	identityID, found, err := s.identity.FindByPhone(ctx, phone)
	if err != nil {
		return "", err
	}

	if !found {
		return "", ErrRecipientNotFound
	}

	riderID, err := s.riders.RiderID(ctx, identityID)
	if err != nil {
		return "", fmt.Errorf("find the rider profile: %w", err)
	}

	if riderID == "" {
		return "", ErrRecipientNotFound
	}

	return riderID, nil
}

func newCode() (string, error) {
	random := make([]byte, codeLength)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("make a request code: %w", err)
	}

	code := make([]byte, codeLength)
	for i, b := range random {
		code[i] = codeAlphabet[int(b)%len(codeAlphabet)]
	}

	return string(code), nil
}

// normalizeCode accepts a code typed in lower case or with spaces.
func normalizeCode(code string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), " ", ""))
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

	if token := strings.TrimSpace(pageToken); token != "" {
		value, err := strconv.Atoi(token)
		if err != nil || value < 0 || strconv.Itoa(value) != token {
			return 0, 0, ErrInvalidPageToken
		}

		offset = value
	}

	return offset, limit, nil
}
