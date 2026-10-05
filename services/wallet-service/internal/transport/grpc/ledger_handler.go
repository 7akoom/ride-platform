package grpc

import (
	"context"
	"time"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

// maxLedgerSummaryRange bounds one SummarizeLedger call (a report covers at
// most 366 days; a day either side for time zones).
const maxLedgerSummaryRange = 368 * 24 * time.Hour

// LedgerSummarizer totals the ledger for the business reports.
type LedgerSummarizer interface {
	Summarize(ctx context.Context, start, end time.Time, timeZone string) (wallet.LedgerSummary, error)
}

// WithLedger answers SummarizeLedger (analytics).
func (h *WalletHandler) WithLedger(summarizer LedgerSummarizer) *WalletHandler {
	h.ledger = summarizer

	return h
}

func (h *WalletHandler) SummarizeLedger(
	ctx context.Context,
	request *walletv1.SummarizeLedgerRequest,
) (*walletv1.SummarizeLedgerResponse, error) {
	if h.ledger == nil {
		return nil, status.Error(codes.Unimplemented, "the ledger summary is not available")
	}

	if request.GetStart() == nil || request.GetEnd() == nil {
		return nil, status.Error(codes.InvalidArgument, "start and end are required")
	}

	start, end := request.GetStart().AsTime(), request.GetEnd().AsTime()
	if !end.After(start) || end.Sub(start) > maxLedgerSummaryRange {
		return nil, status.Error(codes.InvalidArgument, "end must be after start, at most 368 days later")
	}

	if _, err := time.LoadLocation(request.GetTimeZone()); err != nil || request.GetTimeZone() == "" {
		return nil, status.Error(codes.InvalidArgument, "time_zone must be an IANA time zone")
	}

	summary, err := h.ledger.Summarize(ctx, start, end, request.GetTimeZone())
	if err != nil {
		h.logger.Error("summarize ledger failed", "error", err)

		return nil, status.Error(codes.Internal, "failed to summarize the ledger")
	}

	response := &walletv1.SummarizeLedgerResponse{
		CurrencyCode:     summary.CurrencyCode,
		RiderBalances:    summary.RiderBalances.String(),
		DriverCredit:     summary.DriverCredit.String(),
		DriverDebt:       summary.DriverDebt.String(),
		SuspendedDrivers: summary.SuspendedDrivers,
		RiderDues:        summary.RiderDues.String(),
	}

	for _, t := range summary.Totals {
		response.Totals = append(response.Totals, &walletv1.LedgerTotal{
			Date:      t.Date.Format(time.DateOnly),
			OwnerType: t.OwnerType,
			Type:      t.Type,
			Entries:   t.Entries,
			Credited:  t.Credited.String(),
			Debited:   t.Debited.String(),
		})
	}

	return response, nil
}
