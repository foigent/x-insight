# XI-A0：一次真实300k创建成功，实际写事务复现digest/coverage差计划

**唯一一次正常Store创建成功并保留，新reading `xwr_9kVW5mAm_-NntzgtjeDoRQ`；实例现在为20 headers/6000000 members。** 完整ref和coverage与原有效基准及持久化读回一致。实际新写事务的digest/coverage均使用custom，将新ID的300000成员估为1行，确认此前既有行对照机制也发生于本次真实创建。仪器化总耗时10403.924ms，INSERT5468.094ms；这不是原连续20次资格、p95或HTTP通过，A0仍未通过。

代码基线d6ae76f9990f7900276f4ac033ad9965428f7db5。仅新增诊断测试/runner、计划/记录/证据；产品SQL、索引、schema、角色权限、HTTP预算未改。运行完成后未执行产品候选、500k或额外创建。

## 授权、前置及恢复

经理明确批准在同一隔离合成实例 `/private/tmp/xi-pc-hcpwpsza/data` 执行一次300k新ID正常产品创建，并使用现有auto_explain。原 `/private/tmp/xi-cap-7qgkhj7u/qualification/data` 保持停库只读，没有新增数据副本。采用本次启动参数session_preload_libraries及analyze/buffers/WAL/settings/triggers、JSON、timing/nested关闭、参数完整记录；其余WAL1GB/shared128MB/work4MB/wal_buffers4MB、约束/持久性/SQL10s保持。

准备期编译/离线测试/vet完成后，沙箱拒绝/bin/ps；经平台审批仅补做资源读取，未重复编译或启动PG。首次运行08:00:33.281548UTC开始，在参数轻量前置之前因受限pg_settings不显示session_preload_libraries而停在**0创建**，PG正常停库。原始workspace日志已有实际auto_explain计划，全部模块参数也可见；错误是观察器将缺项等同空值。[首次诊断](../../output/xi-a0-auto-create-run/diagnostic.json)、[失败日志](../../output/xi-a0-auto-create-run/diagnostic.log)、执行时Go/runner/计划及原二进制均保留。

PG16实现将该设置标记为GUC_SUPERUSER_ONLY，并按pg_read_all_settings权限决定可见性。[设置定义](https://github.com/postgres/postgres/blob/REL_16_STABLE/src/backend/utils/misc/guc_tables.c)、[可见性实现](https://github.com/postgres/postgres/blob/REL_16_STABLE/src/backend/utils/misc/guc_funcs.c)。修正版只把启动项SHOW放到现有诊断admin，workspace仍强制验证实际模块设置、轻量text/array/timestamptz返回和完整参数日志。未增权、安装或降低前置要求。

恢复复用原20分钟窗口，未重置计时，创建总上限仍1。恢复运行08:03:24.918845–08:03:43.394713UTC，实际周期18.475868秒，距首次起点190.135825秒。端口60869，workspace PID65901，cache512/CacheStatement/auto、非superuser、SQL10s；同一连接前后PID一致，进入产品调用前已释放唯一连接。完整轻量SQL/参数/实际rows/buffer/WAL检查通过后才运行创建。关闭timing后只使用实际行数/调用及buffer/WAL数据，未声称取得节点或trigger耗时。[官方auto_explain行为](https://www.postgresql.org/docs/16/auto-explain.html)。

实际二进制 `/private/tmp/xi-auto-build-kuur3art/db.test`，SHA256 `0c1d3be4db51c75a023cb97a91d1ebb50ca839f6dbb6a807e44e15111fa944fb`，360项输入；首次0创建二进制a552c00d也保留。现有库53376bytes/SHA e880e306b60a240cfdee2ff6a653f92ff59c63a0e67fd9c7e0fcc70687b78cf6绑定未变。本轮两个新二进制连同此前六个都纳入累计计量。

## 创建、参数及语义

通过产品createWorkbenchReading及既有阶段观察器调用，SQL与参数路径未复制。捕获实际事务内7条完整计划：authorization、header INSERT、eligible_count、member INSERT、digest、coverage、header UPDATE；每条Query Text逐字匹配编译产品源码，完整参数分别核对subject/source/period/filters/newID/ref/coverage，PID相同。7条语句此前均未prepare，完成后各custom_plans=1/generic_plans=0，参数OID与实际参数数量一致。它是新会话，不能冒称原7595末次会话或原20次连续重放。

前后计数为 `[19,5700000,0,0,500020,500100]` → `[20,6000000,0,0,500020,500100]`，依次为reading/member/saved_filter/operation/F0/evidence。前19个header仍在，仅新增上述ID；该reading实际成员300000。产品返回和GetWorkbenchReading读回的membership_ref均为 `sha256:bc008bc4c4771660899c4659b8b1815d14999fce75acc347b54651391a0bb9a1`，coverage完整对象等于既有有效样本：not_connected、known_items300000、projection_complete_for_members=true、原固定水位及gaps均一致。这里有真实返回及持久化读回，不以EXPLAIN行数替代语义。

原始[diagnostic.json](../../output/xi-a0-auto-create-run-02/diagnostic.json)、[PG日志](../../output/xi-a0-auto-create-run-02/postgres.log)、[参数/计划派生审计](../../output/xi-a0-auto-create-run-02/analysis.json)及[可重跑分析脚本](../../output/xi-a0-auto-create-run-02/analyze.py)保留。

## 阶段与实际计划

| 阶段 | 产品阶段观察 ms | auto_explain语句 ms |
|---|---:|---:|
| begin | 0.209 | 不提供utility计划 |
| authorization/header | 1.573 | 0.014 / 0.438 |
| count | 284.935 | 284.290 |
| INSERT members | 5468.094 | 5390.191 |
| digest | 1025.485 | 1022.280 |
| coverage | 3616.706 | 3609.315 |
| finalize header | 6.666 | 0.093 |
| commit | 0.154 | 不提供utility计划 |
| 完整Store调用 | **10403.924** | 不拼接语句值代替总耗时 |

SQL均低于10秒；整次调用超过10秒不等于单SQL超时。阶段时间含客户端/规划/日志等边界，不能将其与auto_explain的差额全部归为日志开销。关闭节点timing仍有仪器开销，未做无仪器配对，单次值不能宣称产品p95。

- **count**：本次custom并行路径，未发生generic切换；根shared读26313块。约284ms不能说明先前count切换机制消失，也不能把冷读/仪器差异当作新回归。
- **INSERT**：F0过滤实际300000、估1500，顺序扫描500020条，其中200020被过滤；2行grant只扫描一次并materialize，semi-loop访问它300000次。先按(occurred_at,id)外部排序11760kB并生成ordinal，再按已存在的ctid物理策略外部排序14696kB。根temp读/写3307/3318块、shared hit/read2719062/26233、写9465块、WAL119928329bytes/906055records/15FPI。实际两个FK各调用300000次，未关闭约束。排序和grant循环真实存在，但无节点/trigger时间，不能认定它们或FK中的某个独占5.468秒。
- **digest**：新reading估1、实际300000；成员主键Index Scan接F0主键Index Scan300000循环，根shared hit/read1309931/192751。实际事务内已直接复现新ID低估和逐行访问。
- **coverage**：成员两处均估1、实际300000；两处Index Only Scan各有300000 heap fetch，新写数据不能享受既有可见页状态。F0总600000主键循环，evidence300000，capture_gaps还做300000索引探测、结果0。根shared hit/read3904639/685232，temp写2576块；代价估45.48。与既有非MCV custom机制一致，当前写事务的额外heap访问也被直接记录。

成员统计reltuples4193289及last_autoanalyze12:46:44.402182+08未变；n_live_tup5693289→5993289、n_mod_since_analyze1500000→1800000，其余所选表快照不变。统计估计与真实5.7M/6M计数分别报告；只有前后快照，不能冒称逐时刻读取统计。

创建窗口内集群WAL增长350936399bytes、1206066records、26340FPI，wal_buffers_full增长35571、wal_write_time约2071.763ms；backend写39572块，checkpoint计数未增加。**这些是集群窗口值，不等同INSERT根节点统计，更不能把差额直接当作已计时FK成本。** 观测支持写入/FK/WAL及缓存周转仍需处理；commit返回快和窗口内无checkpoint排除了本样本“慢在提交返回/同时checkpoint”的说法，不证明历史所有样本相同。停库单独checkpoint用82ms，PG自身移除18并回收3个WAL文件，故最终累计文件量下降；没有人工清理。

成员表/索引增长77537280bytes（27934720表、49602560索引）；F0总大小未变。这不把FK行锁造成的物理页/WAL动作误称为业务事实内容修改。

## 资源与验证边界

累计4480MiB=4697620480bytes；新旧8个二进制、全部准备/失败/恢复输出、同一cache基线增长均计入。启动前重新核对23GiB，首次创建预算余量707589945bytes、真实free25041809408bytes，均满足512MiB/21.5GiB门槛。

合并首次1次/恢复11次后台观察、两轮边界和目标前后检查共20项：观测最大累计4118075591bytes，最大保守合计RSS506888192bytes，最低真实free24580730880bytes，全部已记录检查通过，未触21GiB停止/20GiB保护或4GiB RSS。采样间隔中的不可见瞬时峰值不能保证。原20分钟窗口内完成，SQL10s不变。

原源1604文件前后相同；源与复用实例均正常停库，新的6M实例保留。编译、日志解析/错误SQL/错误参数/非实际行/截断负向、现有相关回归、默认跳过与vet通过，资源边界离线模拟通过；实际恢复测试PASS10.97秒，完整语义及参数审计通过。产品未改，本轮未重跑全套Go/frontend/HTTP资格。首次失败及其二进制/执行代码不是成功证据，未被覆盖或从历史移除。

## 最低充分候选与资格顺序（仅建议，待共同落定）

1. **优先试做已具备直接证据的读取模式候选**：count使用pgx.QueryExecModeExec，digest/coverage连续区段使用事务局部generic，完成后恢复进入区段前的模式，错误/取消完整回滚；不改连接全局模式。当前真实写事务已证明两条custom差计划，但未在本次写事务测generic，故收益仍须实际验证。先用0/少量/70k/300k代表性reading评估模式代价与完整语义，再依据已知insertedMembers选择分支；不预设任意阈值。覆盖参数编码、真实角色、空/非空过滤、时间边界、撤权、取消/回滚/模式恢复及ref/coverage。
2. **INSERT保持原SQL/FK/ctid策略，另做单变量判断**：本次7句全部custom，INSERT问题没有generic切换证据。1500→300k误估和两次排序值得关注，但材料不足以认定删ctid排序或重写授权会更快。sourceID当前是text合同，验证也未要求UUID；直接把数组改为uuid还需先证明所有支持输入/错误语义等价。不要为估算数字好看先改SQL或索引。
3. **推荐读取候选稳定后，以无auto_explain的正常创建阶段数据决定INSERT下一步**。若INSERT仍阻断5秒，优先单变量对照shared_buffers128/512MB，明确固定wal_buffers4MB、WAL1GB、work4MB、持久性与SQL10s，检验本次大量随机页/写入周转是否真正下降；这是隔离配置诊断建议，未采用512默认，需另定匹配前态与资源计划。不要把增加WAL上限4GB当作已通过方案；先前反证保留。仅当相应证据支持时再决定产品配置或进一步SQL候选，保留原FK/不可变事实语义。
4. **不以节省读阶段推算资格**：历史INSERT成功子集p95已超过5秒，本次仪器化INSERT也约5.468秒。不能减去异次G/C差额或相加阶段中位数，声称整体能达标。关闭诊断日志后，以最终代码/二进制在共同空成员模板完成原20×300k全成功且p95≤5s；只有该资格达到6M才做原500k，随后完成增长后summary/top/hourly、选定组全量分页、ordinal/money/ref及实际HTTP/lifecycle。当前诊断留下6M不满足该前置，HTTP现有3秒预算未变。

当前已批准的“一次创建诊断”已完成；下一产品修改/配置对照仍供双方决定。原13项/TD/R3/A0/D-37-01和UR禁联、外部操作边界未改变。无push/PR/merge/release/deploy或跨任务消息。最终核验见[delivery-verification.json](../../output/xi-a0-auto-create-run-02/delivery-verification.json)，字节绑定见[evidence-binding.json](../../output/xi-a0-auto-create-run-02/evidence-binding.json)。
