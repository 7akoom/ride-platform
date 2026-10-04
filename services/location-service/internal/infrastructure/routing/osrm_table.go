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

	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
)

type osrmTableResponse struct {
	Code      string       `json:"code"`
	Durations [][]*float64 `json:"durations"`
	Distances [][]*float64 `json:"distances"`
	Sources   []struct {
		Distance float64 `json:"distance"` // meters from the point to the road it snapped to
	} `json:"sources"`
	Destinations []struct {
		Distance float64 `json:"distance"`
	} `json:"destinations"`
}

// TravelTimes asks OSRM's table service how long by road it takes from each origin
// to the destination, in one request.
//
// No snapping radius is sent: with one, a single origin far from a road (a driver's
// GPS in a field) makes OSRM refuse the whole table. Instead every point snaps to
// its nearest road and the snap distance OSRM reports is checked here: an origin
// more than snapRadiusMeters away is unreachable, and so is the destination for all.
func (c *OSRMClient) TravelTimes(ctx context.Context, origins []maps.Coordinates, destination maps.Coordinates) ([]maps.TravelTime, error) {
	if len(origins) == 0 {
		return nil, nil
	}

	pairs := make([]string, 0, len(origins)+1)
	sources := make([]string, 0, len(origins))

	for i, point := range origins {
		pairs = append(pairs, formatCoordinate(point.Longitude)+","+formatCoordinate(point.Latitude))
		sources = append(sources, strconv.Itoa(i))
	}

	pairs = append(pairs, formatCoordinate(destination.Longitude)+","+formatCoordinate(destination.Latitude))

	endpoint, err := url.Parse(c.baseURL + "/table/v1/driving/" + strings.Join(pairs, ";"))
	if err != nil {
		return nil, fmt.Errorf("build OSRM URL: %w", err)
	}

	endpoint.RawQuery = "sources=" + strings.Join(sources, ";") +
		"&destinations=" + strconv.Itoa(len(origins)) +
		"&annotations=duration,distance"

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build OSRM request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: call OSRM: %v", maps.ErrUnavailable, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: read OSRM response: %v", maps.ErrUnavailable, err)
	}

	var decoded osrmTableResponse

	if err := json.Unmarshal(body, &decoded); err != nil || decoded.Code == "" {
		return nil, fmt.Errorf("%w: OSRM answered %d with something that is not a table", maps.ErrUnavailable, response.StatusCode)
	}

	switch decoded.Code {
	case "Ok":
	case "NoSegment":
		return nil, maps.ErrNotNearRoad
	default:
		return nil, errors.New("OSRM refused the table request with code " + decoded.Code)
	}

	if len(decoded.Durations) != len(origins) || len(decoded.Sources) != len(origins) || len(decoded.Destinations) != 1 {
		return nil, fmt.Errorf("%w: OSRM answered a table of the wrong size", maps.ErrUnavailable)
	}

	if decoded.Destinations[0].Distance > snapRadiusMeters {
		return nil, maps.ErrNotNearRoad
	}

	times := make([]maps.TravelTime, len(origins))

	for i := range origins {
		if decoded.Sources[i].Distance > snapRadiusMeters || len(decoded.Durations[i]) != 1 || decoded.Durations[i][0] == nil {
			continue
		}

		times[i] = maps.TravelTime{Reachable: true, DurationSeconds: *decoded.Durations[i][0]}

		if i < len(decoded.Distances) && len(decoded.Distances[i]) == 1 && decoded.Distances[i][0] != nil {
			times[i].DistanceMeters = *decoded.Distances[i][0]
		}
	}

	return times, nil
}
