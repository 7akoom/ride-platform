package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/7akoom/ride-platform/services/pricing-service/internal/application/dispatchsettings"
)

const dispatchSettingsColumns = `next_trips_enabled, next_trip_radius_meters, COALESCE(updated_by::text, ''), updated_at`

func scanDispatchSettings(row pgx.Row) (dispatchsettings.Settings, error) {
	var settings dispatchsettings.Settings

	err := row.Scan(&settings.NextTripsEnabled, &settings.NextTripRadiusMeters, &settings.UpdatedBy, &settings.UpdatedAt)

	return settings, err
}

// GetDispatchSettings returns the one row, or the defaults if it is missing.
func (r *PricingRepository) GetDispatchSettings(ctx context.Context) (dispatchsettings.Settings, error) {
	settings, err := scanDispatchSettings(r.pool.QueryRow(ctx, `SELECT `+dispatchSettingsColumns+` FROM dispatch_settings WHERE id`))
	if err != nil {
		if isNoRows(err) {
			return dispatchsettings.Defaults(), nil
		}

		return dispatchsettings.Settings{}, fmt.Errorf("select dispatch settings: %w", err)
	}

	return settings, nil
}

func (r *PricingRepository) SetDispatchSettings(
	ctx context.Context,
	settings dispatchsettings.Settings,
) (dispatchsettings.Settings, error) {
	saved, err := scanDispatchSettings(r.pool.QueryRow(
		ctx,
		`INSERT INTO dispatch_settings (id, next_trips_enabled, next_trip_radius_meters, updated_by, updated_at)
		 VALUES (true, $1, $2, $3, CURRENT_TIMESTAMP)
		 ON CONFLICT (id) DO UPDATE
		 SET next_trips_enabled = EXCLUDED.next_trips_enabled,
		     next_trip_radius_meters = EXCLUDED.next_trip_radius_meters,
		     updated_by = EXCLUDED.updated_by,
		     updated_at = EXCLUDED.updated_at
		 RETURNING `+dispatchSettingsColumns,
		settings.NextTripsEnabled,
		settings.NextTripRadiusMeters,
		nullableUUID(settings.UpdatedBy),
	))
	if err != nil {
		return dispatchsettings.Settings{}, fmt.Errorf("save dispatch settings: %w", err)
	}

	return saved, nil
}
