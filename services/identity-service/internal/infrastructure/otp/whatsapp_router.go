package otp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type WhatsAppRoute struct {
	PhonePrefix  string
	ProviderName DeliveryMetricProvider
	Provider     WhatsAppProvider
}

type WhatsAppRouterOption func(
	router *WhatsAppRouter,
) error

type WhatsAppRouter struct {
	routes                []WhatsAppRoute
	defaultProviderName   DeliveryMetricProvider
	defaultProvider       WhatsAppProvider
	fallbackProviderName  DeliveryMetricProvider
	fallbackProvider      WhatsAppProvider
	failoverPolicy        ProviderFailoverPolicy
	metricsRecorder       DeliveryMetricsRecorder
	healthMetricsRecorder ProviderHealthMetricsRecorder
	trackingStore         DeliveryTrackingStore
	healthTracker         ProviderHealthTracker
}

func WithWhatsAppRouterDefaultProviderName(
	providerName DeliveryMetricProvider,
) WhatsAppRouterOption {
	return func(
		router *WhatsAppRouter,
	) error {
		providerName = normalizeDeliveryMetricProvider(
			providerName,
		)

		if providerName == "" {
			return errors.New(
				"WhatsApp default provider name is required",
			)
		}

		router.defaultProviderName = providerName

		return nil
	}
}

func WithWhatsAppRouterDeliveryMetricsRecorder(
	recorder DeliveryMetricsRecorder,
) WhatsAppRouterOption {
	return func(
		router *WhatsAppRouter,
	) error {
		if recorder == nil {
			return errors.New(
				"WhatsApp delivery metrics recorder is required",
			)
		}

		router.metricsRecorder = recorder

		return nil
	}
}

func WithWhatsAppProviderHealthMetricsRecorder(
	recorder ProviderHealthMetricsRecorder,
) WhatsAppRouterOption {
	return func(
		router *WhatsAppRouter,
	) error {
		if recorder == nil {
			return errors.New(
				"WhatsApp provider health metrics recorder is required",
			)
		}

		router.healthMetricsRecorder = recorder

		return nil
	}
}

func WithWhatsAppDeliveryTrackingStore(
	store DeliveryTrackingStore,
) WhatsAppRouterOption {
	return func(
		router *WhatsAppRouter,
	) error {
		if store == nil {
			return errors.New(
				"WhatsApp delivery tracking store is required",
			)
		}

		router.trackingStore = store

		return nil
	}
}

func WithWhatsAppProviderHealthTracker(
	tracker ProviderHealthTracker,
) WhatsAppRouterOption {
	return func(
		router *WhatsAppRouter,
	) error {
		if tracker == nil {
			return errors.New(
				"WhatsApp provider health tracker is required",
			)
		}

		router.healthTracker = tracker

		return nil
	}
}

func WithWhatsAppFallbackProvider(
	providerName DeliveryMetricProvider,
	provider WhatsAppProvider,
	policy ProviderFailoverPolicy,
) WhatsAppRouterOption {
	return func(
		router *WhatsAppRouter,
	) error {
		providerName = normalizeDeliveryMetricProvider(
			providerName,
		)

		if providerName == "" {
			return errors.New(
				"WhatsApp fallback provider name is required",
			)
		}

		if provider == nil {
			return errors.New(
				"WhatsApp fallback provider is required",
			)
		}

		if policy == nil {
			return errors.New(
				"WhatsApp failover policy is required",
			)
		}

		router.fallbackProviderName = providerName
		router.fallbackProvider = provider
		router.failoverPolicy = policy

		return nil
	}
}

func NewWhatsAppRouter(
	routes []WhatsAppRoute,
	defaultProvider WhatsAppProvider,
	options ...WhatsAppRouterOption,
) (*WhatsAppRouter, error) {
	normalizedRoutes := make(
		[]WhatsAppRoute,
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
				"WhatsApp route phone prefix is required",
			)
		}

		if !strings.HasPrefix(prefix, "+") {
			return nil, fmt.Errorf(
				"WhatsApp route phone prefix %q must use international format",
				prefix,
			)
		}

		if route.Provider == nil {
			return nil, fmt.Errorf(
				"WhatsApp provider is required for phone prefix %q",
				prefix,
			)
		}

		if _, exists := seenPrefixes[prefix]; exists {
			return nil, fmt.Errorf(
				"duplicate WhatsApp route phone prefix %q",
				prefix,
			)
		}

		seenPrefixes[prefix] = struct{}{}

		normalizedRoutes = append(
			normalizedRoutes,
			WhatsAppRoute{
				PhonePrefix: prefix,
				ProviderName: normalizeDeliveryMetricProvider(
					route.ProviderName,
				),
				Provider: route.Provider,
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

	if len(normalizedRoutes) == 0 &&
		defaultProvider == nil {
		return nil, errors.New(
			"at least one WhatsApp provider is required",
		)
	}

	router := &WhatsAppRouter{
		routes:                normalizedRoutes,
		defaultProvider:       defaultProvider,
		healthMetricsRecorder: newNoopProviderHealthMetricsRecorder(),
		trackingStore:         NoopDeliveryTrackingStore{},
		healthTracker:         NoopProviderHealthTracker{},
	}

	for _, option := range options {
		if option == nil {
			return nil, errors.New(
				"WhatsApp router option cannot be nil",
			)
		}

		if err := option(router); err != nil {
			return nil, fmt.Errorf(
				"configure WhatsApp router option: %w",
				err,
			)
		}
	}

	if router.metricsRecorder != nil {
		for _, route := range router.routes {
			if route.ProviderName == "" {
				return nil, fmt.Errorf(
					"WhatsApp provider name is required for phone prefix %q when delivery metrics are configured",
					route.PhonePrefix,
				)
			}
		}

		if router.defaultProvider != nil &&
			router.defaultProviderName == "" {
			return nil, errors.New(
				"WhatsApp default provider name is required when delivery metrics are configured",
			)
		}
	}

	if router.defaultProvider == nil &&
		router.defaultProviderName != "" {
		return nil, errors.New(
			"WhatsApp default provider is required when a provider name is configured",
		)
	}

	return router, nil
}

func (r *WhatsAppRouter) SendOTP(
	ctx context.Context,
	input WhatsAppOTPProviderInput,
) error {
	phoneNumber := strings.TrimSpace(
		input.PhoneNumber,
	)

	if phoneNumber == "" {
		return errors.New(
			"WhatsApp destination phone number is required",
		)
	}

	input.PhoneNumber = phoneNumber

	for _, route := range r.routes {
		if strings.HasPrefix(
			phoneNumber,
			route.PhonePrefix,
		) {
			if err := r.sendWithFailover(
				ctx,
				route.ProviderName,
				route.Provider,
				input,
			); err != nil {
				return fmt.Errorf(
					"send WhatsApp OTP for phone prefix %q: %w",
					route.PhonePrefix,
					err,
				)
			}

			return nil
		}
	}

	if r.defaultProvider == nil {
		return fmt.Errorf(
			"no WhatsApp provider configured for destination %q",
			phoneNumber,
		)
	}

	if err := r.sendWithFailover(
		ctx,
		r.defaultProviderName,
		r.defaultProvider,
		input,
	); err != nil {
		return fmt.Errorf(
			"send WhatsApp OTP through default provider: %w",
			err,
		)
	}

	return nil
}

func (r *WhatsAppRouter) sendWithFailover(
	ctx context.Context,
	primaryProviderName DeliveryMetricProvider,
	primaryProvider WhatsAppProvider,
	input WhatsAppOTPProviderInput,
) error {
	primaryProviderName =
		normalizeDeliveryMetricProvider(
			primaryProviderName,
		)

	if !r.canAttemptWhatsAppProvider(
		primaryProviderName,
	) {
		r.recordWhatsAppProviderCircuitOpen(
			ctx,
			primaryProviderName,
		)

		return r.sendThroughWhatsAppFallbackProvider(
			ctx,
			primaryProviderName,
			fmt.Errorf(
				"WhatsApp provider %q circuit is open",
				primaryProviderName,
			),
			input,
		)
	}

	primaryErr := r.sendThroughProvider(
		ctx,
		primaryProviderName,
		primaryProvider,
		input,
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

	return r.sendThroughWhatsAppFallbackProvider(
		ctx,
		primaryProviderName,
		primaryErr,
		input,
	)
}
func (r *WhatsAppRouter) sendThroughWhatsAppFallbackProvider(
	ctx context.Context,
	primaryProviderName DeliveryMetricProvider,
	primaryErr error,
	input WhatsAppOTPProviderInput,
) error {
	if r.fallbackProvider == nil {
		return primaryErr
	}

	if primaryProviderName ==
		r.fallbackProviderName {
		return primaryErr
	}

	if !r.canAttemptWhatsAppProvider(
		r.fallbackProviderName,
	) {
		r.recordWhatsAppProviderCircuitOpen(
			ctx,
			r.fallbackProviderName,
		)

		return errors.Join(
			primaryErr,
			fmt.Errorf(
				"WhatsApp fallback provider %q circuit is open",
				r.fallbackProviderName,
			),
		)
	}

	fallbackErr := r.sendThroughProvider(
		ctx,
		r.fallbackProviderName,
		r.fallbackProvider,
		input,
	)
	if fallbackErr != nil {
		return errors.Join(
			primaryErr,
			fmt.Errorf(
				"send WhatsApp OTP through fallback provider %q: %w",
				r.fallbackProviderName,
				fallbackErr,
			),
		)
	}

	return nil
}

func (r *WhatsAppRouter) recordWhatsAppProviderCircuitOpen(
	ctx context.Context,
	providerName DeliveryMetricProvider,
) {
	if r.healthMetricsRecorder == nil {
		return
	}

	providerName = normalizeDeliveryMetricProvider(
		providerName,
	)

	if providerName == "" {
		return
	}

	r.healthMetricsRecorder.RecordOTPProviderHealthEvent(
		ctx,
		DeliveryMetricChannelWhatsApp,
		providerName,
		ProviderHealthMetricEventCircuitOpen,
	)
}

func (r *WhatsAppRouter) canAttemptWhatsAppProvider(
	providerName DeliveryMetricProvider,
) bool {
	if r.healthTracker == nil {
		return true
	}

	return r.healthTracker.CanAttempt(
		DeliveryTrackingChannelWhatsApp,
		DeliveryTrackingProvider(
			providerName,
		),
		time.Now().UTC(),
	)
}

func (r *WhatsAppRouter) recordWhatsAppProviderHealth(
	providerName DeliveryMetricProvider,
	err error,
) {
	if r.healthTracker == nil {
		return
	}

	provider := DeliveryTrackingProvider(
		normalizeDeliveryMetricProvider(
			providerName,
		),
	)

	if provider == "" {
		return
	}

	if err == nil {
		r.healthTracker.RecordSuccess(
			DeliveryTrackingChannelWhatsApp,
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
			DeliveryTrackingChannelWhatsApp,
			provider,
			time.Now().UTC(),
		)
	}
}

func (r *WhatsAppRouter) sendThroughProvider(
	ctx context.Context,
	providerName DeliveryMetricProvider,
	provider WhatsAppProvider,
	input WhatsAppOTPProviderInput,
) error {
	trackedProvider, tracked :=
		provider.(TrackedWhatsAppProvider)

	if strings.TrimSpace(input.ChallengeID) == "" ||
		!tracked {
		startedAt := time.Now()

		err := provider.SendOTP(
			ctx,
			input,
		)

		r.recordWhatsAppProviderHealth(
			providerName,
			err,
		)

		r.recordDeliveryMetric(
			ctx,
			providerName,
			err,
			time.Since(startedAt),
		)

		return err
	}

	attemptedAt := time.Now()

	attemptID, err := r.trackingStore.CreateAttempt(
		ctx,
		DeliveryAttemptCreateInput{
			ChallengeID: input.ChallengeID,
			Channel:     DeliveryTrackingChannelWhatsApp,
			Provider: DeliveryTrackingProvider(
				providerName,
			),
			AttemptedAt: attemptedAt,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"create WhatsApp delivery attempt: %w",
			err,
		)
	}

	startedAt := time.Now()

	result, sendErr := trackedProvider.SendOTPTracked(
		ctx,
		input,
	)

	r.recordWhatsAppProviderHealth(
		providerName,
		sendErr,
	)

	r.recordDeliveryMetric(
		ctx,
		providerName,
		sendErr,
		time.Since(startedAt),
	)

	if sendErr != nil {
		trackingErr := r.recordFailedOrUnknownAttempt(
			ctx,
			attemptID,
			sendErr,
		)

		if trackingErr != nil {
			return errors.Join(
				sendErr,
				trackingErr,
			)
		}

		return sendErr
	}

	if err := r.trackingStore.MarkAccepted(
		ctx,
		DeliveryAttemptAcceptedInput{
			AttemptID:         attemptID,
			ProviderMessageID: result.ProviderMessageID,
			ProviderStatus:    result.ProviderStatus,
			AcceptedAt:        time.Now(),
		},
	); err != nil {
		return fmt.Errorf(
			"mark WhatsApp delivery attempt accepted: %w",
			err,
		)
	}

	return nil
}

func (r *WhatsAppRouter) recordFailedOrUnknownAttempt(
	ctx context.Context,
	attemptID string,
	sendErr error,
) error {
	var providerErr *SMSProviderError

	if !errors.As(
		sendErr,
		&providerErr,
	) {
		if err := r.trackingStore.MarkUnknown(
			ctx,
			DeliveryAttemptUnknownInput{
				AttemptID: attemptID,
			},
		); err != nil {
			return fmt.Errorf(
				"mark WhatsApp delivery attempt unknown: %w",
				err,
			)
		}

		return nil
	}

	switch providerErr.Kind {
	case SMSProviderErrorPermanent,
		SMSProviderErrorRateLimited:
		if err := r.trackingStore.MarkFailed(
			ctx,
			DeliveryAttemptFailedInput{
				AttemptID: attemptID,
				FailureCode: string(
					providerErr.Kind,
				),
				FailedAt: time.Now(),
			},
		); err != nil {
			return fmt.Errorf(
				"mark WhatsApp delivery attempt failed: %w",
				err,
			)
		}

	default:
		if err := r.trackingStore.MarkUnknown(
			ctx,
			DeliveryAttemptUnknownInput{
				AttemptID: attemptID,
			},
		); err != nil {
			return fmt.Errorf(
				"mark WhatsApp delivery attempt unknown: %w",
				err,
			)
		}
	}

	return nil
}

func (r *WhatsAppRouter) recordDeliveryMetric(
	ctx context.Context,
	providerName DeliveryMetricProvider,
	err error,
	duration time.Duration,
) {
	if r.metricsRecorder == nil {
		return
	}

	outcome := DeliveryMetricOutcomeSuccess

	if err != nil {
		outcome = DeliveryMetricOutcomeFailed
	}

	r.metricsRecorder.RecordOTPDelivery(
		ctx,
		DeliveryMetricChannelWhatsApp,
		providerName,
		outcome,
		duration,
	)
}

func normalizeDeliveryMetricProvider(
	provider DeliveryMetricProvider,
) DeliveryMetricProvider {
	return DeliveryMetricProvider(
		strings.ToLower(
			strings.TrimSpace(
				string(provider),
			),
		),
	)
}
