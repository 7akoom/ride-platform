package notification

// pushOnlyEventKeys are notifications that only matter the moment they arrive: they go
// out as a push like any other, but the inbox never lists or counts them. A trip offer
// lasts seconds and the app shows it on its own screen, so a row for it would be a
// pile of dead "new trip request" items with an unread badge nobody can clear by
// answering the offer.
var pushOnlyEventKeys = []string{"trip.offer_received"}

// PushOnlyEventKeys returns the event keys the inbox leaves out (a copy).
func PushOnlyEventKeys() []string {
	return append([]string(nil), pushOnlyEventKeys...)
}
