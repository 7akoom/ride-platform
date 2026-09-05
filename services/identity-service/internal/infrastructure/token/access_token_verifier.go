package token

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrAccessTokenRevoked = errors.New(
	"access token session has been revoked",
)

type SessionAccessRevocationChecker interface {
	IsRevoked(
		ctx context.Context,
		sessionID string,
	) (bool, error)
}

type AccessTokenVerificationKey struct {
	KeyID         string
	PublicKeyPath string
}

type AccessTokenVerifier struct {
	publicKeys        map[string]ed25519.PublicKey
	issuer            string
	audience          string
	revocationChecker SessionAccessRevocationChecker
	metricsRecorder   AccessTokenVerificationMetricsRecorder
}

type AccessTokenVerifierOption func(
	*AccessTokenVerifier,
) error

func WithAccessTokenVerificationMetricsRecorder(
	recorder AccessTokenVerificationMetricsRecorder,
) AccessTokenVerifierOption {
	return func(
		verifier *AccessTokenVerifier,
	) error {
		if recorder == nil {
			return errors.New(
				"access token verification metrics recorder is required",
			)
		}

		verifier.metricsRecorder = recorder

		return nil
	}
}

func NewAccessTokenVerifier(
	publicKeyPath string,
	issuer string,
	audience string,
	keyID string,
	revocationChecker SessionAccessRevocationChecker,
	options ...AccessTokenVerifierOption,
) (*AccessTokenVerifier, error) {
	return NewAccessTokenVerifierWithKeyring(
		[]AccessTokenVerificationKey{
			{
				KeyID:         keyID,
				PublicKeyPath: publicKeyPath,
			},
		},
		issuer,
		audience,
		revocationChecker,
		options...,
	)
}

func NewAccessTokenVerifierWithKeyring(
	keys []AccessTokenVerificationKey,
	issuer string,
	audience string,
	revocationChecker SessionAccessRevocationChecker,
	options ...AccessTokenVerifierOption,
) (*AccessTokenVerifier, error) {
	if len(keys) == 0 {
		return nil, errors.New(
			"access token verification keyring cannot be empty",
		)
	}

	if issuer == "" {
		return nil, errors.New(
			"access token issuer cannot be empty",
		)
	}

	if audience == "" {
		return nil, errors.New(
			"access token audience cannot be empty",
		)
	}

	if revocationChecker == nil {
		return nil, errors.New(
			"session access revocation checker is required",
		)
	}

	publicKeys := make(
		map[string]ed25519.PublicKey,
		len(keys),
	)

	for _, key := range keys {
		if key.KeyID == "" {
			return nil, errors.New(
				"access token verification key ID cannot be empty",
			)
		}

		if key.PublicKeyPath == "" {
			return nil, fmt.Errorf(
				"access token public key path cannot be empty for key ID %q",
				key.KeyID,
			)
		}

		if _, exists := publicKeys[key.KeyID]; exists {
			return nil, fmt.Errorf(
				"duplicate access token verification key ID %q",
				key.KeyID,
			)
		}

		publicKeyPEM, err := os.ReadFile(
			key.PublicKeyPath,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"read access token public key for key ID %q: %w",
				key.KeyID,
				err,
			)
		}

		parsedPublicKey, err :=
			jwt.ParseEdPublicKeyFromPEM(
				publicKeyPEM,
			)
		if err != nil {
			return nil, fmt.Errorf(
				"parse access token public key for key ID %q: %w",
				key.KeyID,
				err,
			)
		}

		publicKey, ok :=
			parsedPublicKey.(ed25519.PublicKey)
		if !ok {
			return nil, fmt.Errorf(
				"access token public key for key ID %q is not an Ed25519 public key",
				key.KeyID,
			)
		}

		publicKeys[key.KeyID] = publicKey
	}

	verifier := &AccessTokenVerifier{
		publicKeys:        publicKeys,
		issuer:            issuer,
		audience:          audience,
		revocationChecker: revocationChecker,
		metricsRecorder:   newNoopAccessTokenVerificationMetricsRecorder(),
	}

	for _, option := range options {
		if option == nil {
			return nil, errors.New(
				"access token verifier option cannot be nil",
			)
		}

		if err := option(
			verifier,
		); err != nil {
			return nil, err
		}
	}

	return verifier, nil
}

func (v *AccessTokenVerifier) Verify(
	ctx context.Context,
	rawToken string,
) (AccessTokenClaims, error) {
	startedAt := time.Now()

	recordOutcome := func(
		outcome AccessTokenVerificationMetricOutcome,
	) {
		if v.metricsRecorder == nil {
			return
		}

		v.metricsRecorder.RecordAccessTokenVerification(
			ctx,
			outcome,
			time.Since(startedAt),
		)
	}

	if rawToken == "" {
		recordOutcome(
			AccessTokenVerificationMetricOutcomeRejected,
		)

		return AccessTokenClaims{}, errors.New(
			"access token cannot be empty",
		)
	}

	claims := AccessTokenClaims{}

	parsedToken, err := jwt.ParseWithClaims(
		rawToken,
		&claims,
		func(parsedToken *jwt.Token) (any, error) {
			if parsedToken.Method.Alg() !=
				jwt.SigningMethodEdDSA.Alg() {
				return nil, fmt.Errorf(
					"unexpected access token signing method: %s",
					parsedToken.Method.Alg(),
				)
			}

			keyID, ok :=
				parsedToken.Header["kid"].(string)
			if !ok || keyID == "" {
				return nil, errors.New(
					"access token key ID is missing",
				)
			}

			publicKey, found :=
				v.publicKeys[keyID]
			if !found {
				return nil, errors.New(
					"access token key ID is invalid",
				)
			}

			return publicKey, nil
		},
		jwt.WithValidMethods(
			[]string{
				jwt.SigningMethodEdDSA.Alg(),
			},
		),
		jwt.WithStrictDecoding(),
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		recordOutcome(
			AccessTokenVerificationMetricOutcomeRejected,
		)

		return AccessTokenClaims{}, fmt.Errorf(
			"verify access token: %w",
			err,
		)
	}

	if parsedToken == nil ||
		!parsedToken.Valid {
		recordOutcome(
			AccessTokenVerificationMetricOutcomeRejected,
		)

		return AccessTokenClaims{}, errors.New(
			"access token is invalid",
		)
	}

	if claims.Subject == "" {
		recordOutcome(
			AccessTokenVerificationMetricOutcomeRejected,
		)

		return AccessTokenClaims{}, errors.New(
			"access token subject is missing",
		)
	}

	if claims.SessionID == "" {
		recordOutcome(
			AccessTokenVerificationMetricOutcomeRejected,
		)

		return AccessTokenClaims{}, errors.New(
			"access token session ID is missing",
		)
	}

	if claims.IssuedAt == nil {
		recordOutcome(
			AccessTokenVerificationMetricOutcomeRejected,
		)

		return AccessTokenClaims{}, errors.New(
			"access token issued-at claim is missing",
		)
	}

	if claims.ID == "" {
		recordOutcome(
			AccessTokenVerificationMetricOutcomeRejected,
		)

		return AccessTokenClaims{}, errors.New(
			"access token ID is missing",
		)
	}

	revoked, err :=
		v.revocationChecker.IsRevoked(
			ctx,
			claims.SessionID,
		)
	if err != nil {
		recordOutcome(
			AccessTokenVerificationMetricOutcomeFailed,
		)

		return AccessTokenClaims{}, fmt.Errorf(
			"check access token session revocation: %w",
			err,
		)
	}

	if revoked {
		recordOutcome(
			AccessTokenVerificationMetricOutcomeRejected,
		)

		return AccessTokenClaims{},
			ErrAccessTokenRevoked
	}

	recordOutcome(
		AccessTokenVerificationMetricOutcomeSuccess,
	)

	return claims, nil
}
