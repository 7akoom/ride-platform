package wallet_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

type fakeStatements struct {
	query   wallet.StatementQuery
	entries int
}

func (f *fakeStatements) Statement(_ context.Context, query wallet.StatementQuery) (wallet.Statement, error) {
	f.query = query

	return wallet.Statement{CurrencyCode: "IQD", Entries: make([]wallet.Transaction, f.entries)}, nil
}

var statementNow = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

func TestAStatementDefaultsToTheLastThirtyDays(t *testing.T) {
	reader := &fakeStatements{entries: 51}

	statement, err := wallet.GetStatement(context.Background(), reader, wallet.StatementInput{
		OwnerType: wallet.OwnerRider, OwnerID: " rider-1 ", Direction: " IN ",
		Types: []wallet.TransactionType{wallet.TxTopUp, wallet.TxDuePayment},
	}, statementNow)
	if err != nil {
		t.Fatal(err)
	}

	q := reader.query
	if q.OwnerID != "rider-1" || q.Direction != "in" || !q.To.Equal(statementNow) ||
		!q.From.Equal(statementNow.Add(-30*24*time.Hour)) || q.Offset != 0 || q.Limit != 51 || len(q.Types) != 2 {
		t.Fatalf("query %+v", q)
	}

	if len(statement.Entries) != 50 || statement.NextOffset != 50 {
		t.Fatalf("%d entries, next %d", len(statement.Entries), statement.NextOffset)
	}
}

func TestTheLastPageHasNoNext(t *testing.T) {
	reader := &fakeStatements{entries: 3}

	statement, err := wallet.GetStatement(context.Background(), reader, wallet.StatementInput{
		OwnerType: wallet.OwnerDriver, OwnerID: "driver-1", PageSize: 500, PageToken: "200",
	}, statementNow)
	if err != nil || statement.NextOffset != 0 || reader.query.Offset != 200 || reader.query.Limit != 201 {
		t.Fatalf("statement %+v, query %+v, err %v", statement, reader.query, err)
	}
}

func TestAStatementIsChecked(t *testing.T) {
	cases := map[string]struct {
		input wallet.StatementInput
		want  error
	}{
		"owner type": {wallet.StatementInput{OwnerID: "x"}, wallet.ErrInvalidOwnerType},
		"owner":      {wallet.StatementInput{OwnerType: wallet.OwnerRider}, wallet.ErrOwnerIDRequired},
		"direction":  {wallet.StatementInput{OwnerType: wallet.OwnerRider, OwnerID: "x", Direction: "both"}, wallet.ErrInvalidDirection},
		"type":       {wallet.StatementInput{OwnerType: wallet.OwnerRider, OwnerID: "x", Types: []wallet.TransactionType{"gift"}}, wallet.ErrInvalidType},
		"reversed": {wallet.StatementInput{OwnerType: wallet.OwnerRider, OwnerID: "x",
			From: statementNow, To: statementNow.Add(-time.Hour)}, wallet.ErrInvalidPeriod},
		"too long": {wallet.StatementInput{OwnerType: wallet.OwnerRider, OwnerID: "x",
			From: statementNow.Add(-367 * 24 * time.Hour)}, wallet.ErrInvalidPeriod},
		"token": {wallet.StatementInput{OwnerType: wallet.OwnerRider, OwnerID: "x", PageToken: "-1"}, wallet.ErrInvalidPageToken},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			reader := &fakeStatements{}

			if _, err := wallet.GetStatement(context.Background(), reader, c.input, statementNow); !errors.Is(err, c.want) {
				t.Fatalf("got %v, want %v", err, c.want)
			}

			if reader.query.OwnerID != "" {
				t.Fatal("the store was read")
			}
		})
	}
}
