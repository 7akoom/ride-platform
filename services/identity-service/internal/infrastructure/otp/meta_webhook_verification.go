package otp

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

type MetaWebhookVerificationHandler struct {
	verifyToken string
}

func NewMetaWebhookVerificationHandler(
	verifyToken string,
) (*MetaWebhookVerificationHandler, error) {
	verifyToken = strings.TrimSpace(
		verifyToken,
	)

	if verifyToken == "" {
		return nil, errors.New(
			"Meta webhook verify token is required",
		)
	}

	return &MetaWebhookVerificationHandler{
		verifyToken: verifyToken,
	}, nil
}

func (h *MetaWebhookVerificationHandler) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if h == nil ||
		strings.TrimSpace(h.verifyToken) == "" {
		http.Error(
			writer,
			http.StatusText(
				http.StatusInternalServerError,
			),
			http.StatusInternalServerError,
		)

		return
	}

	if request == nil {
		http.Error(
			writer,
			http.StatusText(
				http.StatusBadRequest,
			),
			http.StatusBadRequest,
		)

		return
	}

	if request.Method != http.MethodGet {
		writer.Header().Set(
			"Allow",
			http.MethodGet,
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

	query := request.URL.Query()

	mode := strings.TrimSpace(
		query.Get("hub.mode"),
	)

	verifyToken := strings.TrimSpace(
		query.Get("hub.verify_token"),
	)

	challenge := query.Get(
		"hub.challenge",
	)

	if mode != "subscribe" {
		http.Error(
			writer,
			http.StatusText(
				http.StatusForbidden,
			),
			http.StatusForbidden,
		)

		return
	}

	if verifyToken == "" ||
		subtle.ConstantTimeCompare(
			[]byte(verifyToken),
			[]byte(h.verifyToken),
		) != 1 {
		http.Error(
			writer,
			http.StatusText(
				http.StatusForbidden,
			),
			http.StatusForbidden,
		)

		return
	}

	if challenge == "" {
		http.Error(
			writer,
			http.StatusText(
				http.StatusBadRequest,
			),
			http.StatusBadRequest,
		)

		return
	}

	writer.Header().Set(
		"Content-Type",
		"text/plain; charset=utf-8",
	)

	writer.WriteHeader(
		http.StatusOK,
	)

	_, _ = writer.Write(
		[]byte(challenge),
	)
}
