package staff

import "sort"

// Permission keys are stable strings: services name them in their method
// tables, and roles store them. Add a key here (and grant it to the system
// roles in a migration) in the same change that starts enforcing it.
const (
	PermissionStaffRead     = "staff.read"
	PermissionStaffManage   = "staff.manage"
	PermissionRolesManage   = "roles.manage"
	PermissionAuditRead     = "audit.read"
	PermissionDriversRead   = "drivers.read"
	PermissionDriversReview = "drivers.approve"
	PermissionZonesManage   = "zones.manage"
)

// RoleKeyOwner is the system role that always holds every permission,
// including ones added after the role was created.
const RoleKeyOwner = "owner"

var permissionCatalog = map[string]string{
	PermissionStaffRead:     "See staff members, roles and permissions",
	PermissionStaffManage:   "Invite, suspend and reactivate staff, and change their roles",
	PermissionRolesManage:   "Create, change and delete custom roles",
	PermissionAuditRead:     "Read the audit log of staff actions",
	PermissionDriversRead:   "See drivers and the review queue",
	PermissionDriversReview: "Approve or reject drivers",
	PermissionZonesManage:   "Create and change service zones",
}

// PermissionInfo describes one grantable permission.
type PermissionInfo struct {
	Key         string
	Description string
}

// Permissions returns the catalog sorted by key.
func Permissions() []PermissionInfo {
	keys := AllPermissionKeys()
	out := make([]PermissionInfo, 0, len(keys))

	for _, key := range keys {
		out = append(out, PermissionInfo{Key: key, Description: permissionCatalog[key]})
	}

	return out
}

// AllPermissionKeys returns every permission key, sorted.
func AllPermissionKeys() []string {
	keys := make([]string, 0, len(permissionCatalog))
	for key := range permissionCatalog {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

// IsKnownPermission reports whether key is in the catalog.
func IsKnownPermission(key string) bool {
	_, ok := permissionCatalog[key]

	return ok
}
