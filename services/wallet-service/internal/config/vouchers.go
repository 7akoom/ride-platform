package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// developmentVoucherCodeKey is the placeholder checked into the repo, so a
// fresh checkout issues vouchers locally. Anyone who has read the code knows it.
const developmentVoucherCodeKey = "dev-voucher-code-key-change-me"

// minVoucherCodeKeyLength is the shortest key accepted outside development.
// `openssl rand -hex 32` produces 64 characters.
const minVoucherCodeKeyLength = 32

// Vouchers is how codes are protected and how many wrong ones a rider may
// type in a window.
type Vouchers struct {
	CodeKey     string
	MaxFailures int
	Window      time.Duration
}

// ParseVouchers refuses a deployment that would issue vouchers with the
// published placeholder key (or a short one) outside development: with it, a
// copy of the database would be enough to find every code.
func ParseVouchers(cfg Config) (Vouchers, error) {
	key := cfg.VoucherCodeKey

	switch {
	case key == "":
		return Vouchers{}, errors.New("VOUCHER_CODE_KEY is empty")
	case cfg.Environment != "development" && key == developmentVoucherCodeKey:
		return Vouchers{}, fmt.Errorf(
			"VOUCHER_CODE_KEY is still the development placeholder while ENVIRONMENT is %q; "+
				"generate a secret (for example `openssl rand -hex 32`) and keep it: changing it later makes every issued voucher unredeemable",
			cfg.Environment,
		)
	case cfg.Environment != "development" && len(key) < minVoucherCodeKeyLength:
		return Vouchers{}, fmt.Errorf(
			"VOUCHER_CODE_KEY is shorter than %d characters while ENVIRONMENT is %q",
			minVoucherCodeKeyLength,
			cfg.Environment,
		)
	}

	failures, err := strconv.Atoi(strings.TrimSpace(cfg.VoucherRedeemMaxFailures))
	if err != nil || failures < 1 {
		return Vouchers{}, fmt.Errorf("VOUCHER_REDEEM_MAX_FAILURES must be a whole number of at least 1, got %q", cfg.VoucherRedeemMaxFailures)
	}

	window, err := time.ParseDuration(strings.TrimSpace(cfg.VoucherRedeemWindow))
	if err != nil || window < time.Minute {
		return Vouchers{}, fmt.Errorf("VOUCHER_REDEEM_WINDOW must be a duration of at least 1m, got %q", cfg.VoucherRedeemWindow)
	}

	return Vouchers{CodeKey: key, MaxFailures: failures, Window: window}, nil
}
