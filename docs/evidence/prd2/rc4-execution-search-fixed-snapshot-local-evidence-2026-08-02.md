# rc.4 execution-search fixed-snapshot local evidence

Date: 2026-08-02

## Triggering Target fact

The named non-production ECS was successfully upgraded to exact `0.3.0-rc.3`, schema 19, with the
four-role PostgreSQL credential boundary verified and both Feed services inactive/disabled. After
the User manually established a fresh authenticated operator session, the default 24-hour search
correctly returned no new traffic. A bounded historical search then received 23 rows from its first
API request, but the UI's fixed-snapshot replay added the returned `page.as_of` and received
`invalid_argument`. The page consequently showed the successful count together with an invalid
filter error and hid the table.

No test traffic, Case, annotation, saved view, Connector state, credential, database data,
Cursor/Checkpoint or Feed state was created or changed while proving the defect.

## Root cause verified in source

- `ExecutionSearchPage` intentionally persists the server-returned `page.as_of` in the URL and
  re-reads the same page at that fixed snapshot.
- `investigationAPI.search` sends that value as the `as_of` query parameter.
- rc.3 `parseExecutionSearch` omitted `as_of` from its accepted query-key list and never invoked the
  existing snapshot parser for a first-page fixed replay.
- The backend already had the required UTC, 24-hour expiry, future-time and Cursor snapshot-binding
  primitives; removing the frontend snapshot behavior would have weakened reproducibility instead
  of fixing the contract.

## Implemented rc.4 candidate

`0.3.0-rc.4` / `0.3.0-rc.4/manual-r4`:

- accepts optional execution-search `as_of` and validates it through the existing fixed-snapshot
  contract;
- requires an explicitly supplied `as_of` to equal the snapshot embedded in a supplied Cursor;
- keeps the existing filter fingerprint, deterministic sort, maximum 31-day query window and
  24-hour snapshot support boundary;
- updates the OpenAPI search contract and adds first-page fixed replay, same-snapshot pagination and
  snapshot/Cursor mismatch regression coverage;
- adds exact rc.3 to the current upgrade allowlist while retaining schema 19 and the strict
  four-credential upgrade path.

This PATCH does not change migration bytes, Evidence/M0 identity or hash, Connector mappings,
Gap/Provenance/Receipt semantics, Cursor/Checkpoint representation, Workspace data models, runtime
activation or Feed defaults.

## Local verification

- focused execution-search fixed replay, pagination and mismatch tests: PASS;
- repository-wide `go test -p 1 -race -coverprofile=... ./...`: PASS; host lifecycle package
  completed in `202.312s`;
- repository-wide `go vet ./...` and Go formatting check: PASS;
- fixture conformance: PASS (`11/11`);
- release workflow self-tests and shell syntax checks: PASS;
- Phase 2C Reference and LiteLLM gates: PASS after intentionally advancing only the reviewed
  Investigation API/OpenAPI protected digests; migration tree and Connector/Event/M0 surfaces
  remained unchanged;
- TypeScript check, 36-path OpenAPI validation, 22 component tests, 177 locked-license entries,
  production frontend build and dependency audit: PASS; audit found `0` vulnerabilities;
- release-matched manual rebuilt twice with byte-identical HTML; generated HTML SHA-256 is
  `9bd4c7d14c41382fe4d33cd931b4abfd42979530cc0796e9c4bd6fda4ebabfb8` and the embedded manual
  source digest is `sha256:3e3394d17e7f315ce0c4b47b854fb99ebe1baccbd45dc707ece0eae476e655c8`.

Local Playwright was not used as release proof. PR exact-head CI remains responsible for the
frozen Node 22/npm 10 browser suite, signed release qualification and clean-host checks.

## Runtime boundary

This candidate has not been pushed, reviewed by GitHub CI, merged, signed, published or deployed.
The named non-production target remains exact rc.3, X-Insight remains active, both Feed services
remain inactive/disabled, and no Production environment is in scope.
