package clients

import (
	"context"
	"errors"
	"log/slog"
	"time"

	staffv1 "github.com/7akoom/ride-platform/gen/go/ride/staff/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

const staffCallTimeout = 3 * time.Second

// StaffAuthorizer asks staff-service, as a service (internal token), whether a
// staff member may use a permission; staff-service records every attempt.
type StaffAuthorizer struct {
	staff  staffv1.StaffServiceClient
	logger *slog.Logger
}

func NewStaffAuthorizer(conn grpc.ClientConnInterface, logger *slog.Logger) *StaffAuthorizer {
	if conn == nil {
		panic("staff-service connection is required")
	}

	if logger == nil {
		panic("logger is required")
	}

	return &StaffAuthorizer{staff: staffv1.NewStaffServiceClient(conn), logger: logger}
}

func (a *StaffAuthorizer) Authorize(
	ctx context.Context,
	identityID, permission, method, targetID string,
) (bool, string, error) {
	ctx, cancel := context.WithTimeout(ctx, staffCallTimeout)
	defer cancel()

	response, err := a.staff.AuthorizeStaffAction(ctx, &staffv1.AuthorizeStaffActionRequest{
		IdentityId: identityID,
		Permission: permission,
		Method:     method,
		TargetId:   targetID,
	})
	if err != nil {
		a.logger.Error("staff-service could not authorize a staff action", "method", method, "error", err)

		return false, "", err
	}

	if response.GetAllowed() && response.GetAuditEntryId() == "" {
		return false, "", errors.New("staff-service allowed an action without recording it")
	}

	return response.GetAllowed(), response.GetAuditEntryId(), nil
}

// Complete records how an allowed action ended. The action already happened,
// so a failure is only logged.
func (a *StaffAuthorizer) Complete(ctx context.Context, auditEntryID string, code codes.Code) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), staffCallTimeout)
	defer cancel()

	if _, err := a.staff.CompleteStaffAction(ctx, &staffv1.CompleteStaffActionRequest{
		AuditEntryId: auditEntryID,
		OutcomeCode:  code.String(),
	}); err != nil {
		a.logger.Warn("failed to record the outcome of a staff action", "audit_entry_id", auditEntryID, "error", err)
	}
}
