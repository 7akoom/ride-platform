package clients

import (
	"context"
	"fmt"
	"sync"
	"time"

	pricingv1 "github.com/7akoom/ride-platform/gen/go/ride/pricing/v1"
	"github.com/7akoom/ride-platform/services/dispatch-service/internal/application/dispatch"
	"google.golang.org/grpc"
)

// dispatchSettingsTTL is how long the settings read from pricing-service are used
// before they are read again: a change by staff reaches dispatch within it.
const dispatchSettingsTTL = 30 * time.Second

// DispatchSettingsClient reads the dispatch settings staff set (pricing-service),
// kept for dispatchSettingsTTL. When pricing-service does not answer, the last
// settings read are used; with none, the error is returned.
type DispatchSettingsClient struct {
	client pricingv1.PricingServiceClient
	now    func() time.Time

	mu      sync.Mutex
	cached  dispatch.NextTripSettings
	readAt  time.Time
	hasRead bool
}

func NewDispatchSettingsClient(conn *grpc.ClientConn) *DispatchSettingsClient {
	if conn == nil {
		panic("pricing-service connection is required")
	}

	return &DispatchSettingsClient{client: pricingv1.NewPricingServiceClient(conn), now: time.Now}
}

// NextTripSettings: see dispatch.NextTripSettingsSource.
func (c *DispatchSettingsClient) NextTripSettings(ctx context.Context) (dispatch.NextTripSettings, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.hasRead && c.now().Sub(c.readAt) < dispatchSettingsTTL {
		return c.cached, nil
	}

	response, err := c.client.GetDispatchSettings(ctx, &pricingv1.GetDispatchSettingsRequest{})
	if err != nil {
		if c.hasRead {
			return c.cached, nil
		}

		return dispatch.NextTripSettings{}, fmt.Errorf("call pricing-service GetDispatchSettings: %w", err)
	}

	c.cached = dispatch.NextTripSettings{
		Enabled:      response.GetSettings().GetNextTripsEnabled(),
		RadiusMeters: float64(response.GetSettings().GetNextTripRadiusMeters()),
	}
	c.readAt = c.now()
	c.hasRead = true

	return c.cached, nil
}
