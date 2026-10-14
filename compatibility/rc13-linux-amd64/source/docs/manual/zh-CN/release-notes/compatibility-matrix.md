# 兼容矩阵与候选发布说明

## 0.3.0-rc.13

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 23（单一 additive function EXECUTE privilege hardening migration） |
| Config schema | `xinsight.config.v1`；独立 trusted TLS proxy env；受管 identity/target 原字节保留 |
| API | `xinsight.investigation.v1`、`xinsight.access.v1`；响应与 readiness fail-closed 校验强化 |
| Evidence | `m0.1`；不放宽 identity/hash/idempotency/Gap/Provenance 校验 |
| Shared protocol | Foigent `v2.0.2` immutable product pin；completed-event wire 不变 |
| Schema contract | `xinsight.schema-contract.v2`；schema 23 function owner/ACL/semantic properties 纳入权威摘要 |
| Receipt | `xinsight.import-receipt.v1`、`foigent.operational-receipt.v2`、`xinsight.admin-credential-rotation-receipt.v1`、`xinsight.unirouter-campaign-package-import-receipt.v1` |
| Manual | `0.3.0-rc.13/manual-r13` |
| Upgrade source | exact allowlisted Phase 1 artifact、准确 `0.2.0-rc.3`–`0.2.0-rc.15`、`0.3.0-rc.2`–`0.3.0-rc.12`；同版本和任意未列版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | Local gate 已验证；准确 rc.13 CI、Owner-Supervised delivery、Target、External integration、Production 仍分别待证 |

rc.13 收敛发布就绪技术债：PostgreSQL function privilege convergence 与 schema-contract 语义覆盖、
API response/readiness fail-closed 校验、Go/前端覆盖率和 fuzz、确定性 Web 资产，以及受认证的真实
HTTPS/PG16 E2E。它只增加 schema 23 权限收敛 migration，不增加协议、receipt、Connector traffic
或新的 UI 语义；既有 UI、
调查逻辑、Known/Unknown Discovery、全景图与 loopback TLS terminator 继续作为同一受支持产品路径。

## 0.3.0-rc.12 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 22（migration 21/22 增加 Connector installation 与三阶段 control contract） |
| Config schema | `xinsight.config.v1`；独立 trusted TLS proxy env；受管 generate-once identity 与 preserve target env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1`；固定 Connector HTTPS redeem/poll/result 管理端点 |
| Evidence | `m0.1`；不放宽 identity/hash/idempotency/Gap/Provenance 校验 |
| Shared protocol | Foigent `v2.0.2` immutable product pin；completed-event wire 不变 |
| Schema contract | `xinsight.schema-contract.v2`；database schema 22 的生成权威 |
| Receipt | `xinsight.import-receipt.v1`、`foigent.operational-receipt.v2`、`xinsight.admin-credential-rotation-receipt.v1`、`xinsight.unirouter-campaign-package-import-receipt.v1` |
| Manual | `0.3.0-rc.12/manual-r12` |
| Upgrade source | exact allowlisted Phase 1 artifact、准确 `0.2.0-rc.3`–`0.2.0-rc.15`、`0.3.0-rc.2`–`0.3.0-rc.11`；同版本和任意未列版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | exact main `c23a31c9b7a7658765ff7e069a6e0ba2d4f6308d` 已通过 non-publishable CI qualification（CI `31525193516`；Protocol `31525193509`）；尚无该 commit 的 Owner-Supervised delivery，Target、External integration、Production 仍分别待证 |

rc.12 增加 root-owned `xinsight-connector-controller.service`，在 Web 前启动并只接受固定 Unix
socket typed actions。Web 在公开 HTTPS result 完成 durable commit 后唤醒 controller；wake 丢失由
exact result replay 收敛。三阶段 activation、normal-install epoch、managed config、dormant credential
与 worker-last 复用现有 schema22/store/hostctl 生命周期；普通同步不依赖 Neutral/Assurance/DATA_RESET，
不增加第二套 journal、receipt、协议或 UI 原语。

受管 installer 现在在独立 `/etc/x-insight/xinsight-connector-id.env` 创建 root-only stable
deployment ID，root CLI 通过 `/etc/x-insight/xinsight-connector.env` 提供唯一支持的 UniRouter HTTPS
install origin 设置路径；systemd 同时加载 identity、target 与 trusted TLS external origin。
upgrade/rollback/re-upgrade 原字节保留 identity/target，target allowlist 无法覆盖 ID，clean-environment
root `ExecStartPre` 在 Web 前校验 owner/mode/key/full-target TLS 前件。仅有 ID 或 ID+TLS、尚未填写
target 时 Connector 保持 disabled，worker 不启动且不产生流量。

## 0.3.0-rc.11 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 20（与 rc.10 相同；无 migration） |
| Config schema | `xinsight.config.v1`；可选独立 trusted TLS proxy env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1`；空基线返回显式空 collection |
| Evidence | `m0.1`；不放宽 identity/hash/idempotency/Gap/Provenance 校验 |
| Shared protocol | Foigent `v2.0.2` immutable product pin；产品仓不复制可编辑协议源 |
| Schema contract | `xinsight.schema-contract.v2`；生成字节与 rc.10 相同 |
| Receipt | `xinsight.import-receipt.v1`、`foigent.operational-receipt.v2`、`xinsight.admin-credential-rotation-receipt.v1`、`xinsight.unirouter-campaign-package-import-receipt.v1` |
| Manual | `0.3.0-rc.11/manual-r11` |
| Upgrade source | exact allowlisted Phase 1 artifact、准确 `0.2.0-rc.3`–`0.2.0-rc.15`、`0.3.0-rc.2`–`0.3.0-rc.10`；同版本和任意未列版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | signed campaign-package consumer 为 Local 候选；准确 rc.11 CI、Target、External integration、Production 仍分别待证 |

rc.11 增加受约束的离线 UniRouter signed campaign-package import。既有 X 签名 manifest 仍是唯一
release authority；product/verifier receipt 从该 manifest 的 importer 文件身份、schema 20、manual-r11、
M0 schema 与 receipt list 确定性派生，不增加签名域或 hash cycle。operator 输入仅为有界本地 artifact
locator 与稳定 operation ID；X release/runtime、Neutral N1、Source trust、producer package 和 member
validation 必须在加载 importer DSN 前全部闭合。外层 package completeness 与 partial member
`runtime_close_marker_unavailable` 独立保留。persistence/replay 复用既有 schema-20 bounded-offline
operation store 与 receipt；没有 migration、第二套 journal、worker、Cursor/Checkpoint 或 traffic 能力。

## 0.3.0-rc.10 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 20（与 rc.9 相同；无 migration） |
| Config schema | `xinsight.config.v1`；可选独立 trusted TLS proxy env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1`；空基线返回显式空 collection |
| Evidence | `m0.1`；不放宽 identity/hash/idempotency/Gap/Provenance 校验 |
| Shared protocol | Foigent `v2.0.2` immutable product pin；产品仓不复制可编辑协议源 |
| Schema contract | `xinsight.schema-contract.v2`；生成字节与 rc.9 相同 |
| Receipt | `xinsight.import-receipt.v1`、`foigent.operational-receipt.v2`、`xinsight.admin-credential-rotation-receipt.v1` |
| Manual | `0.3.0-rc.10/manual-r10` |
| Upgrade source | exact allowlisted Phase 1 artifact、准确 `0.2.0-rc.3`–`0.2.0-rc.15`、`0.3.0-rc.2`–`0.3.0-rc.9`；同版本和任意未列版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | admin credential rotation 为 Local 候选；准确 rc.10 CI、Target、External integration、Production 仍分别待证 |

rc.10 新增 root-only `xinsightctl database-admin-credential preview|apply`。它以版本化 request、preview、
journal 和 receipt 绑定准确 release/schema/config/protocol、四角色 credential-set metadata/digest、零并发
admin session、34 表/migration/connector/checkpoint/account/config 指纹和 dormant worker 状态。successor
先在同父目录唯一 staging 中完整 sync/验证，再原子 rename；最终目录只按不可变完成态读取。产品在同一
predecessor transaction 中紧邻 verifier 更新前再次确认没有其他 admin session，变更后 lifecycle 为
resume-only，并证明 successor fresh connection 成功、predecessor fresh connection 得到认证拒绝。
reader/workspace/importer DSN 原字节、demo.operator verifier、产品数据、schema ledger、connector/checkpoint、
协议和服务 PID 不变；两个 Feed worker 保持 inactive/disabled，restart/product write/poll/traffic 均为零。

## 0.3.0-rc.9 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 20（与 rc.8 相同；无 migration） |
| Config schema | `xinsight.config.v1`；可选独立 trusted TLS proxy env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1`；空基线返回显式空 collection |
| Evidence | `m0.1`；不放宽 identity/hash/idempotency/Gap/Provenance 校验 |
| Shared protocol | Foigent `v2.0.2` immutable product pin；产品仓不复制可编辑协议源 |
| Schema contract | `xinsight.schema-contract.v2`；named semantic digest 与非权威 column layout digest 分离 |
| Receipt | `xinsight.import-receipt.v1`、`foigent.operational-receipt.v2`；权限边界不变 |
| Manual | `0.3.0-rc.9/manual-r9` |
| Upgrade source | exact allowlisted Phase 1 artifact、准确 `0.2.0-rc.3`–`0.2.0-rc.15`、`0.3.0-rc.2`–`0.3.0-rc.8`；同版本和任意未列版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | schema contract canonicalization 为 Local 候选；准确 rc.9 CI、Target、External integration、Production 仍分别待证 |

rc.9 从 PostgreSQL 16 完整 release migrations 唯一生成 schema contract。列身份固定为
`namespace/table/column` 并按名称排序，物理 ordinal 只进入非权威 layout digest；类型/Domain、nullable、
default、identity/generated、collation 和 constraints 仍受保护。fresh、支持的升级结果、签名 payload、
binary readback 与现场 introspection 必须收敛；真实语义或未知对象继续返回脱敏对象级原因并失败关闭。
本轮不增加 migration，也不改变 DATA_RESET/BOOTSTRAP/TRAFFIC、protocol、Source/Cursor 或 credential 语义。

## 0.3.0-rc.8 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 20（与 rc.7 相同；fresh database 只运行 release migrations） |
| Config schema | `xinsight.config.v1`；可选独立 trusted TLS proxy env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1`；空基线返回显式空 collection |
| Evidence | `m0.1`；fresh baseline 不放宽 identity/hash/idempotency/Gap/Provenance 校验 |
| Shared protocol | Foigent `v2.0.2` immutable product pin；产品仓不复制可编辑协议源 |
| Receipt | `xinsight.import-receipt.v1`、`foigent.operational-receipt.v2`；clean-baseline 使用 product-local durable operation receipt |
| Manual | `0.3.0-rc.8/manual-r8` |
| Upgrade source | exact allowlisted Phase 1 artifact、准确 `0.2.0-rc.3`–`0.2.0-rc.15`、`0.3.0-rc.2`–`0.3.0-rc.7`；同版本和任意未列版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | clean baseline 与 Web UI asset parity 为 Local 候选；准确 rc.8 CI、Target、External integration、Production 仍分别待证 |

rc.8 增加 exact generated Web UI asset manifest。前端生成、Go embed、签名 binary readback 与实际
served index/JS/CSS 必须逐字节同一；旧 retained asset 不可被服务。复核确认基线 main、本 PR head
与先前观察的 installed runtime JS 原本已经共享 Git blob `4c6afd312415a7aaef4d79aa9348a295f055a080`
及 SHA-256 `bb09119c65eb7c89b9db46d2f76faddfed3c85ce5ade216487faec6bad21cba1`；
本轮是预防性 release-integrity hardening，不是修复既有 byte mismatch。先前 HOLD 中的该子前提
据此撤销，其他 HOLD 条件不在本候选发布说明中重裁。

`xinsightctl clean-baseline data-reset-preview|data-reset-apply` 是 root-only、显式 DATA_RESET authority
下的 fresh-database cutover，不是日常 reset。preview 不接受 source/cursor/epoch input；apply 只创建并
切换严格空库，证明 `adapter_import_states`、epoch operation/history 及历史表为零，同时 crash-safe
退役 UniRouter active/staged/operation-state 与 LiteLLM credential/environment 五个 campaign-state
文件且不读取内容、不生成 replacement。LiteLLM systemd unit 保留并保持 inactive+disabled；当前
Paths 合同没有 OpenInference 等价持久文件，不新增虚构路径。独立的
`checkpoint-bootstrap-preview|checkpoint-bootstrap-apply` 必须消费 completed DATA_RESET durable receipt
及其准确 SHA，以不同 authority 绑定新 source/cursor/high-watermark，并从 receipt 封存的历史最大
epoch 推导 `max+1`；两个 worker 始终 dormant，UniRouter-only BOOTSTRAP 不生成 LiteLLM 配置或凭据。

apply 先把 fresh dump 隔离 restore 到 operation 唯一的临时库，以完整 schema/migration/34 表
semantic digest 对比停稳 source，随后删除并证明临时库不存在；live 新数据库只运行 release
migrations，不 restore、TRUNCATE、CASCADE 或手工删除 history。它保留 release schema/source/adapter
catalog；DATA_RESET receipt 时 checkpoint state/operation/history 全部
为零，随后独立 BOOTSTRAP receipt 才写入单一 inactive/idle、零 counter、无 operation/history 的
checkpoint。`demo.operator` subject/username/role/password verifier 原样保留，其他账号与旧内存
Session 不保留。失败在 durable receipt 前自动恢复旧数据库链接和完整账号文件；新数据库保留供
取证且不可复用。receipt 已落盘只允许精确 resume；只有 retained dump path/SHA/PG16 archive、sealed
restore proof、严格空 live link 与旧库零 session 全部重验后，才删除并证明 predecessor/restore 库
不存在，再精确移除含其他账号的单文件前驱备份。缺少 dump/proof 时在旧库删除前 fail closed；
successful completion 只保留一个已验证 custom-format dump，不同时保留旧数据库。
该能力不自行授权 Target backup/reset、凭据、
服务操作、Feed poll、测试流量、External integration 或 Production。

## 0.3.0-rc.7 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 20（新增 source-neutral checkpoint epoch operation/history） |
| Config schema | `xinsight.config.v1`；可选独立 trusted TLS proxy env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1`；Connector checkpoint 增加 epoch 只读字段 |
| Evidence | `m0.1`；epoch delivery state 不进入 event identity/hash |
| Shared protocol | Foigent `v2.0.2` immutable product pin；共享 Schema/vector/mapping byte 不变 |
| Receipt | `xinsight.import-receipt.v1`、`foigent.operational-receipt.v2`；epoch history 是 product-local delivery evidence |
| Manual | `0.3.0-rc.7/manual-r7` |
| Upgrade source | exact allowlisted Phase 1 artifact、准确 `0.2.0-rc.3`–`0.2.0-rc.15`、`0.3.0-rc.2`–`0.3.0-rc.6`；同版本和任意未列版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | checkpoint epoch 为 Local 候选；准确 rc.7 CI、Target、External integration、Production 仍分别待证 |

rc.7 新增 root-only `xinsightctl connector-epoch prepare|status|activate|abort`。它要求受管 worker
存在且 inactive+disabled，数据库状态 idle+inactive、无错误且 Cursor=high-watermark；prepare
固定 active-state/plan SHA，activate 在单一 serializable transaction 内归档不可变旧 epoch、
原子切换新 source instance 与 quiescent Cursor，并清零新 epoch 的 delivery counters/retry state。
重复 activate 收敛到同一结果；activate 前可精确 abort。

迁移 20 只增加 product-local mutable-delivery control/evidence，不修改 M0、Gap、Provenance、
Trace Journey、OpenInference、completed-execution DTO、event/hash/idempotency 或现有 Evidence 行。
CLI 报告不返回 raw Cursor/high-watermark，epoch 激活不启动、enable 或配置 Feed，也不授权
UniRouter reset、凭据、流量或第二轮联调。

## 0.3.0-rc.6 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 19（与 rc.5 相同，无新 migration） |
| Config schema | `xinsight.config.v1`；可选独立 trusted TLS proxy env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1`；新增向后兼容 Posture GET |
| Evidence | `m0.1`；Posture 只读聚合不修改 event identity/hash |
| Shared protocol | Foigent `v2.0.2` immutable product pin；共享 Schema/vector/mapping byte 不变 |
| Receipt | `xinsight.import-receipt.v1`、`foigent.operational-receipt.v2`；可变 delivery state 不进入 M0/hash/provenance |
| Manual | `0.3.0-rc.6/manual-r6` |
| Upgrade source | exact allowlisted Phase 1 artifact、准确 `0.2.0-rc.3`–`0.2.0-rc.15`、`0.3.0-rc.2`、`0.3.0-rc.3`、`0.3.0-rc.4` 或 `0.3.0-rc.5`；同版本和任意未列版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | Evidence Posture release identity 为 Local 候选；准确 rc.6 CI、Target、External integration、Production 仍分别待证 |

rc.6 把 `/inbox` 定义为跨系统 Evidence Posture，而不是 MaaS 运营看板副本。所有 execution、
Gap、coverage 和趋势聚合共用固定 `as_of/from/to`；Direct time、Identity、Outcome、Causal
coverage 保留各自分子、分母与限制，不合成健康分。Runtime Path 只使用来源声明的 Trace
Journey parent edge，未关联 execution 的 Alert 明确保持 unlinked。

MaaS 的吞吐、价格、配额、路由配置和供应商运营指标继续由 MaaS 拥有。X-Insight 只保留能支持
调查的风险、Evidence 覆盖、Gap、来源上下文与路径，并下钻到现有 Execution、Case、Discovery
和 Connector 界面。Connector 当前 trust/checkpoint 与固定 Evidence 快照分开，前者不进入
Event hash 或 Event-linked provenance。

本候选不增加 migration，不改变 schema 19、数据库四角色、共享 Event/Connector Schema、
Golden vectors、M0/Gap/Provenance/Receipt identity、Trace Journey、OpenInference bounded-offline、
Cursor/Checkpoint 或 Workspace 写模型。升级新增准确 rc.5 来源，同版本替换仍拒绝。

## 0.3.0-rc.5 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 19（与 rc.4 相同，无新 migration） |
| Config schema | `xinsight.config.v1`；可选独立 trusted TLS proxy env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1`；新增向后兼容 Trace Journey GET |
| Evidence | `m0.1`；既有与新增 OpenInference event identity/hash 都由原冻结投影产生 |
| Shared protocol | Foigent `v2.0.2` immutable product pin；共享 Schema/vector/mapping byte 不变 |
| Receipt | `xinsight.import-receipt.v1`、`foigent.operational-receipt.v2`；授权与 delivery state 不进入 M0/hash/provenance |
| Manual | `0.3.0-rc.5/manual-r5` |
| Upgrade source | exact allowlisted Phase 1 artifact、准确 `0.2.0-rc.3`–`0.2.0-rc.15`、`0.3.0-rc.2`、`0.3.0-rc.3` 或 `0.3.0-rc.4`；同版本和任意未列版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | Trace Journey、一次性授权门、metadata-only 3-span fixture 与相同 operation 幂等重放已在命名非生产 Target 验证；External integration、Production 仍分别待证 |

rc.5 在不修改 M0 `request_id`、event ID/hash 或共享协议映射的前提下，新增 product-local
`xinsight.trace-journey.v1` 只读投影。它由一个已授权 execution 锚定，只聚合同一
`source_system_id + org_ref + project_ref + trace_id` 的非 Gap Evidence；只有唯一可解析的
`parent_span_id` 生成跨 execution 边，cycle 明确令完整性失败，未连边节点之间不绘制因果箭头。
UI 直接显示“谁发起—做了什么—经过系统—终点”，同时保留 bounded export 不等于完整来源
时间线的限制。

OpenInference general runtime 与 Production bootstrap 继续 blocked。唯一新增入口要求 exact
root-owned、最多两小时的非生产授权 manifest，绑定 target machine ID、当前 build、bundle
path/SHA、operation、producer、scope 与最大 record 数。bundle 必须不含 prompt/response 属性；
listener、Feed、Cursor、Checkpoint 均明确为 false。命令在授权、metadata policy、SHA 和 record
上限全部通过后才读取 importer credential 或连接 PostgreSQL；精确重放仍由既有 durable receipt
幂等收敛。两个既有 Feed 默认状态和运行语义不变。

本候选不增加 migration，不改变 schema 19、数据库四角色、共享 Event/Connector Schema、
Golden vectors、M0/Gap/Provenance/Receipt identity、现有 Cursor/Checkpoint 或 Workspace 写模型。

## 0.3.0-rc.4 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 19（与 rc.3 相同，无新 migration） |
| Config schema | `xinsight.config.v1`；可选独立 trusted TLS proxy env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1` |
| Evidence | `m0.1`；identity/hash 不变，Workspace 仅引用 Evidence |
| Shared protocol | Foigent `v2.0.2` immutable product pin；既有 v1 runtime bindings 保留 |
| Receipt | `xinsight.import-receipt.v1`、`foigent.operational-receipt.v2`；后者不进入 M0/Provenance/Cursor/Checkpoint |
| Manual | `0.3.0-rc.4/manual-r4` |
| Upgrade source | exact allowlisted Phase 1 artifact、准确 `0.2.0-rc.3`–`0.2.0-rc.15`、`0.3.0-rc.2` 或 `0.3.0-rc.3`；同版本和任意未列版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | execution search fixed-snapshot 契约 Local 实现/测试通过；CI、Target、External integration、Production 仍分别待证 |

rc.4 修复 authenticated execution search 的 fixed-snapshot 重读断点。首轮查询返回
`page.as_of` 后，前端会把该值写回 URL 并在同一快照重读；rc.3 后端遗漏了 search `as_of`
允许项，导致第二次请求被错误归类为 `invalid_argument`。rc.4 在 search 使用既有 24 小时
`as_of` 校验，并要求显式 `as_of` 与 Cursor 内快照严格一致；OpenAPI 与回归测试同步冻结该
契约。前端固定快照、分页和 URL 可复核语义不降级。

本 PATCH 不增加 migration，不改变 schema 19、数据库四角色、Evidence/M0 identity/hash、
Connector、Gap、Provenance、Receipt、Cursor/Checkpoint 或 Workspace 数据模型。rc.3 到 rc.4
使用既有四凭据布局，不触发 Workspace credential bridge。

## 0.3.0-rc.3 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 19（与 rc.2 相同，无新 migration） |
| Config schema | `xinsight.config.v1`；可选独立 trusted TLS proxy env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1` |
| Evidence | `m0.1`；identity/hash 不变，Workspace 仅引用 Evidence |
| Shared protocol | Foigent `v2.0.2` immutable product pin；既有 v1 runtime bindings 保留 |
| Receipt | `xinsight.import-receipt.v1`、`foigent.operational-receipt.v2`；后者不进入 M0/Provenance/Cursor/Checkpoint |
| Manual | `0.3.0-rc.3/manual-r3` |
| Upgrade source | exact allowlisted Phase 1 artifact、准确 `0.2.0-rc.3`–`0.2.0-rc.15` 或 `0.3.0-rc.2`；同版本和任意未列版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | 命名非生产 Target 的 rc.15→rc.3 升级、四角色数据库桥接与运行边界已验证；登录后完整 UI 验收被 fixed-snapshot 缺陷阻断；External integration、Production 仍待证 |

rc.3 只修复真实 rc.15 Target 暴露的三凭据到四角色数据库边界迁移断点。非变更 preflight
明确报告 `workspace_bridge_required`；mutating adopt/upgrade 生成唯一 Workspace 密码、保留三条
旧 DSN、建立不可变 successor、创建并验证最小权限角色，再原子切换 credential symlink。
角色扩展开始后只允许 recover/resume；旧凭据集、M0、Connector、Gap、Provenance、Cursor、
Checkpoint、Receipt 与 A+C UI 语义不变。

从 rc.2 升级必须已有严格四凭据布局，不触发兼容桥。rc.3 没有新 migration；旧应用仍不兼容
schema 19，因此 application rollback 继续 fail closed，必要时使用隔离数据库恢复。

## 0.3.0-rc.2 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 19（schema 18 调查能力 + immutable completed Connector Operational Receipt） |
| Config schema | `xinsight.config.v1`；可选独立 trusted TLS proxy env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1` |
| Evidence | `m0.1`；identity/hash 不变，Workspace 仅引用 Evidence |
| Shared protocol | Foigent `v2.0.2` immutable product pin；既有 v1 runtime bindings 保留 |
| Receipt | `xinsight.import-receipt.v1`、`foigent.operational-receipt.v2`；后者不进入 M0/Provenance/Cursor/Checkpoint |
| Manual | `0.3.0-rc.2/manual-r2` |
| Upgrade source | exact allowlisted Phase 1 artifact 或准确 `0.2.0-rc.3`–`0.2.0-rc.15`；同版本和任意未列版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | Local implementation/测试通过后方可形成候选；Target、External integration、Production 仍分别待证 |

本候选提供 outcome-first Story、来源声明 DAG、独立 Chronology、四轴 completeness、Case/
Finding/Evidence Pack、Agent read/cite、来源声明 Session、Compare、Feedback、Alert-to-Case、
版本维度、Gap resolution 和 Known/Unknown Discovery。Discovery 是带 Evidence、limitations 和
abstention 的确定性假设层，不是自动策略或执行平面。

rc.2 当时的产品化修订在同一未发布候选内重组任务导航，加入显式移动菜单、四步执行路径
摘要、1024px 结果卡片、小屏来源关系列表、渐进披露和 Connector 三层状态。它不增加 API、
migration、Evidence 字段或因果关系；发布资格继续绑定最终准确 commit/payload，而不是复用
此前候选构建。

Foigent `v2.0.2` 已作为不可变发布被产品 pin 采用。该 PATCH 修复远程 composite Action 的
snapshot portability；产品 CI 不再 checkout 私有权威仓，也不需要权威仓历史或产品侧凭据。
Event/Connector Schema、vectors、reports、M0 identity/hash 和产品投影输出相对 `v2.0.1`
保持不变。OpenInference bounded-offline importer 与 durable completed receipt 已实现并通过
本地 crash/replay 收敛测试，但 runtime 仍因未获运行时激活授权而 fail closed。Local 实现不能替代
Target/External proof。
`0.3.0-rc.1` 仅保留为未发布的本地历史证据，不在本候选的 upgrade allowlist 中。

## 0.3.0-rc.1 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 18（Case/Workspace、Agent Access、分析扩展与 bounded-offline provenance） |
| Config schema | `xinsight.config.v1`；可选独立 trusted TLS proxy env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1` |
| Evidence | `m0.1`；identity/hash 不变，Workspace 仅引用 Evidence |
| Shared protocol | Foigent `v2.0.0` product pin；既有 v1 runtime bindings 保留 |
| Receipt | `xinsight.import-receipt.v1`；OpenInference bounded-import receipt 尚未实现 |
| Manual | `0.3.0-rc.1/manual-r1` |
| Upgrade source | exact allowlisted Phase 1 artifact 或准确 `0.2.0-rc.3`–`0.2.0-rc.15`；同版本和任意未列版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | Local implementation/测试通过；Target、External integration、Production 仍分别待证 |

本候选提供 outcome-first Story、来源声明 DAG、独立 Chronology、四轴 completeness、Case/
Finding/Evidence Pack、Agent read/cite、来源声明 Session、Compare、Feedback、Alert-to-Case、
版本维度、Gap resolution 和 Known/Unknown Discovery。Discovery 是带 Evidence、limitations 和
abstention 的确定性假设层，不是自动策略或执行平面。

Foigent `v2.0.0` 的产品 pin 已固定，但 OpenInference runtime 仍 fail closed：权威 Manifest
内嵌投影 digest 与同一 release component digest 不一致，且 durable bounded-import receipt
与运行时授权均不存在。Local parser/projection 兼容向量不能替代 Target/External proof。

## 0.2.0-rc.15 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 15（与 rc.14 相同，无新 migration） |
| Config schema | `xinsight.config.v1`；可选独立 trusted TLS proxy env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1` |
| Evidence | `m0.1`；严格 UniRouter Execution Evidence V2 input |
| Receipt | `xinsight.import-receipt.v1` |
| Manual | `0.2.0-rc.15/manual-r15` |
| Upgrade source | exact allowlisted Phase 1 artifact 或准确 `0.2.0-rc.3`–`0.2.0-rc.14`；同版本和任意跳版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | Local implementation/测试通过；Target HTTPS authenticated flow、External integration、Production 由逐环境 evidence 决定 |

rc.15 不改变 schema、API、Access Plane、Evidence identity/hash、Connector credential 或
checkpoint。它把空 Projection Gap 的归一化永久放回产品代码，移除目标 Nginx 临时
`sub_filter` 依赖；客户演示 UI 在 1280px 使用清晰的单列 Investigation/Chronology/Proof
阅读流，HTTPS 入口显示准确的产品 Session 状态，并移除 Phase 编号和内部实现文案。

同机 loopback TLS terminator 继续是显式 opt-in 合同，不是对任意反向代理 header 的信任。
应用仍只监听 `127.0.0.1:8080`；固定 external origin、loopback remote、唯一
`X-Forwarded-Proto: https`、准确 forwarded host、Host/Origin 一致以及无通用 `Forwarded`
header 缺一不可。代理必须覆盖客户端 forwarded headers，只代理 reader-only 产品 UI/API。
HTTP tunnel、代理自身登录页和应用/数据库直接公网监听仍不受支持。

## 0.2.0-rc.14 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 15（additive Execution Evidence V2 产品注册） |
| Config schema | `xinsight.config.v1`；可选独立 trusted TLS proxy env |
| API | `xinsight.investigation.v1`、`xinsight.access.v1` |
| Evidence | `m0.1`；严格 UniRouter Execution Evidence V2 input |
| Receipt | `xinsight.import-receipt.v1` |
| Manual | `0.2.0-rc.14/manual-r14` |
| Upgrade source | exact allowlisted Phase 1 artifact 或准确 `0.2.0-rc.3`–`0.2.0-rc.13`；同版本和任意跳版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | Local implementation/测试通过；Target HTTPS authenticated flow、External integration、Production 由逐环境 evidence 决定 |

rc.14 增加确定性 Trace Investigation projection、Investigation/Chronology/Proof UI、七阶段覆盖、
Evidence/Gaps/citation inspector，以及精确 latency/token/micro-CNY。因果边只来自 source
声明的 parent span；recovery 只在同一执行内观察到有序 failure→retry→fallback→later
provider success 且 source/projection complete 时成立。

同机 loopback TLS terminator 是显式 opt-in 合同，不是对任意反向代理 header 的信任。
应用仍只监听 `127.0.0.1:8080`；固定 external origin、loopback remote、唯一
`X-Forwarded-Proto: https`、准确 forwarded host、Host/Origin 一致以及无通用 `Forwarded`
header 缺一不可。代理必须覆盖客户端 forwarded headers，只代理 reader-only 产品 UI/API。
HTTP tunnel、代理自身登录页和应用/数据库直接公网监听仍不受支持。

## 0.2.0-rc.13 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 14（与 rc.12 相同，无新 migration） |
| Config schema | `xinsight.config.v1` |
| API | `xinsight.investigation.v1`、`xinsight.access.v1` |
| Evidence | `m0.1` |
| Receipt | `xinsight.import-receipt.v1` |
| Manual | `0.2.0-rc.13/manual-r13` |
| Upgrade source | exact allowlisted Phase 1 artifact 或准确 `0.2.0-rc.3`–`0.2.0-rc.12`；同版本和任意跳版本不支持 |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | Local implementation/测试进行收口；PR CI、exact-main CI、Target HTTPS authenticated flow、External integration、Production pending |

rc.13 在不改变 schema 14、Evidence/Connector wire、reader DSN 身份或两个既有 Connector
worker 的前提下，新增产品自有 Access Plane：root-provisioned 严格本地账户 JSON、
Argon2id、内存 Session、`investigator|operator` RBAC、login/logout/current-session、
operator Session list/revoke，以及最小化不含 username/password/token/Evidence 的审计。
账户文件没有默认可用账户，也没有 Web 建号、password reset、改角色或写回能力；启动、
install/adopt/upgrade/resume/abort 会在任何受管 mutation 前复核准确 credential。

匿名调查 UI 现在必须返回 no-store `303 Location: /login`，匿名调查 API 必须返回准确六字段
`401 authentication_required` problem。`/readyz` 使用 reader 执行一个有界、零数据返回的
代表性查询，覆盖五张调查表；任何一张表的 SELECT grant 漂移都会 fail closed。静态资源仅
允许当前 `index.html` 实际引用的哈希文件，仓库中为不做批量删除而保留的历史生成资产不通过
HTTP 暴露。

Session cookie 固定为 `__Host-xinsight_session` 的 Secure/HttpOnly/SameSite=Strict host-only
cookie；状态变更要求 direct same-origin HTTPS 与 Session-bound CSRF。当前签名候选仍没有
TLS listener、certificate/hostname 或 trusted-proxy contract，因此 Local TLS handler/UI
测试不等于 Target 登录证明。qualification 只允许全 disabled synthetic CI credential 来证明
安装门禁和匿名边界，不生成默认可登录账户；Target HTTPS authentication、External
integration 与 Production 均保持 pending。

## 0.2.0-rc.12 历史资格说明

| 维度 | 值 |
| --- | --- |
| Host | Ubuntu 22.04 LTS, Linux amd64, systemd |
| PostgreSQL | 16.14 Alpine 3.24 fixed image, same private host, loopback Compose |
| Database schema | 14 |
| Config schema | `xinsight.config.v1` |
| API | `xinsight.investigation.v1` |
| Evidence | `m0.1` |
| Receipt | `xinsight.import-receipt.v1` |
| Manual | `0.2.0-rc.12/manual-r12` |
| Upgrade source | exact allowlisted Phase 1 artifact or exact `0.2.0-rc.3`/`0.2.0-rc.4`/`0.2.0-rc.5`/`0.2.0-rc.6`/`0.2.0-rc.7`/`0.2.0-rc.8`/`0.2.0-rc.9`/`0.2.0-rc.10`/`0.2.0-rc.11`; same-version and arbitrary version jump unsupported |
| First-customer authority | `owner_supervised_trial`; User+Codex only; no retained private key; no customer self-upgrade |
| Field qualification | LiteLLM Reference Connector implemented locally; PR CI、exact-main CI、Target rc.12 reconciliation、External integration closure、Production pending |

rc.12 以 additive/reversible schema 14 新增受约束的 `connector_pull` provenance mode，
但不改变 completed-execution Feed wire/credential/checkpoint、M0 event identity/hash、
EvidenceStore 写入事务或 Read API。LiteLLM 使用 `connector_pull`，既有 Reference 继续
使用 `completed_execution_pull`；down 在存在 `connector_pull` 行时 fail closed。它以
精确产品注册新增独立 LiteLLM spend-log worker/service/config/credential，
全局 CI authority 与 LiteLLM 产品本地 binding 固定为不可变 Foigent Event Connector
Protocol `v1.1.2`、commit
`fd82aeca9877bea8064b25c365a3cdcdd0bb3d16`、release digest
`sha256:0b414d5d59e5a7ca5c05505cb58d7992c544537da1c84a57256ec205618d9cd1`。
它仅把 LiteLLM v1.93.0 的精确 `cache_hit="None"` 映射为 `cache_status="unknown"`，
不接受大小写变体或其他字符串。既有 completed-execution 产品本地 runtime binding 继续保持其已验证的
Protocol `v1.0.0`。两者各自拥有 source/adapter/cursor identity，LiteLLM-only erratum
不改变 completed-execution telemetry/registry/lifecycle，通用 worker/EvidenceStore 不做
source switch。

LiteLLM Reference V1 严格读取私有 HTTPS `/spend/logs/v2`，只保留 metadata allowlist，
固定 LiteLLM 配置显式关闭 message 与 raw request/response logging；真实组件 CI 用唯一
sentinel 证明 Source DB、容器日志、`/spend/logs/v2` 和 X Evidence 四层均无 prompt/response；
同一资格把三把生成的 raw virtual key 作为 secret sentinels 并逐层证明明文为零。
组件 CI 的 master key 只初始化团队/成员/key；目标团队的独立 traffic key 产生成功/失败，
非管理员 observer 成员同时受团队 `/spend/logs` 权限和 key
`allowed_routes=["/spend/logs/v2"]` 双重限制。资格必须证明 observer 只读取目标团队两条记录、
不能调用 LLM route，且看不到第二个未授权团队的一条记录；运行时严禁使用 master key。
组件模型以 `model_info` 固定 input/output token 单价
`0.000001`/`0.000002` USD，并在 Source HTTP 与 X M0 两层断言成功样本
3/2/5 tokens、`0.000007` USD，以及失败样本 0/0/0 tokens、0 USD。
以 60 秒 start-time eligibility delay 收敛：`F=floor_microsecond(T-60s)`，HTTP inclusive
`end_date` 使用 `Q=floor_second(F)`，`Q < startTime <= F` 延后到重叠轮次，row
`endTime` 不参与查询 fence/checkpoint；checkpoint 按 `(startTime, request_id UTF-8 bytes)`
推进，整窗最多 20 页/2000 行/8 MiB。协议 legacy
`finality=finalized_after_delay` 不投影为 M0 terminal finality。USD spend 仅进入
source-reported extension；M0 主事件为
`model.request.succeeded|failed` 并带四个明确 gaps。晚到且 `startTime` 早于已推进
checkpoint 的行不在 V1 可恢复覆盖内；固定 SourceSystemID/AdapterManifestID 只支持一个
LiteLLM instance profile。此候选不宣称 Production-lossless、多实例 registry 或通用
Phase 4 插件运行时。

## 0.2.0-rc.11 历史资格说明

rc.11 在既有 completed-execution pull 上采用首个 Reference Connector 产品内兼容桥；
`legacy|shadow|neutral_canary` 保持单 writer、单 cursor，并以既有 M0/Gap/provenance
精确兼容门禁保护历史输出。

## 0.2.0-rc.10 历史资格说明

相较 rc.9，rc.10 不改变 schema 13、completed-execution contract、HTTP source、credential、
cursor/high-watermark、EvidenceStore 或 migration set。它只把 outer PostgreSQL
`TIMESTAMPTZ` 与 immutable inner RFC3339 terminal timestamp 的同一 instant 绑定定义为
UTC PostgreSQL 微秒精度：残差严格小于 1µs 兼容，等于或大于 1µs 仍 fail closed；inner
bytes/SHA 不重写。rc.9→rc.10 仍由已验签目标 payload 控制器执行，并在 upgrade/recovery
中保留既有 config、active credential、operation state、DB set、durable cursor 和 worker
的 active、enabled 两项原状态；两者独立保留，包括 active+disabled。

## 0.2.0-rc.9 历史资格说明

rc.9 不改变 schema 13、completed-execution contract、HTTP source、cursor 或
EvidenceStore。它新增 target-local root-only
`xinsightctl feed-credential prepare|status|activate|abort`：raw token 由 32-byte
`crypto/rand` 生成且不离开 X target，UniRouter 只登记严格六字段 public verifier bundle。
`.next` 文件采用 exclusive/no-follow/root `0600`/single-link 校验，exact
operation/verifier 绑定、atomic replace 和 durable nonsecret state 使 prepare/activate/abort
跨 crash 可恢复。release qualification 只证明 CLI/schema 与 dormant absent 状态，不执行
`prepare`、不生成 principal、不启动 worker。

## 0.2.0-rc.8 历史资格说明

rc.8 不改变 schema 13 或 completed-execution contract。它修复跨版本升级控制器边界：升级及
其中断恢复必须由已验签并解出的目标 payload 内 `xinsightctl` 执行，使新 systemd/runtime
inventory 能完整落地。其 exact-main Signed V2/Fresh Install 与 Development rc.8/schema13
dormant reconciliation 已通过；feed unit loaded/inactive/disabled，feed env/key/process
均不存在。该历史资格不替代 rc.9 receiver-generated credential 的独立签名发布与 Target
reconciliation。

本候选实现 execution search/detail/timeline、selected-node/source/provenance/gap UI、七种字段
状态、签名 release envelope、checksum/dirty migration ledger、维护锁与可恢复单机生命周期。

rc.7 相对不可变的 `0.2.0-rc.6` 新增严格的
`unirouter.completed-execution.v1` receiver、私有 pull source、durable activation/cursor/
worker state、现有 EvidenceStore 幂等提交、live payload provenance 与独立 writer worker。
V1 是 at-least-once：partial-page persistence 可接受，但只有所有 item inserted 或
duplicate-noop 后才推进 cursor；冲突失败关闭。rc.8 已把该 worker 以 dormant 状态带到
Development；真实 credential registration、activation、双方流量与 API/UI readback 仍须
按单独 joint activation baton 证明。

## 0.2.0-rc.6 历史资格说明

本候选还关闭了 `0.2.0-rc.1` 在首次真实 Target preflight 中暴露的 P1：受管 Compose、
Phase 1 实际数据盘和 Target runbook 现在统一为 `/data/x-insight/postgres` 与根 `PGDATA`，
支持形态 legacy qualification 覆盖旧 `xinsight` 超管到三角色 credential set 的受监督桥接。

相对 `0.2.0-rc.2`，本候选还让签名 payload 解包、lifecycle 目录和 systemd/Compose
运行时文件显式恢复合同权限，不再继承操作账户的 `umask`。rc.2 在命名 Target 的
`stage_release` 阶段因 `umask 077` 安全失败，并已在数据库迁移和激活前受控中止；它不获得
Target 状态。

相对 `0.2.0-rc.3`，本候选核对并保留准确 Phase 1 release-layout drop-in，再以签名包内
`99-xinsight-managed-launcher.conf` 固定有效 launcher；未知 active drop-in 会在 preflight
失败。既有产品目录也会恢复准确权限，升级/回滚/恢复会额外证明 systemd 的有效
`ExecStart`。

相对 `0.2.0-rc.4`，本候选把 required payload 清单绑定到签名 manifest 的产品版本：只有
准确历史 `0.2.0-rc.3` 可不含后来引入的 managed-launcher drop-in，当前与未知版本仍必须包含。
rc.4 在命名 Target 的只读 preflight 阶段发现无法验证 rc.3 previous bundle，未创建 operation、
未备份、未迁移、未激活，现场已恢复到稳定 Phase 1；因此 rc.4 不获得 Target 状态。

当前包含生产意图的私有 pull worker 和 receiver-generated credential tooling，但尚未由本页
证明 live activation、External integration 或 UI closure。它不包含自动 receipt transport、
raw prompt/response、多租户、公网、HA/DR 或 Production verified；历史
operator-mediated sidecar 不再是 live transport。

精确 PostgreSQL image index/architecture digest、legacy artifact/manifest digest 和签名 key ID
以同包 `release/compatibility-matrix.json`、`owner-supervised-trial-release.json` 与 signed
manifest 为准；本页不复制每次 workflow 动态生成的 key ID。
