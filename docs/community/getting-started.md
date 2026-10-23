# Install and start X-Insight

[English entry](../../README.md) · [中文入口](../../README.zh-CN.md) · [Current status](release-readiness.md) · [For contributors](development.md)

**Install → choose your own password → sign in → connect UniRouter → confirm access and the first fact.**

Use the published package for this route. A package user does not need Go, Node.js, a source checkout or qualification fixtures. Password setup is a prerequisite to completing native installation; there is no post-install password wizard or default production account.

## Available preview

The source is public under Apache-2.0. [FPV20 has seven public release attachments](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv20). The exact source `08435e09` package has passed Linux qualification, an empty-database install on one existing Ubuntu 22.04 amd64 host with pinned container PostgreSQL 16, and normal owner HTTPS access. It is a development preview. The ordinary installed UR→XI connection and first-fact acceptance remain pending; separate installations do not establish that pair.

## 1. Prepare the packaged installation

Use the [exact FPV20 release record](releases/v0.2.0-dev.fpv20.md) and authenticate its verifier, public trust and package identities before execution. The original authority pin is [`aacc40ef50ffa60f31ea924b17c257526f316ddd`](https://github.com/foigent/x-insight/blob/aacc40ef50ffa60f31ea924b17c257526f316ddd/docs/community/release-authority.json). Mutable main is a documentation discovery location, not a trust pin.

An operator prepares the documented system dependencies, dedicated empty PostgreSQL 16 database, four distinct protected database credentials, service account and HTTPS origin. The CLI accepts Ubuntu 22.04/24.04 amd64 with systemd; one accepted Ubuntu 22.04/container-PG16 host does not qualify every topology.

Follow [verify, extract, check and install](signed-distribution.md#consumer-verify-extract-check-then-install). The signed manuals retain two FPV19 example arguments: use the [external FPV20 erratum](releases/v0.2.0-dev.fpv20.md#fpv20-installation-erratum--fpv20-安装勘误), preserving the signed package and version checks.

## 2. Choose your own password and complete installation

Choose your account name and a unique password. The operator uses the verified payload's native `access generate-subject` and `access hash-password`; you enter the password twice through its hidden TTY prompt. Do not send it through chat, arguments, environment variables or a pipe. A named `platform_admin` and the protected account configuration must pass native `access validate` before installation preflight.

The operator runs the documented `install --check`, completes the authorized `install`, and checks status/readiness and HTTPS. Preserve the generated deployment identity. An existing installation needs its supported upgrade or backup/retirement procedure; clearing a data-bearing database is not fresh installation. See the [release-matched access manual](../manual/en-US/index.md#install-and-establish-access) for account shape and separate ordinary reader/reviewer roles.

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

The current installed pair has not completed these checks. See [current status](release-readiness.md), [first-source setup](../manual/en-US/index.md#connect-the-first-source), [ordinary-user grants](../manual/en-US/index.md#grant-ordinary-users-the-right-scope), and [data handling](data-handling.md). Historical component/CI fixtures are separate evidence.

## Developer and historical references

The [development guide](development.md) retains pinned tools, source builds, synthetic data, disposable database/browser tests and historical pairing recipes. Compatibility sources, contracts, notices and historical design records remain available for reproducibility and attribution. They are not additional steps in this packaged installation route.
