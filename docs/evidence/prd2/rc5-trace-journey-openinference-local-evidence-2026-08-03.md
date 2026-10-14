# rc.5 Trace Journey and bounded OpenInference local evidence

Date: 2026-08-03

## Product problem and invariant

The frozen OpenInference v2.0.2 projection correctly preserves one independent M0 execution per
LLM Span. Replacing those identities with one trace-wide `request_id` would change existing Event
IDs, hashes, idempotency and B9 compatibility. The missing product capability was therefore a
source-neutral read model that can show a declared path across those existing executions without
rewriting Evidence.

The implementation adds `xinsight.trace-journey.v1` and keeps these invariants:

- the anchor execution only authorizes the exact source-system, organization, project and trace
  scope visible at a fixed `as_of`;
- a causal edge exists only when one visible child `parent_span_id` resolves to one unique visible
  parent Span; time order, shared trace/session or model similarity never creates an edge;
- existing execution IDs, request IDs, Event IDs/hashes, source refs, Gap rows and Provenance are
  read but never rewritten;
- ambiguous, unresolved, cyclic or truncated parent relations remain explicit integrity failures;
- source timeline completeness remains false for the bounded export.

## Bounded non-production import boundary

General OpenInference activation and Production bootstrap remain fail closed. One metadata-only
offline operation requires an exact authorization with all of the following bound before bundle
credentials or PostgreSQL are accessed:

- one regular root-owned `0400`/`0600` authorization file, no symlink and one hard link;
- canonical UTC validity window no longer than two hours;
- exact non-production target reference and normalized machine-ID SHA-256;
- exact X-Insight version and commit;
- exact absolute bundle path/SHA-256, operation ID, bundle ID, producer instance, tenant/org/project
  scope and at most ten records;
- explicit `metadata_only`, no raw payload, no listener, no Feed activation and no
  Cursor/Checkpoint;
- duplicate-key rejection at every JSON depth and rejection of known prompt/response OpenInference
  attributes before credentials or database access.

The existing durable operation binding and operational receipt remain the replay/conflict
authority. Authorization and mutable delivery state do not enter M0 identity/hash or event-linked
Provenance.

## Local implementation verification

Verified on the clean rc.5 worktree based on exact `origin/main`
`0e1a3a45726d6b7332e9d959a5abdf0fbfb476b8` / tree
`0ccdb07d7b83a37c1e4d43f9b23cac5a794918e4`:

- `go test -p 1 ./...`: PASS; the existing host lifecycle package completed in `172.383s`;
- targeted race tests for DB, API, bounded OpenInference authorization/import and Investigation:
  PASS after running outside the filesystem/network sandbox so `httptest` could bind loopback;
- `go vet ./...`, Go formatting and `git diff --check`: PASS;
- fixture conformance: PASS (`11/11`);
- CI router/contract/release-identity self-tests: PASS (`11`, `8`, `8` cases); release shell syntax:
  PASS;
- Phase 2C Reference and LiteLLM gates: PASS after advancing only reviewed protected hashes and
  retaining the frozen migration tree and existing Connector/Event/M0 surfaces;
- TypeScript check, 36-path OpenAPI validation and component tests: PASS (`24/24`);
- Playwright desktop, laptop and narrow suites: PASS (`15/15`);
- the three-span synthetic metadata-only bundle projects twice to the same 15 identities/hashes:
  three main Events plus twelve first-class Gap Events;
- frontend dependency audit reported zero vulnerabilities and the locked-license check reported
  177 entries; dependency files were not changed by this slice.

The release-matched manual was built twice byte-identically. Its HTML SHA-256 is
`17f6911b8ce545c2d19e0290b402b9d80de87b5723f44ac07c2df75d059bba2f` and embedded source digest is
`sha256:9687f6439d73d413bd643a65fbd63c38bb9ee44b51e46546cf90d4282b7a71fc`.
The production frontend build was also reproduced twice with exact referenced artifacts:

- `index.html`: `03ba414a41f8ebd3cf24cead97b20a383168dd4503d8287b65290ba98af8eb8b`;
- CSS: `092e4f98cb1e91d2514b42bbc2f6e0d547b2270bfd89372bb85da1cf9caec5f1`;
- JavaScript: `3d0d17eb31f34d23d9cf97e09f02c90bc4fe4acbd7f6ed5a1639eaaff7baf157`.

## Browser product acceptance

A local metadata-only API fixture was rendered through the built production assets in the selected
browser, not inferred from source alone:

- desktop `1440x1000`: three cross-execution cards, two declared-parent arrows, zero inferred-break
  markers and no horizontal overflow;
- narrow `390x844`: the same cards stack vertically, the two arrows become vertical and document
  width remains equal to client width;
- the summary visibly answers “谁发起 / 做了什么 / 经过系统 / 到哪里结束” and shows the
  `受限离线导入` assurance label;
- clicking the `policy-router` node navigated to the exact second execution and selected Evidence
  deep link while retaining the fixed `as_of`.

This is local UI proof over a synthetic fixture. It is not named-target, customer-network or
Production proof.

## Remaining evidence boundary

The local environment did not provide the CI PostgreSQL service, so the new PostgreSQL integration
test is expected to execute in exact-head PR CI. This candidate has not yet been pushed, reviewed by
GitHub CI, merged, signed, published or deployed. No target database, credentials, Feed,
Cursor/Checkpoint, listener or traffic was changed. The separately authorized named non-production
study remains gated on exact PR/main CI and the owner-supervised signed rc.5 artifact; both Feed
services must remain inactive/disabled throughout it.
