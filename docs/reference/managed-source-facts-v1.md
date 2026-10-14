# 受管 Source Facts 与独立离线导入

适用于开发候选 schema32。此入口接收 `unirouter.workfacts@1.0.0` 的固定 Source Facts profile；I-01 仍负责经济事实。Local 合成验证不代表已接入真实来源。

## 管理员配置与启停

配置模板在 `deploy/source-facts.json.example`，默认只有执行上下文。登记最多三个明确范围的流；账户/授权来源和计费依据必须显式加入。每个流绑定唯一 ID、source system、source instance、组织、项目、profile 和 scope generation。管理员填写已核对的来源端点及 source-only 凭据文件路径；不能使用 I-01 凭据。

配置文件须为 root 所有、不可被组或其他用户写入的普通文件。凭据原文件须为 root 或执行服务用户所有、0400/0600、单链接普通文件；路径须为规范绝对路径，不含空白、引号、反斜线或 `%`，以保证 systemd 凭据装载与预检一致。配置不包含凭据内容。系统服务通过 `LoadCredential` 取得 importer DSN 和 source key，使用独立 `xinsight-source-facts.service`。其 `/run/credentials/xinsight-source-facts.service` 内的原生只读副本可为 root 所有的 0440，但必须校验只读挂载和精确 ACL：仅 root 与当前服务 UID 可读，组与其他用户无权读取；普通文件不适用此例外。

```sh
sudo xinsightctl source-facts configure --file /absolute/managed-source-facts.json
sudo xinsightctl source-facts register --stream-id UUID
sudo xinsightctl source-facts preflight --stream-id UUID
sudo xinsightctl source-facts enable --stream-id UUID --revision REVISION
sudo xinsightctl source-facts worker-start
sudo xinsightctl source-facts status --stream-id UUID
sudo xinsightctl source-facts worker-status
```

`register` 默认 paused；配置、登记和能力预检都不开始采集。`enable/resume` 重新预检能力，并要求状态输出中的准确 revision；返回冲突时重新检查状态。`worker-start` 明确启用并启动服务；服务只接收 active 且无需处理的登记。配置修改会先停止 worker，不会自动重启。

```sh
sudo xinsightctl source-facts pause --stream-id UUID --revision REVISION
sudo xinsightctl source-facts resume --stream-id UUID --revision REVISION
sudo xinsightctl source-facts revoke --stream-id UUID --revision REVISION
sudo xinsightctl source-facts worker-stop
```

pause/revoke 与页提交串行：已取得提交许可的页先完成，停止操作随后成功；停止确认后旧在途页不能提交。停止可容纳等待期间发生的正常页进度；启用保持精确版本检查。revoked 不复活，范围变化用新登记，保留原流和证据。

401/403、409/410、来源范围/合同错误和不可变事实冲突会留下 `requires_attention`。修正后由管理员执行 `resume --acknowledge`；worker 重启及单步执行都不能自行清除标记。短暂网络失败按 profile 独立退避。每轮每个就绪 profile 至多读取一页，避免持续分页占用全部机会。

```sh
sudo xinsightctl source-facts resume --stream-id UUID --revision REVISION --acknowledge
sudo xinsight-importer unirouter pull-source-facts --stream-id UUID
```

单步与服务使用同一登记、配置和提交校验。旧 `--endpoint/--key-file` 参数仅可作为与受管配置一致的断言，不能覆盖配置。手工运行 importer 需显式 importer DSN 文件；systemd 运行由 credential 提供，不需要 admin DSN。

同范围 source key 轮换由管理员更新文件并停止/重新启动服务，让 `LoadCredential` 重新加载；不清空游标。scope generation 改变须新登记。初始空游标只进行空 bootstrap，记录当前位置后向前接收。

## 独立 offline 授权

离线不要求 live 登记或来源 key，不发现能力、不连接来源。使用 `deploy/source-facts-offline.authorization.json.example`，由管理员将其中目标、机器摘要、产品版本/commit、固定协议与来源 schema、精确来源范围、批次路径/摘要及操作身份替换为当前值。

A 三种 profile 保持 `xinsight.source-facts-offline-authorization.v1`，不添加新字段，已有授权摘要不变。B 五种 observation profile 使用明确的 `xinsight.source-facts-offline-authorization.v2`，当前 D 集成候选必须另设 `target_database_schema: "36"`，source schema 摘要为 `sha256:ca0aabfb0c7b40c00cfb910e6bd255691326883b98b38b174afc86f2c4df2260`，批次 connector version 为 `2.0.0`。v2 只接收 B；单份授权和批次不能混入 A。登记、check、预约及完成/重放检查授权所绑定的实际当前 schema（本候选36）、非 dirty 且无缺失 checksum 的迁移账本；schema35 的旧 B 授权仍可作为恢复历史校验，但不能在 schema36 重放或新增导入；必须另建绑定36和当前目标/产品的授权，不能自动扩权。

授权文件与批次文件均须 root 所有、0400/0600、单链接普通文件。授权时间为微秒 UTC，最长两小时，必须当前有效。`machine_id_sha256` 对 `/etc/machine-id` 去除首尾空白后的字节求 SHA-256；`bundle_sha256` 对批次完整原始字节求 SHA-256；摘要均带 `sha256:` 前缀。产品信息取本次执行二进制的 `version --json`，不能填另一候选的身份。

批次须是 Foigent 2.0.2 EventBatch，单 profile、最多50条、严格小于8MiB；`next_cursor/high_watermark` 必须为空、`has_more=false`。来源文档与标准化事件均重新严格校验。只有 metadata，不能带 raw payload；文件中的 `max_records` 是本次授权上限。

```sh
sudo xinsightctl source-facts offline-register --authorization-file /absolute/offline-authorization.json --target-ref TARGET
sudo xinsightctl source-facts offline-status --authorization-id AUTHORIZATION_ID
sudo xinsightctl source-facts offline-enable --authorization-file /absolute/offline-authorization.json --target-ref TARGET --revision REVISION
sudo xinsight-importer unirouter import-source-facts --authorization-file /absolute/offline-authorization.json --target-ref TARGET --check
sudo xinsight-importer unirouter import-source-facts --authorization-file /absolute/offline-authorization.json --target-ref TARGET
sudo xinsightctl source-facts offline-disable --authorization-id AUTHORIZATION_ID --revision REVISION
sudo xinsightctl source-facts offline-revoke --authorization-id AUTHORIZATION_ID --revision REVISION
```

登记默认 disabled。`--check` 独立预检精确输入、当前登记及已有操作绑定，返回 `status=checked`；只使用只读数据库事务，不创建预约、事实、回执或修改任何live状态，不读取来源凭据或连接来源。预检仍需可达的本地PG及importer DSN。成功仅反映检查时的状态，不获得后续导入许可，也不预演事实写入；实际导入仍会重新校验授权及事实冲突。可在导入前或完成后重复检查。

导入返回准确完成回执；同一 operation/input 重试仍须当前授权，返回原回执。改变绑定不能重用操作 ID。合法准入后的失败可能保留不可变操作预约；事实和 completed 回执在同一事务内提交，回执失败不会留下部分事实。取得所需锁后用另一条数据库查询读取当前时间，避免使用等待授权行锁之前的时间；预约、完成和回执重放均重新验证。已获本次提交许可的事务可完成，撤销等待该事务结束后禁止后续提交。

离线导入不修改 live 状态、错误、revision 或游标；live 与 offline 的启停和撤销相互独立。两者共用不可变事实及修订链校验，重复事实不重复新增，矛盾字节拒绝。导入许可不授予人员阅读权限。

## 升级、恢复与可见状态

31→32 保留原登记、游标和阅读，增加独立离线授权、管理记录和持久需处理标记；不新授予阅读权。升级/回滚先停止受管 worker，再沿原维护流程操作数据库；兼容的新运行时恢复原已运行服务，不支持的旧运行时保持停用。

32→33 增加 B 独立不可变观察及运行阅读、五种默认拒绝的人员字段授权，并允许独立登记 B 采集/离线授权。原 A 事实、金额、阅读和 v1 离线摘要保持原义。B 观察本身不新增经济事实。

33→34 更新人员授权非空约束，允许仅授予 B 字段，不要求顺带授予费用、覆盖或偏好能力；旧授权的含义不变。

B 五字段可以独立授予现有工作台受限读者，不要求同时授予费用摘要或详情。未获得费用权时，运行请求与租约按其 B 范围读取，A/经济关联继续按原权限拒绝；费用项目筛选也要求对应费用权限，不能用筛选结果探测隐藏关联。费用撤权使包含旧关联的运行阅读失效；新阅读只保留当前有权的 B 集合。覆盖状态另需 `allow_coverage`，B 字段不自动授予旧执行、复核或费用入口。

真实恢复只修改新恢复库：非 revoked 的 live 变为 paused 且需重新确认；offline 变为 disabled；revoked 保持 revoked。事实、回执、游标和历史阅读保留。恢复检查本身只读；恢复后启用需要当前管理员授权及预检，不能由旧进程或旧授权自动恢复。

工作台的“来源观察与覆盖”分别显示经济事实与当前有权读取的来源 profile 状态、范围、最后成功接收页和错误。需处理标记也保留在创建阅读时的覆盖观察中。停止采集不撤销历史阅读权，人员或字段阅读授权变化仍按现有规则立即检查。空页/游标不证明历史完整，计费依据不补造费用。

## 共用路径与兼容

同一 profile 的 live 与 offline 共用不可变校验和写入：A 使用 `insertSourceFactItems`，B 使用 `insertSourceObservations`。受管 offline 使用 `WithOperationTransaction` 将事实和回执放入同一 SQL 事务。原 offline 消费者继续使用 `WithOperation` 的独立预约语义。批次严格解析共用 `ValidatePortableEventBatch`，原 `ValidateEventBatch` 保留既有具体绑定接口；未复制通用协议、回执 ledger 或重试框架。


## 内部资源领域验证（schema35）

`xinsight.resource-access.internal.v1` 是仅用于明确合成数据的类型化资源领域，独立于 A/I01/B 生产者合同。没有公开导入、worker 或 CLI 写入口；内部验证方法同时要求 synthetic 标记和一次性合成测试库。资源记录保留报告来源与目标请求的完整来源、实例、组织、项目身份，版本链与原始摘要不可覆盖，不生成经济事实。

读取使用同一 B 阅读和请求的 `GET /api/v1/runtime/readings/{reading_id}/requests/{request_ref}/resources`，CLI 为 `query runtime resources --reading ... --request-ref ...`，认证与查询权限复用原人员会话。须有目标 B 请求权限，以及报告来源/项目的独立 `allow_resource`；费用项目权限不替代资源权限。分页上限100，每项最多两个冻结的目的地观察引用作为相符/冲突见证，按完整请求身份、attempt kind 与 attempt ID 比对。

所有权、分类、设备纳管、来源读取权限、外发许可分别保留未知状态。读取成功或 opaque 输入引用相符，不等于已验证实际内容、脱敏或发送；拒绝一次读取也不排除历史副本。修订晚于阅读截止不会改写冻结记录，已有更高版本时标记需要重新评估；新阅读采用新的完整修订链，撤回不删除原始事实。资源权限变化使旧阅读失效。schema34 的无资源阅读保留原摘要算法，不追补资源。

schema35 的两个新表和资源授权字段纳入原有角色、基线及恢复验证；生成合同仍需由原生 PostgreSQL 生成并与候选绑定。此本地域通过不代表真实企业、PC 或 I-03 来源接通。

D 的当前预算快照使用独立登记与非事件接收路径，见 [全周期预算快照接收与查询](managed-source-budgets-v1.md)。旧 v1/v2 配置不会自动开启预算；显式 v3 才包含 budgets 配置。
