# P2-4 Target Access Listener Correction

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

- Date: 2026-07-14
- Execution owner: Codex
- Classification: execution-owner access-path verification; not independent
  acceptance
- Product payload under test:
  `27e05df26ff7976301a245f9120176e6ca70430d`, `0.2.0-rc.5`
- Target: the same named non-production Alibaba Cloud ECS used by the rc.5
  lifecycle proof; resource identifiers remain outside this public record

## Outcome First

The direct macOS `ali-instance-cli portforward` command is **not an approved
X-Insight operator access path** with the tested CLI version `REDACTED_TARGET_ADDRESS`.
Although the operator connected through `127.0.0.1`, post-proof socket
inspection showed that the CLI listened on `TCP *:<local-port>`. The session
was stopped immediately and a second socket inspection confirmed that no
listener remained.

A bounded replacement passed: use `ali-instance-cli ssh` only as the OpenSSH
standard-input/output proxy, and let OpenSSH create the application forward
with an explicit local bind:

```text
ali-instance-cli ssh (Session Manager stdio proxy to Target SSH)
  -> OpenSSH -L 127.0.0.1:<local-port>:127.0.0.1:8080
  -> Target X-Insight 127.0.0.1:8080
```

Socket inspection then showed one listener owned by `ssh` at exactly
`TCP 127.0.0.1:<local-port> (LISTEN)`. No `ali-instance-cli` listener, wildcard
listener or security-group ingress was present. Target readiness and the
embedded `/executions` UI both passed through that corrected path. The tunnel
was stopped after verification and the local port again had no listener.

## Why The Original Wording Was Too Strong

Alibaba Cloud's
[Session Manager port-forwarding guide](https://www.alibabacloud.com/help/en/ecs/user-guide/perform-port-forwarding-by-using-ali-instance-cli)
documents access through `127.0.0.1:<local-port>`, but the tested command has no
documented bind-address option. An access URL is not proof of the socket's bind
scope. X-Insight therefore requires host-side listener inspection before an
operator tunnel may be accepted.

The earlier rc.5 browser investigation remains valid product evidence: it
exercised the real Target UI, API and persisted evidence. This correction
withdraws only the claim that the first local forwarding process itself was
loopback-only.

## Verification Evidence

| Check | Direct CLI forward | Corrected two-layer forward |
| --- | --- | --- |
| Cloud path | Session Manager | Session Manager |
| ECS inbound security-group rule | None added | None added |
| Target X-Insight listener | `127.0.0.1:8080` | `127.0.0.1:8080` |
| Mac application listener | `ali-instance-cli`, `TCP *:<local-port>` | `ssh`, `TCP 127.0.0.1:<local-port>` |
| Accepted for continued operator use | No | Yes, while the verified session is active |
| End-of-test state | Session stopped; no listener | Session stopped; no listener |

The corrected readiness response reported `status=ready`, PostgreSQL 16,
database schema 11, product `0.2.0-rc.5` and `manual-r5`. The same connection
returned the embedded execution-investigation HTML. No payload, database,
service, security-group or trust-store mutation was performed during this
access correction.

## Classification And Boundary

- P0: none observed.
- P1: local wildcard listener in the direct CLI forwarding method. Closed for
  the current operator workflow by terminating that listener, prohibiting the
  direct command and verifying the explicit OpenSSH loopback bind.
- P2: none newly introduced by this correction.
- Phase 1 remains closed; this is a Phase 2 operator-access finding.
- Target verification for the rc.5 product lifecycle remains valid within its
  named non-production, owner-supervised and approved-metadata scope.
- This result does not add product login, X-IAM, public access, multi-user
  authorization, External integration or Production status.

The approved command shape and fail-closed listener check are maintained in
the
[Alibaba Cloud single-node Target runbook](../../ops/deployment/alibaba-cloud-single-node-target-runbook.md).
