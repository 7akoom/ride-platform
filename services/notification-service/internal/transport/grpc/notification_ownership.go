package grpc

import (
	"context"
	"strings"

	notificationv1 "github.com/7akoom/ride-platform/gen/go/ride/notification/v1"
	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

const notificationRPCPrefix = "/ride.notification.v1.NotificationService/"

// DeviceOwnerReader tells who a device token is registered to. UnregisterDevice
// carries nothing but the token, so ownership can only be checked by looking
// the token up.
type DeviceOwnerReader interface {
	DeviceOwner(ctx context.Context, deviceToken string) (recipientType notification.RecipientType, recipientID string, found bool, err error)
}

// ownsRecipient reports whether the caller is the rider or driver the request
// names as its recipient. A driver id is never accepted as a rider recipient
// or the other way round, and an unspecified type owns nothing.
func ownsRecipient(ctx context.Context, c caller, recipientType notificationv1.RecipientType, recipientID string) (bool, error) {
	switch recipientType {
	case notificationv1.RecipientType_RECIPIENT_TYPE_RIDER:
		return c.ownsRider(ctx, recipientID)

	case notificationv1.RecipientType_RECIPIENT_TYPE_DRIVER:
		return c.ownsDriver(ctx, recipientID)

	default:
		return false, nil
	}
}

// ownsDevice reports whether the device token is registered to the caller. A
// token that is not registered counts as not owned, so callers cannot tell an
// unknown token from someone else's. The token is trimmed exactly as the
// service trims it before deleting, so padding cannot slip past the check.
func (c caller) ownsDevice(ctx context.Context, deviceToken string) (bool, error) {
	token := strings.TrimSpace(deviceToken)
	if token == "" || c.devices == nil {
		return false, nil
	}

	recipientType, recipientID, found, err := c.devices.DeviceOwner(ctx, token)
	if err != nil {
		return false, err
	}

	if !found {
		return false, nil
	}

	switch recipientType {
	case notification.RecipientRider:
		return c.ownsRider(ctx, recipientID)

	case notification.RecipientDriver:
		return c.ownsDriver(ctx, recipientID)

	default:
		return false, nil
	}
}

var ownerChecks = map[string]ownerCheck{
	notificationRPCPrefix + "RegisterDevice": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*notificationv1.RegisterDeviceRequest)
		if !ok {
			return false, nil
		}

		return ownsRecipient(ctx, c, r.GetRecipientType(), r.GetRecipientId())
	},
	notificationRPCPrefix + "UnregisterDevice": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*notificationv1.UnregisterDeviceRequest)
		if !ok {
			return false, nil
		}

		return c.ownsDevice(ctx, r.GetDeviceToken())
	},
	notificationRPCPrefix + "ListNotifications": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*notificationv1.ListNotificationsRequest)
		if !ok {
			return false, nil
		}

		return ownsRecipient(ctx, c, r.GetRecipientType(), r.GetRecipientId())
	},
	// MarkAsRead only ever touches notifications of the recipient it names
	// (the query filters on recipient as well as id), so owning the recipient
	// is enough: ids belonging to anyone else are simply not matched.
	notificationRPCPrefix + "MarkAsRead": func(ctx context.Context, c caller, request any) (bool, error) {
		r, ok := request.(*notificationv1.MarkAsReadRequest)
		if !ok {
			return false, nil
		}

		return ownsRecipient(ctx, c, r.GetRecipientType(), r.GetRecipientId())
	},
}
