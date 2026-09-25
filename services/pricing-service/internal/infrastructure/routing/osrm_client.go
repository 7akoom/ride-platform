package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
)

// OSRM is self-hosted and open-source (BSD), so there are no API keys,
// no usage limits, and no per-request cost — it just needs the map
// extract for the deployment's region preprocessed once. See this
// service's README and infrastructure/osrm/README.md for setup.
type OSRMClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewOSRMClient(baseURL string, timeout time.Duration) *OSRMClient {
	return &OSRMClient{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: timeout},
	}
}

type osrmRouteResponse struct {
	Code   string `json:"code"`
	Routes []struct {
		Distance float64 `json:"distance"` // meters
		Duration float64 `json:"duration"` // seconds
	} `json:"routes"`
}

func (c *OSRMClient) Route(
	ctx context.Context,
	pickupLat, pickupLng, dropoffLat, dropoffLng float64,
	via ...pricing.Point,
) (pricing.Route, error) {
	// OSRM's coordinate order is lng,lat — the reverse of how they're
	// written everywhere else in this codebase. Easy to get backwards.
	// The route goes through the stops in order.
	coordinates := formatCoordinate(pickupLng) + "," + formatCoordinate(pickupLat)
	for _, stop := range via {
		coordinates += ";" + formatCoordinate(stop.Longitude) + "," + formatCoordinate(stop.Latitude)
	}

	coordinates += ";" + formatCoordinate(dropoffLng) + "," + formatCoordinate(dropoffLat)

	endpoint, err := url.Parse(c.baseURL + "/route/v1/driving/" + coordinates)
	if err != nil {
		return pricing.Route{}, fmt.Errorf("build OSRM URL: %w", err)
	}

	query := endpoint.Query()
	// The fare only needs totals, not the geometry or turn-by-turn
	// steps — skipping them keeps responses small.
	query.Set("overview", "false")
	query.Set("steps", "false")
	query.Set("alternatives", "false")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return pricing.Route{}, fmt.Errorf("build OSRM request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return pricing.Route{}, fmt.Errorf("call OSRM: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return pricing.Route{}, fmt.Errorf("OSRM returned status %d", response.StatusCode)
	}

	var decoded osrmRouteResponse

	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return pricing.Route{}, fmt.Errorf("decode OSRM response: %w", err)
	}

	// OSRM reports its own status in the body. "NoRoute" is a genuine
	// answer (e.g. pickup and dropoff separated by water with no
	// crossing), not a transport failure — but either way the caller
	// falls back to an estimate rather than failing the fare.
	if decoded.Code != "Ok" || len(decoded.Routes) == 0 {
		return pricing.Route{}, fmt.Errorf("OSRM returned code %q with %d routes", decoded.Code, len(decoded.Routes))
	}

	best := decoded.Routes[0]

	return pricing.Route{
		DistanceKm:      best.Distance / 1000,
		DurationMinutes: best.Duration / 60,
		Estimated:       false,
	}, nil
}

func formatCoordinate(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}
