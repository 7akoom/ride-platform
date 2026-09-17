package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/pricing"
	"github.com/shopspring/decimal"
)

// Open-Meteo is free for non-commercial use with no API key and no
// account. See this service's README for the note on why this is the
// one external dependency in the project and what to check before
// commercial deployment.
const openMeteoBaseURL = "https://api.open-meteo.com/v1/forecast"

// Weather barely changes minute to minute, and every fare calculation
// would otherwise trigger an HTTP call. Caching per coarse location
// keeps this to a trickle of requests regardless of trip volume.
const cacheTTL = 10 * time.Minute

// Coordinates are rounded to ~11km before being used as a cache key so
// nearby pickups share one entry instead of each creating their own.
const cacheGridDecimals = 1

type cacheEntry struct {
	conditions pricing.WeatherConditions
	expiresAt  time.Time
}

type Client struct {
	httpClient *http.Client

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

func NewClient(timeout time.Duration) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		cache:      make(map[string]cacheEntry),
	}
}

type openMeteoResponse struct {
	Current struct {
		WeatherCode  int     `json:"weather_code"`
		WindSpeed10m float64 `json:"wind_speed_10m"`
	} `json:"current"`
}

func (c *Client) GetConditions(
	ctx context.Context,
	latitude, longitude float64,
) (pricing.WeatherConditions, error) {
	key := cacheKey(latitude, longitude)

	if cached, ok := c.lookup(key); ok {
		return cached, nil
	}

	endpoint, err := url.Parse(openMeteoBaseURL)
	if err != nil {
		return pricing.WeatherConditions{}, fmt.Errorf("parse Open-Meteo URL: %w", err)
	}

	query := endpoint.Query()
	query.Set("latitude", strconv.FormatFloat(latitude, 'f', 4, 64))
	query.Set("longitude", strconv.FormatFloat(longitude, 'f', 4, 64))
	query.Set("current", "weather_code,wind_speed_10m")
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return pricing.WeatherConditions{}, fmt.Errorf("build Open-Meteo request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return pricing.WeatherConditions{}, fmt.Errorf("call Open-Meteo: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return pricing.WeatherConditions{}, fmt.Errorf(
			"Open-Meteo returned status %d",
			response.StatusCode,
		)
	}

	var decoded openMeteoResponse

	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return pricing.WeatherConditions{}, fmt.Errorf("decode Open-Meteo response: %w", err)
	}

	conditions := pricing.WeatherConditions{
		SurgePercent: decimal.NewFromFloat(surgePercentFor(
			decoded.Current.WeatherCode,
			decoded.Current.WindSpeed10m,
		)),
	}

	c.store(key, conditions)

	return conditions, nil
}

func (c *Client) lookup(key string) (pricing.WeatherConditions, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return pricing.WeatherConditions{}, false
	}

	return entry.conditions, true
}

func (c *Client) store(key string, conditions pricing.WeatherConditions) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache[key] = cacheEntry{
		conditions: conditions,
		expiresAt:  time.Now().Add(cacheTTL),
	}
}

func cacheKey(latitude, longitude float64) string {
	factor := math.Pow(10, cacheGridDecimals)
	roundedLat := math.Round(latitude*factor) / factor
	roundedLng := math.Round(longitude*factor) / factor

	return strconv.FormatFloat(roundedLat, 'f', cacheGridDecimals, 64) +
		"," +
		strconv.FormatFloat(roundedLng, 'f', cacheGridDecimals, 64)
}

// surgePercentFor maps WMO weather codes (the standard Open-Meteo
// returns) to surge tiers. The principle: worse conditions mean fewer
// drivers willing to work and higher rider demand, so the tiers escalate
// with severity rather than with any particular measurement.
func surgePercentFor(weatherCode int, windSpeedKmh float64) float64 {
	percent := 0.0

	switch {
	case weatherCode >= 95: // Thunderstorm
		percent = 40
	case weatherCode >= 80 && weatherCode <= 86: // Rain/snow showers
		percent = 30
	case weatherCode >= 71 && weatherCode <= 77: // Snow
		percent = 35
	case weatherCode >= 61 && weatherCode <= 67: // Rain
		percent = 25
	case weatherCode >= 51 && weatherCode <= 57: // Drizzle
		percent = 10
	case weatherCode >= 45 && weatherCode <= 48: // Fog
		percent = 15
	}

	// High wind compounds whatever else is happening, but only counts
	// once regardless of how far above the threshold it is.
	if windSpeedKmh >= 50 {
		percent += 15
	}

	return percent
}
