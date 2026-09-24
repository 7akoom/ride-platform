package transfer

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

var requestNow = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

type fakeRequests struct {
	byCode    map[string]MoneyRequest
	created   []MoneyRequest
	createErr []error // returned by the next creates, in order
	closed    []RequestStatus
	paid      []Record
	payErr    error
	listArgs  []any
	list      []MoneyRequest
}

func newFakeRequests() *fakeRequests {
	return &fakeRequests{byCode: map[string]MoneyRequest{}}
}

func (f *fakeRequests) CreateRequest(_ context.Context, r MoneyRequest) (MoneyRequest, error) {
	if len(f.createErr) > 0 {
		err := f.createErr[0]
		f.createErr = f.createErr[1:]

		if err != nil {
			return MoneyRequest{}, err
		}
	}

	r.ID = "request-" + r.Code
	r.CreatedAt = requestNow
	f.created = append(f.created, r)
	f.byCode[r.Code] = r

	return r, nil
}

func (f *fakeRequests) FindRequestByCode(_ context.Context, code string) (MoneyRequest, bool, error) {
	r, ok := f.byCode[code]
	return r, ok, nil
}

func (f *fakeRequests) FindRequestByKey(_ context.Context, requester, key string) (MoneyRequest, bool, error) {
	for _, r := range f.byCode {
		if r.RequesterRiderID == requester && r.IdempotencyKey == key {
			return r, true, nil
		}
	}

	return MoneyRequest{}, false, nil
}

func (f *fakeRequests) ListRequests(_ context.Context, riderID, role string, status RequestStatus, _ time.Time, offset, limit int) ([]MoneyRequest, error) {
	f.listArgs = []any{riderID, role, status, offset, limit}
	return f.list, nil
}

func (f *fakeRequests) CloseRequest(_ context.Context, id string, status RequestStatus, now time.Time) (MoneyRequest, error) {
	f.closed = append(f.closed, status)

	for code, r := range f.byCode {
		if r.ID == id {
			r.Status = status
			r.ClosedAt = &now
			f.byCode[code] = r

			return r, nil
		}
	}

	return MoneyRequest{}, ErrRequestNotPending
}

func (f *fakeRequests) PayRequest(_ context.Context, id string, payment Record, now time.Time) (MoneyRequest, Transfer, wallet.Wallet, error) {
	f.paid = append(f.paid, payment)
	if f.payErr != nil {
		return MoneyRequest{}, Transfer{}, wallet.Wallet{}, f.payErr
	}

	for code, r := range f.byCode {
		if r.ID == id {
			r.Status, r.PayerRiderID, r.TransferID, r.ClosedAt = RequestPaid, payment.Transfer.SenderRiderID, "transfer-9", &now
			f.byCode[code] = r

			sent := payment.Transfer
			sent.ID = "transfer-9"

			return r, sent, wallet.Wallet{ID: "wallet-payer"}, nil
		}
	}

	return MoneyRequest{}, Transfer{}, wallet.Wallet{}, ErrRequestNotFound
}

func newRequestService() (*Service, *fakeStore, *fakeIdentity, *fakeRequests) {
	service, store, identity := newTestService()
	requests := newFakeRequests()
	service.WithRequests(requests)
	service.now = func() time.Time { return requestNow }

	return service, store, identity, requests
}

// The recipient of these tests asks the sender for money.
func validRequest() RequestInput {
	return RequestInput{
		RequesterRiderID: recipientRider, RequesterIdentityID: recipientIdentity,
		PayerPhone: senderPhone, Amount: dec(2500), Note: " lunch ", IdempotencyKey: "req-1",
	}
}

func TestAskingARiderForMoney(t *testing.T) {
	service, _, _, requests := newRequestService()

	created, err := service.CreateRequest(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}

	if created.Open || created.PayerRiderID != senderRider || created.PayerPhone != senderPhone ||
		created.Note != "lunch" || created.CurrencyCode != "IQD" || created.Status != RequestPending ||
		!created.ExpiresAt.Equal(requestNow.Add(72*time.Hour)) {
		t.Fatalf("created %+v", created)
	}

	if len(created.Code) != codeLength || strings.Trim(created.Code, codeAlphabet) != "" {
		t.Fatalf("code %q", created.Code)
	}

	// The requester's phone is read from their identity (the fake gives one
	// phone to every identity).
	if created.RequesterPhone != senderPhone || len(requests.created) != 1 {
		t.Fatalf("created %+v", requests.created)
	}
}

func TestAnOpenRequestHasNoPayer(t *testing.T) {
	service, _, _, _ := newRequestService()

	input := validRequest()
	input.PayerPhone = ""
	input.ExpiresInHours = 1

	created, err := service.CreateRequest(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	if !created.Open || created.PayerRiderID != "" || !created.ExpiresAt.Equal(requestNow.Add(time.Hour)) {
		t.Fatalf("created %+v", created)
	}
}

func TestARetriedRequestIsTheSameOne(t *testing.T) {
	service, _, _, requests := newRequestService()

	first, err := service.CreateRequest(context.Background(), validRequest())
	if err != nil {
		t.Fatal(err)
	}

	again, err := service.CreateRequest(context.Background(), validRequest())
	if err != nil || again.ID != first.ID || len(requests.created) != 1 {
		t.Fatalf("again %+v, err %v, created %d", again, err, len(requests.created))
	}

	other := validRequest()
	other.Amount = dec(3000)

	if _, err := service.CreateRequest(context.Background(), other); !errors.Is(err, ErrKeyReused) {
		t.Fatalf("got %v", err)
	}
}

func TestATakenCodeIsReplacedByAnother(t *testing.T) {
	service, _, _, requests := newRequestService()
	requests.createErr = []error{ErrCodeTaken, ErrCodeTaken, nil}

	if _, err := service.CreateRequest(context.Background(), validRequest()); err != nil {
		t.Fatal(err)
	}

	requests.createErr = []error{ErrCodeTaken, ErrCodeTaken, ErrCodeTaken, ErrCodeTaken, ErrCodeTaken}

	input := validRequest()
	input.IdempotencyKey = "req-2"

	if _, err := service.CreateRequest(context.Background(), input); err == nil {
		t.Fatal("five taken codes in a row must fail")
	}
}

func TestARequestIsChecked(t *testing.T) {
	cases := map[string]struct {
		change func(*RequestInput)
		want   error
	}{
		"no requester":   {func(i *RequestInput) { i.RequesterRiderID = "" }, ErrSenderRequired},
		"no identity":    {func(i *RequestInput) { i.RequesterIdentityID = "" }, ErrIdentityRequired},
		"no key":         {func(i *RequestInput) { i.IdempotencyKey = " " }, ErrIdempotencyKeyNeeded},
		"zero amount":    {func(i *RequestInput) { i.Amount = dec(0) }, ErrInvalidAmount},
		"long note":      {func(i *RequestInput) { i.Note = strings.Repeat("x", 141) }, ErrNoteTooLong},
		"bad phone":      {func(i *RequestInput) { i.PayerPhone = "0770" }, ErrInvalidPhone},
		"too long":       {func(i *RequestInput) { i.ExpiresInHours = 169 }, ErrInvalidExpiry},
		"negative hours": {func(i *RequestInput) { i.ExpiresInHours = -1 }, ErrInvalidExpiry},
		"below min":      {func(i *RequestInput) { i.Amount = dec(100) }, ErrBelowMin},
		"above max":      {func(i *RequestInput) { i.Amount = dec(100001) }, ErrAboveMax},
		"unknown phone":  {func(i *RequestInput) { i.PayerPhone = "+9647711111111" }, ErrRecipientNotFound},
		"to self":        {func(i *RequestInput) { i.PayerPhone = recipientPhone }, ErrToSelf},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			service, _, _, requests := newRequestService()

			input := validRequest()
			c.change(&input)

			if _, err := service.CreateRequest(context.Background(), input); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}

			if len(requests.created) != 0 {
				t.Fatal("a request was stored")
			}
		})
	}
}

func createdRequest(t *testing.T, service *Service, open bool) MoneyRequest {
	t.Helper()

	input := validRequest()
	if open {
		input.PayerPhone = ""
		input.IdempotencyKey = "req-open"
	}

	created, err := service.CreateRequest(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}

	return created
}

func TestWhoSeesARequest(t *testing.T) {
	service, _, _, _ := newRequestService()

	targeted := createdRequest(t, service, false)

	for rider, visible := range map[string]bool{recipientRider: true, senderRider: true, "rider-stranger": false} {
		_, err := service.GetRequest(context.Background(), rider, strings.ToLower(targeted.Code))
		if visible != (err == nil) {
			t.Fatalf("%s: err %v", rider, err)
		}

		if !visible && !errors.Is(err, ErrRequestNotFound) {
			t.Fatalf("%s: got %v", rider, err)
		}
	}

	service2, _, _, _ := newRequestService()
	open := createdRequest(t, service2, true)

	if _, err := service2.GetRequest(context.Background(), "rider-stranger", open.Code); err != nil {
		t.Fatalf("an open request is seen by anyone with its code: %v", err)
	}
}

func TestPayingARequest(t *testing.T) {
	service, _, identity, requests := newRequestService()
	request := createdRequest(t, service, false)

	paid, sent, balance, err := service.PayRequest(context.Background(), PayInput{
		PayerRiderID: senderRider, PayerIdentityID: senderIdentity, Code: request.Code, PIN: "2580",
	})
	if err != nil {
		t.Fatal(err)
	}

	record := requests.paid[0]
	if record.Transfer.SenderRiderID != senderRider || record.Transfer.RecipientRiderID != recipientRider ||
		!record.Transfer.Amount.Equal(dec(2500)) || record.Transfer.IdempotencyKey != "request:"+request.ID ||
		record.DailyCount != 5 || identity.pinCalls != 1 {
		t.Fatalf("record %+v, pin calls %d", record, identity.pinCalls)
	}

	if paid.Status != RequestPaid || sent.ID == "" || balance.ID == "" {
		t.Fatalf("paid %+v sent %+v balance %+v", paid, sent, balance)
	}
}

func TestPayingAgainReturnsThePayment(t *testing.T) {
	service, store, identity, requests := newRequestService()
	request := createdRequest(t, service, true)

	input := PayInput{PayerRiderID: senderRider, PayerIdentityID: senderIdentity, Code: request.Code, PIN: "2580"}

	if _, _, _, err := service.PayRequest(context.Background(), input); err != nil {
		t.Fatal(err)
	}

	// The store keeps the transfer under its key, as the real one does.
	store.byKey[senderRider+"/request:"+request.ID] = Transfer{ID: "transfer-9"}

	paid, sent, balance, err := service.PayRequest(context.Background(), input)
	if err != nil || paid.Status != RequestPaid || sent.ID != "transfer-9" || balance.ID != "" {
		t.Fatalf("paid %+v sent %+v balance %+v err %v", paid, sent, balance, err)
	}

	if len(requests.paid) != 1 || identity.pinCalls != 1 {
		t.Fatalf("paid %d times, pin checked %d times", len(requests.paid), identity.pinCalls)
	}

	// Someone else paying it is refused.
	if _, _, _, err := service.PayRequest(context.Background(), PayInput{
		PayerRiderID: "rider-other", PayerIdentityID: "identity-other", Code: request.Code, PIN: "2580",
	}); !errors.Is(err, ErrRequestNotPending) {
		t.Fatalf("got %v", err)
	}
}

func TestARequestIsNotPaidWhenItShouldNotBe(t *testing.T) {
	t.Run("own request", func(t *testing.T) {
		service, _, _, requests := newRequestService()
		request := createdRequest(t, service, true)

		_, _, _, err := service.PayRequest(context.Background(), PayInput{
			PayerRiderID: recipientRider, PayerIdentityID: recipientIdentity, Code: request.Code, PIN: "2580",
		})
		if !errors.Is(err, ErrOwnRequest) || len(requests.paid) != 0 {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("expired", func(t *testing.T) {
		service, _, _, requests := newRequestService()
		request := createdRequest(t, service, false)
		service.now = func() time.Time { return requestNow.Add(73 * time.Hour) }

		_, _, _, err := service.PayRequest(context.Background(), PayInput{
			PayerRiderID: senderRider, PayerIdentityID: senderIdentity, Code: request.Code, PIN: "2580",
		})
		if !errors.Is(err, ErrRequestExpired) || len(requests.paid) != 0 {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("declined", func(t *testing.T) {
		service, _, _, requests := newRequestService()
		request := createdRequest(t, service, false)

		if _, err := service.DeclineRequest(context.Background(), senderRider, request.Code); err != nil {
			t.Fatal(err)
		}

		_, _, _, err := service.PayRequest(context.Background(), PayInput{
			PayerRiderID: senderRider, PayerIdentityID: senderIdentity, Code: request.Code, PIN: "2580",
		})
		if !errors.Is(err, ErrRequestNotPending) || len(requests.paid) != 0 {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("wrong PIN", func(t *testing.T) {
		service, _, identity, requests := newRequestService()
		request := createdRequest(t, service, false)
		identity.pin = PINResult{Check: PINWrong, AttemptsLeft: 3}

		_, _, _, err := service.PayRequest(context.Background(), PayInput{
			PayerRiderID: senderRider, PayerIdentityID: senderIdentity, Code: request.Code, PIN: "1111",
		})

		var wrong *WrongPINError
		if !errors.As(err, &wrong) || len(requests.paid) != 0 {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("limits lowered since", func(t *testing.T) {
		service, store, _, requests := newRequestService()
		request := createdRequest(t, service, false)
		store.config.TransferMaxAmount = dec(1000)

		_, _, _, err := service.PayRequest(context.Background(), PayInput{
			PayerRiderID: senderRider, PayerIdentityID: senderIdentity, Code: request.Code, PIN: "2580",
		})
		if !errors.Is(err, ErrAboveMax) || len(requests.paid) != 0 {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("no person", func(t *testing.T) {
		service, _, _, _ := newRequestService()
		request := createdRequest(t, service, false)

		if _, _, _, err := service.PayRequest(context.Background(), PayInput{PayerRiderID: senderRider, Code: request.Code}); !errors.Is(err, ErrIdentityRequired) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestDecliningAndCancelling(t *testing.T) {
	service, _, _, requests := newRequestService()
	request := createdRequest(t, service, false)

	// Only the rider asked declines, only the requester cancels.
	if _, err := service.DeclineRequest(context.Background(), recipientRider, request.Code); !errors.Is(err, ErrNotYourRequest) {
		t.Fatalf("got %v", err)
	}

	if _, err := service.CancelRequest(context.Background(), senderRider, request.Code); !errors.Is(err, ErrNotYourRequest) {
		t.Fatalf("got %v", err)
	}

	cancelled, err := service.CancelRequest(context.Background(), recipientRider, request.Code)
	if err != nil || cancelled.Status != RequestCancelled {
		t.Fatalf("cancelled %+v, err %v", cancelled, err)
	}

	if _, err := service.CancelRequest(context.Background(), recipientRider, request.Code); !errors.Is(err, ErrRequestNotPending) {
		t.Fatalf("got %v", err)
	}

	if len(requests.closed) != 1 {
		t.Fatalf("closed %v", requests.closed)
	}

	// An open request has no one to decline it.
	open := createdRequest(t, service, true)
	if _, err := service.DeclineRequest(context.Background(), senderRider, open.Code); !errors.Is(err, ErrNotYourRequest) {
		t.Fatalf("got %v", err)
	}
}

func TestListingRequests(t *testing.T) {
	service, _, _, requests := newRequestService()
	requests.list = make([]MoneyRequest, 3)

	page, err := service.ListRequests(context.Background(), senderRider, " Incoming ", "EXPIRED", 2, "")
	if err != nil {
		t.Fatal(err)
	}

	if len(page.Requests) != 2 || page.NextOffset != 2 {
		t.Fatalf("page %+v", page)
	}

	if requests.listArgs[1] != "incoming" || requests.listArgs[2] != RequestExpired || requests.listArgs[4] != 3 {
		t.Fatalf("args %v", requests.listArgs)
	}

	for _, bad := range []struct{ role, status, token string }{
		{"both", "", ""}, {"incoming", "open", ""}, {"outgoing", "", "x"},
	} {
		if _, err := service.ListRequests(context.Background(), senderRider, bad.role, bad.status, 0, bad.token); err == nil {
			t.Fatalf("%+v must be refused", bad)
		}
	}
}

func TestAPendingRequestPastItsEndIsExpired(t *testing.T) {
	r := MoneyRequest{Status: RequestPending, ExpiresAt: requestNow}

	if r.StatusAt(requestNow.Add(-time.Second)) != RequestPending || r.StatusAt(requestNow) != RequestExpired {
		t.Fatal("expiry")
	}

	r.Status = RequestPaid
	if r.StatusAt(requestNow.Add(time.Hour)) != RequestPaid {
		t.Fatal("a paid request never expires")
	}
}
