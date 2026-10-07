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

`instance/identity-service.env` (SMS), `instance/wallet-service.env`
(ZainCash), `instance/notification-service.env` (SOS phones). Fill them, set
`IDENTITY_APP_ENV=production` in `instance/instance.env`, deploy again.

## When the development compose changes

`python3 scripts/tools/gen-vps-compose.py` regenerates
`compose.vps.yaml`; commit both.
