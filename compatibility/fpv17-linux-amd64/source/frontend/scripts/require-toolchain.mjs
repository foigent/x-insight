import { readFileSync } from "node:fs";

const packageJSON = JSON.parse(readFileSync(new URL("../package.json", import.meta.url), "utf8"));
const expectedNode = packageJSON.engines?.node;
const expectedNpm = packageJSON.engines?.npm;
const manager = packageJSON.packageManager;
const agent = /^npm\/([^ ]+) node\/([^ ]+)(?: |$)/.exec(process.env.npm_config_user_agent ?? "");

if (!expectedNode || !expectedNpm || manager !== `npm@${expectedNpm}`) {
  throw new Error("package.json must declare matching exact Node and npm toolchain versions");
}
if (process.versions.node !== expectedNode || agent?.[1] !== expectedNpm || agent?.[2] !== `v${expectedNode}`) {
  console.error(`Required frontend toolchain: Node ${expectedNode}, ${manager}; current: Node ${process.versions.node}, ${agent ? `npm ${agent[1]} on Node ${agent[2]}` : "npm invocation unknown"}.`);
  process.exitCode = 1;
}
