package otp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type SMSRoute struct {
	PhonePrefix string
	Provider    SMSProvider
}

type SMSRouter struct {
	routes          []SMSRoute
	defaultProvider SMSProvider
}

func NewSMSRouter(
	routes []SMSRoute,
	defaultProvider SMSProvider,
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
			"at least one SMS provider is required",
		)
	}

	return &SMSRouter{
		routes:          normalizedRoutes,
		defaultProvider: defaultProvider,
	}, nil
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
			if err := route.Provider.Send(
				ctx,
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

	if err := r.defaultProvider.Send(
		ctx,
		message,
	); err != nil {
		return fmt.Errorf(
			"send SMS through default provider: %w",
			err,
		)
	}

	return nil
}
