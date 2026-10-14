# Phase 4 Operator Access Plane — Local Evidence

Date: `2026-07-29`

Work ID: `XI-PHASE4-OPERATOR-ACCESS-20260729`

Evidence class: Local implementation and Local verification only. This record
is not CI, Target, External integration, or Production evidence.

## Authority And Source Identity

- Coordinator decision:
  `2026-07-29-xinsight-operator-access-plane-decision.md`.
- Decision SHA-256:
  `e767ae5262a10e4e1f8e03e1092501c894031add7ceb7f5e2f45e2d34a147a31`.
- Branch: `codex/phase4-operator-access`.
- Base and pre-commit evidence-freeze HEAD:
  `e6c1bd3e3d6e6433b5e5717dedd50488dcf521ee`.
- Base tree:
  `1da84e19b81d52697acedc01f60446782e058023`.
- This evidence was frozen before the root coordinator's authorized local
  commit. A commit cannot self-record its own identity; the final local commit
  and tree are recorded in the external coordinator closure. No push, pull
  request, or merge identity exists for this batch.
- Worktree:
  `worktrees/xinsight-phase4-access`.
- The original dirty X-Insight workspace was not used for writing.

## Implemented Boundary

1. A strict, versioned, externally provisioned local-account credential accepts
   only Argon2id hashes, stable opaque subject IDs, normalized unique usernames,
   investigator/operator roles, and explicit disabled state. The product
   creates no default or usable account.
2. Product-owned sessions use 256-bit opaque values, retain only a one-way
   digest in memory, rotate at login, expire after 30 minutes idle or eight
   hours absolute, and are invalidated on process restart. Logout plus
   operator-only session list/revoke are implemented.
3. UI and API reads require a product Session. State changes require a
   session-bound synchronizer CSRF token and same-origin checks. Login failures
   remain generic and are bounded independently from Argon2 verification
   concurrency. Security headers, `Secure`/`HttpOnly`/`SameSite=Strict`
   cookies, RBAC, and minimized access audit are enforced.
4. Login, logout, Session management, access error handling, OpenAPI, CLI
   account generation/hash/validation, systemd credential wiring, host-control
   preflights, release identity, verifier behavior, qualification scripts, and
   operator documentation are included.
5. The reader-only database boundary remains unchanged. Readiness now executes
   a representative no-data query across the five investigation tables.
   Release schema remains `14`; release identity is `0.2.0-rc.13`, and
   `application_rollback_compatible` is `false`.
6. Anonymous Web UI requests receive a no-store `303` to `/login`; anonymous
   API requests receive the stable six-field `401` problem contract. Encoded
   traversal attempts under `/assets/` do not bypass the access boundary.
7. Only asset filenames referenced by the embedded current index are served.
   Current assets are `index-BNZCj-gf.css` and `index-C1wVW5hC.js`. Historical
   tracked assets `index-BibMuBXx.js` and `index-pndZqYsI.css` remain in the
   source tree but are unreferenced and return `404` through the Web UI
   handler.

## Abort Barrier Ordering

`Engine.Abort` performs mutation-authority and lock checks, reads the active
journal, validates its incomplete status, and then rejects any completed
`migrate_database`, `activate_release`, or `restore_database` phase, or an
active `restore_database` phase. Only after that barrier check does it validate
the source release access-account credential. Database-role restoration,
service starts, journal mutation, and receipt creation occur later.

Two focused tests passed:

- an abort before the barrier with a missing rc.13 source credential leaves the
  symlink, install state, unit, journal, and runner command count unchanged; and
- an abort after `migrate_database`, also with the credential unavailable,
  returns the barrier error before credential validation and leaves the journal
  and runner command count unchanged.

## Exact Toolchains

- Go: `go version go1.25.11 darwin/arm64`, selected with
  `GOTOOLCHAIN=go1.25.11` from the locally cached exact toolchain.
- Node.js: `v22.23.1`.
- npm: `10.9.8`.
- Node executable SHA-256:
  `2e3f1286a7eb3736346ed1803e458a0ff909e2b2d5bc746144dcb76970e9b99d`.
- PostgreSQL server, `pg_dump`, and `pg_restore`: `16.14` Homebrew builds.

## Passing Local Verification

### Go And Static Checks

- `GOTOOLCHAIN=go1.25.11 ... go test -count=1 ./...`: PASS. This included the
  complete host-control suite (`167.416s`) and all access, API, database,
  integration, release-bundle, and Web UI packages.
- `GOTOOLCHAIN=go1.25.11 ... go test -race -count=1 ./internal/access
  ./internal/api ./internal/webui`: PASS.
- `GOTOOLCHAIN=go1.25.11 ... go test -race -count=1 -run '^TestRC13'
  ./internal/hostctl`: PASS.
- Earlier focused race coverage for `./internal/db`: PASS.
- `GOTOOLCHAIN=go1.25.11 ... go vet ./...`: PASS.
- `GOTOOLCHAIN=go1.25.11 ... go mod tidy -diff`: PASS with no diff.
- Release-focused tests for `internal/releasebundle`, `internal/buildinfo`,
  `cmd/xinsightctl`, and `cmd/xinsight-server`: PASS.
- `git diff --check`: PASS.

### Frontend, Contract, Manual, And CI Routing

- Exact Node `npm run check`: PASS.
- Exact Node `npm test`: PASS, 13 of 13 tests across two files. After the root
  coordinator's test-only asynchronous-wait correction, the same complete
  suite passed five consecutive runs, 13 of 13 each time.
- Exact Node `npm run build`: PASS. Vite built the current hashed assets and
  `validate-dist.mjs` proved that the current index references exactly the
  current JS/CSS pair while the two historical tracked assets are retained but
  unreferenced.
- Exact Node `npm run check:openapi`: PASS; 11 required Read and Access API
  paths validated.
- Exact Node `npm run check:licenses`: PASS; 177 locked package license entries
  validated.
- `npm run test:e2e`: PASS, 4 of 4 Chromium desktop/narrow tests. This was a
  mock-only HTTP, pre-authenticated synthetic-Session browser test; it is not
  HTTPS, Target, or real-login evidence.
- The final generated manual source SHA-256 is
  `sha256:52262fd7c4600d19ff780f5ea92db839fbc966b12ff4e9df7753a405a9f051be`.
- `python3 scripts/ci-change-router.py --self-test`: PASS, 11 cases.
- `python3 scripts/ci-contract-gate.py --self-test`: PASS, 8 cases.
- Ruby YAML parsing of `.github/workflows/ci.yml`: PASS.
- `bash -n` for both Ubuntu qualification scripts, the signed-release verifier,
  and the signed-release builder: PASS.
- JSON parsing of both release JSON files and the access-account schema: PASS.

### Isolated PostgreSQL 16.14

An isolated temporary PostgreSQL cluster was initialized under
`/private/tmp/xinsight-phase4-pg16.Lj9ghi` and listened only on
`127.0.0.1:55439`. It used synthetic local test identities. The server was
stopped with fast shutdown after testing; final `pg_isready` returned
`no response`. The retained temporary directory was not deleted.

- Full PostgreSQL integration for `internal/db`, `internal/api`,
  `internal/integration/pullfeed`, and
  `internal/integration/litellmfeed`: PASS.
- PostgreSQL 16 role, backup, and isolated restore qualification: PASS.
  Reader `SELECT` was proved to succeed; `INSERT`, `UPDATE`, `DELETE`, and DDL
  were each proved to be rejected.
- Readiness failed closed when each required representative reader grant was
  revoked individually: PASS.
- UniRouter diagnostic ingestion persisted and found the execution under the
  exact source filter and source type: PASS.
- LiteLLM state persistence, checkpoint replay, scope binding, exact source
  filter/type, and a zero-result wrong-UniRouter filter: PASS on a fresh
  database.
- The PostgreSQL log contains expected permission-denied entries from the five
  negative grant tests and a clean shutdown.

## Root Coordinator Independent Review

The sole-writer handback completed before this review. The root coordinator
then independently inspected the authentication/session implementation, API
middleware, Web UI access boundary, host-control credential lifecycle,
readiness query, signed-release path, release/manual identity, and generated
asset serving boundary.

- Exact Go 1.25.11 focused tests for `internal/access`, `internal/api`,
  `internal/webui`, and `internal/releasebundle`: PASS.
- The first focused Go attempt was blocked because the sandbox denied an
  `httptest` loopback bind. The identical command passed when rerun with
  controlled loopback permission.
- Exact Node typecheck, five consecutive complete 13-of-13 frontend test
  suites, production build/dist validation, 11-path OpenAPI validation, and
  177-entry license validation: PASS.
- Mock-only Playwright browser verification: 4 of 4 PASS after its first local
  server bind was likewise blocked by the sandbox and the identical check was
  rerun with controlled loopback permission.
- `git diff --check`, Go formatting, shell syntax, JSON/YAML parsing, the
  11-case CI change-router self-test, a bounded high-signal secret scan, and
  checks for tracked deletion and migration changes: PASS. The final diff has
  zero tracked deletions and zero migration-file changes.
- No new P0 or P1 finding remained after review.

## Observed Failures And Corrections

These failures are retained as evidence and were not relabeled as passing:

1. The first sandboxed `initdb` attempt failed with
   `could not create shared memory segment: Operation not permitted` at
   `shmget`. The same isolated initialization then succeeded with controlled
   local escalation.
2. An intermediate migration test located `pgcrypto` outside `public`, causing
   later public migrations to fail to resolve `digest`. The helper now creates
   the extension in `public` and uses `<test-schema>,public`; the fresh final
   PostgreSQL run passed.
3. Reusing one database with a changed synthetic LiteLLM source instance
   produced `LiteLLM source registry affected 0 rows`. This is the intended
   fail-closed single-instance binding. The final named cases passed on a fresh
   database.
4. One `go mod tidy -diff` invocation used the default user Go cache and was
   denied by the filesystem sandbox. Repeating it with the controlled
   `/private/tmp` cache passed, including under exact Go 1.25.11.
5. Two frontend assertions synchronously inspected content after only the
   loading shell existed. Each failed once. They were changed to await the
   already-required result content; the full final frontend suite passed 13 of
   13.
6. Under simultaneous host load, one root-coordinator frontend run exceeded
   Testing Library's default one-second asynchronous result wait. An immediate
   sequential rerun passed. Four result waits were given an explicit
   five-second test-only timeout; no runtime code changed, and five consecutive
   complete suites then passed 13 of 13.

## Not Run And Remaining Evidence Gaps

- No signed release build or signature verification was run before this
  evidence freeze. The root coordinator owns a later clean-clone, post-commit
  exact Go 1.25.11 signed build and verifier run; its result belongs in the
  external coordinator closure rather than this self-contained commit.
- The updated GitHub Actions workflow was parsed and its routers were tested,
  but no live CI run exists for this uncommitted tree.
- No Target host login, HTTPS hostname/reverse-proxy test, real account
  provisioning, real login, deployment, traffic, External integration, or
  Production verification occurred.
- Sessions are intentionally process-local. Restart invalidates them. Account
  provisioning, disablement, and password-hash replacement remain explicit
  root-operated credential lifecycle actions.
- Public transport still requires the future HTTPS/reverse-proxy/hostname
  gate. Future OIDC is a seam only.
- Historical frontend asset files remain embedded in the source/binary asset
  set, although the runtime allowlist prevents HTTP access to unreferenced
  assets.

## Repository And External-State Safety

- During implementation and owner/root verification before evidence freeze, no
  commit, push, pull request, merge, release publication, deployment, service
  start/stop, credential creation, Target database mutation, or Target traffic
  action occurred. The separately recorded final local commit is the only
  authorized repository-state transition after this freeze.
- No real secret, password, token, private key, DSN, raw customer payload, or
  direct personal identifier is recorded in this evidence.
- The final Git diff contains no deleted tracked file. During implementation,
  two individually verified untracked/temporary files were removed in separate
  single-file actions: one temporary disabled-CI Go source and one obsolete
  generated asset. No directory, batch, recursive, wildcard, user-data, or
  tracked-file deletion occurred. The generated asset is reproducible.
- The original dirty X-Insight workspace and all user-owned unrelated changes
  were preserved.
