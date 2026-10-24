package maps

// RouteQuery is what the routing engine is asked for one route.
type RouteQuery struct {
	From Coordinates
	To   Coordinates
	// Via are points to pass through on the way, in order.
	Via []Coordinates

	// Heading, when set, is the driver's direction at From (degrees from north).
	Heading *float64

	// Steps asks for the turn-by-turn instructions.
	Steps bool
}

// Step is one instruction along a route: the maneuver that starts it, then the
// stretch of road driven until the next one.
type Step struct {
	Maneuver Maneuver

	// Name is the road's name and Ref its number ("Road 2"), as OpenStreetMap
	// knows them; either may be empty (many streets have neither).
	Name string
	Ref  string

	// DistanceMeters and DurationSeconds are the stretch after the maneuver, up to
	// the next one.
	DistanceMeters  float64
	DurationSeconds float64

	// Leg is which part of the route the step is on: 0 up to the first via point,
	// then 1, and so on.
	Leg int
}

// Maneuver is what the driver does at a point.
type Maneuver struct {
	// Type is what happens, in OSRM's words: depart, turn, new name, continue,
	// merge, on ramp, off ramp, fork, end of road, roundabout, rotary,
	// roundabout turn, exit roundabout, exit rotary, notification, arrive.
	Type string

	// Modifier is which way: uturn, sharp right, right, slight right, straight,
	// slight left, left, sharp left; empty when it does not apply.
	Modifier string

	// Exit is which exit to take at a roundabout (1 is the first), or 0.
	Exit int

	// Location is where the maneuver happens.
	Location Coordinates

	// BearingBefore and BearingAfter are the direction of travel just before and
	// just after it, in degrees from north.
	BearingBefore int
	BearingAfter  int
}
