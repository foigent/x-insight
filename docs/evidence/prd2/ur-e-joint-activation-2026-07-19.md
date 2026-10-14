# UR-E Joint Activation Evidence

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

Work ID: `XI-PRD2-UR-E-JOINT-ACTIVATION-20260719`

Owner: Codex

Activation time: `2026-07-19 12:15:26 CST`

Evidence class: execution-owner governance and local mechanism verification;
not independent runtime acceptance

## Outcome

The shared UR-E decision is `Decision accepted` / `Binding` for exactly one
fresh, manifest-bound, metadata-only synthetic snapshot-v2 sidecar execution.
Only Stage A is authorized at this checkpoint. Stage A.5 remains a sequential
read-only receiver/Target preflight and Stage B remains conditionally gated.

The accepted sidecar-only boundary and completed-execution live-feed deferral
are unchanged. No customer data, raw prompt/response, credential, automated
transport, live endpoint, second artifact, alternate Target or Production claim
was added.

## Verified Publication And Repository Identities

| Item | Verified value |
| --- | --- |
| X-Insight pre-activation main | clean `HEAD = main = origin/main = 73c537e63de51af2ad8d64ee7084203b645cd631` |
| X-Insight proposed-plan publication | PR #58, head `b46a3a456c7cbc9b6ed46f381b75e27886688643`, routed CI `29668957946` successful, merged main `22ccc9322a2a98960209a1b8cf3482765b728190` |
| X-Insight P1 publication | PR #60, head `e599091475a45dc2de99ef981029a36a98882091`, routed CI `29671175856` successful, merged main `73c537e63de51af2ad8d64ee7084203b645cd631` |
| UniRouter correction main | clean `HEAD = main = origin/main = 9a94d54975017de3b6b209d47f561af32c072178` |
| UniRouter owner-authored response | final SHA-256 `12857bd7a37436608461d22caf3c9f1fb8dbfac7274f446384349f488dba8cee`; decision `accept — exact bounded proposal only` |
| UniRouter initial planning publication | PR #221, head `325ad63438b7c09255793f0dfabc53450c3eb16c`, routed CI `29670357845` successful, merged main `c30f9a67588eab4c13486bf08512c8f4bea01ffd` |
| UniRouter correction publication | PR #222, head `85b2614de31d1a44b0f4c03afc2e8f825d8d9f9d`, routed CI `29672169049` successful, merged main `9a94d54975017de3b6b209d47f561af32c072178` |
| Producer authority retained | code identity `5176654d7d320b56c1c136f39b3debb8156ece4b`, PR head `be2d5f5d1cd6d322728b3d7c9aa8a5a347650563`, required CI `29640791281`; complete diff to correction main contains only `docs/**` |
| Proposed decision before activation | SHA-256 `c92fd0b282d2a30d438da651388a5e3958801c65d3735108516e395d7c761cd0` |
| Binding decision after final activation wording | SHA-256 `c674b04f719b64d9746f2afb4f9d5020d7f49fe96af5d1254a83e7cd911595a1` |

The docs-only UniRouter correction merge does not replace the producer code or
required-CI identity. Stage A must still recheck the exact clean generation
HEAD and its complete diff immediately before producer execution.

## Exclusive-Create Verification

The corrected response pins:

- mechanism `ur-e-exclusive-copy-v1`;
- Python `3.14.5` at
  `/opt/homebrew/Cellar/python@3.14/3.14.5/Frameworks/Python.framework/Versions/3.14/bin/python3.14`;
- interpreter SHA-256
  `2477b47fa3ae65b9574eb18a15edb364e96948eaa1875ad3f1c80d780efc9c12`;
  and
- complete mechanism code SHA-256
  `934a2036aa6fbccc95033f560d27eecf9f435d11da3f782fbf261e36a8811a74`.

X-Insight execution-owner verification reproduced two bounded cases with the
exact embedded mechanism:

1. A pre-existing destination probe against `/dev/null` stopped at
   `exclusive_create` with `created=false`, `errno_17` and exit `73`. Its device,
   inode, mode and size were unchanged.
2. A generic non-artifact success probe copied the exact 213 bytes of
   `/etc/hosts`, produced a regular mode-`0600` file, and matched SHA-256
   `c7dd0e2ed261ce76d76f852596c5b54026b9a894fa481381ffd399b556c0e2da`.

The one success-probe file was rechecked by exact path, individually removed
with `/bin/unlink`, and verified absent. The first attempted cleanup command
used the nonexistent `/usr/bin/unlink`, failed without deleting anything, and
was followed by a fresh stat before the successful exact-path operation. No
recursive, wildcard, directory or batch cleanup occurred.

These probes are owner self-verification. A read-only reviewer checked the
response, code identity and publication chain but did not execute the mechanism
or provide independent runtime acceptance.

## Governance Repair And Atomic Activation

The proposed decision had required the amended response to embed its own final
SHA-256. That is impossible to satisfy stably because embedding the value
changes the file. The owner-authored response itself specifies external
handback hashing after its publication identities are final.

The designated shared writer therefore made a non-scope-changing correction:
the response records its correction PR/head/CI/merge, while X-Insight
verification and shared Activation Evidence record the completed response SHA.
In the same activation edit, the decision became `Decision accepted` and
`Binding`, the shared root and decision/response indexes were reconciled, and
the correction request was resolved. P0=0 and P1=0 at activation.

## Negative Operation Proof At Activation

At the activation checkpoint all of the following were absent:

- `/private/tmp/unirouter-ur-e-20260719-001.approval.json`;
- `/private/tmp/unirouter-ur-e-20260719-001.snapshot.json`;
- `/private/tmp/xinsight-ur-e-20260719-001.approval.json`;
- `/private/tmp/xinsight-ur-e-20260719-001.snapshot.json`; and
- the shared sanitized handoff record
  `2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-handoff.md`.

Therefore activation itself performed no seed, manifest generation, artifact
generation, transfer, import, replay or Target mutation. The next sequential
owner action is Stage A only. No External integration claim exists yet.

## Governing Records

- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-execution.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/responses/2026-07-19-unirouter-prd2-v2-sidecar-external-integration-artifact-response.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/requests/2026-07-19-x-insight-ur-e-u0-exclusive-create-correction.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-11-prd5-runtime-contract-v1-boundary.md`
- `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-18-prd2-completed-execution-live-feed-deferral.md`
