package grpc

import "context"

// riderOfTrip passes only the rider of the trip. The driver of the trip is
// deliberately not enough: the RPC exists so a rider can see their driver, and
// a driver already knows their own position.
func riderOfTrip(ctx context.Context, c caller, request any) (bool, error) {
	getter, ok := request.(tripIDGetter)
	if !ok {
		return false, nil
	}

	p, err := c.participation(ctx, getter.GetTripId())

	return p.rider, err
}
