# X-Insight

[Foigent](https://github.com/foigent) · [UniRouter](https://github.com/foigent/unirouter) · [简体中文](README.zh-CN.md)

X-Insight is an evidence workspace for understanding AI work: what a gateway observed, which requests incurred costs, what an agent reported doing, and what a person concluded after reviewing the evidence.

It receives source records asynchronously and keeps their identities, revisions, coverage gaps, and access boundaries. A timeline is not proof of causality; an agent report is not an independently verified file result; an unknown amount is not zero. These distinctions remain visible as you investigate.

Use X-Insight independently for supported imports, reported work activity, investigation, and human review. Use UniRouter independently to execute and route authorized model requests, apply execution policy, and record usage and economic facts. Combine them when you need to follow a gateway execution into an authorized evidence workspace. X-Insight (XI) consumes UniRouter (UR) facts asynchronously; it never becomes a synchronous request dependency.

## Status and support

The current source candidate is **0.2.0-dev.fpv19, schema 45**. The complete product source is offered under Apache-2.0, including access control, investigation, workbench, connectors, and managed installation code. There is no separate paid feature tree. Authentication, source/project grants, trusted release verification, and tenant boundaries remain enforced.

This is a development candidate for a **single deployment tenant**. The current managed validation profile specifies an isolated Ubuntu 24.04 amd64 host with systemd and a dedicated native PostgreSQL 16 database. The repository also retains historical Ubuntu 22.04 qualification lanes and the frozen rc13 release profile; those are not interchangeable deployment promises. There is no public stable download or unattended community installer to claim here. Local checks, CI, signed artifacts, installed-host qualification, external integration, and production acceptance are separate evidence levels. See [release readiness](docs/community/release-readiness.md).

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

1. [Build, test, and open a local workspace](docs/community/getting-started.md). Start with synthetic fixtures; no paid model account is required for offline checks.
2. [Read the current product manual](docs/manual/en-US/index.md) for sign-in, first source connection, reading, content, review, installation, and recovery.
3. [Understand data handling](docs/community/data-handling.md) before connecting real sources or enabling content capture.
4. [Connect UniRouter and check the tested pairing](docs/community/getting-started.md#connect-unirouter-and-verify-the-pair). Follow UniRouter's [source and installation entry](https://github.com/foigent/unirouter/blob/main/docs/community/getting-started.md) and each product's actual release status; two independent test passes are not a joint acceptance result.

Pinned build tools: **Go 1.25.12, Node.js 22.23.1, npm 10.9.8**, Python 3, and PostgreSQL 16 for database tests. Browser tests use the Playwright version pinned in the frontend lockfile. Do not substitute a floating toolchain for a release build.

## How it fits together

UniRouter is responsible for execution, admission, routing, and economic facts. X-Insight is an asynchronous evidence and workspace consumer. A gateway call does not synchronously depend on an XI screen or human assessment.

The Go service embeds the React UI and offline manual. Importers and source workers validate and persist source facts; the read service uses constrained database roles. Workspace writes and administrator operations have separate privileges. See the [architecture map](docs/community/architecture.md) and [source contracts](contracts/).

## Contribute

Start with [CONTRIBUTING.md](CONTRIBUTING.md) and the [contribution map](docs/community/contribution-map.md). Reports, reproductions, translations, documentation, and focused patches are useful contributions. English is the canonical documentation language; Chinese discussion is welcome, with a brief English summary for substantial decisions. English fluency is not a contribution gate.

Use [GitHub issues](https://github.com/foigent/x-insight/issues) for non-sensitive bugs and proposals. Read [SECURITY.md](SECURITY.md) before sharing vulnerability details. This repository does not claim that Discussions or private vulnerability reporting is enabled.

## License

[Apache License 2.0](LICENSE), copyright X-Insight contributors. Contributions use [DCO 1.1](https://developercertificate.org/) sign-off; there is no CLA. Bundled dependencies retain their original licenses: [third-party notices](THIRD_PARTY_NOTICES.md).
