# XI-A0 局部读取模式候选与代表性验证

经理已接受7dcbf77一次真实创建诊断并明确授权本阶段count局部QueryExecModeExec、digest/coverage事务局部generic候选及定向验证。只在已停库 `/private/tmp/xi-pc-hcpwpsza/data` 的20headers/6M合成前态继续；原 `/private/tmp/xi-cap-7qgkhj7u/qualification/data` 停库只读。INSERT文本、ctid、FK/immutable guard、schema、WAL1GB/shared128MB/work4MB/wal_buffers4MB、持久性、真实workspace/SQL10s均不变；不启用auto_explain。未授权512MB对照、新副本、扩大资源、清理旧现场、500k或资格重跑。

产品count只在调用参数前添加pgx.QueryExecModeExec，保留原SQL与text[]/time/subject顺序。generic helper在当前事务读取进入前的模式，局部设置generic，仅覆盖digest/coverage，成功后恢复；错误/取消由现有整事务rollback恢复。普通生产入口暂不选择generic规模，私有正常创建实现接受仅用于定向比较的selector；先取得实测，再落定是否/如何按insertedMembers分支，不预设阈值。模式选择未落定之前是准备中的候选，不能称为完整产品修复。

## 最少执行规模与真实前后态

独立20分钟窗口，新输出 `output/xi-a0-read-mode-ready` / `output/xi-a0-read-mode-run`，新的固定二进制；不复用上次一次创建许可或计时。计划8次正常创建组成4对，期望成员为0、16、70000、300000，交错顺序A/G、G/A、A/G、G/A。A保留派生查询进入时auto，G在正常写入后局部generic；count两者均Exec。请求复用原fixture，16为最后16秒，70k为7天，300k为30天/project-300k，0为无事实的较早一天；先核对实际eligible，不符即停，不补样。

两次额外0成员正常创建验证进入前force_custom/force_generic时的同连接成功恢复；一次16成员创建在digest完成、generic仍活动时取消context，要求整事务无残留。另在同一workspace事务中用相同helper、pg_sleep(5)和观察到PgSleep后pg_cancel_backend，确认57014、Rollback以及同一PID恢复auto，区分驱动丢弃连接与事务局部恢复。拒绝无grant subject须无header残留；原撤权/权限实现不改，已有对应正确性证据可复用。

总计最多11次Store调用（8对照+2空恢复成功+1预期取消），最多10新header、740032提交成员。全成功期望30headers/6740032members，其余saved_filter/operation/F0/evidence计数不变；取消/拒绝不留记录，不删除已成功reading。任一非预期失败、参数/语义/模式/PID不符或资源触线停止并保留，未完成组不补样。

每次保存真实整体/阶段时间、pgx SQL/实际参数/PID/command_tag、创建前后prepared状态、完整reading/持久化读回、summary金额和ordinal核验。通过真实pgx默认cache512/CacheStatement连接，query tracer仅记录调用，不启用服务端计划仪器；私有正常产品核心相同。4规模ref另与不依赖member表、直接按F0时间/项目/排序计算的独立ref核对；300k还保留已验收历史ref。count至少8次实际调用并覆盖empty source、quoted/Unicode text[]、成本过滤、半开时间边界，named prepared缓存不得出现count语句。旧reading只读快值不替代新事务验证；每对仅一次，不称p95。

## 资源与启动边界

累计4480MiB=4697620480bytes，包含前8二进制、同一cache基线、全部旧输出与新binary/ready/run/WAL/temp/停止成本。启动23GiB；首次70000或更大负载前512MiB预算余量+21.5GiB真实free；持续21GiB停止/20GiB保护、RSS4GiB、SQL10s。本轮不会自动放宽任何一条。

初查free24654082048bytes，距23GiB少41979904bytes；两个实例均shut down。后续独立准备期间free曾回升至24699613184，说明主机余量有漂移，不能用该读数代替最终启动核验。原8二进制全保留；当前pg_wal46文件共771751936bytes=736MiB。按上一单次成员实际增长量，740032成员预计约183MiB，预留192；WAL增长预留416（目录至约1152MiB，软上限仍可能超出）、temp80、日志/新证据8、额外安全余量96MiB。构建/cache另约48MiB，准备后以实际计量代替估算。总增量约840MiB，余量较紧，必须先检查实际组件与首次负载门槛，运行逐步守住累计上限；不能保证仅凭WAL配置就不会触线。

prepare只编译/离线验证/绑定/计量，允许在23GiB以下但仍满足保护线和累计预算时继续独立代码工作；execute在启动前强制23GiB，无例外。若未达启动线，交付精确缺口及已有产物，向经理提交最小必要资源调整，不启动/清理/新建副本。

## 交付和后续

先校准模式代价再选择生产分支，并针对最终选择完成正常产品入口的定向新写验证。取消与模式恢复、参数、权限/过滤/时间、money/ordinal/ref/coverage不能用速度替代。当前candidate不覆盖原20×300k全成功p95≤5秒、500k/500001、增长后读取、HTTP/lifecycle及原13/TD01—04；当前6M不触发那些资格。若无auto_explain数据确认INSERT仍阻断，再提交匹配前态/资源的128/512shared对照方案共同落定；本阶段不自动开展。所有外部/UR禁联、D-37-01边界继续。

## 校准后选择与最终普通入口验证

已在23GiB启动检查实际通过后完成上述8对照+2空恢复+1取消，实例现为30headers/6740032members。0/16的generic派生耗时显著增加；70k派生两段合计auto805.675ms、generic451.286ms，300k分别3660.353/598.826ms。整条请求还受INSERT变化影响，不能把总差异全归于模式。所有完整ref/coverage、money/ordinal、参数、模式恢复及取消验证通过。

据此选择**insertedMembers>=70000**为保守启用起点：这是已测得收益的最小规模，不声称最优临界点。0/16保持原auto，70000/300000启用事务局部generic。生产普通入口连接该策略；仅selector连接/边界函数改变，helper与count及其错误恢复实现不变。单元覆盖69999/70000/70001，真实最终普通入口计划执行0/16/69999/70000/300000各一次，最多5新header、440015成员，期望35headers/7180047members。新独立20分钟、输出final-ready/final-run、独立二进制；计入前9个二进制与全部新旧产物。进入大负载门槛从69999检查；所有原资源/SQL边界不变。

校准停库后真实free约23804317696bytes，已低于23GiB；final prepare继续，final execute仍硬性要求23GiB，因此未自动启动。最终普通入口真实验证尚未完成；报告须区分已完成校准和待运行最终候选。任何启动线调整仅可由经理明确批准，代码仍保留23GiB。

## 最终普通入口：已批准的单次22GiB启动例外

经理已接受4360de03ec4fa1fc81e153333e3d3532ed4dd3cf，并批准仅本次最终5次普通入口0/16/69999/70000/300000的启动线23→22GiB。新增`--prepare-final-approved`/`--final-approved`独立入口及`output/xi-a0-read-mode-final-execution-ready`绑定，旧final-ready及23GiB未执行证据保持原字节。复用ab3c14624597b149bea302313c70060306aad2b456152ea3a40425b581d042b9二进制和361输入，不重编译。新绑定包含原准备文件、当前runner/plan和22GiB正负向离线边界；原普通/校准入口仍23GiB。

累计4480MiB、首次69999负载前512MiB预算余量及21.5GiB free、持续21GiB停止/20GiB保留、RSS4GiB、SQL10s、独立20分钟及全部PG/持久性/约束不变。资源路径显式包含原final-ready与新执行准备目录。原30headers/6740032members停库快照须匹配；运行前后只读保存完整header，确认原30不变及新增正好5个实际ID，并在计时结束后按F0顺序核对各新reading ordinal。任一失败即保留实际状态、不补样。原资格实例停库只读；不新建副本、不清理。

交付口径更正：上轮实际12次repeatable-read Store调用，11次到达header（10提交、1取消），另1次无grant在header前拒绝。历史raw planned_store_calls=11未包含已授权的拒绝检查；原始记录不回写，本轮派生报告明确区分调用、header尝试及提交数量。

5次成功后，只提交原共同空member模板20×300k资格的精确代码/资源方案，待经理共同落定再执行。仍须20次全成功且p95≤5s，500k/500001、增长后读取/HTTP、原13项/TD01—04/D-37及UR禁联继续；本次不开展512MB实验。
