package objectstore

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// The examples from the S3 documentation, "Signature Calculations for the
// Authorization Header" and "Authenticating Requests: Using Query Parameters".
var exampleCredentials = credentials{
	accessKey: "AKIAIOSFODNN7EXAMPLE",
	secretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	region:    "us-east-1",
}

var exampleTime = time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)

func TestSignatureOfTheDocumentedGetObject(t *testing.T) {
	request, signedHeaders := canonicalRequest("GET", "/test.txt", nil, map[string]string{
		"host":                 "examplebucket.s3.amazonaws.com",
		"range":                "bytes=0-9",
		"x-amz-content-sha256": emptyPayloadHash,
		"x-amz-date":           "20130524T000000Z",
	}, emptyPayloadHash)

	if signedHeaders != "host;range;x-amz-content-sha256;x-amz-date" {
		t.Fatalf("signed headers = %q", signedHeaders)
	}

	if got := exampleCredentials.signature(request, exampleTime); got != "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41" {
		t.Fatalf("signature = %s", got)
	}
}

func TestSignatureOfTheDocumentedPutObject(t *testing.T) {
	payloadHash := hexSHA256([]byte("Welcome to Amazon S3."))

	request, _ := canonicalRequest("PUT", "/"+uriEncode("test$file.text", false), nil, map[string]string{
		"date":                 "Fri, 24 May 2013 00:00:00 GMT",
		"host":                 "examplebucket.s3.amazonaws.com",
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           "20130524T000000Z",
		"x-amz-storage-class":  "REDUCED_REDUNDANCY",
	}, payloadHash)

	if !strings.HasPrefix(request, "PUT\n/test%24file.text\n") {
		t.Fatalf("canonical request starts %q", request[:30])
	}

	if got := exampleCredentials.signature(request, exampleTime); got != "98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd" {
		t.Fatalf("signature = %s", got)
	}
}

func TestSignatureOfTheDocumentedPresignedURL(t *testing.T) {
	query := url.Values{}
	query.Set("X-Amz-Algorithm", signingAlgorithm)
	query.Set("X-Amz-Credential", "AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request")
	query.Set("X-Amz-Date", "20130524T000000Z")
	query.Set("X-Amz-Expires", "86400")
	query.Set("X-Amz-SignedHeaders", "host")

	request, _ := canonicalRequest("GET", "/test.txt", query, map[string]string{
		"host": "examplebucket.s3.amazonaws.com",
	}, unsignedPayload)

	if got := exampleCredentials.signature(request, exampleTime); got != "aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404" {
		t.Fatalf("signature = %s", got)
	}
}

func TestPresignedPutSignsTheTypeAndLength(t *testing.T) {
	store, err := New(Config{
		Endpoint: "http://seaweedfs:8333", PublicEndpoint: "https://files.example.com",
		Region: "us-east-1", Bucket: "ride-media", AccessKey: "a", SecretKey: "b",
	})
	if err != nil {
		t.Fatal(err)
	}

	store.now = func() time.Time { return exampleTime }

	target, headers, expires := store.PresignPut("driver_document/2026/09/x.jpg", "image/jpeg", 1234, 10*time.Minute)

	if !strings.HasPrefix(target, "https://files.example.com/ride-media/driver_document/2026/09/x.jpg?") {
		t.Fatalf("url = %s", target)
	}

	if !strings.Contains(target, "X-Amz-SignedHeaders=content-length%3Bcontent-type%3Bhost") || !strings.Contains(target, "X-Amz-Expires=600") {
		t.Fatalf("url = %s", target)
	}

	if headers["Content-Type"] != "image/jpeg" || headers["Content-Length"] != "1234" {
		t.Fatalf("headers = %v", headers)
	}

	if !expires.Equal(exampleTime.Add(10 * time.Minute)) {
		t.Fatalf("expires = %v", expires)
	}
}

func TestNewRefusesIncompleteConfiguration(t *testing.T) {
	good := Config{Endpoint: "http://s:1", PublicEndpoint: "http://p:1", Region: "r", Bucket: "b", AccessKey: "a", SecretKey: "s"}

	for name, mutate := range map[string]func(*Config){
		"endpoint": func(c *Config) { c.Endpoint = "seaweedfs:8333" },
		"public":   func(c *Config) { c.PublicEndpoint = "" },
		"bucket":   func(c *Config) { c.Bucket = " " },
		"keys":     func(c *Config) { c.SecretKey = "" },
	} {
		cfg := good
		mutate(&cfg)

		if _, err := New(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
