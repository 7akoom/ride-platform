// Writes the map styles the apps load: light and dark, Arabic and English
// labels, for the Protomaps basemap (tiles schema v4) served by our own
// nginx. Run from the repo root after changing the version below:
//   npm install --prefix /tmp/tile-styles @protomaps/basemaps@5.7.2
//   NODE_PATH=/tmp/tile-styles/node_modules node scripts/tools/gen-tile-styles.mjs
// __TILES_ORIGIN__ (e.g. https://ride-tiles.example.com) is filled in on the
// server by scripts/deploy/prepare-tiles.sh.
// The credits name Protomaps and Overture Maps too: place search
// (location-service) answers with places imported from Overture.
import { createRequire } from "node:module";
import { mkdirSync, writeFileSync } from "node:fs";

const require = createRequire(import.meta.url);
const { layers, namedFlavor } = require("@protomaps/basemaps");

const ORIGIN = "__TILES_ORIGIN__";
const OUT = "infrastructure/tiles/styles";
const CREDITS = '<a href="https://protomaps.com">Protomaps</a> · <a href="https://overturemaps.org">Overture Maps</a>';
mkdirSync(OUT, { recursive: true });

for (const flavor of ["light", "dark"]) {
  for (const lang of ["ar", "en"]) {
    const style = {
      version: 8,
      name: `Ride ${flavor} (${lang})`,
      glyphs: `${ORIGIN}/fonts/{fontstack}/{range}.pbf`,
      sprite: `${ORIGIN}/sprites/v4/${flavor}`,
      sources: {
        protomaps: {
          type: "vector",
          url: `pmtiles://${ORIGIN}/basemap.pmtiles`,
          attribution: '<a href="https://protomaps.com">Protomaps</a> © <a href="https://openstreetmap.org/copyright">OpenStreetMap</a>',
        },
        // MapLibre takes a pmtiles source's credits from the file (OpenStreetMap
        // only), not from the style. This source carries the rest: no layer uses
        // it, so its tiles are never requested.
        credits: {
          type: "vector",
          tiles: [`${ORIGIN}/credits/{z}/{x}/{y}.pbf`],
          minzoom: 0,
          maxzoom: 0,
          attribution: CREDITS,
        },
      },
      layers: layers("protomaps", namedFlavor(flavor), { lang }),
    };
    writeFileSync(`${OUT}/${flavor}-${lang}.json`, JSON.stringify(style, null, 1) + "\n");
  }
}
console.log(`wrote ${OUT}/{light,dark}-{ar,en}.json`);
