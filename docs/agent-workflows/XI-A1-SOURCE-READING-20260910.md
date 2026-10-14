# A1 Source Facts 本地消费与阅读

本包实现经理 review26 批准的三 profile 合成 HTTP 接收、不可变存储、版本化阅读与生产 Handler/UI。产品基线 `417daf1eb0a88f9abbcc3b3c8e9726eb7dfae152`，隔离实施基点 `5368eafa1b76e0c7841a2344d3f07316afc864e0`；开发数据库追加到 schema29。此文不授权启用真实来源、改变真实账户权限或发布部署，也不代表完整 A1/A0 或整体迭代验收。

## 读取与解释

- `unirouter.execution-context.v1`：显示捕获的应用、逻辑凭据及认证上下文。应用标识作为字符串分组。
- `unirouter.execution-identity.v1`：显示捕获时账户归属、授权主体、Sponsor 与来源权威。账户归属可分组；实际操作者与业务目的保持未知。
- `unirouter.cost-basis.v1`：显示来源冻结的计费模式、版本和费率；已定价零费率、未定价、字段未采集分别说明。
- 来源阶段和修订不增加调用数或金额。经济表继续以原 I-01 身份计数；新的 source 表不保存可加总费用。
- I-01 引用绑定来源实例、组织、项目、请求、经济 revision1 与原 snapshot hash。cost profile 自身 revision2 等后续修订仍可引用 I-01 revision1。
- `async_media_charge_fact` 使用独立经济合同，显示待定价或不适用，不进入文本 I-01 费用排名。
- 来源先到时，即使经济成员为零，来源记录及覆盖状态也可见。已知金额小计为零并不代表这些请求费用为零，也不宣称总体完整。
- 建立阅读时，按当前授权选取来源各身份最新已持久化修订，优先终态、派发、准入、认证阶段解释同一经济事实的字段。缺前驱的修订保留、显示 pending，但不采用其字段，也不回退假装旧阶段是当前结论。
- 来源接收时间与重复读事务快照限制本次成员选择，历史 occurred/recorded 时间不能回填已有阅读。迟到 I-01、补齐前驱或修订只进入显式新阅读。

## 两层权限

采集注册与 XI 当前用户权限分离。`source_fact_streams` 的 producer/org/project/profile/generation 注册由管理员明确配置，默认 paused。身份范围不可原位修改，revoked 不可重新激活；新范围使用新注册，旧游标不自动重置。

`workbench_access_grants` 新增 `allow_source_context`、`allow_source_identity`、`allow_source_cost`，全部默认 false。动作布尔值及来源/项目范围仍须同时满足。默认扩展不改变旧授权摘要；新解释另存字段授权摘要，实际撤权使当前 A1 阅读失效。采集许可、旧 Evidence/Case 角色和既有四个动作布尔值均不隐式授权新字段。

来源分组、计数、覆盖与明细只包含授权 profile 和范围。cost_project 不能从这些来源字段推定，因此 cost_project 限制的阅读不采用新来源维度。摘要授权与明细授权分开；仅有摘要权限时不返回来源明细。既有 Case/Evidence 导出未扩展新字段。

review27 返修将授权范围内的采集注册状态冻结到可选 `source_coverage.collection_states`，值为 active/paused/revoked；多个注册可同时出现不同状态，不等于整个来源只有一种状态。采集停止不撤销历史阅读权限；人员字段撤权仍即时限制读取。旧阅读未记录此字段时保持缺失，页面明确不据此推断正在正常采集。

## 有界消费入口

数据库先通过原有迁移器升级并执行既有 runtime grants；四张新表分别为 `source_fact_streams`、`source_fact_documents`、`workbench_source_members`、`workbench_source_manifests`。只有开发态 schema contract 更新，历史迁移和正式发布合同保留。

按经批准的精确来源身份建立采集注册，并独立配置目标用户的三类字段权限。这里没有隐式发现、复制 I-01 凭据或自动扩权。生产启用仍需要对应环境的授权。

```sh
xinsight-importer unirouter pull-source-facts \
  --stream-id <已注册的精确 UUID> \
  --endpoint https://<已批准来源>/api/v1/integrations/xinsight/source-facts \
  --key-file <已批准的来源专用凭据绝对路径>
```

使用已有 importer 数据库凭据解析器。来源 key 文件必须是安全的普通文件；不会从旧 I-01 key 配置回退。只有已批准的私有隧道 HTTP 才使用 `--allow-private-tunnel-http`。每次执行核对 capabilities，无查询参数；随后拉取一个最多50条、8MiB以内的页，请求超时5秒。文档最多64KiB；复用严格 JSON、规范化和 Envelope 校验路径。输出仅含持久化状态、文档数与 has_more。

首次空游标走新来源合同的 forward-only bootstrap。普通空页不能移动 checkpoint；合法但缺前驱的文档可原子持久化并推进页，避免未来前驱造成死锁。全部文档和页位置同事务提交；同身份同版本异内容、来源范围错误、非法字节或摘要错误均不跳过。跨采集代际按来源事务锁串行校验修订链接，重复字节幂等保留。

HTTP 400 invalid_cursor、409、410、权限失败、来源暂停和 generation/profile/producer 变化均记录明确缺口并停止本次消费，不自动重置、换范围或重试。修复后由调用方再次执行同一有界入口。来源文档、source hash、Envelope hash、I-01 hash、XI reading manifest 各自保留意义。

本地同身份同修订异字节或修订链矛盾返回 `ErrConflict`，坏页完整回滚后，以原期望 revision、active状态及checkpoint比较更新 `last_error=source_fact_conflict`。陈旧请求单独返回 `ErrStreamChanged`，不会被记为来源矛盾；若另一消费者已成功推进或注册已停用，旧告警写入同样失败。后续合法页成功提交才清除当前错误。新阅读采用新状态，旧阅读的错误快照不回填；CLI错误和本次原始失败证据保留。`last_error` 是当前状态，并非新增完整错误事件账本。

## API 与恢复

`GET /api/v1/workbench/readings/{id}/source-facts?limit=50&cursor=...` 返回当前用户仍有明细字段权限的冻结记录。分页游标绑定 reading 与查询种类；上限100。阅读与 summary 增加可选 `source_coverage`，阅读增加 `source_manifest_ref`；仅有相应字段权限的阅读提供 application/account 分组。

六个 ALWAYS 触发器保护来源/阅读内容、阅读成员封存顺序及采集范围身份。恢复保留旧 A0 解释分支，同时验证新来源规范字节、两类摘要、原身份、I-01 关联、修订链接、manifest 与覆盖计数。恢复不重建或更新旧解释。运行角色仅增加必要 INSERT 或精确 checkpoint UPDATE 列权限。

本地证据使用固定 UR commit `25f5c1fdfb6b5376496c34e9b9cd295efbaf17cd` 的 schema/goldens，以及经理指定归档中的9份真实生产 Handler 合成导出。归档成员字节与来源摘要记录于 `internal/sourcefacts/contract/captured/provenance.json`。合成 HTTP 服务按保留游标响应；该捕获起点由测试夹具显式注册，不是线上重置或新的实时 UR-XI 集成。

原目的声明/人工纠正、I-03 活动输入、真实来源接入、同一集成候选及原13项验收继续待办。A0 性能资格和大副本资源阻塞保持。完整包验证与资源记录在 `output/xi-a1-source-reading/`；最终交付摘要另绑定实际 commit/tree、源码、检查结果与限制。
