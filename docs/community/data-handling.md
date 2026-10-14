# Data handling and evidence boundaries

Start with synthetic fixtures. Connect real sources only after confirming the exact source, tenant, projects, allowed profiles, content policy, destination, and retention requirements.

Source records can include identities, request and execution references, timestamps, usage and economic facts, routing observations, resource metadata, and content references. Metadata itself can be sensitive. Source read grants and permission to send externally are separate from XI's right to read an imported record.

Content capture is opt-in. A source body reference does not prove that XI holds a copy. XI copy acquisition, byte/digest verification, expiry, missing/corrupt state, and current read permission are displayed independently. Reading a body is explicit and bounded; partial capture is not promoted to a complete reply. A tool-call declaration inside a body does not prove the tool executed.

Source retention and XI review-copy retention are separate policies. Changing a policy affects its documented future scope; it does not silently erase historical human assessments or make an old retained body readable after expiry. Review-body capacity and expiry operations must use the supported configuration and lifecycle commands. Do not remove storage files manually to resolve a displayed gap.

Local account hashes, service credentials, release trust, database role DSNs, and review-body encryption material belong in their prescribed protected locations. Do not commit account files, tokens, cookies, signed request URLs, raw captured content, real database snapshots, or unsanitized diagnostics. Public reports should contain minimized synthetic reproductions and only the metadata needed to understand a failure.

The browser saves a language preference under `xinsight.locale.v1`. Changing it does not alter identity, grants, API calls, evidence, or frozen reading membership. Existing navigation and pending-operation recovery stores retain their separate purpose and authorization checks.

Logs, artifacts, old branches/tags, issue text, attachments, release assets, and Git history can remain accessible even after current files are edited. A public-release review must cover those surfaces explicitly. Current-source cleanup alone is not historical clearance. See [release readiness](release-readiness.md) and [security reporting](../../SECURITY.md).
