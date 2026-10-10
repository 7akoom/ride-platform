# Deploying an instance on one VPS

One instance (one investor, one country) on one Ubuntu server, behind the
host's nginx. The server may run other sites: the platform publishes nothing
but two ports on 127.0.0.1, and nginx sends its two domains there.

What runs: the 13 services and the gateway, one Postgres with PostGIS (a
database and a role per service), two Valkeys, NATS, SeaweedFS (files) and
OSRM (roads). About 3.5 GB of memory in all, each container capped. Address
search (Nominatim) is optional, see below: without it riders can only find the
curated places and pick points on the map.

| Piece | Where |
|---|---|
| `infrastructure/deploy/compose.vps.yaml` | the stack; generated, see below |
| `instance/` (git-ignored) | this server's secrets, key pair, domains, time zone, provider credentials |
| `scripts/deploy/init-instance.sh` | makes `instance/` once |
| `scripts/deploy/nginx-site.sh` | the nginx site and its certificates |
| `scripts/deploy/preflight.sh` | refuses placeholders, checks memory, disk, ports, DNS |
| `scripts/deploy/deploy.sh` | build, migrate, start, smoke test; also every update |
| `scripts/deploy/smoke.sh` | checks the public domain end to end |

## First time

1. DNS: two names pointing at the server, one level under the domain (with
   Cloudflare, `api.ride.example.com` is not covered by its free
   certificate; `ride-api.example.com` is). Example: `ride-api` and
   `ride-files`.
2. Docker:
   `curl -fsSL https://get.docker.com | sudo sh && sudo usermod -aG docker $USER`, then log in again.
3. The code: `git clone https://github.com/7akoom/ride-platform.git && cd ride-platform`
4. The instance:
   `bash scripts/deploy/init-instance.sh --api ride-api.example.com --files ride-files.example.com --owner-email you@example.com --staging`
   (`--staging` writes login codes to the log until an SMS provider is set
   up; leave it out for a real launch). Back up `instance/` off the server
   right away: it is the only copy of the secrets, and losing
   `VOUCHER_CODE_KEY` makes every issued voucher unusable.
5. nginx: `sudo bash scripts/deploy/nginx-site.sh`
6. Deploy: `bash scripts/deploy/deploy.sh` (the first run also prepares the
   road data: a download and a few minutes at about 2 GB of memory).
7. The first owner: staff-service has invited the `--owner-email` address
   (only while there is no staff). Sign in with a phone number, link that
   email (`POST /v1/me/identifiers/link-otp`, then `/link`), then accept
   (`POST /v1/staff/me:accept`). On staging both codes are in the log:
   `docker logs ride-identity-service 2>&1 | grep otp_code | tail -1`.

## Address search (Nominatim)

Finding streets and places by name, and naming a point on the map, needs
Nominatim with the country's map. For one country (Iraq) it takes about 2 GB of
memory and 10 GB of disk once imported; the first start imports the map, about
an hour. A server with 8 GB of memory runs it beside everything else.

1. Swap, so the import cannot run out of memory (once per server):
   ```
   sudo fallocate -l 4G /swapfile && sudo chmod 600 /swapfile
   sudo mkswap /swapfile && sudo swapon /swapfile
   echo '/swapfile none swap sw 0 0' | sudo tee -a /etc/fstab
   ```
2. `echo MAPS_SEARCH=on >> instance/instance.env`
3. `bash scripts/deploy/deploy.sh`, then follow the import:
   `docker logs -f ride-nominatim` (done when it says it is listening;
   `docker ps` then shows it healthy). Until then searches answer
   "unavailable".

The map is the country's OpenStreetMap extract from Geofabrik
(`PBF_URL` in `infrastructure/compose/compose.yaml`); `NOMINATIM_THREADS`
(default 4) sets how many cores the import uses.

## Places for search (Overture Maps)

The map knows streets well but few shops, restaurants and offices. Search also
looks in places imported from Overture Maps (https://overturemaps.org), an open
places dataset (CDLA Permissive 2.0 / Apache 2.0; much of it from businesses'
own pages). They come after curated places and before the map's results.

Nothing names a city or a country: the import takes what lies inside the
instance's **active zones of active cities**, so another governorate or country
only needs its cities and zones, then a run.

1. The instance's cities and zones exist (admin: `/v1/admin/cities`,
   `/v1/admin/zones`).
2. `bash scripts/deploy/import-places.sh --dry-run` reads and counts (a few
   minutes: it reads the dataset's files from S3 for the zones' area).
3. `bash scripts/deploy/import-places.sh` imports. Every run replaces the last
   import in one step, so search never sees it empty; curated places are never
   touched.

Run it again when zones change, and monthly for fresh data (Overture releases
monthly; the latest is found by itself, `--release` picks one), for example
from cron on the first of the month:
`0 3 1 * * cd ~/ride-platform && bash scripts/deploy/import-places.sh >> /var/log/ride-places.log 2>&1`

Settings: `instance/places-import.json` with any keys of
`scripts/tools/import-places/defaults.json`, the rest staying default: the
lowest confidence kept (`min_confidence`), categories never imported
(`exclude_categories`), how categories map to the apps' kinds (`kinds`), how
strongly each kind ranks (`kind_weights`, `category_weights`), and when two
near places are one (`dedupe_meters`, `dedupe_similarity`). Credit "Overture Maps Foundation"
with the map credits in the apps.

## Updates

`git pull && bash scripts/deploy/deploy.sh` — builds what changed, applies
new migrations, restarts, runs the smoke test.

## Providers (P13)

Credentials live only on the server, in `instance/` (never in git, never in
chat). After filling them: `bash scripts/deploy/deploy.sh`; the preflight
says what is missing.

**Login codes**, `instance/identity-service.env`:

- Phone codes by WhatsApp through BulkSMSIraq:
  `OTP_PHONE_DEFAULT_CHANNEL=whatsapp`, `WHATSAPP_DEFAULT_PROVIDER=bulksmsiraq`,
  `BULKSMSIRAQ_ENDPOINT`, `BULKSMSIRAQ_OTP_ENDPOINT`, `BULKSMSIRAQ_API_KEY`,
  `BULKSMSIRAQ_SENDER_ID`. SMS stays off while `SMS_DEFAULT_PROVIDER` and
  `SMS_ROUTES` are empty; to add it later set `SMS_DEFAULT_PROVIDER` (and
  `OTP_PHONE_DEFAULT_CHANNEL=sms` to make it the default).
- Email codes (staff sign in by email): `RESEND_API_KEY`, and `RESEND_FROM`
  on a domain verified in Resend, e.g. `Ride <no-reply@example.com>`.
- Then `IDENTITY_APP_ENV=production` in `instance/instance.env`. From then
  on codes are really sent and no longer logged. Try one:
  `SMOKE_PHONE=+9647501234567 bash scripts/deploy/smoke.sh` (it asks for the
  code that arrived).

**Push**, `instance/notification-service.env`: put the Firebase
service-account key (Firebase console > Project settings > Service accounts >
Generate new private key) at `instance/providers/fcm-service-account.json`,
`chmod 644` it, and uncomment
`FCM_CREDENTIALS_FILE=/app/providers/fcm-service-account.json`. Only
notification-service sees that folder.

**ZainCash**, `instance/wallet-service.env`; **SOS phones**,
`SOS_OPERATOR_PHONES` in `instance/notification-service.env`.

## The map (tiles)

The apps draw the map from this server: one PMTiles file of the instance's
area (cut out of the Protomaps daily build of OpenStreetMap), Noto Sans fonts
(Arabic included), icons, and four styles: `light-ar`, `light-en`,
`dark-ar`, `dark-en`. All static files served by nginx from
`/var/www/ride-tiles`; no container, no extra memory.

1. DNS: an A record for `ride-tiles.<domain>`, like the API's.
2. `sudo bash scripts/deploy/nginx-tiles-site.sh --domain ride-tiles.<domain>`:
   a site of its own (`ride-platform-tiles`, the API site is not touched),
   the folder, the certificate; records `TILES_DOMAIN`/`TILES_DIR`.
3. `bash scripts/deploy/prepare-tiles.sh` (no sudo): the basemap of
   `TILES_BBOX` (Iraq by default, recorded on the first run), fonts, icons,
   styles, then checks them over https. Run it again any time to refresh the
   map; `--build 20261007` picks a given build.

Apps: `kMapStyleUrl = https://ride-tiles.<domain>/styles/light-ar.json`.
MapLibre Native (maplibre_gl 0.22+) reads `pmtiles://` itself; the admin web
needs the `pmtiles` protocol for maplibre-gl JS and the RTL text plugin.
Styles are generated by `scripts/tools/gen-tile-styles.mjs` (see its header)
from `@protomaps/basemaps`.

## Monitoring (Grafana Cloud)

Every request gets an ID: the gateway starts a trace, each service it reaches
adds its part, and the gateway returns the ID as `X-Request-Id`. Log lines
written for a request carry it as `trace_id`. What is logged:

- gateway: one line per request that failed (5xx, ERROR), was refused (4xx,
  INFO) or took a second or more (WARN): method, path, status, time; never a
  header, a body or the query string
- services: one line per RPC that failed (ERROR "rpc failed"), was refused
  (INFO "rpc refused") or was slow (WARN "rpc slow")

Quick successes are not logged; their traces are enough.

On the server, without anything else: `bash scripts/deploy/trace.sh <id>`
prints the request's lines from every service.

With Grafana Cloud (free tier: 14 days of traces, logs and metrics):

1. grafana.com: a stack, then Connections > OpenTelemetry (OTLP): the
   endpoint, the instance ID and a token that may write metrics, logs and
   traces.
2. `bash scripts/deploy/enable-monitoring.sh` asks for the three (the token
   is not shown), checks them with Grafana Cloud, writes
   `instance/monitoring.env` (only the agent reads it) and points the
   services at the agent.
3. `bash scripts/deploy/deploy.sh` starts the Alloy agent (profile
   `monitoring`, ~150 MB). It sends the traces, every service's `/metrics`
   and the services' own log lines (Docker's other lines stay on the server).

In Grafana: Explore > Tempo with an ID finds the whole request; Explore > Loki
`{service="trip-service", level="ERROR"}` lists failures; the `rpc_server_*`
metrics give requests and durations by service, method and code.
`OTEL_TRACES_SAMPLER_ARG` in instance.env (0..1, default 1) keeps fewer
traces when traffic grows.

Background work driven by events (NATS) is not traced yet.

## When the development compose changes

`python3 scripts/tools/gen-vps-compose.py` regenerates
`compose.vps.yaml`; commit both.
