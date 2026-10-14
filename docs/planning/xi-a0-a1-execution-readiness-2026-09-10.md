# A0 / A1 增量执行准备：C 验收后的接续

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

2026-09-10。**C 部署队列/本人筛选 Local 子项已由 review25 接受；本次只交执行准备。A0 的正常路径和失败后隔离恢复尚不能同时得到当前资源保证，建议先审 A1 三 profile 消费与阅读子项。** 不扩大完整 C、A1、SE-03、原13项或 TD 的完成状态。本轮没有构建、启库、迁移、复制数据库或运行负载。

本方案只增补 [5f23135 已审设计](/private/tmp/xinsight-xi-a0-20260909/docs/planning/xi-a0-key-storage-and-c-queue-plan-2026-09-10.md)、[原 A1 接续表](/private/tmp/xinsight-xi-c-responsibility-20260909/docs/planning/xi-continuation-and-debt-review-2026-09-09.md) 和[第46份共同合同](<PRIVATE_HOME/Desktop/projects/PRIVATE_PERSONAL_ARCHIVE/library/reports/企业AI工作事实平台-目标拆解与验收-2026-09-05/46-UR-A供数与XI消费衔接-共同合同准备-2026-09-10.md>)。当前字节、停库控制和资源来源见 [planning-facts.json](/private/tmp/xinsight-xi-c-responsibility-20260909/output/xi-a0-a1-readiness-20260910/planning-facts.json)；这是定向源码判断，不是新功能已通过的证据。

## 1. 实施面及完整 A0 包

当前 C 基线 `417daf1eb0a88f9abbcc3b3c8e9726eb7dfae152` / tree `61cbcbb9b62d534dac201b98a67262e00ac69b5d`，A0 设计基线 `5f23135aba3fd792c58f5035a717b4106ca650cc`。五个 Workbench Store/projection/restore/API/model 核心文件逐字节相同；C 已追加26—28，当前54个迁移文件全部绑定。下一包从已验 C 基线新建隔离工作树，A0 候选位置 `/private/tmp/xinsight-xi-a0-internal-keys-20260910`（当前不存在），不合并现有分支、不覆盖原 checkout。只移植 A0 必需资格工具并重新绑定当前输入。先执行的包使用下一追加迁移29；若先 A1，后续 A0 基于其已验版本接续，不平行预占编号。

同一 XI owner 完整实施并一次交付以下内容，不拆成逐实验审批：

- 按已审设计加入两个不可变 identity bigint 内部键；公开 reading ID、事实 UUID、来源身份、金额和所有逻辑摘要保持。成员换为内部键后仍保留两条真实 FK、PK/UNIQUE及原 cascade/restrict；摘要 JOIN 回公开身份。RR事务、筛选/cutoff、ordinal及同步201不变。
- 维护事务内建立映射、完整核对、建立新约束后切换并退役替代列/约束；不绕过 immutable trigger，不创建永久双写。真实 PG16 importer/workspace/reader、replica immutability、重放/并发/回滚均验证；序列只按真实行为授精确必要权限，不授 ALL SEQUENCES。
- 小规模0/16/70k、特殊数组/授权/时间边界、无效键/错配/重复/父行操作反例；覆盖24→25→新版本、当前28→新版本，保持旧迁移与正式合同、v1/v2/v3包字节。旧 reading 的 ID/ref/金额/顺序/分页/摘要及新内部键映射、序列后续插入经过原生备份与隔离恢复；坏映射/孤儿/错序/缺约束拒绝，check/preview实证无副作用。保留旧 restore 分支和旧二进制拒绝新 schema 的边界，不承诺原地降级。
- 正式配置仍按已审条件验证 `wal_buffers=16MB` 与精确创建端点9秒；其他 API 3秒、SQL 10秒及网络限制不放宽。schema/buildinfo/hostctl/restore/权限和受影响 race、覆盖率、前端/API检查必须绑定最终同一候选。受支持直接 SQL 的旧列依赖须在移除前核清；仓库无命中不代替外部兼容证据。

小规模通过仅支持正确性。大包依次为：迁移已授权诊断副本→一次仪器化300k→两次无仪器完整创建均<5秒→唯一最终资格；前面三次不拼入正式样本。最终同20个真实TLS请求，Store与完整HTTP各自全部成功且p95≤5秒；500k完整201≤9秒；500001 Store/HTTP拒绝且六表/孤儿无残留；至少7.2M成员后600请求、20RPS、30秒，summary/top各300、端到端p95≤2秒；提交前取消与提交后响应未知分别验证。旧失败、旧样本和已结束40分钟窗口不续用，延长请求预算不等于通过。

## 2. 两个副本、恢复和资源决定

| 拟申请转换用途的精确 PG data 路径 | 当前只读核对 | 执行前必须补齐 |
|---|---|---|
| `/private/tmp/xi-mw-final-jdj3t3yc/data` | 停库；root/data inode 179263891/179263894；allocated 2,169,143,296 B；历史未启用的空成员前态 | 重核完整物理清单、真实身份/逻辑0 reading/0 member；原生归档并验证后才迁移为最终候选 |
| `/private/tmp/xi-mw-diagnostic-x3h1z446/data` | 停库；root/data inode 179265546/179265548；allocated 3,633,049,600 B；历史诊断结束22 reading/6.8M member | 重核本次22/6.8M逻辑清单；形成此精确前态的新原生归档并实际隔离恢复后才迁移/诊断 |

两者 device=16777234、systemID=7683380252116611841；当前 pg_control SHA 与完整控制输出已绑定。**本轮只核对控制身份/分配量，不声称重新比过所有数据文件。** 旧 `diagnostic-copy-match.json` 只证明进入诊断前21/6.5M来源，不能充当22/6.8M恢复凭据。现有 final 清单也须在执行前验证新鲜性。

原空模板 `/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-checkpoint-run-eeh92u0h/template/data`、16MB失败源 `/private/tmp/xi-final-qual-zq9g4gdb/data`、4MB失败根 `/private/tmp/xi-final-qual-1np0gnaa` 及其余旧源、失败、binary/raw全部保留。拟归档位于两个可转换根内各自的 `internal-keys-pre29.dump`，使用现有原生 custom archive、权限/hash/list检查；实际编号随先后包调整，已有路径不覆盖。

关键恢复约束来自当前 `internal/hostctl/database.go`：`RestoreToNewDatabase` 要求**不存在的新数据库**，然后恢复、迁移和验权。不能用覆盖候选库、删除失败物理现场或只列 archive 目录来省掉隔离恢复。旧态回退用匹配旧合同的恢复/二进制验证；新态恢复另验内部键和序列。大库转换失败先按原迁移事务/dirty规则停住；需要恢复时保留失败库，在限定合成集群的新恢复库验证，不自动重置再跑性能。新临时验证库只有成功且符合既有原生清理范围时才清理；失败保留。具体 DB 身份在新窗口启动后由维护连接只读核对并绑定，未绑定前不迁移/恢复旧库。

当前资源快照：C **3,044,556,800 B** / 3,288,334,336 B（3136MiB），已分别含 review24/25一次；A0 **15,209,331,582 B** / 19,327,352,832 B（18GiB），余4,118,021,250 B；physical free **30,435,762,176 B**，距21GiB底线7,887,183,872 B。分配会漂移，执行前重核；本轮没有做清理。

以下为包级资源建议，**均未实施，且估计不冒充实测峰值**：

- 正常路径按比前案GB口径更保守的GiB预留：归档/构建/小验证/证据0.9GiB、迁移并存数据/索引/WAL 2.8GiB、最终增长1.8GiB，共5.5GiB。诊断当前 `base` 2,860,417,024 B，另外预留3.5GiB用于保留失败现场时的原生隔离恢复及WAL；保守完整高端为9GiB。顺序运行可降低峰值，但尚无新候选实测，不能先算成已释放空间。
- 因此仅 A0 18→20GiB 虽可容正常估计，仍不能覆盖上述恢复。完整 A0 建议 A0上限 **24GiB（+6GiB）**、开窗前实际free **≥31GiB**；按当前累计加9GiB后累计余896,795,778 B，31GiB减9GiB仍比21GiB底线多1GiB。当前free距建议入口尚差 **2,850,234,368 B**；仅调累计上限不能解除物理阻塞，不据此删除任何旧目录。
- C同步建议3136→**3648MiB（+512MiB）**，承接仍计入C的共享构建/复核增长；不让C停工掩盖共享cache。A0原19路径及cache起点2960270877、C原cache/Git起点全部不重置；新增隔离worktree、小验证runtime和所有归档/恢复分配显式纳入A0，review25与后续经理证据纳入C且不重复review24。两账交叉项不能相加冒充物理量。
- 新开一次 **60分钟数据库活动窗口**，从首次新PG启动算起，涵盖小规模、归档/恢复、迁移、诊断、正式资格及停库；离线实施和可离线完成的构建在之前完成。无并行A0/经理负载；free21GiB/RSS4GiB及各累计限制继续。阶段开始前实测归档/恢复增量与余量，不足就停止相依大动作并交一个准确阻塞，不自动扩窗、丢样本或再建候选。若实际归档/恢复超出估计，本建议重新评估。

当前缺的是包级转换/资源决定以及实际物理余量；未证明完整A0现在可运行。无需为此继续小实验或自行清理，独立A1子项见下。

## 3. A1 接续：三个 profile 的明确落点

原第38份A1与现有接续表仍包括归属、应用/目的声明和人工修订、关系版本、有限活动输入；I-02与I-03职责不变。此处采用第46份已提供边界：固定UR commit `25f5c1fdfb6b5376496c34e9b9cd295efbaf17cd` 的 Source Facts v1、connector `unirouter.workfacts` 1.0.0；只读本地git对象的文档/manifest/schema/golden向量已绑定。没有联系UR、启动其程序或调用真实来源。

| Profile | XI复用与新接收/存储职责 | 新reading解释与验收 |
|---|---|---|
| `unirouter.execution-context.v1` | 复用有界HTTP与Envelope/registry/provenance机制；新增类型化context文档、auth/admission/terminal阶段、修订谱系和profile checkpoint。旧completed-v1 mapper不能承接 | application已知时按捕获时版本展示；阶段不当成多次调用，terminal不声称完整时间线；缺失维持unknown，应用不推断业务目的 |
| `unirouter.execution-identity.v1` | 新类型化owner/grant subject/Sponsor/验证过的Agent lineage，精确scope/profile采集授权；存同一XI PG，不新增服务 | owner/Sponsor不推断当时实际操作者，actual_human未知仍未知；新解释绑定来源版本，历史不以当前目录回填；XI字段/当前阅读权限独立验证 |
| `unirouter.cost-basis.v1` | 复用不可变F0/I-01经济事实；新增冻结费率/authority/version及经济引用的待关联状态，按source/org/project/request/revision/hash精确关联 | 成本profile本身不加金额。I-01先到或后到均只计一次；未收到I-01不当已收到；priced-zero/unpriced、legacy规则和独立media charge-fact明确，未定价不伪装完整成本/排名 |

源码确认的具体缺口均归 XI：`EvidenceCommitter` 固定completed-v1/revision1/完整终态，`ModeAwareEvidenceCommitter` 两模式也先调用它；`PostgresState` 固定旧cursor key。`LiveWorker` 的“持久化收敛后才提交页位置”可复用，但它和 `ProjectConvergedCheckpoint` 都拒绝空页推进，而新合同空cursor先返回空bootstrap页，须提供新类型的bootstrap/checkpoint路径，不能全局放松旧保护。`NormalizedItem.Events`也是旧Evidence承载，需专用source-fact提交/解释入口，不能把新文档伪装旧V1事件。

接收/校验补齐：每profile独立opaque cursor；≤50项/页、<8MiB/页、≤64KiB/项；保留原canonical bytes并分别验source/Envelope/XI引用身份，int64范围/十进制字符串/UTC六位精度/未知字段与null等负向规则。revision前驱/hash/reason、同身份同版本不同字节冲突、缺前驱pending/gap持久化可见，重放不重复；只有已持久化且满足合同收敛定义的页才提交checkpoint。capabilities/profile/scope/epoch/generation、空bootstrap、普通轮换保cursor、409 ahead/410 expiry、7日保留、晚启用/暂停/撤销都有明确状态；不静默跳到latest。旧I-01凭据不自动扩权。

存储/阅读补齐：当前 F0只承接completed-v1金额，`loadReading`和restore只支持A0解释hash，account/application/purpose仍固定unknown。新增不可变source文档/修订和解释manifest，显式规则版本及选定source ID/revision/hash绑定到**新reading**；旧reading继续按原解释分支读取/恢复。total→contribution→detail用同一解释修订和金额事实。源采集许可与XI当前阅读权限分离，断源保留合法历史和缺口，撤销XI阅读权及时拒绝旧reading；不以已有宽泛Case权限暴露identity，不在页面同步查询UR。仅复用接口机制不算这些语义已经实现。

当前未发现需UR修改已交合同的实质分歧；以上是XI接入缺口。真正遇到无法映射的权威/identity/金额/兼容冲突才附最小反例交经理共同判断，不直接联系UR。

## 4. 可先审的 A1 最小完整包

建议同一XI owner从当前已验C基线实施“三profile合成接收→不可变存储/关联→版本化reading→生产Handler/UI”的完整本地子项；必要迁移/权限/restore/开发schema、错误/unknown展示和测试一次交付。保持旧I-01路径与金额，落实上节缺口，不依赖A0 bigint迁移或6.8M大库。

验证入口复用原合成PG和受保护的独立新测试DB：使用固定UR的golden source/page正反向量及第46份已留存的真实生产Handler合成HTTP页，通过XI新HTTP reader、实际PG提交和XI生产Handler/UI重放。覆盖先后到达/重放/重启/部分页/checkpoint失败、修订冲突/缺前驱、bootstrap/各profile权限/scope代际/409/410/gap，验证I-01金额一次、未定价与media边界，旧reading不变/显式新版本/当前撤权及原生恢复。保留原Source/Envelope/M0、旧feed和Case封存兼容回归；所改Go包race/原覆盖率门槛、前端/API及受影响native lifecycle在最终候选上完成。静态schema或独立SQLite结果不代替XI消费证明，重放既有HTTP页也不声称新live UR→XI集成通过。

此包归原A1的I-02来源解释与reading子项；原目的声明/人工纠正UI、有限活动输入/I-03、真实源接入、全流程I2-07/B及原13项最终同版验收仍未完成。未提供或未经授权的实际操作者/业务目的继续unknown，不用推断填满。不扩展为新研究、通用消费者框架、新数据库/服务或UR改造。

若选此替代包：建议C **3136→3904MiB（+768MiB）**，当前余量加增额共1,049,083,904 B，预估新增0.75GiB（805,306,368 B，含构建、合成小库/归档/恢复、UI和经理证据）；A0保持18GiB且共享cache仍回计，physical floor21GiB不变。新一次45分钟数据库验证窗口、离线实现/可离线构建在前；旧guard目标保护、精确新DB绑定、失败留存和停库要求沿用。此为预算建议而非实测保证，执行前重核，未获包级决定不启库或实施。

**提交经理的决定面：** 优先接受A1上述完整子项及其资源窗口；A0设计保留，等物理资源与恢复条件满足后按一个完整包评审。若经理选择A0，需一并明确两个精确副本的用途转换、24GiB/共享C限额/31GiB实际入口与新60分钟窗口。当前所有旧上限仍有效，没有自动增额、清理、外部权限或运行授权。
