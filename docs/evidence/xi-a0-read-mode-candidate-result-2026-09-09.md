# XI-A0：读取模式校准通过，最终候选已准备，普通入口验证受启动空闲线阻塞

**0/16/70k/300k四组真实创建对照、模式恢复和取消验证已通过。** 据此选择已测得收益的最小规模70000作为保守启用起点：count局部Exec；insertedMembers>=70000时digest/coverage连续区段局部generic，成功恢复进入前模式；其余保留auto。最终代码已接入普通入口并通过编译、离线回归、边界单测和vet，**最终普通入口真实验证尚未执行，当前不是完整修复交付或A0通过**。

阻塞是实际磁盘空闲低于已批准23GiB启动线；累计4480MiB预算仍有余量。未重新启动PG、清理旧现场、新增数据副本或擅自放宽边界。经理可以据本报告的精确资源增量决定下一步，独立代码/验证准备已完成。

## 范围与实现

基线7dcbf77e0f4ba1465c3d502125831bada4841d8e。产品只修改[workbench_store.go](../../internal/db/workbench_store.go)：count原SQL前传入pgx.QueryExecModeExec，保持subject/time/text[]业务参数；依据已知insertedMembers选择是否进入generic区段，使用current_setting读取原模式、set_config(...,true)局部设置，完成coverage后恢复。任一SQL/应用错误交由已有整事务rollback；被取消的连接可能被驱动丢弃，不能把新连接的auto冒充原连接恢复。

INSERT SQL、ctid物理策略、FK/immutable guard、schema、默认WAL1GB/shared128MB/work4MB/wal_buffers4MB、持久性和SQL10s均未改变。校准时只给同一正常创建核心指定A/G selector；最终普通入口连接测得策略，无运行时实验环境开关或全局强制模式。旧auto路径继续承担小reading；这是当前实测所需兼容，不移除它。

执行前actual free在23GiB附近波动；首次准备记录不足，执行器随后重新核对，PG启动前实际24696545280bytes，达到24696061952bytes门槛后才启动。没有因准备记录不足而忽略实际启动检查。

## 代表性真实创建结果

同一已授权隔离实例 `/private/tmp/xi-pc-hcpwpsza/data`，前态20headers/6M。PG16.14，真实workspace PID71538，cache512/CacheStatement；auto_explain关闭，保留既有阶段观察器及pgx调用级SQL/参数记录。首次恢复模式auto，各组均通过正常成员INSERT后才执行派生查询，旧reading只读结果没有替代新写事务。

| 成员 | auto整次 ms | generic整次 ms | auto派生两段 ms | generic派生两段 ms |
|---|---:|---:|---:|---:|
| 0 | 8.572 | 697.078 | 3.807 | 695.283 |
| 16 | 5.684 | 418.234 | 4.047 | 415.241 |
| 70000 | 3073.615 | 1411.156 | 805.676 | 451.285 |
| 300000 | 8653.320 | 4796.395 | 3660.353 | 598.826 |

顺序A/G、G/A、A/G、G/A，每个组合只有一个样本，既不是p95也不是最优分界点测定。70k的generic digest249.576ms略慢于auto236.005ms，收益来自coverage569.670→201.710ms，因此依据是**两段合计收益**。300k digest981.226→366.081ms、coverage2679.127→232.745ms。0/16显著退化支持保留小reading的auto。

整条差异不能全归于模式：70k INSERT为2189.050/886.110ms，300k为4940.668/4147.676ms，模式区段在INSERT之后才开始。缓存、写入状态、统计等顺序影响不能用单对样本消除。300k generic整次4.796s是一个真实成功样本，距5秒很近，不能声称原20次全成功或p95通过。

8个对照reading完整ref/coverage、持久化读回、summary金额、ordinal均通过。四种规模另与不依赖member表、直接从不可变F0按时间/项目/排序计算的ref一致。实际计划缓存计数逐次核验：两条派生SQL在8个样本后均generic4/custom4，各次增量与A/G请求相符；没有将pgx调用成功当作实际模式证据。

原始[diagnostic.json](../../output/xi-a0-read-mode-run/diagnostic.json)、[运行记录](../../output/xi-a0-read-mode-run/run.json)、[参数/计数/语义分析](../../output/xi-a0-read-mode-run/analysis.json)和[分析脚本](../../output/xi-a0-read-mode-run/analyze.py)保留。

## 参数、恢复与取消

pgx15次count调用均使用Exec，包括8对照、2空恢复、1预期取消和4直接合同检查；每个对照之后的named prepared列表均无countSQL。真实text[]覆盖空source、引号/反斜线/逗号/Unicode、成本过滤和半开时间边界；离线另用当前pgx text-array codec正反编码。原授权检查代码未改，无grant subject被拒绝且无残留；已有撤权/角色正确性证据复用，本轮没有修改角色或grant。

221条调用记录包含stdlib.QueryContext自动前置的QueryResultFormatsByOID选项。这是**驱动选项，不是SQL $1**；分析依据当前pgx5.10.0源码和精确选项映射剥离它，再区分Exec模式选项与业务参数。原始记录保持原貌；count六个、INSERT七个、digest一个、coverage六个业务参数逐项核对，PID/reading/subject/filter/time均匹配。

- 两次额外0成员创建从force_custom_plan/force_generic_plan进入，PID71538前后相同、原模式恢复；8对照auto也均同PID恢复auto。
- Store在generic活动、digest完成后取消context，返回context canceled，前后计数完全相同。驱动记录旧PID71538的rollback已取消并丢弃连接；之后PID71777为auto。没有声称这是同连接恢复，也没有掩盖驱动清理。
- 另在PID71777同一事务用相同generic helper执行pg_sleep，确认服务端处于PgSleep后发pg_cancel_backend，实际SQLSTATE57014；Rollback后PID仍71777，模式恢复auto。这直接覆盖SQL取消/事务局部状态/物理连接复用。

最大11次Store调用按计划执行（8对照+2空恢复+1预期取消）；没有补样。最终30headers/6740032members，只新增10个header、提交740032成员；saved_filter/operation/F0/evidence计数仍0/0/500020/500100。取消的16行没有提交，统计n_dead_tup增加16属于保留的失败物理痕迹，不是业务残留。原20个header保留，未删除任何reading。

## 资源与现场

校准运行08:40:48.777307–08:41:21.551097UTC，周期32.773790秒；测试PASS25.34秒，独立20分钟内完成。首次70000负载前累计3833134416bytes、真实free24679387136bytes，满足512MiB余量及21.5GiB free。45份已记录边界/后台/样本检查全部通过：最大累计4045184336bytes、最大保守合计RSS730038272bytes、最低真实free23789649920bytes。采样间隔中的不可见瞬时峰值不能保证。

成员reltuples4193289及last_autoanalyze不变；所选表前后变化仅成员n_live_tup5993289→6733321、n_mod_since_analyze1800000→2540032、n_dead_tup0→16。统计估计不替代真实计数，前后快照不代表逐时刻采集。

原源 `/private/tmp/xi-cap-7qgkhj7u/qualification/data` 1604文件前后相同；原源与复用实例均正常停库，当前新前态30/6740032保留。全部旧现场、原始输出和二进制继续保留；累计计量现在包括原8个、校准和最终准备共10个二进制、同一cache基线及全部输出。

## 最终候选与准确阻塞

校准二进制 `/private/tmp/xi-read-mode-build-0vf2ekhj/db.test`，SHA256 `ee1bfc263a76f25b06f3a13e4b2b1c04f0851c7a5edf5d9aab0cbd775c6df44a`，361项输入；执行时product/test/runner/plan快照保留。之后只连接70000分支、补最终普通入口及边界验证，不把校准二进制冒充最终代码。

最终二进制 `/private/tmp/xi-read-mode-build-saaq_bb0/db.test`，SHA256 `ab3c14624597b149bea302313c70060306aad2b456152ea3a40425b581d042b9`，361项输入。编译、全部TestWorkbench离线可运行项、69999/70000/70001边界和vet通过；依赖PG的测试明确skip。最终计划为普通入口0/16/69999/70000/300000各一次，最多5新header/440015成员，期望35headers/7180047members；独立20分钟、无补样。最终源对应的该真实验证**尚未运行**。

最终准备[resource-preflight.json](../../output/xi-a0-read-mode-final-ready/resource-preflight.json)记录：累计4056435387bytes，距4480MiB仍有641185093bytes（约611.5MiB）；free23778684928bytes，距23GiB缺917377024bytes（约874.9MiB）。交付再次核验于08:54:11UTC：累计4057033403bytes、余量640587077bytes，free23774425088bytes、启动缺口921636864bytes（约879MiB）。因此没有调用final执行入口，没有创建final-run输出。当前未满足的条件是**启动free线**，不是累计上限、512MiB首大负载余量、产品测试失败或缺少用户功能决定。

向经理建议最小资源调整：**仅本次5次最终验证把启动free线23→22GiB**，其余4480MiB累计、首大负载512MiB/21.5GiB、21GiB停止/20GiB保护、RSS4GiB/SQL10s/20min全部保持；新增副本/清理/配置变更仍不做。当前free约22.15GiB，按现有成员增长约需109MiB成员/索引，另预留WAL软增长256、temp80、日志8和余量96MiB，共约549MiB，落在当前约611.5MiB累计空间内，但WAL及实际CoW消耗不保证，须以每个资源门槛和持续停止检查为准。若保持23GiB，则需要恢复至少上述约875MiB空闲并在启动前重读；本任务不自行选择删除对象或释放旧现场。**该调整只是待审阅建议，代码仍硬性23GiB。**

## 后续资格

先完成最终普通入口及边界真实验证，才能称本次候选闭环；之后准备精确最终代码/二进制、共同空成员模板和匹配资源方案，执行原20×300k全成功且p95≤5s，再按原前置做500k/500001、增长后读取/全量分页/ordinal/money/ref及实际HTTP/lifecycle。当前诊断累计成员数不触发500k。现有3秒HTTP实现预算和D-37-01未决不变。

当前不能从单个4.796s推断INSERT已解决，也没有依据立即启动512MB对照；先完成普通入口和必要持续创建证据。若INSERT仍阻断，再提交128/512shared且wal_buffers4固定的匹配前态资源方案共同决定。原13项/TD01—04及其他未阻塞切片、UR禁联与外部边界均保留。无push/PR/merge/release/deploy或跨任务消息。

最终文件核验见[delivery-verification.json](../../output/xi-a0-read-mode-final-ready/delivery-verification.json)，本次部分交付字节绑定见[evidence-binding.json](../../output/xi-a0-read-mode-final-ready/evidence-binding.json)。
