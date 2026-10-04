package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/7akoom/ride-platform/services/support-service/internal/application/support"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	url := os.Getenv("SUPPORT_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SUPPORT_TEST_DATABASE_URL is not set")
	}

	files, err := filepath.Glob("../../../../migrations/*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations: %v", err)
	}

	sort.Strings(files)

	psql := func(sql string) {
		cmd := exec.Command("psql", url, "-v", "ON_ERROR_STOP=1", "-q")
		cmd.Stdin = strings.NewReader(sql)

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("psql: %v\n%s", err, out)
		}
	}

	dropAll := func() {
		psql(`DROP TABLE IF EXISTS support_help_votes, support_help_articles, support_help_sections, support_macros,
			support_actions, support_attachments, support_messages, support_tickets,
			support_categories, outbox_events CASCADE; DROP SEQUENCE IF EXISTS support_ticket_number_seq;`)
	}

	dropAll()

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}

		up, _, _ := strings.Cut(string(raw), "-- +goose Down")
		psql(up)
	}

	t.Cleanup(dropAll)

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(pool.Close)

	return pool
}

// --- fakes ----------------------------------------------------------------

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = c.now.Add(d)
}

type ids struct{}

func (ids) NewID() string { return uuid.NewString() }

type fakeTrips map[string]support.TripInfo

func (f fakeTrips) GetTrip(_ context.Context, id string) (support.TripInfo, error) {
	trip, ok := f[id]
	if !ok {
		return support.TripInfo{}, support.ErrNotFound
	}

	return trip, nil
}

type fakeProfiles struct {
	riders  map[string]string // identity -> rider id
	drivers map[string]string // identity -> driver id
	offline []string
}

func (f *fakeProfiles) ProfileIDByIdentity(_ context.Context, a support.Audience, identity string) (string, error) {
	m := f.riders
	if a == support.AudienceDriver {
		m = f.drivers
	}

	if id, ok := m[identity]; ok {
		return id, nil
	}

	return "", support.ErrNoProfile
}

func (f *fakeProfiles) IdentityByProfile(_ context.Context, a support.Audience, profile string) (string, error) {
	m := f.riders
	if a == support.AudienceDriver {
		m = f.drivers
	}

	for identity, id := range m {
		if id == profile {
			return identity, nil
		}
	}

	return "", support.ErrActionNotAllowed
}

func (f *fakeProfiles) SetDriverOffline(_ context.Context, id string) error {
	f.offline = append(f.offline, id)

	return nil
}

type fakeMedia struct {
	owners   map[string]string
	released []string
}

func (f *fakeMedia) Hold(_ context.Context, id, owner string) error {
	if f.owners[id] != owner {
		return support.ErrMediaNotUsable
	}

	return nil
}

func (f *fakeMedia) Release(_ context.Context, id string) error {
	f.released = append(f.released, id)

	return nil
}

func (f *fakeMedia) DownloadURL(_ context.Context, id string) (string, time.Time, error) {
	return "https://files.test/" + id, time.Now().Add(time.Minute), nil
}

type walletCall struct {
	kind, acting, target, amount, driverAmount, key string
}

type fakeWallet struct {
	calls      []walletCall
	refundable decimal.Decimal
	err        error
}

func (f *fakeWallet) RefundTrip(_ context.Context, acting, trip string, amount, driverAmount decimal.Decimal, _, key string) error {
	if f.err != nil {
		return f.err
	}

	f.calls = append(f.calls, walletCall{"refund", acting, trip, amount.StringFixed(2), driverAmount.StringFixed(2), key})

	return nil
}

func (f *fakeWallet) Credit(_ context.Context, acting string, _ support.Audience, owner string, amount decimal.Decimal, _, key string) error {
	if f.err != nil {
		return f.err
	}

	f.calls = append(f.calls, walletCall{"credit", acting, owner, amount.StringFixed(2), "", key})

	return nil
}

func (f *fakeWallet) Refundable(context.Context, string) (decimal.Decimal, error) {
	return f.refundable, nil
}

type fakeAccounts struct {
	suspended map[string]bool
	err       error
}

func (f *fakeAccounts) Suspend(_ context.Context, identity string) error {
	if f.err != nil {
		return f.err
	}

	f.suspended[identity] = true

	return nil
}

func (f *fakeAccounts) Reactivate(_ context.Context, identity string) error {
	if f.err != nil {
		return f.err
	}

	delete(f.suspended, identity)

	return nil
}

type fakeStaff map[string]map[string]bool // identity -> permissions

func (f fakeStaff) Check(_ context.Context, identity, permission, _, _ string) (bool, error) {
	return f[identity][permission], nil
}

type world struct {
	pool     *pgxpool.Pool
	service  *support.Service
	clock    *fakeClock
	trips    fakeTrips
	profiles *fakeProfiles
	media    *fakeMedia
	wallet   *fakeWallet
	accounts *fakeAccounts
	staff    fakeStaff
}

func newWorld(t *testing.T) *world {
	w := &world{
		pool:     testPool(t),
		clock:    &fakeClock{now: time.Now().UTC().Truncate(time.Microsecond)},
		trips:    fakeTrips{},
		profiles: &fakeProfiles{riders: map[string]string{}, drivers: map[string]string{}},
		media:    &fakeMedia{owners: map[string]string{}},
		wallet:   &fakeWallet{},
		accounts: &fakeAccounts{suspended: map[string]bool{}},
		staff:    fakeStaff{},
	}

	w.service = support.NewService(support.Dependencies{
		Repository: NewSupportRepository(w.pool),
		Trips:      w.trips,
		Profiles:   w.profiles,
		Media:      w.media,
		Wallet:     w.wallet,
		Accounts:   w.accounts,
		Staff:      w.staff,
		IDs:        ids{},
		Clock:      w.clock,
	}, support.Config{
		RefundLimit:      decimal.NewFromInt(10000),
		MaxOpenTickets:   3,
		TripMaxAge:       30 * 24 * time.Hour,
		FirstResponse:    map[support.Priority]time.Duration{support.PriorityNormal: 2 * time.Hour},
		AutoResolveAfter: 72 * time.Hour,
		AutoCloseAfter:   7 * 24 * time.Hour,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	return w
}

type person struct {
	identity string
	profile  string
}

func (w *world) rider() person {
	p := person{uuid.NewString(), uuid.NewString()}
	w.profiles.riders[p.identity] = p.profile

	return p
}

func (w *world) driver() person {
	p := person{uuid.NewString(), uuid.NewString()}
	w.profiles.drivers[p.identity] = p.profile

	return p
}

func (w *world) staffMember(permissions ...string) support.Staff {
	s := support.Staff{StaffID: uuid.NewString(), IdentityID: uuid.NewString()}
	w.staff[s.IdentityID] = map[string]bool{}

	for _, p := range permissions {
		w.staff[s.IdentityID][p] = true
	}

	return s
}

func (w *world) trip(rider, driver person, status string, age time.Duration) string {
	id := uuid.NewString()
	w.trips[id] = support.TripInfo{ID: id, RiderID: rider.profile, DriverID: driver.profile, Status: status, RequestedAt: w.clock.Now().Add(-age)}

	return id
}

func (w *world) events(t *testing.T, ticketID string) []string {
	t.Helper()

	rows, err := w.pool.Query(context.Background(),
		`SELECT event_type, payload FROM outbox_events WHERE aggregate_id = $1 ORDER BY occurred_at, id`, ticketID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var out []string

	for rows.Next() {
		var eventType string
		var payload []byte

		if err := rows.Scan(&eventType, &payload); err != nil {
			t.Fatal(err)
		}

		var p map[string]any
		_ = json.Unmarshal(payload, &p)

		if recipient, ok := p["recipient_type"]; ok {
			eventType += fmt.Sprintf(":%v:%v", recipient, p["recipient_id"])
		}

		out = append(out, eventType)
	}

	return out
}

func must[T any](value T, err error) func(*testing.T) T {
	return func(t *testing.T) T {
		t.Helper()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		return value
	}
}

func wantErr(t *testing.T, err, target error) {
	t.Helper()

	if !errors.Is(err, target) {
		t.Fatalf("expected %v, got %v", target, err)
	}
}

const method = "/ride.support.v1.SupportService/Test"

// --- tests ----------------------------------------------------------------

func TestTicketsAndConversation(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	rider, driver := w.rider(), w.driver()
	trip := w.trip(rider, driver, "completed", time.Hour)
	caller := support.Caller{IdentityID: rider.identity}

	// Categories: riders do not see driver ones nor the reserved sos.
	categories := must(w.service.ListCategories(ctx, support.AudienceRider))(t)
	for _, c := range categories {
		if c.Key == "sos" || c.Key == "earnings_payout" {
			t.Fatalf("rider sees %s", c.Key)
		}
	}

	_, err := w.service.CreateTicket(ctx, caller, support.CreateTicketInput{Audience: support.AudienceRider, CategoryKey: "trip_fare", Body: "hi"})
	wantErr(t, err, support.ErrTripRequired)

	_, err = w.service.CreateTicket(ctx, caller, support.CreateTicketInput{Audience: support.AudienceRider, CategoryKey: "sos", Body: "hi"})
	wantErr(t, err, support.ErrInvalidInput)

	_, err = w.service.CreateTicket(ctx, support.Caller{IdentityID: uuid.NewString()},
		support.CreateTicketInput{Audience: support.AudienceRider, CategoryKey: "other", Body: "hi"})
	wantErr(t, err, support.ErrNoProfile)

	stranger := w.rider()
	_, err = w.service.CreateTicket(ctx, support.Caller{IdentityID: stranger.identity},
		support.CreateTicketInput{Audience: support.AudienceRider, CategoryKey: "trip_fare", Body: "hi", TripID: trip})
	wantErr(t, err, support.ErrTripNotYours)

	old := w.trip(rider, driver, "completed", 40*24*time.Hour)
	_, err = w.service.CreateTicket(ctx, caller, support.CreateTicketInput{Audience: support.AudienceRider, CategoryKey: "trip_fare", Body: "hi", TripID: old})
	wantErr(t, err, support.ErrTripTooOld)

	attachment := uuid.NewString()
	w.media.owners[attachment] = rider.identity

	created := must(w.service.CreateTicket(ctx, caller, support.CreateTicketInput{
		Audience: support.AudienceRider, CategoryKey: "trip_fare", Body: "  I was charged twice  ",
		TripID: trip, AttachmentIDs: []string{attachment},
	}))(t)

	ticket := created.Ticket
	if ticket.Status != support.StatusOpen || ticket.CounterpartProfileID != driver.profile || ticket.Number == 0 ||
		created.Messages[0].Body != "I was charged twice" {
		t.Fatalf("created: %+v %+v", ticket, created.Messages)
	}

	// Somebody else's attachment is refused, and nothing is created.
	foreign := uuid.NewString()
	w.media.owners[foreign] = stranger.identity
	_, err = w.service.CreateTicket(ctx, caller, support.CreateTicketInput{
		Audience: support.AudienceRider, CategoryKey: "other", Body: "x", AttachmentIDs: []string{foreign},
	})
	wantErr(t, err, support.ErrMediaNotUsable)

	// The same attachment twice: the second ticket fails and lets it go.
	_, err = w.service.CreateTicket(ctx, caller, support.CreateTicketInput{
		Audience: support.AudienceRider, CategoryKey: "other", Body: "x", AttachmentIDs: []string{attachment},
	})
	wantErr(t, err, support.ErrAttachmentInUse)

	// The same category about the same trip: the open one, with the message.
	again := must(w.service.CreateTicket(ctx, caller, support.CreateTicketInput{
		Audience: support.AudienceRider, CategoryKey: "trip_fare", Body: "still waiting", TripID: trip,
	}))(t)
	if !again.Existing || again.Ticket.ID != ticket.ID || len(again.Messages) != 2 {
		t.Fatalf("duplicate: %+v", again)
	}

	// Staff: the queue, a claim, a reply, an internal note.
	agent := w.staffMember(support.PermissionRead, support.PermissionReply)
	other := w.staffMember(support.PermissionRead, support.PermissionReply)

	queue, _ := must2[support.Ticket](t)(w.service.ListQueue(ctx, support.QueueQuery{}))
	if !containsTicket(queue, ticket.ID) {
		t.Fatal("ticket missing from the queue")
	}

	claimed := must(w.service.ClaimTicket(ctx, agent, ticket.ID, method))(t)
	if claimed.Ticket.AssignedStaffID != agent.StaffID || claimed.Ticket.Status != support.StatusInProgress {
		t.Fatalf("claimed: %+v", claimed.Ticket)
	}

	_, err = w.service.ClaimTicket(ctx, other, ticket.ID, method)
	wantErr(t, err, support.ErrAlreadyAssigned)

	w.clock.advance(time.Minute)

	replied := must(w.service.Reply(ctx, agent, support.ReplyInput{TicketID: ticket.ID, Body: "Looking into it"}, method))(t)
	if replied.Ticket.Status != support.StatusWaitingUser || replied.Ticket.FirstResponseAt == nil {
		t.Fatalf("replied: %+v", replied.Ticket)
	}

	must(w.service.Reply(ctx, agent, support.ReplyInput{TicketID: ticket.ID, Body: "refund likely", Internal: true}, method))(t)

	view := must(w.service.GetMyTicket(ctx, caller, ticket.ID))(t)
	for _, m := range view.Messages {
		if m.Internal {
			t.Fatal("the rider sees an internal message")
		}
	}

	if len(view.Messages) != 3 {
		t.Fatalf("rider's messages: %d", len(view.Messages))
	}

	staffView := must(w.service.GetTicketForStaff(ctx, agent, ticket.ID, method))(t)
	if len(staffView.Messages) < 5 {
		t.Fatalf("staff messages: %d", len(staffView.Messages))
	}

	// The rider answers: back in progress.
	answered := must(w.service.AddTicketMessage(ctx, caller, ticket.ID, "thanks", nil))(t)
	if answered.Ticket.Status != support.StatusInProgress {
		t.Fatalf("after the rider's answer: %s", answered.Ticket.Status)
	}

	// Somebody else cannot read it or write in it.
	_, err = w.service.GetMyTicket(ctx, support.Caller{IdentityID: stranger.identity}, ticket.ID)
	wantErr(t, err, support.ErrNotFound)

	// Attachments: the rider's link; a stranger is not staff.
	must3(t)(w.service.AttachmentURL(ctx, caller, ticket.ID, attachment, method))

	_, _, err = w.service.AttachmentURL(ctx, support.Caller{IdentityID: stranger.identity}, ticket.ID, attachment, method)
	wantErr(t, err, support.ErrNotFound)

	must3(t)(w.service.AttachmentURL(ctx, support.Caller{IdentityID: agent.IdentityID}, ticket.ID, attachment, method))

	// Resolved: the rider is told; a message reopens it; the rider closes it.
	resolved := must(w.service.SetStatus(ctx, agent, ticket.ID, support.StatusResolved, method))(t)
	if resolved.Ticket.ResolvedAt == nil {
		t.Fatal("no resolved time")
	}

	reopened := must(w.service.AddTicketMessage(ctx, caller, ticket.ID, "not fixed", nil))(t)
	if reopened.Ticket.Status != support.StatusInProgress || reopened.Ticket.ResolvedAt != nil {
		t.Fatalf("reopened: %+v", reopened.Ticket)
	}

	closed := must(w.service.CloseMyTicket(ctx, caller, ticket.ID))(t)
	if closed.Ticket.Status != support.StatusClosed || closed.Ticket.ClosedAt == nil {
		t.Fatalf("closed: %+v", closed.Ticket)
	}

	_, err = w.service.AddTicketMessage(ctx, caller, ticket.ID, "hello?", nil)
	wantErr(t, err, support.ErrTicketClosed)

	got := w.events(t, ticket.ID)
	want := []string{
		"support.ticket_created",
		"support.reply_received:rider:" + rider.profile,
		"support.ticket_resolved:rider:" + rider.profile,
	}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("events:\n got %v\nwant %v", got, want)
	}

	// The open-ticket limit (3), counted per audience.
	for i := 0; i < 3; i++ {
		must(w.service.CreateTicket(ctx, caller, support.CreateTicketInput{Audience: support.AudienceRider, CategoryKey: "other", Body: "x"}))(t)
	}

	_, err = w.service.CreateTicket(ctx, caller, support.CreateTicketInput{Audience: support.AudienceRider, CategoryKey: "other", Body: "x"})
	wantErr(t, err, support.ErrTooManyOpen)

	mine, _ := must2[support.Ticket](t)(w.service.ListMyTickets(ctx, caller, support.MyTicketsQuery{Audience: support.AudienceRider, Limit: 2}))
	if len(mine) != 2 {
		t.Fatalf("first page: %d", len(mine))
	}
}

func TestLostItemAndSafety(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	rider, driver := w.rider(), w.driver()
	caller := support.Caller{IdentityID: rider.identity}

	running := w.trip(rider, driver, "in_progress", time.Minute)
	_, err := w.service.CreateTicket(ctx, caller, support.CreateTicketInput{Audience: support.AudienceRider, CategoryKey: "lost_item", Body: "bag", TripID: running})
	wantErr(t, err, support.ErrTripNotCompleted)

	trip := w.trip(rider, driver, "completed", time.Hour)
	lost := must(w.service.CreateTicket(ctx, caller, support.CreateTicketInput{
		Audience: support.AudienceRider, CategoryKey: "lost_item", Body: "I left my bag", TripID: trip, TransactionID: uuid.NewString(),
	}))(t).Ticket

	if lost.ParticipantDriverID != driver.profile || lost.Priority != support.PriorityHigh {
		t.Fatalf("lost item: %+v", lost)
	}

	driverCaller := support.Caller{IdentityID: driver.identity}

	list, _ := must2[support.Ticket](t)(w.service.ListMyTickets(ctx, driverCaller, support.MyTicketsQuery{Audience: support.AudienceDriver}))
	if !containsTicket(list, lost.ID) {
		t.Fatal("the driver does not see the lost-item ticket")
	}

	answered := must(w.service.AddTicketMessage(ctx, driverCaller, lost.ID, "Found it", nil))(t)
	if answered.Role != support.RoleParticipant {
		t.Fatalf("role %s", answered.Role)
	}

	_, err = w.service.CloseMyTicket(ctx, driverCaller, lost.ID)
	wantErr(t, err, support.ErrPermissionDenied)

	must(w.service.AddTicketMessage(ctx, caller, lost.ID, "Great, when?", nil))(t)

	got := strings.Join(w.events(t, lost.ID), ",")
	want := strings.Join([]string{
		"support.ticket_created",
		"support.lost_item_reported:driver:" + driver.profile,
		"support.reply_received:rider:" + rider.profile,
		"support.reply_received:driver:" + driver.profile,
	}, ",")

	if got != want {
		t.Fatalf("events:\n got %s\nwant %s", got, want)
	}

	// Safety: urgent, out of the normal queue, needs support.safety.
	safety := must(w.service.CreateTicket(ctx, caller, support.CreateTicketInput{
		Audience: support.AudienceRider, CategoryKey: "safety", Body: "unsafe driving", TripID: trip,
	}))(t).Ticket

	if !safety.Safety || safety.Priority != support.PriorityUrgent {
		t.Fatalf("safety: %+v", safety)
	}

	queue, _ := must2[support.Ticket](t)(w.service.ListQueue(ctx, support.QueueQuery{}))
	if containsTicket(queue, safety.ID) {
		t.Fatal("the safety ticket is in the normal queue")
	}

	safetyQueue, _ := must2[support.Ticket](t)(w.service.ListQueue(ctx, support.QueueQuery{Safety: true}))
	if !containsTicket(safetyQueue, safety.ID) {
		t.Fatal("the safety ticket is not in the safety queue")
	}

	agent := w.staffMember(support.PermissionRead, support.PermissionReply)
	lead := w.staffMember(support.PermissionRead, support.PermissionReply, support.PermissionSafety)

	_, err = w.service.GetTicketForStaff(ctx, agent, safety.ID, method)
	wantErr(t, err, support.ErrPermissionDenied)

	must(w.service.GetTicketForStaff(ctx, lead, safety.ID, method))(t)

	_, err = w.service.SetPriority(ctx, lead, safety.ID, support.PriorityLow, method)
	wantErr(t, err, support.ErrSafetyPriority)

	// The driver is not in a safety ticket about them.
	_, err = w.service.GetMyTicket(ctx, driverCaller, safety.ID)
	wantErr(t, err, support.ErrNotFound)

	// An SOS opens a ticket once, in the name of who pressed it.
	alert := uuid.NewString()
	sos := support.SOSAlert{AlertID: alert, TripID: running, TriggeredBy: support.AudienceDriver, Latitude: "36.19", Longitude: "44.01"}

	if err := w.service.OpenSOSTicket(ctx, sos); err != nil {
		t.Fatal(err)
	}

	if err := w.service.OpenSOSTicket(ctx, sos); err != nil {
		t.Fatal(err)
	}

	var count int
	var audience, source, counterpart string

	if err := w.pool.QueryRow(ctx,
		`SELECT count(*), max(audience), max(source), max(counterpart_profile_id::text)
		 FROM support_tickets WHERE sos_alert_id = $1`, alert).Scan(&count, &audience, &source, &counterpart); err != nil {
		t.Fatal(err)
	}

	if count != 1 || audience != "driver" || source != "sos" || counterpart != rider.profile {
		t.Fatalf("sos ticket: %d %s %s %s", count, audience, source, counterpart)
	}

	envelope, _ := json.Marshal(map[string]any{"event_id": uuid.NewString(), "payload": map[string]string{
		"trip_id": running, "alert_id": uuid.NewString(), "triggered_by": "rider", "latitude": "1", "longitude": "2",
	}})

	if err := w.service.HandleSOSEvent(ctx, support.SubjectSOSTriggered, envelope); err != nil {
		t.Fatal(err)
	}
}

func TestActions(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	rider, driver := w.rider(), w.driver()
	caller := support.Caller{IdentityID: rider.identity}
	trip := w.trip(rider, driver, "completed", time.Hour)

	ticket := must(w.service.CreateTicket(ctx, caller, support.CreateTicketInput{
		Audience: support.AudienceRider, CategoryKey: "trip_fare", Body: "overcharged", TripID: trip,
	}))(t).Ticket

	agent := w.staffMember(support.PermissionRead, support.PermissionRefund)
	lead := w.staffMember(support.PermissionRead, support.PermissionApprove)

	_, err := w.service.RequestAction(ctx, agent, support.ActionInput{TicketID: ticket.ID, Kind: support.ActionRefund, Amount: "1000", Reason: "x"}, method)
	wantErr(t, err, support.ErrInvalidInput)

	_, err = w.service.RequestAction(ctx, agent, support.ActionInput{TicketID: ticket.ID, Kind: support.ActionRefund, Amount: "10.001", Reason: "double charge"}, method)
	wantErr(t, err, support.ErrInvalidInput)

	done := must(w.service.RequestAction(ctx, agent, support.ActionInput{
		TicketID: ticket.ID, Kind: support.ActionRefund, Amount: "6000", DriverAmount: "1000", Reason: "double charge",
	}, method))(t)

	if done.Status != support.ActionCompleted || len(w.wallet.calls) != 1 {
		t.Fatalf("refund: %+v %+v", done, w.wallet.calls)
	}

	if c := w.wallet.calls[0]; c.acting != agent.IdentityID || c.target != trip || c.amount != "6000.00" ||
		c.driverAmount != "1000.00" || c.key != "support-action:"+done.ID {
		t.Fatalf("wallet call: %+v", c)
	}

	// 6000 + 5000 is over the 10000 limit: it waits.
	waiting := must(w.service.RequestAction(ctx, agent, support.ActionInput{
		TicketID: ticket.ID, Kind: support.ActionCompensation, Amount: "5000", Reason: "goodwill",
	}, method))(t)

	if waiting.Status != support.ActionPendingApproval || len(w.wallet.calls) != 1 {
		t.Fatalf("over the limit: %+v", waiting)
	}

	pending, _ := must2[support.Action](t)(w.service.ListPendingActions(ctx, 10, ""))
	if len(pending) != 1 || pending[0].ID != waiting.ID {
		t.Fatalf("pending: %+v", pending)
	}

	_, err = w.service.ApproveAction(ctx, agent, waiting.ID, "")
	wantErr(t, err, support.ErrOwnApproval)

	approved := must(w.service.ApproveAction(ctx, lead, waiting.ID, "ok"))(t)
	if approved.Status != support.ActionCompleted || approved.DecidedByStaffID != lead.StaffID {
		t.Fatalf("approved: %+v", approved)
	}

	if c := w.wallet.calls[1]; c.kind != "credit" || c.acting != lead.IdentityID || c.target != rider.profile {
		t.Fatalf("credit: %+v", c)
	}

	_, err = w.service.ApproveAction(ctx, lead, waiting.ID, "")
	wantErr(t, err, support.ErrActionNotPending)

	rejectMe := must(w.service.RequestAction(ctx, agent, support.ActionInput{
		TicketID: ticket.ID, Kind: support.ActionCompensation, Amount: "1", Reason: "goodwill",
	}, method))(t)

	rejected := must(w.service.RejectAction(ctx, lead, rejectMe.ID, "not needed"))(t)
	if rejected.Status != support.ActionRejected {
		t.Fatalf("rejected: %+v", rejected)
	}

	// A wallet refusal fails the action, with the reason.
	limitless := w.trip(rider, driver, "completed", time.Hour)
	second := must(w.service.CreateTicket(ctx, caller, support.CreateTicketInput{
		Audience: support.AudienceRider, CategoryKey: "trip_fare", Body: "again", TripID: limitless,
	}))(t).Ticket

	w.wallet.err = fmt.Errorf("%w: refund: refunds would exceed the fare", support.ErrRefused)
	failed := must(w.service.RequestAction(ctx, agent, support.ActionInput{
		TicketID: second.ID, Kind: support.ActionRefund, Amount: "100", Reason: "too much",
	}, method))(t)

	if failed.Status != support.ActionFailed || failed.FailureReason != "refund: refunds would exceed the fare" {
		t.Fatalf("failed: %+v", failed)
	}

	// A wallet that does not answer leaves it processing; the worker
	// finishes it once the lease is over.
	w.wallet.err = fmt.Errorf("%w: timeout", support.ErrUpstreamUnavailable)
	stuck := must(w.service.RequestAction(ctx, agent, support.ActionInput{
		TicketID: second.ID, Kind: support.ActionRefund, Amount: "100", Reason: "retry me",
	}, method))(t)

	if stuck.Status != support.ActionProcessing {
		t.Fatalf("stuck: %+v", stuck)
	}

	if n := w.service.RetryProcessing(ctx, 10); n != 0 {
		t.Fatalf("retried within the lease: %d", n)
	}

	w.wallet.err = nil
	w.clock.advance(2 * time.Minute)

	if n := w.service.RetryProcessing(ctx, 10); n != 1 {
		t.Fatalf("retried: %d", n)
	}

	finished := must(NewSupportRepository(w.pool).GetAction(ctx, stuck.ID))(t)
	if finished.Status != support.ActionCompleted {
		t.Fatalf("after the retry: %+v", finished)
	}

	// Fee waiver: a cancelled trip only, for what is left.
	_, err = w.service.RequestAction(ctx, agent, support.ActionInput{TicketID: ticket.ID, Kind: support.ActionWaiveFee, Reason: "driver late"}, method)
	wantErr(t, err, support.ErrActionNotAllowed)

	cancelled := w.trip(rider, driver, "cancelled", time.Hour)
	feeTicket := must(w.service.CreateTicket(ctx, caller, support.CreateTicketInput{
		Audience: support.AudienceRider, CategoryKey: "trip_fare", Body: "fee", TripID: cancelled,
	}))(t).Ticket

	_, err = w.service.RequestAction(ctx, agent, support.ActionInput{TicketID: feeTicket.ID, Kind: support.ActionWaiveFee, Reason: "driver late"}, method)
	wantErr(t, err, support.ErrNothingToWaive)

	w.wallet.refundable = decimal.NewFromInt(1500)
	waived := must(w.service.RequestAction(ctx, agent, support.ActionInput{TicketID: feeTicket.ID, Kind: support.ActionWaiveFee, Reason: "driver late"}, method))(t)
	if waived.Status != support.ActionCompleted || waived.Amount.StringFixed(2) != "1500.00" {
		t.Fatalf("waived: %+v", waived)
	}

	// Suspending the trip's driver, for a day; lifted when the time is up.
	suspender := w.staffMember(support.PermissionRead, support.PermissionSuspend)
	until := w.clock.Now().Add(24 * time.Hour)

	_, err = w.service.RequestAction(ctx, suspender, support.ActionInput{
		TicketID: ticket.ID, Kind: support.ActionSuspend, Target: support.TargetCounterpart, Reason: "complaint",
		SuspendUntil: ptr(w.clock.Now().Add(time.Minute)),
	}, method)
	wantErr(t, err, support.ErrInvalidInput)

	suspended := must(w.service.RequestAction(ctx, suspender, support.ActionInput{
		TicketID: ticket.ID, Kind: support.ActionSuspend, Target: support.TargetCounterpart, Reason: "complaint", SuspendUntil: &until,
	}, method))(t)

	if suspended.Status != support.ActionCompleted || !w.accounts.suspended[driver.identity] ||
		len(w.profiles.offline) != 1 || w.profiles.offline[0] != driver.profile {
		t.Fatalf("suspended: %+v %v %v", suspended, w.accounts.suspended, w.profiles.offline)
	}

	if n := w.service.LiftDueSuspensions(ctx, 10); n != 0 {
		t.Fatalf("lifted early: %d", n)
	}

	w.clock.advance(25 * time.Hour)

	if n := w.service.LiftDueSuspensions(ctx, 10); n != 1 || w.accounts.suspended[driver.identity] {
		t.Fatalf("lifted: %d %v", n, w.accounts.suspended)
	}

	if n := w.service.LiftDueSuspensions(ctx, 10); n != 0 {
		t.Fatalf("lifted twice: %d", n)
	}

	// A suspension until reactivated by hand.
	must(w.service.RequestAction(ctx, suspender, support.ActionInput{
		TicketID: ticket.ID, Kind: support.ActionSuspend, Target: support.TargetRequester, Reason: "abuse",
	}, method))(t)

	if !w.accounts.suspended[rider.identity] {
		t.Fatal("rider not suspended")
	}

	must(w.service.RequestAction(ctx, suspender, support.ActionInput{
		TicketID: ticket.ID, Kind: support.ActionReactivate, Target: support.TargetRequester, Reason: "cleared",
	}, method))(t)

	if w.accounts.suspended[rider.identity] {
		t.Fatal("rider still suspended")
	}

	view := must(w.service.GetTicketForStaff(ctx, agent, ticket.ID, method))(t)
	if len(view.Actions) != 6 {
		t.Fatalf("actions on the ticket: %d", len(view.Actions))
	}
}

func TestQueuePaging(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	rider := w.rider()
	caller := support.Caller{IdentityID: rider.identity}

	w.service = support.NewService(support.Dependencies{
		Repository: NewSupportRepository(w.pool), Trips: w.trips, Profiles: w.profiles, Media: w.media,
		Wallet: w.wallet, Accounts: w.accounts, Staff: w.staff, IDs: ids{}, Clock: w.clock,
	}, support.Config{RefundLimit: decimal.Zero, MaxOpenTickets: 50, TripMaxAge: time.Hour},
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	var keys []string

	for _, category := range []string{"app_issue", "other", "account", "app_issue", "payment_wallet"} {
		w.clock.advance(time.Second)
		ticket := must(w.service.CreateTicket(ctx, caller, support.CreateTicketInput{Audience: support.AudienceRider, CategoryKey: category, Body: "x"}))(t).Ticket
		keys = append(keys, string(ticket.Priority))
	}

	var all []support.Ticket
	cursor := ""

	for {
		page, next := must2[support.Ticket](t)(w.service.ListQueue(ctx, support.QueueQuery{Limit: 2, Cursor: cursor}))
		all = append(all, page...)

		if next == "" {
			break
		}

		cursor = next
	}

	if len(all) != 5 {
		t.Fatalf("paged: %d", len(all))
	}

	for i := 1; i < len(all); i++ {
		a, b := all[i-1], all[i]
		if rank(a.Priority) < rank(b.Priority) || (a.Priority == b.Priority && a.CreatedAt.After(b.CreatedAt)) {
			t.Fatalf("order: %v then %v", a, b)
		}
	}

	_ = keys
}

func rank(p support.Priority) int { return priorityRank(p) }

func ptr[T any](v T) *T { return &v }

func containsTicket(tickets []support.Ticket, id string) bool {
	for _, t := range tickets {
		if t.ID == id {
			return true
		}
	}

	return false
}

func must2[T any](t *testing.T) func([]T, string, error) ([]T, string) {
	return func(tickets []T, next string, err error) ([]T, string) {
		t.Helper()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		return tickets, next
	}
}

func must3(t *testing.T) func(string, time.Time, error) {
	return func(_ string, _ time.Time, err error) {
		t.Helper()

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}
