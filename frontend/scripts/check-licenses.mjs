import { readFile } from "node:fs/promises";

const allowed = new Set([
  "0BSD",
  "Apache-2.0",
  "BSD-2-Clause",
  "BSD-3-Clause",
  "BlueOak-1.0.0",
  "CC0-1.0",
  "ISC",
  "MIT",
  "MIT-0",
  "MPL-2.0",
  "Python-2.0",
]);

const lockPath = new URL("../package-lock.json", import.meta.url);
const lock = JSON.parse(await readFile(lockPath, "utf8"));
const rejected = [];
let checked = 0;

for (const [path, metadata] of Object.entries(lock.packages ?? {})) {
  if (path === "") {
    continue;
  }
  checked += 1;
  if (!metadata.license || !allowed.has(metadata.license)) {
    rejected.push(`${path}: ${metadata.license ?? "MISSING"}`);
  }
}

if (rejected.length > 0) {
  throw new Error(`unapproved or missing dependency licenses:\n${rejected.join("\n")}`);
}

console.log(`validated ${checked} locked package license entries`);
