package postgres

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/7akoom/ride-platform/services/staff-service/internal/application/staff"
)

// These tests run against a real, EMPTY, throw-away database:
//
//	STAFF_TEST_DATABASE_URL=postgres://... go test ./internal/infrastructure/persistence/postgres/
//
// They apply the migration's Up section themselves (psql must be on PATH) and
// drop everything afterwards. Without the variable they are skipped.

const (
	ownerRole      = "5e7a0000-0000-4000-8000-000000000001"
	operationsRole = "5e7a0000-0000-4000-8000-000000000002"
)

func testRepository(t *testing.T) (*StaffRepository, *pgxpool.Pool) {
	t.Helper()

	url := os.Getenv("STAFF_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("STAFF_TEST_DATABASE_URL is not set")
	}

	migration, err := os.ReadFile("../../../../migrations/00001_create_staff.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	up, down, ok := strings.Cut(string(migration), "-- +goose Down")
	if !ok {
		t.Fatal("migration has no Down section")
	}

	psql := func(sql string) {
		cmd := exec.Command("psql", url, "-v", "ON_ERROR_STOP=1", "-q")
		cmd.Stdin = strings.NewReader(sql)

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("psql: %v\n%s", err, out)
		}
	}

	psql(down)
	psql(up)
	t.Cleanup(func() { psql(down) })

	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	t.Cleanup(pool.Close)

	return NewStaffRepository(pool), pool
}

func invite(t *testing.T, repo *StaffRepository, email string, roles ...string) staff.Member {
	t.Helper()

	member, err := repo.CreateMember(context.Background(), staff.NewMember{
		ID: uuid.NewString(), Email: email, DisplayName: "Member " + email, RoleIDs: roles, InvitedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("invite %s: %v", email, err)
	}

	return member
}

func activate(t *testing.T, repo *StaffRepository, member staff.Member) staff.Member {
	t.Helper()

	active, err := repo.ActivateMember(context.Background(), member.ID, uuid.NewString(), time.Now())
	if err != nil {
		t.Fatalf("activate %s: %v", member.Email, err)
	}

	return active
}

func TestMembersLifecycle(t *testing.T) {
	repo, _ := testRepository(t)
	ctx := context.Background()

	owner := invite(t, repo, "owner@example.com", ownerRole)
	if owner.Status != staff.StatusInvited || len(owner.Roles) != 1 || owner.Roles[0].Key != staff.RoleKeyOwner {
		t.Fatalf("unexpected invitation %+v", owner)
	}

	if _, err := repo.CreateMember(ctx, staff.NewMember{
		ID: uuid.NewString(), Email: "owner@example.com", DisplayName: "Dup", InvitedAt: time.Now(),
	}); !errors.Is(err, staff.ErrEmailAlreadyInvited) {
		t.Fatalf("duplicate email: err = %v", err)
	}

	found, err := repo.FindInvitedMemberByEmails(ctx, []string{"x@example.com", "owner@example.com"})
	if err != nil || found.ID != owner.ID {
		t.Fatalf("find invitation: %+v, %v", found, err)
	}

	owner = activate(t, repo, owner)
	if owner.Status != staff.StatusActive || owner.IdentityID == "" || owner.ActivatedAt == nil {
		t.Fatalf("unexpected activated owner %+v", owner)
	}

	if _, err := repo.ActivateMember(ctx, owner.ID, uuid.NewString(), time.Now()); !errors.Is(err, staff.ErrInvalidTransition) {
		t.Fatalf("activating twice: err = %v", err)
	}

	other := invite(t, repo, "other@example.com", operationsRole)
	if _, err := repo.ActivateMember(ctx, other.ID, owner.IdentityID, time.Now()); !errors.Is(err, staff.ErrIdentityAlreadyStaff) {
		t.Fatalf("binding an identity twice: err = %v", err)
	}

	byIdentity, err := repo.FindMemberByIdentityID(ctx, owner.IdentityID)
	if err != nil || byIdentity.ID != owner.ID {
		t.Fatalf("find by identity: %+v, %v", byIdentity, err)
	}

	revoked, err := repo.SetMemberStatus(ctx, other.ID, staff.StatusInvited, staff.StatusRevoked)
	if err != nil || revoked.Status != staff.StatusRevoked {
		t.Fatalf("revoke: %+v, %v", revoked, err)
	}

	again := invite(t, repo, "other@example.com", operationsRole)
	if again.ID == other.ID {
		t.Fatal("a revoked invitation must free the address for a new one")
	}

	if _, err := repo.SetMemberStatus(ctx, uuid.NewString(), staff.StatusActive, staff.StatusSuspended); !errors.Is(err, staff.ErrMemberNotFound) {
		t.Fatalf("unknown member: err = %v", err)
	}

	if n, _ := repo.CountMembers(ctx); n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}
}

func TestTheLastActiveOwnerIsProtected(t *testing.T) {
	repo, _ := testRepository(t)
	ctx := context.Background()

	first := activate(t, repo, invite(t, repo, "first@example.com", ownerRole))

	if _, err := repo.SetMemberStatus(ctx, first.ID, staff.StatusActive, staff.StatusSuspended); !errors.Is(err, staff.ErrLastOwner) {
		t.Fatalf("suspending the only owner: err = %v", err)
	}

	if _, err := repo.SetMemberRoles(ctx, first.ID, []string{operationsRole}); !errors.Is(err, staff.ErrLastOwner) {
		t.Fatalf("demoting the only owner: err = %v", err)
	}

	reread, _ := repo.FindMemberByID(ctx, first.ID)
	if reread.Status != staff.StatusActive || !reread.IsOwner() {
		t.Fatalf("the refused changes must be rolled back: %+v", reread)
	}

	second := activate(t, repo, invite(t, repo, "second@example.com", ownerRole))

	if _, err := repo.SetMemberStatus(ctx, first.ID, staff.StatusActive, staff.StatusSuspended); err != nil {
		t.Fatalf("suspending one of two owners: %v", err)
	}

	if _, err := repo.SetMemberRoles(ctx, second.ID, []string{operationsRole}); !errors.Is(err, staff.ErrLastOwner) {
		t.Fatalf("demoting the last active owner: err = %v", err)
	}

	updated, err := repo.SetMemberRoles(ctx, second.ID, []string{ownerRole, operationsRole})
	if err != nil || len(updated.Roles) != 2 {
		t.Fatalf("adding a role to an owner: %+v, %v", updated.Roles, err)
	}

	if _, err := repo.SetMemberRoles(ctx, second.ID, []string{uuid.NewString()}); !errors.Is(err, staff.ErrRoleNotFound) {
		t.Fatalf("unknown role: err = %v", err)
	}
}

func TestListMembersPagesNewestFirst(t *testing.T) {
	repo, _ := testRepository(t)
	ctx := context.Background()

	var ids []string
	for _, email := range []string{"a@example.com", "b@example.com", "c@example.com"} {
		ids = append(ids, invite(t, repo, email, operationsRole).ID)
		time.Sleep(5 * time.Millisecond)
	}

	page, err := repo.ListMembers(ctx, staff.MemberQuery{Limit: 2})
	if err != nil || len(page) != 2 || page[0].ID != ids[2] || page[1].ID != ids[1] {
		t.Fatalf("first page: %v, %v", memberIDs(page), err)
	}

	if len(page[0].Roles) != 1 || len(page[0].Roles[0].Permissions) != 3 {
		t.Fatalf("roles must be loaded with their permissions: %+v", page[0].Roles)
	}

	rest, err := repo.ListMembers(ctx, staff.MemberQuery{AfterID: page[1].ID, Limit: 2})
	if err != nil || len(rest) != 1 || rest[0].ID != ids[0] {
		t.Fatalf("second page: %v, %v", memberIDs(rest), err)
	}

	none, err := repo.ListMembers(ctx, staff.MemberQuery{Status: staff.StatusActive, Limit: 10})
	if err != nil || len(none) != 0 {
		t.Fatalf("status filter: %v, %v", memberIDs(none), err)
	}
}

func memberIDs(members []staff.Member) []string {
	var out []string
	for _, member := range members {
		out = append(out, member.ID)
	}

	return out
}

func TestRoles(t *testing.T) {
	repo, _ := testRepository(t)
	ctx := context.Background()

	roles, err := repo.ListRoles(ctx)
	if err != nil || len(roles) != 2 || !roles[0].System {
		t.Fatalf("system roles: %+v, %v", roles, err)
	}

	custom, err := repo.CreateRole(ctx, staff.Role{
		ID: uuid.NewString(), Name: "Reviewers", Description: "d", Permissions: []string{staff.PermissionDriversRead},
	})
	if err != nil || len(custom.Permissions) != 1 || custom.System {
		t.Fatalf("create: %+v, %v", custom, err)
	}

	if _, err := repo.CreateRole(ctx, staff.Role{ID: uuid.NewString(), Name: "reviewers"}); !errors.Is(err, staff.ErrRoleNameTaken) {
		t.Fatalf("name taken (any case): err = %v", err)
	}

	custom.Name = "Driver reviewers"
	custom.Permissions = []string{staff.PermissionDriversRead, staff.PermissionDriversReview}

	updated, err := repo.UpdateRole(ctx, custom)
	if err != nil || updated.Name != "Driver reviewers" || len(updated.Permissions) != 2 {
		t.Fatalf("update: %+v, %v", updated, err)
	}

	system, _ := repo.FindRoleByKey(ctx, "operations")
	system.Name = "Hacked"
	if _, err := repo.UpdateRole(ctx, system); !errors.Is(err, staff.ErrRoleNotFound) {
		t.Fatalf("updating a system role must not match: err = %v", err)
	}

	holder := invite(t, repo, "holder@example.com", custom.ID)

	if err := repo.DeleteRole(ctx, custom.ID); !errors.Is(err, staff.ErrRoleInUse) {
		t.Fatalf("deleting a held role: err = %v", err)
	}

	if _, err := repo.SetMemberStatus(ctx, holder.ID, staff.StatusInvited, staff.StatusRevoked); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if _, err := repo.SetMemberRoles(ctx, holder.ID, []string{operationsRole}); !errors.Is(err, staff.ErrInvalidTransition) {
		t.Fatalf("changing roles of a revoked invitation: err = %v", err)
	}

	if err := repo.DeleteRole(ctx, ownerRole); !errors.Is(err, staff.ErrRoleNotFound) {
		t.Fatalf("deleting a system role must not match: err = %v", err)
	}

	byIDs, err := repo.FindRolesByIDs(ctx, []string{ownerRole, operationsRole, uuid.NewString()})
	if err != nil || len(byIDs) != 2 {
		t.Fatalf("find by ids: %d, %v", len(byIDs), err)
	}
}

func TestAuditIsAppendOnly(t *testing.T) {
	repo, pool := testRepository(t)
	ctx := context.Background()

	actor := activate(t, repo, invite(t, repo, "actor@example.com", ownerRole))
	base := time.Now().UTC().Truncate(time.Millisecond)

	allowed := staff.AuditEntry{
		ID: uuid.NewString(), OccurredAt: base, ActorStaffID: actor.ID, ActorIdentityID: actor.IdentityID,
		Permission: staff.PermissionDriversReview, Method: "/m/Approve", TargetID: "driver-1",
		Decision: staff.DecisionAllowed, Outcome: staff.OutcomePending,
	}
	denied := staff.AuditEntry{
		ID: uuid.NewString(), OccurredAt: base.Add(time.Second), ActorIdentityID: uuid.NewString(),
		Permission: staff.PermissionZonesManage, Method: "/m/CreateZone", Decision: staff.DecisionDenied,
	}

	for _, entry := range []staff.AuditEntry{allowed, denied} {
		if err := repo.RecordAudit(ctx, entry); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	if err := repo.CompleteAudit(ctx, allowed.ID, staff.OutcomeSucceeded, "OK", base.Add(time.Minute)); err != nil {
		t.Fatalf("complete: %v", err)
	}

	if err := repo.CompleteAudit(ctx, allowed.ID, staff.OutcomeFailed, "Internal", base.Add(time.Minute)); !errors.Is(err, staff.ErrAuditEntryNotFound) {
		t.Fatalf("completing twice: err = %v", err)
	}

	if err := repo.CompleteAudit(ctx, denied.ID, staff.OutcomeSucceeded, "OK", base.Add(time.Minute)); !errors.Is(err, staff.ErrAuditEntryNotFound) {
		t.Fatalf("completing a denial: err = %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM audit_entries WHERE id = $1`, allowed.ID); err == nil {
		t.Fatal("deleting an audit entry must fail")
	}

	if _, err := pool.Exec(ctx, `UPDATE audit_entries SET target_id = 'x' WHERE id = $1`, denied.ID); err == nil {
		t.Fatal("rewriting an audit entry must fail")
	}

	all, err := repo.ListAudit(ctx, staff.AuditQuery{Limit: 10})
	if err != nil || len(all) != 2 || all[0].ID != denied.ID {
		t.Fatalf("list: %d entries, %v", len(all), err)
	}

	if all[1].Outcome != staff.OutcomeSucceeded || all[1].OutcomeCode != "OK" || all[1].CompletedAt == nil {
		t.Fatalf("completed entry: %+v", all[1])
	}

	if all[0].ActorStaffID != "" || all[0].Outcome != "" {
		t.Fatalf("denied entry: %+v", all[0])
	}

	byActor, _ := repo.ListAudit(ctx, staff.AuditQuery{ActorStaffID: actor.ID, Limit: 10})
	byTarget, _ := repo.ListAudit(ctx, staff.AuditQuery{TargetID: "driver-1", Limit: 10})
	byPermission, _ := repo.ListAudit(ctx, staff.AuditQuery{Permission: staff.PermissionZonesManage, Limit: 10})
	after := base.Add(500 * time.Millisecond)
	byTime, _ := repo.ListAudit(ctx, staff.AuditQuery{OccurredAfter: &after, Limit: 10})
	paged, _ := repo.ListAudit(ctx, staff.AuditQuery{AfterID: denied.ID, Limit: 10})

	for name, got := range map[string][]staff.AuditEntry{
		"actor": byActor, "target": byTarget, "permission": byPermission, "time": byTime, "cursor": paged,
	} {
		if len(got) != 1 {
			t.Errorf("filter %s: %d entries, want 1", name, len(got))
		}
	}
}
