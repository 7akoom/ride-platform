package otp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type SMSRoute struct {
	PhonePrefix  string
	ProviderName string
	Provider     SMSProvider
}

type SMSRouterOption func(
	router *SMSRouter,
) error

type SMSRouter struct {
	routes                []SMSRoute
	defaultProviderName   string
	defaultProvider       SMSProvider
	fallbackProviderName  string
	fallbackProvider      SMSProvider
	failoverPolicy        ProviderFailoverPolicy
	metricsRecorder       DeliveryMetricsRecorder
	healthMetricsRecorder ProviderHealthMetricsRecorder
	trackingStore         DeliveryTrackingStore
	healthTracker         ProviderHealthTracker
}

func WithSMSDeliveryMetricsRecorder(
	recorder DeliveryMetricsRecorder,
) SMSRouterOption {
	return func(
		router *SMSRouter,
	) error {
		if recorder == nil {
			return errors.New(
				"SMS delivery metrics recorder is required",
			)
		}

		router.metricsRecorder = recorder

		return nil
	}
}

func WithSMSProviderHealthMetricsRecorder(
	recorder ProviderHealthMetricsRecorder,
) SMSRouterOption {
	return func(
		router *SMSRouter,
	) error {
		if recorder == nil {
			return errors.New(
				"SMS provider health metrics recorder is required",
			)
		}

		router.healthMetricsRecorder = recorder

		return nil
	}
}

func WithSMSDeliveryTrackingStore(
	store DeliveryTrackingStore,
) SMSRouterOption {
	return func(
		router *SMSRouter,
	) error {
		if store == nil {
			return errors.New(
				"SMS delivery tracking store is required",
			)
		}

		router.trackingStore = store

		return nil
	}
}

func WithSMSProviderHealthTracker(
	tracker ProviderHealthTracker,
) SMSRouterOption {
	return func(
		router *SMSRouter,
	) error {
		if tracker == nil {
			return errors.New(
				"SMS provider health tracker is required",
			)
		}

		router.healthTracker = tracker

		return nil
	}
}

func WithSMSFallbackProvider(
	providerName string,
	provider SMSProvider,
	policy ProviderFailoverPolicy,
) SMSRouterOption {
	return func(
		router *SMSRouter,
	) error {
		providerName = strings.ToLower(
			strings.TrimSpace(
				providerName,
			),
		)

		if providerName == "" {
			return errors.New(
				"SMS fallback provider name is required",
			)
		}

		if provider == nil {
			return errors.New(
				"SMS fallback provider is required",
			)
		}

		if policy == nil {
			return errors.New(
				"SMS failover policy is required",
			)
		}

		router.fallbackProviderName = providerName
		router.fallbackProvider = provider
		router.failoverPolicy = policy

		return nil
	}
}

func NewSMSRouter(
	routes []SMSRoute,
	defaultProviderName string,
	defaultProvider SMSProvider,
	options ...SMSRouterOption,
) (*SMSRouter, error) {
	normalizedRoutes := make(
		[]SMSRoute,
		0,
		len(routes),
	)

	seenPrefixes := make(
		map[string]struct{},
		len(routes),
	)

	for _, route := range routes {
		prefix := strings.TrimSpace(
			route.PhonePrefix,
		)

		if prefix == "" {
			return nil, errors.New(
				"SMS route phone prefix is required",
			)
		}

		if !strings.HasPrefix(prefix, "+") {
			return nil, fmt.Errorf(
				"SMS route phone prefix %q must use international format",
				prefix,
			)
		}

		providerName := strings.ToLower(
			strings.TrimSpace(
				route.ProviderName,
			),
		)

		if providerName == "" {
			return nil, fmt.Errorf(
				"SMS provider name is required for phone prefix %q",
				prefix,
			)
		}

		if route.Provider == nil {
			return nil, fmt.Errorf(
				"SMS provider is required for phone prefix %q",
				prefix,
			)
		}

		if _, exists := seenPrefixes[prefix]; exists {
			return nil, fmt.Errorf(
				"duplicate SMS route phone prefix %q",
				prefix,
			)
		}

		seenPrefixes[prefix] = struct{}{}

		normalizedRoutes = append(
			normalizedRoutes,
			SMSRoute{
				PhonePrefix:  prefix,
				ProviderName: providerName,
				Provider:     route.Provider,
			},
		)
	}

	sort.SliceStable(
		normalizedRoutes,
		func(i, j int) bool {
			return len(
				normalizedRoutes[i].PhonePrefix,
			) > len(
				normalizedRoutes[j].PhonePrefix,
			)
		},
	)

	defaultProviderName = strings.ToLower(
		strings.TrimSpace(
			defaultProviderName,
		),
	)

	if defaultProvider != nil &&
		defaultProviderName == "" {
		return nil, errors.New(
			"default SMS provider name is required",
		)
	}

	if defaultProvider == nil &&
		defaultProviderName != "" {
		return nil, errors.New(
			"default SMS provider is required when a provider name is configured",
		)
	}

	if len(normalizedRoutes) == 0 &&
		defaultProvider == nil {
		return nil, errors.New(
			"at least one SMS provider is required",
		)
	}

	router := &SMSRouter{
		routes:                normalizedRoutes,
		defaultProviderName:   defaultProviderName,
		defaultProvider:       defaultProvider,
		metricsRecorder:       newNoopDeliveryMetricsRecorder(),
		healthMetricsRecorder: newNoopProviderHealthMetricsRecorder(),
		trackingStore:         NoopDeliveryTrackingStore{},
		healthTracker:         NoopProviderHealthTracker{},
	}

	for _, option := range options {
		if option == nil {
			return nil, errors.New(
				"SMS router option cannot be nil",
			)
		}

		if err := option(router); err != nil {
			return nil, fmt.Errorf(
				"configure SMS router option: %w",
				err,
			)
		}
	}

	return router, nil
}

func (r *SMSRouter) Send(
	ctx context.Context,
	message SMSMessage,
) error {
	phoneNumber := strings.TrimSpace(
		message.To,
	)

	if phoneNumber == "" {
		return errors.New(
			"SMS destination phone number is required",
		)
	}

	message.To = phoneNumber

	for _, route := range r.routes {
		if strings.HasPrefix(
			phoneNumber,
			route.PhonePrefix,
		) {
			if err := r.sendWithFailover(
				ctx,
				route.ProviderName,
				route.Provider,
				message,
			); err != nil {
				return fmt.Errorf(
					"send SMS for phone prefix %q: %w",
					route.PhonePrefix,
					err,
				)
			}

			return nil
		}
	}

	if r.defaultProvider == nil {
		return fmt.Errorf(
			"no SMS provider configured for destination %q",
			phoneNumber,
		)
	}

	if err := r.sendWithFailover(
		ctx,
		r.defaultProviderName,
		r.defaultProvider,
		message,
	); err != nil {
		return fmt.Errorf(
			"send SMS through default provider: %w",
			err,
		)
	}

	return nil
}

func (r *SMSRouter) sendWithFailover(
	ctx context.Context,
	primaryProviderName string,
	primaryProvider SMSProvider,
	message SMSMessage,
) error {
	primaryProviderName = strings.ToLower(
		strings.TrimSpace(
			primaryProviderName,
		),
	)

	if !r.canAttemptSMSProvider(
		primaryProviderName,
	) {
		r.recordSMSProviderCircuitOpen(
			ctx,
			primaryProviderName,
		)

		return r.sendThroughFallbackProvider(
			ctx,
			primaryProviderName,
			fmt.Errorf(
				"SMS provider %q circuit is open",
				primaryProviderName,
			),
			message,
		)
	}

	primaryErr := r.sendThroughProvider(
		ctx,
		primaryProviderName,
		primaryProvider,
		message,
	)
	if primaryErr == nil {
		return nil
	}

	if r.fallbackProvider == nil ||
		r.failoverPolicy == nil {
		return primaryErr
	}

	if !r.failoverPolicy.ShouldFailover(
		primaryErr,
	) {
		return primaryErr
	}

	return r.sendThroughFallbackProvider(
		ctx,
		primaryProviderName,
		primaryErr,
		message,
	)
}

func (r *SMSRouter) sendThroughFallbackProvider(
	ctx context.Context,
	primaryProviderName string,
	primaryErr error,
	message SMSMessage,
) error {
	if r.fallbackProvider == nil {
		return primaryErr
	}

	if primaryProviderName ==
		r.fallbackProviderName {
		return primaryErr
	}

	if !r.canAttemptSMSProvider(
		r.fallbackProviderName,
	) {
		r.recordSMSProviderCircuitOpen(
			ctx,
			r.fallbackProviderName,
		)

		return errors.Join(
			primaryErr,
			fmt.Errorf(
				"SMS fallback provider %q circuit is open",
				r.fallbackProviderName,
			),
		)
	}

	fallbackErr := r.sendThroughProvider(
		ctx,
		r.fallbackProviderName,
		r.fallbackProvider,
		message,
	)
	if fallbackErr != nil {
		return errors.Join(
			primaryErr,
			fmt.Errorf(
				"send SMS through fallback provider %q: %w",
				r.fallbackProviderName,
				fallbackErr,
			),
		)
	}

	return nil
}

func (r *SMSRouter) recordSMSProviderCircuitOpen(
	ctx context.Context,
	providerName string,
) {
	if r.healthMetricsRecorder == nil {
		return
	}

	providerName = strings.ToLower(
		strings.TrimSpace(
			providerName,
		),
	)

	if providerName == "" {
		return
	}

	r.healthMetricsRecorder.RecordOTPProviderHealthEvent(
		ctx,
		DeliveryMetricChannelSMS,
		DeliveryMetricProvider(
			providerName,
		),
		ProviderHealthMetricEventCircuitOpen,
	)
}

func (r *SMSRouter) canAttemptSMSProvider(
	providerName string,
) bool {
	if r.healthTracker == nil {
		return true
	}

	return r.healthTracker.CanAttempt(
		DeliveryTrackingChannelSMS,
		DeliveryTrackingProvider(
			providerName,
		),
		time.Now().UTC(),
	)
}

func (r *SMSRouter) recordSMSProviderHealth(
	providerName string,
	err error,
) {
	if r.healthTracker == nil {
		return
	}

	provider := DeliveryTrackingProvider(
		strings.ToLower(
			strings.TrimSpace(
				providerName,
			),
		),
	)

	if provider == "" {
		return
	}

	if err == nil {
		r.healthTracker.RecordSuccess(
			DeliveryTrackingChannelSMS,
			provider,
		)

		return
	}

	var providerErr *SMSProviderError

	if !errors.As(
		err,
		&providerErr,
	) {
		return
	}

	switch providerErr.Kind {
	case SMSProviderErrorRateLimited,
		SMSProviderErrorTemporary,
		SMSProviderErrorUnknownDeliveryState:
		r.healthTracker.RecordFailure(
			DeliveryTrackingChannelSMS,
			provider,
			time.Now().UTC(),
		)
	}
}

func (r *SMSRouter) sendThroughProvider(
	ctx context.Context,
	providerName string,
	provider SMSProvider,
	message SMSMessage,
) error {
	challengeID := strings.TrimSpace(
		message.ChallengeID,
	)

	trackedProvider, supportsTracking :=
		provider.(TrackedSMSProvider)

	if challengeID == "" || !supportsTracking {
		startedAt := time.Now()

		err := provider.Send(
			ctx,
			message,
		)

		r.recordSMSProviderHealth(
			providerName,
			err,
		)

		outcome := DeliveryMetricOutcomeSuccess
		if err != nil {
			outcome = DeliveryMetricOutcomeFailed
		}

		if r.metricsRecorder != nil {
			r.metricsRecorder.RecordOTPDelivery(
				ctx,
				DeliveryMetricChannelSMS,
				DeliveryMetricProvider(
					providerName,
				),
				outcome,
				time.Since(startedAt),
			)
		}

		return err
	}

	attemptedAt := time.Now().UTC()

	attemptID, err := r.trackingStore.CreateAttempt(
		ctx,
		DeliveryAttemptCreateInput{
			ChallengeID: challengeID,
			Channel:     DeliveryTrackingChannelSMS,
			Provider: DeliveryTrackingProvider(
				providerName,
			),
			AttemptedAt: attemptedAt,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"create SMS delivery tracking attempt: %w",
			err,
		)
	}

	startedAt := time.Now()

	result, sendErr := trackedProvider.SendTracked(
		ctx,
		message,
	)

	r.recordSMSProviderHealth(
		providerName,
		sendErr,
	)

	outcome := DeliveryMetricOutcomeSuccess
	if sendErr != nil {
		outcome = DeliveryMetricOutcomeFailed
	}

	if r.metricsRecorder != nil {
		r.metricsRecorder.RecordOTPDelivery(
			ctx,
			DeliveryMetricChannelSMS,
			DeliveryMetricProvider(
				providerName,
			),
			outcome,
			time.Since(startedAt),
		)
	}

	if sendErr != nil {
		var providerErr *SMSProviderError

		if errors.As(
			sendErr,
			&providerErr,
		) {
			switch providerErr.Kind {
			case SMSProviderErrorPermanent,
				SMSProviderErrorRateLimited:
				if trackingErr := r.trackingStore.MarkFailed(
					ctx,
					DeliveryAttemptFailedInput{
						AttemptID:   attemptID,
						FailureCode: string(providerErr.Kind),
						FailedAt:    time.Now().UTC(),
					},
				); trackingErr != nil {
					return errors.Join(
						sendErr,
						fmt.Errorf(
							"mark SMS delivery attempt failed: %w",
							trackingErr,
						),
					)
				}

			case SMSProviderErrorTemporary,
				SMSProviderErrorUnknownDeliveryState:
				if trackingErr := r.trackingStore.MarkUnknown(
					ctx,
					DeliveryAttemptUnknownInput{
						AttemptID: attemptID,
					},
				); trackingErr != nil {
					return errors.Join(
						sendErr,
						fmt.Errorf(
							"mark SMS delivery attempt unknown: %w",
							trackingErr,
						),
					)
				}

			default:
				if trackingErr := r.trackingStore.MarkUnknown(
					ctx,
					DeliveryAttemptUnknownInput{
						AttemptID: attemptID,
					},
				); trackingErr != nil {
					return errors.Join(
						sendErr,
						fmt.Errorf(
							"mark SMS delivery attempt unknown: %w",
							trackingErr,
						),
					)
				}
			}
		} else {
			if trackingErr := r.trackingStore.MarkUnknown(
				ctx,
				DeliveryAttemptUnknownInput{
					AttemptID: attemptID,
				},
			); trackingErr != nil {
				return errors.Join(
					sendErr,
					fmt.Errorf(
						"mark SMS delivery attempt unknown: %w",
						trackingErr,
					),
				)
			}
		}

		return sendErr
	}

	if err := r.trackingStore.MarkAccepted(
		ctx,
		DeliveryAttemptAcceptedInput{
			AttemptID:         attemptID,
			ProviderMessageID: result.ProviderMessageID,
			ProviderStatus:    result.ProviderStatus,
			AcceptedAt:        time.Now().UTC(),
		},
	); err != nil {
		return fmt.Errorf(
			"mark SMS delivery attempt accepted: %w",
			err,
		)
	}

	return nil
}
