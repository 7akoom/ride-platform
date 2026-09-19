#!/usr/bin/env python3
"""A tiny local receiver for the SOS webhook, used by test-sos-webhook.sh.

Listens on 0.0.0.0:PORT (default 8099), verifies the X-Signature-256 header
(HMAC-SHA256 of the raw body under SOS_WEBHOOK_SECRET) and appends one JSON
line per request to the file named by SINK_LOG, e.g.:

    {"signature_valid": true, "body": {...}}

It exists only for local testing; it does nothing else with the data.
"""
import hashlib
import hmac
import json
import os
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = int(os.environ.get("SINK_PORT", "8099"))
SECRET = os.environ.get("SOS_WEBHOOK_SECRET", "")
LOG = os.environ.get("SINK_LOG", "/tmp/sos-sink.log")


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length)

        signature = self.headers.get("X-Signature-256", "")
        expected = "sha256=" + hmac.new(SECRET.encode(), raw, hashlib.sha256).hexdigest()
        valid = bool(SECRET) and hmac.compare_digest(signature, expected)

        try:
            body = json.loads(raw)
        except ValueError:
            body = None

        with open(LOG, "a", encoding="utf-8") as handle:
            handle.write(json.dumps({"signature_valid": valid, "body": body}) + "\n")

        self.send_response(200)
        self.end_headers()

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    print(f"sos-webhook-sink listening on :{PORT}, logging to {LOG}", file=sys.stderr)
    HTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
