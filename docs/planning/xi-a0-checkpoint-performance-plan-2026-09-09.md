# XI-A0 下一性能切片：WAL checkpoint 单变量对照

状态：**供 XI Owner 与总负责人评审，尚未执行**。本轮只读核对现有代码、原始测量和已停止实例的日志，并新增本计划；没有修改产品、SQL、索引或 PG 配置，没有启动实例或性能负载。

代码基线：`033a19678a38dec6aacd9dcbb8466645259ba016`；tree：`6e834e254b1b69438b38f44b4d2781be022eb870`。工作目录：`/private/tmp/xinsight-xi-a0-20260909`。原失败报告及证据绑定保留原字节。

## 建议与判断

先只检验一个假设：**默认 `max_wal_size=1GB` 导致过密的 WAL checkpoint，增加创建事务的写入竞争；将其调整为 4GB 能否显著改善完整创建耗时，而不把成本仅推迟到测量结束后。**

选择这一切片的新增依据是两份 Store 实例日志均出现 14 次 WAL 触发的 checkpoint、13 次过频警告。它比笼统增大缓存或同时调多个参数更直接，也无需再次改变已批准的事实存储语义。4GB 是一次有界的磁盘预算试验值，不是推荐的部署默认值，更不是已验证能达到 5 秒的结论。

A0 目前仍未通过。用户已接受 immutable-fact/narrow-member 语义；失败本身不撤销该授权。此前“相同 PG 配置”用于保证比较口径，不应解释成禁止有对照的本地参数试验。SQL、索引及隔离 PG 参数的下一步，在既有语义和本地资源范围内由 XI 与总负责人议定；本计划不新增用户批准关卡。按照本轮明确要求，评审达成一致且总负责人的角色测试结束后才开始执行，不与其并行施加性能负载。

## 当前证据及其边界

| 已有测量 | 窄成员原输入顺序 | 窄成员按 tuple 顺序写入 |
| --- | ---: | ---: |
| 20×300k 完整创建 p50 / p95 / max（ms） | 7821 / 10095 / 14823 | 6997 / 9660 / 9803 |
| 第一个样本 INSERT / digest / coverage（ms） | 13336 / 625 / 809 | 5793 / 386 / 550 |
| 500k 完整创建（ms） | 17445 | 16628 |
| 20 次创建之后另测 INSERT EXPLAIN Execution Time（ms） | 4309.976 | 3877.607 |
| 该 EXPLAIN 输入 SELECT 总耗时（ms，含子节点） | 239.219 | 326.457 |
| 该 EXPLAIN 两个 FK trigger 合计（ms） | 2209.480 | 1724.506 |
| 整个实例生命周期 WAL checkpoint / 过频警告 | 14 / 13 | 14 / 13 |

来源：[原顺序 JSON](../../output/xi-a0-storage/store-performance.json)、[tuple 顺序 JSON](../../output/xi-a0-storage/store-locality-performance.json)，相应 instance JSON 指向下面的本地原日志。

- 原顺序日志：`/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-storage-focused-yn1k_td4/postgres.log`。WAL checkpoint 位于第 10–60 行；警告间隔 9–27 秒。SHA256：`a6c2908b28c04914b1605144e1d9c22041edc39364967e4c5bc3ebc8c11e7a8e`。
- tuple 顺序日志：`/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-storage-focused-tgxemwni/postgres.log`。WAL checkpoint 位于第 10–60 行；警告间隔 9–29 秒。SHA256：`d066ef0e3084cfb471a691c6774fd3990aa8fb6043ca2186a718ec4a75c7cd9d`。
- tuple 日志第 8–11 行摘录：`10:49:28.802 ... checkpoints are occurring too frequently (12 seconds apart)`；随后 `checkpoint starting: wal`；完成时 `wrote 10619 buffers ... write=7.436 s, sync=0.482 s, total=7.926 s ... distance=539532 kB`。其他样本也多为 write 阶段，不能把其中包含节奏控制的 checkpoint 时长直接当作请求阻塞时长。
- [最终 HTTP JSON](../../output/xi-a0-storage/http-period-performance.json) 对应实例日志 `/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-storage-focused-0sm7cxih/postgres.log` 有 7 次 WAL checkpoint、5 次过频警告。日志 SHA256：`3eee1844e18367d836f8bc67c1c3c2e94be5e7e10510ef46c7f902f7f68ae140`。

三个实例的 `postmaster.pid` 均不存在；本轮未启动它们。其 `postgresql.conf:252` 均显式写有 `max_wal_size = 1GB`，启动参数没有配置覆盖，`postgresql.auto.conf` 没有设置。旧 JSON 没有运行时 `SHOW max_wal_size`、checkpoint 及 I/O 累计统计，因此以上是停止后配置文件与日志证据，不能补写成当时逐样本的运行时读数。

PostgreSQL 16 文档说明，WAL 填满导致过密 checkpoint 的警告提示考虑增大 `max_wal_size`；该值是软限制，重负载时可能超过，并且增大可能延长崩溃恢复。这支持试验方向，也要求记录实际 WAL 占用和完整写盘成本。[PG16 WAL 配置](https://www.postgresql.org/docs/16/runtime-config-wal.html#GUC-MAX-WAL-SIZE)

仍未确定的因果关系：

- 上述日志包含迁移、seed、创建、查询及测试清理，旧报告没有每次创建的起止时间，不能把 14 次全部归给 20 个样本，也不能从日志反推出各样本的等待时间。
- 旧报告只保留 p50/p95/max 和首个样本阶段；EXPLAIN 是 20 次之后新事务中的单条 INSERT，最终回滚，不包括 count、digest、coverage、header 收尾与 commit。回滚不会撤销已经产生的 WAL、I/O 或空间分配。不能用其 3.88 秒证明完整创建已达标。
- 两份 Store 测量早于运行时 `pg_catalog.sha256` 替换，不能替代最终代码验收；不同随机 UUID、初始物理布局、缓存/后台写入也未配对。p95 从 10095 降到 9660 不能单独确证 tuple 顺序的稳定收益。
- tuple EXPLAIN 有约 110MiB shared read、74MiB shared written、26MiB temp write，输入估计 1509 行而实际 300000 行。这些值得留作后续线索，但 shared read 不等于物理磁盘读取，128MB shared_buffers 也不等于全部可用缓存。输入 SELECT 仅约 326ms，现无证据优先做索引/估计修正即可填平完整创建的数秒差距。
- tuple 报告中成员约 1.895GB 是全部 reading 与诊断分配后的累计大小，不是单次 reading；新试验同时记单次增量与累计值。

## 数据分布与比较口径的纠正

旧交付中的“7-day/30-day distribution”应读为**7/30 天查询窗口内的聚集数据**，没有证明事件均匀铺满 7/30 天。原证据不改写，在新报告中明确纠正：

| 入口/批次 | 当前生成范围 | 实际时间覆盖 |
| --- | --- | --- |
| Store，最近 70k | g=1…70000，减去 `g % 604700` 秒 | cutoff 前 1…70000 秒，约 19.4 小时 |
| Store，其余 430k | g=70001…500000，减去 8 天及 `(g-70000) % 1814400` 秒 | cutoff 前 8 天+1 秒至 12 天23小时26分40秒 |
| Store，30 天 project-300k | 前 70k 加后续 230k | 最近 19.4 小时与 8 天+1…230000 秒两段聚集 |
| HTTP，新增最近 69988 条 | i=1…69988，减去 `(i % 604700)+1` 秒 | 相对该 seed 语句的 now 前 2…69989 秒；另有 12 条原 fixture |
| HTTP，扩至 300k/500k | i=69989…299988 / 499988，减去 8 天及 `(i-69989) % 1814400` 秒 | 分别覆盖 8 天+0…229999 / 429999 秒 |

Store 原测试先有 500k F0、以 project 选中 300k，成功 reading 持续累计到 6M members；HTTP 原测试先创建 10×70k 并读峰值，扩至 300k 后连续 20 次 503 回滚，基线成员只有 700k，且失败可能留物理膨胀。因此两者不能横向作同等负载的耗时比较。

本切片沿用 Store 的聚集分布、性能来源500k F0、300k选员和逐次保留成员，A/B 同等难度，不更换分布来获取改善。原 Store 另有20条可见性测试事实与相应授权，也在共同模板中保留并分列计数，不能将来源内500k误报为数据库全表500k。最终另记录按小时的非空桶数、时间 min/max、精确总数和金额。时间窗口端点/零桶/跨日行为仅补已有用例缺少的最小合成边界样本，不据此声称均匀 30 天容量通过。若对照显示事件时间与物理顺序相关性实质影响结果，再由双方确定同量、配对的分布敏感性切片；不默认扩大成新的全量矩阵。

## 一个实验变量，固定其余条件

| 项目 | A：对照 | B：候选 |
| --- | --- | --- |
| max_wal_size | 1GB，启动时显式指定并读回 | 4GB，启动时显式指定并读回 |
| 版本与代码 | 同一 PG16.14、Go1.25.11、最终 SHA-256 代码、同一测试可执行文件 | 完全相同 |
| 内存与 checkpoint | shared_buffers=128MB、work_mem=4MB、checkpoint_timeout=5min、checkpoint_completion_target=0.9、其余同一配置 | 完全相同 |
| 持久化与数据约束 | fsync/full_page_writes/synchronous_commit=on、wal_level=replica、全部 FK/唯一约束/F0 guard、autovacuum 正常启用 | 完全相同 |
| 执行身份 | 真正连接 xinsight_workspace；statement_timeout=10s，连接上限保持10 | 完全相同 |
| 初态 | 同一停止状态的合成实例副本；同一 Evidence/F0/权限、索引与统计信息 | 完全相同 |
| 观测 | 两臂均开启 track_io_timing、track_wal_io_timing、checkpoint 日志；同一采样方式 | 完全相同 |

配置均限定于此次新建、只监听 loopback 的临时 PG 启动参数，不改 Homebrew 服务、共享配置、旧实例或正式安装默认。A/B 同时增加的观测开销需注明；它们与旧 JSON 不是严格配对，真正对照是本轮 A 与 B。不得同时改 work_mem、shared_buffers、wal_buffers、SQL、索引、FK、排序或 HTTP 时限。

实现只需在现有 opt-in benchmark 旁补充诊断模式、逐样本输出和自建实例 runner；复用现有 `createWorkbenchReading` observer、角色配置/校验与生产 Store。测试中使用固定 cutoff，报告 `session_user/current_user`、连接角色设置、参数及来源、完整约束状态。管理员仅用于 synthetic seed、管理及统计采集，不能代替被测事务身份。合成角色认证沿用已批准的隔离测试办法，不使用真实凭据，也不增加 runtime 函数权限。

## 配对顺序与观测

1. **先确认性能窗口空闲。** 总负责人角色测试结束，核对当前代码及差异，编译一次测试程序。新建一个合成模板，沿用现有 500k seed 语义、ANALYZE、角色设置；校验 count/金额/摘要后正常停止。模板不包括大 reading 的试跑。
2. **同一个静止模板生成每一臂。** 按 PostgreSQL 支持的完整停止实例文件备份/恢复复制整个数据目录，包含 WAL 与事务状态，核对副本字节，再以各自独立目录/端口启动。不得只复制成员表文件或从运行中的目录普通拷贝。[PG16 文件系统备份](https://www.postgresql.org/docs/16/backup-file.html) 这样保留初始 fact ID、ctid、索引和统计；不通过每臂随机重 seed 制造不同初态。
3. **首对 A→B，每臂连续 6 次 300k。** 第一条也计入，全部 reading 保留；无逐条删除、强制 vacuum、预热剔除或中途 checkpoint。6 次是低成本方向诊断，足以看到成员增长并有机会复现默认 WAL 压力，不作为 p95 验收，也不保证 4GB 臂已经跨自然 checkpoint。生产 reading ID 仍随机生成，逐条保存 ID 和插入顺序；不为跑分修改生产 ID 生成器。
4. **把尾部写盘成本也交账。** 在每臂第 6 次完成后，在自己的合成实例执行一次显式 CHECKPOINT，记录开始/完成时间和前后统计。并列报告六次各自完整事务耗时、六次耗时之和、从首条开始至尾部 checkpoint 完成的总时长及每次摊销值。这个人工尾部不计入原请求 p95，也不能证明持续并发 checkpoint 下已合格。之后在相同 reading/权限上做一次 summary、contribution、小时 series、分页详情的等量读探针并核对内容；不先做会改变缓存/空间的 EXPLAIN INSERT。
5. **仅在首对有实质改善时，按 B→A 补一个相同 6 次的反序对。** 进入条件：B 六次全部提交成功、完整创建中位数改善至少20%、最大值不恶化，且含尾部 checkpoint 的每次摊销成本也至少改善20%。这只是排除旧 4% 级波动的方向筛选值，不是统计显著性声明或新产品门槛。新臂仍从同一模板开始；随机 reading ID 与 OS cache 残余影响必须保留为限制。如果顺序反转后收益消失，就停止，不挑选较快的一对。最多四臂、24次300k，不自动扩大样本找成功。
6. **计划/容量探针放在主测量之后。** 至多各取一份 A/B 同等阶段的实际角色 `EXPLAIN (ANALYZE, BUFFERS, WAL, SETTINGS, FORMAT JSON)` INSERT，明确属于会写入的回滚诊断，原样保存 FK/排序/输入/缓冲区/WAL及失败。若 B 已稳定具备 5 秒候选依据，再在一份 B 上做一次 500k 实际 workspace Store 容量探针，仍保留10秒单SQL限制。其成本单列，不能混入300k比较或由管理员延时补成功。

每个创建样本都记录 UTC 开始/结束、单调时钟完整耗时、成功/错误/SQLSTATE、所有已完成阶段及失败阶段、行数/摘要、成员与索引增量。失败的 SQL 阶段当前 observer 不会回调：从最后完成阶段、外层总耗时与服务端错误定位，不能补造阶段时长。观测和校验放在计时外，样本之间不插入大量查询或睡眠来稀释后台写压力。

独立管理员连接采集已有 PG16 统计：`pg_stat_bgwriter` 的 timed/requested checkpoint、checkpoint/后台写入及 backend fsync；`pg_stat_wal` 的 bytes/FPI/buffers_full/write/sync；`pg_stat_io` 的 backend 类型、reads/hits/writes/evictions 与 I/O 时间。保存阶段边界统计，短周期采样仅用于对齐日志和活动等待；不安装扩展、不改变工作负载连接。累计统计存在发布滞后，读取使用新事务并记录 stats_reset，不把每秒差分当精确逐请求归因；checkpoint 日志区分 WAL、定时及手动原因。I/O timing 两臂同时开启，不能把未开启时的零值解释成零成本。[PG16 统计说明](https://www.postgresql.org/docs/16/monitoring-stats.html)

缓存口径为“同一磁盘模板、PG 新进程、首条保留”，不是 OS 冷缓存；不运行系统 cache flush，不停止其他应用。物理模板一致也不能消除随机 reading ID、OS 页缓存和主机负载差异，因此结合反序结果、阶段与 I/O 变化判断，避免仅凭一个更小数字认定因果。

## 资源预算、停止与恢复

本轮只读 `df` 得到 `/private/tmp` 所在卷约 37GiB 可用，这是检查时读数，执行前重查。主机内存总量的 `sysctl` 读取被沙箱拒绝，本计划不猜测 RAM 总量，也不因这次可选读取去提升权限。

| 资源 | 此切片预算与保护 |
| --- | --- |
| 并发 | XI 只运行一个 PG 实例及一个被测创建事务；不与总负责人性能/角色负载重叠；Go GOMAXPROCS 保持10 |
| PG 内存 | shared_buffers 仍128MB，work_mem 仍4MB，角色连接上限不变；PG与测试进程 RSS 合计4GiB作为保守停止阈值，共享页重复统计需注明，不声称这是强制隔离配额 |
| 新增磁盘 | 模板、当前臂及原始结果共计预算12GiB；开始前可用空间至少32GiB；实际新增达到12GiB或剩余降至20GiB即停止发新负载。4GB是WAL软值，不当成物理上限；逐阶段/样本记录pg_wal实际大小及总目录占用 |
| 耗时 | 首对预计数分钟，含必要反序对约10–15分钟量级；诊断总窗口20分钟，超出不自动加时。编译/最终验收另列，不用诊断超时放宽产品时限 |
| 产物与生命周期 | 日志、JSON、参数读回、计划、错误及哈希保留在新输出目录，不覆盖旧报告。成功臂停止并核验产物后，才清理本次创建、精确登记且可再生的临时副本；失败臂保留现场并停止后续臂以守预算。模板仅用于本次实验 |

出现约束/金额/身份/权限/取消回滚差异、durability 设置漂移、磁盘或内存超预算、主机资源明显争用，即中止并保留现场。对照臂 SQL 超时也必须作为结果；先核验回滚，不能把失败扔掉或切管理员继续跑。若失败破坏了同等成员增长，后续比较只到双方共同完成的规模，并将方向结论标为不足。

如果 B 只减少 checkpoint 次数但完整耗时、尾部成本没有实质改善，不继续调更大 WAL 或顺带加内存。若反序复核后 B 中位数仍超过5秒，或500k仍因真实角色限制失败，就说明此切片不足以完成A0，向总负责人提交剩余阶段成本，重新选下一切片；这不是重新向用户请求已批准语义的授权。若部分改善接近门槛但波动仍大，保留原始结果共同判断，不反复重跑相同负载。

恢复此实验配置只需停止并退出新临时实例；任何实例清理均不涉及旧工作树、既有测试实例、原报告或用户数据。若4GB成为最终候选，需另记其峰值WAL与恢复影响；正常停启不能声称验证了崩溃重放。产品既有备份→新库恢复及失败拒绝检查仍是独立必要证据，不由模板副本可启动替代。本轮不实施故障注入或更改正式部署配置。

## 进入最终验收时的条件与证据

这部分说明后续如何完成原目标，不授权在本轮立即执行。只有对照显示最终代码下有稳定达标可能、角色容量可用且总负责人同意进入资格验证，才运行完整资格负载。

- **固定最终身份。** 对被测最终代码/构建、SQL、schema、PG版本、参数和fixture逐项绑定；确认使用当前内建SHA-256路径。任何后续产品或预算修改后，相关资格结果重新绑定并验证，不能复用旧 Store 成功或“等价实现”的性能推断。
- **原300k门槛完整保留。** 20次300k成功提交，保留全部6M成员，nearest-rank p95≤5000ms；首条、失败和超时均公开，不用成功子集p95掩盖错误。若使用4GB，记录全过程自然checkpoint重叠与尾部成本；若20次没有自然checkpoint覆盖，资格结论注明持续写入证据缺口，由双方确定最小追加覆盖，不能无限增大窗口或只交最佳热态。
- **实际production HTTP与数据库身份。** 使用真实生产handler/TLS/认证/CSRF和正式workspace/reader角色，完成20×300k创建、真实500k创建、500001→422及Store ErrTooLarge。全部请求计时至完整响应体读完，记录完整延迟、状态、SQL取消、事务完成和六表零残留；网络错误同样留记录。诊断Store成功不等于HTTP成功。
- **预算有依据地处理。** 本切片不改变现有HTTP3秒/SQL10秒。若完整创建已满足原5秒但HTTP3秒仍不相容，双方按最终完整延迟、500k容量、取消回滚和连接占用审查仅创建端点的明确预算，再实施与验证；它属于原本地工程范围，不新增用户审批。放宽预算不替代5秒验收，不改同步201为异步202，不全局延长读取时限。若SQL10秒也不相容，须单列该证据和影响，不能临时SET绕过角色约束来过关。
- **避免将写成本转移给读。** 保留10个独立主体/各自reading、固定600个请求、20RPS×30秒、summary/top各300的原HTTP峰值；记录调度时刻、排队、请求与端到端p95、实际吞吐和所有失败，不丢tick减载。原summary/top p95≤2秒要求不变。把该峰值安排在大reading成功保留后，以覆盖成员与索引增长；读70000成员的7天窗口，另外对一个300k/30天reading实际执行summary、贡献、小时series、连续分页与末页/详情，记录各自延迟与准确数值。增补读探针不替换或摊薄原600请求负载；规模不相同的读数分列，不以小reading代替大reading。
- **解释读成本。** 对上述查询留必要实际计划/BUFFERS、扫描行数、temp和响应字节；逐级检查成员总数、精确整数金额、趋势总和/零桶、分页无重无漏、固定ordinal与详情引用。不把HTTP200当作数值验收。比较每reading增量与累计table/index/WAL，明确Store与HTTP fixture的不同初态和阶段。
- **保存与恢复。** 迟到事实不改旧reading；重复/冲突不换选定Evidence；撤权、过期、取消与回滚保持。通过现有正式备份/新库恢复路径校验同一ID/引用/十五字段值、金额、digest、解释版本、角色和guard；缺Evidence/成员、hash或版本损坏仍拒绝。下行不原地清空，旧发布合同与schema24字节不重写。
- **原验收边界。** 保留R3适用检查、TD-01至TD-04及13项目标边界；A1/account/application/purpose等未知不冒充完成。已有未受影响证据在commit/条件仍适用时引用，受影响项实际重验。所有结论限定Local，CI/Artifact/Target/External Integration/Production分别报告。继续禁止UR联络、真实流量、真实凭据、外部消息、推送/合并/发布/部署和用户数据清理。

本轮交付到此计划为止。待双方评审决定上述唯一切片及可用窗口后，再修改诊断测试/runner并执行；当前没有产品或配置变更需要回滚。
