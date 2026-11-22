# Contribution map

Use the [development guide](development.md) for tools and commands, the [architecture map](architecture.md) for ownership boundaries, and the [installation walkthrough](getting-started.md) for the published package. The [execution owner protocol](../agent-workflows/AGENT-PROTOCOL.md) records project coordination and evidence rules.

| Change | Start here | Relevant checks |
| --- | --- | --- |
| Routes and reading continuity | [App](../../frontend/src/App.tsx), [execution navigation](../../frontend/src/executionReading.ts), [workbench navigation](../../frontend/src/workbenchNavigation.ts), affected page | Filters, pagination, cutoff/revision, selection and keyboard focus after returning, current read permissions |
| UI and translations | [PageHeader](../../frontend/src/PageHeader.tsx), [Feedback](../../frontend/src/Feedback.tsx), [Identifier](../../frontend/src/Identifier.tsx), [status](../../frontend/src/status.tsx), [format](../../frontend/src/format.ts), [design tokens](../../frontend/src/design-tokens.css), [i18n](../../frontend/src/i18n/) | `npm run check`, `npm run test:coverage`, `npm run build`, relevant Playwright scenario |
| Evidence semantics | `contracts/`, adapter/source-fact implementation and fixtures | Fixture runner, contract checks, affected Go tests and real PostgreSQL when persistence is involved |
| Access and review | `internal/access/`, `internal/api/`, `internal/workspace/` | Authorized and denied reads/writes, revocation, scope revision and recovery tests |
| Database changes | `migrations/`, `internal/db/`, schema contract | Forward migration from supported states, strict schema digest, real DB rollback/recovery constraints |
| Installation/release | `scripts/build-signed-release.sh`, `internal/hostctl/`, `internal/releasebundle/` | Read-only preflight, exact payload/signature checks, supported predecessor and installed-host lanes |
| UniRouter integration | [Development pairing records and reproduction limits](development.md#connect-unirouter-and-verify-the-pair), existing connector/source fixtures; [ordinary connection route](getting-started.md#connect-unirouter-and-verify-the-pair) | Same source and execution identities, usage/cost meaning, ordinary grants, foreign-scope denial and revocation; partial connection success is not full-chain acceptance |
| Documentation | README, current manual, `docs/community/` | Link/readback review, manual builder, same-version metadata and generated-asset checks |

Use the existing headings, feedback components, and semantic tokens when their behavior matches the page. Show empty only after a successful read of the relevant scope. Independent supplemental failures can leave successful sections available. Use [reading failures](../../frontend/src/readFailures.ts) for frozen pages and [connector reads](../../frontend/src/useConnectorRead.ts) for confirmed connector status. Retry transient page failures with the same reading and cursor. A rejected continuation can use the same problem code as an invalid first-page filter. Clear the affected data and start at page one after cursor rejection. Re-establish a frozen reading before showing its results again. Clear denied connector status and its dependent controls. A status retry reads state. An explicit retry of a pending mutation keeps its original request ID and body.

A comparison preserves the cutoff and evidence revision independently for each side, including links to detail. Preserve the distinction between unknown amounts and recorded zero, and between source observations and human conclusions.

When a handler emits a new problem code, declare it in the [OpenAPI contract](../../api/openapi/prd2-v1.yaml) and check that clients offer the matching action. Before editing evidence contract inputs, check [the protected runtime verifier](../../scripts/verify-protected-runtime.sh) and [the versioned draft manifests](../../contracts/execution-evidence/incubation/). Use the contract's versioning process when an input bound to an immutable manifest must change.

Keep the catalogs in parity and preserve interpolation names. Add new message IDs without renumbering existing keys. Store a message recipe or a raw error/code when a visible status must follow a language change; never translate user text by matching its contents. Do not add DOM text replacement, change a component key on locale changes, or make language a data-fetch dependency.

Regenerate frontend assets with the pinned toolchain and the manual with `node scripts/build-manual.mjs --profile=development-d`. Verify the actual embedded files using `go test ./internal/webui`. Link to sibling Markdown chapters by filename. Confirm that the target belongs to the same profile and that the offline manual renders the link as a local anchor. Run frontend tests with `TZ=UTC` to match CI, and use explicit fixture timezones where tests assert displayed times. Check third-party redistribution notices with `python3 scripts/collect-third-party-notices.py --check` after changing production dependencies.

Existing historical design and operations documents are evidence of earlier decisions, not automatic authority for a current deployment. Use the exact current source and release profile for a change. Do not copy private collaboration or Target material into a public patch.

Before handing off maintenance changes, save the local revision in a persistent independent checkout. Record the upstream base, local revision and verification in the [execution record](../agent-workflows/EXECUTION-RECORD.md).
