# X-Insight / UniRouter 第二轮双轨联调执行计划

状态：`LOCAL_RELEASE_CANDIDATE`；尚未获得 PR、发布、Target 部署、数据重置、凭据、流量或
External integration 资格。

## 1. 精确基线与目标

- X-Insight 基线：commit `bb5f3a07e61962fb17cb0a2a08b297b894b3a70b`，tree
  `2db4bc1674e6befb0524ccee20db8860412904f8`；本候选在隔离分支开发，不改原始脏 checkout。
- UniRouter 基线：commit `fcfd6d3860fa6e95c4174fef7f5c3ee9832dca94`，tree
  `6206ee0fff9f32a84922637ae33678568ffb2bfc`。
- 共享协议：不可变 `v2.0.2`，commit
  `ad8fe7a38c9d7335657630baa8670dd07556b98c`，tree
  `288fa23b0591b6ed2953e14bc4a9d3571e2839ac`，manifest SHA-256
  `3c5489c9931255cce5b565046deee3fd082d524e9a80945cbbd88e413ec073b0`。
- 目标 A 为两条相互独立的证据路径：
  1. **Track 1 / V1 Reference Connector**：证明既有 endpoint、DTO、凭据注册、排序、Cursor、
     at-least-once、幂等消费和 M0 数据结果继续兼容。
  2. **Track 2 / Rich Trace**：用批准的真实 metadata-only source facts 证明发起者、动作、
     来源声明的父子路径、经过系统、终点和 first-class Capture Gap；不从时间相邻推断因果。

两条 Track 的成功条件分开记录。V1 Feed 成功不能代替 Rich Trace；受控 Rich Trace artifact
成功也不能冒充连续 live Connector。

## 2. 已实现的第二 epoch 安全边界

X-Insight `0.3.0-rc.7` / database schema 20 增加 source-neutral checkpoint epoch：

- 每个 Source/Adapter/Cursor binding 最多一个 prepared operation；
- worker 必须存在、inactive、disabled，durable state 必须 idle/inactive、无错误且
  Cursor=high-watermark；
- prepare 固定 active-state hash 和 redacted plan hash，不在 argv、报告、M0、Event hash、
  idempotency、event-linked provenance 或 Gap identity 中暴露 raw Cursor；
- prepared operation 通过数据库谓词阻止同一 binding 开始 poll，关闭 systemd 检查后的并发窗口；
- activate 在单一 serializable transaction 中保存不可变旧 epoch、原子重绑新 source instance /
  Cursor，并清零 retry/counters；exact replay 为 no-op；
- 迁移 down 在任何 operation/history 已存在后 fail closed，不能伪装成可逆迁移。

这不会删除第一轮 Evidence。第一轮 M0、Gap、Provenance 与调查视图继续保留；只更换可变交付
epoch，避免第二轮把旧 Cursor、producer instance 或 retry state 当成新 campaign。

## 3. 端到端顺序

### Phase 0 — 三端 exact identity 冻结

记录 X-Insight、UniRouter、共享协议的 commit/tree、release manifest、Target artifact、数据库
migration ledger、Compose/Deployment Profile 与两个 Feed observed state。任一 identity 漂移即
重新生成本轮证据，不继承旧确认。

### Phase 1 — 合并与发布候选

1. UniRouter 只更新产品 pin 到共享协议 `v2.0.2`；产品本地 Reference Connector runtime
   binding 继续为原 v1，不改 Feed 结果。
2. X-Insight 合入 rc.7 epoch 控制、schema 20、OpenAPI/UI epoch read model、发布 Schema 与手册。
3. 两仓分别跑准确 PR CI、exact-main CI；失败按代码/契约/基础设施分类，不复用历史绿灯。

### Phase 2 — 命名非生产 Target 升级

仅在独立授权后生成并验签 rc.7 artifact，在命名非生产 X-Insight Target 执行 preflight、备份、
升级、迁移、失败恢复与登录后 UI 验收。两个 Feed 全程保持 inactive + disabled；不创建流量。

### Phase 3 — reset preview 与备份门

1. UniRouter 运行现有 `uniroute reset preview` 的 REPEATABLE READ / READ ONLY 清单；未知表令
   `scope_complete=false`。
2. 记录 reset candidate、protected、schema ledger 的逐表行数和 exact `plan_sha256`。
3. 在任何清空前生成并恢复验证 PostgreSQL 备份，记录独立 SHA-256；preview digest 不是备份 digest。
4. 用户只需对这份 exact preview、保留项和恢复方案作一次数据删除决定。

当前 UniRouter 工具故意只支持 preview，尚无可执行删除命令。不得用手工 `TRUNCATE CASCADE`、
批量脚本或旧联调命令绕过该门。若用户批准 exact reset，再以该预览为输入实现/审查一次性、
fail-closed 的 apply/restore 工作流，或采用用户批准的受管数据库重建方案。

### Phase 4 — 新 campaign bootstrap

在 reset 后创建全新的 organization、users、project、Workload Application、provider channel、
model governance 和 application-bound credential。部署/TLS 私钥、数据库凭据与发布信任不属于
业务数据 reset，不随业务数据删除。新 `source_instance` 必须显式且与第一轮不同。

### Phase 5 — receiver credential 与 X-Insight epoch 交接

顺序固定，避免新凭据触发旧 Cursor worker：

1. X-Insight `feed-credential prepare` 生成 root-owned staged token，只把 Frozen Registration
   Bundle 交给 UniRouter；
2. UniRouter 在新 project 注册 staged verifier，Feed connection 保持 paused；
3. 用 staged credential 对新 Source 做无 Cursor 的 bootstrap GET，把 exact 空窗口
   `next_cursor == high_watermark` 写入 single-link、root-owned、`0400`/`0600` request file；
4. X-Insight `connector-epoch prepare`，人工核对 operation ID、active-state hash、plan hash 与
   新 source instance；
5. X-Insight `connector-epoch activate`，跨过 checkpoint epoch barrier；此时 worker 仍关闭；
6. 最后执行 `feed-credential activate`，让新 token 原子替换后首次启动 worker。unit 仍不 enable；
   联调窗口结束即停止。

若在第 5 步前失败，abort prepared epoch/credential。第 5 步后 epoch 不可 abort：保持 Feed 关闭，
优先 resume 精确 credential operation；若不能收敛，只能使用已验证 X-Insight 数据库备份恢复，
不得手工覆盖 checkpoint 行。

### Phase 6 — Track 1 / V1 compatibility matrix

运行有界场景：settled、cancelled、非流式成功、流式成功、认证失败、模型未授权、无路由、一次
duplicate replay。逐项核对：

- endpoint 与 `unirouter.completed-execution.v1` revision `1` bytes；
- terminal ordering、source instance、Cursor/high-watermark 与 page replay；
- 相同 source item 只产生相同 M0 event identity/hash/idempotency key；
- duplicate 为 no-op，conflict fail closed，Gap/Provenance 与第一版兼容；
- X-Insight 不进入 UniRouter 同步模型请求路径。

### Phase 7 — Track 2 / Rich Trace

不新增第三个高权限 live worker。本轮复用 UniRouter 已有 source-owned
`scripts/prd5_xinsight_snapshot.go`：只选择本轮批准 request IDs，输出 metadata-only
`call_logs`、`request_trace_events`、显式 Gap、source refs、source event hashes、`exchange_id` 与
approval-manifest SHA-256。禁止 raw prompt/response、credential、raw IP 与 raw upstream error。

X-Insight 先运行 `preflight-sanitized`，再用同一 manifest 执行 `gate-a-check --persist` 和相同
artifact 幂等重放，生成不覆盖既有文件的 import receipt。验收调查 UI 必须直接回答：

- 谁发起或代表谁；
- 做了什么决定/调用；
- 来源明确声明经过哪些组件及 parent edge；
- 在哪个 provider/billing/terminal 结束；
- 哪些事实未知、为何未知、对应哪条 first-class Gap。

相邻时间只能出现在 chronology，不能生成因果边。Track 2 结束后再以实际吞吐、时效、恢复和
运维证据决定是否产品化 UniRouter live high-fidelity Connector；本轮 artifact 不冒充该能力。

### Phase 8 — Client 与恢复矩阵

在新应用凭据下按 OpenAI SDK、Agents SDK Chat model、LangChain、AutoGen、OpenClaw、WorkBuddy
顺序验证非流式/流式、request identity、usage、charge/finality、日志与 X-Insight Evidence。
重启 UniRouter、X-Insight、worker 各一次，证明 application identity、route publication、
billing finality、Cursor/epoch 和幂等结果不丢失。

### Phase 9 — 收口

停止并保持两个 Feed disabled；记录 final Cursor lag、epoch、operation IDs、receipts、查询链接、
backup SHA 与所有未解决 Gap。Local、CI、Target、External integration、Production 结论分层；
本轮不授予 Production。

## 4. 当前唯一需要用户决定的门

本地代码和计划完成后，下一次需要用户决定的是一个合并后的外部影响门：

1. 是否授权把两个隔离候选提交为 PR、通过 exact CI 后合并；
2. 是否授权 rc.7 仅部署到命名非生产 X-Insight Target；
3. Target 与 UniRouter live preview 出具后，是否授权那一份 exact reset scope 的业务数据清空、
   新凭据和有界第二轮流量。

第 3 项必须等实时 preview；任何当前文档或历史审批都不能预先授权未知行数的数据删除。
