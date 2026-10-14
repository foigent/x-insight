# UR-E Cross-Project Governance Correction

- Date: 2026-07-19
- Owner: X-Insight / Codex
- Classification: documentation governance correction; no runtime operation
- Trigger: UniRouter review of the 2026-07-19 counterpart updates

## Outcome First

The review found two P1 governance errors: X-Insight had appended its own facts
and requested next step inside a file owned by `Responding project: UniRouter`,
and X-Insight/shared evidence described `UR-E` as binding even though no
dedicated request, UniRouter response or accepted joint decision existed.

Those findings are valid. No artifact was generated or delivered from the
invalid wording, no UniRouter code/repository/Target was changed, and External
integration was not promoted.

## Severity Disposition

| Severity | Finding | Disposition |
| --- | --- | --- |
| P0 | None observed | None open |
| P1 | `UR-E` was represented as binding without a request/decision chain | Corrected; now Open and non-binding |
| P1 | X-Insight facts were appended to a UniRouter-owned response | Corrected; removed and replaced by a separately owned request link |
| P2 | Original source-registry finding conflicted with the later fix | Corrected with explicit 2026-07-18 checkpoint chronology |
| P2 | Synthetic Target wording implied independent acceptance | Corrected to execution-owner Target verification only |
| P2 | Shared root index retained the unpublished-probe state | Corrected to distinguish the original checkpoint and later merged/Target facts |

No P0/P1 remains open in this correction scope.

## Binding And Non-Binding Facts

| Item | Status after correction |
| --- | --- |
| Operator-mediated `xinsight.import-receipt.v1` sidecar boundary | Binding under the accepted 2026-07-11 decision |
| Completed-execution live feed | Deferred as a whole for the first formal version |
| X-Insight no-new-artifact/no-External-claim condition | Valid X-Insight-owned product gate |
| `UR-E` artifact generation or delivery by UniRouter | Proposed only; not accepted, binding or authorized |
| Fresh External integration | Pending |
| Named rc.6 synthetic-fixture Target proof | Execution-owner Target verified; not independent acceptance |
| Production | Not verified / out of scope |

The shared workspace Decision Rule requires a cross-project obligation to be
summarized in `decisions/` and linked from the relevant project-owned planning
documents. A request, response or evidence file alone is not binding.

## Corrections Applied

1. The X-Insight-authored reconciliation block was removed from the
   UniRouter-owned formal response; that response now retains only a link to the
   separately owned request and explicitly does not answer or accept it.
2. X-Insight created an Open request for UniRouter to accept, decline or defer
   the bounded proposal. The request authorizes no work and pre-creates no
   decision.
3. Shared evidence now labels the original source-registry finding as a
   2026-07-18 checkpoint, records the later fix separately, and describes the
   exact synthetic Target result as execution-owner verified rather than
   independently accepted.
4. The shared root index now distinguishes the original unpublished probe from
   the later merged X-Insight consumer and rc.6 Target proof.
5. X-Insight's current truth chain now treats `UR-E` as its own proposed
   External-claim gate while counterpart decision is pending, not as an active
   UniRouter dependency.

## Current Stop Rule

Wait for UniRouter to respond accept/decline/defer to the Open request. Even an
accepting response is not binding until an accepted joint decision exists and
both project planning chains link it. Until then:

- do not generate, request transfer of or import a new artifact under `UR-E`;
- do not claim External integration;
- do not reopen the deferred live-feed slice; and
- do not modify UniRouter code or deployment.

This correction does not reopen Phase 1 and found no new Phase 1 P0/P1.
