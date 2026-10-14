# Project budget snapshots v1

This local candidate is the D source contract. It is an authenticated current
snapshot transport, never a Foigent financial event or a second amount ledger.
`manifest.json` pins each finite schema and synthetic golden vector by SHA-256.
Regenerate those assets with `python3 generate_assets.py`; run `check_contract.py`
with Python and jsonschema, optionally passing a directory of actual HTTP JSON
responses. The independent consumer checks schema, canonical bytes, exact integer
arithmetic, reading identity/replay, scope revisions and coverage conditions.

The source has three explicit profiles: budget-definition, budget-period-summary
and budget-alert, each under `unirouter.*.v1`. Both connection and immutable
credential registration must grant a requested profile. Upgrade and a new
connection grant cannot add a grant to an old credential. D-only removal and
binding changes invalidate D access/catalog/alert cursors while A/B cursors remain
valid. Pausing/revoking the whole source retains its original global semantics.

`GET /api/v1/integrations/xinsight/source/v2/budgets` lists only the credential
project's gateway budget and explicitly bound enterprise project budgets. Its
cursor is scoped to the credential, project, D access generation and catalog
revision. A changed catalog restarts enumeration. `/budgets/current?budget_ref=…`
reads one budget with one repeatable-read snapshot and a fresh `reading_id`.
Definition, summary and alerts share that ID, cutoff and revisions. Each document
is canonical JSON hashed independently; retain a reading as immutable, and reject
a changed body using the same identity. Alert continuation creates a new reading.

All money is exact decimal-string minor units with explicit currency and scale.
Gateway known_subtotal uses original `usage_finalization_journal` settled charged
money in the original UTC daily/monthly period, before the reading cutoff. It
excludes calculated prices, wallet balances, reservation amounts, call-log
baselines, media charges and enterprise allocations. Six original financial
outbox types are checked regardless of claim, delay, retries or dead-letter state.
Materialized-but-unacknowledged records are not added again. Unattributable pending
work is unknown; incomplete materialization is a gap. Open reservations and holds
are separate current observations and never estimates of final spend.

Complete coverage requires a certified writer epoch covering the whole period,
a valid exact migration/support proof and no relevant materialization gap. M118
certifies from the database migration time; it does not invent historical
coverage. Every supported database restore must run the reviewed
`scripts/source-budget-restore-rebase.sql` before readers/writers restart. It
retains the old epoch, resets coverage to the restore boundary, and makes crossing
periods incomplete. Bypassing that restore contract is not supported continuity.
Late commits appear only in later readings. Old readings are not rewritten.

Enterprise summaries use original approved/report-only budget versions and only
the explicitly bound project's matching allocation slice. No org totals, other
targets, people or raw source rows are disclosed. Gateway and enterprise amounts,
I-01 and source snapshots must never be summed as independent economic events.
Remaining budget is returned only with an active definition, complete applicable
coverage and matching money units. NULL, zero, no period, unsupported settings and
missing allocation runs remain distinct. Acknowledged/delivered alerts do not
prove human reading and retain their trigger evidence, not the current budget.

Limits: catalog/alerts at most 50 entries; at most three 64 KiB documents per
reading; 4096-byte domain-separated cursors; bounded read slots and a five-second
transaction deadline. These bounds are not performance or capacity evidence.
Golden future timestamps are controlled synthetic vectors, not live future usage.
## Reset boundary at M118

The original reset cannot erase D metadata or its authoritative journal. M118
currently has no supported successful reset lifecycle, including with D disabled.
Its CLI writes an incomplete read-only diagnostic preview; loading it as an
executable plan and apply are refused before SQL/Redis mutation. Historical
successful A/M116 reset evidence belongs to that
exact artifact, not this binary or schema. No new erasure lifecycle is included.
