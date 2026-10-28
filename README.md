# X-Insight

[Foigent](https://github.com/foigent) · [UniRouter](https://github.com/foigent/unirouter) · [简体中文](README.zh-CN.md)

X-Insight is an evidence workspace for understanding AI work: what a gateway observed, which requests incurred costs, what an agent reported doing, and what a person concluded after reviewing the evidence.

It receives source records asynchronously and keeps their identities, revisions, coverage gaps, and access boundaries. A timeline is not proof of causality; an agent report is not an independently verified file result; an unknown amount is not zero. These distinctions remain visible as you investigate.

Use X-Insight independently for supported imports, reported work activity, investigation, and human review. Use UniRouter independently to execute and route authorized model requests, apply execution policy, and record usage and economic facts. Combine them when you need to follow a gateway execution into an authorized evidence workspace. X-Insight (XI) consumes UniRouter (UR) facts asynchronously; it never becomes a synchronous request dependency.

## Status and support

The source is public under **Apache-2.0**. The published preview is **0.2.0-dev.fpv20, schema 46**: [download the seven release attachments](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv20) and follow the [installation guide](docs/community/getting-started.md). The complete product source includes access control, investigation, workbench, connectors, and managed installation code. There is no separate paid feature tree. Authentication, source/project grants, trusted release verification, and tenant boundaries remain enforced.

This is a development preview for a **single deployment tenant**, not a stable or Production release. As of 2026-10-23, the exact signed FPV20 package has passed Linux qualification, an empty-database installation on one existing Ubuntu 22.04 amd64 host with pinned container PostgreSQL 16, and normal owner HTTPS access. The installed UR→XI connection, source registration and eight-profile empty synchronization were accepted on 2026-10-27. First-real-fact and ordinary-reader scope acceptance remain pending. The CLI accepts Ubuntu 22.04/24.04 amd64; one accepted host does not qualify every topology. See the [status matrix](docs/community/release-readiness.md) for the separate source, artifact, host, and integration results.

The UI defaults to English, with a persistent Simplified Chinese selector. The same-version offline manual is embedded at `/manual/en-US/` and `/manual/zh-CN/`. API codes, source identities, raw evidence, currency, and saved reading scope do not change with the UI language.

## What you can do

- Read a fixed, authorized view of request usage, CNY costs, trends, contributions, and budget summaries; inspect exactly which records and coverage limits support it.
- Follow request-level outcomes, retries, individual attempts, destination and policy observations, without adding attempt costs to the parent total.
- Inspect execution evidence, source-declared trace links, capture gaps, provenance, and revisions.
- Review reported agent work, resource reads and changes, and model associations; keep reported activity separate from independently checked results.
- Explicitly read authorized captured content and verify its bytes and digest. Capture, source references, XI review copies, expiry, and reading permission have distinct states.
- Create investigations, adopt exact evidence revisions, record human assessments, follow-up responsibility, and review history.
- Connect supported UniRouter feeds and source-fact profiles; manage scoped access and supported OpenClaw work-activity intake. Protocol compatibility and activation still require their own checks.

## Start here

**Install → choose your own password → sign in → connect UniRouter → confirm access and the first fact.**

1. Use the [packaged-preview walkthrough](docs/community/getting-started.md). It explains the verified download, host preparation, and hidden password entry required before installation can complete. A package user does not need Go, Node.js, or a source build.
2. Follow [normal UniRouter connection and first-fact checks](docs/community/getting-started.md#connect-unirouter-and-verify-the-pair). Each administrator confirms their own installation and grants; connection management does not grant every reader access.
3. Keep the [release-matched manual](docs/manual/en-US/index.md), [FPV20 installation erratum](docs/community/releases/v0.2.0-dev.fpv20.md#fpv20-installation-erratum--fpv20-安装勘误), and [data-handling boundaries](docs/community/data-handling.md) alongside that walkthrough.

For source builds, synthetic fixtures, database/browser tests, and historical pairing recipes, use the separate [development guide](docs/community/development.md). Its pinned build tools and qualification fixtures are contributor prerequisites, not extra customer installation steps.

## How it fits together

UniRouter is responsible for execution, admission, routing, and economic facts. X-Insight is an asynchronous evidence and workspace consumer. A gateway call does not synchronously depend on an XI screen or human assessment.

The Go service embeds the React UI and offline manual. Importers and source workers validate and persist source facts; the read service uses constrained database roles. Workspace writes and administrator operations have separate privileges. See the [architecture map](docs/community/architecture.md) and [source contracts](contracts/). Shared protocol contracts and retained compatibility sources support the actual adapters, release checks, upgrade tests, and license provenance; they are not additional products to install. Historical design and validation records remain secondary references.

## Contribute

Start with [CONTRIBUTING.md](CONTRIBUTING.md) and the [contribution map](docs/community/contribution-map.md). Reports, reproductions, translations, documentation, and focused patches are useful contributions. English is the canonical documentation language; Chinese discussion is welcome, with a brief English summary for substantial decisions. English fluency is not a contribution gate.

Use [GitHub issues](https://github.com/foigent/x-insight/issues) for non-sensitive bugs and proposals. Read [SECURITY.md](SECURITY.md) before sharing vulnerability details. This repository does not claim that Discussions or private vulnerability reporting is enabled.

## License

[Apache License 2.0](LICENSE), copyright X-Insight contributors. Contributions use [DCO 1.1](https://developercertificate.org/) sign-off; there is no CLA. Bundled dependencies retain their original licenses: [third-party notices](THIRD_PARTY_NOTICES.md).
