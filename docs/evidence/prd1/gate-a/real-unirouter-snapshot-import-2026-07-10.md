# Real UniRouter Gate A Snapshot Import

Date: 2026-07-10
Status: CI verified and merged; local external integration verified; source denial rerun pending

## Artifact

- Exchange ID: `uxchg_f7de31e541516893a0d9b31cc0f3892a`
- Source instance: `unirouter-dev-gate-a-2026-07-10`
- Snapshot SHA-256:
  `5b1d52b1e8bd9b47f14de74670eb4404ed8862c32cdf323d86766446cdf438e8`
- Approval manifest SHA-256:
  `sha256:d2f51fc61fe84dedb8b1d1556c5a4f135b50711346327cf6fbcdd19d60337d68`
- Source rows: 1 call log, 7 trace events, 2 generated capture gaps.

## First Import Finding

The unmodified X-Insight importer accepted 8 of 10 normalized events and
quarantined 2. The source snapshot contained no raw prompt, raw response,
credential, raw IP, or raw upstream error. The quarantine was a consumer-side
false positive: substring matching treated the safe `completion_tokens` key as
raw `completion` content.

The detector was narrowed to distinguish governed token counts and identity
references from raw payload/credential keys. Raw prompt, completion, messages,
request/response bodies, API keys, tokens, secrets, and passwords remain
blocked.

## Verified Result After Fix

| Check | Result |
|-------|--------|
| In-memory normalization | 10 accepted, 0 rejected, 0 quarantined |
| In-memory rerun | 10 duplicate/no-op |
| First Postgres persistence | 10 inserted, 0 conflict, 0 rejected |
| Second Postgres persistence | 10 duplicate/no-op, 0 conflict, 0 rejected |
| Readback | 10 events, 3 capture gaps |
| Receipt | `persisted` |

Readback by request:

- denied diagnostic request: 2 gap events and 2 persisted gaps, both
  `not_available_from_source` for missing call log and trace evidence;
- successful request: 8 events covering gateway, payload policy, billing freeze,
  routing, provider response, billing settlement, logging, and token usage; one
  `provider_not_returned` gap remains for upstream request identity.

## Evidence Boundary

This is external integration evidence for one immutable UniRouter snapshot
imported into a dedicated local non-production X-Insight Postgres database. It
is not X-Insight ECS/production deployment evidence.

The denied scenario is not a correlated decision trace. UniRouter Batch 1W fixes
that source path in code and CI, but a newly authorized target rerun is required
to produce new target evidence. X-Insight preserves the two source gaps and does
not invent the missing event.

No raw prompt/response, credential, DSN, raw IP, or raw upstream error was
required or persisted from the handoff.

GitHub PR #9 passed the required Backend Tests workflow in run `29055691226`.
The check included formatting, vet, full race tests, fixture conformance,
Postgres migration/import, and read API smoke.

The execution-record reconciliation commit passed final CI run `29055761311`.
PR #9 was squash-merged as `4a562a6`.

## Next Integration Decision

Remain on controlled snapshots for one more narrow cycle. Deploy UniRouter Batch
1W, authorize one corrected denial rerun, and verify the resulting decision
evidence. Do not introduce a read-only source view or exporter/outbox yet: this
first real cycle found correctness issues on both producer and consumer sides,
and another controlled artifact is the cheapest way to prove the fixes before
adding continuous delivery complexity.
