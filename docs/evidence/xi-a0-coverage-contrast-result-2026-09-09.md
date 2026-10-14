# XI-A0：coverage补测9次有效，非MCV custom差计划已复现

**本轮coverage 9次目标执行全部有效，完整auto返回与755e16的同MCV有效基准一致。** 非MCV输入G/C中位数为172.324/2506.404ms；custom将实际300000成员估为1行，执行大量逐行索引访问。结合此前有效digest对照，两个阶段的既有行统计机制得到支持；真实创建事务、INSERT原因和A0资格仍未闭环。

基线提交`2d773fae30d7333e38903ca9c6b9ebbccee08ca8`。经理批准仅coverage的9目标/独立20分钟、4256MiB累计预算和23GiB启动，保留其余首次目标/停止线。平台容量中断发生在PG执行前；接续核对仅有Go草稿、无coverage输出、实例shut down，未重复负载。实际运行2026-09-09 07:25:46.410853–07:26:02.592594 UTC，测试9.15秒、exit0，完整周期16.181741秒。

## 参数、语义与执行证据

- 复用`/private/tmp/xi-pc-hcpwpsza/data`，未新增副本。端口59858；本轮真实workspace PID58773，非superuser、cache512/CacheStatement、默认auto、SQL10s、目标事务READ ONLY。全部目标同PID、实际prepared SQL/OID/name/prepare_time；强制模式计数增量和事务后auto恢复均核验。
- 固定二进制`/private/tmp/xi-pc-mcv-build-ojjm9cyh/db.test`，SHA256 `39c8c8c5adfdfe7b595438f3353a1c290eb467e8c8853652cc53ccfde3e026d7`，359输入与准备绑定一致。本轮只改诊断Go/runner、计划与记录；产品SQL/索引/角色/持久性/HTTP预算未改。
- coverage SQL、类型、$1 subject及所有非reading参数与原批准参考逐项相等；只有$2 reading交错改变。两个固定ID实际读回各300000成员、相同ref，总量19 headers/5700000 members；MCV/非MCV分类仍符合约定。
- auto使用MCV `xwr_vzD1s7lPgy1RE3-Trd5V5Q`，按完整参数唯一匹配755e16的coverage自动第7轮。完整columns及九字段行相等：来源count=1、capture_started=null、last_durable/projection_watermark均为`2026-09-09T11:59:30+08:00`、received_watermark为空、gap_count=0、后三个布尔值为true/false/false。来源数、水位、布尔字段的负向回归均可拒绝差异，未以SQL成功或自我比较代替语义核验。
- 第1次auto约1120.269ms；随后8次顺序为G(M),G(N),C(M),C(N),C(N),C(M),G(N),G(M)。原1+36+18共55次全部排除；此前无效coverage原始记录保持无效，不与本轮拼接。

原始[diagnostic.json](../../output/xi-a0-coverage-run/diagnostic.json)、[run.json](../../output/xi-a0-coverage-run/run.json)、[输入](../../output/xi-a0-coverage-plan/queries.json)及[派生参数/结果审计](../../output/xi-a0-coverage-run/analysis.json)可直接复核。EXPLAIN不返回正文：本轮仅MCV auto验证了完整返回；非MCV的存储ref和EXPLAIN实际行数不能冒充其结果正文已重新核验。

## 对照结果与计划解释

| 输入 | G1 ms | C1 ms | C2 ms | G2 ms | G中位数 ms | C中位数 ms |
|---|---:|---:|---:|---:|---:|---:|
| MCV | 558.143 | 397.255 | 441.470 | 168.540 | 363.342 | 419.362 |
| 非MCV | 181.504 | 2675.481 | 2337.328 | 163.143 | 172.324 | 2506.404 |

MCV首个G样本明显较慢，存在预热/运行波动；两次G/C不构成p95或通用速度保证。非MCV两次custom均复现同一差计划，generic两次均约163–182ms。

非MCV custom估算总代价45.48。member_state的成员Index Only Scan估1、实际300000行；随后F0主键和evidence主键各循环300000次。gap_state又扫描实际300000成员并对F0做300000次索引访问。两个成员扫描共600000实际行，F0探测600000次，加evidence探测300000次；这是当前计划的重复工作，不是数据重复或结果多计。根shared hit/read分别3815633/690729和3822768/683594，temp写均2576块。

generic估算总代价313961.13，成员估379381，采用并行成员/F0/evidence连接，gap空结果避免另一段成员/F0构建；根shared读61635/61443、temp写3664/3668。MCV custom估成员391976，根成本316124.35，仍采用并行主路径但gap分支有额外构建。根buffer/temp是访问计数，不是唯一物理页数或瞬时临时文件峰值。

19份选定表统计快照一致，未观察到本轮ANALYZE/autoanalyze变化。成员表n_distinct仍14，last_autoanalyze为12:46:44.402182+08:00，reltuples4193289、n_mod_since_analyze1500000；这些统计估计与实际5700000计数须区分。

现在的证据链是：此前count在新会话第6次切换generic且变慢；digest与coverage则分别在非MCV custom下估算崩塌。不能把三者统一归因于generic，也不能全局统一force_custom/force_generic。原创建最后成功样本的digest973.975ms、coverage2325.257ms与当前非MCV custom区间接近，是支持线索；原会话计划缺失，不能回填直接证据。当前仍未包含刚INSERT成员、脏页、FK及提交成本。

## 资源、安全与验证

| 检查 | 实际结果 | 边界 |
|---|---:|---:|
| PG启动前真实空闲 | 25151803392 bytes | 至少23GiB |
| 首目标预算余量 / 真实空闲 | 549598237 / 25151819776 bytes | 至少512MiB / 21.5GiB |
| 观测最大累计计量 | 3980479459 bytes | 小于4462739456 bytes（4256MiB） |
| 观测最大保守合计RSS | 290422784 bytes | 小于4GiB |
| 最低真实空闲 | 25088008192 bytes | 高于21GiB停止线，保留20GiB保护量 |
| 总周期 | 16.181741秒 | 20分钟 |

合并9次后台样本、9次目标前检查、边界及最终检查；全部已记录检查通过，短时并行工作可能位于采样间隔，不能把RSS观测最大值当作不可见瞬时峰值保证。计量包含5个旧二进制及本轮新二进制、所有历史/新输出和同一基线的cache增长。

原源1604文件before/after相同，交付再次核对原源和复用实例均shut down。PG仅有停库checkpoint；原失败场景、全部六个二进制及原始输出保留。编译、真实正确输入/基准选择、语义负向/参数合同/OID/混合统计回归、默认跳过及vet通过，实际9目标通过；没有产品改动或HTTP资格新声明。

## 下一步最小诊断：一次正常创建的自动计划日志（仅建议）

建议下一步对保留5.7M诊断实例执行**一次正常Store CreateWorkbenchReading（300k、新ID）**，在实际写事务内自动记录计划，不手工再复制一套创建SQL。目的为确认INSERT的源计划、约束调用/写入量及刚写成员时的digest/coverage路径；单次仪器化结果不用于5秒资格或p95。

已只读确认本机现有`/opt/homebrew/Cellar/postgresql@16/16.14/lib/postgresql/auto_explain.dylib`（53376 bytes，SHA256 `e880e306b60a240cfdee2ff6a653f92ff59c63a0e67fd9c7e0fcc70687b78cf6`），尚未加载或试跑。PG官方支持通过session_preload_libraries加载并自动记录实际计划，提供analyze、buffers、WAL、settings及trigger统计；关闭逐节点timing可减少仪器开销，但不保证消除开销。[PG16 auto_explain](https://www.postgresql.org/docs/16/auto-explain.html)。现场与资料核对见[next-probe-support.json](../../output/xi-a0-coverage-run/next-probe-support.json)。

建议具体边界如下，尚未实施或获得写入授权：

1. 只在该合成实例的启动选项加载auto_explain；先用同一真实workspace的轻量SELECT确认JSON计划确实记录。`log_min_duration=0`、sample_rate=1、log_analyze/buffers/wal/settings/triggers=on、log_format=json、log_timing=off；log_nested_statements=off，避免把逐行FK内部查询写成海量日志。关闭timing后不承诺节点/trigger时间，只使用实际可得的行数、调用及buffer/WAL统计。若模块/格式验证失败，不运行大创建，也不安装工具或更改真实角色权限。
2. 保持pgx cache512/auto、1GB WAL、128MB/4MB、约束/触发器/持久性及10秒SQL不变。复用产品Store入口和已有阶段观察器，仅一个300k创建尝试，成功保留新合成reading；失败保留SQLSTATE/已完成阶段及日志，不补样或关闭约束。记录新ID、实际SQL/参数和PID；prepared/统计检查放在Store返回后，避免在占用连接的事务内从单连接池再次查询。
3. 若成功，应真实读回20 headers/6M、该reading300k、完整ref/coverage及原阶段输出；若失败则核对回滚后的原19/5.7M及相关表无本次残留。即使诊断恰好留下6M，也**不自动触发500k**，它不等于从共同空模板完成的原20次资格。新增写入后的副本明确成为新诊断现场，不能继续称作原未写入的5.7M前态；原1604文件源继续只读保留。
4. 这是一次实际合成写入及额外PG诊断设置，需要经理共同审阅授权，当前9次只读授权不覆盖。正式准备还须绑定代码/库/选项、磁盘组件和停止策略。初步建议累计4480MiB，较本轮多224MiB；以当前约3733MiB常驻计量为起点，预留新构建/cache48、成员与索引96、WAL增长256、temp160、日志8和未建模余量128MiB，约696MiB，须现场校正。WAL上限是软限制，不能把256MiB当保证；23GiB启动、512MiB/21.5GiB首次负载、21GiB停止、RSS4GiB和独立20分钟继续约束，超出即停、不自动扩预算或清理。

这一路径直接观察产品实际参数与事务，减少此前手工参数重放的风险。它仍可能因仪器开销或原SQL限制失败；异常查询未必留下完整自动计划，缺失须如实记录，不以另一会话的静态计划冒充。当前仅提出方案，没有启动该模块或创建新reading。

## 产品候选组合与最终qualification

待真实创建诊断后，优先评估组合：count单独`pgx.QueryExecModeExec`，digest与coverage连续区段使用事务局部generic，完成后恢复原plan_cache_mode，错误路径回滚；不改连接全局默认、SQL文本或参数。这是待测产品设计。count文本参数编码、模式恢复/错误取消和实际角色权限需验证；两种大成员SELECT当前仅有300k证据，不能忽略0/少量/70k等小reading强制generic可能退化的风险。是否按已知insertedMembers选择模式，以及阈值，须用代表性规模实测决定，不先拍定常数。

原19个成功样本的INSERT阶段中位3697.113ms、nearest-rank p957082.444ms，3次自身超过5秒，另有INSERT超时。即使消除全部SELECT耗时，这组历史INSERT仍不能支持整体5秒达标推论；不能把不同诊断中的节省相减后当作新总耗时。若实际INSERT计划暴露可改源查询/排序路径再设计最小候选；若主要是不可省略的约束/写入成本，则需基于证据讨论结构方案，不能关闭FK、持久性或放宽验收掩盖。

候选先完成真实角色、空/非空过滤、时间边界、撤权、取消回滚、ref/coverage及小/大规模语义验证；再关闭诊断日志，以精确最终代码/二进制从验证为空成员的共同模板完成原20×300k全部成功且p95≤5s、6M后500k、增长后summary/top/hourly与选定组全量分页、ordinal/money/ref及实际HTTP/lifecycle。HTTP现有3秒实现预算未改，其后续决定仍须结合整体目标评审，Store通过不能替代实际HTTP可用性。

## 交付范围

本次批准的coverage-only9目标已完成，先前无效9条仍排除。完整参数、基准、源码/二进制、历史65项与原49保护文件/3失败记录的核验见[delivery-verification.json](../../output/xi-a0-coverage-run/delivery-verification.json)，最终字节绑定见[evidence-binding.json](../../output/xi-a0-coverage-run/evidence-binding.json)。本地提交；原13项/TD/R3/A0、D-37-01及创建/HTTP资格未改变，没有push/PR/merge/release/deploy、真实数据/凭据、UR联系或跨任务消息。
