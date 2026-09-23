package grpc

import (
	"context"
	"errors"
	"testing"

	mediav1 "github.com/7akoom/ride-platform/gen/go/ride/media/v1"
	"github.com/7akoom/ride-platform/services/media-service/internal/application/media"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	fileOfA  = "00000000-0000-4000-8000-00000000000a"
	missing  = "00000000-0000-4000-8000-0000000000ff"
	brokenDB = "00000000-0000-4000-8000-0000000000ee"
	userA    = "11111111-1111-4111-8111-111111111111"
	userB    = "22222222-2222-4222-8222-222222222222"
)

type fakeFiles struct{}

func (fakeFiles) Get(_ context.Context, id string) (media.Media, error) {
	switch id {
	case fileOfA:
		return media.Media{ID: id, OwnerIdentityID: userA}, nil
	case brokenDB:
		return media.Media{}, errors.New("connection refused")
	case "not-a-uuid":
		return media.Media{}, media.ErrInvalidID
	default:
		return media.Media{}, media.ErrNotFound
	}
}

type fakeStaff struct {
	allowed   bool
	err       error
	asked     []string
	completed map[string]codes.Code
}

func (f *fakeStaff) Authorize(_ context.Context, identityID, permission, method, targetID string) (bool, string, error) {
	f.asked = append(f.asked, identityID+" "+permission+" "+method+" "+targetID)

	if f.err != nil {
		return false, "", f.err
	}

	return f.allowed, "audit-1", nil
}

func (f *fakeStaff) Complete(_ context.Context, auditEntryID string, code codes.Code) {
	if f.completed == nil {
		f.completed = map[string]codes.Code{}
	}

	f.completed[auditEntryID] = code
}

type call struct {
	ran       bool
	staffCall bool
	err       error
}

func invoke(t *testing.T, staff StaffAuthorizer, identityID, method string, request any) call {
	t.Helper()

	ctx := context.Background()
	if identityID != "" {
		ctx = contextWithAuthenticatedPrincipal(ctx, authenticatedPrincipal{IdentityID: identityID, SessionID: "s"})
	}

	var result call

	_, result.err = NewAuthorizationUnaryInterceptor(fakeFiles{}, staff)(
		ctx,
		request,
		&googlegrpc.UnaryServerInfo{FullMethod: mediaRPCPrefix + method},
		func(ctx context.Context, _ any) (any, error) {
			result.ran = true
			result.staffCall = isStaffCall(ctx)

			return nil, nil
		},
	)

	return result
}

func TestUnauthenticatedCallsAreRefused(t *testing.T) {
	got := invoke(t, &fakeStaff{allowed: true}, "", "GetMedia", &mediav1.GetMediaRequest{MediaId: fileOfA})
	if got.ran || status.Code(got.err) != codes.Unauthenticated {
		t.Fatalf("got %+v", got)
	}
}

func TestAnyUserMayStartAnUpload(t *testing.T) {
	got := invoke(t, nil, userB, "CreateUpload", &mediav1.CreateUploadRequest{})
	if !got.ran || got.err != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestOwnerMethods(t *testing.T) {
	methods := map[string]any{
		"CompleteUpload": &mediav1.CompleteUploadRequest{MediaId: fileOfA},
		"GetMedia":       &mediav1.GetMediaRequest{MediaId: fileOfA},
		"GetDownloadURL": &mediav1.GetDownloadURLRequest{MediaId: fileOfA},
		"DeleteMedia":    &mediav1.DeleteMediaRequest{MediaId: fileOfA},
	}

	for method, request := range methods {
		t.Run(method, func(t *testing.T) {
			if got := invoke(t, nil, userA, method, request); !got.ran || got.staffCall || got.err != nil {
				t.Fatalf("owner: %+v", got)
			}

			// Without a staff permission to fall back on, someone else is refused.
			if got := invoke(t, &fakeStaff{allowed: false}, userB, method, request); got.ran || status.Code(got.err) != codes.PermissionDenied {
				t.Fatalf("another user: %+v", got)
			}
		})
	}
}

func TestAMissingFileLooksLikeSomeoneElses(t *testing.T) {
	for _, id := range []string{missing, "not-a-uuid"} {
		got := invoke(t, nil, userA, "GetMedia", &mediav1.GetMediaRequest{MediaId: id})
		if got.ran || status.Code(got.err) != codes.PermissionDenied {
			t.Fatalf("id %s: got %+v", id, got)
		}
	}
}

func TestOwnershipLookupFailureFailsClosed(t *testing.T) {
	got := invoke(t, nil, userA, "GetMedia", &mediav1.GetMediaRequest{MediaId: brokenDB})
	if got.ran || status.Code(got.err) != codes.Unavailable {
		t.Fatalf("got %+v", got)
	}
}

func TestStaffMayReadButNotChangeOthersFiles(t *testing.T) {
	for _, method := range []string{"GetMedia", "GetDownloadURL"} {
		staff := &fakeStaff{allowed: true}

		got := invoke(t, staff, userB, method, &mediav1.GetMediaRequest{MediaId: fileOfA})
		if !got.ran || !got.staffCall || got.err != nil {
			t.Fatalf("%s: got %+v", method, got)
		}

		want := userB + " media.read " + mediaRPCPrefix + method + " " + fileOfA
		if len(staff.asked) != 1 || staff.asked[0] != want {
			t.Fatalf("%s: asked %v", method, staff.asked)
		}

		if staff.completed["audit-1"] != codes.OK {
			t.Fatalf("%s: outcome not recorded", method)
		}
	}

	for _, method := range []string{"CompleteUpload", "DeleteMedia"} {
		staff := &fakeStaff{allowed: true}

		got := invoke(t, staff, userB, method, &mediav1.DeleteMediaRequest{MediaId: fileOfA})
		if got.ran || status.Code(got.err) != codes.PermissionDenied || len(staff.asked) != 0 {
			t.Fatalf("%s: got %+v, asked %v", method, got, staff.asked)
		}
	}
}

func TestStaffCheckFailsClosed(t *testing.T) {
	cases := map[string]StaffAuthorizer{
		"no authorizer":          nil,
		"staff-service down":     &fakeStaff{err: errors.New("unavailable")},
		"permission not granted": &fakeStaff{allowed: false},
	}

	for name, staff := range cases {
		got := invoke(t, staff, userB, "GetDownloadURL", &mediav1.GetDownloadURLRequest{MediaId: fileOfA})
		if got.ran {
			t.Fatalf("%s: the handler ran", name)
		}

		if code := status.Code(got.err); code != codes.PermissionDenied && code != codes.Unavailable {
			t.Fatalf("%s: got %v", name, got.err)
		}
	}
}

func TestOnlyServicesHoldAndRelease(t *testing.T) {
	for _, method := range []string{"HoldMedia", "ReleaseMedia"} {
		// Not even the owner: holding their own file would stop nothing.
		if got := invoke(t, &fakeStaff{allowed: true}, userA, method, &mediav1.HoldMediaRequest{MediaId: fileOfA}); got.ran {
			t.Fatalf("%s ran for a user", method)
		}

		if got := invoke(t, nil, internalServicePrincipalID, method, &mediav1.HoldMediaRequest{MediaId: fileOfA}); !got.ran {
			t.Fatalf("%s refused the internal caller: %v", method, got.err)
		}
	}
}

func TestUnknownMethodsAreRefused(t *testing.T) {
	got := invoke(t, &fakeStaff{allowed: true}, userA, "DropAllFiles", &mediav1.GetMediaRequest{MediaId: fileOfA})
	if got.ran || status.Code(got.err) != codes.PermissionDenied {
		t.Fatalf("got %+v", got)
	}
}

func TestCreateUploadRefusesServices(t *testing.T) {
	handler := &MediaHandler{}
	ctx := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{IdentityID: internalServicePrincipalID, SessionID: "s"})

	_, err := handler.CreateUpload(ctx, &mediav1.CreateUploadRequest{Purpose: mediav1.MediaPurpose_MEDIA_PURPOSE_PROFILE_PHOTO})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("got %v", err)
	}
}

func TestEveryRPCIsClassified(t *testing.T) {
	desc := mediav1.MediaService_ServiceDesc
	prefix := "/" + desc.ServiceName + "/"

	if prefix != mediaRPCPrefix {
		t.Fatalf("prefix %q != %q", prefix, mediaRPCPrefix)
	}

	registered := map[string]struct{}{}

	for _, method := range desc.Methods {
		full := prefix + method.MethodName
		registered[full] = struct{}{}

		if _, classified := methodAccess[full]; !classified {
			t.Errorf("%s has no access level in methodAccess", full)
		}
	}

	for full, level := range methodAccess {
		if _, ok := registered[full]; !ok {
			t.Errorf("methodAccess lists %s, which is not an RPC", full)
		}

		if _, staffMay := staffPermissions[full]; staffMay && level != accessOwner {
			t.Errorf("%s has a staff permission but is not accessOwner", full)
		}
	}
}

func TestEveryOwnerRequestNamesAFile(t *testing.T) {
	requests := map[string]any{
		"CompleteUpload": &mediav1.CompleteUploadRequest{},
		"GetMedia":       &mediav1.GetMediaRequest{},
		"GetDownloadURL": &mediav1.GetDownloadURLRequest{},
		"DeleteMedia":    &mediav1.DeleteMediaRequest{},
	}

	for full, level := range methodAccess {
		if level != accessOwner {
			continue
		}

		request, known := requests[full[len(mediaRPCPrefix):]]
		if !known {
			t.Errorf("%s is accessOwner; add its request here", full)

			continue
		}

		if _, ok := request.(mediaRequest); !ok {
			t.Errorf("%s: its request has no media_id to check ownership with", full)
		}
	}
}

func TestProtoMappingsCoverEveryValue(t *testing.T) {
	for value := range mediav1.MediaPurpose_name {
		purpose := mediav1.MediaPurpose(value)
		if purpose == mediav1.MediaPurpose_MEDIA_PURPOSE_UNSPECIFIED {
			if _, ok := purposeFromProto(purpose); ok {
				t.Error("UNSPECIFIED must not map to a purpose")
			}

			continue
		}

		domain, ok := purposeFromProto(purpose)
		if !ok || purposeToProto(domain) != purpose {
			t.Errorf("%s does not round-trip", purpose)
		}

		if _, hasPolicy := media.PolicyFor(domain); !hasPolicy {
			t.Errorf("%s has no policy", purpose)
		}
	}

	for _, s := range []media.Status{media.StatusPending, media.StatusReady, media.StatusRejected, media.StatusDeleted, media.StatusExpired} {
		if statusesToProto[s] == mediav1.MediaStatus_MEDIA_STATUS_UNSPECIFIED {
			t.Errorf("status %s has no proto value", s)
		}
	}
}
