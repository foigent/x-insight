# Architecture map

X-Insight receives evidence asynchronously. It does not authorize gateway execution and is not a synchronous dependency of UniRouter request handling.

[UniRouter](https://github.com/foigent/unirouter) and X-Insight are the two product entry points in [Foigent](https://github.com/foigent). [Integration Control](https://github.com/foigent/integration-control) and the [connector protocols](https://github.com/foigent/event-connector-spec) are shared implementation and contract dependencies. They are not additional products a new user must select or independently connect.

| Area | Responsibility | Code |
| --- | --- | --- |
| Source contracts and adapters | Validate source identities, revisions, hashes, gaps, and capability boundaries | `contracts/`, `internal/adapters/`, `internal/sourcefacts/` |
| Intake and projection | Receive/import atomically, distinguish duplicate no-op from conflict, maintain cursors and projections | `cmd/xinsight-importer/`, `internal/integration/`, `internal/db/` |
| Access and scoped readings | Authenticate people and machines, enforce current scope, freeze members and interpretation | `internal/access/`, `internal/api/`, `internal/workbench/` |
| Human workspace | Store assessments, exact adopted evidence, follow-up and review history independently of source facts | `internal/workspace/`, `internal/db/` |
| Web UI | Display known values and limits, preserve navigation and drafts, render both languages | `frontend/src/`, `internal/webui/` |
| Host lifecycle and release | Verify trusted payloads, preflight, journal install/upgrade/recovery, separate DB roles | `internal/hostctl/`, `internal/releasebundle/`, `cmd/xinsightctl/`, `cmd/xinsight-release/` |

Evidence and workspace writes are distinct authorities. A source correction becomes a new source revision; a human assessment does not rewrite the original source record. A saved reading fixes membership and interpretation, while current authorization still governs whether it can be read. Revoked access cannot be recovered by changing the UI language or reopening a saved URL.

The current deployment tenant is one installation. Source, instance, organization, project, and cost-project identities still form real boundaries inside that installation. Full source availability does not weaken those checks.

## Terms across the products

| Term | Meaning to preserve |
| --- | --- |
| Request / execution / attempt | Follow the source's exact identities and declared links. A gateway request, its execution record, an individual attempt, and an agent-reported action are not interchangeable records. |
| Source | Identify the source system, instance, and scope. A display name is not an identity; a source project is not automatically the same as a cost project. |
| Authorization / grant | UniRouter execution authorization, source collection consent, and X-Insight reader capabilities/grants answer different questions. Success at one boundary does not grant the others. |
| Usage / cost / billing | Usage measures consumed units. A calculated cost, a source-provided cost basis, and a gateway-charged amount retain their own meaning, currency, and precision. X-Insight does not turn those observations into a new billing authority or add attempt costs to a parent total. |

The [same-request acceptance path](getting-started.md#connect-unirouter-and-verify-the-pair) keeps these distinctions visible in the result and in denied-access checks.

The React catalog contains system-authored UI text only. Explicit interpolation carries raw source values unchanged. Date and number presentation may follow the selected locale; time-zone choice, ISO references, decimal/micro-CNY meaning, precision, and unknown/zero semantics remain stable.

Current CI pins public dependency identities instead of following floating branches. The neutral integration-control repository, action commit, and UniRouter test revision are separate contracts. Updating one pin requires reviewing its affected contract and running the corresponding negative and integration checks.
