package events

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Where an alert's location came from. The operator needs to know, because
// a trip's pickup point is where the trip STARTED, not necessarily where
// the person in trouble is now.
const (
	LocationSourceSOSPress = "sos_press"
	LocationSourcePickup   = "trip_pickup"
)

// SOSAlert is everything an operator is told when somebody presses SOS.
type SOSAlert struct {
	AlertID     string
	TripID      string
	TriggeredBy string // "rider" or "driver"
	DriverName  string // empty when the trip has no driver or the lookup failed

	HasLocation    bool
	Latitude       float64
	Longitude      float64
	LocationSource string

	TriggeredAt time.Time
}

// OperatorAlerter delivers an SOS alert to a human at the operating
// company, out of band from the app (SMS, a webhook into a chat, ...).
// Implementations return nil once the alert reached at least one person.
type OperatorAlerter interface {
	Alert(ctx context.Context, alert SOSAlert) error
}

// tripSOSTriggeredPayload mirrors the outbox payload trip-service writes
// for trip.sos_triggered. Every value crosses as a string (coordinates
// included), and the payload carries no rider_id/driver_id, so the
// recipient is resolved through GetTrip using triggered_by.
type tripSOSTriggeredPayload struct {
	TripID      string `json:"trip_id"`
	AlertID     string `json:"alert_id"`
	TriggeredBy string `json:"triggered_by"`
	Latitude    string `json:"latitude"`
	Longitude   string `json:"longitude"`
}

// buildSOSAlert prefers the exact place SOS was pressed and falls back to
// the trip's pickup point when that is missing or unusable.
func buildSOSAlert(
	payload tripSOSTriggeredPayload,
	trip TripInfo,
	driverName string,
	occurredAt time.Time,
) SOSAlert {
	alert := SOSAlert{
		AlertID:     strings.TrimSpace(payload.AlertID),
		TripID:      strings.TrimSpace(payload.TripID),
		TriggeredBy: strings.ToLower(strings.TrimSpace(payload.TriggeredBy)),
		DriverName:  strings.TrimSpace(driverName),
		TriggeredAt: occurredAt,
	}

	if lat, lng, ok := parseCoordinates(payload.Latitude, payload.Longitude); ok {
		alert.HasLocation = true
		alert.Latitude = lat
		alert.Longitude = lng
		alert.LocationSource = LocationSourceSOSPress

		return alert
	}

	if validCoordinates(trip.PickupLatitude, trip.PickupLongitude) {
		alert.HasLocation = true
		alert.Latitude = trip.PickupLatitude
		alert.Longitude = trip.PickupLongitude
		alert.LocationSource = LocationSourcePickup
	}

	return alert
}

func parseCoordinates(latitude, longitude string) (float64, float64, bool) {
	lat, err := strconv.ParseFloat(strings.TrimSpace(latitude), 64)
	if err != nil {
		return 0, 0, false
	}

	lng, err := strconv.ParseFloat(strings.TrimSpace(longitude), 64)
	if err != nil {
		return 0, 0, false
	}

	if !validCoordinates(lat, lng) {
		return 0, 0, false
	}

	return lat, lng, true
}

// validCoordinates rejects out-of-range values and exactly (0, 0): a
// missing coordinate crosses the wire as zero, and "somewhere in the Gulf
// of Guinea" is worse than no location at all.
func validCoordinates(lat, lng float64) bool {
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return false
	}

	return lat != 0 || lng != 0
}

// MapURL is an OpenStreetMap link to the alert's location, or "" when it
// has none. (This platform does not use Google Maps.)
func (a SOSAlert) MapURL() string {
	if !a.HasLocation {
		return ""
	}

	lat := strconv.FormatFloat(a.Latitude, 'f', 6, 64)
	lng := strconv.FormatFloat(a.Longitude, 'f', 6, 64)

	return fmt.Sprintf("https://www.openstreetmap.org/?mlat=%s&mlon=%s#map=17/%s/%s", lat, lng, lat, lng)
}

// Text is the human-readable alert, sized for an SMS.
func (a SOSAlert) Text() string {
	who := a.TriggeredBy
	if who == "" {
		who = "user"
	}

	var text strings.Builder

	fmt.Fprintf(&text, "SOS ALERT: the %s pressed SOS. Trip %s.", who, a.TripID)

	if a.DriverName != "" {
		fmt.Fprintf(&text, " Driver: %s.", a.DriverName)
	}

	if url := a.MapURL(); url != "" {
		label := "where SOS was pressed"
		if a.LocationSource == LocationSourcePickup {
			label = "trip pickup, press location unknown"
		}

		fmt.Fprintf(&text, " Location (%s): %s", label, url)
	} else {
		text.WriteString(" No location available.")
	}

	shortID := a.AlertID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}

	if !a.TriggeredAt.IsZero() {
		fmt.Fprintf(&text, " Alert %s at %s UTC.", shortID, a.TriggeredAt.UTC().Format("2006-01-02 15:04"))
	} else {
		fmt.Fprintf(&text, " Alert %s.", shortID)
	}

	return text.String()
}
