# X-Insight Single Execution Owner Protocol

Version: 1.1
Status: active

## Purpose

X-Insight uses one execution owner to complete planning, implementation, tests,
GitHub integration, and authorized non-production environment work. Canary,
Sentinel, Atlas, or other reviewers may perform independent checks, but they are
not mandatory serial handoff roles.

The current execution owner is Codex.

## Non-Negotiable Boundaries

1. Repository tests and required GitHub CI cannot be replaced by agent judgment.
2. Tests run by the execution owner are self-verification, not independent
   review.
3. Production, destructive data operations, credentials, broader evidence
   export, raw payload capture, and paid external calls outside an approved cap
   require explicit User authorization.
4. `Local verified`, `CI verified`, `Target verified`, `Production verified`,
   and `External integration verified` are separate claims.
5. One repository has one active writer at a time. Other tasks may inspect or
   review, but must not concurrently modify the same repository or shared
   execution artifacts without an explicit handoff.

## Execution Owner Responsibilities

The execution owner may, within confirmed scope:

- read and modify X-Insight and approved cross-project files;
- run tests, static analysis, builds, browser checks, and local non-production
  services;
- create `codex/` branches, commits, PRs, and merge after required CI passes;
- deploy to an explicitly approved non-production target when access and
  rollback are established;
- write sanitized contracts, requests, evidence, and receipts to the shared
  UniRouter/X-Insight folder;
- request independent review based on risk without making the User relay normal
  task context.

The execution owner must stop and record the exact blocker when access,
approval, environment identity, rollback, or required evidence is missing.

## Risk Levels

| Level | Typical scope | Required execution evidence |
|-------|---------------|-----------------------------|
| R0 | spelling, links, indexes, non-factual docs cleanup | focused review and `git diff --check` |
| R1 | fixtures, tests, scripts, evidence helpers | focused tests and applicable CI |
| R2 | API/UI/product semantics or status truth changes | focused plus broad regression, execution record, CI |
| R3 | auth, policy enforcement, evidence redaction, secrets, billing, routing, DB migration | isolated change, focused and full regression, stop/rollback conditions, CI, explicit review checklist |
| R4 | target deployment, production, real external provider or enterprise integration | environment/version/result evidence; production and high-impact actions require User authorization |

Independent review is optional for normal continuation, but P0/P1 findings
block or reopen work. R3/R4 changes must leave a reviewer-ready checklist and
should receive an independent pass before a production claim or public release.

## Standard Flow

```text
Read current facts and worktree
  -> read relevant prior retrospective and its implementation/check entry
  -> claim active ownership in EXECUTION-RECORD.md
  -> implement the smallest coherent batch
  -> focused tests and required broad regression
  -> update execution record and immutable evidence
  -> branch, commit, PR
  -> required GitHub CI
  -> merge on pass; fix and rerun on failure
  -> authorized target work when needed
  -> reconcile status, gaps, cleanup, and next decision
  -> complete the long-task retrospective and necessary verified corrections
```

## Records And Review

`docs/agent-workflows/EXECUTION-RECORD.md` is the current mutable record. It must
include owner/task identity, branch/base, scope, risk, commands and actual
results, CI/merge/target state, approvals, rollback, cleanup, gaps, and reviewer
instructions.

Important completed facts are appended as dated evidence under
`docs/evidence/`. Existing evidence is not rewritten to make later results look
historical.

Independent review files go under:

```text
docs/agent-workflows/reviews/<work-id>-<reviewer>-<YYYYMMDD>.md
```

Each review records reviewer identity, independence, baseline commit, scope,
commands, findings, severity, and conclusion. It does not overwrite the
execution owner's record.

### Long-task completion and retrospective

Before closing a long task or an independently accepted delivery batch, the
execution owner records a short retrospective in the existing execution
record. The overall coordinator retrieves both repositories' results when the
task spans UniRouter and X-Insight. Record these five items:

1. Accepted goal, actual result, and evidence level; distinguish source, CI,
   artifact, and target results.
2. Material deviations and failures, classified from evidence as product,
   test expectation, fixture/input, environment, or execution problems; keep
   an unknown cause explicit.
3. The wrong assumption and why the existing check did not detect it.
4. Necessary correction in the original code, test, or delivery entry and its
   verification. For a reproducible failure, show that the check detects the
   old fault and accepts the correction under the relevant runtime conditions.
5. Remaining work, owner, and next action; distinguish current blockers from
   independent later improvements.

A written lesson or an updated digest alone does not close a recurring
failure. Identify why the earlier correction did not cover this occurrence,
then correct that entry or its coverage. At the next relevant task, use the
linked implementation/check, not just the historical prose. Reuse or amend
the existing lesson; if no new lesson is warranted, say so without inventing
another test, rule, approval layer, or tracking document.

Complete the necessary corrections and their applicable verification before
claiming the task complete. Keep unverified or externally blocked work
explicit; unrelated optimization does not expand the task. Update the current
summary in `EXECUTION-RECORD.md` and its `XI-CURRENT.md` entry when the task
state changes, preserving dated historical evidence. A concise final reply
states the result, effective improvement, and any remaining boundary.

## Credentials And Data

- Secrets remain in environment variables, SSH agent, Keychain, or an approved
  secret manager; never in Git, shared Markdown, receipt files, or chat.
- Raw prompt/response capture remains off unless the User approves its exact
  scope, retention, encryption, RBAC, and audit boundary.
- Evidence exchange uses explicit IDs, manifests, hashes, minimization, and
  retention. A controlled snapshot must not silently expand into broad source
  discovery.
- A missing credential or target route is a concrete blocker, not a reason to
  claim that another named role is pending.
