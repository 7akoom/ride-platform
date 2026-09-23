package staff

import (
	"context"
	"errors"
	"testing"
	"time"
)

const (
	ownerIdentity      = "a0000000-0000-4000-8000-000000000001"
	operatorIdentity   = "a0000000-0000-4000-8000-000000000002"
	outsiderIdentity   = "a0000000-0000-4000-8000-000000000003"
	suspendedIdentity  = "a0000000-0000-4000-8000-000000000004"
	ownerStaffID       = "b0000000-0000-4000-8000-000000000001"
	operatorStaffID    = "b0000000-0000-4000-8000-000000000002"
	suspendedStaffID   = "b0000000-0000-4000-8000-000000000004"
	secondOwnerStaffID = "b0000000-0000-4000-8000-000000000005"
	testMethod         = "/ride.driver.v1.DriverService/ApproveDriver"
)

var testNow = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

type fixture struct {
	repo     *fakeRepository
	service  *Service
	owner    Member
	operator Member
}

func newFixture(t *testing.T, identities IdentityReader) fixture {
	t.Helper()

	repo := newFakeRepository()

	if identities == nil {
		identities = fakeIdentities{}
	}

	service := NewService(repo, identities, &sequentialIDs{}, fixedClock{now: testNow})

	activated := testNow
	owner := repo.put(Member{
		ID: ownerStaffID, IdentityID: ownerIdentity, Email: "owner@example.com", DisplayName: "Owner",
		Status: StatusActive, Roles: []Role{repo.roles[ownerRoleID]}, ActivatedAt: &activated,
	})
	operator := repo.put(Member{
		ID: operatorStaffID, IdentityID: operatorIdentity, Email: "ops@example.com", DisplayName: "Ops",
		Status: StatusActive, Roles: []Role{repo.roles[operationsRoleID]}, ActivatedAt: &activated,
	})
	repo.put(Member{
		ID: suspendedStaffID, IdentityID: suspendedIdentity, Email: "gone@example.com", DisplayName: "Gone",
		Status: StatusSuspended, Roles: []Role{repo.roles[operationsRoleID]}, ActivatedAt: &activated,
	})

	return fixture{repo: repo, service: service, owner: owner, operator: operator}
}

func TestAuthorizeRecordsEveryAttemptAndDecidesByPermission(t *testing.T) {
	cases := []struct {
		name       string
		identity   string
		permission string
		allowed    bool
		actorStaff string
	}{
		{"operator with the permission", operatorIdentity, PermissionDriversReview, true, operatorStaffID},
		{"operator without the permission", operatorIdentity, PermissionStaffManage, false, operatorStaffID},
		{"owner holds everything", ownerIdentity, PermissionStaffManage, true, ownerStaffID},
		{"not staff at all", outsiderIdentity, PermissionDriversReview, false, ""},
		{"suspended staff", suspendedIdentity, PermissionDriversReview, false, suspendedStaffID},
		{"unknown permission, even for the owner", ownerIdentity, "rockets.launch", false, ownerStaffID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, nil)

			result, err := f.service.Authorize(context.Background(), AuthorizeInput{
				IdentityID: tc.identity, Permission: tc.permission, Method: testMethod, TargetID: "driver-1",
			})
			if err != nil {
				t.Fatalf("Authorize: %v", err)
			}

			if result.Allowed != tc.allowed {
				t.Fatalf("allowed = %v, want %v", result.Allowed, tc.allowed)
			}

			if len(f.repo.audit) != 1 {
				t.Fatalf("recorded %d audit entries, want 1", len(f.repo.audit))
			}

			entry := f.repo.audit[0]
			if entry.ID != result.AuditEntryID || entry.ActorIdentityID != tc.identity ||
				entry.ActorStaffID != tc.actorStaff || entry.TargetID != "driver-1" || entry.Method != testMethod {
				t.Fatalf("unexpected audit entry %+v", entry)
			}

			wantDecision, wantOutcome := DecisionDenied, Outcome("")
			if tc.allowed {
				wantDecision, wantOutcome = DecisionAllowed, OutcomePending
			}

			if entry.Decision != wantDecision || entry.Outcome != wantOutcome {
				t.Fatalf("decision/outcome = %s/%s, want %s/%s", entry.Decision, entry.Outcome, wantDecision, wantOutcome)
			}

			if tc.allowed && result.Actor.Member.ID != tc.actorStaff {
				t.Fatalf("actor = %q, want %q", result.Actor.Member.ID, tc.actorStaff)
			}
		})
	}
}

func TestAuthorizeRejectsMalformedRequests(t *testing.T) {
	f := newFixture(t, nil)

	for _, input := range []AuthorizeInput{
		{IdentityID: "not-a-uuid", Permission: PermissionDriversRead, Method: testMethod},
		{IdentityID: ownerIdentity, Permission: "", Method: testMethod},
		{IdentityID: ownerIdentity, Permission: PermissionDriversRead, Method: ""},
	} {
		if _, err := f.service.Authorize(context.Background(), input); !errors.Is(err, ErrInvalidAuditRequest) {
			t.Errorf("%+v: err = %v, want ErrInvalidAuditRequest", input, err)
		}
	}

	if len(f.repo.audit) != 0 {
		t.Fatalf("malformed requests must not be recorded, got %d entries", len(f.repo.audit))
	}
}

func TestCompleteActionRecordsTheOutcomeOnce(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()

	ok, _ := f.service.Authorize(ctx, AuthorizeInput{IdentityID: operatorIdentity, Permission: PermissionDriversReview, Method: testMethod})
	failed, _ := f.service.Authorize(ctx, AuthorizeInput{IdentityID: operatorIdentity, Permission: PermissionDriversReview, Method: testMethod})

	if err := f.service.CompleteAction(ctx, ok.AuditEntryID, "OK"); err != nil {
		t.Fatalf("complete OK: %v", err)
	}

	if err := f.service.CompleteAction(ctx, failed.AuditEntryID, "NotFound"); err != nil {
		t.Fatalf("complete NotFound: %v", err)
	}

	if f.repo.audit[0].Outcome != OutcomeSucceeded || f.repo.audit[1].Outcome != OutcomeFailed {
		t.Fatalf("outcomes = %s, %s", f.repo.audit[0].Outcome, f.repo.audit[1].Outcome)
	}

	if err := f.service.CompleteAction(ctx, ok.AuditEntryID, "OK"); !errors.Is(err, ErrAuditEntryNotFound) {
		t.Fatalf("completing twice: err = %v", err)
	}

	if err := f.service.CompleteAction(ctx, ok.AuditEntryID, "Nope"); !errors.Is(err, ErrInvalidOutcomeCode) {
		t.Fatalf("unknown code: err = %v", err)
	}
}

func TestBootstrapInvitesTheFirstOwnerOnlyWhenThereIsNoStaff(t *testing.T) {
	repo := newFakeRepository()
	service := NewService(repo, fakeIdentities{}, &sequentialIDs{}, fixedClock{now: testNow})
	ctx := context.Background()

	if invited, err := service.Bootstrap(ctx, ""); invited || err != nil {
		t.Fatalf("blank email: invited=%v err=%v", invited, err)
	}

	if _, err := service.Bootstrap(ctx, "not an email"); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("bad email: err = %v", err)
	}

	invited, err := service.Bootstrap(ctx, "  Boss@Example.COM ")
	if err != nil || !invited {
		t.Fatalf("first run: invited=%v err=%v", invited, err)
	}

	members, _ := repo.ListMembers(ctx, MemberQuery{Limit: 10})
	if len(members) != 1 || members[0].Email != "boss@example.com" || !members[0].IsOwner() || members[0].Status != StatusInvited {
		t.Fatalf("unexpected first owner %+v", members)
	}

	if invited, err := service.Bootstrap(ctx, "other@example.com"); invited || err != nil {
		t.Fatalf("second run: invited=%v err=%v", invited, err)
	}
}

func TestAcceptInviteBindsTheVerifiedEmail(t *testing.T) {
	const newIdentity = "a0000000-0000-4000-8000-0000000000aa"

	identities := fakeIdentities{me: VerifiedIdentity{IdentityID: newIdentity, Active: true, Emails: []string{"New.Person@Example.com"}}}
	f := newFixture(t, identities)
	ctx := context.Background()

	invited, err := f.service.Invite(ctx, Actor{Member: f.owner}, InviteInput{
		Email: "new.person@example.com", DisplayName: "New Person", RoleIDs: []string{operationsRoleID},
	})
	if err != nil {
		t.Fatalf("invite: %v", err)
	}

	member, permissions, err := f.service.AcceptInvite(ctx, newIdentity, "token")
	if err != nil {
		t.Fatalf("accept: %v", err)
	}

	if member.ID != invited.ID || member.Status != StatusActive || member.IdentityID != newIdentity {
		t.Fatalf("unexpected member %+v", member)
	}

	if len(permissions) != 3 {
		t.Fatalf("permissions = %v", permissions)
	}

	if _, _, err := f.service.AcceptInvite(ctx, newIdentity, "token"); !errors.Is(err, ErrIdentityAlreadyStaff) {
		t.Fatalf("accepting twice: err = %v", err)
	}
}

func TestAcceptInviteRefusals(t *testing.T) {
	const caller = "a0000000-0000-4000-8000-0000000000bb"

	cases := []struct {
		name string
		me   VerifiedIdentity
		err  error
		want error
	}{
		{"no invitation for the verified email", VerifiedIdentity{IdentityID: caller, Active: true, Emails: []string{"nobody@example.com"}}, nil, ErrNoInvitation},
		{"no verified email at all", VerifiedIdentity{IdentityID: caller, Active: true}, nil, ErrNoInvitation},
		{"identity says it is someone else", VerifiedIdentity{IdentityID: ownerIdentity, Active: true, Emails: []string{"invited@example.com"}}, nil, ErrNotStaff},
		{"identity not active", VerifiedIdentity{IdentityID: caller, Active: false, Emails: []string{"invited@example.com"}}, nil, ErrIdentityNotActive},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, fakeIdentities{me: tc.me, err: tc.err})

			if _, err := f.service.Invite(context.Background(), Actor{Member: f.owner}, InviteInput{
				Email: "invited@example.com", DisplayName: "Invited", RoleIDs: []string{operationsRoleID},
			}); err != nil {
				t.Fatalf("invite: %v", err)
			}

			if _, _, err := f.service.AcceptInvite(context.Background(), caller, "token"); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestInviteRefusesEscalation(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()

	customRole, err := f.service.CreateRole(ctx, Actor{Member: f.owner}, RoleInput{
		Name: "Staff admins", Permissions: []string{PermissionStaffManage, PermissionStaffRead},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	f.operator.Roles = append(f.operator.Roles, Role{ID: "x", Name: "Inviter", Permissions: []string{PermissionStaffManage}})
	f.repo.put(f.operator)

	cases := []struct {
		name  string
		roles []string
		want  error
	}{
		{"the owner role, by a non-owner", []string{ownerRoleID}, ErrOwnerRoleRequired},
		{"a role with a permission the inviter lacks", []string{customRole.ID}, ErrPermissionEscalation},
		{"no role", nil, ErrRolesRequired},
		{"an unknown role", []string{"c0000000-0000-4000-8000-000000000009"}, ErrRoleNotFound},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.service.Invite(ctx, Actor{Member: f.operator}, InviteInput{
				Email: "x@example.com", DisplayName: "X", RoleIDs: tc.roles,
			})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	if _, err := f.service.Invite(ctx, Actor{Member: f.operator}, InviteInput{
		Email: "ops2@example.com", DisplayName: "Ops Two", RoleIDs: []string{operationsRoleID},
	}); err != nil {
		t.Fatalf("an inviter may hand out a role within their own permissions: %v", err)
	}
}

func TestMemberActionsProtectSelfAndOwners(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()

	activated := testNow
	f.repo.put(Member{
		ID: secondOwnerStaffID, IdentityID: "a0000000-0000-4000-8000-000000000005", Email: "owner2@example.com",
		DisplayName: "Owner Two", Status: StatusActive, Roles: []Role{f.repo.roles[ownerRoleID]}, ActivatedAt: &activated,
	})

	manager := f.operator
	manager.Roles = append(manager.Roles, Role{ID: "m", Name: "Managers", Permissions: []string{PermissionStaffManage}})

	if _, err := f.service.Suspend(ctx, Actor{Member: f.owner}, ownerStaffID); !errors.Is(err, ErrSelfAction) {
		t.Fatalf("suspending yourself: err = %v", err)
	}

	if _, err := f.service.SetRoles(ctx, Actor{Member: f.owner}, ownerStaffID, []string{operationsRoleID}); !errors.Is(err, ErrSelfAction) {
		t.Fatalf("changing your own roles: err = %v", err)
	}

	if _, err := f.service.Suspend(ctx, Actor{Member: manager}, secondOwnerStaffID); !errors.Is(err, ErrOwnerRoleRequired) {
		t.Fatalf("a non-owner suspending an owner: err = %v", err)
	}

	if _, err := f.service.Suspend(ctx, Actor{Member: f.owner}, secondOwnerStaffID); err != nil {
		t.Fatalf("an owner suspending another owner: %v", err)
	}

	if _, err := f.service.Suspend(ctx, Actor{Member: f.owner}, secondOwnerStaffID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("suspending twice: err = %v", err)
	}

	if _, err := f.service.Reactivate(ctx, Actor{Member: f.owner}, secondOwnerStaffID); err != nil {
		t.Fatalf("reactivate: %v", err)
	}

	if _, err := f.service.RevokeInvite(ctx, Actor{Member: f.owner}, secondOwnerStaffID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("revoking an active member: err = %v", err)
	}

	if _, err := f.service.Suspend(ctx, Actor{}, operatorStaffID); !errors.Is(err, ErrStaffNotAuthenticated) {
		t.Fatalf("no actor: err = %v", err)
	}
}

func TestSetRolesRefusesRemovingPermissionsTheActorLacks(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()

	finance, err := f.service.CreateRole(ctx, Actor{Member: f.owner}, RoleInput{Name: "Audit readers", Permissions: []string{PermissionAuditRead}})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	target := f.repo.put(Member{
		ID: "b0000000-0000-4000-8000-000000000009", IdentityID: "a0000000-0000-4000-8000-000000000009",
		Email: "t@example.com", DisplayName: "T", Status: StatusActive, Roles: []Role{finance},
	})

	manager := f.operator
	manager.Roles = append(manager.Roles, Role{ID: "m", Name: "Managers", Permissions: []string{PermissionStaffManage}})

	if _, err := f.service.SetRoles(ctx, Actor{Member: manager}, target.ID, []string{operationsRoleID}); !errors.Is(err, ErrPermissionEscalation) {
		t.Fatalf("removing audit.read without holding it: err = %v", err)
	}

	updated, err := f.service.SetRoles(ctx, Actor{Member: f.owner}, target.ID, []string{operationsRoleID, finance.ID})
	if err != nil || len(updated.Roles) != 2 {
		t.Fatalf("owner sets roles: %+v, %v", updated.Roles, err)
	}
}

func TestRolesRules(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()

	if _, err := f.service.CreateRole(ctx, Actor{Member: f.operator}, RoleInput{Name: "Auditors", Permissions: []string{PermissionAuditRead}}); !errors.Is(err, ErrPermissionEscalation) {
		t.Fatalf("granting a permission you lack: err = %v", err)
	}

	if _, err := f.service.CreateRole(ctx, Actor{Member: f.owner}, RoleInput{Name: "Bad", Permissions: []string{"rockets.launch"}}); !errors.Is(err, ErrUnknownPermission) {
		t.Fatalf("unknown permission: err = %v", err)
	}

	if _, err := f.service.CreateRole(ctx, Actor{Member: f.owner}, RoleInput{Name: "  ", Permissions: nil}); !errors.Is(err, ErrRoleNameRequired) {
		t.Fatalf("blank name: err = %v", err)
	}

	if _, err := f.service.UpdateRole(ctx, Actor{Member: f.owner}, RoleInput{ID: operationsRoleID, Name: "Ops"}); !errors.Is(err, ErrSystemRole) {
		t.Fatalf("changing a system role: err = %v", err)
	}

	if err := f.service.DeleteRole(ctx, Actor{Member: f.owner}, ownerRoleID); !errors.Is(err, ErrSystemRole) {
		t.Fatalf("deleting a system role: err = %v", err)
	}

	role, err := f.service.CreateRole(ctx, Actor{Member: f.owner}, RoleInput{
		Name: "Reviewers", Permissions: []string{PermissionDriversRead, PermissionDriversRead, PermissionDriversReview},
	})
	if err != nil {
		t.Fatalf("create role: %v", err)
	}

	if len(role.Permissions) != 2 {
		t.Fatalf("duplicates must be removed: %v", role.Permissions)
	}

	if _, err := f.service.UpdateRole(ctx, Actor{Member: f.operator}, RoleInput{ID: role.ID, Name: "Reviewers", Permissions: []string{PermissionDriversRead}}); err != nil {
		t.Fatalf("removing a permission you hold: %v", err)
	}

	if err := f.service.DeleteRole(ctx, Actor{Member: f.owner}, role.ID); err != nil {
		t.Fatalf("delete role: %v", err)
	}
}

func TestOwnerRoleGrantsPermissionsAddedLater(t *testing.T) {
	owner := Role{Key: RoleKeyOwner}

	if len(owner.Grants()) != len(AllPermissionKeys()) {
		t.Fatalf("owner grants %v", owner.Grants())
	}

	member := Member{Status: StatusSuspended, Roles: []Role{owner}}
	if len(member.EffectivePermissions()) != 0 {
		t.Fatal("a suspended owner must not hold any permission")
	}
}

func TestMemberPagingTokens(t *testing.T) {
	f := newFixture(t, nil)
	ctx := context.Background()

	first, err := f.service.ListMembers(ctx, MemberListQuery{PageSize: 2})
	if err != nil || len(first.Members) != 2 || first.NextPageToken == "" {
		t.Fatalf("first page: %d members, token %q, err %v", len(first.Members), first.NextPageToken, err)
	}

	second, err := f.service.ListMembers(ctx, MemberListQuery{PageSize: 2, PageToken: first.NextPageToken})
	if err != nil || len(second.Members) != 1 || second.NextPageToken != "" {
		t.Fatalf("second page: %d members, token %q, err %v", len(second.Members), second.NextPageToken, err)
	}

	if _, err := f.service.ListMembers(ctx, MemberListQuery{PageToken: "garbage"}); !errors.Is(err, ErrInvalidPageToken) {
		t.Fatalf("garbage token: err = %v", err)
	}

	if _, err := f.service.ListMembers(ctx, MemberListQuery{PageToken: encodePageToken(auditTokenPrefix, ownerStaffID)}); !errors.Is(err, ErrInvalidPageToken) {
		t.Fatalf("an audit token on the member list: err = %v", err)
	}

	if _, err := f.service.ListMembers(ctx, MemberListQuery{PageSize: -1}); !errors.Is(err, ErrInvalidPageSize) {
		t.Fatalf("negative page size: err = %v", err)
	}
}

func TestNormalizeEmail(t *testing.T) {
	for raw, want := range map[string]string{
		"  A.B@Example.COM ": "a.b@example.com",
		"x@sub.example.org":  "x@sub.example.org",
	} {
		if got, err := NormalizeEmail(raw); err != nil || got != want {
			t.Errorf("NormalizeEmail(%q) = %q, %v", raw, got, err)
		}
	}

	for _, bad := range []string{"", "no-at", "a@b", "a b@example.com", "Name <a@example.com>", "a@.example.com", "a@example..com"} {
		if _, err := NormalizeEmail(bad); !errors.Is(err, ErrInvalidEmail) {
			t.Errorf("NormalizeEmail(%q) accepted", bad)
		}
	}
}
