#!/usr/bin/env node

import { createHash } from "node:crypto";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const scriptPath = fileURLToPath(import.meta.url);
const root = resolve(dirname(scriptPath), "..");
const outputDir = resolve(root, "internal/webui/manual/zh-CN");
const profiles = new Set(["development-b", "development-d", "rc13"]);
const profile = process.argv[2]?.replace(/^--profile=/, "") ?? "development-d";
if (process.argv.length > 3 || !profiles.has(profile)) throw new Error("usage: build-manual.mjs [--profile=development-b|--profile=development-d|--profile=rc13]");
const legacy = profile === "rc13";
const budget = profile === "development-d";
const chapters = legacy ? [
  ["docs/manual/zh-CN/index.md", "manual-start"],
  ["docs/manual/zh-CN/00-version-and-support-boundary.md", "version-boundary"],
  ["docs/manual/zh-CN/01-product-overview-and-terms.md", "overview-terms"],
  ["docs/manual/zh-CN/admin/02-install-and-first-start.md", "install-first-start"],
  ["docs/manual/zh-CN/admin/03-configuration-security-and-access.md", "security-access"],
  ["docs/manual/zh-CN/admin/04-upgrade-migration-rollback-and-restore.md", "upgrade-restore"],
  ["docs/manual/zh-CN/admin/05-trial-exit-retention-and-handoff.md", "trial-exit"],
  ["docs/manual/zh-CN/operator/06-execution-investigation-workflow.md", "investigation-workflow"],
  ["docs/manual/zh-CN/operator/07-source-provenance-and-capture-gaps.md", "provenance-gaps"],
  ["docs/manual/zh-CN/reference/08-cli-api-and-error-contract.md", "cli-api-errors"],
  ["docs/manual/zh-CN/reference/09-maintenance-diagnostics-and-troubleshooting.md", "diagnostics"],
  ["docs/manual/zh-CN/release-notes/compatibility-matrix.md", "compatibility"],
] : [
  [budget ? "docs/manual/zh-CN/d-validation.md" : "docs/manual/zh-CN/b-validation.md", budget ? "d-validation" : "b-validation"],
  ...(budget ? [["docs/manual/zh-CN/admin/10-workbench-access.md", "workbench-access"]] : []),
  [budget ? "docs/manual/zh-CN/operator/06-execution-investigation-workflow-development-d.md" : "docs/manual/zh-CN/operator/06-execution-investigation-workflow.md", "investigation-workflow"],
  ["docs/manual/zh-CN/operator/07-source-provenance-and-capture-gaps.md", "provenance-gaps"],
];

const configPath = legacy ? "release/release-config.json" : budget ? "release/d-validation-config.json" : "release/b-validation-config.json";
const matrixPath = legacy ? "release/compatibility-matrix.json" : budget ? "release/d-validation-compatibility-matrix.json" : "release/b-validation-compatibility-matrix.json";
const releaseConfigSource = await readFile(resolve(root, configPath), "utf8");
const matrixSource = await readFile(resolve(root, matrixPath), "utf8");
const releaseConfig = JSON.parse(releaseConfigSource);
const matrix = JSON.parse(matrixSource);
if (releaseConfig.product_version !== matrix.product_version || releaseConfig.manual.version !== matrix.manual_version) {
  throw new Error("release config and compatibility matrix identities differ");
}
if (releaseConfig.database_schema.current !== String(matrix.database_schema)
  || ["schema_version", "semantic_sha256", "migration_set_sha256"].some(key => releaseConfig.schema_contract[key] !== matrix.schema_contract[key])) {
  throw new Error("release config and compatibility matrix database contracts differ");
}
const configuredUpgradeSources = releaseConfig.supported_upgrade_from_versions;
const matrixUpgradeSources = matrix.supported_upgrade_from_versions;
if (
  !Array.isArray(configuredUpgradeSources)
  || !Array.isArray(matrixUpgradeSources)
  || JSON.stringify(configuredUpgradeSources) !== JSON.stringify(matrixUpgradeSources)
) {
  throw new Error("release config and compatibility matrix upgrade sources differ");
}

const sourceDigest = createHash("sha256");
sourceDigest.update(await readFile(scriptPath));
sourceDigest.update(`${configPath}\0`);
sourceDigest.update(releaseConfigSource);
sourceDigest.update(`${matrixPath}\0`);
sourceDigest.update(matrixSource);
const rendered = [];
const navigation = [];
for (const [relativePath, chapterID] of chapters) {
  const source = await readFile(resolve(root, relativePath), "utf8");
  if (["docs/manual/zh-CN/b-validation.md", "docs/manual/zh-CN/d-validation.md"].includes(relativePath)) {
    for (const value of [releaseConfig.product_version, releaseConfig.manual.version, ...configuredUpgradeSources]) {
      if (!source.includes(`\`${value}\``)) throw new Error(`validation manual is missing current identity ${value}`);
    }
    if (!source.includes(`数据库 schema | ${releaseConfig.database_schema.current}`)) throw new Error("validation manual database generation differs from config");
  }
  if (relativePath === "docs/manual/zh-CN/admin/04-upgrade-migration-rollback-and-restore.md") {
    for (const version of configuredUpgradeSources) {
      if (!source.includes(`\`${version}\``)) {
        throw new Error(`upgrade manual is missing configured source ${version}`);
      }
    }
    const verifiedTargetController = "sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl";
    for (const command of ["upgrade --check", "upgrade --artifact", "recover", "resume"]) {
      if (!source.includes(`${verifiedTargetController} ${command}`)) {
        throw new Error(`upgrade manual is missing verified target-controller command ${command}`);
      }
    }
    if (source.includes("sudo /opt/x-insight/current/bin/xinsightctl upgrade")) {
      throw new Error("upgrade manual must not drive a cross-version upgrade with the current controller");
    }
  }
  if (relativePath === "docs/manual/zh-CN/admin/02-install-and-first-start.md") {
    for (const required of [
      "xinsightctl access generate-subject",
      "xinsightctl access hash-password",
      "xinsightctl access validate",
      "303 Location: /login",
      "401 authentication_required",
      "Secure/HttpOnly/SameSite cookie",
    ]) {
      if (!source.includes(required)) {
        throw new Error(`install manual is missing current access boundary: ${required}`);
      }
    }
  }
  if (relativePath === "docs/manual/zh-CN/reference/08-cli-api-and-error-contract.md") {
    for (const required of [
      "POST /api/v1/access/login",
      "GET /api/v1/access/session",
      "POST /api/v1/access/logout",
      "GET /api/v1/access/sessions",
      "authentication_rate_limited",
      "最大并发 4",
      "GET /api/v1/executions/{execution_id}/trace-journey",
      "xinsight.openinference-bounded-offline-authorization.v1",
      "xinsightctl connector-epoch prepare",
      "xinsightctl clean-baseline data-reset-preview",
      "xinsightctl clean-baseline checkpoint-bootstrap-preview",
      "xinsightctl database-admin-credential preview",
      "xinsight-release campaign-authority derive",
      "xinsight-importer unirouter import-campaign-package",
    ]) {
      if (!source.includes(required)) {
        throw new Error(`CLI/API reference is missing current access contract: ${required}`);
      }
    }
  }
  if (relativePath === "docs/manual/zh-CN/release-notes/compatibility-matrix.md") {
    if (!source.startsWith("# 兼容矩阵与候选发布说明\n\n## 0.3.0-rc.13\n")) {
      throw new Error("compatibility manual does not present 0.3.0-rc.13 as the current release");
    }
    for (const required of [
      "`xinsight.investigation.v1`、`xinsight.access.v1`",
      "`0.3.0-rc.13/manual-r13`",
      "Database schema | 23",
      "Known/Unknown Discovery",
      "loopback TLS terminator",
    ]) {
      if (!source.includes(required)) {
        throw new Error(`compatibility manual is missing 0.3.0-rc.13 identity: ${required}`);
      }
    }
  }
  sourceDigest.update(relativePath);
  sourceDigest.update("\0");
  sourceDigest.update(source);
  const result = renderChapter(source, chapterID);
  rendered.push(result.html);
  navigation.push(`<a href="#${chapterID}">${escapeHTML(result.title)}</a>`);
}
const digest = `sha256:${sourceDigest.digest("hex")}`;

const html = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="xinsight-product-version" content="${escapeHTML(releaseConfig.product_version)}">
  <meta name="xinsight-manual-version" content="${escapeHTML(releaseConfig.manual.version)}">
  <meta name="xinsight-manual-source-sha256" content="${digest}">
  <title>X-Insight ${escapeHTML(releaseConfig.product_version)} 简体中文产品手册</title>
  <link rel="icon" href="manual-icon.svg" type="image/svg+xml">
  <link rel="stylesheet" href="manual.css">
</head>
<body>
  <a class="skip-link" href="#manual-content">跳到手册正文</a>
  <header class="manual-header">
    <p class="eyebrow">X-Insight · release-matched offline manual</p>
    <h1>简体中文产品手册</h1>
    <p>产品版本 <strong>${escapeHTML(releaseConfig.product_version)}</strong> · 手册版本 <strong>${escapeHTML(releaseConfig.manual.version)}</strong></p>
    <p class="boundary">${budget ? "内部单部署租户 · loopback-only · 正文读取默认关闭 · 按当前授权显式读取并核验 · 尚非 Production verified" : "内部单部署租户 · loopback-only · metadata-only · no raw · 尚非 Production verified"}</p>
    <a class="product-link" href="/executions">返回执行调查</a>
  </header>
  <div class="manual-layout">
    <nav class="toc" aria-label="手册目录">
      <strong>目录</strong>
      ${navigation.join("\n      ")}
    </nav>
    <main id="manual-content">
      ${rendered.join("\n      ")}
    </main>
  </div>
  <footer>
    <span>X-Insight ${escapeHTML(releaseConfig.product_version)}</span>
    <span>${escapeHTML(releaseConfig.manual.version)}</span>
    <span>生成源 ${digest}</span>
  </footer>
</body>
</html>
`;

const css = `:root {
  color-scheme: light;
  font-family: Inter, "Noto Sans SC", "PingFang SC", "Microsoft YaHei", system-ui, sans-serif;
  color: #193430;
  background: #f4f8f7;
}
* { box-sizing: border-box; }
body { margin: 0; line-height: 1.72; }
a { color: #176b61; }
.skip-link { position: absolute; left: -9999px; }
.skip-link:focus { left: 18px; top: 12px; z-index: 3; padding: 8px 12px; background: #fff; }
.manual-header { padding: 42px max(24px, calc((100vw - 1180px) / 2)); color: #eaf8f5; background: #0c302d; }
.manual-header h1 { margin: 4px 0 8px; font-size: clamp(32px, 5vw, 52px); line-height: 1.12; }
.manual-header p { margin: 7px 0; }
.eyebrow { color: #8bc9bf; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; letter-spacing: .08em; text-transform: uppercase; }
.boundary { color: #b7d9d3; }
.product-link { display: inline-block; margin-top: 14px; padding: 8px 12px; border: 1px solid #6ca99f; border-radius: 7px; color: #eaf8f5; text-decoration: none; }
.manual-layout { display: grid; grid-template-columns: 250px minmax(0, 850px); gap: 38px; max-width: 1180px; margin: 0 auto; padding: 34px 24px 64px; }
.toc { position: sticky; top: 22px; align-self: start; display: grid; gap: 5px; max-height: calc(100vh - 44px); overflow: auto; padding: 18px; border: 1px solid #cbded9; border-radius: 12px; background: #fff; }
.toc strong { margin-bottom: 7px; }
.toc a { padding: 5px 7px; border-radius: 5px; color: #365c56; font-size: 13px; text-decoration: none; }
.toc a:hover, .toc a:focus { background: #e8f4f1; }
main { min-width: 0; }
.chapter { min-width: 0; overflow-x: clip; margin-bottom: 32px; padding: 30px; border: 1px solid #d4e3df; border-radius: 14px; background: #fff; box-shadow: 0 6px 24px rgba(20, 65, 59, .05); }
h2 { margin: 0 0 18px; color: #123d37; font-size: 27px; line-height: 1.25; }
h3 { margin: 28px 0 10px; color: #18554d; font-size: 20px; }
h4 { margin: 22px 0 8px; font-size: 16px; }
p, ul, ol, .table-scroll, pre, blockquote { margin: 12px 0; }
li + li { margin-top: 5px; }
code { overflow-wrap: anywhere; padding: 2px 5px; border-radius: 4px; color: #744224; background: #f6eee7; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .9em; }
pre { overflow: auto; padding: 15px; border-radius: 9px; color: #eaf8f5; background: #102c29; }
pre code { overflow-wrap: normal; padding: 0; color: inherit; background: transparent; }
blockquote { padding: 10px 15px; border-left: 4px solid #5ca99d; color: #365c56; background: #eef7f5; }
.table-scroll { width: 100%; max-width: 100%; overflow-x: auto; }
table { width: max-content; min-width: 100%; border-collapse: collapse; font-size: 14px; }
th, td { padding: 10px; border: 1px solid #d5e3df; text-align: left; vertical-align: top; }
th { background: #edf6f4; }
footer { display: flex; flex-wrap: wrap; justify-content: center; gap: 18px; padding: 20px 24px; color: #66807c; background: #e7f0ee; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px; }
footer span { min-width: 0; max-width: 100%; overflow-wrap: anywhere; }
@media (max-width: 860px) {
  .manual-layout { grid-template-columns: 1fr; }
  .toc { position: static; max-height: none; }
  .chapter { padding: 22px; }
}
@media print {
  .toc, .product-link, .skip-link { display: none; }
  .manual-layout { display: block; max-width: none; padding: 0; }
  .chapter { break-inside: avoid; border: 0; box-shadow: none; }
}
`;

const icon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64" role="img" aria-label="X-Insight">
  <rect width="64" height="64" rx="14" fill="#0c302d"/>
  <path d="M17 18h8l7 10 7-10h8L36 32l12 14h-8l-8-10-8 10h-8l12-14z" fill="#9ed9cf"/>
</svg>
`;

await mkdir(outputDir, { recursive: true });
await writeFile(resolve(outputDir, "index.html"), html, "utf8");
await writeFile(resolve(outputDir, "manual.css"), css, "utf8");
await writeFile(resolve(outputDir, "manual-icon.svg"), icon, "utf8");
console.log(`manual_html=${resolve(outputDir, "index.html")}`);
console.log(`manual_css=${resolve(outputDir, "manual.css")}`);
console.log(`manual_icon=${resolve(outputDir, "manual-icon.svg")}`);
console.log(`manual_source_sha256=${digest}`);

function renderChapter(markdown, chapterID) {
  const lines = markdown.replace(/\r\n/g, "\n").split("\n");
  const firstHeading = lines.find((line) => /^#\s+/.test(line));
  if (!firstHeading) {
    throw new Error(`chapter ${chapterID} has no level-one heading`);
  }
  const title = firstHeading.replace(/^#\s+/, "").trim();
  const output = [`<section class="chapter" aria-labelledby="${chapterID}">`];
  let firstTitleRendered = false;
  let paragraph = [];
  let listType = "";
  let code = null;

  const closeParagraph = () => {
    if (paragraph.length) {
      output.push(`<p>${inline(paragraph.join(" "))}</p>`);
      paragraph = [];
    }
  };
  const closeList = () => {
    if (listType) {
      output.push(`</${listType}>`);
      listType = "";
    }
  };
  const closeFlow = () => {
    closeParagraph();
    closeList();
  };

  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    if (code !== null) {
      if (/^```/.test(line)) {
        output.push(`<pre><code>${escapeHTML(code.join("\n"))}</code></pre>`);
        code = null;
      } else {
        code.push(line);
      }
      continue;
    }
    if (/^```/.test(line)) {
      closeFlow();
      code = [];
      continue;
    }
    const heading = line.match(/^(#{1,3})\s+(.+)$/);
    if (heading) {
      closeFlow();
      if (heading[1].length === 1 && !firstTitleRendered) {
        output.push(`<h2 id="${chapterID}">${inline(heading[2])}</h2>`);
        firstTitleRendered = true;
      } else {
        const level = Math.min(4, heading[1].length + 1);
        output.push(`<h${level}>${inline(heading[2])}</h${level}>`);
      }
      continue;
    }
    if (line.startsWith("|")) {
      closeFlow();
      const tableLines = [];
      while (index < lines.length && lines[index].startsWith("|")) {
        tableLines.push(lines[index]);
        index += 1;
      }
      index -= 1;
      output.push(renderTable(tableLines));
      continue;
    }
    const unordered = line.match(/^-\s+(.+)$/);
    const ordered = line.match(/^\d+\.\s+(.+)$/);
    if (unordered || ordered) {
      closeParagraph();
      const wanted = ordered ? "ol" : "ul";
      if (listType !== wanted) {
        closeList();
        output.push(`<${wanted}>`);
        listType = wanted;
      }
      output.push(`<li>${inline((unordered || ordered)[1])}</li>`);
      continue;
    }
    if (line.startsWith("> ")) {
      closeFlow();
      output.push(`<blockquote>${inline(line.slice(2))}</blockquote>`);
      continue;
    }
    if (line.trim() === "") {
      closeFlow();
      continue;
    }
    paragraph.push(line.trim());
  }
  if (code !== null) {
    throw new Error(`chapter ${chapterID} has an unclosed code block`);
  }
  closeFlow();
  output.push("</section>");
  return { title, html: output.join("\n") };
}

function renderTable(lines) {
  if (lines.length < 2 || !/^\|?\s*:?-{3}/.test(lines[1])) {
    throw new Error("manual table requires a header separator row");
  }
  const rows = lines.map((line) => line.slice(1, -1).split("|").map((cell) => cell.trim()));
  const width = rows[0].length;
  if (rows.some((row) => row.length !== width)) {
    throw new Error("manual table row width mismatch");
  }
  const header = `<thead><tr>${rows[0].map((cell) => `<th>${inline(cell)}</th>`).join("")}</tr></thead>`;
  const bodyRows = rows.slice(2).map((row) => `<tr>${row.map((cell) => `<td>${inline(cell)}</td>`).join("")}</tr>`).join("");
  return `<div class="table-scroll"><table>${header}<tbody>${bodyRows}</tbody></table></div>`;
}

function inline(value) {
  return escapeHTML(value)
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    .replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
}

function escapeHTML(value) {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}
