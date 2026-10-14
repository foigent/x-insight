# Controlled Snapshot Intake Runbook

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Status: ready for first real controlled snapshot
Date: 2026-07-10

## Purpose

This runbook defines what X-Insight needs from UniRouter to complete Gate A backend acceptance with real controlled evidence.

Local fixtures and CI prove the importer, persistence path, and read APIs. Gate A is not complete until a real controlled UniRouter sanitized snapshot is imported, persisted, read back, and compared to the UniRouter handoff note.

## Approved Request IDs

Approved request IDs are the small set of UniRouter `request_id` values that the user or UniRouter owner explicitly allows for sanitized X-Insight export.

They are needed because the snapshot tool intentionally forbids broad exports. X-Insight should not ask for "recent requests" or unrestricted table access for Gate A.

An approved ID should meet these conditions:

- generated in the UniRouter dev/test environment or another explicitly approved controlled environment;
- safe to export as metadata-only evidence;
- tied to a known scenario label;
- expected to contain no raw prompt, raw response, credential, token, raw IP, or raw upstream error text in the snapshot;
- stable enough that UniRouter can produce the same sanitized snapshot and handoff note;
- selected after relevant PRD5 evidence fields are available, especially for auth decision and capture-gap scenarios.

## Recommended First Scenario Set

Use one combined snapshot containing these labels when available:

| Scenario label | Required? | Why |
| --- | --- | --- |
| `successful_non_stream` | Yes | Proves normal request evidence. |
| `model_permission_denied` | Yes | Proves terminal auth decision evidence and `permission_denied` gap. |
| `provider_or_routing_failure` | Prefer | Proves failure evidence without provider payload leakage. |
| `quota_or_budget_denial` | Prefer | Proves gateway governance decision evidence. |
| `streaming_request` | Optional | Proves stream metadata behavior if available. |
| `missing_trace_or_call_log_gap` | Optional | Proves generated capture-gap handling. |

If a scenario cannot be produced quickly, do not block the first handoff. Mark it unavailable and continue with the approved IDs that exist.

## UniRouter Handoff Artifacts

Place only sanitized artifacts in the shared cooperation folder:

- `evidence/<snapshot>.json`
- `evidence/<handoff-note>.md`
- optional `evidence/<command-transcript>.md` with DSNs and credentials omitted

Do not place DSNs, credentials, tokens, raw prompts, raw responses, raw IP addresses, or raw upstream error text in the shared folder.

## X-Insight Commands

Discovery:

```sh
go run ./cmd/xinsight-importer unirouter discover \
  --snapshot "PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/evidence/<snapshot>.json"
```

In-memory import and duplicate check:

```sh
go run ./cmd/xinsight-importer unirouter import-snapshot \
  --snapshot "PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/evidence/<snapshot>.json"
```

One-command Gate A check without persistence:

```sh
go run ./cmd/xinsight-importer unirouter gate-a-check \
  --snapshot "PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/evidence/<snapshot>.json"
```

Persistent Gate A check:

```sh
XINSIGHT_POSTGRES_DSN='<x-insight postgres dsn>' \
  go run ./cmd/xinsight-server migrate up

XINSIGHT_POSTGRES_DSN='<x-insight postgres dsn>' \
  go run ./cmd/xinsight-importer unirouter gate-a-check \
  --snapshot "PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/evidence/<snapshot>.json" \
  --persist
```

Optional read API check:

```sh
XINSIGHT_POSTGRES_DSN='<x-insight postgres dsn>' \
  go run ./cmd/xinsight-server serve :8080

curl http://localhost:8080/api/v1/executions/<request_id>
curl http://localhost:8080/api/v1/executions/<request_id>/events
curl http://localhost:8080/api/v1/executions/<request_id>/gaps
```

## Acceptance Evidence To Save

After running against the real controlled snapshot, save an evidence note under `docs/evidence/prd1/gate-a/` with:

- snapshot filename and handoff note filename;
- request IDs and scenario labels;
- import accepted/rejected/quarantined counts;
- persistence inserted/duplicate/conflict counts;
- readback event and gap counts per request ID;
- normalized event types and original source event types;
- capture gaps and candidate gaps;
- any missing, ambiguous, or low-fidelity fields;
- recommendation for the next UniRouter integration mechanism.

## Decision After First Snapshot

Use the first real snapshot result to decide the next integration step:

- snapshot automation if manual handoff is sufficient but repetitive;
- read-only DB/view if field iteration requires faster controlled reads;
- exporter/outbox if near-real-time ingestion and cursor semantics become the real bottleneck.
