# Install and start X-Insight

[English entry](../../README.md) · [中文入口](../../README.zh-CN.md) · [Current status](release-readiness.md) · [For contributors](development.md)

**Install → choose your own password → sign in → connect UniRouter → confirm access and the first fact.**

Use the published package for this route. A package user does not need Go, Node.js, a source checkout or qualification fixtures. A new FPV24 installation needs no pre-created account file or default password: the first owner activates through the native handoff after installation.

## Available preview

The source is public under Apache-2.0. [FPV24](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv24) is the signed Linux amd64 development preview, with schema 47. The tested original host completed a preserving FPV23→FPV24 upgrade. An earlier FPV20 run established the empty-database installation on that host. See [current status](release-readiness.md) for the results tied to each version and topology.

## 1. Prepare and complete the packaged installation

Use the [exact FPV24 release record](releases/v0.2.0-dev.fpv24.md) and authenticate its verifier, public trust and package before execution. Pin [authority commit `9c83fba272b165edcf88134eece5a533f5ab389f`](https://github.com/foigent/x-insight/blob/9c83fba272b165edcf88134eece5a533f5ab389f/docs/community/release-authority.json). The product source is `fa1e64e1d1259c3b7d631baa7794952b0ae72d93`. Use the immutable authority link for verification.

An operator prepares the documented system dependencies, dedicated empty PostgreSQL 16 database, four distinct protected database credentials, service account and HTTPS origin. The CLI accepts Ubuntu 22.04/24.04 amd64 with systemd; one accepted Ubuntu 22.04/container-PG16 host does not qualify every topology.

Follow [verify, extract, check and install](signed-distribution.md#consumer-verify-extract-check-then-install). The operator runs the staged controller's documented `install --check`, completes the authorized `install`, and checks status/readiness and HTTPS. Preserve the generated deployment identity. A fresh install needs no account file. An existing installation needs its supported preserving upgrade or backup/retirement procedure; clearing a data-bearing database is not fresh installation.

## 2. Activate the first owner and choose your own password

For a new installation without imported accounts, follow [Accounts and project reading](https://github.com/foigent/x-insight/blob/fa1e64e1d1259c3b7d631baa7794952b0ae72d93/docs/manual/en-US/accounts.md#first-owner-and-existing-installations). An authorized host administrator uses the verified installed controller's `access bootstrap-owner --check`, then its normal operation with an exact new output file in a private directory. The native handoff points to that installation's approved HTTPS origin.

The holder opens the securely delivered activation link, chooses and confirms a unique password of at least 12 characters, then signs in normally. Links last 15 minutes and can be used once. Do not send passwords or activation/recovery links through chat, command arguments, logs or public reports. The page removes the bearer from the URL; keep the original handoff private and do not refresh an unfinished password page or reuse its stripped address.

An upgrade imports a validated existing account file once, preserving stable identities, roles and passwords; thereafter the database is authoritative. Do not bootstrap a second owner, edit the old file to reset a live password, or repeat activation for an existing active account. Daily reader/reviewer invitations, recovery and disable/enable use the owner **Accounts** page. For an existing username, use **Create recovery link** on that exact row under **Existing accounts**. The **Invite a reader or reviewer** section creates a new account through **Create activation link**. Inviting an existing username is rejected and produces no new link. **Password setup required** means activation or recovery has started but the holder has not completed password setup. Holders set their own passwords. See [normal account operations](https://github.com/foigent/x-insight/blob/fa1e64e1d1259c3b7d631baa7794952b0ae72d93/docs/manual/en-US/accounts.md#normal-account-operations).

## 3. Sign in over HTTPS

Open your deployment's `/login` with the account and password you chose. Verify the current session through Access sessions when your role permits it. Do not submit an unverified saved account from an older installation.

Sessions have a 30-minute idle limit and an eight-hour absolute limit. A previously loaded screen is not a fresh session check. If a protected read returns to login, sign in normally; the page alone does not identify the invalidation cause.

## Connect UniRouter and verify the pair

### 4. Connect the two current installations

Install UR through its [installation entry](https://github.com/foigent/unirouter/blob/main/docs/community/getting-started.md) and [published releases](https://github.com/foigent/unirouter/releases), then configure the intended customer/project and model source there. Provider credentials and paid requests have their own scope; XI installation does not supply them.

The XI operator sets the exact approved UR HTTPS origin through native `connector-target-config` and its documented restart. The XI administrator opens Sources & coverage → Connect UniRouter, confirms XI body custody, and starts the normal invitation. The current UR administrator confirms the installation, projects, capture/field scope and policy delegation on UR.

Prepare both current administrator sessions and UR's source/project configuration before starting the 15-minute invitation. For interrupted setup, inspect the same request and current state before retrying. Shared protocol contracts and Integration Control are product dependencies, not extra applications to install.

### 5. Confirm access and the first fact

Keep Auto off for the initial explicit, bounded Connect/Sync once. Configuration saved, worker loaded, received data and an ordinary user being allowed to read it are separate states.

Use one authorized, non-sensitive completed UR request. An ordinary XI user with the exact source/project grants must find the same source/request identity and supported outcome, usage and cost evidence. Missing facts remain unknown; an empty list is not successful first-fact acceptance. Verify foreign-scope denial and revocation where the acceptance scope requires them. Connection administration does not grant every reader access; capture permission does not authorize body reading.

On the tested installation, the earlier FPV21 preview ingested one authorized completed UR request that the owner and ordinary reader could both read. On FPV24, the installed UR 1.15.6 connection recovered, completed a bounded sync with zero new items, and communicated after automatic control credential rotation. The owner and reader also read the granted project in Runtime. See [current status](release-readiness.md), [first-source setup](https://github.com/foigent/x-insight/blob/fa1e64e1d1259c3b7d631baa7794952b0ae72d93/docs/manual/en-US/index.md#connect-the-first-source), [ordinary-user grants](https://github.com/foigent/x-insight/blob/fa1e64e1d1259c3b7d631baa7794952b0ae72d93/docs/manual/en-US/index.md#grant-ordinary-users-the-right-scope), and [data handling](data-handling.md).

## Developer and historical references

The [development guide](development.md) retains pinned tools, source builds, synthetic data, disposable database/browser tests and historical pairing recipes. Compatibility sources, contracts, notices and historical design records remain available for reproducibility and attribution. They are not additional steps in this packaged installation route.
