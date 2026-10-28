# Public release and acceptance status

As of **2026-10-28**, X-Insight source is public under Apache-2.0 and the **0.2.0-dev.fpv20 / schema 46** preview is published. This is a development release candidate, not stable or Production acceptance.

Start with [install → choose your password → sign in → connect UR → confirm access and the first fact](getting-started.md). Source-building and historical qualification recipes are a separate [contributor entry](development.md).

| Surface | Verified state | Boundary |
| --- | --- | --- |
| Public source | Canonical `foigent/x-insight` is public; Apache-2.0 covers project-owned product source | Dependency licenses, authentication, grants and tenant boundaries still apply |
| Published preview | [FPV20 release](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv20), seven public attachments, source `08435e0949dfae0ae65497d2e12ca356f6d820f0` | Prerelease; no stable or unattended-install claim |
| Exact artifact | Owner signature and Linux artifact qualification accepted | Temporary CI signatures are separate; the published signed bytes remain unchanged |
| Specific existing host | Ordinary empty-database FPV20 install on Ubuntu 22.04 amd64 with pinned container PG16; HTTPS, owner current session and protected integration reads accepted | One selected host/topology, not every supported host or Production; private host receipts are retained separately |
| Ordinary installed UR→XI connection | Accepted on 2026-10-27: connection, scoped source registration and eight active source-fact profiles; bounded empty sync completed with all units done | Empty synchronization imported no facts; connection acceptance does not establish first-fact or ordinary-reader acceptance |
| First real fact and ordinary-reader access | Pending: a matching real completed source request and its authorized XI reading, including the applicable scope checks | Empty pages, checkpoint commits and an active connection do not complete this row |
| Stable / Production | Not claimed | Requires separate release and operational acceptance |

The exact package, bootstrap, public trust, hashes and installation correction are in the [FPV20 record](releases/v0.2.0-dev.fpv20.md). The original publication pin is [authority commit `aacc40ef50ffa60f31ea924b17c257526f316ddd`](https://github.com/foigent/x-insight/blob/aacc40ef50ffa60f31ea924b17c257526f316ddd/docs/community/release-authority.json). Later documentation status corrections do not replace that pin or re-sign the package. The original release attachments record the publication-day qualification boundary; use this dated status for the later accepted host result.

## What remains after connection acceptance

For a new connection, use the normal [two-administrator route](getting-started.md#connect-unirouter-and-verify-the-pair) with current sessions and the intended source/project configuration. Preserve an existing accepted connection rather than creating another invitation to obtain data. The remaining acceptance requires one authorized real completed source request, matching facts in XI, and an ordinary user with the exact reading scope. Keep missing facts and denied scopes explicit. Current protocol pins and historical P3 results remain qualification evidence, not a substitute for these remaining installed-pair checks.

## Why the repository retains technical material

- Product source, migrations, deployment templates, access controls and same-version manuals implement the runnable product.
- Shared contracts and pinned public dependencies support the actual adapters and CI compatibility checks. Users do not install Integration Control or protocol repositories as additional products.
- [Compatibility sources](../../compatibility/README.md), manifests, license texts and notices are needed for predecessor/recovery reproduction and attribution. They are not first-install prerequisites.
- Historical planning, reviews and qualification records explain earlier decisions and exact results. Their dated pending statements are historical; this page gives the current delivery status.

No evidence, compatibility input or required public dependency is removed by this entry-point correction.

## Retained publication and build evidence

The public candidate excludes 25 raw advisor/reference files whose originals and digests are retained privately. Historical synthesis and reference indexes identify that limitation instead of linking to unavailable source copies. The separately retained original history remains private. The initial canonical refs and retained GitHub surfaces were reviewed for publication, and three identified old mixed binary artifacts were removed remotely with complete private originals preserved. Those publication/disposition actions are complete; future source or retained-surface changes still need their applicable review.

Two historical provenance metadata files use explicitly labelled private-location placeholders while retaining their archive, member and schema hashes. The historical `scripts/xi-c-resource-guard.py` is retained privately and excluded from this candidate because it requires private A0 qualification state and manager allocation records. Historical notes that name it describe that private run, not a supported public command. Current product, build, CI and release guards remain in place; this exclusion does not change the private guard or authorize bypassing it.

At that exact source on 2026-10-18, a complete anonymous archive matched all
4,375 file contents/Git modes and the accepted tree. A local M4 cold-cache run
with Go 1.25.12, Node 22.23.1 and npm 10.9.8 passed the ordinary seven-command
Linux cross-compile, unchanged notices/runtime guards, frontend type/coverage
and build checks (579 unit tests), and the four-project synthetic browser suite
(57 passed, three existing representative-layout skips). Generated UI/manual
bytes and all source/lock files remained unchanged. The source archive has no
Git history, the command binaries are not formal release packages, and this
local result does not replace hosted fork-event, PostgreSQL, ordinary installed
Linux or installed UR/XI evidence. See the [cold-build scope](development.md).

## Maintainer boundaries

Public dependency access uses exact pinned public repositories with read-only job credentials. Release-relevant PRs retain current signed-artifact/fresh-install gates and negative authorization checks. Documentation routing is determined by the existing CI classifier; this document does not change it. `Backend Tests` remains the protected aggregate check.

The [signed-distribution guide](signed-distribution.md) separates consumption of the existing preview from preparation of a future release. The current public authority, assets and the accepted specific-host result are no longer pending choices. Future signing, publication, visibility, settings, credential or artifact-disposition changes still require their applicable authorization. The frozen rc13 and historical release identities keep their original scope.

Private build paths, credentials, raw customer content and internal operations evidence do not belong in the public repository.
