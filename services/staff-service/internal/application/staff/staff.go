package staff

import (
	"net/mail"
	"sort"
	"strings"
	"time"
)

type Status string

const (
	StatusInvited   Status = "invited"
	StatusActive    Status = "active"
	StatusSuspended Status = "suspended"
	StatusRevoked   Status = "revoked"
)

func (s Status) Valid() bool {
	switch s {
	case StatusInvited, StatusActive, StatusSuspended, StatusRevoked:
		return true
	default:
		return false
	}
}

type Decision string

const (
	DecisionAllowed Decision = "allowed"
	DecisionDenied  Decision = "denied"
)

type Outcome string

const (
	OutcomePending   Outcome = "pending"
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
)

// Role is a named set of permissions. System roles are created by migrations
// and never change through the API; the owner role grants every permission.
type Role struct {
	ID          string
	Key         string
	Name        string
	Description string
	System      bool
	Permissions []string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Grants returns the permissions the role gives, sorted.
func (r Role) Grants() []string {
	if r.Key == RoleKeyOwner {
		return AllPermissionKeys()
	}

	out := append([]string(nil), r.Permissions...)
	sort.Strings(out)

	return out
}

// Member is one staff member, from invitation on.
type Member struct {
	ID               string
	IdentityID       string
	Email            string
	DisplayName      string
	Status           Status
	Roles            []Role
	InvitedByStaffID string
	InvitedAt        time.Time
	ActivatedAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// IsOwner reports whether the member holds the owner role, whatever their status.
func (m Member) IsOwner() bool {
	for _, role := range m.Roles {
		if role.Key == RoleKeyOwner {
			return true
		}
	}

	return false
}

// EffectivePermissions is what the member may do right now: nothing unless active.
func (m Member) EffectivePermissions() []string {
	if m.Status != StatusActive {
		return nil
	}

	set := map[string]struct{}{}

	for _, role := range m.Roles {
		for _, permission := range role.Grants() {
			set[permission] = struct{}{}
		}
	}

	out := make([]string, 0, len(set))
	for permission := range set {
		out = append(out, permission)
	}

	sort.Strings(out)

	return out
}

// Has reports whether the member may currently use permission.
func (m Member) Has(permission string) bool {
	for _, held := range m.EffectivePermissions() {
		if held == permission {
			return true
		}
	}

	return false
}

// Actor is the staff member performing an admin action.
type Actor struct {
	Member Member
}

// AuditEntry records one attempt at an admin action.
type AuditEntry struct {
	ID              string
	OccurredAt      time.Time
	ActorStaffID    string
	ActorIdentityID string
	Permission      string
	Method          string
	TargetID        string
	Decision        Decision
	Outcome         Outcome
	OutcomeCode     string
	CompletedAt     *time.Time
}

const (
	maxDisplayNameLength = 120
	maxRoleNameLength    = 80
	maxDescriptionLength = 500
	maxMethodLength      = 200
	maxTargetIDLength    = 200
)

// NormalizeEmail applies identity-service's rules, so an invitation matches the
// address identity verified.
func NormalizeEmail(raw string) (string, error) {
	email := strings.TrimSpace(raw)

	if email == "" || len(email) > 254 || strings.ContainsAny(email, "\r\n\t ") {
		return "", ErrInvalidEmail
	}

	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", ErrInvalidEmail
	}

	at := strings.LastIndexByte(email, '@')
	if at <= 0 || at == len(email)-1 {
		return "", ErrInvalidEmail
	}

	local, domain := email[:at], email[at+1:]

	if len(local) > 64 ||
		strings.HasPrefix(domain, ".") ||
		strings.HasSuffix(domain, ".") ||
		strings.Contains(domain, "..") ||
		!strings.Contains(domain, ".") {
		return "", ErrInvalidEmail
	}

	return strings.ToLower(local + "@" + domain), nil
}

func normalizeDisplayName(raw string) (string, error) {
	name := strings.TrimSpace(raw)

	if name == "" {
		return "", ErrDisplayNameRequired
	}

	if len([]rune(name)) > maxDisplayNameLength {
		return "", ErrDisplayNameTooLong
	}

	return name, nil
}

func normalizeRoleName(raw string) (string, error) {
	name := strings.TrimSpace(raw)

	if name == "" {
		return "", ErrRoleNameRequired
	}

	if len([]rune(name)) > maxRoleNameLength {
		return "", ErrRoleNameTooLong
	}

	return name, nil
}

func normalizeDescription(raw string) (string, error) {
	description := strings.TrimSpace(raw)

	if len([]rune(description)) > maxDescriptionLength {
		return "", ErrDescriptionTooLong
	}

	return description, nil
}

// normalizePermissions trims, removes duplicates and refuses unknown keys.
func normalizePermissions(raw []string) ([]string, error) {
	set := map[string]struct{}{}

	for _, permission := range raw {
		permission = strings.TrimSpace(permission)

		if !IsKnownPermission(permission) {
			return nil, ErrUnknownPermission
		}

		set[permission] = struct{}{}
	}

	out := make([]string, 0, len(set))
	for permission := range set {
		out = append(out, permission)
	}

	sort.Strings(out)

	return out, nil
}

func normalizeIDs(raw []string) []string {
	set := map[string]struct{}{}
	out := make([]string, 0, len(raw))

	for _, id := range raw {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}

		if _, seen := set[id]; seen {
			continue
		}

		set[id] = struct{}{}
		out = append(out, id)
	}

	sort.Strings(out)

	return out
}

// looksLikeUUID checks the canonical 8-4-4-4-12 form, so a malformed id never
// reaches a query as a UUID.
func looksLikeUUID(value string) bool {
	if len(value) != 36 {
		return false
	}

	for i, r := range value {
		switch i {
		case 8, 13, 18, 23:
			if r != '-' {
				return false
			}
		default:
			isDigit := r >= '0' && r <= '9'
			isHex := (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')

			if !isDigit && !isHex {
				return false
			}
		}
	}

	return true
}
