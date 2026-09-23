package grpc

import (
	"context"
	"errors"

	"github.com/7akoom/ride-platform/services/media-service/internal/application/media"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

type accessLevel int

const (
	accessInternal accessLevel = iota + 1
	accessOwner
	accessAuthenticated
)

const mediaRPCPrefix = "/ride.media.v1.MediaService/"

var exemptMethods = map[string]struct{}{
	healthv1.Health_Check_FullMethodName: {},
}

// methodAccess classifies every RPC. Methods missing from it are denied to end
// users. accessOwner methods need the caller to own the file, or (for those in
// staffPermissions) a staff member with that permission.
var methodAccess = map[string]accessLevel{
	mediaRPCPrefix + "CreateUpload":   accessAuthenticated,
	mediaRPCPrefix + "CompleteUpload": accessOwner,
	mediaRPCPrefix + "GetMedia":       accessOwner,
	mediaRPCPrefix + "GetDownloadURL": accessOwner,
	mediaRPCPrefix + "DeleteMedia":    accessOwner,

	// Called by the service a file is given to; an owner holding or releasing
	// their own file would defeat the point.
	mediaRPCPrefix + "HoldMedia":    accessInternal,
	mediaRPCPrefix + "ReleaseMedia": accessInternal,
}

// staffPermissions lets staff read files that are not theirs. Staff can never
// complete or delete someone else's upload.
var staffPermissions = map[string]string{
	mediaRPCPrefix + "GetMedia":       "media.read",
	mediaRPCPrefix + "GetDownloadURL": "media.read",
}

// MediaReader is the one lookup the owner check needs.
type MediaReader interface {
	Get(ctx context.Context, id string) (media.Media, error)
}

type mediaRequest interface {
	GetMediaId() string
}

func NewAuthorizationUnaryInterceptor(files MediaReader, staff StaffAuthorizer) googlegrpc.UnaryServerInterceptor {
	return newAuthorizationInterceptor(methodAccess, staffPermissions, files, staff)
}

func newAuthorizationInterceptor(
	levels map[string]accessLevel,
	permissions map[string]string,
	files MediaReader,
	staff StaffAuthorizer,
) googlegrpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		info *googlegrpc.UnaryServerInfo,
		handler googlegrpc.UnaryHandler,
	) (any, error) {
		if info == nil {
			return nil, status.Error(codes.Internal, "gRPC method information is required")
		}

		if _, exempt := exemptMethods[info.FullMethod]; exempt {
			return handler(ctx, request)
		}

		principal, ok := authenticatedPrincipalFromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "valid access token is required")
		}

		if principal.IdentityID == internalServicePrincipalID {
			return handler(ctx, request)
		}

		switch levels[info.FullMethod] {
		case accessAuthenticated:
			return handler(ctx, request)

		case accessOwner:
			owns, err := ownsMedia(ctx, files, principal.IdentityID, request)
			if err != nil {
				return nil, status.Error(codes.Unavailable, "ownership could not be verified")
			}

			if owns {
				return handler(ctx, request)
			}

			if permission, staffMay := permissions[info.FullMethod]; staffMay {
				return runAsStaff(ctx, staff, principal.IdentityID, permission, info, request, handler)
			}
		}

		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}
}

// ownsMedia reports whether the request names a file of the caller. A file
// that does not exist counts as not owned, so nobody can tell a missing file
// from someone else's.
func ownsMedia(ctx context.Context, files MediaReader, identityID string, request any) (bool, error) {
	r, ok := request.(mediaRequest)
	if !ok || files == nil || identityID == "" {
		return false, nil
	}

	found, err := files.Get(ctx, r.GetMediaId())
	if errors.Is(err, media.ErrNotFound) || errors.Is(err, media.ErrInvalidID) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return found.OwnerIdentityID == identityID, nil
}
