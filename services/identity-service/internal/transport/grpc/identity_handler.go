package grpc

import (
	identityv1 "github.com/7akoom/ride-platform/gen/go/ride/identity/v1"
	"github.com/7akoom/ride-platform/services/identity-service/internal/application/auth"
)

type IdentityHandler struct {
	identityv1.UnimplementedIdentityServiceServer

	authService auth.Service
}

func NewIdentityHandler(
	authService auth.Service,
) *IdentityHandler {
	if authService == nil {
		panic("auth service is required")
	}

	return &IdentityHandler{
		authService: authService,
	}
}
