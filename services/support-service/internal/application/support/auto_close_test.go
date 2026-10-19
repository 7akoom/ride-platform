package support

import (
	"testing"
	"time"
)

func TestAutoClosesAtOnlyForResolvedTickets(t *testing.T) {
	resolved := time.Date(2026, 10, 19, 10, 0, 0, 0, time.UTC)
	ticket := Ticket{Status: StatusResolved, StatusChangedAt: resolved}

	at := ticket.AutoClosesAt(24 * time.Hour)
	if at == nil || !at.Equal(resolved.Add(24*time.Hour)) {
		t.Fatalf("resolved: AutoClosesAt = %v", at)
	}

	if got := ticket.AutoClosesAt(0); got != nil {
		t.Fatalf("switched off: AutoClosesAt = %v", got)
	}

	for _, status := range []Status{StatusOpen, StatusInProgress, StatusWaitingUser, StatusClosed} {
		ticket.Status = status
		if got := ticket.AutoClosesAt(24 * time.Hour); got != nil {
			t.Fatalf("%s: AutoClosesAt = %v", status, got)
		}
	}
}
