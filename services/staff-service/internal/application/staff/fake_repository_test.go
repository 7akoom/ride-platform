package staff

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// fakeRepository is an in-memory Repository for the service tests. It keeps
// only what the service relies on; owner protection is tested against Postgres.
type fakeRepository struct {
	mu      sync.Mutex
	members map[string]Member
	roles   map[string]Role
	audit   []AuditEntry
}

const (
	ownerRoleID      = "5e7a0000-0000-4000-8000-000000000001"
	operationsRoleID = "5e7a0000-0000-4000-8000-000000000002"
)

func newFakeRepository() *fakeRepository {
	return &fakeRepository{
		members: map[string]Member{},
		roles: map[string]Role{
			ownerRoleID: {ID: ownerRoleID, Key: RoleKeyOwner, Name: "Owner", System: true},
			operationsRoleID: {
				ID: operationsRoleID, Key: "operations", Name: "Operations", System: true,
				Permissions: []string{PermissionDriversRead, PermissionDriversReview, PermissionMediaRead, PermissionZonesManage},
			},
		},
	}
}

func (f *fakeRepository) put(member Member) Member {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.members[member.ID] = member

	return member
}

func (f *fakeRepository) CountMembers(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.members), nil
}

func (f *fakeRepository) CreateMember(_ context.Context, input NewMember) (Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, existing := range f.members {
		if existing.Email == input.Email && existing.Status != StatusRevoked {
			return Member{}, ErrEmailAlreadyInvited
		}
	}

	member := Member{
		ID:               input.ID,
		Email:            input.Email,
		DisplayName:      input.DisplayName,
		Status:           StatusInvited,
		InvitedByStaffID: input.InvitedByStaffID,
		InvitedAt:        input.InvitedAt,
		CreatedAt:        input.InvitedAt,
		UpdatedAt:        input.InvitedAt,
	}

	for _, id := range input.RoleIDs {
		role, ok := f.roles[id]
		if !ok {
			return Member{}, ErrRoleNotFound
		}

		member.Roles = append(member.Roles, role)
	}

	f.members[member.ID] = member

	return member, nil
}

func (f *fakeRepository) FindMemberByID(_ context.Context, id string) (Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	member, ok := f.members[id]
	if !ok {
		return Member{}, ErrMemberNotFound
	}

	return member, nil
}

func (f *fakeRepository) FindMemberByIdentityID(_ context.Context, identityID string) (Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, member := range f.members {
		if member.IdentityID == identityID {
			return member, nil
		}
	}

	return Member{}, ErrMemberNotFound
}

func (f *fakeRepository) FindInvitedMemberByEmails(_ context.Context, emails []string) (Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, member := range f.members {
		for _, email := range emails {
			if member.Status == StatusInvited && member.Email == email {
				return member, nil
			}
		}
	}

	return Member{}, ErrMemberNotFound
}

func (f *fakeRepository) ListMembers(_ context.Context, query MemberQuery) ([]Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var all []Member
	for _, member := range f.members {
		if query.Status == "" || member.Status == query.Status {
			all = append(all, member)
		}
	}

	sort.Slice(all, func(i, j int) bool { return all[i].ID > all[j].ID })

	start := 0
	if query.AfterID != "" {
		for i, member := range all {
			if member.ID == query.AfterID {
				start = i + 1
			}
		}
	}

	all = all[start:]
	if len(all) > query.Limit {
		all = all[:query.Limit]
	}

	return all, nil
}

func (f *fakeRepository) ActivateMember(_ context.Context, id string, identityID string, at time.Time) (Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	member, ok := f.members[id]
	if !ok {
		return Member{}, ErrMemberNotFound
	}

	if member.Status != StatusInvited {
		return Member{}, ErrInvalidTransition
	}

	member.Status = StatusActive
	member.IdentityID = identityID
	member.ActivatedAt = &at
	f.members[id] = member

	return member, nil
}

func (f *fakeRepository) SetMemberStatus(_ context.Context, id string, from Status, to Status) (Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	member, ok := f.members[id]
	if !ok {
		return Member{}, ErrMemberNotFound
	}

	if member.Status != from {
		return Member{}, ErrInvalidTransition
	}

	member.Status = to
	f.members[id] = member

	return member, nil
}

func (f *fakeRepository) SetMemberRoles(_ context.Context, id string, roleIDs []string) (Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	member, ok := f.members[id]
	if !ok {
		return Member{}, ErrMemberNotFound
	}

	member.Roles = nil
	for _, roleID := range roleIDs {
		member.Roles = append(member.Roles, f.roles[roleID])
	}

	f.members[id] = member

	return member, nil
}

func (f *fakeRepository) FindRoleByID(_ context.Context, id string) (Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	role, ok := f.roles[id]
	if !ok {
		return Role{}, ErrRoleNotFound
	}

	return role, nil
}

func (f *fakeRepository) FindRolesByIDs(_ context.Context, ids []string) ([]Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []Role
	for _, id := range ids {
		if role, ok := f.roles[id]; ok {
			out = append(out, role)
		}
	}

	return out, nil
}

func (f *fakeRepository) FindRoleByKey(_ context.Context, key string) (Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, role := range f.roles {
		if role.Key == key {
			return role, nil
		}
	}

	return Role{}, ErrRoleNotFound
}

func (f *fakeRepository) ListRoles(context.Context) ([]Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []Role
	for _, role := range f.roles {
		out = append(out, role)
	}

	return out, nil
}

func (f *fakeRepository) CreateRole(_ context.Context, role Role) (Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, existing := range f.roles {
		if existing.Name == role.Name {
			return Role{}, ErrRoleNameTaken
		}
	}

	f.roles[role.ID] = role

	return role, nil
}

func (f *fakeRepository) UpdateRole(_ context.Context, role Role) (Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.roles[role.ID] = role

	return role, nil
}

func (f *fakeRepository) DeleteRole(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, member := range f.members {
		for _, role := range member.Roles {
			if role.ID == id {
				return ErrRoleInUse
			}
		}
	}

	delete(f.roles, id)

	return nil
}

func (f *fakeRepository) RecordAudit(_ context.Context, entry AuditEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.audit = append(f.audit, entry)

	return nil
}

func (f *fakeRepository) CompleteAudit(_ context.Context, id string, outcome Outcome, code string, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i, entry := range f.audit {
		if entry.ID == id && entry.Outcome == OutcomePending {
			f.audit[i].Outcome = outcome
			f.audit[i].OutcomeCode = code
			f.audit[i].CompletedAt = &at

			return nil
		}
	}

	return ErrAuditEntryNotFound
}

func (f *fakeRepository) ListAudit(_ context.Context, query AuditQuery) ([]AuditEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := append([]AuditEntry(nil), f.audit...)
	if len(out) > query.Limit {
		out = out[:query.Limit]
	}

	return out, nil
}

type fakeIdentities struct {
	me  VerifiedIdentity
	err error
}

func (f fakeIdentities) Me(context.Context, string) (VerifiedIdentity, error) {
	return f.me, f.err
}

type sequentialIDs struct {
	mu   sync.Mutex
	next int
}

func (s *sequentialIDs) NewID() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.next++

	return fmt.Sprintf("00000000-0000-4000-8000-%012d", s.next)
}

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }
