package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type testWebhookHandler struct {
	calls int
}

func (h *testWebhookHandler) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	h.calls++

	writer.WriteHeader(
		http.StatusNoContent,
	)
}

func TestWebhookRouterRoutesPOSTToProviderHandler(
	t *testing.T,
) {
	handler := &testWebhookHandler{}

	router, err := NewWebhookRouter(
		[]WebhookRoute{
			{
				Provider: "bulksmsiraq",
				Handler:  handler,
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"NewWebhookRouter() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/bulksmsiraq",
		nil,
	)

	recorder := httptest.NewRecorder()

	router.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf(
			"response status = %d, want %d",
			recorder.Code,
			http.StatusNoContent,
		)
	}

	if handler.calls != 1 {
		t.Fatalf(
			"handler calls = %d, want 1",
			handler.calls,
		)
	}
}

func TestWebhookRouterRejectsNonPOSTMethod(
	t *testing.T,
) {
	handler := &testWebhookHandler{}

	router, err := NewWebhookRouter(
		[]WebhookRoute{
			{
				Provider: "bulksmsiraq",
				Handler:  handler,
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"NewWebhookRouter() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/webhooks/otp/bulksmsiraq",
		nil,
	)

	recorder := httptest.NewRecorder()

	router.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code !=
		http.StatusMethodNotAllowed {
		t.Fatalf(
			"response status = %d, want %d",
			recorder.Code,
			http.StatusMethodNotAllowed,
		)
	}

	if recorder.Header().Get("Allow") !=
		http.MethodPost {
		t.Fatalf(
			"Allow header = %q, want %q",
			recorder.Header().Get("Allow"),
			http.MethodPost,
		)
	}

	if handler.calls != 0 {
		t.Fatalf(
			"handler calls = %d, want 0",
			handler.calls,
		)
	}
}

func TestWebhookRouterReturnsNotFoundForUnknownProvider(
	t *testing.T,
) {
	handler := &testWebhookHandler{}

	router, err := NewWebhookRouter(
		[]WebhookRoute{
			{
				Provider: "bulksmsiraq",
				Handler:  handler,
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"NewWebhookRouter() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/unknown",
		nil,
	)

	recorder := httptest.NewRecorder()

	router.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf(
			"response status = %d, want %d",
			recorder.Code,
			http.StatusNotFound,
		)
	}

	if handler.calls != 0 {
		t.Fatalf(
			"handler calls = %d, want 0",
			handler.calls,
		)
	}
}

func TestWebhookRouterRejectsDuplicateProvider(
	t *testing.T,
) {
	_, err := NewWebhookRouter(
		[]WebhookRoute{
			{
				Provider: "bulksmsiraq",
				Handler:  &testWebhookHandler{},
			},
			{
				Provider: " BULKSMSIRAQ ",
				Handler:  &testWebhookHandler{},
			},
		},
	)

	if err == nil {
		t.Fatal(
			"NewWebhookRouter() accepted duplicate provider",
		)
	}
}

func TestWebhookRouterRejectsBlankProvider(
	t *testing.T,
) {
	_, err := NewWebhookRouter(
		[]WebhookRoute{
			{
				Provider: "   ",
				Handler:  &testWebhookHandler{},
			},
		},
	)

	if err == nil {
		t.Fatal(
			"NewWebhookRouter() accepted blank provider",
		)
	}
}

func TestWebhookRouterRejectsNilHandler(
	t *testing.T,
) {
	_, err := NewWebhookRouter(
		[]WebhookRoute{
			{
				Provider: "bulksmsiraq",
				Handler:  nil,
			},
		},
	)

	if err == nil {
		t.Fatal(
			"NewWebhookRouter() accepted nil handler",
		)
	}
}

func TestWebhookRouterNormalizesProviderName(
	t *testing.T,
) {
	handler := &testWebhookHandler{}

	router, err := NewWebhookRouter(
		[]WebhookRoute{
			{
				Provider: " BULKSMSIRAQ ",
				Handler:  handler,
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"NewWebhookRouter() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/bulksmsiraq",
		nil,
	)

	recorder := httptest.NewRecorder()

	router.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf(
			"response status = %d, want %d",
			recorder.Code,
			http.StatusNoContent,
		)
	}

	if handler.calls != 1 {
		t.Fatalf(
			"handler calls = %d, want 1",
			handler.calls,
		)
	}
}
func TestWebhookRouterKeepsPOSTOnlyRouteRejectingGET(
	t *testing.T,
) {
	postHandler := &methodAwareTestWebhookHandler{
		status: http.StatusNoContent,
	}

	router, err := NewWebhookRouter(
		[]WebhookRoute{
			{
				Provider: "telnyx",
				Handler:  postHandler,
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"NewWebhookRouter() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/webhooks/otp/telnyx",
		nil,
	)

	recorder := httptest.NewRecorder()

	router.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusMethodNotAllowed,
		)
	}

	if recorder.Header().Get("Allow") != http.MethodPost {
		t.Fatalf(
			"Allow = %q, want %q",
			recorder.Header().Get("Allow"),
			http.MethodPost,
		)
	}

	if postHandler.calls != 0 {
		t.Fatalf(
			"POST handler calls = %d, want 0",
			postHandler.calls,
		)
	}
}

func TestWebhookRouterRoutesGETToConfiguredGetHandler(
	t *testing.T,
) {
	postHandler := &methodAwareTestWebhookHandler{
		status: http.StatusNoContent,
	}

	getHandler := &methodAwareTestWebhookHandler{
		status: http.StatusOK,
	}

	router, err := NewWebhookRouter(
		[]WebhookRoute{
			{
				Provider:   "meta",
				Handler:    postHandler,
				GetHandler: getHandler,
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"NewWebhookRouter() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/webhooks/otp/meta",
		nil,
	)

	recorder := httptest.NewRecorder()

	router.ServeHTTP(
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

	if getHandler.calls != 1 {
		t.Fatalf(
			"GET handler calls = %d, want 1",
			getHandler.calls,
		)
	}

	if postHandler.calls != 0 {
		t.Fatalf(
			"POST handler calls = %d, want 0",
			postHandler.calls,
		)
	}
}

func TestWebhookRouterRoutesPOSTWhenGetHandlerExists(
	t *testing.T,
) {
	postHandler := &methodAwareTestWebhookHandler{
		status: http.StatusNoContent,
	}

	getHandler := &methodAwareTestWebhookHandler{
		status: http.StatusOK,
	}

	router, err := NewWebhookRouter(
		[]WebhookRoute{
			{
				Provider:   "meta",
				Handler:    postHandler,
				GetHandler: getHandler,
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"NewWebhookRouter() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/otp/meta",
		nil,
	)

	recorder := httptest.NewRecorder()

	router.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusNoContent,
		)
	}

	if postHandler.calls != 1 {
		t.Fatalf(
			"POST handler calls = %d, want 1",
			postHandler.calls,
		)
	}

	if getHandler.calls != 0 {
		t.Fatalf(
			"GET handler calls = %d, want 0",
			getHandler.calls,
		)
	}
}

func TestWebhookRouterAdvertisesGETAndPOSTWhenBothConfigured(
	t *testing.T,
) {
	router, err := NewWebhookRouter(
		[]WebhookRoute{
			{
				Provider: "meta",
				Handler: &methodAwareTestWebhookHandler{
					status: http.StatusNoContent,
				},
				GetHandler: &methodAwareTestWebhookHandler{
					status: http.StatusOK,
				},
			},
		},
	)
	if err != nil {
		t.Fatalf(
			"NewWebhookRouter() returned an error: %v",
			err,
		)
	}

	request := httptest.NewRequest(
		http.MethodDelete,
		"/webhooks/otp/meta",
		nil,
	)

	recorder := httptest.NewRecorder()

	router.ServeHTTP(
		recorder,
		request,
	)

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf(
			"status = %d, want %d",
			recorder.Code,
			http.StatusMethodNotAllowed,
		)
	}

	if recorder.Header().Get("Allow") != "GET, POST" {
		t.Fatalf(
			"Allow = %q, want %q",
			recorder.Header().Get("Allow"),
			"GET, POST",
		)
	}
}

type methodAwareTestWebhookHandler struct {
	calls  int
	status int
}

func (h *methodAwareTestWebhookHandler) ServeHTTP(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	h.calls++

	writer.WriteHeader(
		h.status,
	)
}
