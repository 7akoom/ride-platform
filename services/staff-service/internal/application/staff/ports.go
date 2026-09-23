package staff

import (
	"context"
	"time"
)

// NewMember is an invitation to persist, with its roles.
type NewMember struct {
	ID               string
	Email            string
	DisplayName      string
	RoleIDs          []string
	InvitedByStaffID string
	InvitedAt        time.Time
}

// MemberQuery selects one page of members, newest first, after AfterID.
type MemberQuery struct {
	Status  Status
	AfterID string
	Limit   int
}

// AuditQuery selects one page of audit entries, newest first, after AfterID.
type AuditQuery struct {
	ActorStaffID   string
	Permission     string
	TargetID       string
	OccurredAfter  *time.Time
	OccurredBefore *time.Time
	AfterID        string
	Limit          int
}

// Repository is the persistence port of staff-service.
//
// ActivateMember, SetMemberStatus and SetMemberRoles must refuse (with
// ErrLastOwner, rolling back) any change that would leave no active owner.
type Repository interface {
	CountMembers(ctx context.Context) (int, error)

	CreateMember(ctx context.Context, member NewMember) (Member, error)
	FindMemberByID(ctx context.Context, id string) (Member, error)
	FindMemberByIdentityID(ctx context.Context, identityID string) (Member, error)
	FindInvitedMemberByEmails(ctx context.Context, emails []string) (Member, error)
	ListMembers(ctx context.Context, query MemberQuery) ([]Member, error)

	ActivateMember(ctx context.Context, id string, identityID string, at time.Time) (Member, error)
	SetMemberStatus(ctx context.Context, id string, from Status, to Status) (Member, error)
	SetMemberRoles(ctx context.Context, id string, roleIDs []string) (Member, error)

	FindRoleByID(ctx context.Context, id string) (Role, error)
	FindRolesByIDs(ctx context.Context, ids []string) ([]Role, error)
	FindRoleByKey(ctx context.Context, key string) (Role, error)
	ListRoles(ctx context.Context) ([]Role, error)
	CreateRole(ctx context.Context, role Role) (Role, error)
	UpdateRole(ctx context.Context, role Role) (Role, error)
	DeleteRole(ctx context.Context, id string) error

	RecordAudit(ctx context.Context, entry AuditEntry) error
	CompleteAudit(ctx context.Context, id string, outcome Outcome, code string, at time.Time) error
	ListAudit(ctx context.Context, query AuditQuery) ([]AuditEntry, error)
}

// VerifiedIdentity is what identity-service vouches for about the caller.
type VerifiedIdentity struct {
	IdentityID string
	Active     bool
	Emails     []string
}

// IdentityReader asks identity-service about the caller, with the caller's
// own access token.
type IdentityReader interface {
	Me(ctx context.Context, accessToken string) (VerifiedIdentity, error)
}

type IDGenerator interface {
	NewID() string
}

type Clock interface {
	Now() time.Time
}
