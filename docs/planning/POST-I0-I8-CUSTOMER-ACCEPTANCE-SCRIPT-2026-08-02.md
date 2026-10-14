# X-Insight Post-I0–I8 Customer Acceptance Script

Status: `REPOSITORY_CANDIDATE_NOT_RUNTIME_AUTHORIZATION`

Date: 2026-08-02

## 1. Purpose and boundary

This script verifies whether a reviewed X-Insight build supports one bounded,
metadata-only investigation workflow. It does not authorize deployment,
credentials, Connector activation, replay, traffic, database mutation or a
Production claim.

Before any target run, record the exact product commit/tree, signed artifact and
payload SHA-256, product/manual versions, database schema, environment name and
approved fixture or sanitized dataset identity. Stop if any identity differs
from the reviewed release evidence.

Never paste raw prompts, responses, credentials, tokens or unapproved source
content into the product or acceptance record.

## 2. Required actors and fixtures

- one `investigator` product Session;
- one `operator` Session only if human Session revocation is in scope;
- one `platform_admin` Session only if Agent identity review is in scope;
- one failed execution with at least one first-class Capture Gap;
- one recovered-fallback execution whose provider path is source-declared;
- one open Case and one sealed Case revision, or authorization to create them in
  the explicitly named non-production dataset;
- one dormant/degraded Connector status record with no live activation;
- one Discovery candidate with Evidence support and one abstention/limitation;
- one short-lived read-only service identity, if Agent read acceptance is
  separately authorized.

Repository synthetic fixtures may satisfy Local acceptance. They do not satisfy
Target, External integration or Production acceptance.

## 3. Acceptance sequence

### A. Shell and bounded access

1. Open the exact approved HTTPS origin and authenticate with the assigned local
   product account.
2. Confirm the context bar states metadata-only/no raw and shows the expected
   role.
3. At desktop width, confirm navigation is grouped into investigation,
   analysis, trust/access and help.
4. At 390 CSS pixels, open the explicit menu, reach Execution records, Cases and
   Connectors, then confirm the menu closes after navigation.

Pass: all authorized routes are reachable, no page-level horizontal scrolling
is needed, and no credential or raw source content appears.

### B. Failure and Gap investigation

1. In Execution records, filter to the approved failed request and fixed
   `as_of`.
2. Confirm outcome, source, Evidence status, payload mode and non-zero Gap count
   are visible in the result.
3. Open Story and read the four steps: initiator, action, systems and terminal
   point.
4. Confirm any absent identity or relationship is shown as unknown/unresolved.
5. Expand coverage and verify each incomplete axis and linked Gap remains
   explicit.
6. Inspect one node and copy its full event ID/hash from the technical detail.

Pass: no chronology-only adjacency is described as causal, no missing numeric
value appears as zero, and every limitation remains queryable.

### C. Recovered fallback path

1. Open the approved recovered-fallback execution at its fixed `as_of`.
2. Confirm the Story names the failed provider, retry/fallback and terminal
   success only when the source-declared path supports them.
3. Compare Causal graph with Chronology. At narrow width, compare the ordered
   relationship list with the same selected Evidence node.
4. Open Proof and verify source identity, immutable event hash and provenance.

Pass: graph/list edges match source declarations; Chronology remains a separate
time-order view; the UI does not infer why the first provider failed.

### D. Case citation and seal

1. Add the approved execution to the open Case.
2. Create a Finding with an explicit classification. A manual statement must
   remain `hypothesis` or `unknown` unless the API itself produced an allowed
   evidence-backed/derived claim.
3. Link one supporting Evidence item and one limiting Gap; confirm full hashes
   remain inspectable even when visually shortened.
4. Review the content boundary, prepare the seal confirmation and seal one exact
   revision.
5. Download JSON and HTML Evidence Packs and record both digests.

Pass: sealed revision/digest is immutable, JSON and HTML represent the same
snapshot, and Case writes do not change M0 Evidence.

### E. Connector trust boundary

1. Open Connectors and identify the Reference Connector.
2. Read source contract, delivery runtime and Checkpoint summary as three
   separate layers.
3. Confirm worker/import degradation does not alter historical Evidence and no
   cursor value is exposed beyond the contracted digest/absence summary.

Pass: source compatibility is not confused with worker activation; mutable
delivery fields do not appear in event identity or provenance.

### F. Discovery and Known/Unknown discipline

1. Open Discovery and inspect candidate type, method/version, support score,
   Evidence references, limitations and abstention.
2. Link an approved Alert to an existing Case only if this mutation is in the
   acceptance authorization.
3. Confirm the candidate is not presented as a calibrated fact, automatic root
   cause, routing change or enforcement action.

Pass: Evidence and limitations are visible together, and insufficient samples
remain abstentions.

### G. Agent read boundary

1. As `platform_admin`, inspect the approved short-lived service identity,
   exact permissions, expiry and status.
2. Using the separately delivered one-time token outside screenshots/logs,
   perform one approved bounded Evidence read.
3. Confirm Case/Gap mutation and credential/runtime/admin endpoints are denied.
4. Revoke the identity and confirm the same read is denied afterward.

Pass: browser Session and Agent bearer are not interchangeable; the token is
never re-displayed; revocation is explicit and effective.

## 4. Evidence to retain

- exact build, manual and environment identities;
- sanitized screenshots of each acceptance section;
- request/response status and stable non-secret IDs only;
- sealed Case revision and pack digests;
- Connector state summary without credentials or raw cursor;
- Agent identity reference, permissions, expiry and revoke receipt without the
  token;
- deviations, failed steps and whether rollback was required.

## 5. Stop conditions

Stop immediately on identity drift, unexpected raw content, an invented causal
edge, a missing Gap, an authorization bypass, credential/token exposure,
unapproved worker/traffic activity, database mutation outside the named test
objects, or any need to widen the public listener/security-group boundary.

## 6. Completion statement

Record each result separately as Local, CI, Target, External integration or
Production. Passing this script with synthetic fixtures is Local/Target
synthetic evidence only. It does not authorize real Connector traffic or a
Production claim.
