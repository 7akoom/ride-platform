package postgres

import (
	"context"
	"fmt"

	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
)

// DeviceOwner reports which rider or driver a device token is registered to.
// found is false when the token is not registered at all.
//
// The transport layer uses it to check that a caller unregistering a device
// owns it: UnregisterDevice deletes by token alone, so without this any user
// who knew a token could switch off another account's push notifications.
func (r *NotificationRepository) DeviceOwner(
	ctx context.Context,
	deviceToken string,
) (notification.RecipientType, string, bool, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT recipient_type, recipient_id
		 FROM devices
		 WHERE device_token = $1`,
		deviceToken,
	)
	if err != nil {
		return "", "", false, fmt.Errorf("select device owner: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return "", "", false, fmt.Errorf("select device owner: %w", err)
		}

		return "", "", false, nil
	}

	var recipientType, recipientID string

	if err := rows.Scan(&recipientType, &recipientID); err != nil {
		return "", "", false, fmt.Errorf("scan device owner: %w", err)
	}

	return notification.RecipientType(recipientType), recipientID, true, nil
}
