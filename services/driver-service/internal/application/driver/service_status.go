package driver

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
)

const (
	maxRejectionReasonLength = 500

	defaultListPageSize = 20
	maxListPageSize     = 100

	driverPageTokenPrefix = "d1:"
)

var uuidShape = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func (s *service) ApproveDriver(
	ctx context.Context,
	driverID string,
) (Driver, error) {
	return s.changeStatus(ctx, driverID, "", StatusActive, StatusPending, StatusRejected)
}

func (s *service) RejectDriver(
	ctx context.Context,
	driverID string,
	reason string,
) (Driver, error) {
	reason = strings.TrimSpace(reason)
	if len([]rune(reason)) > maxRejectionReasonLength {
		return Driver{}, ErrRejectionReasonTooLong
	}

	return s.changeStatus(ctx, driverID, reason, StatusRejected, StatusPending)
}

func (s *service) changeStatus(
	ctx context.Context,
	driverID string,
	reason string,
	to Status,
	allowedFrom ...Status,
) (Driver, error) {
	trimmedID := strings.TrimSpace(driverID)
	if trimmedID == "" {
		return Driver{}, ErrDriverIDRequired
	}

	updated, err := s.repository.UpdateStatus(
		ctx,
		UpdateStatusInput{
			DriverID:    trimmedID,
			To:          to,
			AllowedFrom: allowedFrom,
			Reason:      reason,
		},
	)
	if err != nil {
		return Driver{}, fmt.Errorf("change driver status to %s: %w", to, err)
	}

	return updated, nil
}

// ListDrivers returns one page of drivers, newest first; a status narrows it
// (the review queue is the pending ones).
func (s *service) ListDrivers(
	ctx context.Context,
	query ListDriversQuery,
) (DriversPage, error) {
	switch query.Status {
	case "", StatusPending, StatusActive, StatusRejected, StatusSuspended:
	default:
		return DriversPage{}, ErrInvalidListQuery
	}

	size := query.PageSize

	switch {
	case size < 0:
		return DriversPage{}, ErrInvalidListQuery
	case size == 0:
		size = defaultListPageSize
	case size > maxListPageSize:
		size = maxListPageSize
	}

	after := ""
	if query.PageToken != "" {
		raw, err := base64.RawURLEncoding.DecodeString(query.PageToken)
		if err != nil {
			return DriversPage{}, ErrInvalidPageToken
		}

		id, ok := strings.CutPrefix(string(raw), driverPageTokenPrefix)
		if !ok || !uuidShape.MatchString(id) {
			return DriversPage{}, ErrInvalidPageToken
		}

		after = id
	}

	drivers, err := s.repository.List(ctx, ListQuery{Status: query.Status, AfterID: after, Limit: size + 1})
	if err != nil {
		return DriversPage{}, fmt.Errorf("list drivers: %w", err)
	}

	page := DriversPage{Drivers: drivers}

	if len(drivers) > size {
		page.Drivers = drivers[:size]
		page.NextPageToken = base64.RawURLEncoding.EncodeToString([]byte(driverPageTokenPrefix + drivers[size-1].ID))
	}

	return page, nil
}
