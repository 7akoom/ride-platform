# Media Service

Keeps the files people upload: driver documents (licence, vehicle papers),
profile photos, photos that help a captain find a saved address, and support
attachments. The bytes live in an S3-compatible object store (SeaweedFS in
docker compose); this service keeps a record of each file and hands out
short-lived signed URLs. Files never pass through the gateway.

## Upload flow

1. `POST /v1/media:upload` with the purpose, the content type and the exact
   size. The answer holds a media id, an upload URL, the method (`PUT`) and
   the headers to send. The URL accepts exactly that type and that many
   bytes, for `UPLOAD_URL_TTL`.
2. The app `PUT`s the file to that URL, with those headers.
3. `POST /v1/media/{id}:complete`. The service checks the bytes:
   - the real type is read from the content, not taken from the app;
   - images must decode, with sane dimensions (at most 12000 px a side, 40 MP);
   - images are decoded and written again: EXIF (camera, GPS position) and
     anything appended or hidden in the file is dropped, the EXIF rotation is
     applied first, the longer side is scaled down to 2560 px, and WebP is
     stored as JPEG;
   - PDFs with JavaScript, launch actions or embedded files are refused.

   A file that passes is `READY`; one that does not is deleted and
   `REJECTED`, with the reason.
4. `GET /v1/media/{id}:download` returns a URL valid for `DOWNLOAD_URL_TTL`.

The app uploads to an *incoming* object; only the checked bytes are copied to
the object downloads read. A second `PUT` with a still-valid upload URL can
therefore never replace a checked file. Incoming objects are deleted after
completion, and again by a sweep once their upload URL has expired.

Uploads not completed within `PENDING_UPLOAD_TTL` expire and their bytes are
deleted. One person may have at most `MAX_PENDING_UPLOADS_PER_OWNER` uploads
waiting.

| Purpose | Types | Max size |
|---|---|---|
| `DRIVER_DOCUMENT` | JPEG, PNG, WebP, PDF | 10 MB |
| `PROFILE_PHOTO` | JPEG, PNG, WebP | 5 MB |
| `ADDRESS_PHOTO` | JPEG, PNG, WebP | 8 MB |
| `SUPPORT_ATTACHMENT` | JPEG, PNG, WebP, PDF | 10 MB |

## Who may do what

| RPC | Route | Who |
|---|---|---|
| `CreateUpload` | `POST /v1/media:upload` | any signed-in user; the file is theirs (services cannot upload) |
| `CompleteUpload`, `DeleteMedia` | `POST /v1/media/{id}:complete`, `DELETE /v1/media/{id}` | the owner |
| `GetMedia`, `GetDownloadURL` | `GET /v1/media/{id}`, `GET /v1/media/{id}:download` | the owner, or staff with `media.read` (audited) |
| `HoldMedia`, `ReleaseMedia` | none | services only (internal token) |

A file that does not exist answers like someone else's file (`403`), so ids
cannot be probed.

A service that keeps a file (driver-service for a licence, for example) calls
`HoldMedia` with the owner and the purpose it expects; the owner can then no
longer delete it until the service calls `ReleaseMedia`.

## Running locally

```bash
cd services/media-service
cp .env.example .env   # set DATABASE_URL, S3_ACCESS_KEY, S3_SECRET_KEY
goose -dir migrations postgres "$DATABASE_URL" up
go run ./cmd/media-service
```

The bucket is created at startup if it does not exist.

Tests that need real stores are skipped unless their variables are set:

```bash
MEDIA_TEST_DATABASE_URL=postgres://.../empty_db \
  go test ./internal/infrastructure/persistence/postgres/

MEDIA_TEST_S3_ENDPOINT=http://127.0.0.1:8333 \
MEDIA_TEST_S3_ACCESS_KEY=... MEDIA_TEST_S3_SECRET_KEY=... \
  go test ./internal/infrastructure/objectstore/

# Both at once: the whole service, end to end.
MEDIA_TEST_DATABASE_URL=... MEDIA_TEST_S3_ENDPOINT=... \
MEDIA_TEST_S3_ACCESS_KEY=... MEDIA_TEST_S3_SECRET_KEY=... \
  go test ./cmd/media-service/
```
