# Install and start X-Insight

[English entry](../../README.md) · [中文：从 UniRouter 到首条执行记录](getting-started.zh-CN.md) · [Current status](release-readiness.md) · [For contributors](development.md)

**Install → choose your own password → sign in → connect UniRouter → confirm access and the first fact.**

Use the published package for this route. A package user does not need Go, Node.js, a source checkout or qualification fixtures. A new FPV27 installation needs no pre-created account file or default password: the first owner activates through the native handoff after installation.

## Available preview

The source is public under Apache-2.0. [FPV27](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv27) is the signed Linux amd64 development preview with schema 49. See the [current status](release-readiness.md) for the exact release and installed-host evidence.

## 1. Prepare and complete the packaged installation

Use the [FPV27 release record](releases/v0.2.0-dev.fpv27.md) and pin the authority commit linked on the [canonical release](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv27) before executing the verifier. The product source is `4d2f53b048f63439fff59f6194946953732ea106`.

An operator prepares the documented system dependencies, dedicated empty PostgreSQL 16 database, four distinct protected database credentials, service account and HTTPS origin. The CLI accepts Ubuntu 22.04/24.04 amd64 with systemd; one accepted Ubuntu 22.04/container-PG16 host does not qualify every topology.

Follow [verify, extract, check and install](signed-distribution.md#consumer-verify-extract-check-then-install). The operator runs the staged controller's documented `install --check`, completes the authorized `install`, and checks status/readiness and HTTPS. Preserve the generated deployment identity. A fresh install needs no account file. An existing installation needs its supported preserving upgrade or backup/retirement procedure; clearing a data-bearing database is not fresh installation.

## 2. Activate the first owner and choose your own password

For a new installation without imported accounts, follow [Accounts and project reading](https://github.com/foigent/x-insight/blob/4d2f53b048f63439fff59f6194946953732ea106/docs/manual/en-US/accounts.md#first-owner-and-existing-installations). An authorized host administrator uses the verified installed controller's `access bootstrap-owner --check`, then its normal operation with an exact new output file in a private directory. The native handoff points to that installation's approved HTTPS origin.

The holder opens the securely delivered activation link, chooses and confirms a unique password of at least 12 characters, then signs in normally. Links last 15 minutes and can be used once. Do not send passwords or activation/recovery links through chat, command arguments, logs or public reports. The page removes the bearer from the URL; keep the original handoff private and do not refresh an unfinished password page or reuse its stripped address.

An upgrade imports a validated existing account file once, preserving stable identities, roles and passwords; thereafter the database is authoritative. Do not bootstrap a second owner, edit the old file to reset a live password, or repeat activation for an existing active account. Daily reader/reviewer invitations, recovery and disable/enable use the owner **Accounts** page. For an existing username, use **Create recovery link** on that exact row under **Existing accounts**. The **Invite a reader or reviewer** section creates a new account through **Create activation link**. Inviting an existing username is rejected and produces no new link. **Password setup required** means activation or recovery has started but the holder has not completed password setup. Holders set their own passwords. See [normal account operations](https://github.com/foigent/x-insight/blob/4d2f53b048f63439fff59f6194946953732ea106/docs/manual/en-US/accounts.md#normal-account-operations).

## 3. Sign in over HTTPS

Open your deployment's `/login` with the account and password you chose. Verify the current session through Access sessions when your role permits it. Do not submit an unverified saved account from an older installation.

Sessions have a 30-minute idle limit and an eight-hour absolute limit. A previously loaded screen is not a fresh session check. If a protected read returns to login, sign in normally; the page alone does not identify the invalidation cause.

## Connect UniRouter and verify the pair

### 4. Connect the two current installations

Install UR through its [installation entry](https://github.com/foigent/unirouter/blob/main/docs/community/getting-started.md) and [published releases](https://github.com/foigent/unirouter/releases), then configure the intended customer/project and model source there. Provider credentials and paid requests have their own scope; XI installation does not supply them.

The XI operator sets the exact approved UR HTTPS origin through native `connector-target-config` and its documented restart. The XI administrator opens Sources & coverage → Connect UniRouter, confirms XI body custody, and starts the normal invitation. The current UR administrator confirms the installation, projects, capture/field scope and policy delegation on UR.

Prepare both current administrator sessions and UR's source/project configuration before starting the 15-minute invitation. For interrupted setup, inspect the same request and current state before retrying. Shared protocol contracts and Integration Control are product dependencies, not extra applications to install.

### 5. Confirm access and the first fact

#### Find the same request in X-Insight

1. In [UniRouter](https://github.com/foigent/unirouter/blob/main/docs/community/getting-started.md#make-and-inspect-a-request), choose an authorized, non-sensitive completed request from the connected installation and the intended organization/project. Keep its gateway **Request ID**, completion time and time zone, outcome, input/output tokens, and recorded customer charge and currency if available. Use the same Request ID in both products. UR is the source for execution and customer billing facts. If settlement is still pending, verify it in UR before comparing a final charge.
2. On the XI installation's normal HTTPS `/login`, sign in as the owner with an explicit project reading grant. Repeat from an independent session signed in as the reader with the intended source/project metadata grant. In **Runtime and execution → Execution records**, enter the **Request ID** with exact matching. Select the same source if the filter is available. Set the time range to include the request and use the browser’s local time zone for the start and end fields. Open the result and check its source, organization/project and Request ID before comparing outcome, tokens and charge in the evidence you can read. The reader's grant determines which fields are visible. Connection administration and metadata grants confer no body-reading permission.
3. Compare the same measure and currency. The customer charge, a provider cost observation and an estimate are different measures. If an amount is labeled micro-CNY, divide by 1,000,000 to compare with CNY. Preserve the difference between recorded zero, pending settlement and unavailable evidence. In this walkthrough all values come from your own authorized request.

The search time range selects which executions to find. The reading cutoff (`as_of`) controls the time boundary for reading evidence. Retain the cutoff and any `evidence_revision` returned with the detail link when comparing a saved reading. A revision pins the committed evidence for that reading. A cutoff-only link may show evidence received later that occurred by that cutoff. If you open an investigation from **Cost and usage**, the link carries the chosen execution, selected event and cutoff. A saved reading still requires current access. If it expired or authorization changed, check the grant and start a fresh reading when permitted.

#### Check visibility before syncing

On an existing connection, search for the exact Request ID before starting any sync. If the record is already visible, continue with the comparison. If **Auto sync** is already enabled, leave it enabled and allow the worker to poll within scope. The configured cadence is a minimum interval. An eligible UR fact is searchable once committed in XI and visible to the signed-in role in that reading. A connection check, a data commit and a source checkpoint acknowledgment can occur at different times.

#### If the record is missing

First verify UR export eligibility and the exact source, project, ID and time range. If the fact has yet to arrive on a new connection in its default manual mode or an existing connection already in manual mode, an authorized operator can verify the active collection scope and use **Sync once** on that connection. The run is bounded and can collect other eligible records within that scope.

In **Connectors**, an authorized operator can use **Refresh local status** to check the last outgoing check, last data commit and the sync outcome without starting a fetch. Empty, partial and error results call for different checks. Share the safe diagnostic code if present. If the response to a submitted sync was lost, confirm the prior request in the product before starting another operation. If the owner can find the record and the reader cannot, check the reader's current source/project and metadata grants. Share neither connection secrets nor customer bodies. See the [FPV27 synchronization manual](https://github.com/foigent/x-insight/blob/4d2f53b048f63439fff59f6194946953732ea106/docs/manual/en-US/connection-sync.md), [ordinary-user grants](https://github.com/foigent/x-insight/blob/4d2f53b048f63439fff59f6194946953732ea106/docs/manual/en-US/index.md#grant-ordinary-users-the-right-scope), and [data handling](data-handling.md).

See the [public release status and bilingual packaged-manual erratum](release-readiness.md) for verified preview results and their scope.

## Developer and historical references

The [development guide](development.md) retains pinned tools, source builds, synthetic data, disposable database/browser tests and historical pairing recipes. Compatibility sources, contracts, notices and historical design records remain available for reproducibility and attribution. They are not additional steps in this packaged installation route.
