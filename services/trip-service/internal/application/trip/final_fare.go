package trip

import "context"

// FinalFare is what a trip cost as pricing-service recorded it when the trip ended,
// or the fee a cancelled trip owed. Amounts are decimal strings, as pricing gives them.
type FinalFare struct {
	// "trip", or the kind of fee (cancellation, no_show).
	Kind         string
	Total        string
	CurrencyCode string
}

// FinalFares reads the recorded totals of trips, by trip id; trips with no fare are
// left out. The trip lists show them; this service does not keep them.
type FinalFares interface {
	Totals(ctx context.Context, tripIDs []string) (map[string]FinalFare, error)
}
