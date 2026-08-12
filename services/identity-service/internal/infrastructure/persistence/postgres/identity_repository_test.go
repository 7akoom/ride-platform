package postgres

import (
	"context"
	"testing"
)

func TestNewIdentityRepositoryPanicsWhenPoolIsNil(
	t *testing.T,
) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal(
				"NewIdentityRepository() did not panic for nil PostgreSQL pool",
			)
		}
	}()

	NewIdentityRepository(nil)
}

func TestIdentityRepositoryRejectsBlankPhoneNumber(
	t *testing.T,
) {
	tests := []struct {
		name        string
		phoneNumber string
	}{
		{
			name:        "empty",
			phoneNumber: "",
		},
		{
			name:        "spaces",
			phoneNumber: "   ",
		},
		{
			name:        "tabs and newlines",
			phoneNumber: "\t\n ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &IdentityRepository{}

			_, err := repository.FindOrCreateByPhoneNumber(
				context.Background(),
				tt.phoneNumber,
			)

			if err == nil {
				t.Fatal(
					"FindOrCreateByPhoneNumber() accepted a blank phone number",
				)
			}
		})
	}
}
