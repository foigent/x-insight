# PRD2 简体中文产品手册交付计划

Status: accepted PRD2 delivery plan; exact rc.6/manual-r6 incorporates the
deferred-live-feed and v2 sidecar boundary and has passed Local, packaged
clean-VM CI, owner-supervised publication and named-Target synthetic v2 UI/manual
proof. `UR-E` is accepted/binding and its exact manifest-bound synthetic
scenario is execution-owner External integration verified; field-task
qualification remains pending.
Prepared: 2026-07-13
Status reconciliation: 2026-07-19
Decision owner: User
Execution owner: Codex
Fact baseline: X-Insight `a8c25167202d0dd9180d9437f44dcdb25fca48fc`
Implementation-kickoff baseline:
`76f3f0ebe9e3a83598fe17af5067fe9ccd003789`

## 结论先行

在本计划建立时，仓库没有一套可交给用户的完整中文版 X-Insight 产品
手册。合并的 PR #35 现在实现了本计划的信息架构：12 章 `zh-CN` Markdown
权威源、确定性生成的同源离线 HTML/CSS/SVG、UI 帮助入口、版本元数据、
链接/站外依赖检查和 signed-v2 发布包绑定。

原有 `README.md` 仍只是英文开发者快速入口；Alibaba Cloud 单节点文档是
Phase 1 非生产目标运行手册；讨论、规划、审计和 X-IAM 参考材料也不等于
版本化的产品手册。用户 release 只包含生成后的 `manual/zh-CN`，不包含内部
`docs/` 规划源。

简体中文手册仍是现场版本的 P1 发布门槛：手册源文件进入
仓库，离线 HTML 进入版本化发布包，内容与二进制、API、数据库兼容矩阵和
升级路径绑定。手册本身在断网后必须完整可读；如果现场还要求完全
air-gapped 安装/升级，则 PostgreSQL/系统依赖、签名信任根、release artifact、
服务模板、SBOM/license 也必须进入另一个明确验收，不能由“离线手册”推导。

当前 Local 已通过源到 HTML 重复生成 hash、Go route/安全 header、组件测试、
桌面/390px 浏览器同源跳转、版本与零站外请求检查。GitHub run `29278281737`
进一步证明同一离线手册进入可重复签名 release，并分别在 fresh-install 与
legacy-upgrade clean VM 上由运行中的 server 同源提供；这仍是执行 owner 的 CI
自测。准确 rc.5 后续完成命名非生产 Target 的 lifecycle/UI 技术资格；准确
rc.6/manual-r6 又完成同一 Target 的原位升级、非变更式
`rollback --check --previous` 检查和 synthetic v2 UI/手册证明。
MAN-A01/A02/A03 的 User 按手册现场任务、MAN-A12 面向第二客户的 durable
publisher authority、MAN-A14 的独立盲测及 External/Production 状态仍未通过，
不能由执行 owner 自测、CI 或 Target 技术资格替代。

2026-07-14 接受的 B0-D32 为第一客户增加了更简单的受监督交付：产品 owner
不管理私钥，GitHub 手动 workflow 只接受准确当前 `main` 与同 commit 成功的
push CI，一次性私钥在交付目录组装前删除。交付记录绑定 artifact、bootstrap、
公开 trust store 和 SHA-256。这足以建立第一客户 User+Codex 受监督 bootstrap，
但不替代 MAN-A12 面向第二客户、第三方安装或自助升级的持久发行者真实性。

## 目标读者与任务

### 管理员

- 检查受支持主机、网络和 PostgreSQL 前置条件；
- 安装或接管现有 X-Insight；
- 配置 loopback-only 服务和批准的 tunnel；
- 执行备份、升级、应用回滚和数据库恢复；
- 查看版本、兼容性、健康与诊断信息。

### 平台/值班操作员

- 从 UniRouter 请求或来源标识搜索 execution；
- 阅读 execution 摘要和 evidence timeline；
- 区分时间顺序与来源声明的父子/因果关系；
- 检查 selected node、source、provenance 和 capture gap；
- 区分 direct、derived、declared、suppressed、gap、unknown 和
  `not_applicable`；
- 在证据不足时明确说“未知”，而不是补全事实。

### 安全/合规观察者

- 理解当前 metadata-only、no-raw 和单部署租户边界；
- 识别真实批准证据与 synthetic fixture 的声明差异；
- 理解 Local、CI、Target、External integration 与 Production 的验证状态
  不能互相替代；field usability 是单独的试用指标，不是现行 protocol
  定义的 verified 状态。

## 信息架构

建议的权威源目录：

```text
docs/manual/zh-CN/
  index.md
  00-version-and-support-boundary.md
  01-product-overview-and-terms.md
  admin/
    02-install-and-first-start.md
    03-configuration-security-and-access.md
    04-upgrade-migration-rollback-and-restore.md
    05-trial-exit-retention-and-handoff.md
  operator/
    06-execution-investigation-workflow.md
    07-source-provenance-and-capture-gaps.md
  reference/
    08-cli-api-and-error-contract.md
    09-maintenance-diagnostics-and-troubleshooting.md
  release-notes/
    compatibility-matrix.md
  assets/
```

### 各章最低内容

| 章节 | 必须回答的问题 |
| --- | --- |
| 版本与支持边界 | 当前产品/手册版本是什么；支持哪些 OS/CPU/PostgreSQL/升级路径；哪些状态尚未验证？ |
| 产品概览与术语 | execution、event、timeline、source、provenance、gap、proof class 分别是什么和不是什么？ |
| 安装与首次启动 | 如何校验发布包、安装 systemd 服务、配置 loopback、运行迁移并完成首个受限读取？ |
| 配置、安全与访问 | 配置项来源、secret 处理、tunnel、no-raw、数据保管和禁止公网暴露规则是什么？ |
| 升级/回滚/恢复 | 如何 preflight、备份、升级、验证、应用回滚；何时必须数据库恢复？ |
| 调查工作流 | 如何从 UniRouter 标识完成 search → detail → timeline → node/source → gap？ |
| 来源/溯源/缺口 | 七种标签如何解释；哪些是来源声明、系统派生、缺失或未知？`not_applicable` 保留协议原名。 |
| CLI/API/error | 命令、参数、版本、分页、cursor、stable error 与 correlation ID 如何使用？ |
| 维护/诊断/排障 | health/readiness、日志、升级 receipt、常见错误和安全收集诊断的方法是什么？ |
| 试用退出/保管 | retention owner、试用结束移交、无自动删除、逐一明确路径清理和清理证据如何处理？ |
| 兼容矩阵 | 每个产品版本对应的 schema/config/API/evidence/receipt/manual 版本和支持的升级来源是什么？ |

## 单一事实源与发布形式

- 仓库 Markdown 是手册的权威源；离线 HTML 从该源确定性生成。MAN-0
  必须锁定生成器、依赖 lockfile 和构建环境 digest，并通过相同源输入的
  byte/hash reproducibility 验收。
- 不维护一套与 Markdown 手工分叉的网页内容。
- 第一现场版本使用 SemVer；建议 `v0.2.0-rc.1` 作为候选，所有手册页
  显示产品版本、手册修订和适用范围。
- `manual_version` 采用 `<product-semver>/manual-r<N>`；同一已发布产品的
  文档更正递增 `manual-rN`，不得重写原 artifact。更正包同样签名并标记
  supersedes/superseded-by；历史手册/manifest 至少在对应产品支持期内保留，
  之后只能按另行接受的 release retention 规则明确处理。
- 发布 manifest 记录 `product_version`、`manual_language`、
  `manual_version` 和兼容矩阵引用。
- `xinsight-server --version`、`xinsight-importer --version` 和未来的
  `xinsightctl version` 输出产品版本、commit、schema compatibility 和
  manual version 的安全摘要。
- 离线 HTML 不使用 CDN、在线字体、外部图片或运行时网络请求。
- 离线 HTML 由同一 loopback-only Go 服务提供，不以 `file://` 为受支持
  路径。支持的浏览器家族/最低版本在 P2-0 compatibility matrix 中冻结。
- X-Insight UI 的“帮助/产品手册”入口只打开同一发布包中的同源
  `zh-CN` 手册首页，并显示相同产品/手册版本；不跳转公网文档站。
- 协议标识、JSON 字段、CLI 参数和 error code 保留英文原名；中文正文
  解释含义，不翻译成与运行时不一致的新标识。

## 发布包调整

当前验证器禁止发布包包含 `docs/`，这是 Phase 1 最小运行 artifact 的事实，
不是 PRD2 手册设计约束。PRD2 应将验证规则升级为只允许一个明确、版本化、
离线安全的手册子树，例如：

```text
manual/zh-CN/index.html
manual/zh-CN/assets/...
release-notes/...
compatibility-matrix.json
```

源码规划/讨论/内部审计文档不进入用户 artifact。发布验证器需要检查：

- 仅出现 allowlist 中的用户文档；
- HTML/asset hash 与 manifest 一致；
- 无外部 URL 依赖、secret、raw prompt/response 或内部主机信息；
- 所有内部链接和锚点可解析；
- 手册版本与二进制/manifest 一致；以及
- release artifact digest 由接受的信任根验证；detached signature、key ID
  和有效/撤销状态失败时，在解包或数据库变更前停止；以及
- 发布说明明确区分 implemented、accepted、experimental 和 not
  supported。

## 内容生产顺序

### MAN-0 — 术语和版本规则

冻结术语表、文档语气、状态词、支持矩阵字段、版本页脚和 no-raw 示例
规则。建立链接/拼写/敏感内容检查。

### MAN-1 — 管理员最短闭环

先写支持边界、安装/首次启动、配置/安全、升级/回滚/恢复。使用一个全新
受支持主机和一个已冻结、可验证的 legacy exact-commit 安装执行第一现场
版本的命令，不从作者机器状态推断用户结果。首个 SemVer release 之后，
后续版本才以真实 N-1 环境作为标准升级门槛。

MAN-0 必须生成首个升级基线记录：legacy commit、artifact/manifest SHA-256、
schema/config/service-layout 身份和被测实际安装 ID。默认候选 commit 是 Phase 1
最终部署 artifact 的 `b9e5811cc53a5cb06614956e66a1c54756617f0a`，但在
archive digest 和实际起点安装核对前 MAN-A02 保持 blocked，不补造版本/hash。

### MAN-2 — 操作员调查闭环

在 UI/API 合同稳定后写 search、timeline、selected node、source/
provenance 和 gap。每个界面截图/示例只使用 fixture 或批准的 sanitized
metadata，并标记 real/synthetic proof class。

### MAN-3 — 参考和故障排查

从版本化 CLI/API/error/schema 生成或核对参考内容；补充 readiness、listener、
cursor、overload、import limit、upgrade receipt 和 restore 决策树。

### MAN-4 — 离线构建和发布验证

生成静态 HTML，随 release candidate 打包。在网络禁用时验证手册阅读和
一次调查；完成 legacy exact-commit → 首个 SemVer 升级、应用回滚和隔离
恢复演练。若现场要求 air-gapped 安装，再单独验证所有系统/数据库依赖、
release、信任根、SBOM/license 均可离线获得。

### 手册任务盲测协议

- 管理员/操作员被试不能参与被测章节编写；记录实际人数和角色。
- 每个任务固定干净主机/数据库起点、可用 artifact、允许工具、禁止的口头
  补充、超时和完成条件。
- 安装、升级、应用回滚/数据库恢复和调查任务各有标准答案及允许错误预算。
- 至少给出两个故障决策场景，要求管理员正确选择应用回滚或数据库恢复，
  而不是仅观看系统演练。
- 任何导致敏感泄漏、错误数据处置、无法恢复或错误因果结论的缺口是 release
  P0；阻断核心任务或需要作者口头补充的缺口是 release P1。P0/P1 未处置前
  不得通过手册 gate。

## 验收矩阵

| ID | 验收条件 | 可执行证据 |
| --- | --- | --- |
| MAN-A01 | 新管理员可仅按中文手册完成全新安装和 loopback-only 首次启动。 | 干净主机任务记录、listener/readiness/read-path 证据。 |
| MAN-A02 | 管理员可仅按手册完成已验证 legacy exact-commit → 首个 SemVer；后续 release 完成真实 N-1→N。 | preflight、backup、migration、activation、receipt 和读回记录。 |
| MAN-A03 | 管理员可在盲测场景中正确区分应用回滚与数据库恢复。 | 两个决策场景、兼容应用回滚演练和隔离数据库恢复演练。 |
| MAN-A04 | 操作员可完成完整调查任务且不误判因果。 | 浏览器任务记录和问题清单。 |
| MAN-A05 | 七种证据状态的名称和示例与 UI/schema 一致。 | schema/UI/manual 自动一致性检查和人工复核。 |
| MAN-A06 | 离线手册完全可用。 | 网络禁用后的页面、导航、图片、搜索/目录检查。 |
| MAN-A07 | 手册与 release 一一对应且可重复生成。 | manifest、binary `--version`、manual footer 和 compatibility matrix 比对；相同 source/generator digest 的两次隔离构建产生完全一致的 HTML/assets hashes。 |
| MAN-A08 | 用户文档不泄漏受限数据。 | secret/sensitive/raw-content scan 和 allowlist 打包检查。 |
| MAN-A09 | 所有内部链接有效，外部依赖为零。 | Markdown/HTML link and asset checker。 |
| MAN-A10 | 状态表述真实。 | Release checklist 逐项确认 Local/CI/Target/External integration/Production，并把 field usability 单列为试用指标。 |
| MAN-A11 | UI 帮助入口打开同版本离线中文手册。 | 浏览器 E2E 检查同源 URL、版本和断网可用性。 |
| MAN-A12 | 联网/离线交付均验证 artifact 发行者真实性。 | 受信 key ID/signature/digest/有效与撤销状态检查；失败在解包前停止。 |
| MAN-A13 | Air-gapped 安装承诺仅在单独通过时出现。 | 系统/数据库依赖、信任根、release、SBOM/license 离线清单和干净主机演练。 |
| MAN-A14 | 未参与编写的用户仅按手册完成任务。 | 固定起点/帮助/超时/标准答案/错误预算的盲测记录；未处置 P0/P1 阻断 release。 |
| MAN-A15 | 试用退出、保留或移交路径可执行且无自动/批量删除。 | 受控试用数据集的 custodian/retention 决定、移交 receipt 或每次一个明确路径的清理步骤与清理证据。 |
| MAN-A16 | 第一客户不接触私钥也能验证受监督交付。 | 准确当前 `main`、成功匹配 push CI、手动 workflow、严格七文件 delivery、无私钥、SHA-256 复核及目标安装 receipt。 |

PRD2 首轮 `N=1` 是 owner-operated technical field proof，不能满足
MAN-A14 的独立盲测含义。首轮可以验证手册完整性、命令可执行性和 User 的
调查任务，但报告必须把 MAN-A14 标为 `Not run`，不得写成独立验收通过。
在未来对外声称“新管理员仅凭手册即可完成”前，仍需一名未参与编写的实际
管理员执行该项；这不阻止本轮受控技术现场验证。

## 利弊与取舍

| 选择 | 优点 | 代价/限制 |
| --- | --- | --- |
| 中文权威源 + 生成离线 HTML | 版本可审计；适合内网/现场；避免双份内容漂移 | 需要构建、链接和视觉检查；网页定制受限 |
| 手册与 release 强绑定 | 用户看到的步骤与二进制一致；升级风险更低 | 每个 release 都要维护兼容矩阵和 release notes |
| 先管理员/操作员最短闭环 | 最快支持现场安装、升级和调查 | 完整概念教程、视频和高级功能文档后置 |
| 协议名保留英文 | 可直接对应日志、CLI 和 API，减少误操作 | 中文阅读中会保留部分英文术语 |
| 不把内部规划文档打包 | 用户文档更清晰，减少内部信息泄漏 | 需要单独维护产品化解释，而不能直接复制架构笔记 |

## 非目标

- 不把 README、Phase 1 target runbook 或讨论文档改名后充当产品手册；
- 不承诺英文、繁体中文或其他语言同步交付；
- 不在手册中承诺公网、多租户、HA/DR、100x 或 Production verified；
- 不加入 raw prompt/response、真实 secret、内部账号/主机或未经批准的用户
  证据；
- 手册阅读和产品调查不依赖公网文档站；未通过 MAN-A13 时不承诺完整
  air-gapped 安装/升级；以及
- 不把 CI clean-VM 手册证明描述成 User 盲测、Named Target 或 Production
  验证。

## 已接受现场默认值与剩余外部输入

- `N=1`：User/产品决策人是唯一人类现场验证参与者；Codex 指导、执行和
  记录但不计为人类被试或独立验收者。客户 IT 在需要时提供主机、访问和
  政策事实，不是本轮必需的可用性参与者。
- User 可按中文手册执行调查任务；安装/升级/恢复由 Codex 负责实施和记录，
  并在需要客户权限或事实时向客户 IT 提出明确请求。第一版本不制作客户
  专用术语附录。
- 手册截图只使用 fixture/synthetic 数据；现场可另用批准的 sanitized
  metadata 验证产品，但不把它复制进手册。
- 已接受默认不是完整 air-gap：产品无公网监听/运行时公网依赖，IT 通过批准的
  私有路径上传签名 release 和 pinned PostgreSQL image。
- 专用 sanitized trial database 及相关备份保留至现场结束后七个自然日，
  然后由 custodian 明确选择移交或受控退役；不自动、递归、通配符或批量
  删除。

发布排期仍需客户 IT 确认实际主机是否匹配 Ubuntu 22.04 x86_64、systemd
X-Insight 和本机 PostgreSQL 16 Compose；如果实际现场改为完全 air-gapped，
MAN-A13 才成为额外交付门槛。联合现场日期和一份经批准的 UniRouter bounded
sanitized-real field input 仍是外部输入；精确 synthetic UR-E artifact 已完成，
live completed-execution feed 已从第一正式版本整体延后。七天 retention 不再
待 User 决定。第一客户采用已接受的
`owner_supervised_trial`：User 不保管长期私钥，由受控 workflow 使用一次性
key 生成准确 release，并只交付 artifact、bootstrap verifier、public trust
store、checksums 和与准确 main/CI 绑定的记录。

## 相关文档

- [PRD2-M1 现场验证实施计划](prd2-m1-unirouter-field-validation-implementation-plan-2026-07-13.md)
- [PRD2 单节点升级与回滚合同](../architecture/prd2-single-node-upgrade-and-rollback-contract-2026-07-13.md)
- [P2-4 release/upgrade/manual Local and CI evidence](../evidence/prd2/p2-4-release-upgrade-manual-local-evidence-2026-07-14.md)
- [P2-4 rc.6 named Target, v2 sidecar and UI evidence](../evidence/prd2/p2-4-rc6-named-target-v2-sidecar-and-ui-evidence-2026-07-19.md)
- [UR-E exact synthetic External integration evidence](../evidence/prd2/prd2-ur-e-exact-synthetic-external-integration-evidence-2026-07-19.md)
- [Phase 2 Product Priority](phase-2-product-priority.md)
- [Alibaba Cloud Single-Node Target Runbook](../ops/deployment/alibaba-cloud-single-node-target-runbook.md)
