#!/usr/bin/env node

import { lstat, readFile, readdir } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { buildAssetManifest, encodeAssetManifest } from "./asset-manifest.mjs";

const scriptDirectory = dirname(fileURLToPath(import.meta.url));
const distDirectory = resolve(scriptDirectory, "../../internal/webui/dist");
const assetsDirectory = resolve(distDirectory, "assets");
const index = await readFile(resolve(distDirectory, "index.html"), "utf8");
const references = [...index.matchAll(/\/assets\/([A-Za-z0-9][A-Za-z0-9._-]*)/g)]
  .map((match) => match[1]);
const uniqueReferences = [...new Set(references)].sort();

if (
  references.length !== 2
  || uniqueReferences.length !== 2
  || !uniqueReferences.some((name) => /^index-[A-Za-z0-9_-]{8}\.js$/.test(name))
  || !uniqueReferences.some((name) => /^index-[A-Za-z0-9_-]{8}\.css$/.test(name))
) {
  throw new Error(`dist index asset references are not one hashed JS and one hashed CSS: ${uniqueReferences.join(",")}`);
}

for (const name of uniqueReferences) {
  const metadata = await lstat(resolve(assetsDirectory, name));
  if (!metadata.isFile() || metadata.isSymbolicLink()) {
    throw new Error(`dist index asset is not a regular file: ${name}`);
  }
}

for (const stale of ["index-BibMuBXx.js", "index-pndZqYsI.css"]) {
  if (uniqueReferences.includes(stale)) {
    throw new Error(`dist index still references a retained historical asset: ${stale}`);
  }
}

const retained = (await readdir(assetsDirectory)).sort();
for (const name of retained) {
  const metadata = await lstat(resolve(assetsDirectory, name));
  if (!metadata.isFile() || metadata.isSymbolicLink()) {
    throw new Error(`dist asset directory contains a non-regular entry: ${name}`);
  }
}
const unreferenced = retained.filter((name) => !uniqueReferences.includes(name));

const expectedManifest = encodeAssetManifest(await buildAssetManifest(distDirectory));
const committedManifest = await readFile(resolve(distDirectory, "asset-manifest.json"), "utf8");
if (committedManifest !== expectedManifest) {
  throw new Error("dist asset manifest does not exactly describe generated index/JS/CSS bytes");
}

console.log(`dist_referenced_assets=${uniqueReferences.join(",")}`);
console.log(`dist_retained_unreferenced_assets=${unreferenced.join(",") || "none"}`);
console.log("dist_asset_manifest=exact");
