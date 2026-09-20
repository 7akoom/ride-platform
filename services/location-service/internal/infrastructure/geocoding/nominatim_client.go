// Package geocoding talks to the self-hosted Nominatim place search.
package geocoding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/location-service/internal/application/maps"
)

// rankingBoxDegrees is the half-size of the box around the point a search is ranked
// towards, about 27 km: a city and its surroundings.
const rankingBoxDegrees = 0.25

const maxResponseBytes = 4 << 20

// addressParts are the parts of an address worth passing on to an app.
var addressParts = []string{
	"house_number", "road", "neighbourhood", "suburb", "city_district",
	"city", "town", "village", "county", "state", "postcode",
}

// NominatimClient looks places up by name and by position.
type NominatimClient struct {
	baseURL      string
	countryCodes string
	httpClient   *http.Client
}

// NewNominatimClient searches only within countryCodes (a comma-separated list such
// as "iq"), which keeps a search for "Erbil" from finding an Erbil elsewhere.
func NewNominatimClient(baseURL string, countryCodes string, timeout time.Duration) *NominatimClient {
	return &NominatimClient{
		baseURL:      strings.TrimRight(baseURL, "/"),
		countryCodes: countryCodes,
		httpClient:   &http.Client{Timeout: timeout},
	}
}

type nominatimPlace struct {
	OSMType     string         `json:"osm_type"`
	OSMID       int64          `json:"osm_id"`
	Latitude    string         `json:"lat"`
	Longitude   string         `json:"lon"`
	Category    string         `json:"category"`
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name"`
	Address     map[string]any `json:"address"`
	Error       string         `json:"error"`
}

// Search finds places by name, best match first. near ranks places close to it first.
func (c *NominatimClient) Search(
	ctx context.Context,
	query string,
	near *maps.Coordinates,
	limit int,
	languages string,
) ([]maps.Place, error) {
	values := url.Values{}
	values.Set("q", query)
	values.Set("format", "jsonv2")
	values.Set("addressdetails", "1")
	values.Set("dedupe", "1")
	values.Set("limit", strconv.Itoa(limit))
	values.Set("accept-language", languages)

	if c.countryCodes != "" {
		values.Set("countrycodes", c.countryCodes)
	}

	if near != nil {
		// west,north,east,south; bounded=0 ranks what is inside first without
		// excluding the rest.
		values.Set("viewbox", fmt.Sprintf("%.6f,%.6f,%.6f,%.6f",
			near.Longitude-rankingBoxDegrees, near.Latitude+rankingBoxDegrees,
			near.Longitude+rankingBoxDegrees, near.Latitude-rankingBoxDegrees))
		values.Set("bounded", "0")
	}

	body, err := c.get(ctx, "/search", values)
	if err != nil {
		return nil, err
	}

	var found []nominatimPlace

	if err := json.Unmarshal(body, &found); err != nil {
		return nil, fmt.Errorf("%w: Nominatim answered with something that is not a list of places: %v", maps.ErrUnavailable, err)
	}

	places := make([]maps.Place, 0, len(found))

	for _, item := range found {
		place, ok := toPlace(item)
		if ok {
			places = append(places, place)
		}
	}

	return places, nil
}

// Reverse names what is at a point.
func (c *NominatimClient) Reverse(ctx context.Context, at maps.Coordinates, languages string) (maps.Place, error) {
	values := url.Values{}
	values.Set("format", "jsonv2")
	values.Set("addressdetails", "1")
	values.Set("zoom", "18")
	values.Set("lat", strconv.FormatFloat(at.Latitude, 'f', 6, 64))
	values.Set("lon", strconv.FormatFloat(at.Longitude, 'f', 6, 64))
	values.Set("accept-language", languages)

	body, err := c.get(ctx, "/reverse", values)
	if err != nil {
		return maps.Place{}, err
	}

	var found nominatimPlace

	if err := json.Unmarshal(body, &found); err != nil {
		return maps.Place{}, fmt.Errorf("%w: Nominatim answered with something that is not a place: %v", maps.ErrUnavailable, err)
	}

	// Nominatim answers 200 with {"error": "Unable to geocode"} for a point where
	// there is nothing.
	if found.Error != "" {
		return maps.Place{}, maps.ErrPlaceNotFound
	}

	place, ok := toPlace(found)
	if !ok {
		return maps.Place{}, maps.ErrPlaceNotFound
	}

	return place, nil
}

func (c *NominatimClient) get(ctx context.Context, path string, values url.Values) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+values.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build Nominatim request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: call Nominatim: %v", maps.ErrUnavailable, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: Nominatim answered %d", maps.ErrUnavailable, response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("%w: read Nominatim response: %v", maps.ErrUnavailable, err)
	}

	return body, nil
}

// toPlace converts Nominatim's answer; a place without a usable position is dropped.
func toPlace(item nominatimPlace) (maps.Place, bool) {
	latitude, latErr := strconv.ParseFloat(item.Latitude, 64)
	longitude, lngErr := strconv.ParseFloat(item.Longitude, 64)

	if latErr != nil || lngErr != nil {
		return maps.Place{}, false
	}

	name := strings.TrimSpace(item.Name)
	if name == "" {
		// An address has no name of its own: its first part will do.
		name = strings.TrimSpace(strings.SplitN(item.DisplayName, ",", 2)[0])
	}

	address := map[string]string{}

	for _, part := range addressParts {
		if value, ok := item.Address[part].(string); ok && value != "" {
			address[part] = value
		}
	}

	id := ""
	if item.OSMType != "" {
		id = item.OSMType + "/" + strconv.FormatInt(item.OSMID, 10)
	}

	return maps.Place{
		ID:          id,
		Name:        name,
		DisplayName: item.DisplayName,
		Category:    item.Category,
		Type:        item.Type,
		Coordinates: maps.Coordinates{Latitude: latitude, Longitude: longitude},
		Address:     address,
	}, true
}
