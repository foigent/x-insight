# PRD2 P2-4 Owner-Supervised Trial Main Delivery Evidence

Status: exact-main CI and owner-supervised GitHub delivery verified; named
Target, supported-host crash sample, manual-guided User tasks and live UniRouter
integration remain separate gates
Date: 2026-07-14
Execution owner: Codex
PR: [#37](https://github.com/foigent/x-insight/pull/37)
Merged main: `3884f6d7c9a8de04bb558c41975f44f3c511c671`

## Scope And Claim Boundary

This append-only record closes the exact-main packaging gate accepted in
B0-D32. It records GitHub PR/main job routing, exact release qualification, the
manually triggered first-customer delivery and a second download/hash
inspection by the execution owner.

It does not claim a customer-host installation, `Target verified`, External
integration, Production, independent acceptance, a durable publisher identity
or customer self-upgrade. The one-run public key authorizes only this exact
delivery inside the accepted User-plus-Codex supervised trial.

## PR And Main CI

PR #37 merged only after release-relevant PR run
[`29298496412`](https://github.com/foigent/x-insight/actions/runs/29298496412)
passed the intended routed contract:

- Change Classification: success in 6 seconds;
- Frontend Tests: success in 1 minute 15 seconds;
- Backend and API Validation: success in 3 minutes 6 seconds on the first cold
  Node 24 action/cache run;
- Signed V2 Release Qualification and Fresh Install Qualification: skipped; and
- protected aggregate `Backend Tests`: success in 5 seconds.

The PR was squash-merged as exact main
`3884f6d7c9a8de04bb558c41975f44f3c511c671`. Its push run
[`29298660052`](https://github.com/foigent/x-insight/actions/runs/29298660052)
then ran the complementary main-only route:

- Change Classification: success in 4 seconds;
- Frontend Tests and Backend/API Validation: skipped;
- Signed V2 Release Qualification: success in 3 minutes 24 seconds, including
  current legacy compatibility, signed double-build, exact Phase 1 artifact,
  signature/extraction and legacy transition/recovery;
- Fresh Install Qualification: success in 1 minute 1 second on a second clean
  Ubuntu VM; and
- aggregate `Backend Tests`: success in 5 seconds.

The job topology proves that ordinary code validation was not repeated on main
and release qualification was not run on the PR. These timings include cold
cache effects and are not a performance SLO.

Evidence-only PR #38 then exercised the safe-docs-only PR route in run
[`29299294613`](https://github.com/foigent/x-insight/actions/runs/29299294613):
Change Classification passed in 6 seconds, aggregate `Backend Tests` passed in
5 seconds, and Frontend, Backend/API, Signed and Fresh Install all skipped. The
PR merged as `0485a60bdbe296f65ad9e21698b2163e0f532166`; corresponding
safe-docs-only main run
[`29299387213`](https://github.com/foigent/x-insight/actions/runs/29299387213)
passed classification in 7 seconds and the aggregate gate in 6 seconds while
the same four expensive jobs skipped. Because its Signed and Fresh job results
are skipped, this green documentation run cannot qualify a new supervised
release.

## Owner-Supervised Workflow

Manual workflow run
[`29298897133`](https://github.com/foigent/x-insight/actions/runs/29298897133)
completed successfully in 41 seconds. It verified exact current main, the
matching successful main-push CI and successful aggregate/signed/fresh jobs;
generated a one-run Ed25519 identity; built and verified the delivery; confirmed
the private-key file was absent from delivery; reconfirmed main immediately
before upload; and published one bounded artifact.

| Field | Exact value |
|---|---|
| GitHub artifact ID | `8297791940` |
| Artifact name | `owner-supervised-trial-xinsight-3884f6d7c9a8de04bb558c41975f44f3c511c671` |
| Artifact ZIP size | `31,549,512` bytes |
| Artifact API digest | `sha256:0e6796f0151202b327e02cafe885e11f1549cbd8384879585f7f2368f771fa80` |
| Created | `2026-07-14T01:35:21Z` |
| Expires | `2026-10-12T01:34:39Z` |
| Product/manual | `0.2.0-rc.1` / `0.2.0-rc.1/manual-r1` |
| One-run key ID | `ed25519:sha256:79d8d19239dd2260ce22f340f5a789bbf1632c692f9a7d34838945a9c1ece645` |

## Strict Delivery Hashes

The downloaded ZIP contained exactly seven root-level regular files, no
directory or symbolic-link entry, and no filename or JSON field carrying a
private key. `SHA256SUMS` covered the other six files exactly:

| File | SHA-256 |
|---|---|
| `build-result.json` | `b1be7ed5865ddf1f4ed53a425de658978052a8e454fd342c6432f43b2ebca419` |
| `owner-supervised-trial-release.json` | `e4e5639a502c865e28578b88cc2354ef9dee4916f826e1c42dfddc120b2ad705` |
| `owner-supervised-trial.txt` | `3ccb97d46b6205a87dc6a344e5611e6d22240b371a011615a8d330be7b227344` |
| `trusted-release-keys.json` | `051d51ad0ad6ed223f1aa26e27d227ff41b3d0f310ece0628ab8f9870792d60e` |
| `xinsight-release-linux-amd64-3884f6d7c9a8de04bb558c41975f44f3c511c671` | `dab0f5d26f3ad8857a690cd74e6eee8b7f547cb2bc8205f91a1eb4526174e90a` |
| `xinsight-v0.2.0-rc.1-linux-amd64-3884f6d7c9a8de04bb558c41975f44f3c511c671.tar.gz` | `14f99e1612cd24036f0548215d52f4970c78ac556964286854ec89971bb17079` |

The signed build also recorded manifest SHA-256
`3a8d3a8aee41fb147e152cce8b6bb2a3f993af98068161dd5ed5e86d05fa9ad1`
and payload SHA-256
`988cf9d904591624a1a247b7a93d8892332757a47143989cf9a46219c6ba4dd7`.

## Execution-Owner Download Verification

Codex downloaded the GitHub artifact ZIP to the single explicit path
`/tmp/xinsight-owner-supervised-29298897133.zip` and verified:

- the downloaded bytes matched the GitHub artifact API digest;
- the strict seven-file/root-level/regular-file boundary;
- all six `SHA256SUMS` entries and content hashes;
- exact product commit, main CI run, workflow run, trust key and portable build
  bindings;
- `private_key_retained=false` and `long_lived_private_key=false`; and
- Target, External integration and Production claims remained false.

The ZIP was then deleted as one explicit file path and confirmed absent. No
recursive, wildcard or batch deletion was used. These checks are execution-
owner self-verification, not independent acceptance.

## Remaining Field Gates

- Verify this exact GitHub delivery and hashes again on the named Ubuntu 22.04
  x86_64 Target before installation.
- Complete the bounded supported-host SIGKILL/power-loss recovery sample.
- Run the accepted fixture smoke, manual-guided User tasks and only then any
  separately approved sanitized metadata.
- Obtain the customer host/access facts and UniRouter field window. A live pull
  still requires the jointly accepted endpoint/auth/cursor/revision contract;
  an approved sidecar artifact remains the binding fallback.
- Do not describe this one-run key as a second-customer, delegated, unattended
  or durable commercial release authority.

The two temporary Phase 1 snapshot artifacts remain governed by their separate
2026-07-17 exact-path cleanup obligation and were not touched.
