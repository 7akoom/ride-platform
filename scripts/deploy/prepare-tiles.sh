#!/usr/bin/env bash
# Puts the map on this server: the basemap of the instance's area (cut out of
# the Protomaps daily build of OpenStreetMap), its fonts and icons, and the
# four styles the apps load. Run on the server, from the repo root, after
# nginx-tiles-site.sh (no sudo):
#   bash scripts/deploy/prepare-tiles.sh [--build 20261007]
# Run it again to refresh the map; the old one is served until the new one
# is complete. Only this step downloads anything: the apps then read the map
# from this server alone.
#
# The area is TILES_BBOX in instance/instance.env (west,south,east,north);
# the first run records Iraq.
set -Eeuo pipefail
trap 'echo "FAIL: prepare-tiles stopped at line $LINENO" >&2' ERR

PMTILES_IMAGE="protomaps/go-pmtiles:v1.31.2"
# Fonts (Noto Sans, Arabic included) and icons for tiles schema v4.
ASSETS_COMMIT="028c18f713baecad011301ff7a69acc39bcc2ae7"
IRAQ_BBOX="38.79,29.06,48.64,37.39"

die() { echo "FAIL: $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" instance/instance.env | tail -1; }

[ -f instance/instance.env ] || die "run this from the repo root of a deployed instance"
DOMAIN="$(value TILES_DOMAIN)"; DIR="$(value TILES_DIR)"
[ -n "$DOMAIN" ] && [ -n "$DIR" ] || die "no TILES_DOMAIN yet: sudo bash scripts/deploy/nginx-tiles-site.sh --domain ride-tiles.<your domain>"
[ -d "$DIR" ] && [ -w "$DIR" ] || die "$DIR is missing or not yours: sudo bash scripts/deploy/nginx-tiles-site.sh"

BBOX="$(value TILES_BBOX)"
if [ -z "$BBOX" ]; then
  BBOX="$IRAQ_BBOX"
  printf 'TILES_BBOX=%s\n' "$BBOX" >> instance/instance.env
  echo "recorded TILES_BBOX=$BBOX (Iraq) in instance/instance.env"
fi
echo "$BBOX" | grep -Eq '^-?[0-9.]+,-?[0-9.]+,-?[0-9.]+,-?[0-9.]+$' || die "TILES_BBOX must be west,south,east,north"

BUILD=""
while [ $# -gt 0 ]; do
  case "$1" in
    --build) BUILD="$2"; shift 2 ;;
    *) die "unknown option $1" ;;
  esac
done
if [ -z "$BUILD" ]; then
  BUILD="$(curl -fsS https://build-metadata.protomaps.dev/builds.json | python3 -c '
import json, sys
builds = [b["key"] for b in json.load(sys.stdin) if b.get("key", "").endswith(".pmtiles")]
print(sorted(builds)[-1].removesuffix(".pmtiles"))')"
fi
echo "$BUILD" | grep -Eq '^[0-9]{8}$' || die "build must look like 20261007, got $BUILD"

free_gb="$(df -BG --output=avail "$DIR" | tail -1 | tr -dc 0-9)"
[ "$free_gb" -ge 3 ] || die "only ${free_gb} GB free under $DIR (3 GB at least)"

echo "==> basemap: build $BUILD, area $BBOX (a few minutes)"
rm -f "$DIR/basemap.next.pmtiles"
docker run --rm --user "$(id -u):$(id -g)" -v "$DIR:/data" "$PMTILES_IMAGE" \
  extract "https://build.protomaps.com/$BUILD.pmtiles" /data/basemap.next.pmtiles \
  --bbox="$BBOX" --download-threads=4
docker run --rm --user "$(id -u):$(id -g)" -v "$DIR:/data" "$PMTILES_IMAGE" \
  show /data/basemap.next.pmtiles > /tmp/ride-tiles-show.txt \
  || die "the new basemap does not read back; the old one is still served"
mv -f "$DIR/basemap.next.pmtiles" "$DIR/basemap.pmtiles"
chmod 644 "$DIR/basemap.pmtiles"
echo "  $(du -h "$DIR/basemap.pmtiles" | cut -f1) basemap.pmtiles"

echo "==> fonts and icons"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "https://codeload.github.com/protomaps/basemaps-assets/tar.gz/$ASSETS_COMMIT" | tar xz -C "$tmp"
src="$tmp/basemaps-assets-$ASSETS_COMMIT"
[ -f "$src/fonts/Noto Sans Regular/1536-1791.pbf" ] || die "the fonts have no Arabic range"
for part in fonts sprites; do
  rm -rf "$DIR/$part.next"
  mkdir -p "$DIR/$part.next"
done
cp -r "$src/fonts/." "$DIR/fonts.next/"
mkdir -p "$DIR/sprites.next/v4"
cp -r "$src/sprites/v4/." "$DIR/sprites.next/v4/"
for part in fonts sprites; do
  rm -rf "$DIR/$part.old"
  [ -d "$DIR/$part" ] && mv "$DIR/$part" "$DIR/$part.old"
  mv "$DIR/$part.next" "$DIR/$part"
  rm -rf "$DIR/$part.old"
done
chmod -R a+rX "$DIR/fonts" "$DIR/sprites"

echo "==> styles"
mkdir -p "$DIR/styles"
for style in infrastructure/tiles/styles/*.json; do
  sed "s#__TILES_ORIGIN__#https://$DOMAIN#g" "$style" > "$DIR/styles/$(basename "$style").next"
  python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$DIR/styles/$(basename "$style").next"
  mv -f "$DIR/styles/$(basename "$style").next" "$DIR/styles/$(basename "$style")"
done
chmod 644 "$DIR"/styles/*.json
printf 'build=%s\nbbox=%s\nassets=%s\n' "$BUILD" "$BBOX" "$ASSETS_COMMIT" > "$DIR/VERSION"
ls "$DIR/styles"

echo "==> https://$DOMAIN"
fails=0
code() { curl -s -o /dev/null -w '%{http_code}' "$@" || true; }
check() { if [ "$2" = "$3" ]; then printf '  ok    %s -> %s\n' "$1" "$3"; else printf '  FAIL  %s -> expected %s, got %s\n' "$1" "$2" "$3"; fails=$((fails + 1)); fi; }
check "a style" 200 "$(code "https://$DOMAIN/styles/light-ar.json")"
check "a piece of the basemap (range request)" 206 "$(code -H 'Range: bytes=0-126' "https://$DOMAIN/basemap.pmtiles")"
check "an Arabic font range" 200 "$(code "https://$DOMAIN/fonts/Noto%20Sans%20Regular/1536-1791.pbf")"
check "the icons" 200 "$(code "https://$DOMAIN/sprites/v4/light.json")"
echo
if [ "$fails" -gt 0 ]; then echo "FAIL: $fails check(s)"; exit 1; fi
echo "PASS: the map is served from https://$DOMAIN"
echo "apps: kMapStyleUrl = https://$DOMAIN/styles/light-ar.json (or light-en, dark-ar, dark-en)"
