package documents_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/7akoom/ride-platform/services/driver-service/internal/application/documents"
)

const (
	driverID   = "0b0e0000-0000-4000-8000-000000000001"
	identityID = "1d000000-0000-4000-8000-000000000001"
	mediaID    = "3e000000-0000-4000-8000-000000000001"
	documentID = "d0c00000-0000-4000-8000-000000000001"
)

// 2026-09-27 10:00 UTC is 13:00 in Baghdad, the same day.
var now = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

var types = map[string]documents.Type{
	"licence": {Code: "licence", MediaPurpose: documents.PurposeDriverDocument, NameEN: "Licence", NameAR: "إجازة", NameKU: "مۆڵەت",
		Required: true, RequiresNumber: true, RequiresExpiry: true, Active: true, SortOrder: 1},
	"photo": {Code: "photo", MediaPurpose: documents.PurposeProfilePhoto, NameEN: "Photo", NameAR: "صورة", NameKU: "وێنە",
		Required: true, Active: true, SortOrder: 2},
	"extra": {Code: "extra", MediaPurpose: documents.PurposeDriverDocument, NameEN: "Extra", NameAR: "إضافي", NameKU: "زیادە",
		Required: false, Active: true, SortOrder: 3},
	"old": {Code: "old", MediaPurpose: documents.PurposeDriverDocument, NameEN: "Old", NameAR: "قديم", NameKU: "کۆن",
		Required: true, Active: false, SortOrder: 4},
}

type fakeRepo struct {
	docs        []documents.Document
	submitErr   error
	superseded  []documents.Document
	submitted   []documents.Document
	approved    []documents.ApproveRecord
	approveErr  error
	rejected    []documents.RejectRecord
	upserted    []documents.Type
	pendingSeen []documents.PendingQuery
	pending     []documents.PendingItem
}

func (r *fakeRepo) ListTypes(_ context.Context, includeInactive bool) ([]documents.Type, error) {
	var out []documents.Type

	for _, t := range types {
		if t.Active || includeInactive {
			out = append(out, t)
		}
	}

	slices.SortFunc(out, func(a, b documents.Type) int { return a.SortOrder - b.SortOrder })

	return out, nil
}

func (r *fakeRepo) GetType(_ context.Context, code string) (documents.Type, error) {
	t, ok := types[code]
	if !ok {
		return documents.Type{}, documents.ErrTypeNotFound
	}

	return t, nil
}

func (r *fakeRepo) UpsertType(_ context.Context, t documents.Type) (documents.Type, error) {
	r.upserted = append(r.upserted, t)

	return t, nil
}

func (r *fakeRepo) Submit(_ context.Context, d documents.Document) (documents.Document, []documents.Document, error) {
	r.submitted = append(r.submitted, d)

	if r.submitErr != nil {
		return documents.Document{}, nil, r.submitErr
	}

	return d, r.superseded, nil
}

func (r *fakeRepo) Get(_ context.Context, id string) (documents.Document, error) {
	for _, d := range r.docs {
		if d.ID == id {
			return d, nil
		}
	}

	return documents.Document{}, documents.ErrDocumentNotFound
}

func (r *fakeRepo) ListByDriver(_ context.Context, _ string, _ bool) ([]documents.Document, error) {
	return r.docs, nil
}

func (r *fakeRepo) Approve(_ context.Context, record documents.ApproveRecord) (documents.Document, []documents.Document, error) {
	r.approved = append(r.approved, record)

	if r.approveErr != nil {
		return documents.Document{}, nil, r.approveErr
	}

	return documents.Document{ID: record.DocumentID, Status: documents.StatusApproved, Number: record.Number, ExpiresOn: record.ExpiresOn}, r.superseded, nil
}

func (r *fakeRepo) Reject(_ context.Context, record documents.RejectRecord) (documents.Document, error) {
	r.rejected = append(r.rejected, record)

	return documents.Document{ID: record.DocumentID, Status: documents.StatusRejected, RejectionReason: record.Reason}, nil
}

func (r *fakeRepo) ListPending(_ context.Context, q documents.PendingQuery) ([]documents.PendingItem, error) {
	r.pendingSeen = append(r.pendingSeen, q)

	return r.pending, nil
}

func (r *fakeRepo) RunExpiry(context.Context, documents.Date, []int, int) (documents.ExpiryRound, error) {
	return documents.ExpiryRound{}, nil
}

type fakeDrivers struct{ err error }

func (d fakeDrivers) IdentityOf(context.Context, string) (string, error) {
	if d.err != nil {
		return "", d.err
	}

	return identityID, nil
}

type fakeMedia struct {
	holdErr   error
	held      []string
	purposes  []documents.MediaPurpose
	released  []string
	discarded []string
}

func (m *fakeMedia) Hold(_ context.Context, id, owner string, purpose documents.MediaPurpose) error {
	if owner != identityID {
		return documents.ErrMediaNotUsable
	}

	m.held = append(m.held, id)
	m.purposes = append(m.purposes, purpose)

	return m.holdErr
}

func (m *fakeMedia) Release(_ context.Context, id string) error {
	m.released = append(m.released, id)

	return nil
}

func (m *fakeMedia) Discard(_ context.Context, id string) error {
	m.discarded = append(m.discarded, id)

	return nil
}

type fixedIDs struct{}

func (fixedIDs) NewID() string { return documentID }

type fixedClock struct{}

func (fixedClock) Now() time.Time { return now }

func newService(t *testing.T, repo *fakeRepo, media *fakeMedia) *documents.Service {
	t.Helper()

	baghdad, err := time.LoadLocation("Asia/Baghdad")
	if err != nil {
		t.Fatal(err)
	}

	return documents.NewService(repo, fakeDrivers{}, media, fixedIDs{}, fixedClock{},
		documents.Config{Location: baghdad, ReminderDays: []int{30, 7, 1, 7}},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func submitLicence() documents.SubmitInput {
	return documents.SubmitInput{
		DriverID: driverID, TypeCode: "licence", MediaID: mediaID,
		Number: "  ab  123-45 ", ExpiresOn: "2028-01-31",
	}
}

// --- submitting ------------------------------------------------------------------

func TestSubmit_HoldsTheFileAndSavesAPendingDocument(t *testing.T) {
	repo := &fakeRepo{superseded: []documents.Document{{MediaID: "old-file"}}}
	media := &fakeMedia{}

	got, err := newService(t, repo, media).Submit(context.Background(), submitLicence())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if got.Status != documents.StatusPending || got.Number != "AB 123-45" || got.ExpiresOn.String() != "2028-01-31" {
		t.Errorf("saved %+v", got)
	}

	if !slices.Equal(media.held, []string{mediaID}) || media.purposes[0] != documents.PurposeDriverDocument {
		t.Errorf("held %v %v", media.held, media.purposes)
	}

	if !slices.Equal(media.discarded, []string{"old-file"}) {
		t.Errorf("the superseded file was not deleted: %v", media.discarded)
	}
}

func TestSubmit_UsesTheTypesPurposeAndDropsWhatItDoesNotAskFor(t *testing.T) {
	repo := &fakeRepo{}
	media := &fakeMedia{}

	got, err := newService(t, repo, media).Submit(context.Background(), documents.SubmitInput{
		DriverID: driverID, TypeCode: "photo", MediaID: mediaID, Number: "123456", ExpiresOn: "not a date",
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	if got.Number != "" || !got.ExpiresOn.IsZero() {
		t.Errorf("kept details the type does not ask for: %+v", got)
	}

	if media.purposes[0] != documents.PurposeProfilePhoto {
		t.Errorf("purpose = %v", media.purposes[0])
	}
}

func TestSubmit_Refusals(t *testing.T) {
	cases := map[string]struct {
		change func(*documents.SubmitInput)
		want   error
	}{
		"bad driver id":       {func(in *documents.SubmitInput) { in.DriverID = "nope" }, documents.ErrDriverNotFound},
		"unknown type":        {func(in *documents.SubmitInput) { in.TypeCode = "passport" }, documents.ErrTypeNotFound},
		"inactive type":       {func(in *documents.SubmitInput) { in.TypeCode = "old" }, documents.ErrTypeNotFound},
		"no file":             {func(in *documents.SubmitInput) { in.MediaID = " " }, documents.ErrMediaIDRequired},
		"bad file id":         {func(in *documents.SubmitInput) { in.MediaID = "x" }, documents.ErrInvalidMediaID},
		"no number":           {func(in *documents.SubmitInput) { in.Number = "  " }, documents.ErrNumberRequired},
		"short number":        {func(in *documents.SubmitInput) { in.Number = "12" }, documents.ErrInvalidNumber},
		"odd number":          {func(in *documents.SubmitInput) { in.Number = "12<script>" }, documents.ErrInvalidNumber},
		"no expiry":           {func(in *documents.SubmitInput) { in.ExpiresOn = "" }, documents.ErrExpiryRequired},
		"bad expiry":          {func(in *documents.SubmitInput) { in.ExpiresOn = "31/01/2028" }, documents.ErrInvalidExpiryDate},
		"expires today":       {func(in *documents.SubmitInput) { in.ExpiresOn = "2026-09-27" }, documents.ErrExpiryNotInFuture},
		"expired":             {func(in *documents.SubmitInput) { in.ExpiresOn = "2026-01-01" }, documents.ErrExpiryNotInFuture},
		"too far":             {func(in *documents.SubmitInput) { in.ExpiresOn = "2050-01-01" }, documents.ErrExpiryTooFar},
		"someone else's file": {nil, documents.ErrMediaNotUsable},
	}

	for name, c := range cases {
		repo := &fakeRepo{}
		media := &fakeMedia{}
		svc := newService(t, repo, media)

		in := submitLicence()
		if c.change != nil {
			c.change(&in)
		} else {
			svc = documents.NewService(repo, fakeDrivers{}, &otherOwnerMedia{}, fixedIDs{}, fixedClock{},
				documents.Config{Location: time.UTC}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		}

		if _, err := svc.Submit(context.Background(), in); !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", name, err, c.want)
		}

		if len(repo.submitted) != 0 {
			t.Errorf("%s: a document was saved", name)
		}
	}
}

type otherOwnerMedia struct{ fakeMedia }

func (m *otherOwnerMedia) Hold(context.Context, string, string, documents.MediaPurpose) error {
	return documents.ErrMediaNotUsable
}

func TestSubmit_ExpiryIsJudgedInTheDeploymentsTimeZone(t *testing.T) {
	// 22:30 UTC on the 27th is already the 28th in Baghdad: the 28th is "today".
	late := time.Date(2026, 9, 27, 22, 30, 0, 0, time.UTC)
	baghdad, _ := time.LoadLocation("Asia/Baghdad")

	svc := documents.NewService(&fakeRepo{}, fakeDrivers{}, &fakeMedia{}, fixedIDs{}, clockAt(late),
		documents.Config{Location: baghdad}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	in := submitLicence()
	in.ExpiresOn = "2026-09-28"

	if _, err := svc.Submit(context.Background(), in); !errors.Is(err, documents.ErrExpiryNotInFuture) {
		t.Fatalf("got %v, want ErrExpiryNotInFuture", err)
	}
}

type clockAt time.Time

func (c clockAt) Now() time.Time { return time.Time(c) }

func TestSubmit_FailureLetsTheFileGoUnlessAnotherDocumentUsesIt(t *testing.T) {
	repo := &fakeRepo{submitErr: errors.New("database down")}
	media := &fakeMedia{}

	if _, err := newService(t, repo, media).Submit(context.Background(), submitLicence()); err == nil {
		t.Fatal("no error")
	}

	if !slices.Equal(media.released, []string{mediaID}) {
		t.Errorf("released %v, want the new file", media.released)
	}

	repo = &fakeRepo{submitErr: documents.ErrMediaAlreadyUsed}
	media = &fakeMedia{}

	if _, err := newService(t, repo, media).Submit(context.Background(), submitLicence()); !errors.Is(err, documents.ErrMediaAlreadyUsed) {
		t.Fatalf("got %v", err)
	}

	if len(media.released) != 0 {
		t.Errorf("released a file another document still needs: %v", media.released)
	}
}

// --- where the driver stands ----------------------------------------------------------

func date(s string) documents.Date {
	d, err := documents.ParseDate(s)
	if err != nil {
		panic(err)
	}

	return d
}

func TestOverview_States(t *testing.T) {
	older := now.Add(-time.Hour)

	cases := map[string]struct {
		docs      []documents.Document
		licence   documents.State
		compliant bool
	}{
		"nothing yet": {nil, documents.StateMissing, false},
		"waiting": {[]documents.Document{
			{TypeCode: "licence", Status: documents.StatusPending},
		}, documents.StatePendingReview, false},
		"turned down": {[]documents.Document{
			{TypeCode: "licence", Status: documents.StatusRejected},
		}, documents.StateRejected, false},
		"approved": {[]documents.Document{
			{TypeCode: "licence", Status: documents.StatusApproved, ExpiresOn: date("2027-12-31")},
		}, documents.StateApproved, true},
		"expiring in 30 days": {[]documents.Document{
			{TypeCode: "licence", Status: documents.StatusApproved, ExpiresOn: date("2026-10-27")},
		}, documents.StateExpiringSoon, true},
		"last valid day is today": {[]documents.Document{
			{TypeCode: "licence", Status: documents.StatusApproved, ExpiresOn: date("2026-09-27")},
		}, documents.StateExpiringSoon, true},
		"ran out yesterday, renewal waiting": {[]documents.Document{
			{TypeCode: "licence", Status: documents.StatusPending, CreatedAt: now},
			{TypeCode: "licence", Status: documents.StatusApproved, ExpiresOn: date("2026-09-26"), CreatedAt: older},
		}, documents.StateExpired, false},
	}

	for name, c := range cases {
		docs := append(c.docs, documents.Document{TypeCode: "photo", Status: documents.StatusApproved})
		svc := newService(t, &fakeRepo{docs: docs}, &fakeMedia{})

		got, err := svc.Overview(context.Background(), driverID, false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		if len(got.Requirements) != 3 {
			t.Fatalf("%s: %d requirements, want the 3 active types", name, len(got.Requirements))
		}

		if got.Requirements[0].Type.Code != "licence" || got.Requirements[0].State != c.licence {
			t.Errorf("%s: licence is %s, want %s", name, got.Requirements[0].State, c.licence)
		}

		if got.Compliant != c.compliant {
			t.Errorf("%s: compliant = %v, want %v (missing %v)", name, got.Compliant, c.compliant, got.Missing)
		}

		// The optional type never blocks.
		if slices.Contains(got.Missing, "extra") {
			t.Errorf("%s: an optional document counted as missing", name)
		}
	}
}

func TestOverview_ARejectionOlderThanAWaitingOneIsHistory(t *testing.T) {
	docs := []documents.Document{
		{TypeCode: "licence", Status: documents.StatusPending, CreatedAt: now},
		{TypeCode: "licence", Status: documents.StatusRejected, CreatedAt: now.Add(-time.Hour)},
	}

	got, err := newService(t, &fakeRepo{docs: docs}, &fakeMedia{}).Overview(context.Background(), driverID, false)
	if err != nil {
		t.Fatal(err)
	}

	if got.Requirements[0].Rejected != nil || got.Requirements[0].Pending == nil {
		t.Errorf("got %+v", got.Requirements[0])
	}
}

func TestCheckCompliance_NamesWhatIsMissing(t *testing.T) {
	svc := newService(t, &fakeRepo{docs: []documents.Document{{TypeCode: "photo", Status: documents.StatusApproved}}}, &fakeMedia{})

	got, err := svc.CheckCompliance(context.Background(), driverID)
	if err != nil {
		t.Fatal(err)
	}

	if got.Compliant || !slices.Equal(got.Missing, []string{"licence"}) {
		t.Errorf("got %+v", got)
	}
}

// --- review -----------------------------------------------------------------------------

func pendingLicence() documents.Document {
	return documents.Document{
		ID: documentID, DriverID: driverID, TypeCode: "licence", MediaID: mediaID,
		Number: "AB123", ExpiresOn: date("2028-01-31"), Status: documents.StatusPending,
	}
}

func TestApprove_TakesTheReviewersCorrections(t *testing.T) {
	repo := &fakeRepo{docs: []documents.Document{pendingLicence()}, superseded: []documents.Document{{MediaID: "old-file"}}}
	media := &fakeMedia{}

	if _, err := newService(t, repo, media).Approve(context.Background(), documents.ApproveInput{
		DocumentID: documentID, Number: "ab999", ExpiresOn: "2029-05-01", ReviewedBy: "  " + identityID,
	}); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	got := repo.approved[0]
	if got.Number != "AB999" || got.ExpiresOn.String() != "2029-05-01" || got.ReviewedBy != identityID {
		t.Errorf("approved with %+v", got)
	}

	if !slices.Equal(media.discarded, []string{"old-file"}) {
		t.Errorf("the replaced approved file was not deleted: %v", media.discarded)
	}
}

func TestApprove_KeepsTheDriversDetailsAndRefusesAnOutOfDateOne(t *testing.T) {
	repo := &fakeRepo{docs: []documents.Document{pendingLicence()}}

	if _, err := newService(t, repo, &fakeMedia{}).Approve(context.Background(), documents.ApproveInput{DocumentID: documentID}); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	if repo.approved[0].Number != "AB123" || repo.approved[0].ExpiresOn.String() != "2028-01-31" || repo.approved[0].ReviewedBy != "" {
		t.Errorf("approved with %+v", repo.approved[0])
	}

	stale := pendingLicence()
	stale.ExpiresOn = date("2026-09-20")
	repo = &fakeRepo{docs: []documents.Document{stale}}

	if _, err := newService(t, repo, &fakeMedia{}).Approve(context.Background(), documents.ApproveInput{DocumentID: documentID}); !errors.Is(err, documents.ErrExpiryNotInFuture) {
		t.Fatalf("got %v, want ErrExpiryNotInFuture", err)
	}
}

func TestApprove_OnlyPending(t *testing.T) {
	approved := pendingLicence()
	approved.Status = documents.StatusApproved
	repo := &fakeRepo{docs: []documents.Document{approved}}

	if _, err := newService(t, repo, &fakeMedia{}).Approve(context.Background(), documents.ApproveInput{DocumentID: documentID}); !errors.Is(err, documents.ErrDocumentNotPending) {
		t.Fatalf("got %v", err)
	}

	if _, err := newService(t, repo, &fakeMedia{}).Approve(context.Background(), documents.ApproveInput{DocumentID: "nope"}); !errors.Is(err, documents.ErrDocumentNotFound) {
		t.Fatalf("bad id: got %v", err)
	}
}

func TestReject_NeedsAReasonAndWithdrawingARequiredOneTakesTheDriverOffline(t *testing.T) {
	repo := &fakeRepo{docs: []documents.Document{pendingLicence()}}
	svc := newService(t, repo, &fakeMedia{})

	if _, err := svc.Reject(context.Background(), documents.RejectInput{DocumentID: documentID, Reason: "  "}); !errors.Is(err, documents.ErrReasonRequired) {
		t.Fatalf("no reason: got %v", err)
	}

	if _, err := svc.Reject(context.Background(), documents.RejectInput{DocumentID: documentID, Reason: " blurry "}); err != nil {
		t.Fatal(err)
	}

	if r := repo.rejected[0]; r.Reason != "blurry" || r.TakeOffline || r.ExpectedStatus != documents.StatusPending {
		t.Errorf("rejected a pending one with %+v", r)
	}

	approved := pendingLicence()
	approved.Status = documents.StatusApproved
	repo = &fakeRepo{docs: []documents.Document{approved}}

	if _, err := newService(t, repo, &fakeMedia{}).Reject(context.Background(), documents.RejectInput{DocumentID: documentID, Reason: "forged"}); err != nil {
		t.Fatal(err)
	}

	if r := repo.rejected[0]; !r.TakeOffline || r.ExpectedStatus != documents.StatusApproved {
		t.Errorf("withdrew an approved one with %+v", r)
	}

	superseded := pendingLicence()
	superseded.Status = documents.StatusSuperseded
	repo = &fakeRepo{docs: []documents.Document{superseded}}

	if _, err := newService(t, repo, &fakeMedia{}).Reject(context.Background(), documents.RejectInput{DocumentID: documentID, Reason: "x"}); !errors.Is(err, documents.ErrDocumentNotReviewable) {
		t.Fatalf("superseded: got %v", err)
	}
}

// --- types, queue, reminders ---------------------------------------------------------------

func TestUpsertType_Checks(t *testing.T) {
	svc := newService(t, &fakeRepo{}, &fakeMedia{})
	good := documents.Type{Code: "taxi_permit", MediaPurpose: documents.PurposeDriverDocument, NameEN: " Taxi permit ", NameAR: "إجازة تاكسي", NameKU: "مۆڵەتی تەکسی"}

	saved, err := svc.UpsertType(context.Background(), good)
	if err != nil || saved.NameEN != "Taxi permit" {
		t.Fatalf("got %+v, %v", saved, err)
	}

	bad := map[string]func(*documents.Type){
		"code":    func(t *documents.Type) { t.Code = "Taxi Permit" },
		"purpose": func(t *documents.Type) { t.MediaPurpose = "selfie" },
		"name":    func(t *documents.Type) { t.NameKU = " " },
		"order":   func(t *documents.Type) { t.SortOrder = 5000 },
	}

	for name, change := range bad {
		in := good
		change(&in)

		if _, err := svc.UpsertType(context.Background(), in); err == nil {
			t.Errorf("accepted a bad %s", name)
		}
	}
}

func TestPending_PagesWithAnOpaqueToken(t *testing.T) {
	items := make([]documents.PendingItem, 3)
	for i := range items {
		items[i].Document = documents.Document{ID: documentID, CreatedAt: now.Add(time.Duration(i) * time.Minute)}
	}

	repo := &fakeRepo{pending: items}
	svc := newService(t, repo, &fakeMedia{})

	page, err := svc.Pending(context.Background(), 2, "")
	if err != nil || len(page.Items) != 2 || page.NextPageToken == "" {
		t.Fatalf("got %d items, token %q, %v", len(page.Items), page.NextPageToken, err)
	}

	repo.pending = nil

	if _, err := svc.Pending(context.Background(), 2, page.NextPageToken); err != nil {
		t.Fatal(err)
	}

	next := repo.pendingSeen[1]
	if next.AfterID != documentID || !next.AfterCreatedAt.Equal(now.Add(time.Minute)) || next.Limit != 3 {
		t.Errorf("second page asked for %+v", next)
	}

	if _, err := svc.Pending(context.Background(), 2, "garbage"); !errors.Is(err, documents.ErrInvalidPageToken) {
		t.Errorf("bad token: %v", err)
	}
}

func TestReminderThreshold(t *testing.T) {
	thresholds := []int{1, 7, 30}

	cases := []struct {
		daysLeft int
		want     int
		ok       bool
	}{
		{45, 0, false}, {30, 30, true}, {12, 30, true}, {7, 7, true}, {5, 7, true}, {1, 1, true}, {0, 1, true},
	}

	for _, c := range cases {
		got, ok := documents.ReminderThreshold(c.daysLeft, thresholds)
		if got != c.want || ok != c.ok {
			t.Errorf("%d days left: got %d %v, want %d %v", c.daysLeft, got, ok, c.want, c.ok)
		}
	}
}

func TestNormalizeNumber(t *testing.T) {
	good := map[string]string{
		" 1234 5678 ": "1234 5678",
		"ab-12/99":    "AB-12/99",
		"أ ب ١٢٣":     "أ ب ١٢٣",
	}

	for in, want := range good {
		if got, err := documents.NormalizeNumber(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v", in, got, err)
		}
	}
}
