package otp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMetaWebhookVerificationHandlerReturnsChallenge(
	t *testing.T,
) {
	handler, err :=
		NewMetaWebhookVerificationHandler(
			"verify-secret",
		)
	if err != nil {
		t.Fatalf(
			"NewMetaWebhookVerificationHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/webhooks/otp/meta?hub.mode=subscribe&hub.verify_token=verify-secret&hub.challenge=123456",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusOK,
		)
	}

	if recorder.Body.String() != "123456" {
		t.Fatalf(
			"body = %q, want %q",
			recorder.Body.String(),
			"123456",
		)
	}

	contentType :=
		recorder.Header().Get(
			"Content-Type",
		)

	if contentType !=
		"text/plain; charset=utf-8" {
		t.Fatalf(
			"Content-Type = %q, want %q",
			contentType,
			"text/plain; charset=utf-8",
		)
	}
}

func TestMetaWebhookVerificationHandlerRejectsWrongToken(
	t *testing.T,
) {
	handler, err :=
		NewMetaWebhookVerificationHandler(
			"verify-secret",
		)
	if err != nil {
		t.Fatalf(
			"NewMetaWebhookVerificationHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/webhooks/otp/meta?hub.mode=subscribe&hub.verify_token=wrong-token&hub.challenge=123456",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code !=
		http.StatusForbidden {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusForbidden,
		)
	}
}

func TestMetaWebhookVerificationHandlerRejectsWrongMode(
	t *testing.T,
) {
	handler, err :=
		NewMetaWebhookVerificationHandler(
			"verify-secret",
		)
	if err != nil {
		t.Fatalf(
			"NewMetaWebhookVerificationHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/webhooks/otp/meta?hub.mode=unsubscribe&hub.verify_token=verify-secret&hub.challenge=123456",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code !=
		http.StatusForbidden {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusForbidden,
		)
	}
}

func TestMetaWebhookVerificationHandlerRejectsMissingChallenge(
	t *testing.T,
) {
	handler, err :=
		NewMetaWebhookVerificationHandler(
			"verify-secret",
		)
	if err != nil {
		t.Fatalf(
			"NewMetaWebhookVerificationHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/webhooks/otp/meta?hub.mode=subscribe&hub.verify_token=verify-secret",
		nil,
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code !=
		http.StatusBadRequest {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusBadRequest,
		)
	}
}

func TestMetaWebhookVerificationHandlerRejectsNonGET(
	t *testing.T,
) {
	handler, err :=
		NewMetaWebhookVerificationHandler(
			"verify-secret",
		)
	if err != nil {
		t.Fatalf(
			"NewMetaWebhookVerificationHandler() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/meta",
		strings.NewReader("{}"),
	)

	recorder := httptest.NewRecorder()

	handler.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code !=
		http.StatusMethodNotAllowed {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusMethodNotAllowed,
		)
	}

	if recorder.Header().Get("Allow") !=
		http.MethodGet {
		t.Fatalf(
			"Allow = %q, want %q",
			recorder.Header().Get("Allow"),
			http.MethodGet,
		)
	}
}

func TestNewMetaWebhookVerificationHandlerRejectsBlankToken(
	t *testing.T,
) {
	tests := []string{
		"",
		" ",
		"\t",
	}

	for _, verifyToken := range tests {
		handler, err :=
			NewMetaWebhookVerificationHandler(
				verifyToken,
			)

		if err == nil {
			t.Fatalf(
				"NewMetaWebhookVerificationHandler(%q) accepted blank token",
				verifyToken,
			)
		}

		if handler != nil {
			t.Fatalf(
				"NewMetaWebhookVerificationHandler(%q) returned handler",
				verifyToken,
			)
		}
	}
}
