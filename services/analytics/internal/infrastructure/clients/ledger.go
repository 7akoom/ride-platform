package clients

import (
	"context"
	"time"

	driverv1 "github.com/7akoom/ride-platform/gen/go/ride/driver/v1"
	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"github.com/shopspring/decimal"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/7akoom/ride-platform/services/analytics/internal/application/query"
	"github.com/7akoom/ride-platform/services/analytics/internal/domain"
)

const peerCallTimeout = 10 * time.Second

// Ledger asks wallet-service (as a service) for the ledger's totals.
type Ledger struct {
	wallet walletv1.WalletServiceClient
}

var _ query.Ledger = (*Ledger)(nil)

func NewLedger(conn grpc.ClientConnInterface) *Ledger {
	if conn == nil {
		panic("wallet-service connection is required")
	}

	return &Ledger{wallet: walletv1.NewWalletServiceClient(conn)}
}

func (l *Ledger) Summarize(ctx context.Context, start, end time.Time, timeZone string) (domain.Ledger, error) {
	ctx, cancel := context.WithTimeout(ctx, peerCallTimeout)
	defer cancel()

	response, err := l.wallet.SummarizeLedger(ctx, &walletv1.SummarizeLedgerRequest{
		Start: timestamppb.New(start), End: timestamppb.New(end), TimeZone: timeZone,
	})
	if err != nil {
		return domain.Ledger{}, domain.ErrUpstreamUnavailable
	}

	out := domain.Ledger{
		Currency: response.GetCurrencyCode(),
		Held: domain.MoneyHeld{
			RiderBalances:    amount(response.GetRiderBalances()),
			DriverCredit:     amount(response.GetDriverCredit()),
			DriverDebt:       amount(response.GetDriverDebt()),
			SuspendedDrivers: response.GetSuspendedDrivers(),
			RiderDues:        amount(response.GetRiderDues()),
		},
	}

	for _, t := range response.GetTotals() {
		day, err := time.Parse(time.DateOnly, t.GetDate())
		if err != nil {
			return domain.Ledger{}, domain.ErrUpstreamUnavailable
		}

		if n := len(out.Days); n == 0 || !out.Days[n-1].Day.Equal(day) {
			out.Days = append(out.Days, domain.LedgerDay{Day: day})
		}

		last := &out.Days[len(out.Days)-1]
		last.Lines = append(last.Lines, domain.LedgerLine{
			OwnerType: t.GetOwnerType(), Type: t.GetType(), Entries: t.GetEntries(),
			Credited: amount(t.GetCredited()), Debited: amount(t.GetDebited()),
		})
	}

	return out, nil
}

// Drivers asks driver-service (as a service) how many drivers are in each
// state.
type Drivers struct {
	driver driverv1.DriverServiceClient
}

var _ query.Drivers = (*Drivers)(nil)

func NewDrivers(conn grpc.ClientConnInterface) *Drivers {
	if conn == nil {
		panic("driver-service connection is required")
	}

	return &Drivers{driver: driverv1.NewDriverServiceClient(conn)}
}

func (d *Drivers) Supply(ctx context.Context) ([]domain.DriverSupply, error) {
	ctx, cancel := context.WithTimeout(ctx, peerCallTimeout)
	defer cancel()

	response, err := d.driver.GetDriverSupply(ctx, &driverv1.GetDriverSupplyRequest{})
	if err != nil {
		return nil, domain.ErrUpstreamUnavailable
	}

	out := make([]domain.DriverSupply, 0, len(response.GetCounts()))
	for _, c := range response.GetCounts() {
		out = append(out, domain.DriverSupply{
			Status: c.GetStatus(), Availability: c.GetAvailability(), VehicleClass: c.GetVehicleClass(), Drivers: c.GetDrivers(),
		})
	}

	return out, nil
}

func amount(text string) decimal.Decimal {
	value, err := decimal.NewFromString(text)
	if err != nil {
		return decimal.Zero
	}

	return value
}
