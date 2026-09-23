package staff

import (
	"context"
	"fmt"
	"strings"
)

// RoleInput creates or changes a custom role.
type RoleInput struct {
	ID          string
	Name        string
	Description string
	Permissions []string
}

func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	return s.repository.ListRoles(ctx)
}

func (s *Service) CreateRole(ctx context.Context, actor Actor, input RoleInput) (Role, error) {
	name, err := normalizeRoleName(input.Name)
	if err != nil {
		return Role{}, err
	}

	description, err := normalizeDescription(input.Description)
	if err != nil {
		return Role{}, err
	}

	permissions, err := normalizePermissions(input.Permissions)
	if err != nil {
		return Role{}, err
	}

	if err := mayChangePermissions(actor, permissions); err != nil {
		return Role{}, err
	}

	return s.repository.CreateRole(ctx, Role{
		ID:          s.idGenerator.NewID(),
		Name:        name,
		Description: description,
		Permissions: permissions,
	})
}

func (s *Service) UpdateRole(ctx context.Context, actor Actor, input RoleInput) (Role, error) {
	id := strings.TrimSpace(input.ID)
	if !looksLikeUUID(id) {
		return Role{}, ErrInvalidID
	}

	current, err := s.repository.FindRoleByID(ctx, id)
	if err != nil {
		return Role{}, err
	}

	if current.System {
		return Role{}, ErrSystemRole
	}

	name, err := normalizeRoleName(input.Name)
	if err != nil {
		return Role{}, err
	}

	description, err := normalizeDescription(input.Description)
	if err != nil {
		return Role{}, err
	}

	permissions, err := normalizePermissions(input.Permissions)
	if err != nil {
		return Role{}, err
	}

	if err := mayChangePermissions(actor, symmetricDifference(current.Permissions, permissions)); err != nil {
		return Role{}, err
	}

	current.Name = name
	current.Description = description
	current.Permissions = permissions

	updated, err := s.repository.UpdateRole(ctx, current)
	if err != nil {
		return Role{}, fmt.Errorf("update role: %w", err)
	}

	return updated, nil
}

func (s *Service) DeleteRole(ctx context.Context, actor Actor, id string) error {
	if actor.Member.ID == "" {
		return ErrStaffNotAuthenticated
	}

	id = strings.TrimSpace(id)
	if !looksLikeUUID(id) {
		return ErrInvalidID
	}

	current, err := s.repository.FindRoleByID(ctx, id)
	if err != nil {
		return err
	}

	if current.System {
		return ErrSystemRole
	}

	if err := mayChangePermissions(actor, current.Permissions); err != nil {
		return err
	}

	return s.repository.DeleteRole(ctx, id)
}

func mayChangePermissions(actor Actor, permissions []string) error {
	if actor.Member.ID == "" {
		return ErrStaffNotAuthenticated
	}

	for _, permission := range permissions {
		if !actor.Member.Has(permission) {
			return ErrPermissionEscalation
		}
	}

	return nil
}

func symmetricDifference(a []string, b []string) []string {
	inA := map[string]struct{}{}
	for _, value := range a {
		inA[value] = struct{}{}
	}

	inB := map[string]struct{}{}
	for _, value := range b {
		inB[value] = struct{}{}
	}

	var out []string

	for value := range inA {
		if _, ok := inB[value]; !ok {
			out = append(out, value)
		}
	}

	for value := range inB {
		if _, ok := inA[value]; !ok {
			out = append(out, value)
		}
	}

	return out
}
