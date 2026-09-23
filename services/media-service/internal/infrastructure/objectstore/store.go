package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrNotFound means the object (or bucket) does not exist.
var ErrNotFound = errors.New("object not found")

// ErrTooLarge means an object is larger than the caller would read.
var ErrTooLarge = errors.New("object is larger than allowed")

// Config locates the S3-compatible object store (SeaweedFS in this deployment).
//
// Endpoint is how this service reaches it (inside the Docker network).
// PublicEndpoint is how the apps reach it; presigned URLs carry that host,
// and the signature covers it, so a proxy in front must pass Host unchanged.
type Config struct {
	Endpoint       string
	PublicEndpoint string
	Region         string
	Bucket         string
	AccessKey      string
	SecretKey      string
}

// ObjectInfo is what a HEAD request tells about an object.
type ObjectInfo struct {
	Size        int64
	ContentType string
}

// Store talks to one bucket over path-style URLs ({endpoint}/{bucket}/{key}).
type Store struct {
	credentials credentials
	bucket      string
	internal    *url.URL
	public      *url.URL
	http        *http.Client
	now         func() time.Time
}

func New(cfg Config) (*Store, error) {
	internal, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("S3 endpoint: %w", err)
	}

	public, err := parseEndpoint(cfg.PublicEndpoint)
	if err != nil {
		return nil, fmt.Errorf("S3 public endpoint: %w", err)
	}

	switch {
	case strings.TrimSpace(cfg.Bucket) == "":
		return nil, errors.New("S3 bucket is required")
	case strings.TrimSpace(cfg.Region) == "":
		return nil, errors.New("S3 region is required")
	case cfg.AccessKey == "" || cfg.SecretKey == "":
		return nil, errors.New("S3 access key and secret key are required")
	}

	return &Store{
		credentials: credentials{accessKey: cfg.AccessKey, secretKey: cfg.SecretKey, region: cfg.Region},
		bucket:      cfg.Bucket,
		internal:    internal,
		public:      public,
		http:        &http.Client{Timeout: 60 * time.Second},
		now:         func() time.Time { return time.Now().UTC() },
	}, nil
}

func parseEndpoint(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(raw), "/"))
	if err != nil {
		return nil, err
	}

	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("%q must be an http(s) URL", raw)
	}

	return parsed, nil
}

func (s *Store) objectPath(key string) string {
	return "/" + uriEncode(s.bucket, true) + "/" + uriEncode(key, false)
}

// PresignPut returns a URL that accepts exactly one PUT of exactly size bytes
// of contentType, until it expires. The headers must be sent as returned.
func (s *Store) PresignPut(key string, contentType string, size int64, ttl time.Duration) (string, map[string]string, time.Time) {
	headers := map[string]string{
		"Content-Type":   contentType,
		"Content-Length": strconv.FormatInt(size, 10),
	}

	signed := map[string]string{
		"content-type":   contentType,
		"content-length": strconv.FormatInt(size, 10),
	}

	target, expires := s.presign(http.MethodPut, key, nil, signed, ttl)

	return target, headers, expires
}

// PresignGet returns a URL that reads the object until it expires.
func (s *Store) PresignGet(key string, ttl time.Duration) (string, time.Time) {
	return s.presign(http.MethodGet, key, nil, nil, ttl)
}

func (s *Store) presign(
	method string,
	key string,
	extraQuery url.Values,
	extraHeaders map[string]string,
	ttl time.Duration,
) (string, time.Time) {
	at := s.now()
	path := s.objectPath(key)

	headers := map[string]string{"host": s.public.Host}
	for name, value := range extraHeaders {
		headers[name] = value
	}

	query := url.Values{}
	for name, values := range extraQuery {
		query[name] = values
	}

	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}

	query.Set("X-Amz-Algorithm", signingAlgorithm)
	query.Set("X-Amz-Credential", s.credentials.accessKey+"/"+s.credentials.scope(at))
	query.Set("X-Amz-Date", at.Format(amzDateFormat))
	query.Set("X-Amz-Expires", strconv.Itoa(int(ttl/time.Second)))
	sort.Strings(names)
	query.Set("X-Amz-SignedHeaders", strings.Join(names, ";"))

	request, _ := canonicalRequest(method, path, query, headers, unsignedPayload)
	signature := s.credentials.signature(request, at)

	return s.public.Scheme + "://" + s.public.Host + s.public.Path + path +
			"?" + canonicalQuery(query) + "&X-Amz-Signature=" + signature,
		at.Add(ttl)
}

// do sends a header-signed request to the internal endpoint.
func (s *Store) do(ctx context.Context, method string, path string, query url.Values, body []byte, headers map[string]string) (*http.Response, error) {
	at := s.now()

	payloadHash := emptyPayloadHash
	if len(body) > 0 {
		payloadHash = hexSHA256(body)
	}

	signed := map[string]string{
		"host":                 s.internal.Host,
		"x-amz-date":           at.Format(amzDateFormat),
		"x-amz-content-sha256": payloadHash,
	}
	for name, value := range headers {
		signed[strings.ToLower(name)] = value
	}

	request, signedHeaders := canonicalRequest(method, path, query, signed, payloadHash)
	signature := s.credentials.signature(request, at)

	target := s.internal.Scheme + "://" + s.internal.Host + s.internal.Path + path
	if encoded := canonicalQuery(query); encoded != "" {
		target += "?" + encoded
	}

	httpRequest, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	for name, value := range signed {
		if name != "host" {
			httpRequest.Header.Set(name, value)
		}
	}

	httpRequest.ContentLength = int64(len(body))
	httpRequest.Header.Set("Authorization", fmt.Sprintf(
		"%s Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		signingAlgorithm, s.credentials.accessKey, s.credentials.scope(at), signedHeaders, signature,
	))

	return s.http.Do(httpRequest)
}

// EnsureBucket creates the bucket when it does not exist yet.
func (s *Store) EnsureBucket(ctx context.Context) error {
	path := "/" + uriEncode(s.bucket, true)

	response, err := s.do(ctx, http.MethodHead, path, nil, nil, nil)
	if err != nil {
		return fmt.Errorf("check bucket: %w", err)
	}
	drain(response)

	switch response.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
	default:
		return fmt.Errorf("check bucket: unexpected status %d", response.StatusCode)
	}

	response, err = s.do(ctx, http.MethodPut, path, nil, nil, nil)
	if err != nil {
		return fmt.Errorf("create bucket: %w", err)
	}
	defer drain(response)

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("create bucket: unexpected status %d: %s", response.StatusCode, readSnippet(response))
	}

	return nil
}

// Stat reads an object's size and content type.
func (s *Store) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	response, err := s.do(ctx, http.MethodHead, s.objectPath(key), nil, nil, nil)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("stat object: %w", err)
	}
	drain(response)

	switch response.StatusCode {
	case http.StatusOK:
		return ObjectInfo{Size: response.ContentLength, ContentType: response.Header.Get("Content-Type")}, nil
	case http.StatusNotFound:
		return ObjectInfo{}, ErrNotFound
	default:
		return ObjectInfo{}, fmt.Errorf("stat object: unexpected status %d", response.StatusCode)
	}
}

// Read returns the whole object, refusing one larger than limit.
func (s *Store) Read(ctx context.Context, key string, limit int64) ([]byte, error) {
	response, err := s.do(ctx, http.MethodGet, s.objectPath(key), nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}
	defer drain(response)

	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		return nil, fmt.Errorf("read object: unexpected status %d", response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}

	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}

	return data, nil
}

// Put writes an object.
func (s *Store) Put(ctx context.Context, key string, contentType string, data []byte) error {
	response, err := s.do(ctx, http.MethodPut, s.objectPath(key), nil, data, map[string]string{"Content-Type": contentType})
	if err != nil {
		return fmt.Errorf("write object: %w", err)
	}
	defer drain(response)

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("write object: unexpected status %d: %s", response.StatusCode, readSnippet(response))
	}

	return nil
}

// Delete removes an object; one that is already gone is not an error.
func (s *Store) Delete(ctx context.Context, key string) error {
	response, err := s.do(ctx, http.MethodDelete, s.objectPath(key), nil, nil, nil)
	if err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	drain(response)

	switch response.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	default:
		return fmt.Errorf("delete object: unexpected status %d", response.StatusCode)
	}
}

func drain(response *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	_ = response.Body.Close()
}

func readSnippet(response *http.Response) string {
	data, _ := io.ReadAll(io.LimitReader(response.Body, 512))

	return strings.TrimSpace(string(data))
}
