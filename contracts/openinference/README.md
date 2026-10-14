# OpenInference adapter contract boundary

This directory contains X-Insight's product-local, metadata-only OpenInference
projection and compatibility evidence. Foigent Event Connector Protocol
`v2.0.2` is immutably pinned; its bounded-offline model resolves the historical
`v1.1.2` transport conflict without changing X-Insight M0 identity, hash,
first-class Capture Gap or provenance semantics.

General runtime and Production bootstrap remain fail-closed. X-Insight permits
only one exact non-production bounded-offline import when a root-owned,
short-lived `xinsight.openinference-bounded-offline-authorization.v1` binds the
target machine, product release, bundle bytes, operation and scope. This does
not enable an OTLP listener, Feed, Cursor, Checkpoint or plugin runtime.

`trace-journey-study.v1.json` is a three-span synthetic metadata-only input. It
contains no prompt/response attributes and deterministically projects three
main M0 Events plus twelve first-class Gap Events. The product-local Trace Journey read
model can correlate its persisted spans across distinct M0 executions using
the exact source/scope/trace tuple and explicit parent span IDs; it never
rewrites frozen M0 request/event/hash identities.

## Historical v1.1.2 conflict

The exact authority state reviewed by X-Insight is:

- immutable tag commit `fd82aeca9877bea8064b25c365a3cdcdd0bb3d16`;
- tree `d316d6ada678066ed7679a772856aea6157d869d`;
- read-only reassembled release-truth checkout commit
  `b0c57472fc00a72301a795a7f0e605529e9647d1` with that same tree;
- Connector Manifest v1 Schema SHA-256
  `59c405086310d0dfb2aaff37e55d73853e04319e6ccf659c66f98d8ba704a205`.

That Schema freezes `transport.mode=https_pull`,
`bootstrap.mode=activation_forward`, a pull-oriented checkpoint strategy and
`compatibility.timeline_complete=true`. A bounded OTLP/OpenInference export
processor cannot truthfully claim those semantics:

- standard OTLP delivery is push or an explicitly bounded offline export, not
  a Connector-owned HTTPS pull cursor;
- no delivery cursor or global source high-watermark exists for one accepted
  OTLP export;
- a bounded span batch cannot establish execution-level timeline completeness.

The local `connector-manifest.v1.json` deliberately preserves the last
candidate input used to expose this conflict. It must not be published,
registered, deployed or represented as conformant. The machine-readable
assessment is `shared-protocol-compatibility.v1.json`.

Resolution required a shared Protocol major version that separates generic
Connector semantics from Reference Pull Connector constraints and explicitly
models pull, push and bounded offline input. X-Insight must not resolve the
conflict by calling OTLP push “pull” or by setting `timeline_complete=true`.
Foigent Protocol v2 provided that source-neutral delivery separation; the
historical v1 artifacts remain evidence of why the boundary is necessary.
