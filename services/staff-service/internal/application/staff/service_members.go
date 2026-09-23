package staff

import (
	"context"
	"fmt"
	"strings"
)

// InviteInput is a new invitation.
type InviteInput struct {
	Email       string
	DisplayName string
	RoleIDs     []string
}

// MemberListQuery asks for one page of staff members.
type MemberListQuery struct {
	Status    Status
	PageSize  int
	PageToken string
}

// MemberPage is one page of staff members, newest first.
type MemberPage struct {
	Members       []Member
	NextPageToken string
}

func (s *Service) GetMember(ctx context.Context, id string) (Member, error) {
	id = strings.TrimSpace(id)
	if !looksLikeUUID(id) {
		return Member{}, ErrInvalidID
	}

	return s.repository.FindMemberByID(ctx, id)
}

func (s *Service) ListMembers(ctx context.Context, query MemberListQuery) (MemberPage, error) {
	if query.Status != "" && !query.Status.Valid() {
		return MemberPage{}, ErrInvalidStatus
	}

	size, err := pageSize(query.PageSize)
	if err != nil {
		return MemberPage{}, err
	}

	after := ""
	if query.PageToken != "" {
		if after, err = decodePageToken(memberTokenPrefix, query.PageToken); err != nil {
			return MemberPage{}, err
		}
	}

	members, err := s.repository.ListMembers(ctx, MemberQuery{Status: query.Status, AfterID: after, Limit: size + 1})
	if err != nil {
		return MemberPage{}, fmt.Errorf("list staff members: %w", err)
	}

	page := MemberPage{Members: members}

	if len(members) > size {
		page.Members = members[:size]
		page.NextPageToken = encodePageToken(memberTokenPrefix, members[size-1].ID)
	}

	return page, nil
}

func (s *Service) Invite(ctx context.Context, actor Actor, input InviteInput) (Member, error) {
	email, err := NormalizeEmail(input.Email)
	if err != nil {
		return Member{}, err
	}

	name, err := normalizeDisplayName(input.DisplayName)
	if err != nil {
		return Member{}, err
	}

	roles, err := s.rolesFor(ctx, input.RoleIDs)
	if err != nil {
		return Member{}, err
	}

	if err := s.mayGrant(actor, roles); err != nil {
		return Member{}, err
	}

	return s.repository.CreateMember(ctx, NewMember{
		ID:               s.idGenerator.NewID(),
		Email:            email,
		DisplayName:      name,
		RoleIDs:          roleIDs(roles),
		InvitedByStaffID: actor.Member.ID,
		InvitedAt:        s.now(),
	})
}

func (s *Service) SetRoles(ctx context.Context, actor Actor, id string, requested []string) (Member, error) {
	target, err := s.targetFor(ctx, actor, id)
	if err != nil {
		return Member{}, err
	}

	if target.Status == StatusRevoked {
		return Member{}, ErrInvalidTransition
	}

	roles, err := s.rolesFor(ctx, requested)
	if err != nil {
		return Member{}, err
	}

	added, removed := roleDifference(target.Roles, roles)

	if err := s.mayGrant(actor, added); err != nil {
		return Member{}, err
	}

	if err := s.mayGrant(actor, removed); err != nil {
		return Member{}, err
	}

	return s.repository.SetMemberRoles(ctx, target.ID, roleIDs(roles))
}

func (s *Service) Suspend(ctx context.Context, actor Actor, id string) (Member, error) {
	target, err := s.targetFor(ctx, actor, id)
	if err != nil {
		return Member{}, err
	}

	if target.Status != StatusActive {
		return Member{}, ErrInvalidTransition
	}

	return s.repository.SetMemberStatus(ctx, target.ID, StatusActive, StatusSuspended)
}

func (s *Service) Reactivate(ctx context.Context, actor Actor, id string) (Member, error) {
	target, err := s.targetFor(ctx, actor, id)
	if err != nil {
		return Member{}, err
	}

	if target.Status != StatusSuspended {
		return Member{}, ErrInvalidTransition
	}

	return s.repository.SetMemberStatus(ctx, target.ID, StatusSuspended, StatusActive)
}

func (s *Service) RevokeInvite(ctx context.Context, actor Actor, id string) (Member, error) {
	target, err := s.targetFor(ctx, actor, id)
	if err != nil {
		return Member{}, err
	}

	if target.Status != StatusInvited {
		return Member{}, ErrInvalidTransition
	}

	return s.repository.SetMemberStatus(ctx, target.ID, StatusInvited, StatusRevoked)
}

// targetFor loads the member an admin action is about and applies the rules
// every such action shares: nobody acts on themselves, only owners act on owners.
func (s *Service) targetFor(ctx context.Context, actor Actor, id string) (Member, error) {
	if actor.Member.ID == "" {
		return Member{}, ErrStaffNotAuthenticated
	}

	id = strings.TrimSpace(id)
	if !looksLikeUUID(id) {
		return Member{}, ErrInvalidID
	}

	if id == actor.Member.ID {
		return Member{}, ErrSelfAction
	}

	target, err := s.repository.FindMemberByID(ctx, id)
	if err != nil {
		return Member{}, err
	}

	if target.IsOwner() && !actor.Member.IsOwner() {
		return Member{}, ErrOwnerRoleRequired
	}

	return target, nil
}

// rolesFor loads the requested roles; every id must exist and at least one is needed.
func (s *Service) rolesFor(ctx context.Context, requested []string) ([]Role, error) {
	ids := normalizeIDs(requested)
	if len(ids) == 0 {
		return nil, ErrRolesRequired
	}

	for _, id := range ids {
		if !looksLikeUUID(id) {
			return nil, ErrRoleNotFound
		}
	}

	roles, err := s.repository.FindRolesByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("find roles: %w", err)
	}

	if len(roles) != len(ids) {
		return nil, ErrRoleNotFound
	}

	return roles, nil
}

// mayGrant refuses roles that would hand out (or take away) permissions the
// actor does not hold, and the owner role to anyone but an owner.
func (s *Service) mayGrant(actor Actor, roles []Role) error {
	if actor.Member.ID == "" {
		return ErrStaffNotAuthenticated
	}

	for _, role := range roles {
		if role.Key == RoleKeyOwner && !actor.Member.IsOwner() {
			return ErrOwnerRoleRequired
		}

		for _, permission := range role.Grants() {
			if !actor.Member.Has(permission) {
				return ErrPermissionEscalation
			}
		}
	}

	return nil
}

func roleDifference(current []Role, requested []Role) (added []Role, removed []Role) {
	currentByID := map[string]Role{}
	for _, role := range current {
		currentByID[role.ID] = role
	}

	requestedByID := map[string]Role{}
	for _, role := range requested {
		requestedByID[role.ID] = role

		if _, held := currentByID[role.ID]; !held {
			added = append(added, role)
		}
	}

	for _, role := range current {
		if _, kept := requestedByID[role.ID]; !kept {
			removed = append(removed, role)
		}
	}

	return added, removed
}

func roleIDs(roles []Role) []string {
	ids := make([]string, 0, len(roles))
	for _, role := range roles {
		ids = append(ids, role.ID)
	}

	return normalizeIDs(ids)
}
