# X-Insight product manual

This manual describes the complete development candidate, including access, source collection, investigations, costs, runtime evidence, and review. Apache-2.0 licensing does not remove authentication, source consent, field permissions, signatures, or audit requirements. There is no separate paid feature edition.

X-Insight (XI) can be used independently for supported imports, reported work activity, investigation, and review. UniRouter (UR) independently executes and routes authorized model requests, applies execution policy, and records usage and economic facts. When connected, XI receives authorized UR facts asynchronously for analysis and review; it does not enter the synchronous request path. The shared protocols and Integration Control support that connection rather than introduce more products to select.

## Version and support

Product `0.2.0-dev.fpv19`; manual `0.2.0-dev.fpv19/manual-r1`.

| Item | Current contract |
| --- | --- |
| Database schema | 45 |
| Database | Dedicated PostgreSQL 16, loopback only |
| Application | Linux amd64; systemd for managed installations |
| Installer runtime host prerequisite | Ubuntu 22.04 or 24.04 amd64 with systemd; exact artifact/topology qualification is separate |
| Previously documented isolated validation profile | Ubuntu 24.04 guest with native PostgreSQL; not current FPV19 Target acceptance |
| Pending native-host artifact qualification | Ubuntu 22.04 with a dedicated pinned PostgreSQL 16 container; fresh-install evidence pending |
| Configuration | `xinsight.config.v1` |
| APIs | `xinsight.investigation.v1`, `xinsight.access.v1` |
| Default language | English; Simplified Chinese is available in the language control |

This is a development candidate, not a public stable release or a production acceptance claim. The repository also retains an Ubuntu 22.04 CI installation lane and a frozen rc.13 profile. Their results and artifacts are distinct. Use the exact profile, signed artifact, trust authority, and host qualification for the intended installation; do not apply historical instructions to another profile.

Provision the documented system dependencies and a dedicated loopback PostgreSQL 16 database before installation. The supplied Compose profile uses a pinned PostgreSQL container; the application still runs natively under systemd. A fresh install requires an empty database and unused XI application paths and unit names. If XI is already installed, use its supported upgrade path or first back up and retire its exact application objects under your maintenance and recovery procedure. A data reset, test-root mapping or deletion of only install-state metadata is not a fresh installation. Preserve shared services and databases. Exact FPV19 artifact qualification on Ubuntu 22.04 with container PostgreSQL is pending; preinstalled OS dependencies and completed evidence must be recorded separately.

The configured upgrade-source list is `0.2.0-dev.b2`, `0.2.0-dev.d1`, `0.2.0-dev.d2`, `0.2.0-dev.fpv1`, `0.2.0-dev.fpv2`, `0.2.0-dev.fpv3`, `0.2.0-dev.fpv4`, `0.2.0-dev.fpv5`, `0.2.0-dev.fpv11`, `0.2.0-dev.fpv12`, `0.2.0-dev.fpv13`, `0.2.0-dev.fpv14`, `0.2.0-dev.fpv15`, `0.2.0-dev.fpv16`, `0.2.0-dev.fpv17`, `0.2.0-dev.fpv18`. Inclusion is a preflight compatibility declaration, not evidence that every source-to-target installation has passed. Verify the actual predecessor and candidate separately. Schema45 does not authorize application rollback to an older binary.

## Build and try locally

Use Go 1.25.12, Node 22.23.1, npm 10.9.8, Python 3, and PostgreSQL 16. From the repository root:

```sh
go mod download
npm --prefix frontend ci
node scripts/build-manual.mjs --profile=development-d
npm --prefix frontend run build
python3 scripts/collect-third-party-notices.py --check
go build ./cmd/...
TZ=UTC npm --prefix frontend test
```

The repository's `docs/community/getting-started.md` gives the disposable PostgreSQL and real HTTPS browser-harness procedure. That harness creates synthetic data and a test account; it is not an installed service or a real source connection. The normal frontend browser suite uses mocked responses. Keep those evidence levels distinct. Never point integration tests at a valuable database. Do not put a real DSN in a command, screenshot, issue, or shell history.

A clean checkout can prepare an unsigned Linux payload using preinstalled dependencies:

```sh
bash scripts/build-signed-release.sh /absolute/new-preflight --profile=development-d --dry-run
bash scripts/build-signed-release.sh /absolute/new-prepared --profile=development-d --prepare-only
```

Dry-run is read-only identity/toolchain preflight. Preparation builds binaries and payload checksums but does not sign or produce an installable release. Neither operation is a deployment. The source, embedded UI, both manuals, release config, compatibility matrix, and third-party license texts must match the same commit. The preparation result records that commit and each payload digest.

## Install and establish access

Managed installation needs an explicitly selected, qualified host, a dedicated empty PostgreSQL16 database, separate admin/reader/workspace/importer roles, an approved service account, and a trusted signed artifact. There is no public stable installer or default production login. Obtain the artifact, bootstrap verifier, and public trust store through the operator's approved release process. A trust store bundled with an untrusted download does not establish trust by itself.

After independently checking the bootstrap verifier and trust authority, verify and extract into a new directory:

```text
xinsight-release verify --artifact /approved/candidate.tar.gz --trust-store /etc/x-insight/trusted-release-keys.json --expected-commit EXACT_COMMIT --expected-version 0.2.0-dev.fpv19
xinsight-release extract --artifact /approved/candidate.tar.gz --trust-store /etc/x-insight/trusted-release-keys.json --destination /approved/staging/candidate --expected-commit EXACT_COMMIT --expected-version 0.2.0-dev.fpv19
sudo /approved/staging/candidate/bin/xinsightctl version --json
```

Use the verified payload's deploy templates. Preserve the four independent protected DSN files under `/etc/x-insight/database/current/`: `postgres-admin.dsn`, `postgres-reader.dsn`, `postgres-workspace.dsn`, and `postgres-importer.dsn`. They refer to one dedicated loopback database and distinct roles; do not pass their secrets as arguments or service environment values. Host tools, file ownership, database identity, listener scope, and profile must pass the controller's checks.

Create a stable local subject and password hash with the verified controller. Password hashing requires an interactive TTY and two hidden entries; do not pipe a password or use an environment variable.

```text
sudo /approved/staging/candidate/bin/xinsightctl access generate-subject
sudo /approved/staging/candidate/bin/xinsightctl access hash-password
sudo /approved/staging/candidate/bin/xinsightctl access validate --file /etc/x-insight/access-accounts.json
```

The protected account file has `schema_version:"xinsight.local-accounts.v3"` and an `accounts` array. Each entry has explicit `subject_id`, normalized `username`, `role`, `disabled`, and `password_hash` fields. Use the generated subject and hash, not sample credentials. Preserve existing accounts. The file must be root-owned, regular, single-link, non-symlink, with mode0400 or0600. Validation prints counts, not account secrets. For ordinary investigation, use `workbench_investigation_reader`; saving reviews requires `workbench_investigation_reviewer`. An older `investigator` role does not automatically grant workbench access. Source/project grants are separately required.

```text
sudo /approved/staging/candidate/bin/xinsightctl install --check --artifact /approved/candidate.tar.gz
sudo /approved/staging/candidate/bin/xinsightctl install --artifact /approved/candidate.tar.gz
sudo /opt/x-insight/current/bin/xinsightctl status
```

Only run the mutating install after its preflight passes and the deployment is authorized. Installation creates the managed deployment identity; do not copy or edit it. A first install does not automatically connect to UniRouter or start an unconfigured source worker. Keep application/database listeners on loopback and configure the approved HTTPS origin and trusted local TLS terminator before browser login. Unauthenticated product navigation redirects to login; unauthenticated API calls return401. Session cookies remain Secure/HttpOnly/SameSite. The browser refuses password submission over plain HTTP.

## Connect the first source

An administrator starts setup from Connections. Confirm the exact XI deployment, UniRouter installation, source projects, collection capabilities, and retention choices. Follow the invitation to UniRouter and complete consent there. Historical consent does not override current source policy. Invitation secrets are transient; do not paste them into tickets or persistent browser storage.

The setup page distinguishes waiting for consent, preparing the connection, waiting for data, and actionable failures. Configuration saved is not the same as a worker loading it; a loaded worker is not proof that data arrived. Confirm source status and a received execution with matching source identity. X-Insight receives facts asynchronously; it is not a synchronous admission dependency for UniRouter requests.

With no existing XI retention policy, setup proposes full body,90days,5GiB per project,32MiB per item. This is a draft requiring explicit confirmation. Existing active or disabled policy is reused. UniRouter source capture is independently approved; its normal proposal is full body,30days,5GiB per project,32MiB per item, subject to actual supported protocols and observation points. Metadata collection does not implicitly authorize body capture or reading. Enterprise source-policy changes require the corresponding administrator permissions; project/user exceptions remain effective.

For interrupted setup, return to the same request and inspect its current state before retrying. The UI keeps non-secret request references for the current subject. An expired invitation needs a new review and consent. Do not create a replacement source to hide a failed original connection. Active control credentials renew on the original installation; recovery after expiry requires administrators on both sides and the original retained source secret. Revoked access or scope drift is not silently restored.

## Grant ordinary users the right scope

Role capabilities and active grants both apply. Grants form a union; a narrow grant cannot restrict another broad one. UniRouter projects use `project-<project_id>`, not a bare number. The following is a shape example, not an instruction to grant every project. Replace every identity and scope with an approved, registered value:

```json
{
  "schema_version": "xinsight.workbench-grant-change.v1",
  "action": "create",
  "grant_id": "11111111-1111-4111-8111-111111111111",
  "subject_id": "xs_1111111111111111",
  "source_system_id": "22222222-2222-4222-8222-222222222222",
  "source_project_ref": "project-42",
  "cost_project_ref": "replace-with-actual-cost-project-ref",
  "expected_revision": 0,
  "capabilities": ["summary", "details"]
}
```

```text
sudo xinsightctl workbench-access apply --file /approved/grant.json --check
sudo xinsightctl workbench-access apply --file /approved/grant.json
sudo xinsightctl workbench-access show --grant-id 11111111-1111-4111-8111-111111111111
```

Use protected admin access for these commands. Check is read-only; apply rechecks current state and records the change atomically with its audit. Existing grants need their current revision and a complete `replace`, or explicit `revoke`; do not replay stale create requests. Budget-only access uses `budget_definition`, `budget_summary`, and `budget_alert`, exact source project, and `cost_project_ref:"*"`. Keep that grant separate from restricted cost details. It does not grant all execution details.

Execution-linked resource reviews also require the exact target and resource-reporter scopes and the relevant `source_context`, `source_identity`, and `source_cost` fields. Preview and submission enforce the same current permissions. C content grants additionally carry explicit content purposes; a v1 example must not overwrite a v2 content grant. Permission changes invalidate old readings. Obtain a fresh reading instead of repeatedly submitting a stale page.

## Investigate, read costs, and review

Start with the overview or one of the usage/purpose, runtime/execution, security/review, cost/usage, or source/coverage topics. Select the period and permitted projects, then open the execution. Check its source, execution identifier, reading ID, cutoff, coverage, and evidence references before drawing a conclusion. The same reading freezes the evidence basis; later source revisions do not rewrite it.

Follow an execution to its runtime requests, resources, source facts, and related work. Runtime outcomes and economic facts are separate: a failed or missing charge does not prove an execution failed, and a missing resource event does not prove no access occurred. A provider request shows what was submitted, not what a model internally used. Related work is grouped only when its actual relationship evidence permits it.

Costs retain their original currency and precision. Do not add unlike currencies, source totals and overlapping XI details, or independent snapshots. Budget remaining-to-plan is not a cash balance, reservation, or permission to call a model. Source budget periods retain their UTC bounds; a global seven-day filter cannot redefine a source billing period.

Trend lines connect adjacent valid known points, including an observed zero with incomplete coverage. Missing, invalid, or nonadjacent intervals break the line. An observed zero is not proof of no activity. Positive and negative costs remain visible; negative values may represent corrections. Focus or hover a point for its exact interval and value. Timezone and ISO evidence timestamps do not change when the UI language changes.

For a review, inspect the selected evidence and preview the precise links before saving. A reader cannot gain write permission by receiving broader data grants. Permission loss, expiry, invalid input, and service failure remain distinct states. After saving, open the stored review and return to the original reading. Check both the saved references and the current access result; do not infer success from a click or a stale panel. Subject drafts, filters, and review references remain in place when switching languages; raw source text, identifiers, protocols, and API codes are not translated.

## Content, retention, and gaps

Body capture and body reading are separate opt-ins. Read a body only with current content authorization, the exact purpose/reference, and an unexpired copy. Retention or permission expiry can leave permitted metadata visible while removing body access. Unknown, unavailable, unauthorized, unsupported, and expired do not mean empty or zero. Do not reconstruct missing content from an old screen.

The managed body key must be loaded by both the API and relevant worker. A file on disk alone does not establish this. If ciphertext exists and its original key is missing, stop and restore that key; generating a replacement cannot decrypt historical bodies. A complete private backup includes the database and original key. Protect that backup as sensitive data.

## Upgrade, recovery, and diagnosis

Verify the exact next artifact and extract its controller before an upgrade. Use the target controller to validate the actual predecessor; do not drive a cross-version upgrade with the old installed controller.

```text
sudo /approved/staging/candidate/bin/xinsightctl upgrade --check --artifact /approved/candidate.tar.gz
sudo /approved/staging/candidate/bin/xinsightctl upgrade --artifact /approved/candidate.tar.gz
sudo /approved/staging/candidate/bin/xinsightctl status
```

Preflight must leave service state, database, current release, and operation records unchanged. The mutating upgrade needs an approved window, exact source identity, validated backup, and sufficient disk capacity. Preserve the original installation, source identities, grants, checkpoints, retained keys, and facts. A same-schema upgrade still needs artifact and service lifecycle qualification.

```text
xinsightctl backup --output /absolute/private-backup.tar --check
xinsightctl backup --output /absolute/private-backup.tar
xinsightctl restore --recovery-bundle /absolute/private-backup.tar --recovery-bundle-sha256 sha256:EXACT_DIGEST --new-database xinsight_restore_approved --check
```

Do not run a restore over valuable data. Review the exact private backup digest and a new dedicated database/installation destination, then use the corresponding approved restore procedure. Restoring to a different deployment revokes restored runtime identities and tokens; retained history remains, and reconnection requires current authorization. Expired bodies remain unreadable. A database-only historical backup does not establish restoration of body keys.

After an interrupted operation, keep its identity and inspect status before using `xinsightctl recover` or `xinsightctl resume` with that operation's required arguments. These are recovery actions, not generic retry buttons. Diagnose signature/version mismatch, credential ownership, database identity/schema, missing key, denied scope, source expiry, and network errors separately. Never relax checks to bypass one of them.

For a bug report, give the exact commit/profile, OS/toolchain, sanitized reproduction, expected/actual behavior, and whether the evidence is Local, CI, Artifact, Target, or Production. Keep real prompts, bodies, DSNs, tokens, account hashes, private host identifiers, and raw logs out of public issues. Private vulnerability reporting is not yet verified as an enabled repository feature; request a private contact without publishing exploit details. See root `SECURITY.md` and `CONTRIBUTING.md` for the current contribution process and manual DCO sign-off.
