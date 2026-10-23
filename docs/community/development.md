# Develop and qualify X-Insight

This is the contributor and qualification entry. Package users should follow the separate [installation walkthrough](getting-started.md); Go, Node.js, synthetic fixtures, historical source reproduction and P3 qualification are not customer onboarding steps. The current preview is FPV20/schema 46. Historical scenarios below retain their original revisions and evidence scope.

## Prerequisites

Use Go 1.25.12, Node.js 22.23.1, npm 10.9.8, Git, Python 3, and a POSIX shell. Database tests need a **dedicated, disposable PostgreSQL 16 instance** with a test administrator allowed to create roles and databases. Tests may create, migrate, restore, and drop their own fixture databases. Do not point them at business data. Installed-host qualification additionally requires its declared Linux/systemd environment.

The public repository can be cloned over anonymous HTTPS with `git clone https://github.com/foigent/x-insight.git`. Check out the exact commit when reproducing a recorded result. GitHub's [exact-commit source archives](https://docs.github.com/en/rest/repos/contents#download-a-repository-archive-tar) also contain the complete source needed for the ordinary commands below, but have no `.git` database. The signed-release builder later in this guide requires a real, clean Git checkout; do not create a substitute commit to satisfy it.

Run these commands from the source root:

```sh
go version
node --version
npm --version
go mod download
npm --prefix frontend ci
node scripts/build-manual.mjs --profile=development-d
npm --prefix frontend run check
TZ=UTC npm --prefix frontend run test:coverage
npm --prefix frontend run build
python3 scripts/collect-third-party-notices.py --check
go vet ./...
go test -p 1 ./...
go run ./cmd/xinsight-importer fixtures run
```

For a cold dependency/build check, use a new disposable source directory and
fresh empty directories for `GOMODCACHE`, `GOCACHE`, `npm_config_cache` and
`PLAYWRIGHT_BROWSERS_PATH`. Point `npm_config_userconfig` and
`npm_config_globalconfig` to **two distinct empty files** while retaining the
project's `frontend/.npmrc`. Use the pinned tools and explicit Go settings:

```sh
export GOTOOLCHAIN=local GOENV=/dev/null GOWORK=off GOAUTH=off
export GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org GOFLAGS=-mod=readonly
```

Keep signing, provider, database and hosted-job credentials out of that process.
Preserve existing user caches and settings. Materialize the lock, then run the
manual, type, coverage and frontend build commands above:

```sh
npm --prefix frontend ci --ignore-scripts --no-audit --no-fund
```

Use that flag only for dependency installation: project `precheck` and
`prebuild` toolchain guards must still run. The current lock marks only the two
`fsevents` packages with install scripts; a future lock change needs its own
review. Before the first Go source operation, the new module/build caches must
still be empty. For the ordinary Linux cross-compile, choose a new output path:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=readonly -trimpath -buildvcs=false -o /absolute/new-cold-output/ ./cmd/...
```

This produces seven source command binaries, not a versioned, signed release
bundle. Keep the existing notice and protected-runtime checks:

```sh
python3 scripts/collect-third-party-notices.py --check
bash scripts/verify-protected-runtime.sh
```

For the existing synthetic browser suite, use the same fresh
`PLAYWRIGHT_BROWSERS_PATH` for
`node node_modules/playwright/cli.js install chromium` from `frontend` and
`npm --prefix frontend run test:e2e` from the root. Browser/system prerequisites
must already be available for this recipe. If they are missing, prepare them
separately using the supported platform installation instructions. Keep the
existing project assertions and record skips separately from passes.

Without a database opt-in, database tests skip: this verifies only the offline subset. `frontend` enforces the exact Node/npm versions. Do not disable that guard. The UI and manuals are embedded into the Go server, so rebuild them before building or running the server after a frontend change.

Offline source samples can be inspected without connecting to a gateway:

```sh
go run ./cmd/xinsight-importer unirouter discover --snapshot fixtures/unirouter_snapshot/minimal.json
go run ./cmd/xinsight-importer unirouter import-snapshot --snapshot fixtures/unirouter_snapshot/model_permission_denied.json
go run ./cmd/xinsight-importer unirouter import-snapshot --snapshot fixtures/unirouter_snapshot/missing_trace_gap.json
```

These fixtures demonstrate mapping and explicit gaps. They do not prove a current UniRouter deployment or a paid provider interaction.

## Database verification

Provision a fresh local PostgreSQL 16 cluster through the normal PostgreSQL tools on your platform. Keep it loopback-only. Before setting a test DSN, independently record the new cluster's connection and identity in a JSON scope file. Its required fields are `host` (`127.0.0.1`), numeric `port`, `database`, `user`, `database_oid`, and `system_id`. Read the OID from `pg_database` and the system identifier from `pg_control_system()` through the explicit connection to the cluster you just created; the latter must also match `pg_controldata` for its new data directory. Use strings for both identifiers. Do not derive this record from an unverified inherited DSN.

Set `XINSIGHT_LOCAL_TEST_DATABASE_SCOPE` to that file's absolute path and `XINSIGHT_POSTGRES_DSN` in a private process environment to the same test administrator connection. The guard checks the endpoint, database, administrator, OID, and live cluster identity before tests run. Do not put a real password in a command, issue, or transcript. Clear inherited `XINSIGHT_HOSTCTL_*_DSN` variables: native local fixtures derive their role connections only after the scope check. `XINSIGHT_ALLOW_LEGACY_SINGLE_DATABASE_ROLE=1` is a test/source compatibility opt-in, not an installed-production role policy.

```sh
export XINSIGHT_ALLOW_LEGACY_SINGLE_DATABASE_ROLE=1
export XINSIGHT_RESPONSIBILITY_LOCAL_TEST=1
export XINSIGHT_REQUIRE_POSTGRES_TESTS=1
: "${XINSIGHT_LOCAL_TEST_DATABASE_SCOPE:?Set the independently recorded scope file}"
mkdir -p output
bash scripts/reproduce-direct-fpv9.sh --source-directory "$PWD/compatibility/fpv9-linux-amd64/source" "$PWD/output/local-fpv9-source" "$PWD/output/local-fpv9-bin"
export XI_FPV9_REPRO_DIR="$PWD/output/local-fpv9-bin"
go test -p 1 -race -coverprofile=output/local-go-coverage.out ./...
python3 scripts/check-go-coverage.py output/local-go-coverage.out
```

The FPV9 output paths must be new. The command validates the locked public source selection, copies it into a new source directory, cross-compiles its two unsigned Linux binaries, and verifies both frozen hashes. `source-manifest.json` binds the original identity and 316 file digests; no private Git history is required. With the existing two-output-argument form, the script uses this bundled source when present and otherwise retains the original private worktree fallback. `bash scripts/reproduce-direct-fpv9.sh --check-source-directory "$PWD/compatibility/fpv9-linux-amd64/source"` validates the input without building or writing files. The separate original UI/manual sources and their 13-component, 18-text license mapping are recorded in `original-asset-source-manifest.json` and the fixture `NOTICE`. The Go recipe reuses the original embedded assets; it does not establish a cold historical frontend build or redistribution rights. The local recovery tests inspect these bytes using their existing fixture; they do not run Linux services on macOS. Without these inputs, the recovery cases skip and the coverage floor may fail. Keep the floor unchanged. Packages are serialized because some qualification tests intentionally share a fixture database and maintenance lock.

For Neutral D1 compatibility, obtain and qualify the five public assets using the immutable authority at `foigent/integration-control@db10730d64a9302887ad59c53921e98c757927e7`, then set `XINSIGHT_N1_RELEASE_ASSET_DIRECTORY` to that flat asset directory before the tests. The authority supports Darwin arm64 and Linux amd64. The exact download, digest, and qualification steps are retained in `.github/workflows/ci.yml`; no maintainer App credential is needed. Missing assets leave that particular compatibility test skipped, not passed. Additional CI opt-ins cover administrator rotation, Linux privilege behavior, native host lifecycle, and installed release lanes; the local command does not claim those lanes ran.

If PostgreSQL fails to initialize because shared memory IDs are exhausted, stop and resolve the local environment. Do not silently change kernel settings, stop unrelated databases, or fall back to an existing data-bearing instance.

## Open the real handler with synthetic data

The existing browser harness runs the embedded UI, HTTPS handler, access service, PostgreSQL evidence store, and workspace store. It creates its own fixture database and deletes that database when the test exits. Use a dedicated parent test database as above. No model-provider call is needed.

```sh
mkdir -p output/local-browser
export XINSIGHT_A0_BROWSER_HARNESS=1
export XINSIGHT_A0_RUNTIME_ROLES=1
export XINSIGHT_A0_BROWSER_OUTPUT="$PWD/output/local-browser"
go test ./internal/api -run '^TestWorkbenchProductionBrowserHarness$' -count=1 -timeout=35m
```

Leave that process running. In another terminal, read `output/local-browser/ready.json` for the exact HTTPS address. The default automatic harness lasts at most 30 minutes and uses a self-signed test certificate. The synthetic account is `workbench.one` with password `workbench password`; it exists only in this test harness and is not an installer default. Sign in, read the cost workbench, change scope, open a contribution, and follow its evidence. Unknown coverage remains visible even when amounts are nonzero.

To finish normally, create the exact harness stop marker:

```sh
touch output/local-browser/stop
```

Use a fresh output directory on the next run so an old stop marker does not immediately end it. Do not reuse this fixture account, certificate, or single-role compatibility setting in a deployment.

## Browser checks

Install the pinned Playwright Chromium only if it is not already available and installing browser dependencies is allowed in your environment:

```sh
cd frontend
npm run install:e2e:chromium
npm run test:e2e
```

The default suite uses synthetic API routes on a loopback Vite server. The database browser suite uses the actual harness URL:

```sh
XINSIGHT_E2E_BASE_URL=https://127.0.0.1:PORT ./node_modules/.bin/playwright test --config playwright.workbench.config.ts workbench.spec.ts
```

Replace `PORT` with the value in the current `ready.json`; do not guess another service's port. Mock browser tests establish UI behavior, while the PostgreSQL harness additionally establishes ordinary authenticated local reads. Neither is installed Linux or production acceptance.

To run `source-managed.spec.ts`, also set `XINSIGHT_A1_BROWSER_HARNESS=1` before starting a fresh harness. This loads retained synthetic source records at their original timestamps. The test reads `source_facts_period` from `ready.json`, creates an ordinary personal filter for that exact period, and reopens it in the UI. It also checks frozen versus current collection status and denial after a field grant is revoked. The production rate limit remains enabled; the real-browser configuration paces UI actions. The older workbench-only fixture account is expected to receive a scope denial for the optional work-activity panel on the home page.

## Build an unsigned preparation

After committing generated files in a clean checkout, use the existing builder:

```sh
scripts/build-signed-release.sh output/current-preparation --profile=development-d --dry-run
scripts/build-signed-release.sh output/current-preparation --profile=development-d --prepare-only
```

Choose a new output path. `--dry-run` checks release preconditions without looking up a signing key. `--prepare-only` builds an unsigned, non-installable preparation and stops before signing. A release requires a separately authorized trust authority and exact-artifact verification. CI-generated ephemeral keys and packages are qualification outputs. Public downloadability does not establish owner release authority or authorize customer installation.

For an approved managed distribution, follow [signed distribution and installation](signed-distribution.md). It covers the published FPV20 preview, independent bootstrap/trust and managed installation. The ordinary installed UniRouter pair still has its own pending acceptance; see [current status](release-readiness.md).

## Connect UniRouter and verify the pair

For ordinary installation and connection, use the [customer route](getting-started.md#connect-unirouter-and-verify-the-pair). This section preserves contributor recipes and historical records; it is not the current installed-pair status.

Start from [Foigent](https://github.com/foigent), choose [UniRouter](https://github.com/foigent/unirouter) for execution/routing or X-Insight for evidence and review, and follow the selected product's README. UniRouter's [getting-started guide](https://github.com/foigent/unirouter/blob/main/docs/community/getting-started.md) and [release status](https://github.com/foigent/unirouter/blob/main/docs/community/release-readiness.md) distinguish runnable source, installer preparation, and official signed downloads. X-Insight's [release status](release-readiness.md) is independent. Neither product requires the other for its standalone role.

For a managed connection, use [Connect the first source](../manual/en-US/index.md#connect-the-first-source) ([中文](../manual/zh-CN/current.md#首次来源接入)), followed by the ordinary-user grant instructions in that same manual. The existing invitation, source consent, credential activation, and reader grants are the connection path. Integration Control and the connector protocols are shared dependencies, not two more products to set up.

### Historical pairing and qualification limits

The following is the observed state on 2026-10-11. Commit identity, test scope, and product release status are separate facts.

| Pair or dependency | Evidence and limit |
| --- | --- |
| X-Insight `b48cc602232c065c723a98fdbc3131065ac13253` + UniRouter `69d1ef753a2121e5f288b4618bcc4bf1a5d29d48` | Local synthetic PostgreSQL and real HTTPS component handlers passed registration, normal new-install/adopt-existing setup, and restored-original setup. The restored-original case passed separately after an earlier lock/ACK conflict; the first failure was retained. This did not run installed Linux services or a complete cost/execution browser chain. |
| X-Insight CI's UniRouter fixture | `.github/workflows/ci.yml` still pins `ef52a29f2d463a8c5b136f9f18fdd975c88a0c26`. A local result at another revision does not update or qualify that pin. Current-candidate hosted CI remains required. |
| UniRouter public `main` observed at `300592e36c28e8836e37c1a2085401b8701d29d6` | Source version is 1.11.0; signed public downloads were still pending. Compared with the locally tested `69d1ef75…`, its `internal`, `cmd/uniroute`, and `contracts` trees are identical. The 18 changed files concern community installation preparation, tests, version metadata, and documentation. This comparison is not a new joint runtime result. |
| Shared dependencies | Integration Control authority `db10730d64a9302887ad59c53921e98c757927e7`; connector protocol/action `ad8fe7a38c9d7335657630baa8670dd07556b98c`. Keep their existing exact identities and checks. |

The limited local connection test above is available through the existing source entry. After completing the disposable-database prerequisites in this guide, use a fresh destination for the public UniRouter checkout. Run from the X-Insight repository root, using the pinned Go toolchain:

```sh
export XI_JOINT_TEST_CHECKOUT="$(pwd -P)"
git clone --filter=blob:none --no-checkout https://github.com/foigent/unirouter.git output/ur-joint-source
git -C output/ur-joint-source checkout --detach 69d1ef753a2121e5f288b4618bcc4bf1a5d29d48
: "${XINSIGHT_POSTGRES_DSN:?Use only the independently scoped disposable test database}"
: "${XINSIGHT_LOCAL_TEST_DATABASE_SCOPE:?Set its verified scope file}"
export TEST_DATABASE_URL="$XINSIGHT_POSTGRES_DSN"
export XI_JOINT_POSTGRES_DSN="$XINSIGHT_POSTGRES_DSN"
export REQUIRE_TEST_DATABASE=1
(
  unset XI_JOINT_MANAGED_MODE XI_JOINT_NORMAL_SETUP XI_JOINT_FUTURE_PROJECT XINSIGHT_JOINT_RESTORED_ORIGINAL
  cd output/ur-joint-source
  go test -tags integration ./internal/admin -run '^TestXInsightJointConsumerIntegration$' -count=1
  XI_JOINT_MANAGED_MODE=new_install XI_JOINT_NORMAL_SETUP=1 \
    go test -tags integration ./internal/admin -run '^TestXInsightJointConsumerIntegration$' -count=1
)
```

Run these scenarios serially, without other qualification work sharing that fixture database. The producer compiles the existing XI consumer test and supplies synthetic credentials through controlled process input. Use `adopt_existing` for the corresponding normal-setup variant; its restored-original variant also sets `XINSIGHT_JOINT_RESTORED_ORIGINAL=1`. Keep the PostgreSQL scope guard and the earlier database-test opt-ins enabled. Do not impersonate a GitHub runner or weaken an installed-host check to make a local scenario run.

This fixture verifies authorization and connection behavior. It polls eight profiles but deliberately emits only context, identity, runtime `request_started`, and captured content. It does **not** produce terminal cost, attempt, capability, policy, or destination facts. The separate XI browser fixture's costs must not be combined with this result and called one joint chain.

### Complete same-execution acceptance remains required

A complete contributor reproduction must use public source and the existing authorized connection path to produce one synthetic UniRouter execution with usage and economic facts, then let the same ordinary authorized XI user find that execution, its exact source identity, its cost basis/charged amount, and supporting evidence. Record both exact revisions and configuration, and check foreign-scope denial plus revocation of an existing reading. Never join records by a similar name or add attempt costs to the parent amount.

The existing full-chain entry is `TestScopedWorkbenchBrowserHarness` with `XI_P3_INTEROP=1`; it launches UniRouter's authenticated-pull producer and the real XI importer, then checks the same request's terminal monetary values and ordinary-reader access. The installed lane in `.github/workflows/ci.yml` has its own isolated Linux/systemd and ephemeral-trust prerequisites. This describes the retained qualification entry. The [current status matrix](release-readiness.md) separates hosted qualification from the ordinary installed pair.

The XI side now accepts an explicit disposable P3 scope through `internal/api/p3_portable_scope_test.go`. This parameterizes the existing test entry only. Without these inputs, the historical root and ports remain unchanged; J1 and CC14/15 retain their exact historical run identities.

| Operator input | Exact condition |
| --- | --- |
| `P3_PORTABLE` | `1`, together with `XI_P3_INTEROP=1` and `XINSIGHT_SCOPED_BROWSER=1`; other fixture modes must be absent |
| `P3_APPROVED_RUN` | A fresh `p3-public-<id>` using lowercase letters, digits and hyphens |
| `P3_APPROVED_ROOT` | A clean absolute root with at least three path components and the approved run as its final component; mode `0700` |
| `P3_APPROVED_ACTIONS` | `synthetic-db-redis,http,credentials,source-binding,resource-authorization,human-review-grant,secret-cleanup` |
| `P3_XI_DATABASE_PORT`, `P3_UR_DATABASE_PORT`, `P3_REDIS_PORT` | Three distinct, canonical ports from 1024 through 65535, on `127.0.0.1`; Redis remains DB 15 |
| `P3_CREDENTIAL_DIR` | `<root>/credentials/xi-product-test/<run>`, absent before the run |
| `XINSIGHT_SCOPED_BROWSER_OUTPUT` | `<root>/evidence/<run>/xi`; the run evidence directory must be fresh |

These values record an independently authorized environment; setting them does not grant permission to create credentials, start services, or access an existing database. Root execution, root-owned non-symlink directory ancestry without group/other writes, exact binary digests, database OIDs and cluster system identifiers, fresh credential/output paths, exclusive receipt creation, and exact-file secret cleanup remain required. Database URLs reject fallbacks and session overrides in portable mode. Keep the separate `P3_RESOURCE_REV2_APPROVED_RUN` consent for the existing resource-revision step. The normal formal-account option still requires the exact scoped `xinsightctl` binary and owner-prepared ordinary-account inputs.

UniRouter must mirror this P3-only scope admission in `cmd/uniroute/source_facts_authenticated_pull_integration_test.go` before the portable pair can run. Its existing producer checks must retain the empty dedicated UR database, exact OID/system identity, root/mode/link checks and cleanup ownership. The XI child forwards only the admitted scope values alongside the existing controlled environment. Pin both resulting source revisions and binary digests; the earlier public UR revision has not been qualified for these new inputs. This XI source change is not a claim that the matching UR change or a root-owned disposable Linux runtime is available.

The scope and legacy-admission tests are safe to run without databases or services:

```sh
go test ./internal/api -run '^Test(P3PortableScope|P3PortableDatabaseBoundary|P3FormalAccountModePure|J1ContentRunPathsRequireFreshR3|CC14XIAdmissionGuardPure|CC15XIAdmissionGuardPure)$' -count=1
```

For failures, inspect the existing connection request and source status before retrying: distinguish invitation/consent, configuration publication, successful polling, committed records, and ordinary-reader grants. Preserve the exact source/request IDs, expected and observed cost interpretation, HTTP/problem code, and revision in a redacted report. Follow [SECURITY.md](../../SECURITY.md) for sensitive findings; use the existing issue template and [contribution guide](../../CONTRIBUTING.md) for non-sensitive reproductions.
