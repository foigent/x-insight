# Release readiness

The source candidate is `0.2.0-dev.fpv19` with database schema 45. This page states acceptance boundaries; it is not a release announcement or a signed deployment receipt.

The [Foigent](https://github.com/foigent) product path leads through each repository's README, supported startup/installation guide, connection instructions, and contribution/security entry. The [UniRouter pairing record](getting-started.md#connect-unirouter-and-verify-the-pair) names the exact locally tested revisions, unchanged CI pins, comparison with the observed public UniRouter source, and the still-missing complete same-execution reproduction. UniRouter's published source or installer preparation does not make this X-Insight candidate public, signed, or jointly accepted.

The public candidate excludes 25 raw advisor/reference files whose originals and digests are retained privately. Historical synthesis and reference indexes identify that limitation instead of linking to unavailable source copies. This is a current-tree boundary: Git history and retained GitHub objects still require their own approved disclosure disposition before repository visibility changes.

Two historical provenance metadata files use explicitly labelled private-location placeholders while retaining their archive, member and schema hashes. The historical `scripts/xi-c-resource-guard.py` is retained privately and excluded from this candidate because it requires private A0 qualification state and manager allocation records. Historical notes that name it describe that private run, not a supported public command. Current product, build, CI and release guards remain in place; this exclusion does not change the private guard or authorize bypassing it.

| Surface | Required evidence before claiming completion |
| --- | --- |
| Source and licenses | Complete product source, authoritative LICENSE, actual redistributed dependency notices, provenance clearance for retained references |
| Local | Pinned type/unit/coverage/build checks, relevant PostgreSQL and ordinary-user browser paths, unchanged negative authorization behavior |
| CI | A successful exact-commit run, including required current-release lanes for release-relevant PRs; fork behavior must be observed on GitHub |
| Artifact | Exact clean commit, version/schema/manual agreement, complete notices, verified payload and signature under an approved authority |
| Installed host | Supported OS/topology, first install and accepted predecessor checks, non-mutating preflight, journal recovery and ordinary read chain |
| External integration | Exact source revision, authorized synthetic rejection/retry/cost path, matching request identity and source evidence read by an ordinary XI user |
| Public visibility | Review all reachable heads/tags, retained logs/artifacts and collaboration surfaces; resolve disclosure and rights findings before changing visibility |
| Production | Separately approved operational and security acceptance; none of the rows above alone establishes it |

Public dependency access uses exact pinned public repositories with read-only job credentials; ordinary PR validation must not require a maintainer GitHub App secret. A release-relevant fork PR must run the same current signed-artifact and fresh-install gates. Documentation-only changes keep the existing narrow route. Negative tests reject missing, skipped, canceled, and failed required lanes. `Backend Tests` remains the protected aggregate check; hosted protection settings are not changed by source files.

The manual workflow for an owner-supervised delivery has a separate authorization and trust boundary. CI ephemeral signatures do not authorize public distribution or customer self-upgrade. The historical rc13 profile and prior immutable release identities remain historical; use `--profile=development-d` for this candidate.

Public visibility, publishing releases, deleting artifacts, rewriting history, credential rotation, and changes to repository settings require their own approval. Finding a disclosure issue does not authorize deleting its evidence. Private audit details should stay outside the public repository.
