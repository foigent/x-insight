# Finding 生命周期：一次完整覆盖率补验计划

状态：**只读核对与计划完成，等待经理审阅范围和资源窗口；尚未实施修正或启动新负载。** 候选产品固定为 `b780fbbb212682c3ec79787258834195794975f9` / tree `fcec24590ace98cbcd7c1ceab9fdac3a28232aec`，同一 owner/工作树 `/private/tmp/xinsight-xi-c-responsibility-20260909`。本计划不把已通过专项替换为未执行的覆盖率证据。

## 1. 结论与已经核实的差异

建议在既有 PG49998 上完成6个包的缺失数据库覆盖，保留其余包的有效结果；修正两处明确测试前置不匹配，真实合并覆盖数据后执行原门槛。无需启动A0容量/性能或联系UR，亦无需为了单条绿色日志再执行整个仓库。现有约117MiB余量不足以可靠覆盖数据库/WAL、编译及独立复核，建议一次增加512MiB，见第5节。

当前 `assessment-coverage-combined.out` 共27531个statement，已覆盖13384，总48.6%；原脚本要求total63%、api53%、db69%、hostctl64%、workspace46%。按当前分母，total至少还缺3961个真实覆盖statement，api缺105、db缺2807、hostctl缺578；workspace已过。各包达到最低值也不自动保证total通过，不能只补一个最显眼的包。

核对的是当前 `.github/workflows/ci.yml:122-276`、`scripts/check-go-coverage.py` 和测试源码，不是历史CI运行结果：

| 条件 | 原CI backend_validation | 此前本地整仓 | 本次建议 |
| --- | --- | --- | --- |
| 系统/工具 | Ubuntu22.04，Go1.25.11，PG16.14固定镜像，匹配PG16客户端 | macOS，Go1.25.11；整仓无DSN，PG只在专项启用 | 复用已安装Go1.25.11/PG16.14；仍是Local证据，不声称Linux CI实际通过 |
| 普通数据库 | `XINSIGHT_POSTGRES_DSN` 指向CI新建测试库；`XINSIGHT_ALLOW_LEGACY_SINGLE_DATABASE_ROLE=1` | 两者未作为整仓PG环境提供，大量普通PG测试跳过 | 独立新测试库；仅相应子进程启用同等旧单角色兼容入口 |
| hostctl资格 | `XINSIGHT_HOSTCTL_DATABASE_TEST=1`，独立空库及admin/reader/workspace/importer四种DSN | 该专用资格没有以coverage执行；此前选定native专项有行为证据但无profile | 运行原资格测试及已存在ConnectorDigest原生测试；实际四角色/pg_dump/restore，仍隔离于普通库 |
| 新合成夹具 | 当前CI普通DSN使用localhost，且未设`XINSIGHT_RESPONSIBILITY_LOCAL_TEST` | 独立专项明确使用127.0.0.1及该开关 | 修正CI这个自有临时服务的地址/开关；本地同样显式绑定，不能去掉夹具保护 |
| A0/浏览器/真实组件 | backend_validation未启用A0、浏览器或LiteLLM真实组件开关；admin rotation也在另一个job | 全部未启用 | 保持既有opt-in关闭，不新增Skip、不把这些工作偷并入覆盖补验 |
| Neutral D1 | CI另下载固定authority及平台release asset，启用N1兼容测试 | 当前本地无这一已绑定asset | 两个已用任务根未找到合格本机N1资产；不下载/运行未授权外部路径，不将本地门槛通过宣传成完整CI通过 |

普通PG回归与A0不能按文件名粗分：`TestWorkbenchFactKeepsSelectedEvidenceAcrossProjectionReplay` 虽复用名为performance的建库helper，实际只写一条合成事实，应运行。`TestWorkbenchProjectionReadingAuthorizationPaginationAndLateEvidence` 只用7条事实，其备份恢复子项由 `XINSIGHT_PG_BIN` 开启，也应运行。A0的50万事实、20×30万创建、600请求读峰值等则分别受明确开关保护。

## 2. 执行前需要修正的两处测试前置

1. `internal/db/workbench_store_postgres_test.go:104` 先加载精确schema23，再应用当前完整迁移集，却仍断言追加数为2。当前schema27应为4；拟用现有迁移校验器读取当前数量并减去已验证的23，同时保留single/duplicate/conflict和数据不变断言。不是改产品迁移或放宽结果。该断言在产品基线fcda883也存在，不能称为本包新引入的产品缺陷。
2. `responsibilityDatabase` 和 `connectorDigestDatabase` 在DSN已提供时要求 `127.0.0.1` 与 `XINSIGHT_RESPONSIBILITY_LOCAL_TEST=1`，否则Fatal。原CI是localhost且缺开关，已与现有夹具不匹配；fcda883也已有这一要求。拟只在backend_validation的自有临时PG job中改DSN主机为127.0.0.1并明确开启该本地合成资格，不移除保护、不改生产配置、真实账号或其它job，不触发远端CI。

本消息只授权计划，所以以上尚未修改。执行批准宜一并覆盖这两个必要修正，以及本包真实失败/覆盖缺口所需的小规模、有业务意义的测试补充；不逐测试回请设计。若发现需要扩大产品范围、修改冻结A0或新增外部权限，则只暂停对应步骤并集中报告。

## 3. 一次完整的执行范围

先核对候选、最终源码/测试/依赖/资产摘要及资源；随后只启动既有已停止PG49998。不建立新集群、安装依赖或修改PG参数。原配置末项为shared_buffers32MB/max_wal_size128MB/min_wal_size32MB；运行时回读这些值和身份，不把离线配置直接当实时设置。

为本次资格使用新的普通测试库 `xic_cov_reg_20260910` 和hostctl专用空库 `xic_cov_host_20260910`。创建前核对名字不存在；不得重置已存在同名库。hostctl四DSN仅对应该合成实例/专用库；复用已批准合成角色机制，测试临时凭据只进入受限子进程或0600测试文件，不进入命令参数、日志、提交或普通产物。运行hostctl时普通应用连接先关闭，不与经理PG/负载或A0并发。

| 批次 | 完整范围与条件 | 必须得到的真实结果 |
| --- | --- | --- |
| 普通PG与生产HTTP | `./internal/db ./internal/api ./internal/checkpointepoch ./internal/integration/litellmfeed ./internal/integration/pullfeed`；原DSN与旧单角色兼容入口，127.0.0.1本地资格开关；`XINSIGHT_PG_BIN`指向既有PG16工具 | 5包完整测试产生race/atomic coverage。包括普通迁移/证据/调查/Workbench小记录、3类checkpoint测试、两个Feed包的本地持久化/重放、全部原责任及新assessment原生HTTP；不是仅挑能抬百分比的成功用例 |
| hostctl原生资格 | 独立空hostctl库、四角色DSN与 `XINSIGHT_HOSTCTL_DATABASE_TEST=1`；选择 `^Test(PostgreSQL16RoleBackupAndIsolatedRestoreQualification|ConnectorDigest.*)$`；保持其它opt-in关闭 | 真实legacy9、rc.10到当前27、角色/权限、维护、backup/restore、check/preview、DATA_RESET隔离资格及既有digest全路径，收集hostctl profile。保留原90秒等测试内超时，不自动放宽 |
| 合并与原门槛 | 绑定最终输入，将以上profile与仍有效的无DSN结果组合；运行未修改的 `scripts/check-go-coverage.py` | total及全部4个包门槛均实际通过，全部适用测试通过；否则明确未完成，分析真实未覆盖区域或测试失败 |

相应Go命令沿用 `go test -p 1 -ldflags=-w -race -count=1 -coverprofile=<本次独立输出> <明确包/测试范围>`、原guard/GOCACHE/TMPDIR/GOTMPDIR、GOPROXY=off。不运行 `go test ./...` 加DSN；不运行benchmark。API中的人工生产角色测试仍使用真实workspace/reader连接，SQL权限断言保留；兼容入口不能替代既有独立权限专项证据。

执行进程显式不继承任何 `XINSIGHT_A0_*`、assessment/responsibility/browser、Connector browser输出、admin rotation、LiteLLM真实组件endpoint/key、外部UR来源root等未授权opt-in；这是限定进程环境，保持源码已有的默认条件，不增加或修改Skip。`XINSIGHT_RESPONSIBILITY_LOCAL_TEST=1` 是本次批准的专用小库夹具开关，不能与被排除的浏览器开关混淆。

普通PG测试内部CREATE/DROP只作用于刚创建并记录身份的测试数据库/临时角色，沿原helper原生生命周期。hostctl原资格含合成库内迁移、角色LOGIN/授权变化、清空并恢复自身测试数据与新恢复库退役，必须包含在这次明确授权范围；不得指向postgres管理库、用户库或A0留存实例。异常终止保留日志/现存测试库，不自行递归清理失败目录，重启先核身份与状态。

本包当前已有的真实浏览器/E2E、frontend85测试/构建、旧v1/v2/新v3、Gap快照与并发行为证据可依最终输入一致复用；不再启动浏览器。新增Go测试或CI环境修正后做相应gofmt/源码差异/工作流静态检查，最终受影响包vet。若产品/资产未变，不重建前端；原正式schema23/release/手册和1—26迁移不改。

## 4. 覆盖数据的有效合并与失败处理

当前37包清单与原profile/输入绑定可直接复用。5个完整补跑包采用新完整profile替换旧包profile。hostctl若源、测试、依赖、生成物及构建参数都未变，可将本次原生资格profile与已通过无DSN完整包profile取覆盖并集：以文件/起止位置/statement数为唯一block键，每个block只出现一次；任一真实执行计数>0才标覆盖，不叠加statement分母或重复计数。保留每份原profile和选择/合并说明。

如果相关文件或构建输入变化使旧包profile失效，则重跑该完整包，包括hostctl需要时的约4—5分钟普通部分，不靠mtime掩盖不一致。Go源码block集合或statement数不匹配时拒绝合并。其余31包在最终输入仍一致时复用现有结果；最终产品包集合不得漏项。没有采集coverage的历史native专项不能补写计数。

首先保持CI现有测量方式，不增加 `-coverpkg`、不改脚本分母、阈值或配置。如果真实数据仍不足，按未覆盖源码/行为说明原因，补充有意义的Store/恢复/错误路径测试，使用真实小规模PG或适合该层的现有夹具；不得只增加镜像实现的断言或跳过难测分支。新增db代码虽已被API专项调用，默认包覆盖率不会因此自动计算到db，这一点需要实测profile判断，不能预先承诺门槛必过。

批准范围内的测试修正和必要本地补证由owner整体收敛。若剩余缺口依赖未授权N1资产、真实外部组件、A0容量或新增主机资源，停止对应部分并集中提交准确缺口/可行路径；不从“补coverage”推定这些权限。最终即使本地原脚本通过，也只报告“Local同阈值通过”，完整远端CI、正式Artifact/Target和跨来源验收仍未执行。

## 5. 资源、时间和恢复

23:29:18Z只读实测累计1,823,109,120 bytes，上限1,946,157,056，余123,047,936（约117.35MiB），free38,584,254,464。PG停止；runtime中data57,999,360（base23,420,928/WAL33,554,432），tmp521,908,224，其余浏览器旧profile/node-cache/旧基线继续保留计入。原baseline SHA256 `bf8e3b9087608b56d5ca85a776c5eefcaa7dfa64c4e28b3ef41806aea9f47c4a`不变。

建议原累计上限一次增加512MiB至**2,368MiB（2,483,027,968 bytes）**，owner补验/失败保留与经理复核共用；连同当前余量约629.35MiB可用，不是重新发一份预算。估计分配：

| 新增用途 | 计划余量 | 依据与限制 |
| --- | --- | --- |
| Go新缓存、重编译和同时存在的测试临时文件 | 128MiB | 已有6包race/coverage缓存，预计只为测试修正产生新归档；无新工具链/依赖/通用跨包instrumentation |
| 合成小库、恢复中并存库与WAL增长 | 256MiB | 新库基数约十余MiB、每组顺序执行；现有WAL32MiB可增长，max_wal_size是软限制，不作为硬容量保证 |
| hostctl保留目录与原生备份/失败材料 | 96MiB | 上轮小库原生dump约256KB；仍为多前态/清空恢复及可能完整hostctl回归留出空间 |
| 源码、profile、日志与Git对象 | 32MiB | 前轮全部assessment产物约5MB，原提交Git增量约3MB；不重复复制旧大现场 |
| 经理复核与估算偏差 | 96MiB | 与owner共享累计；不得在owner用尽后另算经理额度 |
| 合计 | 608MiB | 小于当前余量加提议增量；仍以guard实测为准，不是测得峰值承诺 |

先前6项并发/负向原生race组8.271s，恢复/升级组各小项约1—6s；普通hostctl无DSN整包267.906s。普通PG全组本候选尚未实测，预计纯验证约5—15分钟，测试修正时间另计；不伪造精确工期，不为赶估计放宽既有超时或改用并行负载。开始前和每个负载入口重新量测余量/20GiB空闲，保留1秒guard采样和越界停止，采样不声称连续峰值。

正常结束关闭连接、停止PG49998，回读本次测试对象/角色状态、来源/冻结输入、资源与日志完整性。guard或测试失败后先分类；进程被停止时不自动重跑残留库，保留准确失败身份，通过既有维护/恢复入口或新的已核空测试库恢复。禁止重设baseline、移出计量范围、清理旧tmp/缓存/主管留存或删除三旧目录腾额度。

## 6. 请求的一次完整决定

请求经理审阅后批准：第2节两个必要测试/CI环境修正，第3—4节6包完整覆盖补验及必要小规模测试修正，专用合成库/四角色/原生资格的明确写入与恢复范围，以及第5节512MiB累计增量。在这个完整范围内由同一owner自主完成，下一次主动报告为完整结果或实质阻塞。

A0仍冻结，361源码/9准备/固定二进制、模板和三旧目录不动。任何测试修正只在C工作树发生，不修改A0冻结工作树或重编其固定binary。当前空间超过28GiB不等于A0性能通过，也不授权本轮启动A0；先完成生命周期独立验收，再安排原资格的不重叠窗口。C队列/合格来源、A1/B/D/X、原13/TD父级、D-37-01、UR禁联及push/PR/merge/release/deploy/真实凭据流量等边界全部保留。
