# Alibaba Cloud Single-Node Target Runbook

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: Active
Owner: X-Insight
Applies to: non-production Alibaba Cloud ECS single-node deployments

## Purpose

This runbook defines the project-owned deployment and acceptance requirements
for the current X-Insight backend. It is stable project documentation, not an
operator task file and not evidence that a particular deployment succeeded.

Per-target X-Insight operator requests and sanitized results belong under
`docs/ops/atlas/`. Cross-project UniRouter/X-Insight contracts and evidence stay
in the shared cooperation workspace. Operator results become project evidence
only after execution-owner reconciliation.

## Current Product Boundary

The current backend provides:

- PostgreSQL migrations;
- sanitized evidence import and idempotency handling;
- health and readiness endpoints;
- bounded execution, event and capture-gap read APIs;
- an embedded authenticated execution-search/investigation UI;
- root-provisioned local accounts, in-memory Sessions, fixed RBAC and access
  audit; and
- a release-matched offline Chinese product manual.

It does not yet provide:

- customer self-service account lifecycle, SSO or an external identity
  provider;
- general-purpose public API or unsupervised customer exposure;
- HA/DR or encrypted-storage proof;
- continuous UniRouter database access or exporter/outbox;
- raw prompt/response storage.

The application listener and PostgreSQL must remain loopback-only. Authenticated
operator UI access is supported only through the separately authorized,
same-host TLS terminator path shipped as signed payload templates, with one
pinned external HTTPS origin and publicly trusted certificate. A deployment
under this runbook can become `Target verified` for a named non-production
environment, but not `Production verified`.

## Minimum Target

| Resource | Minimum current baseline |
| --- | --- |
| CPU | 4 vCPU, x86_64 |
| Memory | 8 GiB |
| OS | Ubuntu 22.04 LTS |
| System disk | 40 GiB |
| Data disk | 50 GiB expandable ESSD |
| Runtime | Docker Engine 24+ and Compose 2.20+ |
| Delivery tooling | Independently hash-verified bootstrap verifier and signed Owner-Supervised delivery; no Target-side source build |
| Database | PostgreSQL 16 |
| Management | Alibaba Cloud Assistant/Session Manager or another approved private path |

No Redis, ClickHouse, Kafka, object storage, monitoring stack, Kubernetes, or
model-provider credential is required for the current backend baseline.

## Non-Negotiable Security Boundary

1. Use a dedicated security group; never inherit a group that exposes all TCP
   ports or unrelated application layers.
2. Default-deny public inbound traffic. Cloud Assistant is the preferred initial
   management path; public SSH is not a standing requirement.
3. Before detaching a previous group, verify both Cloud Assistant command
   execution and the control-plane capability to reattach the previous group.
4. Verify management again after detachment and read back final group rules.
5. Enable host-firewall default-deny inbound.
6. Bind PostgreSQL only to `127.0.0.1:5432`.
7. Bind X-Insight exactly to `127.0.0.1:8080`; a TLS terminator must proxy only
   to this loopback listener.
8. Do not add an improvised auth proxy or ad hoc exposure. Authenticated UI
   validation requires the signed `trusted-tls-proxy.env.example` and
   `xinsight-nginx-ip.conf.example` templates, one canonical HTTPS origin, a
   publicly trusted certificate, and separate authority for public ports
   80/443.
   The X-Insight HTTPS server retains its 8 KiB general request limit. Its
   exact `/api/v1/work-event-ingest/contents` location permits 32 MiB review
   bodies and streams them to `127.0.0.1:8080` without Nginx request-body
   buffering. Check the effective location with `nginx -T` and validate any
   configuration change with `nginx -t` before a graceful reload. Before
   checking the result, confirm the new Nginx worker generation is accepting
   connections; an immediate probe can still reach a draining old worker.
   Anonymous synthetic POSTs of 16 KiB and exactly 32 MiB to this path should
   reach X-Insight and return 401; 32 MiB plus one byte should return 413.
   A 16 KiB POST to `/api/v1/work-event-ingest/items` should remain 413. The
   32 MiB unauthenticated 401 proves proxy-size admission, not ciphertext
   custody: X-Insight may reject before reading the entire body. These probes
   use fixed synthetic bytes, no credential or user content.
9. Secrets must not appear in Git, Markdown, shell history, process arguments,
   Cloud Assistant command content/output, or broad environment dumps.

Any wildcard X-Insight backend/database listener, or public ingress other than
the explicitly authorized TLS terminator on ports 80/443, is a blocking
failure.

## Approved Operator UI Access And Loopback Diagnostics

The current trial UI uses the root-provisioned local accounts and `/login`
defined in the release-matched
[installation manual](../../manual/zh-CN/admin/02-install-and-first-start.md).
Authenticated UI access requires the approved same-host TLS terminator and
external origin described there. After the signed templates are instantiated,
their owner/mode/content and certificate are verified, and ports 80/443 are
separately authorized, the operator uses
`https://APPROVED_HOST_OR_IP/executions`. The acceptance record must prove HSTS,
the Secure/HttpOnly/SameSite cookie, same-origin login/logout, authenticated
views, and anonymous 303/401 boundaries. Do not submit product credentials over
HTTP, a generic private proxy, or a tunnel URL.

The SSH forward below is retained only for loopback diagnostics such as
`readyz` and anonymous redirect checks. It cannot establish authenticated UI or
Target acceptance evidence.

On the tested macOS `ali-instance-cli` version `REDACTED_TARGET_ADDRESS`, the direct
`portforward` command listened on `TCP *:<local-port>` even when the operator
used a `127.0.0.1` URL. That command is prohibited until a future version is
separately proven to bind only loopback.

Use Session Manager as an OpenSSH stdio proxy, and make OpenSSH own the explicit
loopback application forward:

```bash
ssh -N -i <private-key-path> \
  -o BatchMode=yes \
  -o StrictHostKeyChecking=accept-new \
  -o HostKeyAlias=xinsight-<target-name> \
  -o ExitOnForwardFailure=yes \
  -o ServerAliveInterval=30 \
  -o ServerAliveCountMax=3 \
  -o "ProxyCommand=<ali-instance-cli-path> ssh --instance <instance-id> --region <region-id> --port 22 --profile <profile>" \
  -L 127.0.0.1:18080:127.0.0.1:8080 \
  <ssh-user>@<instance-id>
```

Keep credentials in their approved local files/profile; never substitute key
or AccessKey contents into the command. Because `accept-new` records an unseen
host key without a prompt, obtain the named Target's SSH host-key fingerprint
through the authenticated Session Manager shell before first use, then compare
it with the newly recorded `HostKeyAlias` entry. Stop and investigate any
mismatch; never overwrite an existing changed key merely to reconnect.

Before issuing a diagnostic request, run:

```bash
lsof -nP -iTCP:18080 -sTCP:LISTEN
```

Proceed only when the sole result is owned by `ssh` and reads exactly
`TCP 127.0.0.1:18080 (LISTEN)`. Stop on `*:18080`, `0.0.0.0:18080`,
`[::]:18080`, an unexpected process or more than one listener. Only perform
bounded loopback diagnostics such as `curl --fail http://127.0.0.1:18080/readyz`
or verify the anonymous `/executions` redirect; do not log in through this HTTP
forward.

End the tunnel with `Ctrl-C` when the investigation is complete and repeat the
listener command. Its required final result is no listener. The verified
finding and correction are recorded in
[P2-4 Target access listener correction](../../evidence/prd2/p2-4-target-access-listener-correction-2026-07-14.md).

## Data Disk

Identify a candidate disk from cloud attachment metadata, size, and serial, then
reconcile it to the guest device. Do not trust an assumed device path.

Before formatting, use non-destructive inspection including:

- `lsblk` and filesystem fields;
- `blkid`;
- `wipefs -n`;
- `findmnt`;
- partition-table inspection;
- a device-content probe such as `file -s`.

Stop if any partition, filesystem, mount, prior signature, data, or device
identity is uncertain. Only a positively blank disk may be partitioned and
formatted.

Current layout:

```text
/data/x-insight/postgres
/data/x-insight/staging
/opt/x-insight/releases
/etc/x-insight
/var/backups/x-insight
/var/log/x-insight/deployments
```

The managed lifecycle writes upgrade and restore backups to
`/var/backups/x-insight`, as reported by `xinsightctl` host paths. Verify the
actual filesystem and free space for that directory separately; free space on
`/data/x-insight` does not prove backup headroom. Historical manually created
pre-adoption backups under `/data/x-insight/backups` may remain as retained
evidence, but that path is not the managed backup destination and must not be
silently moved or deleted.

Mount ext4 at `/data/x-insight` by filesystem UUID through `/etc/fstab` with
`defaults,nofail,noatime`. Directories must be least-privilege and not
world-writable. Secret files under `/etc/x-insight` are root-owned mode `0600`.

An unencrypted or delete-with-instance disk without an automated snapshot
policy may store repository fixtures and synthetic metadata only. Before any
non-fixture evidence, customer-like data, or production claim, require an
explicit decision covering:

- KMS-backed disk encryption or approved equivalent;
- delete-with-instance lifecycle;
- snapshot/backup location, cost, retention, and restore test;
- renewal and capacity ownership.

Capacity thresholds:

- warn at 70%;
- decide expansion by 80%;
- do not wait beyond 85%;
- expand in place after a backup/snapshot checkpoint;
- never use automatic deletion as capacity management.

## Service Identity And Secrets

Create a non-login `xinsight` user. Generate database credentials on the target
under a restrictive `umask` directly into root-owned files; do not print values
to stdout. Keep the four admin/reader/workspace/importer DSNs in the managed
root-only credential set under `/etc/x-insight/database`, with
`/etc/x-insight/database/current` selecting the active set. X-Insight systemd
units consume the minimum applicable DSN with `LoadCredential`; database DSNs
must not appear in `EnvironmentFile`, inline environment values, argv, or logs.
The root-only `postgres.env` is only the pinned Compose container's
target-specific initialization environment and is not loaded by the X-Insight
services.

Create separate PostgreSQL databases for runtime and target tests. Do not share
UniRouter's database, role, container, or volume. Do not enable remote
superuser access.

## PostgreSQL

Use a pinned PostgreSQL 16 image and record both:

- immutable registry `RepoDigest`;
- local image ID.

Persist the database under `/data/x-insight/postgres`, explicitly publish only
`127.0.0.1:5432:5432`, configure health and restart behavior, and verify the
effective listener after startup.

The signed payload already contains the supported systemd units, pinned Compose
file, and non-secret/placeholder environment templates. Install them only
through the verified managed controller. Target-specific instantiated
configuration and credential files are not delivery artifacts; return only
their sanitized shape and SHA-256 as Target evidence, and never substitute them
for the signed templates.

## Release Provenance

Each deployment task must name an exact CI-passing commit. Do not deploy a dirty
working tree or an unversioned directory.

The only supported first-customer delivery path is:

1. the exact current `main` commit passes a matching `CI` push run, including
   `Backend Tests`, `Signed V2 Release Qualification`, and
   `Fresh Install Qualification`; ordinary CI artifacts remain explicitly
   non-publishable;
2. after separate workflow-dispatch authority, the
   [Owner-Supervised Trial Release workflow](../../../.github/workflows/owner-supervised-trial-release.yml)
   is invoked with that exact commit, source CI run ID, and
   `OWNER_SUPERVISED_TRIAL` confirmation;
3. the workflow builds and verifies the strict seven-file signed delivery,
   removes the one-run private key before creating the delivery record,
   checksums, and upload set, reconfirms that the commit is still current
   `main`, and uploads the bounded delivery artifact;
4. the operator independently reconciles the workflow record, exact file set,
   and SHA-256 values before and after transfer through an approved private or
   control-plane path; and
5. install, adoption, or upgrade follows the release-matched
   [installation](../../manual/zh-CN/admin/02-install-and-first-start.md) and
   [upgrade/adoption](../../manual/zh-CN/admin/04-upgrade-migration-rollback-and-restore.md)
   procedures using only the verified payload and its target controller.

Repository clones, worktrees, `git archive` source packages, unversioned source
directories, and binaries built on the Target are unsupported deployment
inputs. The historical private-repository source-package fallback must not be
used.

Do not place GitHub PATs or deploy keys on the target without a separately
approved credential lifecycle.

### Cloud Assistant Chunked Transfer

Cloud Assistant `SendFile` accepts at most 32 KiB after Base64 encoding. This is
only a transport mechanism for an already approved delivery file; it does not
authorize source packaging, rebuilding, or changing delivery bytes. For a
larger delivery file:

1. split the exact file locally into zero-padded 8 KiB binary parts;
2. create a manifest containing each part name/SHA-256 and the final file
   name/SHA-256;
3. send each part separately with `SendFile`, `ContentType=Base64`, restrictive
   mode, and no overwrite unless retrying that exact failed part;
4. verify each send result and each target-side part hash;
5. concatenate zero-padded parts in deterministic lexical order;
6. verify the reconstructed file SHA-256, then verify the complete delivery's
   `SHA256SUMS` and Owner-Supervised record before any release extraction.

Do not inline the complete file into `CreateCommand`, open a temporary public
listener, or weaken the security group. Retain parts until target acceptance;
cleanup requires explicit single-path handling, not a bulk delete.

## Signed Delivery And Managed Release Layout

Do not install a Go toolchain or build X-Insight from source on the Target. The
Owner-Supervised workflow is the sole builder for this first-customer path. Its
delivery directory must contain exactly:

```text
xinsight-v<VERSION>-linux-amd64-<COMMIT>.tar.gz
xinsight-release-linux-amd64-<COMMIT>
build-result.json
trusted-release-keys.json
owner-supervised-trial-release.json
owner-supervised-trial.txt
SHA256SUMS
```

Any extra or missing entry fails closed. Independently verify the bootstrap
binary hash before executing it; then use it to verify the artifact and extract
to a new commit-named staging directory. `xinsightctl` is obtained only from
that verified payload. Do not copy runtime files from another archive or
workspace, modify an activated release, or manually switch
`/opt/x-insight/current`.

## Managed Install, Adoption, Upgrade, And Service

For a fresh host, run the verified target controller's `install --check` before
`install`. For the exact supported Phase 1 legacy source, complete
`adopt --check`/`adopt`, then `upgrade --check`/`upgrade`; for another allowlisted
managed source, run `upgrade --check` before `upgrade`. Use the commands and
preconditions in the release-matched manuals linked above.

These managed operations own preflight, backup, migrations, immutable release
layout, service files, release-pointer changes, readiness checks, and sanitized
receipts. Do not run `xinsight-server migrate` directly or hand-edit release
links to reproduce the old source-package procedure. Never run down migrations
automatically or drop a database to hide a failed migration.

## Target Acceptance

Required evidence:

1. exact commit, source CI and Owner-Supervised workflow run, strict delivery
   file set, and artifact/bootstrap/trust-store hashes;
2. final security-group membership and no public application/database ingress
   other than separately authorized TLS-terminator ports 80/443;
3. data-disk precheck, UUID mount, lifecycle/encryption/snapshot gaps, and use;
4. PostgreSQL image digest/ID, health, persistence path, and loopback listener;
5. the applicable `install --check`, `adopt --check`, or `upgrade --check`, plus
   the completed managed operation receipt and migration/readiness result;
6. successful required source CI jobs, kept distinct from Target evidence;
7. `healthz` and `readyz` HTTP 200 over loopback;
8. one approved synthetic fixture persistence, duplicate/no-op rerun, and local
   execution/events/gaps readback;
9. service restart preserving readiness and fixture readback;
10. listener proof showing the application and PostgreSQL only on
    `127.0.0.1:8080` and `127.0.0.1:5432`; when TLS exposure is separately
    authorized, only the terminator may additionally listen on approved ports
    80/443, with its forwarding/origin boundary verified;
11. explicit confirmation that no secret, raw payload, real Gate A artifact,
    customer data, external provider call, or unapproved Connector traffic was
    introduced.

Full sanitized logs remain root-owned under
`/var/log/x-insight/deployments/<UTC-work-id>/`. The operator report records
command, UTC timing, exit status, concise result, and log SHA-256. Full successful
logs do not belong in shared communication or Git.

## Rollback And Failure

- For an application failure, use the managed `rollback --check`/`rollback`
  path only when the signed manifest declares it compatible; otherwise follow
  the release-matched isolated-restore path. On a first deployment, stop and
  disable only X-Insight.
- Do not execute down migrations, drop databases, delete disks, or reformat
  storage as rollback.
- If network isolation fails, stop rather than relying only on host firewall.
- If a transfer part fails, retry only that exact part and verify its hash.
- Do not bulk-delete staging, evidence, or logs. Record exact paths and obtain a
  cleanup decision.

## Status Claims

| Claim | Minimum meaning |
| --- | --- |
| Local verified | repository checks passed outside the target |
| CI verified | exact commit passed required GitHub CI |
| Target verified | reconciled non-production deployment meets every applicable requirement in this runbook |
| Production verified | separately authorized customer-like/production evidence, including security, data, backup/restore, auth, and operational controls |

An external operator statement alone does not promote project status.
