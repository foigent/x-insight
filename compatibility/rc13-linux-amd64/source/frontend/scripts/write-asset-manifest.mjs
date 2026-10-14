#!/usr/bin/env node

import { writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { buildAssetManifest, encodeAssetManifest } from "./asset-manifest.mjs";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const distDirectory = resolve(scriptDirectory, "../../internal/webui/dist");
const manifest = await buildAssetManifest(distDirectory);
await writeFile(
  resolve(distDirectory, "asset-manifest.json"),
  encodeAssetManifest(manifest),
  { encoding: "utf8", mode: 0o644 },
);

console.log(`webui_asset_manifest_entries=${manifest.entries.length}`);
