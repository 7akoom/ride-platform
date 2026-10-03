package support

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type ActionInput struct {
	TicketID     string
	Kind         ActionKind
	Amount       string
	DriverAmount string
	Target       ActionTarget
	Reason       string
	SuspendUntil *time.Time
}

// PermissionForAction is what a staff member needs to ask for the action.
func PermissionForAction(kind ActionKind) string {
	if kind.Account() {
		return PermissionSuspend
	}

	return PermissionRefund
}

func parseAmount(value string) (decimal.Decimal, error) {
	amount, err := decimal.NewFromString(value)
	if err != nil || !amount.IsPositive() || amount.Exponent() < -2 || amount.GreaterThan(decimal.NewFromInt(1_000_000_000)) {
		return decimal.Decimal{}, fmt.Errorf("%w: an amount is a positive number with at most 2 decimals", ErrInvalidInput)
	}

	return amount, nil
}

// RequestAction asks for a refund, fee waiver, wallet credit, suspension or
// reactivation from a ticket. Money over the ticket's limit waits for
// approval; anything else is carried out at once.
func (s *Service) RequestAction(ctx context.Context, staff Staff, input ActionInput, method string) (Action, error) {
	reason, err := cleanReason(input.Reason)
	if err != nil {
		return Action{}, err
	}

	if !input.Kind.Money() && !input.Kind.Account() {
		return Action{}, fmt.Errorf("%w: unknown action", ErrInvalidInput)
	}

	ticket, err := s.staffTicket(ctx, staff, input.TicketID, method)
	if err != nil {
		return Action{}, err
	}

	now := s.clock.Now()
	action := Action{
		ID:                    s.ids.NewID(),
		TicketID:              ticket.ID,
		Kind:                  input.Kind,
		Reason:                reason,
		RequestedByStaffID:    staff.StaffID,
		RequestedByIdentityID: staff.IdentityID,
		CreatedAt:             now,
	}

	if err := s.prepareAction(ctx, ticket, input, &action, now); err != nil {
		return Action{}, err
	}

	created, err := s.repository.CreateAction(ctx, ticket.ID, func(_ Ticket, moneySoFar decimal.Decimal) (Action, Message, error) {
		action.Status = ActionProcessing
		action.ActingIdentityID = staff.IdentityID

		if action.Kind.Money() && moneySoFar.Add(*action.Amount).GreaterThan(s.config.RefundLimit) {
			action.Status = ActionPendingApproval
			action.ActingIdentityID = ""
		} else {
			lease := now.Add(actionLease)
			action.LeaseUntil = &lease
		}

		note := fmt.Sprintf("Action %s %s asked by staff %s: %s", action.Kind, describe(action), staff.StaffID, reason)
		if action.Status == ActionPendingApproval {
			note += " (waiting for approval: over the ticket's limit)"
		}

		return action, s.systemMessage(ticket.ID, note), nil
	})
	if err != nil {
		return Action{}, err
	}

	if created.Status != ActionProcessing {
		return created, nil
	}

	return s.execute(ctx, created), nil
}

func describe(action Action) string {
	switch {
	case action.Amount != nil && action.DriverAmount != nil && action.DriverAmount.IsPositive():
		return fmt.Sprintf("%s (driver pays %s)", action.Amount.StringFixed(2), action.DriverAmount.StringFixed(2))
	case action.Amount != nil:
		return action.Amount.StringFixed(2)
	case action.SuspendUntil != nil:
		return fmt.Sprintf("%s %s until %s", action.TargetType, action.TargetProfileID, action.SuspendUntil.UTC().Format(time.RFC3339))
	default:
		return fmt.Sprintf("%s %s", action.TargetType, action.TargetProfileID)
	}
}

// prepareAction checks the action fits the ticket and fills in its amount
// or target.
func (s *Service) prepareAction(ctx context.Context, ticket Ticket, input ActionInput, action *Action, now time.Time) error {
	switch input.Kind {
	case ActionRefund:
		if ticket.Audience != AudienceRider || ticket.TripID == "" {
			return fmt.Errorf("%w: a refund needs a rider's ticket about a trip", ErrActionNotAllowed)
		}

		amount, err := parseAmount(input.Amount)
		if err != nil {
			return err
		}

		driverAmount := decimal.Zero
		if input.DriverAmount != "" {
			if driverAmount, err = decimal.NewFromString(input.DriverAmount); err != nil ||
				driverAmount.IsNegative() || driverAmount.Exponent() < -2 || driverAmount.GreaterThan(amount) {
				return fmt.Errorf("%w: driver_amount is 0 to amount", ErrInvalidInput)
			}
		}

		action.Amount = &amount
		action.DriverAmount = &driverAmount

	case ActionWaiveFee:
		if ticket.Audience != AudienceRider || ticket.TripID == "" {
			return fmt.Errorf("%w: a fee waiver needs a rider's ticket about a trip", ErrActionNotAllowed)
		}

		trip, err := s.trips.GetTrip(ctx, ticket.TripID)
		if err != nil {
			return err
		}

		if trip.Status != "cancelled" {
			return fmt.Errorf("%w: only a cancelled trip has a fee to waive", ErrActionNotAllowed)
		}

		left, err := s.wallet.Refundable(ctx, ticket.TripID)
		if err != nil {
			return err
		}

		if !left.IsPositive() {
			return ErrNothingToWaive
		}

		zero := decimal.Zero
		action.Amount = &left
		action.DriverAmount = &zero

	case ActionCompensation:
		amount, err := parseAmount(input.Amount)
		if err != nil {
			return err
		}

		action.Amount = &amount

	case ActionSuspend, ActionReactivate:
		action.Target = input.Target

		switch input.Target {
		case TargetRequester:
			action.TargetType = ticket.Audience
			action.TargetProfileID = ticket.RequesterProfileID
			action.TargetIdentityID = ticket.RequesterIdentityID
		case TargetCounterpart:
			if ticket.CounterpartProfileID == "" {
				return fmt.Errorf("%w: the ticket has no other side of a trip", ErrActionNotAllowed)
			}

			action.TargetType = ticket.Audience.Other()
			action.TargetProfileID = ticket.CounterpartProfileID

			identityID, err := s.profiles.IdentityByProfile(ctx, action.TargetType, action.TargetProfileID)
			if err != nil {
				return err
			}

			action.TargetIdentityID = identityID
		default:
			return fmt.Errorf("%w: target is requester or counterpart", ErrInvalidInput)
		}

		if input.SuspendUntil != nil {
			if input.Kind != ActionSuspend {
				return fmt.Errorf("%w: suspend_until is for a suspension", ErrInvalidInput)
			}

			until := input.SuspendUntil.UTC()
			if until.Before(now.Add(minSuspension)) || until.After(now.Add(maxSuspension)) {
				return fmt.Errorf("%w: a suspension lasts 1 hour to 90 days", ErrInvalidInput)
			}

			action.SuspendUntil = &until
		}
	}

	return nil
}

// execute carries out a processing action. A definite refusal fails it; a
// service that did not answer leaves it processing for the retry worker.
func (s *Service) execute(ctx context.Context, action Action) Action {
	err := s.carryOut(ctx, action)

	if err != nil && !errors.Is(err, ErrRefused) {
		s.logger.WarnContext(ctx, "support action not carried out yet; it will be retried", "action_id", action.ID, "kind", action.Kind, "error", err)

		return action
	}

	now := s.clock.Now()
	status := ActionCompleted
	failure := ""
	note := fmt.Sprintf("Action %s %s completed", action.Kind, describe(action))

	if err != nil {
		status = ActionFailed
		failure = truncate(strings.TrimPrefix(err.Error(), ErrRefused.Error()+": "), maxFailureReasonLen)
		note = fmt.Sprintf("Action %s %s failed: %s", action.Kind, describe(action), failure)
	}

	finished, finishErr := s.repository.FinishAction(context.WithoutCancel(ctx), action.ID, status, failure, now, s.systemMessage(action.TicketID, note))
	if finishErr != nil {
		s.logger.ErrorContext(ctx, "failed to record how a support action ended; it will be retried", "action_id", action.ID, "error", finishErr)

		return action
	}

	if status == ActionCompleted {
		s.afterAction(ctx, finished)
	}

	return finished
}

func (s *Service) carryOut(ctx context.Context, action Action) error {
	key := "support-action:" + action.ID

	ticket, err := s.repository.GetTicket(ctx, action.TicketID)
	if err != nil {
		return err
	}

	switch action.Kind {
	case ActionRefund, ActionWaiveFee:
		return s.wallet.RefundTrip(ctx, action.ActingIdentityID, ticket.TripID, *action.Amount, *action.DriverAmount,
			fmt.Sprintf("Support %s: %s", TicketNumber(ticket.Number), action.Reason), key)
	case ActionCompensation:
		return s.wallet.Credit(ctx, action.ActingIdentityID, ticket.Audience, ticket.RequesterProfileID, *action.Amount,
			fmt.Sprintf("Support %s: %s", TicketNumber(ticket.Number), action.Reason), key)
	case ActionSuspend:
		return s.accounts.Suspend(ctx, action.TargetIdentityID)
	case ActionReactivate:
		return s.accounts.Reactivate(ctx, action.TargetIdentityID)
	}

	return fmt.Errorf("%w: unknown action %q", ErrRefused, action.Kind)
}

// afterAction does what follows a completed action; failures are only logged.
func (s *Service) afterAction(ctx context.Context, action Action) {
	ctx = context.WithoutCancel(ctx)

	switch action.Kind {
	case ActionSuspend:
		if action.TargetType == AudienceDriver {
			if err := s.profiles.SetDriverOffline(ctx, action.TargetProfileID); err != nil {
				s.logger.WarnContext(ctx, "suspended driver could not be taken offline", "driver_id", action.TargetProfileID, "error", err)
			}
		}
	case ActionReactivate:
		if err := s.repository.MarkReactivated(ctx, action.TargetIdentityID, s.clock.Now(), nil); err != nil {
			s.logger.WarnContext(ctx, "failed to mark suspensions lifted", "action_id", action.ID, "error", err)
		}
	}
}

func (s *Service) ListPendingActions(ctx context.Context, limit int, cursor string) ([]Action, string, error) {
	return s.repository.ListPendingActions(ctx, limit, cursor)
}

// ApproveAction carries out an action waiting for approval, as the approver.
func (s *Service) ApproveAction(ctx context.Context, staff Staff, actionID, reason string) (Action, error) {
	if !validID(actionID) {
		return Action{}, ErrActionNotFound
	}

	decided, err := s.repository.DecideAction(ctx, actionID, func(a *Action) (*Message, error) {
		if a.Status != ActionPendingApproval {
			return nil, ErrActionNotPending
		}

		if a.RequestedByStaffID == staff.StaffID {
			return nil, ErrOwnApproval
		}

		now := s.clock.Now()
		lease := now.Add(actionLease)
		a.Status = ActionProcessing
		a.DecidedByStaffID = staff.StaffID
		a.DecidedByIdentityID = staff.IdentityID
		a.DecidedAt = &now
		a.DecisionReason = truncate(reason, maxReasonLength)
		a.ActingIdentityID = staff.IdentityID
		a.LeaseUntil = &lease

		message := s.systemMessage(a.TicketID, fmt.Sprintf("Action %s %s approved by staff %s", a.Kind, describe(*a), staff.StaffID))

		return &message, nil
	})
	if err != nil {
		return Action{}, err
	}

	return s.execute(ctx, decided), nil
}

func (s *Service) RejectAction(ctx context.Context, staff Staff, actionID, reason string) (Action, error) {
	reason, err := cleanReason(reason)
	if err != nil {
		return Action{}, err
	}

	if !validID(actionID) {
		return Action{}, ErrActionNotFound
	}

	return s.repository.DecideAction(ctx, actionID, func(a *Action) (*Message, error) {
		if a.Status != ActionPendingApproval {
			return nil, ErrActionNotPending
		}

		now := s.clock.Now()
		a.Status = ActionRejected
		a.DecidedByStaffID = staff.StaffID
		a.DecidedByIdentityID = staff.IdentityID
		a.DecidedAt = &now
		a.DecisionReason = reason

		message := s.systemMessage(a.TicketID, fmt.Sprintf("Action %s %s rejected by staff %s: %s", a.Kind, describe(*a), staff.StaffID, reason))

		return &message, nil
	})
}

// RetryProcessing carries out actions left processing by a service that
// did not answer.
func (s *Service) RetryProcessing(ctx context.Context, limit int) int {
	actions, err := s.repository.ClaimProcessingActions(ctx, s.clock.Now(), actionLease, limit)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to claim processing support actions", "error", err)

		return 0
	}

	for _, action := range actions {
		s.execute(ctx, action)
	}

	return len(actions)
}

// LiftDueSuspensions reactivates accounts whose suspension time is up.
func (s *Service) LiftDueSuspensions(ctx context.Context, limit int) int {
	now := s.clock.Now()

	actions, err := s.repository.ClaimDueSuspensions(ctx, now, actionLease, limit)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to claim due suspensions", "error", err)

		return 0
	}

	for _, action := range actions {
		if err := s.accounts.Reactivate(ctx, action.TargetIdentityID); err != nil {
			s.logger.WarnContext(ctx, "failed to lift a suspension; it will be retried", "action_id", action.ID, "error", err)

			continue
		}

		message := s.systemMessage(action.TicketID, fmt.Sprintf("Suspension of %s %s lifted: its time was up", action.TargetType, action.TargetProfileID))
		if err := s.repository.MarkReactivated(ctx, action.TargetIdentityID, now, &message); err != nil {
			s.logger.WarnContext(ctx, "failed to record a lifted suspension", "action_id", action.ID, "error", err)
		}
	}

	return len(actions)
}

// RunWorkers retries processing actions and lifts due suspensions until ctx
// ends.
func (s *Service) RunWorkers(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.RetryProcessing(ctx, 20)
			s.LiftDueSuspensions(ctx, 20)
		}
	}
}
