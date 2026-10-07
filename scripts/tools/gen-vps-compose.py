#!/usr/bin/env python3
"""Writes infrastructure/deploy/compose.vps.yaml from the development compose.

Run from the repo root after changing infrastructure/compose/compose.yaml:
    python3 scripts/tools/gen-vps-compose.py

It needs `docker compose` (to read the development file without
interpolating it). The VPS file is the same stack, changed for one server
that also runs other sites:
  - one Postgres (with PostGIS) holds every service's database; it answers to
    every old host name (identity-postgres, rider-postgres...), so the
    services' DATABASE_URLs stay as they are
  - nothing is published except the gateway and the file store, on 127.0.0.1
    (nginx on the host sends traffic to them)
  - every service runs with ENVIRONMENT=production and reads its settings
    from its .env.example, then instance/shared.env (the internal token, key
    id, time zones), then instance/<service>.env if present (provider
    credentials); its own secrets come from instance/instance.env through
    interpolation, so no service sees another's secrets and no .env from a
    development machine is needed
  - restart unless-stopped, memory limits, rotated logs
  - a "migrate" tool (profile tools) runs goose on every database
"""
import json
import pathlib
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[2]
DEV = ROOT / "infrastructure/compose/compose.yaml"
OUT = ROOT / "infrastructure/deploy/compose.vps.yaml"

# Memory each container may use (the VPS shares its memory with other sites).
MEMORY = {"postgres": "1200m", "osrm": "1536m", "seaweedfs": "512m", "nats": "256m", "gateway": "192m"}
DEFAULT_SERVICE_MEMORY = "256m"
SMALL_MEMORY = "128m"

PUBLISHED = {
    "gateway": ["127.0.0.1:${GATEWAY_PORT:-18080}:8080"],
    "seaweedfs": ["127.0.0.1:${FILES_PORT:-18333}:8333"],
}

KEYS = "../../instance/keys"

# Secrets only the service that needs them gets (from instance/instance.env,
# which compose reads for interpolation and never hands to containers whole).
SERVICE_SECRETS = {
    "identity-service": ["OTP_HASH_SECRET", "VALKEY_PASSWORD"],
    "wallet-service": ["VOUCHER_CODE_KEY"],
    "notification-service": ["SOS_WEBHOOK_SECRET"],
}
LOGGING = {"driver": "json-file", "options": {"max-size": "10m", "max-file": "3"}}


def load_dev():
    raw = subprocess.run(
        ["docker", "compose", "-f", str(DEV), "config", "--no-interpolate", "--no-path-resolution", "--format", "json"],
        check=True, capture_output=True, text=True,
    ).stdout
    return json.loads(raw)


def main():
    dev = load_dev()
    services = dev["services"]
    databases = sorted(name for name in services if name.endswith("-postgres"))
    out = {"name": "ride", "services": {}, "volumes": {}}

    prefixes = []
    for db in databases:
        env = services[db].get("environment", {})
        prefix = env["POSTGRES_DB"].split(":")[0].lstrip("${").split("_DB_NAME")[0]
        prefixes.append(prefix)

    out["services"]["postgres"] = {
        "image": "postgis/postgis:17-3.5-alpine",
        "container_name": "ride-postgres",
        "environment": {
            "POSTGRES_USER": "ride_admin",
            "POSTGRES_PASSWORD": "${POSTGRES_ADMIN_PASSWORD:?run scripts/deploy/init-instance.sh}",
            "POSTGRES_DB": "postgres",
            "RIDE_DATABASES": " ".join(prefixes),
            **{f"{p}_DB_{k}": f"${{{p}_DB_{k}}}" for p in prefixes for k in ("NAME", "USER", "PASSWORD")},
        },
        "volumes": [
            {"type": "volume", "source": "ride-postgres-data", "target": "/var/lib/postgresql/data"},
            {"type": "bind", "source": "./postgres-init.sh", "target": "/docker-entrypoint-initdb.d/10-ride.sh", "read_only": True},
        ],
        "command": ["postgres", "-c", "shared_buffers=256MB", "-c", "max_connections=300"],
        "healthcheck": {
            "test": ["CMD-SHELL", "pg_isready -U ride_admin -d postgres"],
            "interval": "5s", "timeout": "5s", "retries": 20,
        },
        "networks": {"default": {"aliases": databases}},
    }
    out["volumes"]["ride-postgres-data"] = {}

    for name, svc in services.items():
        if name in databases:
            continue

        svc = json.loads(json.dumps(svc))
        svc.pop("ports", None)
        if name in PUBLISHED:
            svc["ports"] = PUBLISHED[name]

        deps = svc.get("depends_on", {})
        if any(dep in databases for dep in deps):
            deps = {dep: cond for dep, cond in deps.items() if dep not in databases}
            deps["postgres"] = {"condition": "service_healthy", "required": True}
            svc["depends_on"] = deps

        bootstrap = name.endswith("nats-bootstrap")
        svc["restart"] = "no" if bootstrap else "unless-stopped"
        svc["logging"] = LOGGING

        if "build" in svc:
            service_dir = svc["build"].get("args", {}).get("SERVICE_NAME")
            svc["image"] = svc["image"].replace(":dev", ":vps")
            env_files = []
            if service_dir:
                env_files.append({"path": f"../../services/{service_dir}/.env.example", "required": True})
            if name != "gateway":
                # The gateway never holds the internal token.
                env_files.append({"path": "../../instance/shared.env", "required": True})
            if service_dir:
                # Provider credentials (SMS, ZainCash, SOS phones...), P13.
                env_files.append({"path": f"../../instance/{service_dir}.env", "required": False})
            svc["env_file"] = env_files
            env = svc.setdefault("environment", {})
            for secret in SERVICE_SECRETS.get(service_dir or "", []):
                env[secret] = "${%s:?run scripts/deploy/init-instance.sh}" % secret
            env["ENVIRONMENT"] = "production"
            if name == "gateway":
                env["ALLOWED_ORIGINS"] = "${ALLOWED_ORIGINS:-}"
            if service_dir == "identity-service":
                # production once SMS credentials are in (P13); "test" logs
                # the code for a staging server.
                env["APP_ENV"] = "${IDENTITY_APP_ENV:-production}"
            svc["mem_limit"] = MEMORY.get(name, DEFAULT_SERVICE_MEMORY)
        else:
            svc["mem_limit"] = MEMORY.get(name, SMALL_MEMORY)

        for volume in svc.get("volumes", []):
            source = volume.get("source", "")
            if source.startswith("../../services/identity-service/.local/keys"):
                volume["source"] = source.replace("../../services/identity-service/.local/keys", KEYS)
            elif source.startswith("./"):
                volume["source"] = "../compose/" + source[2:]
            elif source.startswith("../") and not source.startswith("../../"):
                volume["source"] = "../" + source[3:]

        if bootstrap:
            svc.pop("mem_limit", None)

        out["services"][name] = svc

    for volume, spec in dev.get("volumes", {}).items():
        if volume.endswith("-postgres-data"):
            continue
        out["volumes"][volume] = spec

    out["services"]["migrate"] = {
        "build": {"context": ".", "dockerfile": "migrate.Dockerfile"},
        "image": "ride-platform/migrate:vps",
        "profiles": ["tools"],
        "env_file": [{"path": "../../instance/instance.env", "required": True}],
        "environment": {"RIDE_DATABASES": " ".join(prefixes), "DB_HOST": "postgres"},
        "volumes": [
            {"type": "bind", "source": "../../services", "target": "/services", "read_only": True},
            {"type": "bind", "source": "./migrate.sh", "target": "/migrate.sh", "read_only": True},
        ],
        "entrypoint": ["sh", "/migrate.sh"],
        "depends_on": {"postgres": {"condition": "service_healthy", "required": True}},
        "restart": "no",
    }

    header = (
        "# GENERATED by scripts/tools/gen-vps-compose.py from infrastructure/compose/compose.yaml.\n"
        "# Do not edit by hand: change the development file and run the generator again.\n"
        "# Use: docker compose -f infrastructure/deploy/compose.vps.yaml --env-file instance/instance.env up -d\n"
    )
    OUT.write_text(header + json.dumps(out, indent=2, sort_keys=True) + "\n")
    print(f"wrote {OUT.relative_to(ROOT)}: {len(out['services'])} services, databases: {' '.join(prefixes)}")


if __name__ == "__main__":
    sys.exit(main())
