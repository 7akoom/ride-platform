package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testJWT = "eyJhbGciOiJSUzI1NiIsImtpZCI6ImlkZW50aXR5LWRldi0xIn0.eyJzdWIiOiJ1c2VyLTEifQ.c2lnbmF0dXJl-_x"

func serve(t *testing.T, headers map[string]string) (*httptest.ResponseRecorder, http.Header) {
	t.Helper()

	var seen http.Header

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/riders/x", nil)
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	recorder := httptest.NewRecorder()
	requireUserCredentials(next).ServeHTTP(recorder, request)

	return recorder, seen
}

func TestUserAccessTokensAndAnonymousRequestsPassThrough(t *testing.T) {
	// No header at all: the public identity routes (login) have none, and the
	// backends refuse anonymous calls to everything else on their own.
	for name, headers := range map[string]map[string]string{
		"no credentials": {},
		"a user token":   {"Authorization": "Bearer " + testJWT},
	} {
		if recorder, _ := serve(t, headers); recorder.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", name, recorder.Code)
		}
	}
}

func TestAnythingElseInTheAuthorizationHeaderIsRefused(t *testing.T) {
	internalToken := strings.Repeat("f", 64)

	for name, header := range map[string]string{
		"the internal service token":  "Bearer " + internalToken,
		"the placeholder token":       "Bearer dev-internal-service-token-change-me",
		"a bare token":                internalToken,
		"basic auth":                  "Basic dXNlcjpwYXNz",
		"an empty bearer":             "Bearer ",
		"two segments":                "Bearer aaa.bbb",
		"four segments":               "Bearer aaa.bbb.ccc.ddd",
		"an empty segment":            "Bearer aaa..ccc",
		"a space inside the token":    "Bearer aaa.bbb.c cc",
		"a padded segment":            "Bearer aaa.bbb.ccc==",
		"a lower-case scheme":         "bearer " + testJWT,
		"a token followed by another": "Bearer " + testJWT + ", Bearer " + internalToken,
	} {
		recorder, seen := serve(t, map[string]string{"Authorization": header})

		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401, got %d", name, recorder.Code)
		}

		if seen != nil {
			t.Errorf("%s: the request reached the backends", name)
		}
	}
}

func TestTheRefusalIsAnAPIError(t *testing.T) {
	recorder, _ := serve(t, map[string]string{"Authorization": "Bearer " + strings.Repeat("f", 64)})

	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("content type: got %q", got)
	}

	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("the body is not JSON: %v", err)
	}

	if body.Code != 16 || body.Message == "" {
		t.Errorf("unexpected error body: %+v", body)
	}
}

func TestGRPCMetadataHeadersAreDropped(t *testing.T) {
	recorder, seen := serve(t, map[string]string{
		"Authorization":               "Bearer " + testJWT,
		"Grpc-Metadata-Authorization": "Bearer " + strings.Repeat("f", 64),
		"grpc-metadata-x-anything":    "1",
		"X-Request-Id":                "keep-me",
	})

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}

	for name := range seen {
		if strings.HasPrefix(strings.ToLower(name), "grpc-metadata-") {
			t.Errorf("%s reached the backends", name)
		}
	}

	if seen.Get("Authorization") != "Bearer "+testJWT || seen.Get("X-Request-Id") != "keep-me" {
		t.Errorf("ordinary headers must be untouched: %v", seen)
	}
}

func TestPreflightRequestsCarryNoCredentialsAndPass(t *testing.T) {
	request := httptest.NewRequest(http.MethodOptions, "/v1/trips", nil)
	recorder := httptest.NewRecorder()

	requireUserCredentials(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", recorder.Code)
	}
}

func TestPaymentProviderWebhooksAreNotHeldToTheUserTokenShape(t *testing.T) {
	var seen http.Header

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/wallet/zaincash/webhook", nil)
	request.Header.Set("Authorization", "Bearer provider-shared-secret")
	request.Header.Set("Grpc-Metadata-Authorization", "Bearer "+strings.Repeat("f", 64))

	recorder := httptest.NewRecorder()
	requireUserCredentials(next).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("the webhook must reach the wallet service, got %d", recorder.Code)
	}

	if seen.Get("Grpc-Metadata-Authorization") != "" {
		t.Error("Grpc-Metadata-* headers must be dropped on the webhook route too")
	}

	// Only that one path is exempt.
	other := httptest.NewRequest(http.MethodPost, "/v1/wallet/topups/zaincash", nil)
	other.Header.Set("Authorization", "Bearer provider-shared-secret")

	recorder = httptest.NewRecorder()
	requireUserCredentials(next).ServeHTTP(recorder, other)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("every other route must still refuse it, got %d", recorder.Code)
	}
}
