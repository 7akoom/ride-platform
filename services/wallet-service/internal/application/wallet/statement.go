package wallet

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidDirection  = errors.New("direction must be in or out")
	ErrInvalidType       = errors.New("an unknown transaction type")
	ErrInvalidPeriod     = errors.New("from must be before to, at most 366 days apart")
	ErrInvalidPageToken  = errors.New("page_token is not a token from a previous page")
	statementMaxPeriod   = 366 * 24 * time.Hour
	statementDefaultSpan = 30 * 24 * time.Hour
)

const (
	defaultStatementPage = 50
	maxStatementPage     = 200
)

// StatementQuery is what a repository reads: one wallet's ledger rows in
// [From, To), with the direction and types kept, a page of them.
type StatementQuery struct {
	OwnerType OwnerType
	OwnerID   string
	// Direction is "in", "out" or "" for both.
	Direction string
	Types     []TransactionType
	From      time.Time
	To        time.Time
	Offset    int
	Limit     int
}

// Statement is a wallet over a period: the balance before and at its end,
// what came in and went out (of the rows kept), and a page of rows, newest
// first. NextOffset 0 means there is no next page.
type Statement struct {
	CurrencyCode string
	Entries      []Transaction
	Opening      Money
	Closing      Money
	TotalIn      Money
	TotalOut     Money
	NextOffset   int
}

// StatementReader reads statements.
type StatementReader interface {
	Statement(ctx context.Context, query StatementQuery) (Statement, error)
}

// StatementInput asks for a statement. Zero From is 30 days before To; zero
// To is now.
type StatementInput struct {
	OwnerType OwnerType
	OwnerID   string
	Direction string
	Types     []TransactionType
	From      time.Time
	To        time.Time
	PageSize  int
	PageToken string
}

var knownTypes = map[TransactionType]bool{
	TxTopUp: true, TxTripPayment: true, TxTripEarning: true, TxCommission: true, TxPayout: true,
	TxAdjustment: true, TxChangeCredit: true, TxTransferOut: true, TxTransferIn: true, TxDuePayment: true,
	TxVoucher: true, TxRefund: true, TxPayoutReturn: true, TxTip: true,
}

// GetStatement checks the input and reads the statement.
func GetStatement(ctx context.Context, reader StatementReader, input StatementInput, now time.Time) (Statement, error) {
	if !input.OwnerType.Valid() {
		return Statement{}, ErrInvalidOwnerType
	}

	ownerID := strings.TrimSpace(input.OwnerID)
	if ownerID == "" {
		return Statement{}, ErrOwnerIDRequired
	}

	direction := strings.ToLower(strings.TrimSpace(input.Direction))
	if direction != "" && direction != "in" && direction != "out" {
		return Statement{}, ErrInvalidDirection
	}

	for _, t := range input.Types {
		if !knownTypes[t] {
			return Statement{}, ErrInvalidType
		}
	}

	to := input.To
	if to.IsZero() {
		to = now
	}

	from := input.From
	if from.IsZero() {
		from = to.Add(-statementDefaultSpan)
	}

	if !from.Before(to) || to.Sub(from) > statementMaxPeriod {
		return Statement{}, ErrInvalidPeriod
	}

	limit := input.PageSize

	switch {
	case limit <= 0:
		limit = defaultStatementPage
	case limit > maxStatementPage:
		limit = maxStatementPage
	}

	offset := 0

	if token := strings.TrimSpace(input.PageToken); token != "" {
		value, err := strconv.Atoi(token)
		if err != nil || value < 0 || strconv.Itoa(value) != token {
			return Statement{}, ErrInvalidPageToken
		}

		offset = value
	}

	statement, err := reader.Statement(ctx, StatementQuery{
		OwnerType: input.OwnerType,
		OwnerID:   ownerID,
		Direction: direction,
		Types:     input.Types,
		From:      from,
		To:        to,
		Offset:    offset,
		Limit:     limit + 1,
	})
	if err != nil {
		return Statement{}, fmt.Errorf("read statement: %w", err)
	}

	if len(statement.Entries) > limit {
		statement.Entries = statement.Entries[:limit]
		statement.NextOffset = offset + limit
	}

	return statement, nil
}
