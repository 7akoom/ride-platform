package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
	"github.com/7akoom/ride-platform/services/identity-service/internal/config"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
)

func buildProductionOTPDelivery(
	cfg config.Config,
) (auth.OTPDelivery, error) {
	httpTimeout, err := time.ParseDuration(
		strings.TrimSpace(
			cfg.OTPProviderHTTPTimeout,
		),
	)
	if err != nil || httpTimeout <= 0 {
		return nil, errors.New(
			"OTP provider HTTP timeout must be a positive duration",
		)
	}

	httpClient := &http.Client{
		Timeout: httpTimeout,
	}

	smsRenderer, err :=
		otp.NewDefaultSMSMessageRenderer(
			cfg.OTPBrandName,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"configure SMS message renderer: %w",
			err,
		)
	}

	defaultSMSProviderName := normalizeProviderName(
		cfg.SMSDefaultProvider,
	)
	if defaultSMSProviderName == "" {
		return nil, errors.New(
			"global SMS default provider is required in production",
		)
	}

	defaultSMSProvider, err :=
		buildSMSProvider(
			defaultSMSProviderName,
			httpClient,
			cfg,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"configure global SMS default provider: %w",
			err,
		)
	}

	smsRoutes, err := buildSMSRoutes(
		cfg.SMSRoutes,
		httpClient,
		cfg,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"configure SMS routes: %w",
			err,
		)
	}

	smsRouter, err := otp.NewSMSRouter(
		smsRoutes,
		defaultSMSProvider,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"configure SMS router: %w",
			err,
		)
	}

	smsSender, err :=
		otp.NewProviderSMSSender(
			smsRouter,
			smsRenderer,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"configure SMS sender: %w",
			err,
		)
	}

	whatsAppSender, err := buildWhatsAppSender(
		httpClient,
		cfg,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"configure WhatsApp sender: %w",
			err,
		)
	}

	emailRenderer, err :=
		otp.NewDefaultEmailMessageRenderer(
			cfg.OTPBrandName,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"configure email message renderer: %w",
			err,
		)
	}

	var emailProvider otp.EmailProvider

	switch normalizeProviderName(
		cfg.EmailProvider,
	) {
	case "resend":
		emailProvider, err =
			otp.NewResendProvider(
				httpClient,
				otp.ResendProviderConfig{
					Endpoint: cfg.ResendEndpoint,
					APIKey:   cfg.ResendAPIKey,
					From:     cfg.ResendFrom,
				},
			)
		if err != nil {
			return nil, fmt.Errorf(
				"configure Resend provider: %w",
				err,
			)
		}

	default:
		return nil, fmt.Errorf(
			"unsupported email provider %q",
			cfg.EmailProvider,
		)
	}

	emailSender, err :=
		otp.NewProviderEmailSender(
			emailProvider,
			emailRenderer,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"configure email sender: %w",
			err,
		)
	}

	delivery, err :=
		otp.NewProductionDelivery(
			smsSender,
			whatsAppSender,
			emailSender,
		)
	if err != nil {
		return nil, fmt.Errorf(
			"configure production OTP delivery: %w",
			err,
		)
	}

	return delivery, nil
}

func buildSMSRoutes(
	value string,
	httpClient otp.HTTPDoer,
	cfg config.Config,
) ([]otp.SMSRoute, error) {
	value = strings.TrimSpace(value)

	if value == "" {
		return nil, nil
	}

	rawRoutes := strings.Split(
		value,
		",",
	)

	routes := make(
		[]otp.SMSRoute,
		0,
		len(rawRoutes),
	)

	for _, rawRoute := range rawRoutes {
		parts := strings.SplitN(
			strings.TrimSpace(rawRoute),
			"=",
			2,
		)

		if len(parts) != 2 {
			return nil, fmt.Errorf(
				"invalid SMS route %q",
				rawRoute,
			)
		}

		prefix := strings.TrimSpace(
			parts[0],
		)

		if !isValidSMSRoutePrefix(prefix) {
			return nil, fmt.Errorf(
				"invalid SMS route prefix %q",
				prefix,
			)
		}

		providerName := normalizeProviderName(
			parts[1],
		)
		if providerName == "" {
			return nil, fmt.Errorf(
				"SMS route %q has no provider",
				rawRoute,
			)
		}

		provider, err := buildSMSProvider(
			providerName,
			httpClient,
			cfg,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"configure SMS provider %q for prefix %q: %w",
				providerName,
				prefix,
				err,
			)
		}

		routes = append(
			routes,
			otp.SMSRoute{
				PhonePrefix: prefix,
				Provider:    provider,
			},
		)
	}

	return routes, nil
}

func buildSMSProvider(
	name string,
	httpClient otp.HTTPDoer,
	cfg config.Config,
) (otp.SMSProvider, error) {
	switch normalizeProviderName(name) {
	case "telnyx":
		return otp.NewTelnyxProvider(
			httpClient,
			otp.TelnyxProviderConfig{
				Endpoint:           cfg.TelnyxEndpoint,
				APIKey:             cfg.TelnyxAPIKey,
				From:               cfg.TelnyxFrom,
				MessagingProfileID: cfg.TelnyxMessagingProfileID,
			},
		)

	case "bulksmsiraq":
		return otp.NewBulkSMSIraqProvider(
			httpClient,
			otp.BulkSMSIraqProviderConfig{
				Endpoint: cfg.BulkSMSIraqEndpoint,
				APIKey:   cfg.BulkSMSIraqAPIKey,
				SenderID: cfg.BulkSMSIraqSenderID,
			},
		)

	default:
		return nil, fmt.Errorf(
			"unsupported SMS provider %q",
			name,
		)
	}
}

func buildWhatsAppSender(
	httpClient otp.HTTPDoer,
	cfg config.Config,
) (otp.WhatsAppSender, error) {
	defaultProviderName := normalizeProviderName(
		cfg.WhatsAppDefaultProvider,
	)

	routes, err := buildWhatsAppRoutes(
		cfg.WhatsAppRoutes,
		httpClient,
		cfg,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"configure WhatsApp routes: %w",
			err,
		)
	}

	if defaultProviderName == "" &&
		len(routes) == 0 {
		return nil, nil
	}

	var defaultProvider otp.WhatsAppProvider

	if defaultProviderName != "" {
		defaultProvider, err =
			buildWhatsAppProvider(
				defaultProviderName,
				httpClient,
				cfg,
			)
		if err != nil {
			return nil, fmt.Errorf(
				"configure global WhatsApp default provider: %w",
				err,
			)
		}
	}

	router, err := otp.NewWhatsAppRouter(
		routes,
		defaultProvider,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"configure WhatsApp router: %w",
			err,
		)
	}

	sender, err := otp.NewProviderWhatsAppSender(
		router,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"configure WhatsApp provider sender: %w",
			err,
		)
	}

	return sender, nil
}

func buildWhatsAppRoutes(
	value string,
	httpClient otp.HTTPDoer,
	cfg config.Config,
) ([]otp.WhatsAppRoute, error) {
	value = strings.TrimSpace(value)

	if value == "" {
		return nil, nil
	}

	rawRoutes := strings.Split(
		value,
		",",
	)

	routes := make(
		[]otp.WhatsAppRoute,
		0,
		len(rawRoutes),
	)

	for _, rawRoute := range rawRoutes {
		parts := strings.SplitN(
			strings.TrimSpace(rawRoute),
			"=",
			2,
		)

		if len(parts) != 2 {
			return nil, fmt.Errorf(
				"invalid WhatsApp route %q",
				rawRoute,
			)
		}

		prefix := strings.TrimSpace(
			parts[0],
		)

		if !isValidWhatsAppRoutePrefix(
			prefix,
		) {
			return nil, fmt.Errorf(
				"invalid WhatsApp route prefix %q",
				prefix,
			)
		}

		providerName := normalizeProviderName(
			parts[1],
		)
		if providerName == "" {
			return nil, fmt.Errorf(
				"WhatsApp route %q has no provider",
				rawRoute,
			)
		}

		provider, err := buildWhatsAppProvider(
			providerName,
			httpClient,
			cfg,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"configure WhatsApp provider %q for prefix %q: %w",
				providerName,
				prefix,
				err,
			)
		}

		routes = append(
			routes,
			otp.WhatsAppRoute{
				PhonePrefix: prefix,
				Provider:    provider,
			},
		)
	}

	return routes, nil
}

func buildWhatsAppProvider(
	name string,
	httpClient otp.HTTPDoer,
	cfg config.Config,
) (otp.WhatsAppProvider, error) {
	switch normalizeProviderName(name) {
	case "meta":
		templates := map[string]otp.MetaWhatsAppTemplate{
			"en": {
				Name: strings.TrimSpace(
					cfg.MetaWhatsAppTemplateENName,
				),
				LanguageCode: strings.TrimSpace(
					cfg.MetaWhatsAppTemplateENLanguage,
				),
			},
		}

		if strings.TrimSpace(
			cfg.MetaWhatsAppTemplateARName,
		) != "" {
			templates["ar"] =
				otp.MetaWhatsAppTemplate{
					Name: strings.TrimSpace(
						cfg.MetaWhatsAppTemplateARName,
					),
					LanguageCode: strings.TrimSpace(
						cfg.MetaWhatsAppTemplateARLanguage,
					),
				}
		}

		if strings.TrimSpace(
			cfg.MetaWhatsAppTemplateKUName,
		) != "" {
			templates["ku"] =
				otp.MetaWhatsAppTemplate{
					Name: strings.TrimSpace(
						cfg.MetaWhatsAppTemplateKUName,
					),
					LanguageCode: strings.TrimSpace(
						cfg.MetaWhatsAppTemplateKULanguage,
					),
				}
		}

		return otp.NewMetaWhatsAppProvider(
			httpClient,
			otp.MetaWhatsAppProviderConfig{
				Endpoint:    cfg.MetaWhatsAppEndpoint,
				AccessToken: cfg.MetaWhatsAppAccessToken,
				Templates:   templates,
			},
		)

	default:
		return nil, fmt.Errorf(
			"unsupported WhatsApp provider %q",
			name,
		)
	}
}

func isValidWhatsAppRoutePrefix(
	value string,
) bool {
	return isValidSMSRoutePrefix(
		value,
	)
}

func normalizeProviderName(
	value string,
) string {
	return strings.ToLower(
		strings.TrimSpace(value),
	)
}

func isValidSMSRoutePrefix(
	value string,
) bool {
	if len(value) < 2 ||
		value[0] != '+' {
		return false
	}

	for _, character := range value[1:] {
		if character < '0' ||
			character > '9' {
			return false
		}
	}

	return true
}
