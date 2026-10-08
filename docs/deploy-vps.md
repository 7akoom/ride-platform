# Deploying an instance on one VPS

One instance (one investor, one country) on one Ubuntu server, behind the
host's nginx. The server may run other sites: the platform publishes nothing
but two ports on 127.0.0.1, and nginx sends its two domains there.

What runs: the 13 services and the gateway, one Postgres with PostGIS (a
database and a role per service), two Valkeys, NATS, SeaweedFS (files) and
OSRM (roads). Nominatim (address search) is left out: it wants 8 GB of memory
on its own; searches then return the curated places only. About 3.5 GB of
memory in all, each container capped.

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

## When the development compose changes

`python3 scripts/tools/gen-vps-compose.py` regenerates
`compose.vps.yaml`; commit both.
