# XI-A0：参数化计划缓存诊断方案

状态：**经理已审定诊断设计，并有条件授权下述强制 CoW 修订；本次按修订实施。** 原审阅快照保留于 `9fd8c3e0eceda1b80f3b131b42d8b58386a335f6`，计划 SHA256 `858d3217154b70d8d57981a8e9cf8a4f121f6eecb4b25e769d39271ff7a14884`。以下原方案及准备时事实保留，执行时以末尾资源修订为准。产品 SQL/索引、PG 默认、HTTP 预算与原资格条件不变。

## 判断与精确输入

计划缓存应优先作为竞争假设。原始数据中 eligible_count 前五次为 45.8–57.5ms，第六次为 287.0ms；digest/coverage 在第七次才从约 300/400ms 跃至 990/2348ms。count 不读取 reading_members，因此目前只能确认顺序相关的台阶，不能仅由累计成员解释。

本地 `OpenWorkspaceStore` 使用 `sql.Open("pgx", dsn)`；固定 pgx v5.10.0 的 `conn.go:171/191/898` 确认默认 cache=512、CacheStatement，并按 SQL 原文复用服务端 prepared statement。PG16 的 auto 规则先用五次 custom，再比较 generic 的估计成本；并非第六次必然 generic。当前会话可通过计数和 EXPLAIN EXECUTE 验证选择。[PG16 PREPARE](https://www.postgresql.org/docs/16/sql-prepare.html)、[pg_prepared_statements](https://www.postgresql.org/docs/16/view-pg-prepared-statements.html)。旧连接已结束，无法追溯其原 prepared 状态；本实验只能复现/排除机制，不能把新会话结果冒充旧会话记录。

[queries.json](../../output/xi-a0-plan-cache-plan/queries.json) 已从当前 Go 原始字符串提取三条完整 SQL，逐条 SHA256 绑定；没有改空白、改谓词或手写等价 SQL。参数保持原 Go 的 string、time.Time、[]string，空数组为 `[]string{}`，不换 NULL：

| 查询 | `$1…$n` 的确切映射 |
| --- | --- |
| eligible_count | subject、2026-08-10T03:59:30Z、2026-09-09T03:59:30Z、单一原 source ID 数组、`["project-300k"]`、空 cost_project 数组 |
| membership_digest | reading ID |
| coverage | subject、reading ID、相同 source 数组、相同 project 数组、空 cost_project 数组、上述 period start |

subject/source 来自原 fixture；auto 第 1–7 轮分别使用原样本 1–7 的七个成功 reading ID，保留生产调用每次 ID 不同的特点。后续对照统一固定第七个 ID。完整值和预期参数类型在 JSON 中；现场以 `parameter_types`/OID 再核对，不用猜测转换类型。三条目标 SQL均为 SELECT。

## 执行序列与证据

评审同意后新增独立 test/runner 入口，复用已有连接、快照和资源观察方法；不修改已绑定的旧 runner 或产品查询。先离线编译和 SQL 字节对照，再启动唯一副本。

1. **前态与连接。** 从停库失败 data 完整复制一次至 `/private/tmp/xi-pc-<随机短名>/data`，逐文件验证；原目录只读。PG16.14、1GB WAL、128MB shared_buffers、4MB work_mem、原持久性/并行/autovacuum/I/O 计时不变。真实 xinsight_workspace 登录；调用 OpenWorkspaceStore 后保留一条 `*sql.Conn`，目标和 prepared 观察都走此连接。记录 backend PID、session/current user、10s statement_timeout、plan_cache_mode、pgx cache/mode；PID 变化或重连使对应序列不再连续，不合并计数。
2. **基线。** 启动后读回 19 headers/5.7M members 与七个目标 reading 各 300k、membership_ref，并记录关系/统计前态。若不符即停止负载。此读回单独计时，不能回填为上一轮已经完成的检查。记录所有相关表的 reltuples/relpages、last_analyze/last_autoanalyze、n_mod_since_analyze，及目标谓词相关 pg_stats；不主动 VACUUM/ANALYZE，不新增索引。全表计数若触发原 10s 上限则如实停止，不用管理员延时补成功。
3. **A：auto 会话序列。** 同一连接连续七轮，每轮一个 REPEATABLE READ / READ ONLY 事务，依次 count→digest→coverage；只执行上述精确查询和参数，没有 header/INSERT。每条实际执行前后读取同连接 `pg_prepared_statements`，按原 SQL精确匹配，保存 name、statement、parameter_types、from_sql、prepare_time、generic_plans/custom_plans 与增量。结果、耗时、SQLSTATE、事务/PID一并保存；count=300k，digest 与原 membership_ref 对照，coverage 原始行与对应旧 reading 所能核对字段另列。前七轮不插入 EXPLAIN 或额外执行目标查询，避免改变第五/第六次边界。
4. **A 的计划取证。** 七轮结束后每条增加一次明确标记为第八次诊断的 `EXPLAIN (ANALYZE, BUFFERS, SETTINGS, FORMAT JSON) EXECUTE <实际服务端名称>(<相同类型的参数值>)`。名称来自该连接的 prepared 视图；参数用服务端 quote_literal/quote_ident 与已核对类型生成 EXECUTE 包装并保存，核心 SQL仍是同一参数化 prepared statement。不能以 `EXPLAIN SELECT …字面量…` 或外层 `$1` 绑定失败后的静默改写替代。EXPLAIN 前后也记计划计数；它是另一次执行，不冒称精确捕获了第六次计划树。
5. **B：同一晚期数据、同一连接的 G→C→C→G。** 对每条相同 prepared statement、固定第七个 reading 参数，依次在独立只读事务内 `SET LOCAL plan_cache_mode=force_generic_plan/force_custom_plan/force_custom_plan/force_generic_plan`，各执行一次同样的 EXPLAIN ANALYZE EXECUTE。每次记录计数、设置、完整 JSON。事务结束即恢复 auto；不 ALTER ROLE/DATABASE/SYSTEM，不切换 pgx 默认执行方式。反向顺序帮助识别预热顺序影响，不宣称严格控制了 OS 缓存。不开第二实例、不 flush 缓存、不重新复制作冷启动对照。

上限为 **21 次普通目标执行 +3 次 auto EXPLAIN +12 次强制模式 EXPLAIN =36 次目标执行**，并非创建/HTTP负载或 p95 资格。全部单 SQL仍 10s、总运行窗口最多20分钟、合计 RSS4GiB；无并发目标查询。观察查询不计入目标执行次数，计入总体资源/时间。

从完整计划提取节点类型、估计/实际行数及 loops、过滤行数、join/scan 路径、索引条件中的参数或常量、shared/local/temp blocks、I/O read/write time、sort/hash spill、Planning/Execution Time。节点 loops 与并行计划单列，不能将所有节点时间/块数直接相加。目标运行耗时与 EXPLAIN 的测量开销分开；强制模式的相同参数对照结合计划和 BUFFERS 判断，不凭单条时延下结论。

SQL取消后先保存错误并回滚，再取同连接状态。pgx 可能失效/移除该 prepared statement，须记录缺失或新身份，不能把重建后的计数当连续序列；停止该查询剩余取样，其余独立查询在资源许可时按原方案继续，不重新凑七次。资源停止则停止全部新查询。后台 autoanalyze/DDL/统计更新可能失效计划；每轮及对照前后记录统计时间，发生变化时明确限制可比性，不自动重跑或关闭 autovacuum。

## 结果如何改变下一步

- auto 的台阶与 custom→generic 计数切换同时出现，且 G 慢/C 快在反向顺序下仍伴随可解释的计划/行数/I/O 差异：支持计划选择机制，下一步提出最小候选；不直接采用 force_custom 或宣称整体根因已证实。
- auto 不切 generic，或 G/C 相近而相同模式的后次耗时随 blocks/read time 下降：优先考虑缓存/读取周转；该副本成员量不变，有助于排除“必须继续累积成员才变慢”，仍不等于复现原 INSERT 后的缓存和统计前态。
- 二者都有证据则报告混合因素。取消、资源不足、计划失效或模式未切换则保留明确不确定性，不能用强制模式快推导原 auto 必然慢。

本实验省略 INSERT；它不能解释第10次 FK 内的全部取消成本。500k、后续金额/分页及 HTTP/A0 资格仍需后续对应测量；shared_buffers512MB 只保留为后续候选。

## 资源方案：建议 4GiB / 25GiB / 21GiB

2026-09-09 05:14:43 UTC 只读核对：失败 `data` 逻辑量3690800421 bytes，实际分配3713318912 bytes（3541.297MiB），1604文件，停库且无pid；整个父目录还含 PG 日志，因此与经理测得父目录逻辑量有小幅差异。当前可用26879451136 bytes，约25.033GiB。[preflight](../../output/xi-a0-plan-cache-plan/preflight.json) 与 [源文件清单](../../output/xi-a0-plan-cache-plan/failed-source-files.json) 已保存。

**4GiB 可以作为受监控、失败即保存的本地诊断预算，不能保证全部查询一定完成。** 一份普通完整复制后仅余约554.7MiB；本方案不依赖 APFS 克隆节省、不增加隐含第二份数据，也不设置新的 temp_file_limit 或改变并行计划以凑预算。

| 新增项目 | 预算 MiB |
| --- | ---: |
| 一份完整 data，按实际分配计 | 3542 |
| 单条只读查询临时文件峰值，含 leader/workers 合计 | 320 |
| 启动、后台维护、WAL/关系额外增长 | 128 |
| 新二进制、cache 增量、计划/日志/输出 | 64 |
| 分配及未建模余量 | 42 |
| 合计 | **4096** |

320MiB 是分项估算，不是 work_mem 推导出的硬上限；work_mem 为各运算节点预算，temp_file_limit 默认不设限且按进程限制，不能据此保证全查询临时占用。[PG16 资源参数](https://www.postgresql.org/docs/16/runtime-config-resource.html)。复制前25GiB入口、复制后21GiB停止线及原20GiB安全余量的关系明确：25−4=21，21至20留停止缓冲。既有失败副本/模板/B1均已经计入当前空闲读数，全部保留。

执行时重查入口至少25GiB（编译完成后、分配副本前）；复制后/PG启动前重新统计，只有剩余新增预算至少512MiB且可用空间至少21.5GiB才发第一条目标查询。使用实际分配与逻辑长度中较大值统计本任务新目录/构建/输出，另加构建cache增量，同时监测文件系统真实剩余空间，覆盖临时文件和外部空间变化。沿用现有资源采样，并在每条目标查询前检查，不把28GiB门槛带入本切片。

新增达到4GiB、可用降至21GiB、合计RSS达到4GiB或20分钟耗尽：取消当前诊断连接查询，停止发新查询，使用正常 pg_ctl fast 停本次副本，保存结果/SQLSTATE/资源停止原因；这些是采样停止线，不冒称无瞬时超出。原20GiB是停止缓冲的保护目标。查询spill或后台增长超过估算时不提高预算、不清理旧现场、不自动重试。入口不足或复制后门槛不满足则保留已完成离线工作和精确读数，交经理调整工程资源决定。

新副本及输出使用独立短随机目录和独立 `output/xi-a0-plan-cache-run`；开跑前已有 run.json 则拒绝覆盖。结束后停库、保留副本，原失败源全文件摘要再核对；不启动原失败实例。不会复制到保存仓库、改已有服务、接触真实凭据/API/UR或执行任何外部写操作。

## 交付与当前评审点

执行后交付全部36次以内的原定执行记录、逐会话 prepared 计数、原始计划/绑定参数、查询结果校验、统计变化与资源曲线、停库状态及代码/输出哈希；不写“资格通过”。本轮先提交本计划、精确SQL/参数和源/资源快照；诊断 test/runner 仍待本计划审定后实现。请求经理审定的变化仅为上述只读诊断序列和4/25/21资源方案，无需用户重新选择产品方向或批准外部动作。

## 已授权资源修订：强制逐文件 CoW，24GiB 入口

认可经理提出的原生 CoW 路径。独立读取本机 clonefile(2) 手册并运行本任务的8MiB合成文件探针：普通文件 clonefile(flags=0) 成功、inode不同、初始摘要/权限一致；分别修改源与副本，另一份内容均不变。对已有目的文件再次调用返回EEXIST，副本未被覆盖。探针仅在本任务新建临时目录运行并清理，不触碰PG源。正式准备输出保留同一函数的探针结果。

采用标准 shutil.copytree 的目录遍历/目录元数据复制，普通文件 copy_function 只调用原生 clonefile；任何返回错误即抛出并终止，没有 cp -c、普通复制、硬链接或目录 clonefile 回退。源必须仍是指定停库 data，1604文件摘要完全匹配；额外核对根目录和全部子路径的类型、mode、uid/gid、flags、xattr摘要。链接、特殊文件、非空pg_tblspc、特殊权限位或现有ACL均停止，不扩展复制范围。复制后完整类型/权限/字节摘要一致，所有普通文件与源非同inode才允许启动；交付时原源再次全量核验。

强制CoW入口改为 **至少24GiB真实可用空间**。仍按副本的逻辑量与st_blocks分配量较大者计入4GiB预算，不把共享块算零；真实空闲独立监测，涵盖后续私有块和临时文件。原4GiB新增、21GiB停止、20GiB保护余量、复制后512MiB预算空间/21.5GiB真实空闲、RSS4GiB/20分钟和每条目标查询前检查全部保留。若native clone失败则停库/未启动并保留现场，不改用不足25GiB入口的普通复制。

准备和执行分别写 `output/xi-a0-plan-cache-ready-02` 与 `output/xi-a0-plan-cache-run`；不覆盖原计划快照。新Go测试/runner仅诊断用途；正式36次序列不包括INSERT，不改变产品。构建cache保守计入相对上次保留容量code-binding的全部增长，包含本轮最初编译检查，避免遗漏准备成本。原缓存、原失败副本、旧模板/B1不清理。上述独立验证通过即可按本次持续授权执行，无需再次等待经理或用户批准。

首次准备在源元数据检查时发现本机Python缺少os.listxattr，尚未复制或启动PG；失败记录及成功的clone探针保留在 `output/xi-a0-plan-cache-ready`。改用本机sys/xattr.h确认的原生listxattr/getxattr接口，另起ready-02准备目录，仍不回退普通复制。此兼容修正不改变SQL、计划序列或资源门槛。

## 已授权恢复：复用同一副本，新的36次/20分钟

经理已独立审阅 `9c9deec663417392e730ad5e16707aa013489067` 的CoW/观察器失败、37项绑定与修复二进制回归，并明确批准一次新的最多36次目标执行/20分钟。复用 `/private/tmp/xi-pc-hcpwpsza/data`，不再复制；先前会话的一次count、旧二进制和旧输出独立保留，不拼接为同一序列。修复版二进制SHA256 `aaa6ecbdbc7b27256c0eb8b7ecff59936d9ac96b48211b49d0dda73cb8221a5a` 的359个当前输入重新核对；本轮只更新runner与记录，没有再改Go目标/观察器代码。

累计新增预算为4128MiB（4328521728 bytes），包含该副本、两个保留二进制、全部准备/失败/修复/新输出和相对保留基线的cache增长。24GiB真实可用启动检查、发样前512MiB新增预算余量/21.5GiB真实可用、21GiB停止/20GiB保护余量、RSS4GiB、每SQL10秒和逐目标资源检查均不变。新20分钟只绑定此次明确批准的新尝试，不覆盖/重置旧记录。

新入口 `python3 scripts/xi-a0-plan-cache-diagnostic.py --resume` 只接受上述已停止的精确目录，输出 `output/xi-a0-plan-cache-run-02`；已有输出则拒绝覆盖。先保存保留实例当前inventory，明确重启与旧baseline查询留下的缓存/统计前态不同，再由新连接读回角色/PID、19/5.7M及七个reading。使用新PG日志文件，不把旧启动日志混作本轮事件。原七轮auto及G/C顺序不变；完成后分析能支持和不能支持的结论，后续产品候选与完整资格验证方案交经理评审，不直接采用配置/SQL优化。

## 已授权统计对照：同一副本，18次/20分钟，4192MiB与23GiB入口

经理接受755e16f的36次只读诊断交付，不验收A0，并批准下一次独立MCV对照。认可经理本次资源修订：只复用`/private/tmp/xi-pc-hcpwpsza/data`，不会再复制3.5GiB实例，23GiB启动门槛至21GiB停止线仍有2GiB缓冲；4192MiB（4395630592 bytes）累计预算比前轮多64MiB，为新保留二进制约18MiB、编译cache增量及新输出留空间。这个估计不保证足够，以离线编译后的精确计量及每个运行门槛为准。

累计计入原失败和成功诊断的全部路径、三个二进制、全部准备/失败/修复/本轮输入输出及相同基线起算的cache增长，不清理旧场景。启动前至少23GiB真实空闲；首次目标前至少512MiB累计预算余量且21.5GiB真实空闲，实际第一条目标前另做同步检查。运行中21GiB停止、20GiB保护、RSS4GiB、10秒SQL、20分钟及逐目标检查不变。门槛不足仅停止负载，保留已完成离线准备，不新增副本、扩大预算或自动重试。

限定digest、coverage两条原SQL及类型/其他参数不变，精确输入为MCV `xwr_vzD1s7lPgy1RE3-Trd5V5Q`和非MCV `xwr_XMf3SdgKSIV7KJ_kxrItBg`。先真实读回总19/5.7M、两个ID各300000及相同ref并对照原成功记录，再核对成员reading_id的MCV分类。缺少统计、分类改变、对象/基数/ref改变均判为前态无效，不换对象或手动ANALYZE来凑样本。每次目标前及EXPLAIN后重查分类；相关表统计另保留，变化需在结论中说明。

同一实际workspace连接，先对MCV输入每条SQL各执行一次auto以建立真实prepared statement，共2次；随后每条SQL按`G(M), G(N), C(M), C(N), C(N), C(M), G(N), G(M)`执行8次EXPLAIN ANALYZE EXECUTE，共16次。每个ID独立G/C/C/G，两个ID的前后顺序交错反转；保留PID、SQL/OID/name/prepare_time、计数增量、真实绑定参数和所有计划/资源/统计。最多18次目标执行；原失败1次与前轮36次均独立，不拼接。辅助身份/统计/基线SELECT不计为digest/coverage目标。

2次auto保留返回值及digest ref核验，16次EXPLAIN保留实际行数/耗时/计划，但EXPLAIN不返回digest/coverage结果正文；两个ID的存储ref读回不能冒充非MCV重新计算digest值。本切片隔离既有行统计选择率，不等同新写事务/INSERT/脏页条件或完整创建资格。若非MCV custom没有计划恶化，则不重复这项只读试验，应提出真实创建上下文的下一步设计；不会自行启动完整创建或采用产品候选。

入口`--prepare-cohort`仅离线编译、定向验证和计量，准备输出`output/xi-a0-plan-cache-mcv-ready`；`--cohort`执行新切片并写`output/xi-a0-plan-cache-mcv-run`，均拒绝覆盖。输入单独保留`output/xi-a0-plan-cache-mcv-plan/queries.json`。当前普通离线实现与本地提交已授权；编译绑定和全部运行边界通过即可依据本次条件批准执行，无需格式性再确认。原13项/TD、20×300k全部成功且p95≤5s、6M后500k、HTTP及后续读取标准、UR禁联与外部操作边界均不变。

首次启动读回基线后，观察器误将其他列的布尔/数值MCV一起解码为字符串，0条目标执行即退出，实例已正常停止。该状态不是分类变化；原始pg_stats确认指定分类仍成立。保留失败binary/source/runner/plan及全部输出后，修复为先按table/column筛选，再严格解码reading_id为字符串，并增加布尔/数值混合输入及非法reading_id类型回归。修复准备`--prepare-cohort-fix`单列输出，`--cohort-recover`仅允许该0目标且停库前态；复用首次运行06:49:51 UTC起算的同一个20分钟窗口，不重置预算或计数。恢复前重新编译绑定、计入第四个保留二进制/cache/失败输出，并重新满足全部门槛；超出原窗口或资源不足则不发目标。恢复不覆盖失败输出，单列`output/xi-a0-plan-cache-mcv-run-02`。

恢复执行18条后，事后参数审查发现coverage误替换$1 subject而非$2 reading；其9条对照无效，digest9条保留。参数映射及参考输入合同已修复并离线验证，但18目标预算已耗尽，未追加PG样本。原始成功标志不改写，派生审计明确排除coverage。最终事实、缺口及尚未获准的coverage-only9目标/资源建议见`docs/evidence/xi-a0-mcv-contrast-result-2026-09-09.md`；不把该建议当作本计划自动续跑授权。

## 已授权coverage补测：9目标/独立20分钟，4256MiB累计

经理接受2d773fa的有效9次digest及失败取证，明确排除9次coverage，并批准仅coverage的9目标补测。延用同一已停库副本和两个固定ID，先实际count/ref及分类核验，再1次MCV auto及`G(M),G(N),C(M),C(N),C(N),C(M),G(N),G(M)`八次实际prepared EXPLAIN。原1+36+18次均排除，原无效记录不覆盖，不重做digest。

单条coverage SQL/类型及全部非reading参数与原批准参考逐项相等；只在$2替换reading，$1 subject不变。auto完整返回对象（columns及九字段行）须与755e16有效的同MCV自动第7轮基准相等；该基准授权来源count=1，含已保存的时间、水位、gap与布尔值。基准根据完整参数唯一匹配，并绑定原文件摘要；若行值不同，保留实际/预期结果并停止，不将count=0或类型正确当作语义通过。EXPLAIN不返回正文，仍只证明实际计划/行数，不冒称非MCV结果正文已校验。

接受经理批准的4256MiB（4462739456 bytes）累计上限：所有5个既有二进制及新二进制/cache增量/全部旧新输出计入，编译后实测，不能将64MiB估计当作保证。启动前23GiB，首次目标前512MiB预算余量与21.5GiB真实空闲，运行21GiB停止/20GiB保护、RSS4GiB、SQL10s、逐目标检查不变；新独立20分钟仅绑定本次批准切片。资源或分类前态无效则停止，无复制、清理、自动扩预算或替换ID。

新增明确入口`--prepare-coverage`/`--coverage`，分别写`output/xi-a0-coverage-ready`/`output/xi-a0-coverage-run`；输入`output/xi-a0-coverage-plan/queries.json`，均拒绝覆盖。普通离线实现/验证、满足条件后的执行和本地提交已批准，无需格式性再确认。完成后提出真实创建上下文/INSERT与coverage的最小诊断及候选/qualification方案供共同评审；本轮不提前实施产品候选，原13项/TD/A0/20×300k全成功p95≤5s/6M后500k/HTTP及UR、外部边界不变。
