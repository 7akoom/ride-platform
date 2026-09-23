package staff

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Service holds staff-service's use cases. Transport authorizes a caller with
// Authorize, then passes the resulting Actor to the admin use cases, which
// apply the rules that depend on who acts (no escalation, owners protect
// owners, nobody acts on themselves).
type Service struct {
	repository  Repository
	identities  IdentityReader
	idGenerator IDGenerator
	clock       Clock
}

func NewService(
	repository Repository,
	identities IdentityReader,
	idGenerator IDGenerator,
	clock Clock,
) *Service {
	if repository == nil {
		panic("staff repository is required")
	}

	if identities == nil {
		panic("identity reader is required")
	}

	if idGenerator == nil {
		panic("id generator is required")
	}

	if clock == nil {
		panic("clock is required")
	}

	return &Service{
		repository:  repository,
		identities:  identities,
		idGenerator: idGenerator,
		clock:       clock,
	}
}

// AuthorizeInput is one request to perform an admin action.
type AuthorizeInput struct {
	IdentityID string
	Permission string
	Method     string
	TargetID   string
}

// Authorization is the answer to AuthorizeInput. AuditEntryID is set whenever
// the attempt was recorded; Actor only when it is allowed.
type Authorization struct {
	Allowed      bool
	AuditEntryID string
	Actor        Actor
}

// Authorize decides whether the identity may use the permission, and records
// the attempt either way before answering. An unknown permission is denied.
func (s *Service) Authorize(ctx context.Context, input AuthorizeInput) (Authorization, error) {
	identityID := strings.TrimSpace(input.IdentityID)
	permission := strings.TrimSpace(input.Permission)
	method := strings.TrimSpace(input.Method)
	target := strings.TrimSpace(input.TargetID)

	if !looksLikeUUID(identityID) || permission == "" || method == "" ||
		len(method) > maxMethodLength || len(permission) > maxMethodLength {
		return Authorization{}, ErrInvalidAuditRequest
	}

	if len(target) > maxTargetIDLength {
		target = target[:maxTargetIDLength]
	}

	member, err := s.repository.FindMemberByIdentityID(ctx, identityID)
	if err != nil && !errors.Is(err, ErrMemberNotFound) {
		return Authorization{}, fmt.Errorf("find staff member: %w", err)
	}

	allowed := err == nil && IsKnownPermission(permission) && member.Has(permission)

	entry := AuditEntry{
		ID:              s.idGenerator.NewID(),
		OccurredAt:      s.clock.Now(),
		ActorIdentityID: identityID,
		Permission:      permission,
		Method:          method,
		TargetID:        target,
		Decision:        DecisionDenied,
	}

	if err == nil {
		entry.ActorStaffID = member.ID
	}

	if allowed {
		entry.Decision = DecisionAllowed
		entry.Outcome = OutcomePending
	}

	if err := s.repository.RecordAudit(ctx, entry); err != nil {
		return Authorization{}, fmt.Errorf("record audit entry: %w", err)
	}

	result := Authorization{Allowed: allowed, AuditEntryID: entry.ID}
	if allowed {
		result.Actor = Actor{Member: member}
	}

	return result, nil
}

// grpcCodeNames are the status code names CompleteAction accepts.
var grpcCodeNames = map[string]struct{}{
	"OK": {}, "Canceled": {}, "Unknown": {}, "InvalidArgument": {}, "DeadlineExceeded": {},
	"NotFound": {}, "AlreadyExists": {}, "PermissionDenied": {}, "ResourceExhausted": {},
	"FailedPrecondition": {}, "Aborted": {}, "OutOfRange": {}, "Unimplemented": {},
	"Internal": {}, "Unavailable": {}, "DataLoss": {}, "Unauthenticated": {},
}

// CompleteAction records how an allowed action ended. Completing an entry twice,
// or one that was denied, is ErrAuditEntryNotFound.
func (s *Service) CompleteAction(ctx context.Context, auditEntryID string, code string) error {
	auditEntryID = strings.TrimSpace(auditEntryID)
	code = strings.TrimSpace(code)

	if !looksLikeUUID(auditEntryID) {
		return ErrInvalidID
	}

	if _, ok := grpcCodeNames[code]; !ok {
		return ErrInvalidOutcomeCode
	}

	outcome := OutcomeFailed
	if code == "OK" {
		outcome = OutcomeSucceeded
	}

	return s.repository.CompleteAudit(ctx, auditEntryID, outcome, code, s.clock.Now())
}

// MyProfile returns the caller's staff record and what they may do now.
func (s *Service) MyProfile(ctx context.Context, identityID string) (Member, []string, error) {
	identityID = strings.TrimSpace(identityID)
	if !looksLikeUUID(identityID) {
		return Member{}, nil, ErrNotStaff
	}

	member, err := s.repository.FindMemberByIdentityID(ctx, identityID)
	if errors.Is(err, ErrMemberNotFound) {
		return Member{}, nil, ErrNotStaff
	}

	if err != nil {
		return Member{}, nil, fmt.Errorf("find staff member: %w", err)
	}

	return member, member.EffectivePermissions(), nil
}

// AcceptInvite binds the caller to the invitation sent to one of the email
// addresses identity-service verified for them.
func (s *Service) AcceptInvite(ctx context.Context, identityID string, accessToken string) (Member, []string, error) {
	identityID = strings.TrimSpace(identityID)
	if !looksLikeUUID(identityID) {
		return Member{}, nil, ErrNotStaff
	}

	if _, err := s.repository.FindMemberByIdentityID(ctx, identityID); err == nil {
		return Member{}, nil, ErrIdentityAlreadyStaff
	} else if !errors.Is(err, ErrMemberNotFound) {
		return Member{}, nil, fmt.Errorf("find staff member: %w", err)
	}

	me, err := s.identities.Me(ctx, accessToken)
	if errors.Is(err, ErrIdentityRejected) {
		return Member{}, nil, ErrIdentityRejected
	}

	if err != nil {
		return Member{}, nil, fmt.Errorf("read the caller's identity: %w", err)
	}

	if me.IdentityID != identityID {
		return Member{}, nil, ErrNotStaff
	}

	if !me.Active {
		return Member{}, nil, ErrIdentityNotActive
	}

	emails := make([]string, 0, len(me.Emails))

	for _, raw := range me.Emails {
		if email, err := NormalizeEmail(raw); err == nil {
			emails = append(emails, email)
		}
	}

	if len(emails) == 0 {
		return Member{}, nil, ErrNoInvitation
	}

	invited, err := s.repository.FindInvitedMemberByEmails(ctx, emails)
	if errors.Is(err, ErrMemberNotFound) {
		return Member{}, nil, ErrNoInvitation
	}

	if err != nil {
		return Member{}, nil, fmt.Errorf("find invitation: %w", err)
	}

	activated, err := s.repository.ActivateMember(ctx, invited.ID, identityID, s.clock.Now())
	if err != nil {
		return Member{}, nil, err
	}

	return activated, activated.EffectivePermissions(), nil
}

// Bootstrap invites the first owner when there is no staff at all. It does
// nothing once any staff member exists, so it is safe to run on every start.
func (s *Service) Bootstrap(ctx context.Context, rawEmail string) (bool, error) {
	if strings.TrimSpace(rawEmail) == "" {
		return false, nil
	}

	email, err := NormalizeEmail(rawEmail)
	if err != nil {
		return false, err
	}

	count, err := s.repository.CountMembers(ctx)
	if err != nil {
		return false, fmt.Errorf("count staff members: %w", err)
	}

	if count > 0 {
		return false, nil
	}

	owner, err := s.repository.FindRoleByKey(ctx, RoleKeyOwner)
	if err != nil {
		return false, fmt.Errorf("find the owner role: %w", err)
	}

	_, err = s.repository.CreateMember(ctx, NewMember{
		ID:          s.idGenerator.NewID(),
		Email:       email,
		DisplayName: "Owner",
		RoleIDs:     []string{owner.ID},
		InvitedAt:   s.clock.Now(),
	})
	if errors.Is(err, ErrEmailAlreadyInvited) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("invite the first owner: %w", err)
	}

	return true, nil
}

// ListPermissions returns the permission catalog.
func (s *Service) ListPermissions() []PermissionInfo {
	return Permissions()
}

func (s *Service) now() time.Time {
	return s.clock.Now()
}
