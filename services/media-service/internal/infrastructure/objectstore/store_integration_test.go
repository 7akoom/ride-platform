package objectstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Runs against a real S3-compatible store (SeaweedFS in development):
//
//	MEDIA_TEST_S3_ENDPOINT=http://127.0.0.1:8333 MEDIA_TEST_S3_ACCESS_KEY=... \
//	MEDIA_TEST_S3_SECRET_KEY=... go test ./internal/infrastructure/objectstore/
func testStore(t *testing.T) *Store {
	t.Helper()

	endpoint := os.Getenv("MEDIA_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("MEDIA_TEST_S3_ENDPOINT is not set")
	}

	store, err := New(Config{
		Endpoint:       endpoint,
		PublicEndpoint: endpoint,
		Region:         "us-east-1",
		Bucket:         "ride-media-test",
		AccessKey:      os.Getenv("MEDIA_TEST_S3_ACCESS_KEY"),
		SecretKey:      os.Getenv("MEDIA_TEST_S3_SECRET_KEY"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("ensure bucket: %v", err)
	}

	if err := store.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("ensure bucket twice: %v", err)
	}

	return store
}

func put(t *testing.T, target string, headers map[string]string, body []byte) int {
	t.Helper()

	request, _ := http.NewRequest(http.MethodPut, target, bytes.NewReader(body))
	for name, value := range headers {
		request.Header.Set(name, value)
	}

	request.ContentLength = int64(len(body))

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	return response.StatusCode
}

func TestPresignedUploadAndDownload(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	key := "test/" + uuid.NewString() + ".txt"
	body := []byte("hello, ride platform")

	target, headers, _ := store.PresignPut(key, "text/plain", int64(len(body)), time.Minute)

	if code := put(t, target, headers, append(body, '!')); code == http.StatusOK {
		t.Fatal("a body longer than the signed length was accepted")
	}

	wrongType := map[string]string{"Content-Type": "image/png", "Content-Length": headers["Content-Length"]}
	if code := put(t, target, wrongType, body); code == http.StatusOK {
		t.Fatal("a different content type was accepted")
	}

	if code := put(t, target, headers, body); code != http.StatusOK {
		t.Fatalf("presigned PUT answered %d", code)
	}

	info, err := store.Stat(ctx, key)
	if err != nil || info.Size != int64(len(body)) {
		t.Fatalf("stat: %+v, %v", info, err)
	}

	data, err := store.Read(ctx, key, 1024)
	if err != nil || string(data) != string(body) {
		t.Fatalf("read: %q, %v", data, err)
	}

	if _, err := store.Read(ctx, key, 5); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("read over the limit: %v", err)
	}

	download, _ := store.PresignGet(key, time.Minute)
	response, err := http.Get(download)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(response.Body)
	response.Body.Close()

	if response.StatusCode != http.StatusOK || string(got) != string(body) {
		t.Fatalf("presigned GET: %d %q", response.StatusCode, got)
	}

	tampered, _ := store.PresignGet("test/other.txt", time.Minute)
	tampered = tampered[:len(tampered)-1] + "0"
	if response, err := http.Get(tampered); err == nil {
		response.Body.Close()
		if response.StatusCode == http.StatusOK {
			t.Fatal("a tampered signature was accepted")
		}
	}

	if err := store.Put(ctx, key, "text/plain", []byte("replaced")); err != nil {
		t.Fatalf("put: %v", err)
	}

	if data, _ := store.Read(ctx, key, 1024); string(data) != "replaced" {
		t.Fatalf("after put: %q", data)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := store.Stat(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stat after delete: %v", err)
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("deleting twice: %v", err)
	}
}

func TestExpiredPresignedURLIsRefused(t *testing.T) {
	store := testStore(t)
	key := "test/" + uuid.NewString() + ".txt"

	target, headers, _ := store.PresignPut(key, "text/plain", 2, time.Second)
	time.Sleep(2500 * time.Millisecond)

	if code := put(t, target, headers, []byte("hi")); code == http.StatusOK {
		t.Fatal("an expired URL was accepted, code " + strconv.Itoa(code))
	}
}
