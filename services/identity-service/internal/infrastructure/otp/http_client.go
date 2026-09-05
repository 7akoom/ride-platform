package otp

import (
	"net/http"
)

type HTTPDoer interface {
	Do(
		request *http.Request,
	) (*http.Response, error)
}
