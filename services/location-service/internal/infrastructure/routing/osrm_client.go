// Package routing talks to the self-hosted OSRM routing engine.
package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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

// headingRangeDegrees is how far from the driver's heading the first road may
// point: wide enough for a GPS heading that is a little off, narrow enough to
// keep the route from starting on the other side of a divided road.
const headingRangeDegrees = 60

type osrmResponse struct {
	Code   string `json:"code"`
	Routes []struct {
		Distance float64   `json:"distance"` // meters
		Duration float64   `json:"duration"` // seconds
		Geometry string    `json:"geometry"` // encoded polyline, 5 digits
		Legs     []osrmLeg `json:"legs"`
	} `json:"routes"`
}

type osrmLeg struct {
	Steps []struct {
		Name     string  `json:"name"`
		Ref      string  `json:"ref"`
		Distance float64 `json:"distance"`
		Duration float64 `json:"duration"`
		Maneuver struct {
			Type          string    `json:"type"`
			Modifier      string    `json:"modifier"`
			Exit          int       `json:"exit"`
			Location      []float64 `json:"location"` // longitude, latitude
			BearingBefore int       `json:"bearing_before"`
			BearingAfter  int       `json:"bearing_after"`
		} `json:"maneuver"`
	} `json:"steps"`
}

// Route returns the best route by road from one point to another, through the
// via points in order, with its full geometry and, when asked, the turn-by-turn
// steps.
//
// With a heading, the route starts on a road going the driver's way. When no road
// near the start goes that way (the GPS heading was wrong, say), the route is asked
// again without it rather than failing.
func (c *OSRMClient) Route(ctx context.Context, query maps.RouteQuery) (maps.Route, error) {
	route, err := c.route(ctx, query)

	if query.Heading != nil && (errors.Is(err, maps.ErrNoRoute) || errors.Is(err, maps.ErrNotNearRoad)) {
		query.Heading = nil

		return c.route(ctx, query)
	}

	return route, err
}

func (c *OSRMClient) route(ctx context.Context, query maps.RouteQuery) (maps.Route, error) {
	points := make([]maps.Coordinates, 0, len(query.Via)+2)
	points = append(points, query.From)
	points = append(points, query.Via...)
	points = append(points, query.To)

	// OSRM writes coordinates as longitude,latitude: the reverse of everywhere else
	// in this codebase.
	pairs := make([]string, 0, len(points))
	radiuses := make([]string, 0, len(points))

	for _, point := range points {
		pairs = append(pairs, formatCoordinate(point.Longitude)+","+formatCoordinate(point.Latitude))
		radiuses = append(radiuses, strconv.Itoa(snapRadiusMeters))
	}

	coordinates := strings.Join(pairs, ";")

	endpoint, err := url.Parse(c.baseURL + "/route/v1/driving/" + coordinates)
	if err != nil {
		return maps.Route{}, fmt.Errorf("build OSRM URL: %w", err)
	}

	// The query is written out rather than encoded: OSRM wants the ';' in radiuses
	// as it is, not as %3B.
	endpoint.RawQuery = "overview=full&geometries=polyline&steps=" + strconv.FormatBool(query.Steps) +
		"&alternatives=false&radiuses=" + strings.Join(radiuses, ";")

	if query.Heading != nil {
		// One entry per point; only the start is constrained.
		bearings := make([]string, len(points))
		bearings[0] = strconv.Itoa(int(math.Round(*query.Heading))%360) + "," + strconv.Itoa(headingRangeDegrees)
		endpoint.RawQuery += "&bearings=" + strings.Join(bearings, ";")
	}

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

		route := maps.Route{
			DistanceMeters:  best.Distance,
			DurationSeconds: best.Duration,
			Polyline:        best.Geometry,
		}

		if query.Steps {
			route.Steps = stepsOf(best.Legs)
		}

		return route, nil

	case "NoRoute":
		return maps.Route{}, maps.ErrNoRoute

	case "NoSegment":
		return maps.Route{}, maps.ErrNotNearRoad

	default:
		// InvalidQuery, TooBig...: not something the caller can fix by trying again.
		return maps.Route{}, errors.New("OSRM refused the request with code " + decoded.Code)
	}
}

// stepsOf flattens OSRM's legs into one list of steps, each marked with its leg.
func stepsOf(legs []osrmLeg) []maps.Step {
	steps := []maps.Step{}

	for leg, each := range legs {
		for _, step := range each.Steps {
			maneuver := maps.Maneuver{
				Type:          step.Maneuver.Type,
				Modifier:      step.Maneuver.Modifier,
				Exit:          step.Maneuver.Exit,
				BearingBefore: step.Maneuver.BearingBefore,
				BearingAfter:  step.Maneuver.BearingAfter,
			}

			if len(step.Maneuver.Location) == 2 {
				maneuver.Location = maps.Coordinates{
					Longitude: step.Maneuver.Location[0],
					Latitude:  step.Maneuver.Location[1],
				}
			}

			steps = append(steps, maps.Step{
				Maneuver:        maneuver,
				Name:            step.Name,
				Ref:             step.Ref,
				DistanceMeters:  step.Distance,
				DurationSeconds: step.Duration,
				Leg:             leg,
			})
		}
	}

	return steps
}

func formatCoordinate(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}
