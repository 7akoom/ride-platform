package otp

import "testing"

func TestGeneratorGenerate(t *testing.T) {
	generator := NewGenerator()

	const iterations = 100

	for i := 0; i < iterations; i++ {
		code, err := generator.Generate()
		if err != nil {
			t.Fatalf("Generate() returned an error: %v", err)
		}

		if len(code) != otpDigits {
			t.Fatalf(
				"Generate() returned OTP with length %d, expected %d: %q",
				len(code),
				otpDigits,
				code,
			)
		}

		for _, char := range code {
			if char < '0' || char > '9' {
				t.Fatalf(
					"Generate() returned non-numeric OTP: %q",
					code,
				)
			}
		}

		if i == 0 {
			t.Logf("generated OTP sample: %s", code)
		}
	}
}