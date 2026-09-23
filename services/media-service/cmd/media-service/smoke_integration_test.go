package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	mediav1 "github.com/7akoom/ride-platform/gen/go/ride/media/v1"
	"github.com/7akoom/ride-platform/services/media-service/internal/application/media"
	clockinfra "github.com/7akoom/ride-platform/services/media-service/internal/infrastructure/clock"
	"github.com/7akoom/ride-platform/services/media-service/internal/infrastructure/database"
	"github.com/7akoom/ride-platform/services/media-service/internal/infrastructure/identifier"
	"github.com/7akoom/ride-platform/services/media-service/internal/infrastructure/objectstore"
	postgresrepo "github.com/7akoom/ride-platform/services/media-service/internal/infrastructure/persistence/postgres"
	"github.com/7akoom/ride-platform/services/media-service/internal/infrastructure/token"
	grpcserver "github.com/7akoom/ride-platform/services/media-service/internal/transport/grpc"
)

// TestSmoke wires the service as main does, against a real, EMPTY database
// and a real S3 endpoint, and walks an upload end to end:
//
//	MEDIA_TEST_DATABASE_URL=postgres://... \
//	MEDIA_TEST_S3_ENDPOINT=http://127.0.0.1:8333 \
//	MEDIA_TEST_S3_ACCESS_KEY=... MEDIA_TEST_S3_SECRET_KEY=... \
//	go test ./cmd/media-service/
//
// Staff-service is not needed: no staff permission is granted here.

const internalToken = "smoke-internal-token-0123456789abcdef"

type denyStaff struct{}

func (denyStaff) Authorize(context.Context, string, string, string, string) (bool, string, error) {
	return false, "", nil
}

func (denyStaff) Complete(context.Context, string, codes.Code) {}

func TestSmoke(t *testing.T) {
	databaseURL := os.Getenv("MEDIA_TEST_DATABASE_URL")
	endpoint := os.Getenv("MEDIA_TEST_S3_ENDPOINT")

	if databaseURL == "" || endpoint == "" {
		t.Skip("MEDIA_TEST_DATABASE_URL and MEDIA_TEST_S3_ENDPOINT are not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	applyMigration(t, databaseURL)

	pool, err := database.NewPostgresPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	store, err := objectstore.New(objectstore.Config{
		Endpoint:       endpoint,
		PublicEndpoint: endpoint,
		Region:         "us-east-1",
		Bucket:         "media-smoke-" + strings.ToLower(uuid.NewString()[:8]),
		AccessKey:      os.Getenv("MEDIA_TEST_S3_ACCESS_KEY"),
		SecretKey:      os.Getenv("MEDIA_TEST_S3_SECRET_KEY"),
	})
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := ensureBucket(ctx, store, logger); err != nil {
		t.Fatalf("bucket: %v", err)
	}

	service := media.NewService(
		postgresrepo.NewMediaRepository(pool),
		objectstore.MediaStore{Store: store},
		identifier.NewUUIDGenerator(),
		clockinfra.NewSystemClock(),
		media.Settings{
			UploadURLTTL:             time.Minute,
			DownloadURLTTL:           time.Minute,
			PendingTTL:               10 * time.Minute,
			MaxPendingPerOwner:       5,
			MaxConcurrentInspections: 1,
		},
		logger,
	)

	keyPath, privateKey := writeKey(t)

	verifier, err := token.NewAccessTokenVerifier(keyPath, "ride-identity", "ride-platform", "smoke-key")
	if err != nil {
		t.Fatal(err)
	}

	address := freeAddress(t)
	server := grpcserver.NewServer(
		address,
		logger,
		grpcserver.NewAuthenticationUnaryInterceptor(verifier, internalToken),
		grpcserver.NewRateLimitUnaryInterceptor(100, 100),
		grpcserver.NewAuthorizationUnaryInterceptor(service, denyStaff{}),
	)
	server.RegisterMediaService(grpcserver.NewMediaHandler(service, logger))

	go func() { _ = server.Run() }()
	defer server.Stop()

	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	client := mediav1.NewMediaServiceClient(conn)
	alice, bob := uuid.NewString(), uuid.NewString()
	as := func(bearer string) context.Context {
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+bearer)
	}
	aliceToken, bobToken := mint(t, privateKey, alice), mint(t, privateKey, bob)

	waitReady(t, client, as(aliceToken))

	photo := pngWithTrailer(t)

	created, err := client.CreateUpload(as(aliceToken), &mediav1.CreateUploadRequest{
		Purpose:     mediav1.MediaPurpose_MEDIA_PURPOSE_DRIVER_DOCUMENT,
		ContentType: "image/png",
		SizeBytes:   int64(len(photo)),
	})
	if err != nil {
		t.Fatalf("CreateUpload: %v", err)
	}

	id := created.GetMedia().GetId()

	if created.GetMedia().GetOwnerIdentityId() != alice || created.GetMedia().GetStatus() != mediav1.MediaStatus_MEDIA_STATUS_PENDING {
		t.Fatalf("created %+v", created.GetMedia())
	}

	// The upload URL only takes the declared type.
	if code := put(t, created, "image/jpeg", photo); code != http.StatusForbidden {
		t.Fatalf("PUT with another content type: %d", code)
	}

	if code := put(t, created, "image/png", photo); code != http.StatusOK {
		t.Fatalf("PUT: %d", code)
	}

	if _, err := client.CompleteUpload(as(bobToken), &mediav1.CompleteUploadRequest{MediaId: id}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("bob completing alice's upload: %v", err)
	}

	ready, err := client.CompleteUpload(as(aliceToken), &mediav1.CompleteUploadRequest{MediaId: id})
	if err != nil || ready.GetMedia().GetStatus() != mediav1.MediaStatus_MEDIA_STATUS_READY {
		t.Fatalf("CompleteUpload: %+v %v", ready, err)
	}

	if ready.GetMedia().GetWidth() != 40 || ready.GetMedia().GetHeight() != 30 {
		t.Fatalf("dimensions %dx%d", ready.GetMedia().GetWidth(), ready.GetMedia().GetHeight())
	}

	// A second PUT with the still-valid URL must not change what is served.
	evil := bytes.Repeat([]byte("X"), len(photo))
	if code := put(t, created, "image/png", evil); code != http.StatusOK {
		t.Fatalf("second PUT: %d", code)
	}

	download, err := client.GetDownloadURL(as(aliceToken), &mediav1.GetDownloadURLRequest{MediaId: id})
	if err != nil {
		t.Fatalf("GetDownloadURL: %v", err)
	}

	served := get(t, download.GetUrl())
	if bytes.Contains(served, []byte("TRAILER")) || bytes.Equal(served, evil) || int64(len(served)) != ready.GetMedia().GetSizeBytes() {
		t.Fatal("the served file is not the checked, re-encoded one")
	}

	if _, _, err := image.Decode(bytes.NewReader(served)); err != nil {
		t.Fatalf("the served file is not an image: %v", err)
	}

	if _, err := client.GetDownloadURL(as(bobToken), &mediav1.GetDownloadURLRequest{MediaId: id}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("bob downloading: %v", err)
	}

	// Services hold files; users cannot.
	if _, err := client.HoldMedia(as(aliceToken), &mediav1.HoldMediaRequest{MediaId: id}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("alice holding: %v", err)
	}

	held, err := client.HoldMedia(as(internalToken), &mediav1.HoldMediaRequest{
		MediaId: id, OwnerIdentityId: alice, Purpose: mediav1.MediaPurpose_MEDIA_PURPOSE_DRIVER_DOCUMENT,
	})
	if err != nil || !held.GetMedia().GetHeld() {
		t.Fatalf("HoldMedia: %+v %v", held, err)
	}

	if _, err := client.DeleteMedia(as(aliceToken), &mediav1.DeleteMediaRequest{MediaId: id}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("deleting a held file: %v", err)
	}

	if _, err := client.ReleaseMedia(as(internalToken), &mediav1.ReleaseMediaRequest{MediaId: id}); err != nil {
		t.Fatalf("ReleaseMedia: %v", err)
	}

	if _, err := client.DeleteMedia(as(aliceToken), &mediav1.DeleteMediaRequest{MediaId: id}); err != nil {
		t.Fatalf("DeleteMedia: %v", err)
	}

	if response, err := http.Get(download.GetUrl()); err == nil {
		_ = response.Body.Close()

		if response.StatusCode == http.StatusOK {
			t.Fatal("a deleted file is still served")
		}
	}

	// A file whose bytes are not what was declared is rejected.
	fake, err := client.CreateUpload(as(aliceToken), &mediav1.CreateUploadRequest{
		Purpose:     mediav1.MediaPurpose_MEDIA_PURPOSE_PROFILE_PHOTO,
		ContentType: "image/png",
		SizeBytes:   int64(len("<html>not a picture</html>")),
	})
	if err != nil {
		t.Fatal(err)
	}

	if code := put(t, fake, "image/png", []byte("<html>not a picture</html>")); code != http.StatusOK {
		t.Fatalf("PUT: %d", code)
	}

	rejected, err := client.CompleteUpload(as(aliceToken), &mediav1.CompleteUploadRequest{MediaId: fake.GetMedia().GetId()})
	if err != nil || rejected.GetMedia().GetStatus() != mediav1.MediaStatus_MEDIA_STATUS_REJECTED {
		t.Fatalf("CompleteUpload of a fake: %+v %v", rejected, err)
	}
}

func applyMigration(t *testing.T, url string) {
	t.Helper()

	migration, err := os.ReadFile("../../migrations/00001_create_media_objects.sql")
	if err != nil {
		t.Fatal(err)
	}

	up, down, _ := strings.Cut(string(migration), "-- +goose Down")

	psql := func(sql string) {
		cmd := exec.Command("psql", url, "-v", "ON_ERROR_STOP=1", "-q")
		cmd.Stdin = strings.NewReader(sql)

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("psql: %v\n%s", err, out)
		}
	}

	psql(down)
	psql(up)
	t.Cleanup(func() { psql(down) })
}

func writeKey(t *testing.T) (string, ed25519.PrivateKey) {
	t.Helper()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "public.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}

	return path, private
}

func mint(t *testing.T, key ed25519.PrivateKey, subject string) string {
	t.Helper()

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": "ride-identity",
		"aud": "ride-platform",
		"sub": subject,
		"sid": uuid.NewString(),
		"iat": now.Unix(),
		"nbf": now.Unix(),
		"exp": now.Add(10 * time.Minute).Unix(),
	}

	signed := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed.Header["kid"] = "smoke-key"

	raw, err := signed.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

func freeAddress(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	address := listener.Addr().String()
	_ = listener.Close()

	return address
}

func waitReady(t *testing.T, client mediav1.MediaServiceClient, ctx context.Context) {
	t.Helper()

	for i := 0; i < 50; i++ {
		_, err := client.GetMedia(ctx, &mediav1.GetMediaRequest{MediaId: uuid.NewString()})
		if status.Code(err) != codes.Unavailable {
			return
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatal("the server did not start")
}

// pngWithTrailer is a real PNG with bytes appended after its end.
func pngWithTrailer(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for y := 0; y < 30; y++ {
		for x := 0; x < 40; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 6), G: uint8(y * 8), B: 90, A: 255})
		}
	}

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}

	out.WriteString("TRAILER <script>alert(1)</script>")

	return out.Bytes()
}

func put(t *testing.T, ticket *mediav1.CreateUploadResponse, contentType string, body []byte) int {
	t.Helper()

	request, err := http.NewRequest(ticket.GetUploadMethod(), ticket.GetUploadUrl(), bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	for name, value := range ticket.GetUploadHeaders() {
		request.Header.Set(name, value)
	}

	request.Header.Set("Content-Type", contentType)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}

	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()

	return response.StatusCode
}

func get(t *testing.T, url string) []byte {
	t.Helper()

	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET: %d", response.StatusCode)
	}

	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	return data
}
