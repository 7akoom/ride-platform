package otp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type WhatsAppRoute struct {
	PhonePrefix string
	Provider    WhatsAppProvider
}

type WhatsAppRouter struct {
	routes          []WhatsAppRoute
	defaultProvider WhatsAppProvider
}

func NewWhatsAppRouter(
	routes []WhatsAppRoute,
	defaultProvider WhatsAppProvider,
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
				Provider:    route.Provider,
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

	return &WhatsAppRouter{
		routes:          normalizedRoutes,
		defaultProvider: defaultProvider,
	}, nil
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
			if err := route.Provider.SendOTP(
				ctx,
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

	if err := r.defaultProvider.SendOTP(
		ctx,
		input,
	); err != nil {
		return fmt.Errorf(
			"send WhatsApp OTP through default provider: %w",
			err,
		)
	}

	return nil
}
