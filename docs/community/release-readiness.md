# Public release and acceptance status

As of **2026-11-14**, X-Insight source is public under Apache-2.0. [FPV24 / schema 47](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv24) is published and owner-signed. Evidence below binds qualified artifacts, host results and integration checks to their respective versions.

Start with [install → choose your password → sign in → connect UR → confirm access and the first fact](getting-started.md). Source-building and historical qualification recipes are a separate [contributor entry](development.md).

| Surface | Verified state | Boundary |
| --- | --- | --- |
| Public source | Canonical `foigent/x-insight` is public; Apache-2.0 covers project-owned product source | Dependency licenses, authentication, grants and tenant boundaries still apply |
| Signed FPV25 candidate | [FPV25 release record](releases/v0.2.0-dev.fpv25.md), product source `b77fa1d7cff40a9adafe495d1c4951632819a4d3`. [Exact main CI](https://github.com/foigent/x-insight/actions/runs/43592588455), [protocol](https://github.com/foigent/x-insight/actions/runs/43592588498), ten Linux scenarios, owner signature and reconstruction from the exact FPV24 package passed | See the [canonical publication record](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv25) for availability and the immutable authority reference. Original-host and installed-pair acceptance have separate evidence |
| Signed FPV24 | [FPV24 release record](releases/v0.2.0-dev.fpv24.md), product source `fa1e64e1d1259c3b7d631baa7794952b0ae72d93`. Exact main CI/protocol, nine Linux lanes, owner signature and byte-identical reconstruction verified | Consumer trust is pinned to the [immutable authority record](https://github.com/foigent/x-insight/blob/9c83fba272b165edcf88134eece5a533f5ab389f/docs/community/release-authority.json) |
| FPV24 installed host | Native preserving FPV23→FPV24 upgrade with backup and isolated restore on Ubuntu 22.04 amd64 with pinned container PostgreSQL 16 | Original host and topology. Earlier native empty-database installation was FPV20 |
| UR 1.15.6 → XI FPV24 | Original connection recovered and completed a bounded sync. Owner and ordinary reader read the granted project in Runtime. Automatic credential rotation and communication after rotation passed | That sync returned zero new items. Real first-fact arrival was separately observed on the earlier preview |
| FPV23 predecessor | [FPV23 preview](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv23), source `82ca877d023058e48c93975c50171f606f8924f2`; signed archive is the verified FPV24 delta basis. The preserving FPV22→FPV23 upgrade passed with private backup and isolated restore on schema 47 | The [FPV23 publication authority](https://github.com/foigent/x-insight/blob/19bf1314fc296684e765991f23d8d38d24a44bad/docs/community/release-authority.json) remains pinned |
| FPV22 predecessor | [FPV22 preview](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv22), source `bd4503a13a46156f8361e56730abef96cc5628bd`; published 2026-11-05; exact signed archive is the verified FPV23 delta basis. One preserving FPV21→FPV22 original-host upgrade passed with private backup and isolated restore on schema 47 | The [FPV22 publication authority](https://github.com/foigent/x-insight/blob/a4768ab62d62769926cc45a8110c95e5b47fdf03/docs/community/release-authority.json) remains pinned |
| Historical preview | [FPV21 release](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv21), seven public attachments, source `6463d980958fae86d975eecde239b9534ec831a5` | Prerelease; no stable or unattended-install claim |
| FPV21 exact artifact | Actual owner signature, Linux artifact qualification, and original-host anonymous download/signature verification accepted | Temporary CI signatures are separate; the published signed bytes remain unchanged |
| FPV21 existing-host result | Native preserving FPV20/schema46→FPV21/schema47 upgrade on Ubuntu 22.04 amd64 with pinned container PG16; private backup and actual isolated restore; four managed services ready | Original identities, accounts, passwords, facts, grants, cursors and trust retained. One selected host/topology, not every supported host or Production; private receipts stay separate |
| Installed UR→XI connection | Connection, scoped source registration and eight-profile empty sync accepted on 2026-10-27; later bounded sync ingested metadata from one authorized real completed request | The later native run remained partial because of control-step failures. Metadata ingestion and checkpoints do not establish a fully successful control run or a separate feed acknowledgement |
| Owner project reading | The matching real request's metadata was read through an explicit owner source/project grant; an ungranted project was denied | Management is not an automatic reading grant. Generic body-unavailable UI is not proof of a permission denial |
| Ordinary-reader access | Native holder recovery and actual reader login verified; the exact real request was read in the granted project, and an ungranted project was denied | The reader result has its own identity and evidence. Overview behavior and account identification remain unaccepted |
| Content boundary | Existing owner/reader grants do not allow body access. The reader content list returned 404; that frozen Runtime reading has no content authorization | This is not a 403 result. Byte-fetch denial against an actual captured body was not independently verified; the complete content-refusal acceptance remains open |
| Historical FPV21 UI findings | The overview repeatedly rechecks the owner session after some legitimate 403s; an ordinary reader can instead reach a full-page denial. The account flow and current-account display are unclear | Runtime access does not establish overview or full ordinary-user acceptance; FPV23 has separate source and CI evidence for scoped navigation and denial handling |
| Stable / Production | Not claimed | Requires separate release and operational acceptance |

The exact FPV21 package, verifier, public trust and hashes are in the [FPV21 record](releases/v0.2.0-dev.fpv21.md). Pin [authority commit `e61a59fae14d1cfe323fa379329a80db25645de4`](https://github.com/foigent/x-insight/blob/e61a59fae14d1cfe323fa379329a80db25645de4/docs/community/release-authority.json); it is separate from the product source commit. Later status corrections do not replace that pin or re-sign the package. Release attachments retain their publication-day qualification boundary. The [FPV20 record and installation erratum](releases/v0.2.0-dev.fpv20.md) and its original authority remain historical evidence.

## What remains after connection acceptance

For a new connection, use the normal [two-administrator route](getting-started.md#connect-unirouter-and-verify-the-pair) with current sessions and the intended source/project configuration. Preserve an existing accepted connection rather than creating another invitation to obtain data. The existing pair has matching completed-request metadata and an authorized owner reading. Native holder recovery, true reader login, exact project reading and foreign-project denial are now verified independently. Complete account-identification and legitimate-denial UI fixes and the remaining body-refusal boundary; keep the partial control-step result and unverified acknowledgement explicit. Current protocol pins and historical P3 results do not substitute for these installed-pair checks.

## Why the repository retains technical material

- Product source, migrations, deployment templates, access controls and same-version manuals implement the runnable product.
- Shared contracts and pinned public dependencies support the actual adapters and CI compatibility checks. Users do not install Integration Control or protocol repositories as additional products.
- [Compatibility sources](../../compatibility/README.md), manifests, license texts and notices are needed for predecessor/recovery reproduction and attribution. They are not first-install prerequisites.
- Historical planning, reviews and qualification records explain earlier decisions and exact results. Their dated pending statements are historical; this page gives the current delivery status.

No evidence, compatibility input or required public dependency is removed by this entry-point correction.

## Retained publication and build evidence

The public candidate excludes 25 raw advisor/reference files whose originals and digests are retained privately. Historical synthesis and reference indexes identify that limitation instead of linking to unavailable source copies. The separately retained original history remains private. The initial canonical refs and retained GitHub surfaces were reviewed for publication, and three identified old mixed binary artifacts were removed remotely with complete private originals preserved. Those publication/disposition actions are complete; future source or retained-surface changes still need their applicable review.

Two historical provenance metadata files use explicitly labelled private-location placeholders while retaining their archive, member and schema hashes. The historical `scripts/xi-c-resource-guard.py` is retained privately and excluded from this candidate because it requires private A0 qualification state and manager allocation records. Historical notes that name it describe that private run, not a supported public command. Current product, build, CI and release guards remain in place; this exclusion does not change the private guard or authorize bypassing it.

For the separately retained 2026-10-18 source qualification, a complete anonymous archive matched all
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

The [signed-distribution guide](signed-distribution.md) covers the current FPV24 preview and retains the maintainer procedure separately. The frozen rc13 and earlier preview records retain their original scope.

Private build paths, credentials, raw customer content and internal operations evidence do not belong in the public repository.
