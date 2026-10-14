# XI接续与技术债复核（规划交付，不启动新产品批次）

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

当前重点仍为A0资源解决与资格准备。经理已接受最终普通入口5/5，不代表20×300k、HTTP或A0完成。以下按第38原任务映射整理独立可准备内容，供经理共同评审；本轮不实施A1/B/C/D/X产品变更、不联系UR，也不启用活动来源。

权威来源是PRIVATE_PERSONAL_ARCHIVE第38及其 `supporting/xi-kickoff-20260909/task-plan.json`、`supporting/implementation-v1/implementation-map.json`。这里只派生接续顺序和当前证据状态，不重写原13项目标、验收ID或源字段合同。源路径/哈希与原13项映射保留在 `output/xi-a0-resource-preparation/continuation-map.json`。

## 原13项如何继续

| 原目标 | 对应XI批次 | 当前证据／仍需完成 |
| --- | --- | --- |
| CT-01 能力登记、资格和发布依据 | B | 当前A0不覆盖；需合格capability来源及资格/发布依据链，COMP-A01/07/11保留 |
| CT-02 运行分母、attempt、过程与资源详情 | B | trace-journey是可复用代码，不等于新来源已资格；CT02/J/DATA-A/E01–10保留 |
| US-01 应用与活动用途 | A0/A1 | A0费用成员底座部分已验证；应用/用途当前unknown，实际活动来源正向链仍待 |
| US-02 来源账号、应用、当次操作者/委托 | A0/A1 | A0不推断当次身份；I-02/I-03及身份来源未资格不能启用 |
| US-03 用途候选、采用和纠正 | A1 | 未实现本轮解释修订/人工采用链；PURPOSE/XR-04–07保留 |
| SE-01 模型授权与资源访问不混淆 | B | 当前A0授权只管已知成员；resource/input/destination依据须来源及可见性证明 |
| SE-02 检测、决定、执行与影响 | C | 缺新合格观察/effect事实；不以人工结案推出风险消失 |
| SE-03 最小人工复核与处理历史 | C | 既有Case/Finding/封存可复用；新负责人与期限/追加历史/并发原验收未完成 |
| FA-01 费用口径、已知小计与缺项 | A0/A1 | 已有精确micros、固定成员、覆盖未知及模式候选Local证据；A0性能/HTTP及source补充仍未完成 |
| FA-02 来源归属、展示归属与分摊守恒 | A0/A1 | 既有source/model分组及金额守恒；应用/actor/成本归属/分摊修订仍待 |
| FA-03 同口径全周期预算 | D | 未完成预算定义/完整期间摘要来源；控制权在UR，XI只比较 |
| FA-04 独立账单及估算匹配 | D条件分支 | 只有合法独立账单来源被明确绑定时启用；当前不制造账单演示来关闭目标 |
| GX-01 范围、覆盖、保存、撤权和一致阅读 | A0/A1/B/C/D/X | A0授权/保存/失效/覆盖已有局部证据；全产品范围、真实来源/配对版本/客户工作仍待E验收 |

所有父级 `E-END-TO-END` 仍pending。原A—E依赖保留：B依赖A，C依赖A/B，D依赖A，E聚合A—D；独立本地准备可提前，不将局部合成结果上提为整包通过。

## 我建议的接续安排

首先完成A0同一最终代码下的20次全成功+p95<=5s、500k及增长后Store读取。随后依据真实耗时与取消/连接占用共同解决当前创建HTTP3秒预算，再执行完整HTTP/500001、10主体600请求/20RPS×30秒、summary/top各300及p95<=2s、300k分页/series/恢复等原检查。若资格再次失败，保留失败与物理状态，按阶段/自然checkpoint证据选单一下一切片，不把重试或512MB实验当默认修复。

资源解决期间，以下工作不存在必须等待真实来源才能讨论的依赖；当前只提交规划，实施仍由经理确定批次：

| 批次／建议顺序 | 可以独立形成的最小方案 | 复用落点与必要验证 | 明确阻塞的部分 |
| --- | --- | --- | --- |
| A1：A0稳定后首先衔接 | 解释修订作为独立不可变对象，包含来源/规则/采用者及确切版本；新reading显式绑定，旧A0不变。先定义人工声明/纠正及关系修订的最小事务 | `internal/db/workbench_store.go`已有A0解释版本拒绝分支；`workbench_projection.go`/`workbench_restore.go`保留F0锚。测试旧reading不变、unknown/过期/撤权、冲突幂等、金额/范围/详情使用同一修订 | I-02由UR正式产生；I-03实际活动锚/来源权限/版本未定，先做XI内合成契约，生产接入不启用 |
| B：A1范围与修订语义稳定后 | 同一执行详情中的typed relation和有界过程reading，区分父执行/attempt、资源访问/传输事实、进行中/准入前失败；未知保持明确 | `internal/db/trace_journey_store.go`、`frontend/src/ExecutionDetailPage.tsx`及既有journey tests；验证全到达、重复/乱序/迟到、分母不重复加费、关联不扩权、逐attempt目的地 | 新capability/runtime/resource/input/destination profile及真实资源来源未资格，不能把V1内容硬塞成新协议 |
| C：可先设计原子复核命令，落地接在A/B语义之后 | 负责人/期限/追加历史、依据变化后重评；读存量与期间新建分开。结案只表示人工状态 | `internal/db/workspace_store.go`、`internal/api/workspace.go`与Case/Finding/封存能力；幂等、乐观并发、封存拒绝、实际human+CSRF事务验证 | 未合格policy/effect来源不能形成真实安全闭环；不加自动阻断或越权处理 |
| D：与B/C方案相互独立 | 同口径期间预算比较DTO及守恒检查；父汇总与明细不相加，解释周期/货币/范围不匹配 | 复用workbench精确numeric/micros与reading范围，合成边界测试；不新增XI预算控制权 | 合格预算定义/全周期摘要须UR；FA-04等独立账单被授权后再绑定，当前禁用 |
| X：贯穿上述批次 | 来源观察与独立模式、共同导航、API/CLI同权、当前schema/role/升级/恢复输入；每批绑定增量证据 | `internal/hostctl/database.go`、schema contract/restore、正式OpenAPI及共享前端transport；适用迁移/角色/旧前态验证 | 跨产品SSO、版本配对、真实近实时、签名制品/安装/真实恢复需共同任务；D-37-01维护窗口仍未决，不承诺HA或零停机 |

以上是下一步方案，不是“未发现代码就假定未实现”的全仓审计。已定向读取上述复用点；其他候选API/测试清单应在批次开工时按实际差异补齐。原来源身份、Source/Envelope/M0 hash以及分页/页提交模型沿第38适配边界，XI不单方冻结新profile或改V1。

## TD-01—04：当前交付、保留与退出

| 状态／问题与代码依据 | 处理与已具备证据 | 原目标／退出条件或剩余问题 |
| --- | --- | --- |
| 已消除：旧GET重复transport；`frontend/src/api.ts` requestJSON现委托requestAccessJSON | 原A0交付已删除重复实现；原tests保留headers/no-store/same-origin/AbortSignal/global401/Problem，POST/PUT保留CSRF。此次回读确认委托仍在，不声称本轮重跑旧浏览器 | TD01/02/03、GX-01/PX-03/06；调用迁移已完成，旧API签名保留兼容，删除签名需无受支持调用者 |
| 已消除：当前host角色SQL数组与Go授权集合双份维护；`internal/hostctl/database.go` textArraySQL | 当前三数组由权威Go列表派生，database_test有实际表名单/rc10隔离断言；既有真实PG role/restore与schema25回归证据在storage-candidate报告 | TD01/02/03、GX-01/PX-04/06；新增表同步通过相同角色/恢复检查，不另建schema平台 |
| 已修补本轮缺陷：工作台异步结果和标签错配／失效处理重复；WorkbenchPage runView/viewGuard | 已接受UI修补有真实handler/browser与失败/撤权证据；当前源码按reading generation和view序号绑定成功，失效清除。明确这是新功能草稿缺陷，不冒称清除旧权限漏洞 | TD01/02、GX-01/XR；其他旧调查/导出权限不能由此宣布全仓安全通过 |
| 已完成候选替代：wide member重复字段→immutable F0+narrow member | migration25对前态15字段/摘要/ordinal校验，旧runtime宽拷贝及三维索引已移除；当前运行内建sha256和局部Exec/generic修补有真实角色/5次结果 | TD01/02/03；新路径定向可用、兼容迁移/恢复已有Local证据，原20次/HTTP性能资格仍不通过；不宣布整个A0替代验收完成 |
| 保留：rc.10历史权限常量；database.go rc10WorkspaceUpdateTablesArraySQL及verifyRoleBoundaries调用 | 为识别真实受支持rc10升级/恢复前态而独立保留，不可套当前表名单。旧schema23正式制品、migration24字节同样留存；当前schema25独立合同 | TD03/04、PX-04/06；只有明确停止支持对应前态且升级/备份恢复覆盖通过，才可另行退役。当前未获此决定 |
| 保留：A0解释分支、原Evidence锚与immutable事实；workbench_store.go/restore.go | A0固定字段和unknown含义保持，未知解释版本拒绝；恢复不从raw Evidence猜造身份 | TD02/03/04、FA-01/02、GX-01；A1必须提供显式新修订及旧reading dispatch/恢复策略。不能用latest映射覆盖旧reading来“去重” |
| 保留：V1/source snapshot/Envelope/M0及独立foigent bridge | 职责和规范化算法不同；第38已明确不合并。受保护Draft Gate为另一项既有工作，不是可清理缓存 | TD02/03/04、PX-03；只有真正的受支持替代及历史校验退出决定后才讨论移除，当前无此条件 |
| 待处理：创建尾部性能/HTTP预算、成员保留生命周期、真实升级维护与D-37-01 | 本轮先完成资源准备和原资格；未加purge或后台重建，不以清理物理PG测试副本代替产品数据生命周期设计 | TD04、FA-01/GX-01/PX-04/06；生命周期需单独精确目标/恢复策略及授权，真实维护承诺须用户决定 |

TD01要求每个已消除项有具体差异和验证；TD02保持金额/权限/事实/解释/错误处理的权威；TD03逐个交代兼容与旧路径；TD04分清已解决、保留有理由、待处理。以上复用了已接受且当前未变的证据，本轮只回读必要源码和对应报告，没有重复全仓测试，也不承诺零债务。


### 2026-09-14合成资源入口实施边界

离线资源输入复用既有授权登记、管理事件、受限importer与中性回执。纯本地资源批将操作绑定/事实/回执原子提交，其他消费者保留原两阶段绑定语义。schema37补内部synthetic-only profile与只向importer暴露cluster ID的无参数只读函数，读取/恢复规则沿原资源链。

兼容入口`StoreSyntheticResourceRecord`仍有迁移/领域测试调用者及严格库名guard。退出条件是这些实际调用者迁到等价正式受限入口且旧前态、坏链和恢复覆盖齐全；本轮不删除。生产I-03仍缺真实来源合同、身份授权、采集与外部正向证据，本合成输入不能关闭该目标。资源正式CLI/schema37正在本地验证，ASUS整包和最终F/V不据此接受。
