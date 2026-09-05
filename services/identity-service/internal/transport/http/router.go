package httptransport

import (
	"errors"
	"net/http"
	"strings"
)

type WebhookHandler interface {
	ServeHTTP(
		http.ResponseWriter,
		*http.Request,
	)
}

type WebhookRoute struct {
	Provider   string
	Handler    WebhookHandler
	GetHandler WebhookHandler
}

func NewWebhookRouter(
	routes []WebhookRoute,
) (http.Handler, error) {
	mux := http.NewServeMux()

	seenProviders := make(
		map[string]struct{},
		len(routes),
	)

	for _, route := range routes {
		provider := strings.ToLower(
			strings.TrimSpace(
				route.Provider,
			),
		)

		if provider == "" {
			return nil, errors.New(
				"webhook provider is required",
			)
		}

		if route.Handler == nil {
			return nil, errors.New(
				"webhook handler is required",
			)
		}

		if _, exists := seenProviders[provider]; exists {
			return nil, errors.New(
				"duplicate webhook provider",
			)
		}

		seenProviders[provider] = struct{}{}

		path := "/webhooks/otp/" + provider

		postHandler := route.Handler
		getHandler := route.GetHandler

		mux.Handle(
			path,
			http.HandlerFunc(
				func(
					writer http.ResponseWriter,
					request *http.Request,
				) {
					switch request.Method {
					case http.MethodPost:
						postHandler.ServeHTTP(
							writer,
							request,
						)

					case http.MethodGet:
						if getHandler == nil {
							writer.Header().Set(
								"Allow",
								http.MethodPost,
							)

							http.Error(
								writer,
								http.StatusText(
									http.StatusMethodNotAllowed,
								),
								http.StatusMethodNotAllowed,
							)

							return
						}

						getHandler.ServeHTTP(
							writer,
							request,
						)

					default:
						allowedMethods := http.MethodPost

						if getHandler != nil {
							allowedMethods =
								http.MethodGet +
									", " +
									http.MethodPost
						}

						writer.Header().Set(
							"Allow",
							allowedMethods,
						)

						http.Error(
							writer,
							http.StatusText(
								http.StatusMethodNotAllowed,
							),
							http.StatusMethodNotAllowed,
						)
					}
				},
			),
		)
	}

	return mux, nil
}
