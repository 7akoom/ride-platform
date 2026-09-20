// Package routing talks to the self-hosted OSRM routing engine.
package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
)

// snapRadiusMeters is how far from a road a point may be and still start or end a
// route. Without it OSRM snaps a point in the desert (or the sea) to the nearest
// road however far away and answers with a route from somewhere else entirely.
const snapRadiusMeters = 1000

// maxResponseBytes bounds what is read from OSRM: a long route's full-resolution
// polyline is some tens of kilobytes.
const maxResponseBytes = 4 << 20

// OSRMClient asks OSRM for the best route by car.
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

type osrmResponse struct {
	Code   string `json:"code"`
	Routes []struct {
		Distance float64 `json:"distance"` // meters
		Duration float64 `json:"duration"` // seconds
		Geometry string  `json:"geometry"` // encoded polyline, 5 digits
	} `json:"routes"`
}

// Route returns the best route by road from one point to another, with its full
// geometry.
func (c *OSRMClient) Route(ctx context.Context, from maps.Coordinates, to maps.Coordinates) (maps.Route, error) {
	// OSRM writes coordinates as longitude,latitude: the reverse of everywhere else
	// in this codebase.
	coordinates := formatCoordinate(from.Longitude) + "," + formatCoordinate(from.Latitude) +
		";" + formatCoordinate(to.Longitude) + "," + formatCoordinate(to.Latitude)

	endpoint, err := url.Parse(c.baseURL + "/route/v1/driving/" + coordinates)
	if err != nil {
		return maps.Route{}, fmt.Errorf("build OSRM URL: %w", err)
	}

	// The query is written out rather than encoded: OSRM wants the ';' in radiuses
	// as it is, not as %3B.
	endpoint.RawQuery = "overview=full&geometries=polyline&steps=false&alternatives=false" +
		"&radiuses=" + strconv.Itoa(snapRadiusMeters) + ";" + strconv.Itoa(snapRadiusMeters)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return maps.Route{}, fmt.Errorf("build OSRM request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return maps.Route{}, fmt.Errorf("%w: call OSRM: %v", maps.ErrUnavailable, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return maps.Route{}, fmt.Errorf("%w: read OSRM response: %v", maps.ErrUnavailable, err)
	}

	// OSRM reports its own verdict in the body, and answers 400 for a point it
	// cannot place on a road, so the body is read whatever the status.
	var decoded osrmResponse

	if err := json.Unmarshal(body, &decoded); err != nil || decoded.Code == "" {
		return maps.Route{}, fmt.Errorf("%w: OSRM answered %d with something that is not a route", maps.ErrUnavailable, response.StatusCode)
	}

	switch decoded.Code {
	case "Ok":
		if len(decoded.Routes) == 0 {
			return maps.Route{}, fmt.Errorf("%w: OSRM answered Ok without a route", maps.ErrUnavailable)
		}

		best := decoded.Routes[0]

		return maps.Route{
			DistanceMeters:  best.Distance,
			DurationSeconds: best.Duration,
			Polyline:        best.Geometry,
		}, nil

	case "NoRoute":
		return maps.Route{}, maps.ErrNoRoute

	case "NoSegment":
		return maps.Route{}, maps.ErrNotNearRoad

	default:
		// InvalidQuery, TooBig...: not something the caller can fix by trying again.
		return maps.Route{}, errors.New("OSRM refused the request with code " + decoded.Code)
	}
}

func formatCoordinate(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}
