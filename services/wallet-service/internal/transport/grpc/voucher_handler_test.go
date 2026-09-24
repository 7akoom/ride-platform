package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	walletv1 "github.com/7akoom/ride-platform/gen/go/ride/wallet/v1"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/voucher"
	"github.com/7akoom/ride-platform/services/wallet-service/internal/application/wallet"
)

type fakeStaff struct {
	allowed   bool
	err       error
	asked     []string
	targets   []string
	completed []codes.Code
}

func (f *fakeStaff) Authorize(_ context.Context, _, permission, _, target string) (bool, string, error) {
	f.asked = append(f.asked, permission)
	f.targets = append(f.targets, target)

	return f.allowed, "audit-1", f.err
}

func (f *fakeStaff) Complete(_ context.Context, _ string, code codes.Code) {
	f.completed = append(f.completed, code)
}

var voucherAdminRequests = map[string]any{
	"CreateVoucherBatch": &walletv1.CreateVoucherBatchRequest{},
	"ListVoucherBatches": &walletv1.ListVoucherBatchesRequest{},
	"GetVoucherBatch":    &walletv1.GetVoucherBatchRequest{BatchId: "batch-1"},
	"ExportVoucherBatch": &walletv1.ExportVoucherBatchRequest{BatchId: "batch-1"},
	"CancelVoucherBatch": &walletv1.CancelVoucherBatchRequest{BatchId: "batch-1"},
	"GetVoucher":         &walletv1.GetVoucherRequest{Serial: "V7-00001"},
	"VoidVoucher":        &walletv1.VoidVoucherRequest{Serial: "V7-00001"},
}

func callAsStaff(staff StaffAuthorizer, method string, request any) (bool, codes.Code) {
	interceptor := NewAuthorizationUnaryInterceptor(ownershipTestProfiles, staff)

	ctx := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{
		IdentityID: "identity-staff", SessionID: "session-1",
	})

	called := false

	_, err := interceptor(ctx, request, &googlegrpc.UnaryServerInfo{FullMethod: walletRPCPrefix + method}, func(context.Context, any) (any, error) {
		called = true

		return nil, status.Error(codes.NotFound, "handler ran")
	})

	return called, status.Code(err)
}

func TestVoucherAdministrationNeedsVouchersManage(t *testing.T) {
	for method, request := range voucherAdminRequests {
		if called, code := callAsStaff(nil, method, request); called || code != codes.PermissionDenied {
			t.Errorf("%s without staff-service: called=%v %v", method, called, code)
		}

		denied := &fakeStaff{}
		if called, code := callAsStaff(denied, method, request); called || code != codes.PermissionDenied || denied.asked[0] != "vouchers.manage" {
			t.Errorf("%s denied: called=%v %v %v", method, called, code, denied.asked)
		}

		down := &fakeStaff{err: errors.New("unavailable")}
		if called, code := callAsStaff(down, method, request); called || code != codes.Unavailable {
			t.Errorf("%s with staff-service down: called=%v %v", method, called, code)
		}

		allowed := &fakeStaff{allowed: true}
		called, code := callAsStaff(allowed, method, request)

		if !called || code != codes.NotFound || len(allowed.completed) != 1 || allowed.completed[0] != codes.NotFound {
			t.Errorf("%s allowed: called=%v %v completed=%v", method, called, code, allowed.completed)
		}

		if target := staffTargetOf(request); allowed.targets[0] != target {
			t.Errorf("%s audited target %q, want %q", method, allowed.targets[0], target)
		}
	}
}

func TestARiderRedeemsIntoTheirOwnWalletOnly(t *testing.T) {
	method := walletRPCPrefix + "RedeemVoucher"

	if code := callOwnershipAs(t, "identity-rider-a", ownershipTestProfiles, method, &walletv1.RedeemVoucherRequest{RiderId: "rider-a"}); code != codes.OK {
		t.Fatalf("own wallet: %v", code)
	}

	if code := callOwnershipAs(t, "identity-rider-a", ownershipTestProfiles, method, &walletv1.RedeemVoucherRequest{RiderId: "rider-b"}); code != codes.PermissionDenied {
		t.Fatalf("another rider's wallet: %v", code)
	}

	if code := callOwnershipAs(t, "identity-driver-a", ownershipTestProfiles, method, &walletv1.RedeemVoucherRequest{RiderId: "driver-a"}); code != codes.PermissionDenied {
		t.Fatalf("a driver: %v", code)
	}
}

type emptyVoucherStore struct{ voucher.Store }

func TestTheInternalTokenIssuesNoVouchers(t *testing.T) {
	codec, err := voucher.NewCodec("unit-test-key")
	if err != nil {
		t.Fatal(err)
	}

	handler := (&WalletHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}).
		WithVouchers(voucher.NewService(emptyVoucherStore{}, codec, voucher.Limits{MaxFailures: 5, Window: time.Hour}))
	internal := contextWithAuthenticatedPrincipal(context.Background(), authenticatedPrincipal{IdentityID: internalServicePrincipalID, SessionID: internalServicePrincipalID})

	if _, err := handler.CreateVoucherBatch(internal, &walletv1.CreateVoucherBatchRequest{Amount: "1000"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("create: %v", err)
	}

	if _, err := handler.ExportVoucherBatch(internal, &walletv1.ExportVoucherBatchRequest{BatchId: "b"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("export: %v", err)
	}

	if _, err := handler.CancelVoucherBatch(internal, &walletv1.CancelVoucherBatchRequest{BatchId: "b"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cancel: %v", err)
	}

	if _, err := handler.VoidVoucher(internal, &walletv1.VoidVoucherRequest{Serial: "V1-00001"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("void: %v", err)
	}

	if _, err := (&WalletHandler{}).RedeemVoucher(internal, &walletv1.RedeemVoucherRequest{}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("without vouchers: %v", err)
	}
}

func TestVoucherErrorsReachTheCallerAsTheyShould(t *testing.T) {
	handler := &WalletHandler{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	for err, want := range map[error]codes.Code{
		&voucher.TooManyAttemptsError{Until: time.Now()}: codes.ResourceExhausted,
		voucher.ErrCodeNotValid:                          codes.NotFound,
		voucher.ErrBatchNotFound:                         codes.NotFound,
		voucher.ErrVoucherNotFound:                       codes.NotFound,
		voucher.ErrCodeUsed:                              codes.FailedPrecondition,
		voucher.ErrCodeCancelled:                         codes.FailedPrecondition,
		voucher.ErrCodeExpired:                           codes.FailedPrecondition,
		voucher.ErrNotExportable:                         codes.FailedPrecondition,
		voucher.ErrNotCancellable:                        codes.FailedPrecondition,
		voucher.ErrNotVoidable:                           codes.FailedPrecondition,
		voucher.ErrInvalidCode:                           codes.InvalidArgument,
		voucher.ErrInvalidQuantity:                       codes.InvalidArgument,
		voucher.ErrReasonRequired:                        codes.InvalidArgument,
		voucher.ErrKeyReused:                             codes.AlreadyExists,
		voucher.ErrStaffRequired:                         codes.PermissionDenied,
		wallet.ErrDuplicateRequest:                       codes.AlreadyExists,
		errors.New("database down"):                      codes.Internal,
	} {
		if got := status.Code(handler.mapVoucherError(err)); got != want {
			t.Errorf("%v: got %v, want %v", err, got, want)
		}
	}
}
