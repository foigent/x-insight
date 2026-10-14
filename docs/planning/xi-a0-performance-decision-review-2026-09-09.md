# XI-A0 创建性能：可审阅决策输入（仅设计，未批准）

基于 `1ffd5de985edfd544bcef5cf2d347f6a0c42256e`。本文件补充并纠正 `xi-a0-blocked-performance-design-2026-09-09.md`；旧文和三份失败报告保留原字节。本轮只读检查现有源码/SQL/测量，未跑新候选、未编辑或执行DDL/DML，没有实施或拆分重试2026-09-08 22:44 UTC被拒绝的“immutable economic fact + narrow membership”。

## 建议与待决定事项

先修补独立的UI问题。性能上没有已验证满足目标的安全替代，不能承诺一次重构必达标。建议对本文件所列“冻结A0事实、保留确切引用和解释版本、窄成员”的整体语义作知情决策；若接受，才允许在隔离合成环境构造候选、量化验证，达不到门槛即保留失败证据并退回基线。未接受前仍保留宽快照，A0持续不通过，不再反复跑无新依据的全量基准。

需用户接受的实质变化是：被reading引用的经济投影从可重建缓存变为须保留身份/版本的持久事实锚点；不可变规则、引用恢复和A1解释兼容成为读取正确性的前提。不是简单删除重复列，也不是自动审批已经允许的查询优化。

## 当前实测与可比较替代

以下均为已有Local PG16.14资料，不是本轮新试验或生产结论。

| 路径 | 实测依据 | 判断 |
| --- | --- | --- |
| 宽快照基线 | 20×300k：带维度索引p95=22495ms；后续wide diagnostic p95=19848ms | 未达5000ms，继续保留为恢复基线 |
| 去三个维度索引 | 同口径p95=24722ms，已经恢复索引 | 实测更差；不再以此为候选，也不牺牲查询能力 |
| 只优化输入查询/count/digest/coverage | 首次成员INSERT14945ms，count44ms/digest123ms/coverage556ms；EXPLAIN INSERT总16614.580ms，输入SELECT约774.8ms | 查询侧成本相对较小；即便消除这些测量项，也没有证据接近5秒。阶段/plan是不同测量，节点含子节点，不能直接相加或推算精确优化后p95 |
| 保留全部快照，仅调整FK校验/批量路径 | 四FK累计6762.325ms/各300k次，其中fact FK4033.294ms；已有INSERT…SELECT已是集合写入 | 约束成本值得定位，但不存在“把逐行应用循环改成批量”的现成收益。COPY、延迟FK等未实测且仍要完整验证，不承诺解决；不移除约束、不关触发器 |
| 压缩宽快照/局部索引设计 | 成员表累计约7.58GB，其中索引约4.17GB（多次reading累计，并非单个） | 没有压缩候选的测量；可能增加解码、索引和维护成本，只能列为未验证假设，不能冒称更安全且已达标 |
| 窄成员+冻结事实 | 可减少每次reading重复写入的15列与部分直接外键/维度索引；尚无候选实测 | 与写入成本诊断方向相符，但改变持久语义，读时join与事实索引可能转移成本；须整体批准和实测，不能预报≥4倍提升 |
| 预计算/跨主体缓存/异步202 | 当前reading要求精确授权、cutoff、不可变membership且POST完成才返回reading | 需新的失效、任务、取消与恢复协议，无现成路径/性能证明。复杂度与语义代价更大，本轮不推荐 |

同步写入、fsync、full_page_writes、WAL、约束、20×300k/真实500k/500001和10主体20RPS负载不降级。已有500k Store耗时29527ms；已有HTTP读p95=76ms仅证明预建reading查询。

## 窄成员候选：所有字段的归属

当前 `workbench_reading_members` 恰有18列。候选只保留前三列；以下是其余15列逐项的归属，不是已执行的迁移。`F0`表示现有fact ID对应的冻结A0经济事实内容；在本候选中它本身不可改，不是查询“当前最新版本”的别名。

| 当前成员列 | 候选归属/读取 | 不变量 |
| --- | --- | --- |
| reading_id | 成员保留 | 指向同一owner/period/cutoff/expiry/授权revision的header；生命周期不变 |
| ordinal | 成员保留 | 原 `(occurred_at,fact_id)` 排序和已分配ordinal不变；分页/游标不可重排 |
| economic_fact_id | 成员保留、FK至F0 | 同一business identity的稳定ID；不是每次重建分配新UUID |
| evidence_event_id | F0.evidence_event_id | 保留当前已选定引用，不因后续adapter/replay/排序更小的UUID改指针 |
| source_system_id | F0.source_system_id | source身份及FK不变；当前权限仍按这一身份校验 |
| source_version | F0.source_version | V1来源契约版本不改写，不与metric/解释版本混同 |
| source_event_hash | F0.source_event_hash | 来源事实hash，不用M0/Envelope摘要替换 |
| event_hash | 新增F0锚定Evidence的event_hash | 取且仅取F0.evidence_event_id对应的M0 event_hash；不是后续adapter最新hash |
| request_id | F0.request_id | 精确请求身份，不与adapter/delivery epoch组成费用identity |
| execution_id | F0.execution_id | 原已验证来源+请求的执行identity保持 |
| model_ref | F0.model_ref | 冻结A0投影出的模型值；后续mapper/解释修订不覆写旧F0 |
| source_project_ref | F0.source_project_ref | 冻结A0直接project_ref；不得从来源当前项目/最新归属动态join |
| cost_project_ref | F0.cost_project_ref（A0无来源，NULL） | 旧A0仍unavailable/未归属；不能在A1上线时原地填值导致旧reading改账 |
| terminal_status | F0.terminal_status | settled/cancelled语义固定；不按当前执行表最新状态重解释 |
| occurred_at | F0.occurred_at | UTC事件时间不变；选员、排序、趋势一致；不改为ingested_at |
| gateway_charged_micros | F0.gateway_charged_micros | numeric(30,0)非负整数，CNY/scale=1e6；独立于calculated |
| calculated_reference_micros | F0.calculated_reference_micros | 独立来源金额，不回填为charged，不浮点重算 |
| currency | F0.currency | CNY不变；金额基准仍取reading.money_basis |

F0还保留原source_ref（business key组成）、amount_scale=1000000和projected_at；它们原本不是成员快照列。source_ref不换成Evidence UUID。projected_at只表示该投影形成时间，不是事实发生时间；恢复旧F0必须保留旧值，reading已保存的coverage不重算。

header的contract/metric版本、display_timezone、filters、money_basis、authorization_revision、interpretation_revision、membership_sha256、coverage、cutoff/expiry全部保留。成员hash算法仍按ordinal串联 `fact_id:source_event_hash:event_hash`，因此迁移前后应逐reading完全一致。它不是对所有解释列的完整校验，不能仅凭该hash一致就声称值无漂移：迁移预检及恢复验收需额外逐列对照上表及摘要/合计/明细。

当前成员直接到Evidence/source的存在约束，候选经F0的NOT NULL FK传递维持；reading FK、fact FK、(reading,ordinal)主键和(reading,fact)唯一约束保留。F0对Evidence/source仍RESTRICT。移除直接FK/维度列索引属于被拒绝的整体设计范围，不能作为单独“安全优化”先做。维度查询将转为F0 join，需实际计划和峰值读取验证；没有承诺原三索引可无损消失。

## 选定Evidence、重复及冲突

须纠正“首次Evidence”用语：当前在线路径是首次成功建立business key投影的事务所选eventID；它不是可保证的最早事件时间或最早网络接收。schema23回填当前SQL是同business key按Evidence UUID排序选择，**没有按ingested_at排序**。候选只承诺保留“既有选定Evidence”，绝不借迁移重新挑最早引用。

- 对已经有F0和reading的本地前态：直接保留F0的ID/evidence_event_id。新增event_hash从精确引用读出；验证所有旧成员15列等于对应F0与锚定Evidence，任一不同即整体拒绝转换，保留宽表/旧读取，不丢列补齐。
- 对clean安装或23→候选：沿用现有business key去重、冲突拒绝和UUID确定选择规则，不修改已发历史迁移字节。若将来产品要求按接收先后定义“首次”，须另作兼容决定，不在此偷换。
- 后续同源同key、同经济内容的adapter重复：Evidence可有新M0 hash，但F0及选定引用保持；不加第二次费用。同key源hash或任何经济字段冲突：Evidence+投影事务整体拒绝；不更新F0来吸收冲突。
- adapter版本/epoch不是费用identity。对于同一业务事实不同mapper生成的经济内容变化，必须显式判冲突或新解释版本，不能无声覆盖。

## 解释版本与A1边界

当前interpretation_revision只是对固定说明字符串的hash；detail的account/application/purpose又是代码固定unknown。这不足以自动支持未来版本并存。候选必须把这个hash绑定成明确的A0解释版本调度：识别旧revision→读取F0直接字段并返回A0的unknown/unavailable；未知revision fail closed，不能套当前最新解释器。

A1新增归属是独立、不可变的解释修订，须带其来源/规则版本及确切修订ID，新reading显式绑定该修订；旧reading仍走A0。若需要基于新解释值筛选成员，成员选择、摘要、贡献、权限scope和详情必须都使用同一修订。不能只让detail join最新归属。A1的具体新表/映射UI仍属A1，不在本轮实现；本候选只实现A0版本保留/未知版本拒绝所必需的兼容，并保留原13项未知边界。若做不到这个明确边界，就不允许去掉相关快照列。

## 重建、恢复与旧路径退役

本候选不能同时宣称“F0可随意重建”与“reading只引用F0但永久稳定”。推荐的支持模型如下：

1. 投影暂存/核对可以从Evidence重新计算为候选值，但对已被reading引用的F0只能比较，不得DELETE/INSERT替换、UPDATE或TRUNCATE。受支持的修复是从确切备份恢复同一ID/引用/内容，或建立新解释修订用于新reading。
2. 完整恢复须包含Evidence、F0、reading header/membership及现有授权/保存偏好，保留所有身份和时间。新数据库恢复后验证引用、逐列一致性/合计、interpretation可识别、角色及触发器，再放行读取；授权仍以恢复后的当前grant为准，不因旧reading保存授权快照就绕过撤权。
3. 只剩原始Evidence、失去F0 ID或选定引用绑定时，不能保证恢复旧reading身份。明确拒绝旧reading恢复，不制造新UUID并冒称旧reading。缺Evidence、缺F0、内容或解释不匹配、损坏archive均fail closed。
4. 原宽表到候选的预检必须在精确前态和合成备份上完成；包括合法相同重复、冲突回滚、事务取消、部分损坏前态。新候选若需迁移版本/开发合同变化，先明确身份与预检，不重写rc.13/schema23发布合同。
5. 尚未获准的未来真实迁移不能用down脚本清空用户reading作为恢复路径。恢复应是已验证备份到新DB+匹配旧二进制；D-37-01维护/停机策略仍未決。本轮无真实升级、删除、清理或reset授权。
6. 宽列/其索引/旧SQL只有在上表所有字段、版本/权限/恢复与新查询验证都通过后才可在已批准隔离候选中一并退役；有差异就保留完整旧路径并失败，不保留一套长期双写兼容系统掩盖问题。Reading过期清理的正式生命周期仍需单独授权，不能顺带清空F0。

## 角色约束：能保证什么

当前roles来自`internal/hostctl/database.go`：reader只读；importer允许INSERT F0，workspace允许INSERT reading/member，header仅membership_sha256/coverage列UPDATE；F0没有赋予runtime UPDATE/DELETE。候选不扩大这些权限。

| 身份/路径 | 必须维持或新增的候选保证 |
| --- | --- |
| importer | Evidence/F0同事务INSERT及去重读取；禁止改既有F0、reading/member、grant；同key冲突回滚 |
| workspace | SELECT F0并建reading/member、既有header收尾/个人偏好；禁止改F0/Evidence/授权；无新的bypass函数 |
| reader | 只读；当前应用层subject/grant/ownership检查不替换为仅DB登录角色 |
| 普通管理SQL | 候选UPDATE/DELETE及TRUNCATE guard拒绝改F0，引用FK拒绝删除被依赖Evidence/source；schema contract检查准确函数/trigger定义、启用状态和owner |
| xinsight_admin超用户 | 当前安装要求其为superuser。触发器不是对恶意/任意超级用户的防篡改保证；其可通过管理能力绕过。只在受支持runbook约束和schema/恢复核验范围内保证，不声称阻止所有超级用户路径 |

PostgreSQL16允许管理触发器启停，session_replication_role还影响默认触发器和FK；不能用这些开关优化跑分。[ALTER TABLE](https://www.postgresql.org/docs/16/sql-altertable.html)、[客户端设置](https://www.postgresql.org/docs/16/runtime-config-client.html#GUC-SESSION-REPLICATION-ROLE)。候选guard应覆盖普通UPDATE/DELETE/TRUNCATE和所需启用模式，不能只防DELETE而遗漏TRUNCATE。超用户信任边界若不可接受，需要另审管理身份架构，不在本方案静默更换权限。

## 三秒HTTP与五秒验收

当前HTTP `withAPILimits` 对整个API handler使用3秒context；workspace角色statement_timeout=10秒（单SQL）、reader10秒、importer30秒。现有大容量Store测量使用管理员/合成数据库，不能证明真实workspace角色路径成功。读取role的10秒不延长HTTP3秒；HTTP3秒也不能保证所有请求都在3秒内成功。

本轮UI修补不调整时限。3秒是当前实现约束，不是用户新定的永久性能目标。原20×300k创建p95≤5000ms、真实production HTTP+workspace角色创建必须可用，两项要求都保留。现状仍须记录HTTP3秒context及SQL10秒时限，不能以管理员Store成功替代真实角色/HTTP结果；超时失败全部计入结果，验证取消和事务回滚，不只计算成功请求p95。

若获批的存储候选达到原5秒性能门槛，但与当前HTTP3秒预算不相容，应由双方根据完整HTTP延迟、容量、取消/回滚证据审查仅创建端点的合理预算；该调整属于原本地工程授权时，不再另设用户审批。它不是自动获准的随意全局放宽，更不能单靠延长超时算性能通过。500k支持上限仍需真实创建可用，500001保持422及零残留。改成异步202则涉及独立契约语义，不能从一次超时调整推出。用户知情批准仍专门适用于被拒绝的存储重构，不与这个时限解释混淆。

## 获批后的最小充分验证顺序（尚未执行）

- 首先证明逐字段兼容、选定Evidence稳定、business identity/重复/冲突、金钱、权限、解释版本、恢复负向；先让错误候选在小合成数据上失败，不先消耗全量基准。
- 再对与基线相同的300k、70000、500k/500001数据分布在相同PG配置下看真实INSERT/读取计划。开启durability和全部约束，记录四角色身份与statement_timeout；禁止用admin连接替代最终workspace验收。未来使用EXPLAIN ANALYZE INSERT属于写入试验，只能在获批自建合成DB执行，不混称只读诊断。
- 有足够改善依据才执行原20×300k最近秩p95、实际500k创建、500001 Store+HTTP422/六表零残留；生产HTTP记录创建请求全部成功/失败、SQL取消及回滚、完整耗时。10独立主体/reading/20RPS/30秒读取峰值保持；包含7天和30天趋势以及全部分页/详情，避免把写代价转给慢join。
- 同时核验旧reading与并发迟到事实、当前撤权、保存重放、A0解释不变；备份恢复后同一读数与引用。对比存储为每次reading增量及累计总量，不能拿累计表大小称单次。
- 最后生成真实PG开发schema合同、历史发布fail-closed和R3回归；交出精确候选diff/commit/tree、前后计划与全部失败报告。候选任一关键不变量或5秒/HTTP成功门槛不满足，就保持A0未通过。

这是一份完整的决策输入，不是已经验证有效的优化。当前暂停来源是实际自动审批拒绝：“broad migration redesign … removes many snapshot columns, adds an immutable database trigger, and changes core storage semantics”。需用户对上述明确语义变化知情批准；经理确认UI计划不替代这项批准。
