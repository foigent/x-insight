# Trace Journey 与 bounded OpenInference activation

日期：2026-08-02
状态：`0.3.0-rc.5` product-local candidate；不扩大 Production/runtime 权限

## 问题与已验证约束

OpenInference 的冻结 v2.0.2 Projection 为每个 LLM Span 生成一个稳定 Event Envelope 和一个
M0 execution：`request_id` 来自共享 Event identity，M0 event ID/hash、Gap 与 Provenance 已有
Golden compatibility。把同一 trace 的所有 Span 强行改成相同 `request_id` 会改变既有 M0 hash、
幂等身份和 B9 compatibility，因此不是可接受的 UI 修补方式。

既有 execution Investigation 只在同一 execution 内解析 parent span。一个多系统 OpenInference
trace 会正确保存为多个 execution，但旧 UI 无法把这些 execution 组合成“谁发起—做了什么—经过
哪些系统—到哪里结束”的可读路径。

## 决策：product-local source-neutral read model

新增 `xinsight.trace-journey.v1`，由一个已通过现有 access/RBAC 的 execution 锚定：

1. 从 anchor 读取唯一的 `source_system_id + org_ref + project_ref + trace_id`；anchor 没有 trace
   或包含多个 scope/trace 时返回 unavailable，不猜测。
2. 只读取同一 exact tuple、fixed `as_of` 可见且非 Gap 的最多 200 条 Evidence。
3. 只在 child 的 `parent_span_id` 对应一个唯一可见 Span 时生成
   `source_declared_parent_span_across_execution` 边；零个为 unresolved，多个为 ambiguous。
4. 来源声明的父子边若形成 cycle，完整性明确失败；UI 只为实际返回的边绘制箭头，未连接节点
   显示断点，不以时间相邻补边。
5. 时间排序只用于可读展示，不生成因果边。bounded export 始终保持
   `source_timeline_complete=false`。
6. 节点保留各自原始 execution/event/request/source ref/event hash；跨 execution 点击进入该节点
   自己的固定 Evidence snapshot。

这不是共享 Event Envelope 或 M0 Projection 的新增语义。它不修改 EvidenceStore writer、M0
schema、event/hash/idempotency、Gap、Provenance、Connector lifecycle、Cursor 或 Checkpoint。
现有 `trace_id` 高基数字段已有索引；查询还受 exact source/scope 与 200 条上限约束，本轮无需以
新 migration 换取一个尚无规模证据的复合索引。

## 一次性非生产 activation

General OpenInference runtime 和 Production bootstrap 继续 fail closed。唯一允许的 product-local
入口是既有 `bounded_offline` importer，并新增严格
`xinsight.openinference-bounded-offline-authorization.v1`：

- 单一 regular hard link、root-owned、`0400`/`0600`，拒绝 symlink；读取前后验证同一文件与字节；
- `environment=nonproduction`、`purpose=openinference_trace_study`，时间窗口最多两小时；
- exact target ref 与规范化 `/etc/machine-id` SHA-256；
- exact release version/commit；
- exact bundle absolute path/SHA-256、operation ID、bundle ID、producer instance、tenant/org/project
  scope 与最多 10 条 record；
- `metadata_only` 且 `raw_payload_present=false`；bundle 中出现 OpenInference prompt/response attribute
  会在 credential/数据库前拒绝；所有值为 false 的安全边界字段必须显式存在，授权与 OTLP JSON
  任意层级出现 duplicate key 均 fail closed；
- `network_listener=false`、`feed_activation=false`、`cursor_checkpoint=false`。

授权通过后仍使用既有 durable operation binding/completed operational receipt。未变的 exact
operation replay 返回同一 receipt；任何 identity 或 bundle drift 冲突。授权 metadata、page time、
delivery state 不进入 M0 hash 或 event-linked Provenance。

## 非目标

- 不增加 OTLP/HTTP/gRPC listener，不接 live traffic；
- 不启用或修改 UniRouter/LiteLLM Feed；
- 不创建通用 Connector daemon、插件运行时或供应商分支核心引擎；
- 不把 chronology、同 trace、同 session 或模型相同推断成因果；
- 不声称 bounded batch 是完整业务执行或 Production 可用性证明。

## 本轮验证要求

- 共享 OpenInference reference fixture 的既有 M0 IDs/hashes 与 4 Gap rows byte-identical；
- 新 3-span metadata-only synthetic bundle 产生三个独立 execution、两条显式 parent edge、3 个主
  Event 与 12 个 first-class Gap；相同输入重投影的 15 个 ID/hash 完全一致；
- API/OpenAPI、PostgreSQL query、桌面/移动 UI、无 raw、授权 negative matrix、durable exact replay；
- exact PR CI、exact-main CI 与签名 release 后，才允许在命名非生产 ECS 执行一次导入；
- 导入前后两个 Feed 均 inactive/disabled，且不出现 OTLP listener、Cursor 或 Checkpoint 变更。
