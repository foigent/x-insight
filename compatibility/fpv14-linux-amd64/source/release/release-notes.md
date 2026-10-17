# X-Insight 0.3.0-rc.13 发布说明

状态：本地资格已验证；准确 rc.13 CI、签名交付、Target、External integration 与 Production
资格仍分别待证。

`0.3.0-rc.13` 是一次发布就绪与技术债收敛版本，不增加产品协议或 Connector
流量授权。它强化 PostgreSQL function 权限收敛及 schema-contract 语义校验，修复 API 响应校验与
readiness 边界，补足 Go/前端覆盖率与 fuzz 回归，并让前端构建清理旧 hash 资产、真实 HTTPS/PG16
E2E 使用受认证数据路径。UI、调查逻辑、全景图和手册仍使用既有产品语义。

- database schema 推进到 23；单一 additive migration 撤销历史触发器函数的默认 PUBLIC EXECUTE，
  并固定后续函数的默认拒绝策略；schema-contract v2 现在覆盖 function owner、ACL 与 semantic
  properties，升级仍通过既有 hostctl journal 收敛 role grants；
- API 在序列化前验证受支持响应，readiness 明确区分数据库、schema、版本与手册漂移；
- CI 固定前端单元覆盖率、Go race/coverage gate、JSON/cursor fuzz、三视口 UI E2E 与真实 HTTPS/PG16
  E2E；Web 生成目录不再保留历史 hash 资产；
- `0.3.0-rc.12` 成为准确受支持升级来源；同版本覆盖和任意未列版本仍拒绝；
- Connector worker 继续 dormant，未授权 Target credential、真实 checkpoint/worker、Source resume、
  poll、Feed/Provider/upstream/Gateway traffic 或 Production。

配置 schema 仍为 `xinsight.config.v1`，手册身份为 `0.3.0-rc.13/manual-r13`。

## 0.3.0-rc.12 历史发布说明

状态：准确 main `3726e777402ba53396ef40fe5f423dcfeca32b70` 已部署为当前 Target 基线；External
integration 与 Production 资格未由本说明推导。

`0.3.0-rc.12` 将 one-click Connector 的 product-local CP1/CP2A/CP2B 生命周期接入受管 root
controller runtime。UniRouter 继续只调用固定 HTTPS redeem/poll/result 管理端点；Web 在 durable
result commit 后仅通过固定 `/run/x-insight/connector-controller.sock` 唤醒 root reconciler。
controller 使用 `xinsightctl connector-controller serve`、独立 admin DSN systemd credential、
`root:xinsight` 的 `0660` Unix socket 与固定 typed actions，不接受调用方提供的 path、unit、SQL、
shell、URL 或 DSN。

- schema 22 固定三阶段 `prepare_paused_source -> register_receiver -> resume_source`，每阶段独立
  operation/result digest、前驱绑定、精确大小写身份和幂等 replay；Event Connector Protocol 仍为
  `v2.0.2`，completed-event wire 不变；
- X 按 installation durable history 生成 normal-install epoch 1/max+1，普通安装不依赖
  DATA_RESET、Neutral 或 Assurance；receiver bearer、Connector control token 与 invitation 分域；
- root composer 只在准确 pending/active/controller_pending 生命周期中执行 checkpoint/config/
  dormant credential 操作，并在 phase-3 active result 的 fresh durable readback 后最后启动 worker；
  版本、状态、register/resume authority 漂移均在 worker side effect 前失败关闭；
- 新 `xinsight-connector-controller.service` 在 Web 之前启动，保留 AF_UNIX/AF_INET/AF_INET6 与既有
  私网 allowlist 以完成 paused-auth；安装、升级、回滚和恢复通过现有 hostctl journal/readback 收敛，
  不增加第二套 runtime、journal、receipt、schema 或手工 SHA baton；
- signed payload 增加 target-only `xinsight-connector.env.example`；受管 installer 在 root-only
  `/etc/x-insight/xinsight-connector-id.env` generate-once 写入 stable deployment ID，root CLI 只在
  独立 `/etc/x-insight/xinsight-connector.env` 原子更新规范 HTTPS UniRouter origin。upgrade、rollback
  与 re-upgrade 原字节保留 identity/target；target allowlist 无法覆盖 ID，root-only clean-environment
  `ExecStartPre` 在 Web 前执行 owner/mode/key/full-target TLS 校验。仅有 ID 或 ID+TLS 时 Connector
  disabled，不阻断 Web 启动；
- ordinary Connector 同步不依赖 N1/N2/Assurance，当前候选也不授权 Target credential、真实
  checkpoint/worker、Source resume、poll、Feed/Provider/upstream/Gateway traffic 或 Production。

数据库 schema 与 schema-contract v2 推进到 22，配置 schema 仍为 `xinsight.config.v1`；手册身份为
`0.3.0-rc.12/manual-r12`，升级新增准确 `0.3.0-rc.11` 来源。

## 0.3.0-rc.11 历史发布说明

状态：本地候选；准确 rc.11 CI、签名交付、Target、External integration 与 Production
资格仍分别待证。

`0.3.0-rc.11` 增加 product-owned 的离线签名 campaign-package consumer。operator 命令在加载
importer DSN 前完成 Neutral N1 preauthorization、独立 `source_evidence` trust、不可变 UniRouter
producer package、member/M0 schema、X component lifecycle 和准确运行二进制的验证；它没有 endpoint、
worker、Cursor、Checkpoint 或 credential surface，不会发起 Provider/Feed/upstream/Gateway 流量。

- 既有 X 签名 release manifest 是唯一发布权威。qualification 从验签 manifest 的准确
  `bin/xinsight-importer` 条目、schema 20、`0.3.0-rc.11/manual-r11`、M0 schema 与 receipt list
  确定性派生 product/verifier sidecar receipt；sidecar 不回填 payload，因此没有 hash cycle，
  也不增加 signing key/domain；
- runtime 必须同时收敛准确签名 manifest、product/verifier receipt、build commit/version/manual、
  schema 20 和正在执行的 importer SHA-256/size；任一 executable/product/manifest 漂移在 producer
  parsing 和数据库连接前失败关闭；
- package-level `package_scope_complete` 与 member-level `source_profile_complete` 独立保留。当前
  compatibility v2 缺少 runtime close marker 时保持 partial，并通过
  `runtime_close_marker_unavailable` Capture Gap 诚实表达；
- persistence 复用 schema 20 的 bounded-offline operation store、EvidenceStore 与 typed receipt。
  相同 operation/request replay 收敛为 completed receipt，request drift 或 Evidence conflict 失败关闭；
  本轮没有 migration，也不复制 editable Neutral/UniRouter schema 或实现第二套 journal/importer。

Neutral D1 只通过准确私有 release/tag/commit 与 repository-scoped GitHub App read token 获取；产品 CI
调用 Neutral 自有 qualifier，不复制其 manifest/asset/aggregate verifier。真实 campaign 还要求准确
X 签名 release/verifier identity 与另行授权的 Source trust material；本候选不授权 Target import、
signer/trust provision、DATA_RESET、BOOTSTRAP、worker、poll、traffic 或 Production。

数据库 schema 与 schema-contract v2 保持 20；shared protocol 仍为 v2.0.2；升级新增准确
`0.3.0-rc.10` 来源。

## 0.3.0-rc.10 历史发布说明

状态：本地候选；准确 rc.10 PR/CI、签名交付、Target、External integration 与 Production
资格仍分别待证。

`0.3.0-rc.10` 增加 product-owned、root-only 的 PostgreSQL admin credential rotation 生命周期，
用于在不重启长运行产品服务、不写产品数据、不启动 Connector worker 且不产生 poll/traffic 的前提下，
替换四角色 credential set 中唯一的 `xinsight_admin` 成员。preview 绑定准确 release/commit/schema/config/
protocol、完整四角色 metadata/canonical digest、PostgreSQL 16/role、零并发 admin session、34 表/migration/
connector/checkpoint/account/config 指纹和 dormant worker 状态；apply 只接受相同 preview digest、明确
`local_tooling_exposure` reason 与受保护 request file。

- successor 在 managed sets 同父目录的唯一 `0700` staging 中构建；四个 `0600` 文件分别 sync，完整加载、
  继承三角色原字节校验和 admin identity/non-reuse 校验全部通过后，才原子 rename 到 journal 的最终路径并
  sync 父目录。最终路径一旦存在只视为不可变完成态，绝不原地修补；任意 pre-rename crash 留下的 staging
  都不活跃且不妨碍 resume/abort；
- 产品在受控 predecessor transaction 中紧邻 `ALTER ROLE` 前再次确认其他 `xinsight_admin` session 为零，
  并使用进程内生成的 PostgreSQL SCRAM verifier 更新唯一角色；明文、DSN、verifier 均不进入 argv、输出、
  journal、receipt 或共享证据；
- verifier 变更前可 abort 且 predecessor 继续 active；变更后为 resume-only，随后原子切换 active set、终止
  竞态/恢复期间残留的旧 admin session，并证明 fresh successor 成功、fresh predecessor 得到预期认证拒绝；
- 静默边界只计算 PostgreSQL `client backend`，不把数据库后台进程计作或终止为 admin 工作；若 verifier
  已提交而 journal 回写前失联，abort/resume 会重新探测外部认证屏障、拒绝回滚并避免二次 verifier 更新；
- predecessor set 成功后仅 sealed/inactive 保留，不在本生命周期删除。reader/workspace/importer 原字节、
  `demo.operator` account/verifier 文件、schema/migration/product/connector/checkpoint/protocol 状态不变；主服务
  PID 不变，两个 Feed worker 继续 present/inactive/disabled，restart/product write/poll/traffic 均为零。

本轮不增加 migration，数据库 schema 仍为 20，schema-contract v2 生成字节与 shared protocol v2.0.2 pin
不变；手册身份为 `0.3.0-rc.10/manual-r10`，升级新增准确 `0.3.0-rc.9` 来源。本候选不授权 Target
credential rotation、部署、重启、reset、bootstrap、worker 或流量。

## 0.3.0-rc.9 历史发布说明

状态：本地候选；准确 rc.9 PR/CI、签名交付、Target、External integration 与 Production
资格仍分别待证。

`0.3.0-rc.9` 修复 schema-20 clean-baseline authorization contract 的 canonicalization：列对象
现在以 `namespace/table/column` 名称为身份并按名称确定性排序，物理 ordinal 仅进入非权威
layout digest。类型/Domain、nullable、default expression、identity/generated 状态与参数、collation
以及适用 constraint 仍全部受保护；任何真实语义漂移或未知对象继续 fail closed，并返回脱敏的
对象身份、原因及 expected/actual digest。

- 唯一生成源是 PostgreSQL 16 上完整执行 release migrations 后的 catalog；生成清单与 database
  schema、完整 migration-set digest、对象计数和产品 release identity 绑定；
- fresh schema-20、支持的历史升级结果、签名 payload、嵌入 binary readback、Fresh Install 与
  supported-upgrade qualification 必须收敛到同一 semantic contract；
- `schema_migrations` 的 fresh/legacy 物理列序可以不同，但命名语义相同即授权摘要相同；运行时
  ledger SQL 全部使用显式列清单，不依赖 `SELECT *` 或无列清单 INSERT；
- `xinsightctl schema-contract expected|inspect` 提供不含 DDL/secret 的 release/readback 证据；
  DATA_RESET preview 仍严格比较 expected/actual，对 semantic drift、未知表/FK/trigger/function/
  extension/type/relation 失败关闭；layout mismatch 单独报告但不阻断相同命名语义；
- DATA_RESET/BOOTSTRAP/TRAFFIC 权限边界、shared protocol v2.0.2、V1 binding、Source/Cursor、
  credential format、M0/hash/idempotency/Gap/Provenance 与 completed-execution 语义均不改变。

本轮不增加 migration，数据库 schema 仍为 20，手册身份为 `0.3.0-rc.9/manual-r9`，升级新增
准确 `0.3.0-rc.8` 来源。现存 release/upgrade 历史 dump 不属于 live-state contamination；
DATA_RESET 的单一 fresh dump/restore proof 仍按其当前 operation namespace 独立绑定，不扫描或删除
共享备份目录中的历史文件。本候选不授权 Target 部署、重启、reset、credential、登录、worker 或流量。

## 0.3.0-rc.8 历史发布说明

`0.3.0-rc.8` 为第二轮 Connector 联调增加受支持的 full-product fresh-database clean baseline，
并增加 committed/generated/embedded/signed-runtime/served Web UI 资产的预防性一致性门禁。
复核更正：基线 main 与本 PR head 的同名 JS 是同一 Git blob
`4c6afd312415a7aaef4d79aa9348a295f055a080`，SHA-256 均为
`bb09119c65eb7c89b9db46d2f76faddfed3c85ce5ade216487faec6bad21cba1`，也与先前观察到的
已安装 runtime digest 相同；因此不存在需要本 PR“修复”的既有 JS byte mismatch，先前 HOLD 中的
该 byte-mismatch 子前提不成立。其他 HOLD 条件不由本 PR 重新裁决。新增 manifest/gate 的价值是
阻止后续构建或打包重新引入漂移：

- 前端 exact Node/npm 构建生成 `xinsight.webui-asset-manifest.v1`，运行时启动时逐字节验证嵌入的
  index/JS/CSS；签名 release qualification 从已构建 binary 读回同一 manifest，并与当前 commit
  的生成资产逐项核对；未列入当前 manifest 的历史资产不能被服务；
- root-only `xinsightctl clean-baseline data-reset-preview` 对当前 34 个 public table、schema/FK/trigger/
  namespace/function/extension/type 契约、release source/adapter catalog、每表行数与稳定 semantic
  digest 做只读 fail-closed 预览；它不接受 next source/cursor/epoch。`data-reset-apply` 只接受准确
  preview SHA、fresh schema-20 backup SHA 与 `pg_restore --list` 证明和新 `xinsight_clean_*` 数据库；
  apply 先在临时 `xinsight_restore_*` 库隔离恢复同一 dump，以完整 schema/migration/34 表 semantic
  digest 证明它与停稳后的授权 source 相同，并在 receipt 前删除该临时库；live 新库只运行 migrations，
  不 restore/TRUNCATE/CASCADE 或手工删 history，仅保留 release schema/catalog，并证明
  `adapter_import_states`、epoch operation/history 及所有历史/运行表均为零；
- DATA_RESET receipt 以 stable digest 把 rollback backup、isolated-restore proof、旧 database identity、
  旧 binding identity 摘要及历史最大 epoch 封存，但不在 live DB 复制旧 checkpoint history。独立的
  `checkpoint-bootstrap-preview|apply` 只能消费
  该精确 completed receipt 和不同 reason authority，再绑定新 source/cursor/high-watermark，使用封存的
  `historical max+1` 建立一个 inactive/idle、零 counter、无 operation/history 的 checkpoint；
- DATA_RESET 以 crash-safe quarantine 退役 UniRouter receiver active/staged/operation-state，以及独立的
  LiteLLM credential 与包含旧 source/org/project scope 的 environment 文件；receipt 前失败恢复精确
  原状态，receipt 后移除 quarantine 并证明五个 canonical path 均为空。预览/readback 只读元数据，
  不读取或输出文件内容，也不生成任何 replacement。systemd unit 保留但 inactive+disabled；后续独立
  UniRouter BOOTSTRAP 仍不得生成 LiteLLM credential/config。当前 Paths 合同没有 OpenInference 等价
  持久文件路径，因此不虚构或扩张文件清理面，其数据库历史由全局 fresh database 归零；
- cutover 只保留 `demo.operator` 的 subject/username/role/disabled/password verifier 原字节，移除
  其他本地账号并通过主服务进程替换清除旧内存 Session；两个 Feed worker 全程必须 present、
  inactive、disabled，不启动、不 enable、不 poll；
- durable journal 在 receipt 前失败会删除临时 restore 库并恢复旧 database-set link、完整账号前件和
  importer/service，同时保留新严格空库供取证且禁止复用；receipt 已精确落盘时只允许 readback 后
  resume。resume 在旧库退役前再次验证 retained dump identity、isolated-restore proof、strict-empty live
  link 与零 session，然后删除并证明 predecessor/restore 数据库均不存在，再精确移除含其他账号的
  单文件前驱备份；缺少 dump/proof 时 fail closed，不允许把已完成 cutover abort 掉。成功 completed
  只保留一个已验证 custom-format dump，不同时保留旧数据库；
- fresh PostgreSQL 16、未知 schema/FK/trigger、demo.operator 精确保留、空 Read API/UI、严格空
  checkpoint receipt、独立 bootstrap authority、monotonic epoch、旧 source identity 拒绝、全局
  connector credential/config 退役、隔离 restore proof、旧库 receipt 后退役和 crash recovery 均加入
  回归测试。

本轮不增加 migration，数据库 schema 仍为 20，手册身份为 `0.3.0-rc.8/manual-r8`，升级新增
准确 `0.3.0-rc.7` 来源。Foigent Protocol 仍固定不可变 v2.0.2；M0、event/hash/idempotency、
Gap、Provenance、Receipt 与 Feed wire 语义不变。clean-baseline 命令的存在不授权 Target reset、
备份、credential、登录、服务操作、Feed/测试流量、External integration 或 Production。

## 0.3.0-rc.7 历史发布说明

`0.3.0-rc.7` 为第二轮 Connector 联调增加 source-neutral checkpoint epoch 过渡能力：

- schema 20 为任一 product-local Source/Adapter/Cursor binding 增加单一 prepared operation、
  不可变历史 epoch 与当前 epoch identity；旧 checkpoint 不再通过手工 SQL 删除或覆盖；
- root-only `xinsightctl connector-epoch prepare|status|activate|abort` 要求受管 worker 存在、
  inactive、disabled，且 durable state 为 idle/inactive、无错误、Cursor=high-watermark；
- prepare 固定 active-state SHA-256 与不泄露 raw Cursor 的 plan SHA-256；activate 在单一
  serializable PostgreSQL transaction 中归档旧 epoch、原子切换新 source instance/Cursor，
  并清零新 epoch counters/retry state；重试 exact activate 幂等，激活前可 exact abort；
- Connector trust API/UI 增加当前 checkpoint epoch，只读展示继续与 Evidence 聚合分离；
- Foigent Protocol 仍固定不可变 `v2.0.2`。completed-execution DTO、Feed wire、M0、Gap、
  Provenance、Receipt、Trace Journey、event/hash/idempotency 与已有 Evidence 行不改变。

本轮数据库 schema 为 20，手册身份为 `0.3.0-rc.7/manual-r7`，升级新增准确
`0.3.0-rc.6` 来源。epoch prepare/activate 不会启动或 enable Feed，不配置凭据、不创建流量、
不执行 UniRouter reset，也不授权 External integration 或 Production。

## 0.3.0-rc.6 历史发布说明

状态：本地候选；准确 rc.6 PR/CI、签名交付、Target、External integration 与 Production
资格仍分别待证。
`0.3.0-rc.6` 为已经合入 `main` 的 Evidence Posture 工作区建立独立可升级发布身份：

- `/inbox` 从最近工作样本升级为固定 `as_of/from/to` 的跨系统运行态势，汇总开放 Alert、
  execution 结果、first-class Gap、四个独立覆盖口径与固定窗口序列；不从搜索第一页制造
  全局统计，也不合成没有证据依据的健康分；
- 首页 Runtime Path 和调查下钻只复用来源声明的 Trace Journey 父子关系；时间相邻不生成
  因果边，未关联 execution 的 Alert 不借用其他 Trace；
- MaaS 的吞吐、价格、配额和路由运营指标不在 X-Insight 建立第二事实源。X-Insight 只展示
  调查所需的 Evidence 风险、覆盖、Gap、来源上下文与可复核路径；
- Connector 当前 trust/checkpoint 与固定 Evidence 聚合继续分离，可变交付状态不进入
  Event hash、幂等身份或 Event-linked provenance；
- Posture UI 使用统一排版层级、同一图标家族、渐进下钻和窄屏等价信息结构；新增只读
  `GET /api/v1/posture`，API 版本名仍为 `xinsight.investigation.v1`/`xinsight.access.v1`。

本轮不增加 migration，数据库 schema 仍为 19，手册身份为
`0.3.0-rc.6/manual-r6`。M0 `m0.1`、共享 Foigent `v2.0.2`、Trace Journey、
OpenInference bounded-offline、Receipt、Gap、Provenance、Cursor/Checkpoint 与数据库四角色
语义均不改变。升级新增准确 `0.3.0-rc.5` 来源；同版本替换及任意未列版本仍 fail closed。
两个 Feed 的默认关闭状态和运行合同不变。

## 0.3.0-rc.5 历史发布说明

状态：命名非生产 Target 已完成受监督部署、metadata-only synthetic operation 与相同
operation 幂等重放；External integration 与 Production 未验证。
`0.3.0-rc.5` 完成 metadata-only OpenInference Trace Journey 的产品闭环：

- 新增 `xinsight.trace-journey.v1` 只读 API/界面，按 exact source/org/project/trace 跨 execution
  展示来源声明的 Span 父子路径，并直接回答“谁—做什么—经过系统—终点”；
- 保持每个 OpenInference Span 原有 M0 execution/request/event/hash，不改共享 Foigent v2.0.2
  Schema、vector、mapping、Gap 或 Provenance 语义；
- OpenInference general/Production runtime 继续 fail closed，只允许 exact root-owned、短时、
  绑定 target/build/bundle/operation/scope 的非生产 bounded-offline import；
- 授权和 bundle metadata policy 在 credential/数据库之前验证，拒绝 raw prompt/response 属性，
  且不启用 listener、Feed、Cursor 或 Checkpoint；
- 新增固定 3-span metadata-only synthetic bundle，用于证明跨三个独立 execution 的两条显式
  parent edge、15 个稳定 M0/Gap event identity（3 个主 Event + 12 个 first-class Gap）、durable
  receipt 与 UI 路径，不生成 live traffic。

本轮不增加 migration，数据库 schema 仍为 19。两个既有 Feed 的 wire、凭据、Cursor、
Checkpoint、状态与默认 dormant 边界不变；Local/CI、Target、External integration 与 Production
证据继续分别记录。

## 0.3.0-rc.4 历史发布说明

状态：本地候选，尚未发布、部署或获得 rc.4 Target/External integration/Production 资格。
`0.3.0-rc.4` 修复 authenticated execution search 的 fixed-snapshot 重读断点：

- 首轮搜索返回服务端 `page.as_of` 后，前端继续把该固定快照写入 URL，并用它重读当前页；
- rc.3 后端搜索白名单遗漏 `as_of`，使已成功返回结果的页面在第二次请求收到
  `invalid_argument` 并隐藏表格；
- rc.4 搜索复用既有 UTC/24 小时 `as_of` 校验，并要求显式 `as_of` 与 Cursor 内快照严格
  相等；不一致继续返回 `cursor_mismatch`，不会静默切换快照；
- OpenAPI 与回归测试覆盖首次固定重读、同快照分页和快照/Cursor 冲突；前端固定快照与 URL
  可复核语义不降级。

数据库 schema 仍为 `19`，手册身份为 `0.3.0-rc.4/manual-r4`。本 PATCH 不增加 migration，
不改变数据库四角色、Evidence/M0 identity/hash、Connector、Gap、Provenance、Receipt、
Cursor/Checkpoint 或 Workspace 数据模型。升级接受准确 Phase 1 allowlist、
`0.2.0-rc.3`–`0.2.0-rc.15`、`0.3.0-rc.2` 与 `0.3.0-rc.3`；从 rc.3 升级使用现有四凭据布局，
不触发 Workspace credential bridge。

本轮尚未执行 rc.4 PR CI、exact-main CI、签名发布或 Target 升级。Feed/Worker 必须保持关闭；
Local/CI、Target、External integration 与 Production 证据继续分别记录。

## 0.3.0-rc.3 历史发布说明

状态：命名非生产 Target 的升级、四角色数据库桥接和运行边界已验证；登录后完整 UI 验收被
fixed-snapshot 搜索缺陷阻断；External integration 与 Production 未验证。
`0.3.0-rc.3` 修复 `0.2.0-rc.3`–`0.2.0-rc.15` 与准确 Phase 1 legacy 来源升级到
Workspace 四角色数据库边界时的凭据布局断点：旧版本合法的
reader/importer/admin 三凭据集不再在目标控制器 preflight 中被误拒绝。

- 三凭据兼容只对 manifest 已接受且代码明确列出的旧来源开放；任意其他来源缺少
  `postgres-workspace.dsn` 仍 fail closed，存在但无效的 workspace 文件不得降级为旧格式；
- 三凭据桥接要求数据库中不存在来源不明的 `xinsight_workspace` cluster-global 角色，避免
  目标控制器静默接管解释不清的既有权限状态；
- `upgrade --check` 只读验证旧三角色身份、连接端点、密码隔离、迁移前缀和权限边界，并明确
  报告 `workspace_bridge_required`；不生成密码、不创建角色、不切换 symlink；
- mutating adopt/upgrade 在停服并排空 writer 后生成 256-bit 随机 Workspace 密码；upgrade 还会
  在数据库边界变更前先完成并验证备份。流程原样保留 reader/importer/admin DSN，建立不可变
  四凭据 successor，创建并验证 `xinsight_workspace` 最小权限角色后原子切换 database
  credential symlink；旧凭据集不修改；
- staging、角色扩展、凭据切换、迁移和激活均进入 durable journal。角色扩展一旦开始即成为
  数据库 barrier，只允许 recover/resume，不允许用 abort 伪装回到未变更状态；
- 从 `0.3.0-rc.2` 升级不执行三凭据桥接，只接受既有严格四凭据布局。schema 仍为 19，
  migration、API、Evidence、M0 identity/hash、Connector、Gap、Provenance、Cursor、Checkpoint、
  Receipt 和 A+C UI 字节语义均不改变。

数据库 schema 为 `19`，手册身份为 `0.3.0-rc.3/manual-r3`。升级接受准确 Phase 1
allowlist、`0.2.0-rc.3`–`0.2.0-rc.15` 与 `0.3.0-rc.2`；同版本替换及任意未列版本拒绝。
application rollback 继续 fail closed；需要回到旧应用时使用已验签备份的隔离
restore-to-new-database 流程。

rc.3 已在命名非生产 ECS 从 rc.15 完成受监督升级与四角色数据库凭据迁移；Feed/Worker 保持
关闭。该 Target 事实不替代 rc.4 的独立 CI、签名发行与浏览器验收。

## 0.3.0-rc.2 历史发布说明

状态：本地候选，尚未发布、部署或获得 Target/External integration/Production 资格。
`0.3.0-rc.2` 在已关闭的 `0.3.0-rc.1` 调查工作台上增加 bounded-offline import 的耐久
operation binding 与 completed receipt，同时保持 M0
`m0.1` Event identity/hash、既有 UniRouter/LiteLLM Connector 数据结果、凭据、Cursor 和
Checkpoint 兼容：

- schema 16 增加 Case、Finding、Annotation、Saved View、Evidence/Gap 引用和确定性 sealed
  Evidence Pack；这些产品本地记录只能引用 M0，不能改写或冒充 Evidence；
- schema 17 增加 root 管理的 service identity、短期 bearer token 与最小化访问审计；Agent
  只获明确的 `evidence.read|evidence.cite`，浏览器 Session 与 Agent bearer 不互换；
- schema 18 增加 Gap resolution、来源声明的 Session、evaluation/feedback、Alert-to-Case、
  prompt/app/policy/model/connector version dimensions，以及有证据引用、限制和强制 abstention
  的 Known/Unknown Discovery；
- schema 19 增加只保存 completed 状态的不可变 Connector Operational Receipt；产品本地
  request binding 绑定 operation、bundle、scope 与投影 digest，回执不进入 M0 identity/hash、
  Event-linked provenance、Cursor 或 Checkpoint；
- A+C 调查界面采用 outcome-first Story、来源声明的因果 DAG、独立 Chronology、四轴
  completeness、Evidence drawer、Case dossier、Compare 与 Discovery；相邻时间不再冒充因果，
  `Missing`、`Partial` 与数值零保持不同；
- 产品化界面按调查、分析、信任与访问重组导航；Story 首屏明确展示“发起者—动作—经过系统—
  终点”，1024px Explorer 保留全部字段并转为卡片，390px 使用显式菜单和等价因果关系列表；
  完整覆盖、语义说明、Saved View 与 Finding 编辑按证据风险渐进披露；
- API 继续使用 `xinsight.investigation.v1`/`xinsight.access.v1` 版本名，新增的 Case、Agent、
  Session、Compare、Feedback、Alert、Gap resolution 和 Discovery 路径均有严格输入、权限、
  固定快照和 problem contract；
- 数据库角色改为更窄的列级权限：importer 只能更新明确的运行状态/绑定列，reader 保持只读，
  workspace 与 Agent 写入不能修改 Evidence identity/hash。

产品仓全局 CI pin 采用不可变 Foigent Event Connector Protocol `v2.0.2`、tag commit
`ad8fe7a38c9d7335657630baa8670dd07556b98c`、release digest
`sha256:99f8a541b6d33b0a74d2e4c06aa312c6d6c88c35a4bfb1d4d34b98b4e4e2e8e8`。
既有 UniRouter/LiteLLM 产品本地运行时 binding 继续固定其原不可变 v1 版本，不因全局 pin
迁移而改写。OpenInference 产品本地投影改用 source-neutral v2 binding、
`connector_bounded_offline`/`bounded_offline_import`，不再构造 pull Cursor/Checkpoint，并固定
现有五个 M0 ID/hash 与四个 Gap Event。

OpenInference 的 bounded-offline importer、不可变 operational receipt 与 crash/replay
收敛逻辑已在产品代码中实现。Foigent `v2.0.2` PATCH 仅修复远程 composite Action 的
snapshot portability；`v2.0.1` 已修正的 Reference Manifest 投影 digest 继续与同一 release
component 固定的
`sha256:d5abcabffeb8bb368e5c188b7ed5179b5674092e9421af2c38e548bfdbec7077`
一致。Event/Connector Schema、vectors、reports、产品投影输出和 M0 identity/hash 相对
`v2.0.1` 保持不变。运行时仍明确不可激活，唯一剩余门禁是未获单独运行时激活授权；命令在解析
参数、读取 bundle、加载凭据或连接数据库前继续 fail closed。

数据库 schema 为 `19`，手册身份为 `0.3.0-rc.2/manual-r2`。升级只接受准确 Phase 1
allowlist 与 `0.2.0-rc.3`–`0.2.0-rc.15`；同版本替换及任意未列版本拒绝。旧应用没有声明
兼容 schema 19，因此 application rollback 继续 fail closed；需要回退时使用已验签备份的
隔离 restore-to-new-database 流程。`0.3.0-rc.1` 仅保留为未发布的本地历史证据，不是已授权
升级来源。

本轮没有执行发布、部署、服务启停、凭据、流量、Feed、Cursor/Checkpoint 或 Target 数据库
变更。Local/CI、Target、External integration 与 Production 证据继续分别记录。
该候选的产品/UI 语义由 rc.3 完整继承；rc.3 只修复真实 Target 预检暴露的数据库凭据布局
迁移缺口。最终可安装 artifact 仍必须由准确 commit、payload digest、签名 manifest 和 CI
共同绑定。

## 0.3.0-rc.1 历史发布说明

状态：本地候选，尚未发布、部署或获得 Target/External integration/Production 资格。
`0.3.0-rc.1` 把 X-Insight 从单次 Trace 演示面扩展为证据保全与调查工作台，同时保持 M0
`m0.1` Event identity/hash、既有 UniRouter/LiteLLM Connector 数据结果、凭据、Cursor 和
Checkpoint 兼容：

- schema 16 增加 Case、Finding、Annotation、Saved View、Evidence/Gap 引用和确定性 sealed
  Evidence Pack；这些产品本地记录只能引用 M0，不能改写或冒充 Evidence；
- schema 17 增加 root 管理的 service identity、短期 bearer token 与最小化访问审计；Agent
  只获明确的 `evidence.read|evidence.cite`，浏览器 Session 与 Agent bearer 不互换；
- schema 18 增加 Gap resolution、来源声明的 Session、evaluation/feedback、Alert-to-Case、
  prompt/app/policy/model/connector version dimensions，以及有证据引用、限制和强制 abstention
  的 Known/Unknown Discovery；
- A+C 调查界面采用 outcome-first Story、来源声明的因果 DAG、独立 Chronology、四轴
  completeness、Evidence drawer、Case dossier、Compare 与 Discovery；相邻时间不再冒充因果，
  `Missing`、`Partial` 与数值零保持不同；
- API 继续使用 `xinsight.investigation.v1`/`xinsight.access.v1` 版本名，新增的 Case、Agent、
  Session、Compare、Feedback、Alert、Gap resolution 和 Discovery 路径均有严格输入、权限、
  固定快照和 problem contract；
- 数据库角色改为更窄的列级权限：importer 只能更新明确的运行状态/绑定列，reader 保持只读，
  workspace 与 Agent 写入不能修改 Evidence identity/hash。

产品仓全局 CI pin 采用 Foigent Event Connector Protocol `v2.0.0`、tag commit
`a8ab25ef8c8dd1c12d073ec0cf23c376f5741744`、release digest
`sha256:aa9537a8779758af1227ba42371e75ab5aeafc84d5b73c4cdff35320481aa3f9`。
既有 UniRouter/LiteLLM 产品本地运行时 binding 继续固定其原不可变 v1 版本，不因全局 pin
迁移而改写。OpenInference 产品本地投影改用 source-neutral v2 binding、
`connector_bounded_offline`/`bounded_offline_import`，不再构造 pull Cursor/Checkpoint，并固定
现有五个 M0 ID/hash 与四个 Gap Event。

OpenInference 运行时仍明确不可激活：权威 v2 Manifest 内嵌的 X-Insight 投影 SHA 仍为旧
`b374f…`，而同一发布清单固定的实际文件 SHA 为 `d5abc…`；此外产品尚未实现 durable bounded
import operational receipt，也没有运行时授权。该冲突不通过产品仓私自修改共享工件规避；
production bootstrap 静态门禁继续拒绝 OpenInference adapter import。

数据库 schema 为 `18`，手册身份为 `0.3.0-rc.1/manual-r1`。升级只接受准确 Phase 1
allowlist 与 `0.2.0-rc.3`–`0.2.0-rc.15`；同版本替换及任意未列版本拒绝。旧应用没有声明
兼容 schema 18，因此 application rollback 继续 fail closed；需要回退时使用已验签备份的
隔离 restore-to-new-database 流程。

本轮没有执行发布、部署、服务启停、凭据、流量、Feed、Cursor/Checkpoint 或 Target 数据库
变更。Local/CI、Target、External integration 与 Production 证据继续分别记录。

## 0.2.0-rc.15 历史发布说明

状态：候选版本。rc.15 不增加 migration，不改变 schema 15、API、Access Plane、Evidence
identity/hash、Connector、credential、checkpoint、billing 或 usage 合同。它把 rc.14 现场
临时代理层的空 Projection Gap 归一化永久收回 X-Insight 产品代码，并改进客户演示界面：

- HTTPS 公网入口显示准确的产品 Session 状态，不再误报 `loopback-only`；
- Investigation、Chronology、Proof 在 1280px 客户笔记本宽度使用单列阅读流，避免 inspector
  与主内容互相挤压或造成横向溢出；
- 移除 Phase 编号、内部项目状态和实现过程文案，保留 Evidence、Gap、proof class、
  `metadata-only · no raw` 等客户调查所需边界；
- 空 projection gap 固定归一化为空数组，不依赖 Nginx `sub_filter` 修补。

手册身份为 `0.2.0-rc.15/manual-r15`。升级准确接受 Phase 1 allowlist 与
`rc.3`–`rc.14`；同版本替换仍明确拒绝。Local、CI、Target HTTPS、External integration
与 Production 证据继续分开记录。

## 0.2.0-rc.14 历史发布说明

状态：候选版本。rc.14 在 rc.13 Access Plane 上新增 schema 15 的 UniRouter
`execution.evidence.bundle.v2` 精确产品注册、确定性 Investigation Projection，以及
Investigation/Chronology/Proof 三种客户演示视图。它只接受同时固定 profile 与 bundle schema
digest 的 metadata-only Evidence；未知字段、digest 漂移、完整性矛盾、缺失 close marker、
跨 Trace/Call hash 复用和虚假阶段覆盖均 fail closed。UI 不用 LLM 补写因果关系，也不根据
状态码或 route selection 推断最终成功。

rc.14 还增加一个明确 opt-in 的同机 TLS 终止合同。服务仍只监听 `127.0.0.1:8080`；只有
`XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback` 与一个规范化
`XINSIGHT_EXTERNAL_ORIGIN=https://HOST_OR_IP` 同时存在时，后端才接受来自 loopback、具有唯一
`X-Forwarded-Proto: https`、准确 `X-Forwarded-Host`、Host 与 Origin 完全一致且不含通用
`Forwarded` header 的认证状态变更。未配置时仍严格只承认 direct TLS。发布包提供 Nginx/IP
模板，但不自动申请证书、开放安全组、创建账户或建立公共入口。

schema 15 只增加 Evidence V2 产品注册；它不改变既有 M0 Evidence identity/hash、两个 V1
Connector cursor/checkpoint 或 reader/importer/admin 权限边界。手册身份为
`0.2.0-rc.14/manual-r14`，准确接受 Phase 1 allowlist 与 `rc.3`–`rc.13` 升级。Local、CI、
Target HTTPS、External integration 与 Production 证据继续分开记录。

## 0.2.0-rc.13 历史发布说明

状态：候选版本。rc.13 在既有 schema 14、`m0.1` Evidence、两个精确 Connector 产品注册和
reader-only Web 数据库身份上新增 `xinsight.access.v1` Access Plane。它不增加 migration，
不改变 completed-execution/LiteLLM wire、cursor、idempotency、provenance、M0 identity/hash
或 EvidenceStore writer，也不把 importer/admin/Source credential 交给 Web 服务。

本地账户 authority 是 root-provisioned 的 `/etc/x-insight/access-accounts.json` systemd
credential。格式是严格 `xinsight.local-accounts.v1`：稳定 opaque subject、规范化 username、
`investigator|operator` role、显式 boolean `disabled` 和固定参数 Argon2id hash。duplicate/
unknown/missing/null/trailing JSON、重复 subject/username、不合规文件 identity/mode/link 或空
账户集都在启动和生命周期 preflight fail closed。没有默认可用账户；产品没有 Web 建号、
password reset、改角色或写回 credential 能力。root 可使用
`xinsightctl access generate-subject|hash-password|validate` 在目标主机离线准备并只读验证，
password 不接受 argv/env/stdin/pipe。

`investigator` 只有 `evidence.read`；`operator` 另有
`access.sessions.read|access.sessions.revoke`。API 提供 login、current session、logout、
operator Session list/revoke。Session token 为 256-bit opaque 值，服务端只保存 SHA-256
digest；cookie 固定为 host-only `__Host-xinsight_session`，带
`Secure; HttpOnly; SameSite=Strict; Path=/`。idle/absolute timeout 为 30 分钟/8 小时，
每 subject/单进程最多 5/500 个 Session，重启、logout 或 revoke 立即失效。所有认证状态变更
要求 direct same-origin HTTPS 与 Session-bound CSRF。登录失败不枚举账户；默认每 username
15 分钟 5 次、全进程每分钟 50 次失败，并单独限制最多 4 个并发 Argon2 verification。

匿名调查 UI 固定返回 no-store `303 Location: /login`，匿名调查 API 固定返回
`401 authentication_required` 的六字段 `application/problem+json`。`/readyz` 新增
reader-only 有界代表性查询，准确覆盖 `source_systems`、`evidence_events`、`capture_gaps`、
`import_provenance`、`import_provenance_events` 且不返回数据；任一表的 SELECT grant 漂移都会
令 readiness 失败。install/adopt/upgrade 及中断后的 resume/abort 会在任何受管 mutation 前
重验 access credential；历史 rc.12 及更早 release 的匿名读取 activation proof 保持其原合同，
不被 rc.13 的 303/401 合同追溯改写。

前端构建保留已追踪历史哈希资产，以避免构建隐式删除或依赖构建后恢复；当前
`index.html` 只引用 rc.13 新哈希，运行时只允许这些实际引用资产，历史文件即使存在于嵌入源中
也返回 404。Access audit 只记录 event/time、已知 stable subject、role、permission、result
和 correlation，不含 unknown username、IP、password/hash、Session/CSRF、DSN/key、
request body 或 Evidence 内容。

当前 server/unit 仍是 loopback HTTP，没有 TLS listener、certificate、hostname 或可信代理
contract。由于 login/logout/revoke 在后端不是 direct TLS 时必须 fail closed，HTTP SSH
tunnel、一般私网、代理登录页或 localhost HTTP 都不是可部署登录路径。rc.13 qualification
只使用一个全 disabled synthetic CI account 验证 credential/lifecycle/匿名边界，不生成可登录
默认账户。Local 测试证据、PR/exact-main CI、Target HTTPS authenticated flow、External
integration 和 Production 状态分别记录；后四者在本说明中均为 pending。手册身份为
`0.2.0-rc.13/manual-r13`，准确接受 Phase 1 allowlist 与 `rc.3`–`rc.12` 升级。

## 0.2.0-rc.12 历史发布说明

状态：候选版本。rc.12 以 additive/reversible schema 14 新增第二个产品注册
`foigent.litellm.spend-logs` Reference Connector，并把共享协议 authority 精确固定为
Foigent Event Connector Protocol `v1.1.2`、commit
`fd82aeca9877bea8064b25c365a3cdcdd0bb3d16` 和不可变 release digest
`sha256:0b414d5d59e5a7ca5c05505cb58d7992c544537da1c84a57256ec205618d9cd1`。该
协议版本只把 LiteLLM v1.93.0 的精确字符串 `cache_hit="None"` 兼容映射为
`cache_status="unknown"`；不接受大小写变体或其他字符串，不扩大 Source allowlist。既有
completed-execution Connector 的版本、manifest、projection、M0 identity/hash、
idempotency、provenance 和 checkpoint 均不改变；其产品本地 runtime binding 继续固定在
已经采用并验证的 Protocol `v1.0.0`。全局 CI authority 与某个产品本地 Connector
runtime binding 是两个独立版本边界，LiteLLM-only erratum 不冒充 completed-execution
运行时变更。

schema 14 只把 `import_provenance.integration_mode` 的受约束集合扩展为
`connector_pull`；LiteLLM 使用该 source-neutral mode，既有 completed-execution
provenance identity 继续使用 `completed_execution_pull`。down 在存在任何
`connector_pull` 行时 fail closed，不删除证据或静默改写 provenance。

LiteLLM 只通过独立的 `xinsight-importer litellm pull-spend-logs` worker 读取严格私有
HTTPS `/spend/logs/v2`。API key 必须属于非管理员团队成员：团队准确允许
`/spend/logs`，该独立 observer key 的 `allowed_routes` 准确只有
`/spend/logs/v2`；LiteLLM master key 只允许初始化/清理，不得用于 X、模型流量或日常
spend-log 读取。key 只从独立 systemd credential 读取；raw response 只在进程内有界解析。
固定 LiteLLM 配置显式设置 `turn_off_message_logging: true` 与
`log_raw_request_response: false`；真实组件 CI 使用唯一 request/response sentinels，并
分别证明 LiteLLM Source DB、容器日志、`/spend/logs/v2` 原始响应和 X Evidence 均不含
这些值及三把生成的 raw virtual key。该 CI 还以独立目标团队 traffic key 产生成功/失败，证明 observer LLM route
被拒绝，并证明第二个未授权团队的一条记录不出现在 observer 响应或 X Evidence。
组件模型在 `model_info` 中固定 input/output token 单价为
`0.000001`/`0.000002` USD；成功样本固定为 3/2/5 tokens 和
`0.000007` USD，受控失败固定为 0/0/0 tokens 和 0 USD，并在 Source HTTP 记录与
X M0 `usage`/source-reported money 两层逐值断言。
prompt、response、API key、user、IP、metadata 等字段仍在 Source projection前 fail closed
丢弃。Reference V1 只接受 USD source-reported spend，将 succeeded/failed
投影为现有 M0 `model.request.succeeded`/`model.request.failed`，并为 billing authority、
business outcome、downstream side effects、routing decision 生成四个确定性 capture
gaps。除 schema 14 对 provenance mode 的窄扩展外，M0 evidence schema、事件写入事务、
Read API 和 reader 服务不变。

durable checkpoint 复用 `adapter_import_states`，但以 LiteLLM 固定
source-system/adapter/cursor identity 与 tenant/org/project scope 独立绑定。每次窗口固定
60 秒 start-time eligibility delay：`F=floor_microsecond(T-60s)`，实际上游 inclusive
`end_date` 为 `Q=floor_second(F)`；`Q < startTime <= F` 的行延后到重叠轮次，
row `endTime` 不参与 HTTP fence 或 checkpoint。整窗最多 20 页/2000 行/8 MiB，并按
`(startTime, request_id UTF-8 bytes)` 确定性排序；所有 item inserted 或
duplicate-noop 后才推进 checkpoint。首次 activation 与 F 恰好相等时是零 HTTP、零写入的
合法空轮次。共享协议保留的 `finality=finalized_after_delay` 是不可变 legacy 标签，仅表示
start-time eligibility；X 不把它投影为 M0 terminal finality。worker/config/key/service 与
UniRouter feed 完全隔离，升级、回滚和恢复分别保留两个 worker 的 active/enabled 状态。

Reference V1 的覆盖边界是 Development 验证而非 Production-lossless 承诺：若 LiteLLM 在
记录已超过 60 秒后才把 `startTime` 早于已提交 checkpoint 的晚到行暴露出来，该行不在
本版本可恢复覆盖内。固定的 SourceSystemID/AdapterManifestID 也只允许一个 LiteLLM
instance profile；多实例 registry、通用插件管理器、HA、CDC、回补和 Phase 4 运行时不在
rc.12 范围。手册身份为 `0.2.0-rc.12/manual-r12`，准确接受 `rc.3`–`rc.11` 升级。
Local/CI、Target、External integration 与 Production 证据继续分别记录。

## 0.2.0-rc.11 历史发布说明

rc.11 在 schema 13 和既有 `unirouter.completed-execution.v1` pull 上采用首个
Foigent completed-execution Reference Connector 产品内兼容桥。映射模式
`legacy|shadow|neutral_canary` 维持单写者并通过现有 M0/Gap/provenance 精确兼容门禁；
它不创建第二 DB/cursor，也不发布通用第三方 Connector SDK。

## 0.2.0-rc.10 历史发布说明

状态：候选版本。rc.10 不改变 schema 13、completed-execution contract、固定 HTTPS
route、credential/config、durable cursor/high-watermark、EvidenceStore persistence 或
数据库 migration set。它只修正 live receiver 对同一 terminal instant 的跨表示绑定：
outer PostgreSQL `TIMESTAMPTZ` 与 immutable inner RFC3339 timestamp 统一按 UTC
PostgreSQL 微秒精度比较；残差严格小于 1µs 兼容，等于或大于 1µs 仍 fail closed。inner
bytes 与 SHA-256 不得重写。

手册身份为 `0.2.0-rc.10/manual-r10`。升级准确接受 `rc.3`–`rc.9`，但同版本仍明确拒绝；
rc.9→rc.10 必须由已验签目标 payload 控制器执行。升级与 crash-resume 资格证明既有
feed config、active credential、nonsecret operation state、数据库 credential set、
durable cursor marker 均保留；worker 的 active 与 enabled 两项状态分别按原值保留
（包括 active+disabled），且不创建第二 credential、
不重置数据库/cursor、不手工导入或发送替代流量。

## 0.2.0-rc.9 历史发布说明

状态：候选版本。rc.9 在 schema 13 和既有 completed-execution pull V1 上增加
receiver-generated feed credential 生命周期；不改变
`unirouter.completed-execution.v1`、固定 HTTPS route、HTTP source、DTO、
activation/cursor/high-watermark、EvidenceStore persistence 或数据库 migration set。

`xinsightctl feed-credential prepare` 在准确 X-Insight target 上用 `crypto/rand`
生成 32-byte token，原始值严格为 `ur-xf1-` 加 64 个小写十六进制字符，并始终留在
root-only target 文件。它只输出 Frozen Registration Bundle V1 的六个严格字符串字段：
schema、token format、verifier scheme、首 20 字符 prefix、domain-separated SHA-256
verifier 和独立 16-byte operation ID；raw token 不进入 stdout/stderr、argv、env、
浏览器、共享状态或 UniRouter。

staged 文件固定为 `/etc/x-insight/unirouter-feed.key.next`，必须是 O_EXCL 创建、
非 symlink、root:root `0600`、regular、单 link，并由非 secret durable operation state
绑定 exact operation/verifier。`status` 只报告公开 bundle、文件 metadata 和恢复动作；
`abort` 只删除准确 pending staged 文件；`activate` 在替换前使用 `.next` token 与
准确 feed runtime config 执行 bounded no-proxy/no-redirect 认证预检，只接受严格
`completed_execution_feed.connection_paused`、`retryable=true` 的 `503` envelope。
`401`/`403`/`200`/invalid envelope/transport failure 保留旧 live credential、`.next` 和
prepared state。预检通过后才在 exact binding 下原子替换 live credential，跨 crash
可恢复，并且只在 activation barrier 之后执行
`systemctl restart xinsight-feed.service`。rotation prepare 期间旧 credential 保持不变；
permanent 401 不推进 cursor，restart 后仍从同一 durable cursor 收敛。

数据库 schema 仍为 `13`，手册为 `0.2.0-rc.9/manual-r9`。升级接受准确
`rc.3`–`rc.8`。Signed V2/Fresh Install/clean legacy transition 资格必须包含新的严格
registration schema、CLI absent-status proof，以及 feed unit loaded/inactive/disabled、
live/next key 与 credential operation state 均 absent、worker process absent 的 dormant
证明。release tooling 的安装和资格不会执行 `prepare`，不会生成运行时 feed principal，
不会启动 worker 或发送 feed traffic。

## 0.2.0-rc.8 历史发布说明

状态：候选版本。rc.8 是 schema 13 上的跨版本主机生命周期修复，不改变
`unirouter.completed-execution.v1`、持久 cursor/idempotency、reader/writer 边界或数据库
migration set。

rc.7 在 Development 从准确 rc.6 升级时暴露一个真实缺陷：正常升级手册要求运行当前安装的
`xinsightctl`，但 rc.6 控制器的 runtime file inventory 固定在 rc.6 发布时，因此即使 rc.7
数据库迁移、应用激活、readiness 与 search 都成功，也不会安装 rc.7 新增的
`xinsight-feed.service`。目标主机已用与已激活签名 payload 逐字节一致的 unit 做受控 dormant
修复；worker 未配置、未启动、未轮询。

rc.8 将已验签并解出的目标 payload 内 `xinsightctl` 定义为跨版本 upgrade 及同一 operation
recover/resume/abort 的唯一控制器。旧 current 控制器不得驱动 next release；这保证 next
release 新增的 systemd/runtime inventory 由理解该 inventory 的控制器完整安装。生成手册会
fail closed 拒绝 current-controller upgrade 命令；clean Ubuntu legacy
SIGKILL/recovery 资格必须额外证明 feed unit 与目标 payload 一致、root `0644`、loaded、
inactive、disabled，且 feed env/key/process 均不存在。

数据库 schema 仍为 `13`，手册为 `0.2.0-rc.8/manual-r8`。升级接受准确
`rc.3`–`rc.7`；没有新 migration、feed principal、外部 traffic、provider attempt、
customer/Production 数据或 worker activation。Local、PR CI、exact-main Signed V2/Fresh
Install、Target reconciliation、External integration 和 Production 仍分别记录。

## 0.2.0-rc.7 历史发布说明

状态：历史候选版本。rc.7 实现生产意图的 UniRouter completed-execution pull
V1，但当前仅有 Local 实现与测试证据；PR CI、exact-main CI、Target、
External integration 和 Production 均须分别记录，不能由本说明推导。

相对不可变的 `0.2.0-rc.6`，本候选新增：

- 严格绑定 `unirouter.completed-execution.v1` schema 与 golden fixtures 的
  metadata-only byte receiver；
- 固定 endpoint、无环境代理、不跟随 redirect、受限 timeout/body、feed-only
  Bearer service key 的私有 HTTP source；默认只允许 HTTPS，private-tunnel HTTP
  必须由显式且默认关闭的运行设置启用；
- `adapter_import_states` 中持久 activation/source-instance/cursor/high-watermark/
  worker status 与累计计数；
- 逐 item 复用现有 EvidenceStore 幂等事务，整页全部 inserted 或
  duplicate-noop 后才单独推进 cursor 的 at-least-once 收敛；逐 item provenance
  不提前声称 page-next cursor，空页也不能在 activation 后推进 cursor；
- 独立 writer worker `xinsight-importer unirouter pull-feed`，保持
  `xinsight-server` reader-only、loopback-only；
- live payload SHA provenance、固定 `api_import` adapter identity、显式
  capture gaps、结构化安全日志，以及升级/回滚/恢复时的 worker stop/drain/restart。

数据库 schema 为 `13`，手册为 `0.2.0-rc.7/manual-r7`。升级接受准确
`rc.3`–`rc.6`；迁移 `000012`/`000013` 为新增状态/provenance 能力。既有
legacy/rc.6 应用都未声明支持 schema 13，因此 rc.7 manifest 不声明 application-pointer
rollback 兼容；需要回到旧应用时必须走显式隔离数据库恢复，不能扩大旧 manifest 的
兼容范围。

本候选不包含 pre-billing rejection、late revision、public POST collector/push、
direct UniRouter DB access、CDC、customer data、Production、HA/leader、Binding，
也不把历史 approval-manifest/artifact sidecar 语义带入 live payload。

## 0.2.0-rc.6 历史发布说明

状态：候选版本。UniRouter snapshot v2 exact micro-CNY sidecar 的代码承载合并
`0462e698775488d86e1263debf64990239464340` 已完成 Local、PR CI `29645951982` 和
exact-main CI `29646094307` 验证。每个实际交付的同 commit release qualification 与
owner-supervised workflow 身份以其 delivery metadata 为准，不由这份静态说明自证。
rc.6 Target、External integration 与 Production 尚未验证；准确 rc.5 的命名 Target 结果是
有效历史证据，但不自动提升 rc.6 状态。

相对不可变的 `0.2.0-rc.5`，本候选增加严格的 UniRouter snapshot v1 cents / v2
micro-CNY 兼容边界、精确字符串持久化/API/UI 展示、独立 v1/v2 mapper identity、
单调 source registry 和首次写入防污染校验。当前跨项目绑定路径仍是 operator-mediated
sidecar；整个 live completed-execution feed 切片已从第一正式版本延后。rc.6 明确接受
rc.5 作为 upgrade source，避免同版本新 commit 被现有 rc.5 主机误判为已经激活。

rc.6 继承 rc.5 对 `0.2.0-rc.4` 命名 Target 预检缺陷的修复。required payload
清单绑定签名 manifest 的准确产品版本：只有历史
`0.2.0-rc.3` 可以不含后来才加入的 `deploy/99-xinsight-managed-launcher.conf`，当前和未知
版本仍必须包含该文件并 fail closed。回归测试覆盖 rc.3 的 pack/verify/extract，以及 rc.5、
rc.6 和未知版本的缺失拒绝。

rc.4 的签名交付在本地、PR CI、main CI、owner-supervised workflow 和目标主机交付验签均
通过；目标主机从 rc.3 回滚至 Phase 1 也保持服务、schema ledger、PostgreSQL 容器与规范化
产品证据不变。但 rc.4 `upgrade --check` 验证 previous rc.3 时误用了新清单，在创建 operation、
备份、迁移和激活之前安全停止。现场随后恢复到稳定 Phase 1。因此 rc.4 不获得 Target 资格，
rc.5 后续已经完成 Phase 1→rc.5→Phase 1 application rollback→rc.5 的完整命名
Target 证明；rc.6 如进入 Target，仍须使用自己的准确 artifact 重新资格验证。

一次性签名的跨版本升级还明确采用临时双公开公钥 transition trust store：它只包含 current/
previous 与 next 两次受监督交付的公开 key，必须保留到 rollback window 关闭；私钥不进入目标
主机，客户也无需管理。旧 release 正式退出回滚窗口后才能以单一明确文件替换收窄信任，并
保留前后 SHA-256 清理证据。

本候选产品线继续提供：

- bounded execution search、execution detail、evidence timeline；
- selected-node、source/provenance 和 capture-gap inspection；
- direct、derived、declared、suppressed、gap、unknown、not_applicable 标签；
- loopback-only 简体中文同源 UI；
- 版本化、Ed25519 签名的单机 release envelope；
- 可恢复的 install/adopt/upgrade/application rollback/隔离 restore 主机工具；
- 与 release 身份绑定的简体中文离线产品手册；
- checksum/dirty-state migration ledger 和共享/独占维护锁；以及
- 面向 Ubuntu 22.04 x86_64、systemd、PostgreSQL 16 Compose 的受控部署基础。

相对 `0.2.0-rc.3`，本候选修正了命名 Target 实际 application rollback 暴露的两项
P1 运行桥接缺陷：Phase 1 的 `10-release-layout.conf` 会覆盖新主 unit 的 managed
launcher，而 rc.2 失败尝试留下的既有 `/opt/x-insight/control` `0700` 权限不会被
rc.3 的“仅新建目录”归一化。rc.4 保留并核对准确已知的 Phase 1 drop-in，同时安装
更高优先级、签名 payload 内的 `99-xinsight-managed-launcher.conf`；任何未知 active
`.conf` 都在 preflight 阶段被拒绝。activation proof 还会读取 systemd 的有效
`ExecStart`，必须指向 `/opt/x-insight/control/xinsight-launcher`。产品自有目录无论
新建还是已存在都会恢复为准确合同权限。

rc.3 在命名 Target 的升级、UI/API、重启和产品证据读取均通过；实际回滚在旧应用
readiness 阶段暴露上述问题。失败 journal 被保留并按原 operation ID 恢复，期间未执行
down migration、未重建 PostgreSQL、未改变规范化产品证据；随后重新升级回 rc.3，
schema ledger 的最新时间戳不变。rc.3 的 Target 产品证明仍是有效历史证据，但不能替代
rc.4 对回滚路径的重新资格验证。

相对 `0.2.0-rc.2`，本候选修正了目标机限制性 `umask 077` 暴露的 P1 权限问题：
签名 payload 解包会显式恢复 release 根目录、子目录、普通文件和 `bin/` 可执行文件
的 `0755/0644` 合同权限；新建 lifecycle 目录和复制到 systemd/Compose 运行位置的
文件也显式设置合同权限，不再依赖执行账户的 umask。回归测试在进程级
`umask 077` 下验证完整 install staging 和运行时文件权限。

`0.2.0-rc.2` 已通过 exact-main CI 与 owner-supervised 发布，但在命名 Target 的
`stage_release` 阶段因 release 树继承 `0700/0600` 而失败。失败发生在数据库迁移、
备份和 release 激活之前；受控 `abort` 已恢复旧应用服务，保留失败 journal、receipt
和 release 目录。因此 `0.2.0-rc.2` 不获得 Target 状态，也不得重新解释为可发布
Target 工件。

本候选继续包含 `0.2.0-rc.2` 的 PostgreSQL 部署形态修正：受管 Compose 与既有
Phase 1 及项目 Target runbook 统一使用 `/data/x-insight/postgres`、根
`PGDATA=/var/lib/postgresql/data` 和固定容器名 `xinsight-postgres`，避免未来重建时
误初始化空集群；支持形态 CI 从真实旧账号/secret-file/数据目录形态执行 legacy
bridge、adopt 与升级。

第一客户采用 User+Codex `owner_supervised_trial`：单独的手动 workflow 只接受
准确当前 `main` 和同 commit 成功的 push CI，使用一次性 Ed25519 key，并在
组装严格七文件 delivery 前删除私钥。Codex 将 workflow/CI、commit、artifact、
bootstrap、trust store 和 SHA-256 一并核对后才安装。普通 PR/push CI 的临时
key artifact 仍只用于资格测试，明确不可发布或安装。该试点不是持久发行者
身份；第二客户、第三方安装或自助升级前需要正式发布权威。

不包含 live UniRouter feed、自动 receipt transport、raw prompt/response、
多租户、公网、HA/DR 或 Production verified 声明。当前跨项目绑定路径仍是
operator-mediated sidecar。
