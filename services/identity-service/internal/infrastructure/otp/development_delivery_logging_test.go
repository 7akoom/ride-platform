package otp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

func TestDevelopmentDeliveryLogsCodeAndRecipientOnlyInAllowedEnvironments(t *testing.T) {
	for _, environment := range []string{"development", "test", "production", "staging", "", "unknown"} {
		t.Run(environment, func(t *testing.T) {
			var output bytes.Buffer
			delivery, err := NewDevelopmentDelivery(environment, slog.New(slog.NewJSONHandler(&output, nil)))
			if environment != "development" && environment != "test" {
				if err == nil || delivery != nil || output.Len() != 0 {
					t.Fatal("development delivery must be rejected without logs outside development/test")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			input := auth.OTPDeliveryInput{
				Identifier: auth.Identifier{Type: auth.IdentifierTypePhone, Value: "+9647501234567"},
				Code:       "123456", Purpose: auth.OTPPurposeLogin, Channel: auth.OTPDeliveryChannelSMS,
			}
			if err := delivery.Send(context.Background(), input); err != nil {
				t.Fatal("development delivery failed")
			}
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal("expected a structured development delivery log")
			}
			if record["otp_code"] != input.Code || record["otp_identifier"] != input.Identifier.Value {
				t.Fatal("development/test logs must preserve the code and recipient for manual OTP testing")
			}
			if record["msg"] != "OTP delivered through development adapter" || record["otp_purpose"] != string(input.Purpose) {
				t.Fatal("expected delivery message and purpose")
			}
		})
	}
}
