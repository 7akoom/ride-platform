package notification

import (
	"context"
	"testing"
)

// translations answers FindTranslation from a map; nothing else is called.
type translations struct {
	Repository
	found map[string]TemplateTranslation
	asked int
}

func (t *translations) FindTranslation(_ context.Context, eventKey string, locales []string) (TemplateTranslation, error) {
	t.asked++

	for _, locale := range locales {
		if found, ok := t.found[eventKey+"/"+locale]; ok {
			return found, nil
		}
	}

	return TemplateTranslation{}, ErrTranslationNotFound
}

func TestInLocaleWritesNotificationsAgainInTheReadersLanguage(t *testing.T) {
	repository := &translations{found: map[string]TemplateTranslation{
		"support.reply_received/ar": {Locale: "ar", Title: "رد على تذكرتك", Body: "التذكرة {ticket}"},
	}}
	s := &service{repository: repository}

	got := s.inLocale(context.Background(), []Notification{
		{EventKey: "support.reply_received", Locale: "en", Title: "Reply", Body: "Ticket S-1", Variables: map[string]string{"ticket": "S-1"}},
		{EventKey: "support.reply_received", Locale: "en", Title: "Reply", Body: "Ticket S-2", Variables: map[string]string{"ticket": "S-2"}},
		// Stored before the values were kept: left as it is.
		{EventKey: "support.reply_received", Locale: "en", Title: "Old", Body: "Old body"},
		// No Arabic for this event: left in English.
		{EventKey: "trip.started", Locale: "en", Title: "Started", Body: "On the way", Variables: map[string]string{}},
	}, "ar")

	if got[0].Title != "رد على تذكرتك" || got[0].Body != "التذكرة S-1" || got[0].Locale != "ar" {
		t.Fatalf("first: %+v", got[0])
	}

	if got[1].Body != "التذكرة S-2" {
		t.Fatalf("second: %+v", got[1])
	}

	if got[2].Title != "Old" || got[3].Title != "Started" {
		t.Fatalf("kept: %+v %+v", got[2], got[3])
	}

	if repository.asked != 2 {
		t.Fatalf("one lookup per event key, got %d", repository.asked)
	}

	same := s.inLocale(context.Background(), []Notification{{Title: "x", Variables: map[string]string{}}}, "")
	if same[0].Title != "x" {
		t.Fatalf("no locale: %+v", same[0])
	}
}
