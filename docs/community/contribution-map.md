# Contribution map

Choose a small outcome with a clear acceptance path. Documentation corrections, a synthetic reproduction of a bug, or an English/Chinese wording improvement are useful starting points.

| Change | Start here | Relevant checks |
| --- | --- | --- |
| UI and translations | `frontend/src/i18n/`, affected page and its tests | `npm run check`, `npm run test:coverage`, `npm run build`, relevant Playwright scenario |
| Evidence semantics | `contracts/`, adapter/source-fact implementation and fixtures | Fixture runner, contract checks, affected Go tests and real PostgreSQL when persistence is involved |
| Access and review | `internal/access/`, `internal/api/`, `internal/workspace/` | Authorized and denied reads/writes, revocation, scope revision and recovery tests |
| Database changes | `migrations/`, `internal/db/`, schema contract | Forward migration from supported states, strict schema digest, real DB rollback/recovery constraints |
| Installation/release | `scripts/build-signed-release.sh`, `internal/hostctl/`, `internal/releasebundle/` | Read-only preflight, exact payload/signature checks, supported predecessor and installed-host lanes |
| UniRouter integration | [Exact pair and reproduction limits](getting-started.md#connect-unirouter-and-verify-the-pair), existing connector/source fixtures | Same source and execution identities, usage/cost meaning, ordinary grants, foreign-scope denial and revocation; partial connection success is not full-chain acceptance |
| Documentation | README, current manual, `docs/community/` | Link/readback review, manual builder, same-version metadata and generated-asset checks |

Keep the catalogs in parity and preserve interpolation names. Add new message IDs without renumbering existing keys. Store a message recipe or a raw error/code when a visible status must follow a language change; never translate user text by matching its contents. Do not add DOM text replacement, change a component key on locale changes, or make language a data-fetch dependency.

Regenerate frontend assets with the pinned toolchain and the manual with `node scripts/build-manual.mjs --profile=development-d`. Check third-party redistribution notices with `python3 scripts/collect-third-party-notices.py --check` after changing production dependencies.

Existing historical design and operations documents are evidence of earlier decisions, not automatic authority for a current deployment. Use the exact current source and release profile for a change. Do not copy private collaboration or Target material into a public patch.
