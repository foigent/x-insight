import { createHash } from "node:crypto";
import { lstat, readFile } from "node:fs/promises";
import { resolve } from "node:path";

export const assetManifestSchema = "xinsight.webui-asset-manifest.v1";

export async function buildAssetManifest(distDirectory) {
  const indexPath = resolve(distDirectory, "index.html");
  const index = await readFile(indexPath);
  const references = [...index.toString("utf8").matchAll(/\/assets\/([A-Za-z0-9][A-Za-z0-9._-]*)/g)]
    .map((match) => `assets/${match[1]}`);
  const uniqueReferences = [...new Set(references)].sort();
  if (references.length !== 2 || uniqueReferences.length !== 2) {
    throw new Error("web UI asset manifest requires exactly two unique index references");
  }

  const paths = ["index.html", ...uniqueReferences];
  const entries = [];
  for (const path of paths) {
    const absolutePath = resolve(distDirectory, path);
    const metadata = await lstat(absolutePath);
    if (!metadata.isFile() || metadata.isSymbolicLink()) {
      throw new Error(`web UI release asset is not a regular file: ${path}`);
    }
    const contents = await readFile(absolutePath);
    entries.push({
      path,
      size_bytes: contents.length,
      sha256: `sha256:${createHash("sha256").update(contents).digest("hex")}`,
    });
  }
  return { schema_version: assetManifestSchema, entries };
}

export function encodeAssetManifest(manifest) {
  return `${JSON.stringify(manifest, null, 2)}\n`;
}
