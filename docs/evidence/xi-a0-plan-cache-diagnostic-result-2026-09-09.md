# XI-A0：计划缓存诊断完成，整体创建瓶颈仍未闭环

本轮独立的36次目标SELECT全部成功。**eligible_count在第六次auto执行切换为generic，且明显变慢；membership_digest与coverage七次均保持custom。不能把原容量试验的三个阶段变慢统一归因于generic，也不能统一强制custom。** 本轮只读诊断没有完成创建性能、HTTP或A0验收。

基线提交为`9c9deec663417392e730ad5e16707aa013489067`。经理明确批准新的36次/20分钟诊断及4128MiB累计预算，复用已停止的`/private/tmp/xi-pc-hcpwpsza/data`，未再复制。此前观察器失败的一次count、旧PID、二进制和输出单列保留，没有拼接计数。实际执行2026-09-09 06:15:20–06:15:41 UTC；测试13.80秒、exit0，整个启动/查询/停库/源核验周期21.417806秒。

## 执行身份与证据

- 固定二进制`/private/tmp/xi-pc-fix-build-jdy_iwzj/db.test`，SHA256 `aaa6ecbdbc7b27256c0eb8b7ecff59936d9ac96b48211b49d0dda73cb8221a5a`。359个编译输入与修复版绑定相同；本轮未修改Go产品或测试源码。
- 端口56301；三条SQL始终使用同一真实workspace连接PID43322，非superuser，pgx CacheStatement/cache512，默认auto，statement_timeout=10s，目标事务READ ONLY。prepared名称、prepare_time、SQL、参数OID及计数增量均核验，EXPLAIN执行的是该连接实际prepared statement。
- 独立读回19 headers、5700000 members；指定七个reading各300000，membership_ref均与原记录一致。21次auto目标执行后，三条SQL各进行auto EXPLAIN及G→C→C→G，共15次EXPLAIN ANALYZE；总计36次，无失败、替换或超预算补样。
- PG16.14；max_wal_size=1GB、shared_buffers=128MB、work_mem=4MB、wal_buffers=4MB，fsync/full_page_writes/synchronous_commit/autovacuum均on，max_parallel_workers_per_gather=2，temp_file_limit=-1。I/O计时按诊断计划开启。未改产品SQL、索引、角色权限、PG默认或HTTP预算。

原始数据：[diagnostic.json](../../output/xi-a0-plan-cache-run-02/diagnostic.json)、[run.json](../../output/xi-a0-plan-cache-run-02/run.json)、[code-binding.json](../../output/xi-a0-plan-cache-run-02/code-binding.json)。[analysis.json](../../output/xi-a0-plan-cache-run-02/analysis.json)及[plan-nodes.json](../../output/xi-a0-plan-cache-run-02/plan-nodes.json)是事后派生数据，不覆盖原始输出hash清单。

## 三条SQL的auto与G/C结果

以下为客户端单次耗时ms，按真实执行顺序；G/C为会话内事务局部的force_generic_plan/force_custom_plan。小样本用于解释计划差异，不作为性能p95或普遍速度结论。

| SQL | auto 1 | 2 | 3 | 4 | 5 | 6 | 7 | 七次后generic/custom计数 |
|---|---:|---:|---:|---:|---:|---:|---:|---|
| eligible_count | 46.749 | 44.979 | 47.805 | 50.991 | 44.071 | 435.698 | 280.461 | 2 / 5 |
| membership_digest | 314.319 | 329.007 | 348.345 | 335.132 | 333.636 | 307.726 | 285.846 | 0 / 7 |
| coverage | 807.773 | 698.493 | 377.316 | 360.003 | 343.186 | 346.821 | 351.872 | 0 / 7 |

| SQL | auto EXPLAIN | G1 | C1 | C2 | G2 | G中位数 | C中位数 |
|---|---:|---:|---:|---:|---:|---:|---:|
| eligible_count | 304.485 | 304.164 | 48.885 | 46.604 | 301.429 | 302.796 | 47.745 |
| membership_digest | 273.169 | 265.989 | 261.320 | 301.051 | 264.381 | 265.185 | 281.186 |
| coverage | 392.623 | 172.479 | 406.072 | 421.629 | 160.560 | 166.519 | 413.850 |

**count：本会话切换已直接证实。** 前五次custom计数递增，六、七次分别变为generic=1/2、custom=5。generic选择串行Nested Loop Semi Join，把实际300000行估成1行：期间索引估2500行、实际500000行，经其他条件过滤后实际300000行；授权表的两行扫描执行300000次。custom选择两个worker加leader的并行Hash Join，授权表仅每个参与者扫描一次；custom仍有估算误差，不能称为准确估计。两次generic约301–304ms，custom约47–49ms，相差约6.34倍。generic估算总代价7669.38，低于custom的32541.10，但实际更慢，说明成本比较在这里选错了路径。I/O读时约30ms对12ms、无temp块，差异也不能仅归为磁盘读延迟。

这强烈支持原容量试验count在第六次出现台阶的解释；原会话已丢失，没有其prepared状态，因此不能回填成原会话切换的直接证据。

**digest：没有generic切换或稳定的G/C优势。** 两种模式均为并行成员/F0 Hash Join、Gather Merge和聚合所需排序；成员估算约379381/391976，实际300000。都出现external merge和4批hash，根节点temp写块约18794–18798（约147MiB累计块写入，不能当作瞬时临时文件峰值）。原最后成功创建的digest为973.975ms，本轮未复现；不能据本轮反证原失败，也没有依据采用force_custom优化digest。

**coverage：generic反而更快。** member_state均经成员/F0/evidence并行hash连接；主要差异在gap_state。generic先连接小capture_gaps与授权来源，结果为零，另一侧昂贵成员/F0分支Actual Loops=0；custom先构建含300000 members及500020 F0的连接，再发现没有匹配gap。generic根shared读块52257/52278、temp写3672/3692；custom读68885/68794、temp写9893/9889。这支持本输入下连接顺序及空结果短路解释，不能外推成generic普遍更好。auto前两次较慢、后续下降却一直custom，提示缓存/预热参与，但未隔离证明它是唯一原因。原最后成功创建coverage为2325.257ms，本轮也未复现。

## 统计状态与本轮的关键局限

39份表统计快照（首尾、七轮后、十五次EXPLAIN前后）中选定字段一致；本轮没有观察到ANALYZE/autoanalyze或相关表统计变化，也没有执行DDL或手动ANALYZE。成员表`reltuples=4193289`、`n_live_tup=5693289`均为统计估计，实际计数是5700000；`n_mod_since_analyze=1500000`，last_autoanalyze为2026-09-09 12:46:44.402182+08:00。

`reading_id`统计n_distinct=14，常见值列表含14个ID；**本轮七个reading全部已在常见值列表中。** 原创建流程每次生成新ID，在同一写事务内INSERT成员后立即计算digest/coverage。因此本轮没有重现“新ID不在常见值列表、刚写入的成员、脏页与FK/写入成本”等条件。

下一竞争假设是：成员表自动分析形成旧reading的常见值统计后，新ID的custom选择率被低估，导致digest/coverage选差计划。当前仅有统计与运行语境支持，**尚未验证新ID的实际计划**，也没有证明它解释原INSERT超时。

原容量试验最后成功样本（第20次尝试，原始index=19）的`xwr_XMf3SdgKSIV7KJ_kxrItBg`不在当前常见值列表，原返回known_items=300000，membership_ref与本轮七个相同。它可用作下一次最小只读对照；本轮没有单独读回或测量该ID，不能把历史返回值当作当前个体计数验证。

## 资源与现场状态

| 项目 | 本轮证据 | 已批准边界 |
|---|---:|---:|
| PG启动前真实空闲 | 25859317760 bytes，约24.084GiB | 至少24GiB |
| 首次目标前同步检查 | 预算剩余567146515 bytes；真实空闲25859325952 bytes | 剩余512MiB预算且21.5GiB真实空闲 |
| 最大累计计量 | 3862288365 bytes，约3.597GiB | 小于4328521728 bytes（4128MiB） |
| 最大保守合计RSS | 1137360896 bytes，约1.059GiB | 小于4GiB |
| 最低真实空闲 | 25758519296 bytes，约23.989GiB | 21GiB停止线；20GiB保护量 |
| 新尝试总周期 | 21.417806秒 | 20分钟 |

统计合并14次后台资源采样、36次目标前同步检查及各边界/最终读数。累计计量包含现有clone、两个保留二进制、全部准备/失败/修复/本轮输出和编译cache增量，取文件逻辑/分配长度中的较大值；真实磁盘余量另测。所有已记录检查均满足边界；采样峰值不等于不可见瞬时峰值。运行期低于24GiB不构成违例，24GiB是启动门槛，运行停止线为21GiB。

本轮PG日志只有停库时一次checkpoint（3 buffers，0 WAL added/removed/recycled），目标查询期间没有记录checkpoint。原始源before/after完整inventory一致，交付再次核对源和复用实例均为shut down。原失败场景、两个二进制和旧输出全部保留，未做旧场景清理或另一份PG复制。

## 下一步最小改动及qualification方案（仅提请经理审阅）

建议先做小范围统计对照，再决定产品改动；现在只修count预计节省约0.25秒，不能解释原数秒差额或INSERT超时。

1. **优先诊断：两种reading输入、两条SQL，最多18次目标执行。** 在当前停止的诊断实例上，先一次性读回既有MCV ID `xwr_vzD1s7lPgy1RE3-Trd5V5Q`与非MCV ID `xwr_XMf3SdgKSIV7KJ_kxrItBg`的实际成员数及ref，确认同为300000且ref相同，并重核统计分类；不INSERT。每条SQL先一次auto执行MCV输入建立真实prepared statement，再对两ID各做G/C/C/G，共2+16=18。采用交错并反转两个ID的顺序，保留同PID、OID、prepare_time、计数、结果、计划行数、buffer/temp及前后统计。精确SQL与其他参数不变。该设计只需扩展现有诊断输入/循环，不增加新的prepare协议。
2. 该对照的新runner/test应先离线编译并绑定；保留所有旧二进制及输出，按4128MiB累计重新核算编译cache/新二进制/输出后再判断24GiB启动与512MiB/21.5GiB发样门槛能否通过，不能预先宣称已有足够空间。不自动清理或扩大资源。本轮36次已结束，尚未执行这18次，也没有实施该harness扩展。
3. 若仅非MCV输入出现custom严重低估和昂贵连接，而同基数generic恢复，则新ID统计假设得到直接支持，再设计对应digest/coverage的局部候选；如果没有差异，应转到新建事务内的实际计划/写入状态，不能反复重复相同晚期只读试验。
4. **已具依据的最小产品候选是count单独绕开命名statement缓存。** 在`workbench_store.go`的eligible_count调用为首个选项参数传`pgx.QueryExecModeExec`，保留SQL和六个绑定参数；不改变连接全局默认。固定pgx5.10.0的stdlib CheckNamedValue/QueryContext会透传该选项，Conn.Query使用ExecParams路径。本轮只验证force_custom计划，尚未验证此调用方式与其速度；需真实workspace定向验证字符串、时间、空/非空数组、权限撤销及边界过滤语义，尤其该模式使用文本参数编码。不能把候选描述为已实现或性能已通过。
5. 选定候选后才做原始qualification：精确最终代码/二进制，从验证为空成员的共同模板开始，真实workspace、1GB WAL、128MB/4MB、10s SQL及持久性不变，完整20×300k且包含第一次、保留所有失败；达到6M才做500k。原5秒创建指标仍须满足，不能只报成功子集。测试在每次Store返回后、计时外记录同连接prepared状态及分析时间，避免在占用连接的事务内另从单连接池查询而阻塞。需覆盖count、member INSERT、digest、coverage的确切SQL及新ID/统计变化；这些附加检查不能冒充原无观察器基线。
6. 创建通过后完成既定summary/top/hourly、选定组全量分页、ordinal/money/ref及实际HTTP/lifecycle qualification。当前3秒HTTP实现预算未变，Store通过不能替代真实HTTP可用性；任何预算决策须单独基于总目标评审。新qualification的资源与精确运行计划仍需先准备、审阅，本轮未启动。

## 交付边界与复核

本轮Python语法和资源边界定向检查通过，实际固定二进制36次诊断通过；Go未变，复用上一提交的OID回归、默认跳过及vet证据。原49个保护文件、3份继承失败和本轮10项原始hash清单均匹配。此前37项交付绑定在原提交全部匹配，其中34项当前字节仍相同，另3项为本轮已授权的runner、计划及执行记录修改；旧失败原始输出9项hash仍匹配。核验结果见[delivery-verification.json](../../output/xi-a0-plan-cache-run-02/delivery-verification.json)，本轮最终字节绑定见[evidence-binding.json](../../output/xi-a0-plan-cache-run-02/evidence-binding.json)。

这是Local诊断完成；原13项父目标、TD/R3未完成事项及A0仍未验收，D-37-01仍待决定。没有产品模式采用、完整创建性能资格、500k、后续读取或HTTP/CI/Target/External/Production的新通过声明。本地提交，不push/PR/merge/release/deploy，不联系UR，不使用真实凭据或业务数据，不发送跨任务消息。
