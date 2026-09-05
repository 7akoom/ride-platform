package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/config"
	"github.com/7akoom/ride-platform/services/identity-service/internal/infrastructure/otp"
)

type testOTPWebhookReceiptStore struct{}

func (testOTPWebhookReceiptStore) ApplyReceipt(
	context.Context,
	otp.DeliveryReceiptInput,
) error {
	return nil
}

func TestBuildOTPWebhookServerReturnsNilWhenNoWebhookProviderConfigured(
	t *testing.T,
) {
	server, err := buildOTPWebhookServer(
		config.Config{
			OTPWebhookAddress: ":8081",
		},
		testOTPWebhookReceiptStore{},
	)
	if err != nil {
		t.Fatalf(
			"buildOTPWebhookServer() returned an error: %v",
			err,
		)
	}

	if server != nil {
		t.Fatal(
			"buildOTPWebhookServer() returned server with no configured webhook provider",
		)
	}
}

func TestBuildOTPWebhookServerRequiresReceiptStoreForTelnyx(
	t *testing.T,
) {
	publicKey := testTelnyxWebhookPublicKey(
		t,
	)

	server, err := buildOTPWebhookServer(
		config.Config{
			OTPWebhookAddress: ":8081",
			TelnyxPublicKey:   publicKey,
		},
		nil,
	)

	if err == nil {
		t.Fatal(
			"buildOTPWebhookServer() accepted nil receipt store",
		)
	}

	if server != nil {
		t.Fatal(
			"buildOTPWebhookServer() returned server with nil receipt store",
		)
	}
}

func TestBuildOTPWebhookServerRejectsInvalidTelnyxPublicKey(
	t *testing.T,
) {
	server, err := buildOTPWebhookServer(
		config.Config{
			OTPWebhookAddress: ":8081",
			TelnyxPublicKey:   "not-valid-base64",
		},
		testOTPWebhookReceiptStore{},
	)

	if err == nil {
		t.Fatal(
			"buildOTPWebhookServer() accepted invalid Telnyx public key",
		)
	}

	if server != nil {
		t.Fatal(
			"buildOTPWebhookServer() returned server with invalid Telnyx public key",
		)
	}
}

func TestBuildOTPWebhookServerRejectsBlankAddressWhenEnabled(
	t *testing.T,
) {
	publicKey := testTelnyxWebhookPublicKey(
		t,
	)

	server, err := buildOTPWebhookServer(
		config.Config{
			OTPWebhookAddress: "",
			TelnyxPublicKey:   publicKey,
		},
		testOTPWebhookReceiptStore{},
	)

	if err == nil {
		t.Fatal(
			"buildOTPWebhookServer() accepted blank webhook address",
		)
	}

	if server != nil {
		t.Fatal(
			"buildOTPWebhookServer() returned server with blank webhook address",
		)
	}
}

func TestBuildOTPWebhookServerBuildsTelnyxWebhookServer(
	t *testing.T,
) {
	publicKey := testTelnyxWebhookPublicKey(
		t,
	)

	server, err := buildOTPWebhookServer(
		config.Config{
			OTPWebhookAddress: ":8081",
			TelnyxPublicKey:   publicKey,
		},
		testOTPWebhookReceiptStore{},
	)
	if err != nil {
		t.Fatalf(
			"buildOTPWebhookServer() returned an error: %v",
			err,
		)
	}

	if server == nil {
		t.Fatal(
			"buildOTPWebhookServer() returned nil server",
		)
	}
}

func testTelnyxWebhookPublicKey(
	t *testing.T,
) string {
	t.Helper()

	publicKey, _, err :=
		ed25519.GenerateKey(
			rand.Reader,
		)
	if err != nil {
		t.Fatalf(
			"GenerateKey() returned an error: %v",
			err,
		)
	}

	return base64.StdEncoding.EncodeToString(
		publicKey,
	)
}
func TestBuildOTPWebhookServerRequiresMetaVerifyTokenWhenMetaConfigured(
	t *testing.T,
) {
	server, err := buildOTPWebhookServer(
		config.Config{
			OTPWebhookAddress: ":8081",
			MetaAppSecret:     "meta-secret",
		},
		testOTPWebhookReceiptStore{},
	)

	if err == nil {
		t.Fatal(
			"buildOTPWebhookServer() accepted Meta app secret without verify token",
		)
	}

	if server != nil {
		t.Fatal(
			"buildOTPWebhookServer() returned server for incomplete Meta configuration",
		)
	}
}

func TestBuildOTPWebhookServerRequiresMetaAppSecretWhenMetaConfigured(
	t *testing.T,
) {
	server, err := buildOTPWebhookServer(
		config.Config{
			OTPWebhookAddress:      ":8081",
			MetaWebhookVerifyToken: "verify-token",
		},
		testOTPWebhookReceiptStore{},
	)

	if err == nil {
		t.Fatal(
			"buildOTPWebhookServer() accepted Meta verify token without app secret",
		)
	}

	if server != nil {
		t.Fatal(
			"buildOTPWebhookServer() returned server for incomplete Meta configuration",
		)
	}
}

func TestBuildOTPWebhookServerRequiresReceiptStoreForMeta(
	t *testing.T,
) {
	server, err := buildOTPWebhookServer(
		config.Config{
			OTPWebhookAddress:      ":8081",
			MetaWebhookVerifyToken: "verify-token",
			MetaAppSecret:          "meta-secret",
		},
		nil,
	)

	if err == nil {
		t.Fatal(
			"buildOTPWebhookServer() accepted nil receipt store for Meta",
		)
	}

	if server != nil {
		t.Fatal(
			"buildOTPWebhookServer() returned server with nil receipt store for Meta",
		)
	}
}

func TestBuildOTPWebhookServerBuildsMetaWebhookServer(
	t *testing.T,
) {
	server, err := buildOTPWebhookServer(
		config.Config{
			OTPWebhookAddress:      ":8081",
			MetaWebhookVerifyToken: "verify-token",
			MetaAppSecret:          "meta-secret",
		},
		testOTPWebhookReceiptStore{},
	)
	if err != nil {
		t.Fatalf(
			"buildOTPWebhookServer() returned an error: %v",
			err,
		)
	}

	if server == nil {
		t.Fatal(
			"buildOTPWebhookServer() returned nil server for Meta configuration",
		)
	}
}

func TestBuildOTPWebhookServerBuildsTelnyxAndMetaTogether(
	t *testing.T,
) {
	server, err := buildOTPWebhookServer(
		config.Config{
			OTPWebhookAddress: ":8081",

			TelnyxPublicKey: testTelnyxWebhookPublicKey(
				t,
			),

			MetaWebhookVerifyToken: "verify-token",
			MetaAppSecret:          "meta-secret",
		},
		testOTPWebhookReceiptStore{},
	)
	if err != nil {
		t.Fatalf(
			"buildOTPWebhookServer() returned an error: %v",
			err,
		)
	}

	if server == nil {
		t.Fatal(
			"buildOTPWebhookServer() returned nil server with Telnyx and Meta configured",
		)
	}
}
