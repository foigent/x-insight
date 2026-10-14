# 新A：受管来源采集与具名离线导入完整计划

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

2026-09-11。提交经理独立审阅，审阅一致后实施。本包沿第38份XI-A1/XI-X接续，交付能安装、启动、暂停、恢复并核对来源范围的采集路径，以及有明确授权和回执的离线入口；源事实进入现有真实工作台。R35 Local已获主管接受，M4镜像等待不构成本包硬依赖。

修订v2：仅补主管首轮审阅提出的操作预约、在途停止/多profile、live/offline授权分离三项语义及对应验收。基线、合同配对、范围和12GiB整包预算保持不变。审阅入口：[plan-review.md](<PRIVATE_HOME/Desktop/projects/PRIVATE_PERSONAL_ARCHIVE/library/reports/企业AI工作事实平台-目标拆解与验收-2026-09-05/supporting/xi-iteration-20260909/a-managed-offline/plan-review.md>)。

## 1. 基线、现状与边界

权威实施基线是华硕 `7bf683b55337a6e7f7bd957ce8aadb2227d988c0` / tree `b3eb9ac22fb474d158752319577ab4b5098402c8`，见 [R35收口](../../output/xi-asus-g1-20260911/R35-G1-FINAL-CLOSEOUT.md)。M4仍为`ece4d074...`加WIP，不能用其旧dist构建下一候选。下一源码工作副本从华硕完整Git基线形成，Linux新路径`/opt/xi-validation/sources/a-managed-offline`保留原R35现场；M4可做轻量文本修改，传入前逐项绑定基线/差异，最终提交仍在华硕完整父链形成。下一commit由实际内容产生。

当前源码事实：

- `cmd/xinsight-importer/source_facts.go:23`只执行一次`Consumer.PullOnce`；`internal/sourcefacts/consumer.go:87`已校验capabilities、范围与页，`internal/db/source_facts.go:44`将文档和游标事务提交，并保留迟到修订、重复字节与冲突处理。
- 生产路径尚无新A流登记/暂停/恢复的完整管理员入口；当前登记主要由`internal/testsourcefacts/fixture.go`完成。不能把测试夹具或已有只读来源页当成可安装的受管能力。
- 既有`pullfeed.LiveWorker`绑定completed-execution V1，不能直接把其经济事实检查点或授权引用套到新A。可复用配置/退避/服务管理的已有做法；新A围绕现有`PullOnce`形成最小运行循环。
- `internal/connector.ValidateEventBatch`与`boundedoffline.PostgresOperationStore`已有严格批次/具名操作/回执基础，但当前batch入口参数仍要求旧ManifestBinding，不能直接传新A的profileBinding。此包抽取共用严格解析并接入新A产品注册，保留旧入口约束；新A专用offline入口尚待实现，不修改UR协议或实现。

本包使用XI专属环境、合成HTTPS来源及冻结合同输入，不连接UR或真实来源。I-01、I-03、B5/D3消费、SSO、A0、S和真实活动验收继续开放，不用本包代替。

## 2. 产品行为与最小实现

**管理员可完成受管安装与恢复。** 扩展既有hostctl/CLI的管理员工作流：具名登记`source_system / source_instance / org / project / profile / scope_generation`，配置精确端点与owner-only的source凭据文件引用，做能力预检，显式启用、暂停、恢复和撤销。默认仅context；identity/cost必须显式选定。配置、安装或能力发现本身不启动采集。日志和状态只输出安全标识与错误分类，不返回凭据。

这些变更由既有hostctl管理员权限执行并记录操作journal；不提升workspace/importer对stream身份、授权范围或status字段的写权。复用已安装的systemd与CLI；同一XI受管实例只运行一个新A worker，对登记的profile分别保存流状态，每页之间重新核对当前状态并响应停止。已有`pull-source-facts`保留为同语义的单步诊断入口，服务与单步都调用同一Consumer，不维护第二套游标。

**使用者看到真实采集状态和范围。** 现有来源状态/详情展示最后成功接收、暂停/撤销、失联、过期或范围变化；没有新数据也不显示完整历史已采齐。停止或撤权不增加可见数据，已冻结阅读按原授权规则读取；无identity授权不泄漏身份，无cost事实不补算金额。`cost-basis`仍是来源依据，费用权威保留于I-01/UR。

**断开后能接续。** 同范围凭据轮换继承已保存游标；范围generation改变须新登记和forward-only bootstrap，旧流/证据保留。409、410、401/403、capabilities不匹配及不可变事实冲突进入可解释的需处理状态，不自动清空游标、扩权或追历史。短暂503/传输失败沿现有有界退避及可取消等待；参数集中于现有运行配置，不另建重试调度系统。成功空bootstrap只记录当前位置；后续页完成事务后才前进。

**离线导入可预检、可重试、可追溯。** 新CLI消费Foigent 2.0.2严格EventBatch，单次单profile、最多50项且总字节严格小于8MiB，复用新A逐项envelope/source重投影验证。需要具名操作ID、精确输入摘要与有效本地授权，绑定目标、XI版本/commit、UR合同版本、source/org/project/profile及授权期限；仅有文件或哈希不能构成授权。预检无DB写入、游标变化或网络请求。数据与完成回执在同一事务形成；同操作同输入返回原回执，不同输入冲突，进程中断后可判定重试结果。离线回执不改live cursor或伪称UR已确认接收，也不宣称timeline complete。

复用现有中立操作表、回执类型和严格批次校验；事实写入抽取现有不可变/修订链共用事务函数。须明确传入同一DB事务：现有`WithOperation`的外层事务不会自动使另一个store的写入原子化。所需最小事务接口同时回归原offline消费者，不复制一套锁/ledger框架。

### 2.1 操作预约、原子完成与当前授权

选择保留既有防输入替换语义：现有`WithOperation`及其他offline消费者不改。新A增加能够传递同一事务的入口，仍允许在正式执行准入后，先独立持久化`connector_id / operation_id / request_digest`预约。摘要绑定输入、目标、来源范围、协议/产品版本及具名离线授权身份；成功预约后不允许换输入、换范围或换授权身份复用此ID，预约不等于完成、计费事实或采集权限。

| 失败/重试位置 | 明确结果 |
| --- | --- |
| 只执行preflight/check，或在正式准入时发现输入/路径/版本/当前本地授权无效 | 零DB写入；不创建或改动预约、事实、回执、游标。已有他次预约也不删除 |
| 当前授权及全部输入校验通过，预约已提交；之后授权失效、事务错误或进程中断 | 允许只保留原预约；本次新增事实与完成回执全部回滚，已有他次事实不受影响。不称“所有表无残留” |
| 事实/完成回执事务已提交，响应丢失 | 同ID同摘要且当前授权仍有效时返回原完成回执，不再插入事实；授权已失效则拒绝执行及回执重放 |
| 同ID不同绑定，或旧预约在无当前授权时重试 | 拒绝；保留原预约和已完成结果，不用新身份替换旧授权，也不自动清预约 |

预约准入与真正写入都在各自持锁事务内验证当前离线登记/版本/范围；完成事务对授权、不可变事实和回执使用同一个DB事务。到期按取得所需锁后、开始写入前的最后一次DB时间校验判断；等待期间已到期则拒绝。已取得该次提交许可的事务可以完成，随后到期或撤销不倒改其既成结果。明确注册的撤销与提交通过同一授权行锁排序：撤销先完成则写入失败，完成事务先获得锁则其提交在撤销之前，撤销等待结束后不再允许旧授权提交。

### 2.2 在途页停止、持久需处理状态与profile公平性

本地pause/revoke与页提交以数据库事务先后为准。管理员与提交路径对同一登记串行化并使用一致锁顺序；每次有效管理状态变更递增登记revision。页发出前保存登记快照，返回后在事实/游标事务内重新核`status / revision / scope_generation`及持久需处理标记。管理员先提交pause/revoke，旧页即使已返回也不能写事实或推进游标；页事务先获得提交锁，则该页可以完成，pause/revoke等待它后再成功返回。停止确认之后没有旧在途页可继续提交。pause→resume即使回到active也改变revision，旧请求不能靠状态相同通过；必须重新加载并发起新请求。

管理员独占status、范围、管理变更及“确认修复后恢复”的权限。worker只更新观察/进度，不将管理员paused/revoked自动改成active；401/403、409/410、范围/合同不符和不可变冲突留下持久需处理标记，进程重启和单步CLI都在取页前、提交前服从它。worker可清除已恢复的短暂错误，不能自行清除需管理员确认的标记。管理员核准修复后显式恢复并递增revision；撤销的登记不复活，范围改变用新登记。实现须用真实角色覆盖标记/管理状态的写入边界，不能只靠UI禁用。采集暂停/撤销与XI用户历史读取授权是独立控制。

同一worker按具名profile轮转：每轮每个就绪profile至多取一页，再轮到其他profile；`has_more`只让该profile继续就绪，不在它内部无限排空。退避/需处理状态按profile记录；capabilities/网络的有界超时只影响当前尝试，不能因一个profile失败退出整个worker。公平性保证其他合法profile持续获得尝试机会，不承诺零延迟；单步CLI使用同一准入/提交逻辑，不能绕过pause或需处理状态。

### 2.3 离线登记与live授权分别生效

离线使用管理员建立的**独立本地offline登记**和owner-only授权文件，精确绑定source/org/project/profile、目标、产品/协议版本、授权身份和期限。登记可在来源凭据不存在、live从未登记/启用、没有网络的环境创建并使用；它不要求`Stream.status=active`、远端capabilities或source bearer。复用来源身份与事实表，不复用live采集许可；离线入口不发DNS/HTTP请求，也不写live的status、revision、last_error、cursor或high-watermark。

live的pause/revoke、source凭据失效或scope generation变化只关闭/改变live采集许可，不暗中撤销一份仍有效且明确覆盖该批输入的本地offline授权；反之，offline到期/撤销不改live登记。offline授权不能据live曾经成功而扩大范围，也不能因live已暂停而绕过离线自身授权。XI用户读取权限继续独立检查，导入许可不授予阅读权。

offline登记的持久授权状态和版本由hostctl管理员管理，importer只读。每次执行、重试及已完成回执返回都检查当前有效性；登记撤销、过期、范围/版本不符或授权文件无效均拒绝。正式在途撤销通过管理员撤销登记并按2.1与提交排序；仅删除文件只阻止后续打开文件的调用，不冒充可靠的在途撤销。更换授权身份或范围必须使用新登记和新操作ID。恢复保留旧事实/回执及撤销状态；其他offline登记也默认不取得写入许可，须核当前有效授权后显式恢复。已撤销登记禁止恢复启用；旧备份中的许可不是当前授权证明。

## 3. 离线合同精确配对

[本次字节配对证据](../../output/xi-asus-g1-20260911/new-a-contract-pairing.json)：XI已验schema和golden-source-facts与UR候选归档逐字节相同。

| 项目 | 本包固定输入 |
| --- | --- |
| UR候选 | `59f7712d09653034756b846b15b8d69a16782791`，1.4.0未发布；归档精确路径见配对证据 |
| Foigent精确身份 | commit `ad8fe7a38c9d7335657630baa8670dd07556b98c`，release digest `sha256:99f8a541b6d33b0a74d2e4c06aa312c6d6c88c35a4bfb1d4d34b98b4e4e2e8e8`；已与XI现有pin逐字段配对 |
| 连接器/线格式 | `unirouter.workfacts@1.0.0`；`unirouter.source-fact.v1`；Foigent 2.0.2 |
| profiles | `unirouter.execution-context.v1`、`unirouter.execution-identity.v1`、`unirouter.cost-basis.v1` |
| source schema SHA256 | `29af900edc7fa4e6b835b71b2c14eae4a027e5ec4a588b8e31cd6fddabb2da30` |
| source vectors SHA256 | `27444000449574deec17b7dd1930458c8be45392f611eb99777149fe28f82d47` |
| page vectors SHA256 | `7a91c2d891e575fdd063feb1a67cc52b3eb83e423102420f40d325902e143b73`，319,157B |
| UR manifest SHA256 | `caaa8c73571a060481ad024714cfb37f59f30a419c783adc43efcaee980c10da`；标记local_candidate，不能当正式发布证明 |

capabilities为`unirouter.source-capabilities.v1`，purpose为`xinsight.source.read`；source文档≤64KiB，page≤50项/<8MiB，retention为604800秒，bootstrap为forward_only且timeline_complete=false。保留原数值字符串、6位微秒时间、canonical bytes、scope/epoch及修订前驱规则。

UR所交offline页向量仅是互操作试料，字面游标不是生产签名游标；本包离线EventBatch由冻结envelope生成，验证消费者的授权和落库。不能把它描述为UR已有正式离线导出功能，不能让离线包扩展live范围。新A必须登记自己的source/projection及精确artifact pins，不能拿OpenInference的来源/映射注册代用。Foigent协议身份沿XI现有pin，不从UR local_candidate manifest推导新的发布授权，也不把新B/D的2.0.0 profile混入本包。

## 4. 兼容、迁移与恢复

复用schema29的streams/documents及schema20的离线操作表，当前schema31是受支持前态。v2明确需要独立offline授权登记和可执行的持久需处理写入边界，纳入最小schema32迁移；管理revision优先复用现有revision，不复制游标或中立操作表。绑定hostctl合同并实际验证31→32、版本拒绝、角色权限、损坏备份和无副作用check。此类普通工程版本绑定沿本包审阅范围处理，不逐commit请求用户审批。

现有I-01 LiveWorker、经济事实、已保存阅读、R35用途声明/撤销和其他offline消费者必须保持原语义。明确新A管理员与importer的权限差异，原禁止改scope/status及不可变文档的负向检查继续有效。恢复用原生PG备份及新隔离库，比较授权、登记、不可变源字节、修订链、阅读与回执；来源凭据沿安全配置边界处理，不放入一般SQL产物/日志。恢复后默认不自动采集，只有版本/范围/凭据和当前授权匹配后显式resume；不能借恢复重置源epoch或激活撤销连接。

新旧运行路径共用消费核心。旧单步CLI保留为诊断兼容入口；临时fixture专用启动代码不成为产品部署路径。本包若替换临时重复配置/执行入口，在调用及现有支持文档核对后完成必要收缩，原证据/备份/用户目录不删。

## 5. 整包资源与运行安排

沿现有G1：5GiB WSL、swap0、4CPU；重操作前Windows只读核身份、保护元数据、独占状态与完整资源，再应用G1。主机启动余量3GiB、停止新增负载线2GiB，磁盘21GiB停止线不变；受管采集、API、模拟来源与浏览器的组合验证必须实际同时运行。构建与无关压力分开，systemd任务沿现有3500MiB上限、PG独立700MiB。G1事实不外推为F并发资格。

本包**追加预留12GiB完整高端**，含执行方与经理，既有R35占用保持实占，不用预计删除抵扣：

| 增量项目 | GiB | 覆盖 |
| --- | ---: | --- |
| 源码/Git/交付制品 | 1 | 新副本、对象及双方候选 |
| 构建缓存/测试临时文件 | 2 | 复用已有工具，缓存增长与全套必需回归 |
| 执行方DB/恢复及WAL | 2 | 新工作库、恢复库、索引和失败事务 |
| 经理独立DB/恢复及WAL | 2 | 独立同版本验证，不只看执行方结论 |
| 原生备份/原始日志/浏览器证据 | 2 | 双方备份、授权及恢复证明保留 |
| 失败候选/边界输入 | 1 | 损坏与最大尺寸输入及现场 |
| 收尾和停止余量 | 2 | 打包/恢复/不可预见的有界增量 |
| 合计 | **12** | 完整包，不按单次小实验续预算 |

准入要求为free≥`max(45GiB, 21GiB+12GiB)`，并核现有96GiB虚拟盘的guest剩余、双方尚未完成占用及RAM。13:47Windows约117.90GiB只说明方案有可行余量，开工前重新核相关读数。实占趋近整包高端时停止受影响新增负载并重新判断，不能小额追加或切包掩盖不足。M4不承担本包PG/browser；镜像同步仍独立要求21GiB+128MiB，未准入前只做轻量文本工作。

有限功能输入采用完整官方向量、200请求×3profiles×最多3修订（最多1800文档）及逐项负向，另核单文档/单页边界。64KiB全上界的原始文档约112.5MiB；额外预算用于envelope/JSONB/索引/WAL/恢复和双方副本，不当作实测压缩率。该样本不替代S的300,000请求、原读并发/长期留存及A0资格。

## 6. 一次交付前的验收

| 验收项 | 必须得到的实际结果 |
| --- | --- |
| 管理生命周期 | 从未登记→预检→显式登记/启用→暂停/恢复→撤销；重复/并发操作、scope扩大与角色拒绝可复核，只有配置无隐式采集 |
| 持续与重启 | 真实合成HTTPS源分页；空bootstrap、多profile各自checkpoint、在页事务前/后中断、服务重启/停止、迟到/乱序修订。用可控事务屏障覆盖pause/revoke先提交、页先提交及pause→resume后旧页返回；服务/单步一致，无暂停确认后的旧页提交或游标回退 |
| 来源错误/轮换 | 401/403、409/410、503/传输故障、capabilities错配、同范围轮换、范围generation改变；需处理标记在进程重启/单步调用后仍阻止采集，真实importer不能清除或改管理状态。两个profile分别证明首个401/410或始终has_more时，另一个仍合法的profile持续取得进展 |
| 离线授权/回执 | 实际CLI+PG：无来源凭据、live未启用、出站网络不可用时合法offline成功；非法/过期/撤销授权及错schema/hash/版本/目标/范围/路径拒绝。preflight/准入拒绝零写入；预约后中断可留预约但事实/完成回执共回滚；提交后响应丢失重试原回执，当前授权失效时拒绝重放。验证live/offline并发同事实幂等、矛盾字节/前驱冲突、无半份完成回执，live各状态字段/游标全不变；保留实际零网络证明 |
| 产品与权限 | 原生产品来源页/详情反映实际状态与证据；管理员操作和只读阅读分别按现行角色；identity撤权、旧阅读、R35用途修订及费用结果不漂移，页面不泄漏密钥/源身份 |
| 恢复与兼容 | 工作库→原生备份→新库→候选运行；损坏/越权/不支持版本拒绝；同scope续跑与撤销不复活，已有I-01和其他offline路径回归 |
| 完整交付 | 改动稳定后一次运行适用Go/race、原覆盖线、frontend/API/schema/hostctl必需检查；最终源码、协议配对、三类制品(server/ctl/importer及受影响controller)、配置与证据绑定，经理按风险独立验收；最终停止/恢复记录 |

交付前集中核目标/依赖、双方完整预算、合同/版本与权限分离、实际支持前态、错误与恢复和制品身份；发现缺口先修后交。期间仅实质阻塞或边界变化提前回报，正常中间试验不逐项送审。

## 7. 原目标贡献与首个F入口

本包使XI-A1的归属/当次actor/应用与来源依据可以受管接入，支撑US-01—03、FA-02、GX-01；使XI-X具备独立模式、来源状态和运行恢复的本地完整链。不把context当身份、不把cost-basis当已收费金额，也不声称完成CT/SE/预算或原13项整体。

首个F需要：本包最终XI commit及上述离线配对通过；R35 `7bf683b5`行为在整合候选保持；具名新隔离UR候选`59f7712d...`及其批准的安装/身份/来源范围；UR+XI、两逻辑DB、采集/浏览器/恢复的完整并发资源准入和精确配置。之后另按已批环境范围执行真实两产品组合，不联系原UR任务或改原UR资产。A0不是本包/F硬前置；S原规模/留存、适用A0与至少一个获准真实活动来源仍是最终V/E条件。

本计划请求主管审阅以上完整包。没有新增用户审批项；合同/当前现场检查发现实质边界变化时只暂停相关操作，其余已授权工作继续。
