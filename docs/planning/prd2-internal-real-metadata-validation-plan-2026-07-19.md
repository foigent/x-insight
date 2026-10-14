# PRD2 Internal-Real UniRouter Metadata Validation Plan

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: X-Insight receiver preparation is merged; the User-authorized narrow
raw-IP policy adaptation is merged in UniRouter and the matching receiver
compatibility is implemented in this X-Insight revision; B2a source readiness
and B2b bounded inventory remain separate dependency-gated work; no internal-
real artifact has been generated, transferred or imported

Prepared: 2026-07-19

Decision owner and data custodian: User/product owner

Execution owner: Codex

X-Insight receiver-preparation main:
`cd537c246d75cc149b8064b24dcefb4521cabdb3` (PR #63, successful PR CI
`29684113519`)

UniRouter adaptation main: `d43fef6eed92323da0880f0f6e0e8d35e388e8e0`

## Purpose And Accepted Direction

The User accepted continuing beyond the completed `UR-E` synthetic proof with
one bounded batch of X-Insight/UniRouter development data. This batch answers a
different question:

> Can the existing X-Insight product investigate actual, non-customer
> UniRouter development executions without importing raw content or weakening
> the accepted sidecar, isolation and custody boundaries?

This is not customer data, a live feed, a Production claim or permission to
query/export a broad UniRouter database. It adds one pre-field proof class:

| Data class | Meaning | Current state |
| --- | --- | --- |
| Synthetic fixture/UR-E | Fabricated rows designed to prove mechanics | Complete for its exact scenario |
| Internal-real development metadata | Actual execution behavior created in a non-production UniRouter environment by the project team; no customer facts | Receiver preparation and narrow producer/receiver IP-policy compatibility implemented; B2a readiness and B2b bounded inventory not run; no artifact/import |
| Customer sanitized metadata | Customer operational metadata under customer custody | Not authorized and not part of this batch |

## 2026-07-20 Policy-Adaptation Checkpoint

The User authorized one narrow source-policy adaptation through decision
`UR-XI-IR-AUTH-20260720-02`, SHA-256
`34c5081403697666d14f05d04ee2e98aa74cbfda872961de48ca0099a4870e9f`.
The owner-authored request is SHA-256
`7517496d8a40aa824f2c96391c8363e6eaf8533a11652cecbfaa57e92d467d22`.
UniRouter implemented the producer side through PR #225, tested head
`c3930e3086299bc0ea3c0727b81262de85c501b1`, required CI run
`29711162577` attempt 2 and merged main `d43fef6e...`; its immutable response is
SHA-256 `e4524057a0fd5591054552d5d55a0c782b35ee9131c0968fb013f9335eac8205`.

This checkpoint permits only the designated
`public.call_logs.ip_address` column in one exact project-owned,
non-customer, non-Production current-schema source to be evaluated inside the
database for presence. The raw string is not selected into or returned to the
producer. Artifact and X-Insight state remain limited to
`ip_address_was_present` and `ip_address_redacted`.

The cross-project request remains `Not binding`. B2a may later prove readiness
without reading a source row or value. B2b remains blocked until all named
dependencies are complete and may then perform only one bounded aggregate
inventory. B3, artifact generation/transfer/import, Target, Production and
live feed remain unauthorized.

## Facts Found In Current Code

The current UniRouter producer can select explicit request IDs through
`unirouter.prd5.xinsight.approval.v1` and emit
`unirouter.prd5.xinsight.snapshot.v2`. Its merged Linux-first strict path can
evaluate presence of the one designated source IP column without returning
the raw value, and it emits only the two approved boolean proofs. It does not
export raw prompts, responses, API-key plaintext, raw IP addresses or raw
upstream error text.

It also has limits that matter before real development data is used:

- the producer has no native 20-request hard cap;
- `request_id` is not bound to `org_id` in the approval manifest;
- call-log and trace queries are not executed in one repeatable-read
  transaction; and
- several opaque/reference/free-text-capable values are exported unchanged,
  including `actor_ref`, `on_behalf_of`, `decision_reason`, `evidence_ref`,
  `payload_ref`, session/upstream IDs and sanitized metadata values.

The current X-Insight importer rejects declared raw-content flags and unsafe
key names, but that guard is not a semantic DLP system. Before this change it
did not enforce the 20-request batch or inspect values for obvious email,
phone, IP, URL, credential or unapproved free-text patterns.

These are why the completed synthetic proof cannot simply be relabelled as a
real-data proof.

## Frozen Batch Boundary

### Included

- exactly one snapshot-v2 artifact selected by exactly one approval manifest;
- `1..20` explicit request IDs from one non-production, single-org UniRouter
  development/test source;
- at most 200 total call-log, trace-event and capture-gap source rows;
- existing completed executions created by the project team, with success,
  usage/cost, denial, provider failure/timeout, streaming, retry/fallback and
  capture-gap cases included only where they actually exist;
- one X-Insight in-memory import check, one first persistence and one identical-
  byte replay;
- execution search, detail, timeline, selected-node, source/provenance and
  capture-gap inspection through the real embedded UI; and
- exact manifest/artifact hashes, counts, receipts and sanitized result
  evidence.

The batch does not authorize paid/provider calls solely to manufacture missing
scenarios. A scenario absent from the source remains `not observed`.

### Excluded

- customer or field-site data;
- raw prompt, response, message, header, body, tool argument or attachment;
- API-key plaintext, authorization material, DSN, cookie, secret or token;
- direct personal identifiers, raw IP or raw upstream error text;
- broad source discovery, more than 20 request IDs, a second artifact or
  historical auto-backfill;
- live feed, exporter/outbox, endpoint, service credential, automatic receipt
  transport or freshness SLA;
- writes to the existing fixture/synthetic-only named Target;
- raw-content observability, semantic evaluation, X-IAM, Tool Gateway,
  multi-tenant SaaS or Production.

## Data And Value Policy

Numeric org/project/API-key/owner IDs may be retained only as opaque
operational references; API-key plaintext never enters the artifact. Request,
trace, span, session and upstream IDs must be opaque and must not encode an
email, phone number, IP address, URL or credential.

For this batch:

- `actor_ref`, `on_behalf_of`, `decision_reason` and `evidence_ref` must be
  empty or bounded code-like/opaque values;
- `payload_ref` must be empty;
- payload capture mode is limited to `none`, `metadata_only` or `hash_only`;
- trace metadata values must remain bounded scalars and code-like; unexpected
  free text is blocked even when its key was allowlisted by UniRouter;
- source rows must resolve to one org, every row must belong to the approved
  request set and more than one call log for one request blocks intake; and
- counts, scenarios, approval-manifest hash and each source ref's exact
  `source_instance/table/row-id` identity must reconcile; every approved
  request must have at least one positive-org source row and all rows must
  resolve to the same org; and
- `source_event_hash` must be lowercase 64-hex but remains a producer-declared
  field under producer custody. X-Insight does not claim to recompute a
  database-provided source-event hash from the sidecar artifact.

The value-aware check is a fail-closed preflight and an additional operator
guard, not a claim of general DLP completeness.

For the designated source column, the producer-visible and receiver-visible
matrix is exact:

| `ip_address_was_present` | `ip_address_redacted` | Strict result |
| ---: | ---: | --- |
| `false` | `false` | Allowed |
| `true` | `true` | Allowed; the raw value remains behind the database query-result boundary |
| `true` | `false` | Blocked: redaction is not proved |
| `false` | `true` | Blocked: the proof is internally inconsistent |

Every new strict internal-real snapshot must use exactly:

```text
raw IP addresses are not exported; only ip_address_was_present and ip_address_redacted boolean fields are retained
```

The historical single-boolean declaration remains accepted only when every
call-log row is `false/false`; it is rejected for `true/true` and either
mismatch. Raw `ip_address` and unapproved aliases remain unknown fields. No
schema, migration or new artifact field is introduced.

This proves only query-result/artifact non-disclosure on the approved path. It
does not prove the source IP was deleted or modified, does not prove the
database principal lacks capability to select the base column, and the two
booleans are not general DLP proof.

## X-Insight Receiver Preparation

The new non-persisting command is:

```text
xinsight-importer unirouter preflight-sanitized \
  --snapshot /exact/private/path/snapshot.json \
  --approval-manifest /exact/private/path/approval-manifest.json \
  --max-request-ids 20 \
  --max-events 200
```

Its JSON report emits computed artifact identity, bounded counts and fixed
finding paths/codes; it does not echo metadata keys or untrusted artifact field
values and does not persist data. Filesystem failures may include the operator-
supplied exact input path on stderr, which remains private execution output and
is never copied to repository/shared evidence. It computes the exact manifest
SHA-256 and requires the snapshot-declared hash,
source-instance, approved request set and scenario mapping to match that file.
Every manifest row must be `approved`, `required=true` and notes-free for this
batch. Both inputs reject symlink/non-regular or multi-link identities and must
be current-owner, group/other-inaccessible files at absolute clean paths with
mode `0400` or `0600`. Each read uses a no-follow descriptor and reconciles
pre-open/opened/post-read path identity, size, mode, owner, link count,
modification time and change time. A non-`pass` result stops the batch.
The later `import-snapshot` and `gate-a-check` paths accept the same optional
manifest and, when supplied, compare the actual import bytes to the preflight
artifact SHA in-process. This manifest-bound mode is mandatory for this batch;
the pre-existing manifest-free compatibility path is unchanged but is
ineligible for any internal-real acceptance claim. `operator_approved`
database provenance is also contingent on the manifest being supplied and
passing that preflight; snapshot self-declaration alone is insufficient.

The existing Alibaba Cloud X-Insight Target remains ineligible because its
documented storage boundary is unencrypted, delete-with-instance and lacks an
approved snapshot policy. The first internal-real proof therefore uses a
dedicated local PostgreSQL 16 trial database on this operator Mac. FileVault
was read-only verified `On` on 2026-07-19, and Homebrew PostgreSQL 16.14 is
installed. The trial database, roles, DSN files, artifact and backup will use
private paths outside Git and the shared cooperation folder. No trial database
or artifact is created before the later exact B3 activation gate.

The product server points to one database at a time. The internal-real UI proof
will use a maintenance-window switch to the trial database and then return to
the prior fixture database; running a second server is rejected as needless
first-batch complexity.

## Remaining UniRouter Cooperation

The producer policy adaptation is complete. The remaining source-owned work is
split so that readiness cannot consume the one inventory attempt:

1. **B2a source readiness:** bind the exact non-Production/non-customer source,
   custodian, proof that normal boot cannot generate product/provider/request
   traffic, least-privilege reader, reachability, reader expiry and a maximum
   two-hour access window without reading a source row or value.
2. **B2b bounded inventory:** only after every named dependency passes, run one
   existing-data-only `READ ONLY`, `REPEATABLE READ` aggregate inventory for
   `1..20` requests and at most 200 total rows. If existing data is
   insufficient, return a blocker; do not manufacture three to five calls.

On completion or failure, the dedicated reader must expire or be revoked. The
exact instance may be restored to its original stopped state only after the
source owner proves no other authorized workload became active; otherwise the
owner must not stop or interfere with it and must return an exact blocker.

That bounded inventory must:

1. identify one non-production, project-owned, non-customer source and its
   exact current producer/main/CI identities;
2. confirm whether source writes can be quiesced or a consistent read-only
   source can be used, otherwise propose the smallest producer correction;
3. inventory candidate completed requests without exporting broad rows;
4. prove the selected request IDs belong to one org, do not collide and have
   no duplicate call logs;
5. list actual available scenario labels and source-row counts without
   claiming unavailable scenarios;
6. inventory every non-empty reference/free-text-capable field and metadata key
   by path/class, without copying values to the shared folder;
7. confirm `payload_ref` is empty and all remaining identity/reference values
   are opaque and project-owned; and
8. state the exact private generation/handoff/custody path, without generating
   or transferring bytes until a later exact activation decision is binding.

No additional UniRouter code or Target change is authorized by the X-Insight
compatibility work item. B2a, B2b and B3 remain independently gated.

## Acceptance Matrix

| Gate | Required result | Executable evidence |
| --- | --- | --- |
| IR-0 Authorization | Exact internal, non-customer source, field policy, custodian, purpose, retention and stop rules accepted | X-Insight plan, UniRouter response, binding limited decision |
| IR-1 Source selection | `1..20` terminal request IDs, one org, no collision/duplicate call log, actual scenarios only | Approval manifest, bounded source inventory and producer identity |
| IR-2 Artifact preflight | V2, exact manifest bytes/hash/set, at most 20 IDs/200 rows, exact source refs and per-request org-bound source-row coverage, artifact hash, producer-declared source-event-hash format, no blocked field/value | `preflight-sanitized` report plus private owner review |
| IR-3 Receiver isolation | FileVault-on local host, dedicated PostgreSQL 16 trial database/roles/DSN files, backup and restore proof | Database identity, migration ledger, permissions, backup/restore receipt |
| IR-4 First write/replay | First write exactly matches import plan; identical bytes replay as duplicate/no-op with no mutation | Import receipts, database fingerprints and readback |
| IR-5 API/UI | Every approved execution is searchable and its timeline/source/provenance/gap agrees with the artifact | Bounded API evidence and real embedded-UI walkthrough |
| IR-6 Retention/exit | Retain through validation closure plus seven calendar days, then explicit handoff or controlled retirement | Custody inventory and later append-only cleanup/handoff evidence |

## Severity And Stop Rules

### P0

- raw content, credential, secret, customer data or unapproved direct
  identifier enters the artifact, database, report, Git or shared folder;
- storage is not verified encrypted/private or the trial data is mixed into
  fixture/synthetic evidence;
- source/org/request identity is ambiguous or the artifact is not bound to the
  approved manifest; or
- any result is promoted to customer, Production, live-feed or whole-
  integration verification.

### P1

- request/event bounds, count/hash/replay invariants or dedicated database
  isolation fail;
- source can change across the producer's two reads without an accepted
  consistency mitigation;
- an exported value is free text or merely assumed opaque; or
- API/UI disagrees with source artifact/receipt facts.

### P2

- an optional scenario is unavailable;
- investigation copy/ergonomics needs refinement without changing evidence
  truth; or
- performance beyond this small batch remains unmeasured.

Any P0/P1 stops before persistence or status promotion. The execution owner's
tests are self-verification, not independent acceptance.

## Implementation Order

1. **IR-A — X-Insight preparation (complete):** value-aware bounded preflight,
   tests, plan/manual and receiver runbook merged through PR #63 / CI
   `29684113519` / main `cd537c2...`.
2. **IR-B1 — producer correction and IP policy adaptation (complete):** the
   Linux-first UniRouter producer correction and narrow two-boolean adaptation
   are merged; no source was read and no artifact was generated.
3. **IR-X — X-Insight compatibility (this revision):** enforce the exact new
   declaration and state matrix while retaining historical `false/false`
   compatibility; no source, artifact or persistence action occurs.
4. **IR-B2a/B2b — readiness and inventory (dependency-gated):** readiness reads
   no source row/value; inventory is one later bounded aggregate transaction.
5. **IR-C — activation:** accept one exact manifest/artifact task through the
   shared decision rule.
6. **IR-D — execution:** private generation/transfer, local trial database,
   first-write/replay and API/real-UI proof.
7. **IR-E — closeout:** exact claim, retained hashes/receipts and explicit
   seven-day handoff/retirement decision.

The maximum successful claim is:

> Internal-real External integration verified — execution-owner verified,
> exact approved non-customer metadata sidecar batch only.

It is not independent acceptance, customer field validation or Production.

## Related Records

- [Phase 2 Batch 0 Scope And Decision Register](phase-2-batch-0-scope-and-decision-register-2026-07-13.md)
- [PRD2-M1 UniRouter Execution Investigation Field Proof](prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md)
- [PRD2 UR-E Bounded Synthetic Sidecar Execution Plan](prd2-ur-e-bounded-synthetic-sidecar-execution-plan-2026-07-19.md)
- [Configuration, Security And Access Manual](../manual/zh-CN/admin/03-configuration-security-and-access.md)
- [Execution Investigation Workflow](../manual/zh-CN/operator/06-execution-investigation-workflow.md)
- [Internal-Real Local Trial Runbook](../ops/prd2-internal-real-local-trial-runbook-2026-07-19.md)
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/requests/2026-07-19-x-insight-prd2-internal-real-metadata-inventory-request.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-20-prd2-internal-real-raw-ip-policy-adaptation-b2-retry-authorization.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/requests/2026-07-20-x-insight-prd2-internal-real-raw-ip-policy-adaptation-b2-retry-request.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/responses/2026-07-20-unirouter-prd2-internal-real-raw-ip-policy-adaptation-implementation-response.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-11-prd5-runtime-contract-v1-boundary.md`
