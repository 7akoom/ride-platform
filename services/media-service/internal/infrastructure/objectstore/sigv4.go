package objectstore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
	"time"
)

// AWS Signature Version 4 for S3, the part this service needs: header-signed
// requests (the service's own calls) and presigned query URLs (the apps').
// https://docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-authenticating-requests.html

const (
	signingAlgorithm = "AWS4-HMAC-SHA256"
	amzDateFormat    = "20060102T150405Z"
	shortDateFormat  = "20060102"
	unsignedPayload  = "UNSIGNED-PAYLOAD"
	service          = "s3"
)

// emptyPayloadHash is the SHA-256 of no bytes.
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

type credentials struct {
	accessKey string
	secretKey string
	region    string
}

func (c credentials) scope(at time.Time) string {
	return at.Format(shortDateFormat) + "/" + c.region + "/" + service + "/aws4_request"
}

func (c credentials) signingKey(at time.Time) []byte {
	key := hmacSHA256([]byte("AWS4"+c.secretKey), at.Format(shortDateFormat))
	key = hmacSHA256(key, c.region)
	key = hmacSHA256(key, service)

	return hmacSHA256(key, "aws4_request")
}

// signature signs a canonical request made at the given time.
func (c credentials) signature(canonicalRequest string, at time.Time) string {
	stringToSign := strings.Join([]string{
		signingAlgorithm,
		at.Format(amzDateFormat),
		c.scope(at),
		hexSHA256([]byte(canonicalRequest)),
	}, "\n")

	return hex.EncodeToString(hmacSHA256(c.signingKey(at), stringToSign))
}

// canonicalRequest builds the request S3 recomputes to check a signature.
// headers must hold every signed header, names lowercase.
func canonicalRequest(
	method string,
	escapedPath string,
	query url.Values,
	headers map[string]string,
	payloadHash string,
) (request string, signedHeaders string) {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}

	sort.Strings(names)

	var canonicalHeaders strings.Builder
	for _, name := range names {
		canonicalHeaders.WriteString(name)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(strings.Join(strings.Fields(headers[name]), " "))
		canonicalHeaders.WriteByte('\n')
	}

	signedHeaders = strings.Join(names, ";")

	request = strings.Join([]string{
		method,
		escapedPath,
		canonicalQuery(query),
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	return request, signedHeaders
}

func canonicalQuery(query url.Values) string {
	keys := make([]string, 0, len(query))
	for key := range query {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	parts := make([]string, 0, len(keys))

	for _, key := range keys {
		values := append([]string(nil), query[key]...)
		sort.Strings(values)

		for _, value := range values {
			parts = append(parts, uriEncode(key, true)+"="+uriEncode(value, true))
		}
	}

	return strings.Join(parts, "&")
}

// uriEncode escapes everything but the unreserved characters, as SigV4 requires
// (url.QueryEscape differs: it turns spaces into '+'). A '/' stays as it is in
// paths and is escaped in query strings.
func uriEncode(value string, encodeSlash bool) string {
	var out strings.Builder

	for i := 0; i < len(value); i++ {
		b := value[i]

		switch {
		case b >= 'A' && b <= 'Z', b >= 'a' && b <= 'z', b >= '0' && b <= '9',
			b == '-', b == '_', b == '.', b == '~':
			out.WriteByte(b)
		case b == '/' && !encodeSlash:
			out.WriteByte(b)
		default:
			out.WriteString("%" + strings.ToUpper(hex.EncodeToString([]byte{b})))
		}
	}

	return out.String()
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))

	return mac.Sum(nil)
}

func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}
