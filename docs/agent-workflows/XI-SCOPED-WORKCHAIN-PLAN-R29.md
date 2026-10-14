# 正式账号的费用→调查→复核→返回：可实施方案（review30，仅规划）

2026-09-10。经理 review30 已接受 UI Local 候选 `6034983c66293dc7338c5643e39c385b7ead38d4`（tree `9b76af1b8784d531121bfb3d437628fde321fec4`）。本方案替代本文件在6034983中的初稿；按经理要求选定技术路径，待经理合并审阅一致后才实施权限代码。原13项、完整工作链和TD不因UI通过而关闭。持续授权覆盖本地规划、诊断、提交及主动回报，不扩展到真实账号/凭据、部署、UR或额外writer/模型。

## 1. 正式入口：保留单Role，新增两个显式受限角色

选定 `xinsight.local-accounts.v3`，仍是一账号一个Role，v3也接受原六种角色；继续完整支持v1/v2的既有规则。新增 `workbench_investigation_reader` 与 `workbench_investigation_reviewer`，只在v3可配置。配置解析保持严格、未知schema/角色/字段拒绝；不增加第二份能力配置，不自动转换旧账号，也不将产品角色映射成新的PG角色。

| 正式角色 | 固定能力 | 仍需逐资源判断 |
| --- | --- | --- |
| workbench_investigation_reader | 既有四项workbench权限；新增investigation.scoped.read；调查个人筛选 | 当前subject/grant、资源范围及字段可见性。保存自己的偏好不属于复核写。 |
| workbench_investigation_reviewer | 上述能力，加investigation.scoped.cite / investigation.scoped.write | 所选依据与整个案件可授权，现有CSRF、版本、幂等及业务约束仍通过。 |

不授予这两个角色全域 `evidence.read/evidence.cite/investigation.write`，避免遗漏的新handler因旧权限名称意外放行。`access.go` 为其先执行精确方法/路由表，只有已接入范围检查的handler可进入；旧六角色继续走原权限分支。前端按新能力显式接入可用页和动作，不伪造Session权限、不只隐藏菜单。Local正式账号是本包的正向主体；OIDC、服务身份/Bearer和管理功能维持当前含义。

依据：`internal/access/accounts.go:21/135/245` 定义版本、严格单Role解析和固定Permissions；`internal/api/access.go:211` 的旧evidence分支覆盖多组全域入口。用现有单Role足够表达只读/复核两个档位，单独能力配置没有必要。

## 2. 授权权威与可信关联

授权唯一来源仍是当前Session主体/固定角色以及现有 `workbench_access_grants`。每个请求在服务端建立不可由URL/body伪造的ScopeAccess，绑定subject、role、能力、grant的Revision与SourceRevision、投影版本。SQL范围谓词和结果投影使用同一对象；专用store方法强制接收它，缺少范围时拒绝，不能回退调用旧全域查询。

- 费用跳转先校验owned reading、有效期、A0/A1解释、当前授权修订，以及该经济成员确实属于reading。阅读ID/返回路径只用于保留上下文。后续案件持久性不依赖reading永不过期：案件每次读写重新检查其资料范围；返回费用仍须校验原reading，过期时显式新读，不悄悄换分母。
- 关联沿 `workbench_reading_members → workbench_economic_facts.evidence_event_id → evidence_events` 的实际主键、hash、source和规范xe1身份核对。`workbench_projection.go:112` 的xe1由source_system_id和request_id共同生成，source_project取project_ref；不是单独按request ID找来源。一个source内同request但混合项目的全部成员也须核对，不能以首个获权锚点放行整个execution。
- 来源项目与费用项目分别匹配。当前completed-execution投影没有给CostProjectRef赋值，不能补造费用项目。项目限定grant不匹配空/未知项目；明确source-wide的grant才可覆盖该来源中未知项目。多个grant只在各自完整scope匹配后合并能力，不能把A项目的details与B项目的字段许可拼起来。
- 对execution的每个Event、关联Gap及拟返回的关联节点，核对其实际source/project与经济锚点；需要费用项目约束而没有可信映射时不可跳转。缺少锚点、歧义、hash不符或成员跨范围时拒绝该execution，不返回部分成员的“完整”计数。允许的列表在分页/统计之前应用同一谓词。
- A1资料沿 `workbench_sources.go:47` 的实际source/version/source_ref/request/project/org连接及当前manifest读取，不把source-event修订当成经济修订；只保留已有已授权profile。费用成员不能给同request的其他来源资料授权。

## 3. 明确路由边界和旁路处置

| 路由/动作 | 本包受限角色的处理 |
| --- | --- |
| /api/v1/workbench/*及本人的保存筛选 | 保持原reading/grant/owner/版本检查，纳入新的撤权同步；每条费用依据给出是否可进入调查的服务端结果。 |
| /api/v1/executions搜索、规范xe1详情、timeline、investigation、events/{uuid}、gaps | 支持范围SQL和字段投影。ID与所属execution须同时核验，计数和游标不能来自全域结果再在前端过滤。旧request-ID详情/events/gaps分支对新角色一律403。 |
| trace-journey、compare | 进入同一检查；compare两端各自通过。跨execution的Journey只有全部拟返回节点/边可授权时返回；不能证明时返回无节点/边/隐藏数量的不可用状态，正常详情不受影响。隐藏关联不称作“来源缺边”。 |
| /api/v1/sources | 只返回当前可访问来源的必要显示字段；不带全域执行/导入计数或连接器内部状态。 |
| cases列表/详情；investigation/queue及queue-filters | 先判断整案可见，再筛选、计数、排序和生成游标。queue版本包含ScopeAccess完整修订，保留原固定评价时刻/版本与409恢复。 |
| security/reviews及preview、案件execution/finding/evidence/gap/annotation、finding review/assessment/history | 支持下述案件规则。只读角色不得写；预览也检查可见性。写入、当前依据解析、目标案件、重复/跟进关联和最终范围标签在同一事务完成。 |
| security/assignee-candidates | 仅复核角色可用；列出对当前整案有权且可承担复核的正式主体，不能借目录列举其他范围用户；停用/不可用负责人显示通用状态。 |
| 裸POST cases创建空案；seal、evidence-pack.json/html | 新角色拒绝。新案通过含至少一个可授权依据的原子reviews创建；已有完整封存/导出协议继续服务旧角色，受限视图不伪装成原始签名包。 |
| posture、connectors/connector-control、sessions、alerts、discovery、execution feedback/versions、旧saved-views、gaps全域resolution写、access/service-identities管理 | 本包不为新角色开放；这些现有聚合/跨域/管理路径尚未接入所需范围。其旧角色行为与入口保留。 |
| 其他、未来或变体API | 默认拒绝；对编码斜线、重复分隔、路由前缀伪装及方法变体做真实HTTP反例。公共健康检查、登录/退出/本会话和静态UI不承载这类业务越权数据。 |

代码落点：`server.go:246/403` 的注册和旧request-ID分支；`investigation.go:118/174/322/553` 的详情/投影/事件/对比；`workspace.go:42/83` 的全域案件；`investigation_queue_store.go:47` 的候选查询必须在LIMIT之前接入范围。以后要开放表中未支持入口，须先完成同等范围检查；本包不把这些功能向新角色开放作为“默认兼容”。

## 4. 字段投影、原始依据和哈希

范围允许后才投影，建立显式字段白名单，不用删几个敏感键后透传。现有EventDetail会序列化Correlation、Identity、Decision、Usage、Source等分组，不能视为已按grant裁切（`investigation/model.go:285`）。F0费用口径与A1来源profile权限分别保留：allow_summary/details允许的已知金额不能被重新解释为allow_source_cost；后者仍控制来源cost-basis及相应新增调查字段。

| 可见层 | 字段规则 |
| --- | --- |
| F0已许可的基础阅读 | 规范execution/Event ID、原始event_hash、时间、受控类型/状态，以及既有获权request/model/金额口径；不由这些字段推导操作者或业务目的。 |
| allow_source_context | 白名单中的来源上下文、模型/provider及已授权声明关系；自由格式source_ref、decision reason、Gap描述/解决说明等另按下述保守规则处理，不能透传任意extensions或URL。 |
| allow_source_identity | actor/owner/org/project/team/key/end-user等身份字段；未获权时值不出现在响应、标签、工具提示、搜索候选、引用摘要或DOM中。 |
| allow_source_cost | 来源cost-basis和对应Usage成本细节；未获权时缺省并标记withheld，不填0或冒充来源未采集。F0已授权金额保持原定义。 |
| 任意原文、扩展对象、URL、历史自由文本 | 不自动因一个profile位获权。已知、可确定分类的字段走白名单；不可分类的字段默认扣留。Gap等锚定资源的自由文本保守要求同资源context、identity、cost三项全部获权；无可信资源范围的文本不可见。 |

M0 Event、source document、原始hash、SourceRef快照及现有不可变receipt原样留在存储中，不修改内容、不重算成投影hash。新角色响应带明确的authorized_projection版本/withheld说明；`event_hash`始终指向原事件，不宣称客户端可对裁切JSON重算原hash。引用提交仅收规范ID/hash/选择，服务器在事务内解析SourceRef和当前依据，不要求客户端提交被隐藏值。预览明确可见依据范围；某引用的必需复核字段不可见时不可选入提交，不能只持有digest就绕过可见性。旧角色v1协议/原包及校验不变。

## 5. 空案、混合案、自由文本与历史

仅凭当前引用可见仍不足以授权旧案件自由文本：旧创建者可能在全域视图下填写标题/说明/批注，历史也可能保留已撤去的依据。因此选择schema30新增一张 `investigation_case_scope_labels` 资料标签表，而不自动给旧案补标签。该表只说明数据要求，不给任何人授予权限；最终权威仍是现有grant。

标签按case_id记录scope schema、来源/来源项目/费用项目要求集合、必需profile集合、labeled_case_version及对案件自身持久内容的digest。覆盖标题/说明、Finding、链接选择、批注、复核/责任历史和后续/重复关系；不把动态重读的来源结果混入内容digest。现有 `touchOpenCase` 被主要案件/复核写调用（workspace_store.go:813、finding_assessment_store.go:315/435、finding_followup_store.go:211）；版本加内容绑定防止遗漏写路径静默保留旧标签。

- 新受限案必须由原子reviews创建，至少一个可信且获权Event/Gap依据；创建空案或空依据Finding拒绝且无残留。新案的人工文本继承整案资料要求；这是明确的资料分类边界，不声称能自动识别人为粘贴文本的真实来源。
- 受限写每次验证现有标签、整案全部当前及历史依据和跨案关系，复用当前案件锁/版本约束，再以新内容和累计要求更新标签；范围与字段要求只能累积，不能通过移除当前链接消除历史限制。actor可见A/B也不能把B链接写入仍标A的案。
- 新角色对无标签旧案、空案、来源/项目未知案、标签过期/内容不符案，以及不能完整覆盖的混合案，整案不可见/不可写。列表不包含其标题、数量、存在提示；已知ID返回与不存在一致的404。旧角色对这些案的读写语义不变，本包不偷偷迁移旧案。
- 旧全域路径仍可修改一个新案，但不会更新其受限标签；版本或内容变化使新角色失去可见性，不能自动按剩余引用恢复。后续要重新分类必须是有明确证据的独立受控动作，本包不增加自动重标入口。
- 受限详情、队列、历史、关联预览和重试receipt使用同一整案判断；成功写过也不能在撤权后重放幂等receipt取回隐藏内容。封存/导出对新角色关闭，避免裁切历史后继续冒用旧包digest。旧数据、旧包及失败候选都保留。

## 6. 并发撤权与恢复的具体边界

选择数据库原生事务 advisory shared/exclusive 锁，覆盖新角色的完整业务请求与现有grant变更；不用进程内互斥假装能同步外部数据库操作。一个固定grant锁即可：读者共享；grant的INSERT/UPDATE/DELETE/TRUNCATE通过schema30的BEFORE STATEMENT触发器取排他锁，先于行锁，避免多subject排序系统。授权仍由表内容决定，锁不授予权限。PostgreSQL提供共享/排他及事务结束自动释放的advisory锁；所有参与路径必须遵循协议，触发器用于覆盖现有SQL变更入口。[官方锁语义](https://www.postgresql.org/docs/16/explicit-locking.html#ADVISORY-LOCKS)，[事务共享锁接口](https://www.postgresql.org/docs/16/functions-admin.html#FUNCTIONS-ADVISORY-LOCKS)。

review30实施裁定的两个并发反例按以下路径解决，保证与原计划一致，不扩池或延长超时：授权事务只使用现有reader池，业务只使用现有workspace池，始终reader→workspace；两者若指向同一个sql.DB池，在借连接前拒绝新受限入口。生产原本分别Open两个10连接池，无新池/角色。受限调查的只读查询复用workspace池已有SELECT权限，使用专用只读接口，不能写M0；旧全域调查仍走原reader池。现有Workspace方法自身的业务事务照常从workspace池获取一条连接，不再套同池授权事务。任何持workspace连接的路径都不反向等待reader授权池。以两池各1/2和原10容量、超过容量的并发、取消/错误验证完成及释放，且确认旧角色行为不变。

授权事务为READ COMMITTED且只读：先LOCK TABLE workbench_access_grants IN ACCESS SHARE MODE，再单独执行共享advisory锁，最后用下一条语句读grant。DML/ TRUNCATE本来先取得各自表锁，触发器随后取排他advisory锁，因此所有参与方都是表锁→advisory；TRUNCATE持AccessExclusive时，读者尚未持advisory，不形成反向等待。保持原TRUNCATE能力，用受控屏障证明完整提交、撤权后拒绝、DML/回滚及取消无死锁，不把死锁回滚当成功。

业务队列/阅读原有REPEATABLE READ事务在授权锁取得后才开启，避免等待锁前的旧快照。授权事务覆盖业务提交和有界响应构造后释放，不等待客户端传输。Session/Role在提交或响应发布前用现有access Service互斥保护的短临界段重查，与同服务会话撤销排序。撤权提交后取得授权锁的请求拒绝旧范围；此前已授权构造的响应可完成，不承诺收回已发数据。保持既有reader/workspace权限，不给reader grant表UPDATE权；取消、锁/存储错误或同池配置都失败关闭。

前端导航、返回、pageshow/重新激活以及业务请求恢复前重新读取Session与范围修订；角色/grant变化、过期或403/授权冲突时清空缓存，异步旧响应按本地请求世代丢弃。没有收到撤权事件时不声称静态页面瞬间得知外部变化，不添加轮询守护。当前配置/provider和Session是启动时内存状态（sessions.go:166、subjects.go:30）：配置换文件不代表热重载。角色变更按支持的服务重启/会话重建生效，旧cookie失效；单会话撤销仍走现有接口。

## 7. 支持前态、schema和恢复

- 账号：v1/v2配置在新版本仍按原含义工作；v3仅显式新增角色。旧二进制拒绝v3，这是预期的fail-closed。恢复优先使用新二进制+保留的旧v1/v2配置，重建服务使内存会话失效、重新登录；保留v3文件，不复用旧Session或静默将新角色降为全域investigator。
- 数据库：选定schema29→30的加法迁移，只新增上述标签表/必要索引和grant同步触发器，更新现有schema合同、权限清单、备份/恢复与check入口。既有来源、经济事实、A0/A1 reading、Finding receipts和原包不改写；旧案没有标签而不是被错误授权。标签支持版本和完整性检查，失效标签保留为不可用于受限读取的状态。
- 迁移覆盖空29、真实已支持29旧案/旧reading/封存/撤权grant前态、新30带受限案、损坏/未知标签版本、重复迁移拒绝及native角色负向场景。已有check/dry-run验证无副作用。30→29只允许新增表为空的隔离回退验证；存在标签/新案时拒绝删表降级。若必须跑旧二进制，使用完整29备份恢复到新的隔离库并恢复旧配置，明确该快照不包含后续30新增工作；保留30现场，推荐前向修复而非丢弃新记录。
- A0/A1旧解释、owned reading和个人筛选不迁移语义；grant变化使旧reading失效，保存偏好重新求当前交集，原409/草稿恢复不变。新角色的案件持久标签随现有备份保留，不依赖临时reading作长期授权。受限功能关闭时，新资料保留，旧全域功能继续可用。

## 8. 完整实施包、验证和资源

这是一个完整子包：账号与精确路由边界、scope-aware SQL/字段投影、案件标签/撤权同步及schema30、前端同账号工作链、原生分权/恢复/回归一起交付。内部先核对最小真实前置场景后推进，无逐实验回报或审批；不可变制品和完整回交放在验证稳定之后。未改的M0/hash、原包和A0大负载证据可复用；改到的权限、迁移、恢复必须重新实测。

核心验收使用Production HTTPS Handler、PG16原生三角色、v3正式配置的一个 `workbench_investigation_reviewer` 合成账号：仅授来源A/项目P及所需字段，费用→获权执行→原子建立复核→更新/历史→原阅读，阅读/金额/分母/选择与返回不漂移。独立只读账号验证所有写请求拒绝。浏览器验证最终资产、窄屏和明暗适用页面；接口及DB层覆盖同request跨来源/同来源跨项目、未知/费用项目错配、篡改ID/hash/reading/游标、缺字段、跨案/混合历史、空案、标签失效、旧角色写后失效、撤权及幂等重放。Journey/对比所有节点与两端分别验证，旧request-ID和表中关闭入口逐项真实HTTP拒绝；路由表有防未来默认开放的测试。验证500/2001量级混合授权队列的计数/分页/版本与适用性能，不重开A0资格大窗口。

前端保留既有单元/覆盖门槛并新增工作链与撤权竞态；Go对受影响access/API/DB/workspace和schema/native/restore执行必需测试、race/vet及对应覆盖门槛，稳定后一次完整必要回归。安全边界较上包广，经理独立复验应包括正向正式账号、旧入口旁路、字段/混合案、真实撤权并发、迁移恢复和最终资产，而非只看截图或测试数。

2026-09-10 12:01 UTC沿原guard实测：C=4,433,649,664B（约4228MiB），原上限4672MiB余量约444MiB；A0=16,008,915,332B，free=25,033,445,376B（约23.31GiB），RSS=0。原起点与经理review目录均计入；证据为 `output/xi-ui-overflow-r29/review30-plan-resource.json`，本轮未改guard或上限。

| 完整包新增占用估计 | 上界估计 |
| --- | --- |
| 代码/Go缓存和编译临时增量 | 576MiB |
| 本次隔离PG、schema29/30升级恢复及保留前后态 | 192MiB |
| 前端生成物、浏览器profile和自测证据 | 128MiB |
| 经理独立复验及保留证据 | 256MiB |
| 错误现场和估计余量 | 128MiB |
| 合计，从当前累计值再增长 | 1280MiB |

现有余量不足覆盖完整包。建议经理一次将C上限增加1024MiB至5696MiB（5,972,688,896B），原累计起点不变；按估计C最终约5508MiB，仍留约188MiB，free约22.06GiB、A0按最大共享缓存增量仍低于18GiB。实施/经理重操作依次使用既有guard，预计RSS 2–3.5GiB，保持4GiB硬界；free>21GiB和所有旧现场保留不变。这是整个子包的估计而非额度已获批准或峰值保证；实施前重读会漂移的现场，经理审阅时同时决定资源增量，不先改额度、安装依赖或制造试验负担。

经理 scoped-workchain-implementation-decision.md 已批准整包实施和C增量；上述两项处理先写回本计划后连续实施。仍无真实账号/凭据、外部写操作或UR权限；完成整个子包后统一回交。
