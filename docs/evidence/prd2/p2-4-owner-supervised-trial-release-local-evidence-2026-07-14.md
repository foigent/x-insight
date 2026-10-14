# PRD2 P2-4 Owner-Supervised Trial Release Local Evidence

Status: Local implementation and owner self-verification pass; exact canonical
`main` workflow run, frozen-toolchain delivery, named Target and User task proof
remain separate gates
Date: 2026-07-14
Execution owner: Codex
Branch: `codex/prd2-owner-supervised-trial`
Base: `main` / `origin/main` at
`2669b35c9b0e835068981adc645ba73e23c8f789`

## Accepted Outcome And Boundary

B0-D32 replaces the previously proposed long-lived offline key for the first
customer only. The product owner and Codex perform and verify every install or
upgrade; the customer cannot self-upgrade; the product owner does not create,
store, transport or enter a signing private key.

The implementation adds a manual GitHub workflow that:

1. runs only from canonical `foigent/x-insight`;
2. accepts only the exact current `main` commit and a successful `CI` push run
   for that same commit;
3. generates a one-run Ed25519 identity inside the ephemeral runner;
4. uses the existing signed-v2 release implementation;
5. removes the exact private-key file before delivery assembly;
6. validates the strict delivery and signed payload on Linux `amd64`;
7. rechecks that the commit is still current `main` immediately before upload;
   and
8. retains the bounded workflow artifact for 90 days.

“Removes” means the exact file is unlinked before upload and the one-use runner
is subsequently discarded. This design does not claim storage-level secure
overwrite; it relies on GitHub-hosted ephemeral-runner isolation and teardown.

This is a supervised bootstrap, not a durable publisher identity. It does not
authorize a second customer, delegated/third-party installation, unattended or
self-service upgrade, a Production claim or a UniRouter live transport.

## Strict Delivery Contract

The upload contains exactly seven regular files and rejects directories,
symbolic links, missing files or extras:

- one commit-addressed signed-v2 release archive;
- one commit-addressed Linux `amd64` bootstrap verifier;
- `trusted-release-keys.json` containing exactly one active public key;
- portable `build-result.json` with a basename-only output path;
- `owner-supervised-trial.txt`;
- `owner-supervised-trial-release.json`; and
- `SHA256SUMS` covering the other six files.

The release record binds product/manual versions, exact commit, artifact,
bootstrap and trust-store hashes/sizes, canonical workflow identity, successful
matching main CI, supervision limits and explicit false Target/External/
Production claims. The record schema rejects a fork repository or retained
private-key claim.

## Local Executable Evidence

The local host is macOS arm64 with Go `1.25.0`, Node `23.11.0` and npm `10.9.2`.
It is not used to claim the frozen Linux toolchain. The following owner checks
passed against the full working change:

- `bash -n` for the new build/verify scripts and existing signed-release
  scripts;
- YAML parsing for ordinary CI and the manual release workflow;
- parsing of all repository JSON and strict draft-2020-12 schema compilation;
- a valid trial record accepted, while a fork repository and
  `private_key_retained=true` were rejected;
- missing confirmation and non-canonical repository negative guards rejected
  before creating output, key or trust-store staging paths;
- `go vet ./...`, `go test -count=1 ./...` and
  `go test -race -count=1 ./...`;
- fixture conformance `11/11`;
- TypeScript, required OpenAPI paths, six component tests, 177 locked license
  entries, high-severity dependency audit with zero vulnerabilities and
  production frontend build;
- deterministic manual generation with source SHA-256
  `b7004ec64c3db4199e456ef1125e0ee2b92d12729a5e19ae40df3e2cd8a2e65c`
  and HTML SHA-256
  `d91368bfa863edf1176950f93e09526cb9ffa230f49692691323228298e35d51`;
- headed browser inspection after correcting the repository renderer's
  single-line list constraint; at 390 by 844 pixels the manual has no
  horizontal overflow; and
- relative Markdown links, sensitive diff patterns, formatting and
  `git diff --check`.

These are execution-owner self-tests, not independent acceptance.

## PR CI Finding And Disposition

PR #37 run
[`29286487988`](https://github.com/foigent/x-insight-private-history/actions/runs/29286487988)
passed Backend, Frontend and exact legacy release reproduction, then failed the
signed-v2 verification step with `trailing envelope bytes`. The exact signed
build and byte-for-byte double-build had already passed; Fresh Install was
correctly blocked by the failed prerequisite.

The failure exposed a latent gzip/tar reader defect rather than a signature,
key or artifact-tampering failure. `tar.Reader` can report its valid end markers
before `gzip.Reader` has consumed the checksum/footer. The old code immediately
checked the underlying reader and, at this release size/buffer alignment,
mistook the valid unread footer for appended bytes.

The focused correction now drives the single gzip stream to EOF, validates at
most 1 MiB of remaining tar padding as zero-only, validates the gzip checksum,
and still rejects a second gzip member or raw trailing bytes. The same helper is
used by envelope verification, payload inspection and extraction. A 26,911,206-
byte payload regression reproduces the pre-fix CI failure and passes after the
fix; tests also reject nonzero in-stream tails, excessive zero padding, a second
gzip member and raw appended bytes.

The exact PR merge-ref content was reproduced locally using Go `1.25.11`, Node
`22.23.1` and npm `10.9.8`. Its pre-fix bundle failed identically, then verified
with the corrected parser at bundle SHA-256
`9345e7a6a9c8dfcda90e28806d3cab13f8bd6b863c35894653aa7e78b0461649`.
That reproduction used a separate one-run local key; its exact private-key file
was removed immediately and is not retained. This Darwin reproduction is not a
Linux, Target or field-artifact claim. A new full PR CI run remains required.

## P0 / P1 / P2 Classification

- P0 before the first field mutation: merge this implementation, pass canonical
  `main` push CI, run the manual workflow against that exact commit/run, retain
  its immutable workflow/hash evidence, and verify that exact delivery on the
  named Target before install.
- P1 for PRD2-M1 closure: run a supported-host SIGKILL/power-loss sample and the
  accepted User manual-guided install/upgrade/restore/investigation tasks.
- P2/later: establish KMS/HSM, keyless or offline durable publisher authority
  before a second customer, delegated installer or self-service/unattended
  upgrade.

No new verified Phase 1 P0/P1 was found. The two Phase 1 temporary snapshots are
not due until 2026-07-17 and were not touched.

## Next Evidence Record

After merge, a separate append-only record will bind the exact `main` commit,
successful push CI run, owner-supervised workflow run, GitHub artifact identity,
strict seven-file set and SHA-256 values. It will not rewrite this Local record
to make later evidence appear historical.

## Post-Record CI Update — 2026-07-14

GitHub billing was restored and the failed jobs of run
[`29287222439`](https://github.com/foigent/x-insight-private-history/actions/runs/29287222439)
were rerun as attempt 3. Frontend and Backend retained their successful results;
Legacy Release Reproduction, Signed V2 Release Qualification and the dependent
Fresh Install Qualification all completed successfully. The billing block is
therefore closed and no bypass or weakened check was used.

This successful run verifies the existing full PR workflow and the focused
gzip/tar correction. It does not verify the subsequently proposed event/path
routing contract; that contract requires a new PR run and an exact merged-main
push run. Those later results belong in the separate append-only record named
above.
