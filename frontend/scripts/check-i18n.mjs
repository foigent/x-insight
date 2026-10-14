import { readFile, readdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

const root = fileURLToPath(new URL("../src/", import.meta.url));
const read = path => readFile(resolve(root, path), "utf8");
const en = JSON.parse(await read("i18n/en-US.json"));
const zh = JSON.parse(await read("i18n/zh-CN.json"));
const problems = [];
const parameters = text => [...text.matchAll(/\{value\d+\}/g)].map(match => match[0]).sort().join(",");
if (JSON.stringify(Object.keys(en).sort()) !== JSON.stringify(Object.keys(zh).sort())) problems.push("catalog keys differ");
for (const [key, value] of Object.entries(en)) {
  if (!value.trim() || /[\u3400-\u9fff]/.test(value)) problems.push(`${key}: English translation missing`);
  if (typeof zh[key] !== "string" || parameters(value) !== parameters(zh[key] ?? "")) problems.push(`${key}: interpolation parameters differ`);
}
// Production source text belongs in the catalog. API values, identifiers, and
// user content flow through interpolation without translation or DOM rewriting.
for (const file of await readdir(root)) {
  if (!/\.(?:tsx?|css)$/.test(file) || file.includes(".test.")) continue;
  const source = await read(file);
  source.split("\n").forEach((line, index) => {
    if (/[\u3400-\u9fff]/.test(line) && !/^\s*(?:\/\/|\*)/.test(line)) problems.push(`${file}:${index + 1}: uncatalogued Chinese text`);
  });
  for (const match of source.matchAll(/\b(?:t|tx)\("([^"]+)"/g)) if (!Object.hasOwn(en, match[1])) problems.push(`${file}: unknown key ${match[1]}`);
}
if (problems.length) throw new Error(problems.join("\n"));
console.log(`validated ${Object.keys(en).length} bilingual UI messages and production source references`);
