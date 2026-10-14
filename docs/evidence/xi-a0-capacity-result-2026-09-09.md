# XI-A0 1GB WAL：20×300k 失败结果与下一步判断

**本切片按预定失败路径完成取证；300k 创建资格未通过。** 原定 20 次尝试全部保留：19 次成功，第 10 次在成员 INSERT 内触发 workspace 的 10 秒单 SQL 超时并回滚。成功子集 n=19 的 nearest-rank p95 为 **10700.776ms**，不能作为 20 次全成功资格，且仍超过原 5000ms 门槛。

19 次成功事务对应 5.7M 已提交成员，未达到 6M 前置条件，因此 **500k 未执行**。已审定测试在此失败退出，创建后 summary/top/小时 series、完整贡献组分页、全体 ordinal 与金额对照亦 **未执行**；本次不能宣称这些读取检查通过。没有补成功样本、降低成员前态或放宽 SQL/HTTP 时限。

## 代码、授权与证据身份

- 执行基线 commit `025a397fe1ccbb5490a10a8a5d9ef5fe830d4837`，tree `9a8700dee0a5b8ecc0a883f3336e4db1b6b47a1f`；经理审定资源计划 SHA256 `a0bd3fa68a814e3e9c3b3d275286b67cbfe7e3921899a1d6cb75e30994a9d060`。
- 准备快照仍在 `output/xi-a0-capacity`。执行输出分别为首次启动失败的 `output/xi-a0-capacity-run` 与实际负载的 `output/xi-a0-capacity-run-02`，没有覆盖准备或旧 A/B 输出。
- 实际测试二进制 SHA256 `f6829111ca6c0f6e79aa598b9e1ecdedbf9515b5bfdc676b57b661b85764a88b`，与已审定准备版完全相同；358 个 Go/迁移编译输入逐个匹配。工作树本轮仅改 runner 的已批准资源阈值、短 socket 路径与首次失败资源计入，另追加记录和结果文件。产品 SQL、索引、schema、权限、HTTP 预算及 Go 测试未改。
- R1 本地 runner/诊断交付。XI 仍为同一 sole writer；没有跨任务发信、UR 联系、push/PR/merge、发布部署、真实数据/凭据或外部写入。本结果不代表 CI、Target、External Integration、Production 或原 13 项/A0 整体验收。

## 原定 20 次创建结果

原始精度为纳秒；下表换算毫秒并四舍五入。序号从 1 起，原始 JSON 的 `index` 从 0 起。首条计入，没有预热剔除；SQL 取消后只完成剩余原定尝试。

| 次数 | 完整 Store 事务耗时 ms | 结果 |
| ---: | ---: | --- |
| 1 | 4456.585 | 成功 |
| 2 | 3840.631 | 成功 |
| 3 | 4441.329 | 成功 |
| 4 | 3656.928 | 成功 |
| 5 | 4427.894 | 成功 |
| 6 | 4104.012 | 成功 |
| 7 | 7676.671 | 成功 |
| 8 | 7356.438 | 成功 |
| 9 | 7202.888 | 成功 |
| 10 | 10291.166 | 失败，SQLSTATE 57014 |
| 11 | 9568.676 | 成功 |
| 12 | 8361.846 | 成功 |
| 13 | 6842.021 | 成功 |
| 14 | 8628.460 | 成功 |
| 15 | 7413.873 | 成功 |
| 16 | 7676.241 | 成功 |
| 17 | 10700.776 | 成功 |
| 18 | 7822.561 | 成功 |
| 19 | 10111.827 | 成功 |
| 20 | 7585.230 | 成功 |

第 10 次 PostgreSQL 日志明确记录 `INSERT INTO workbench_reading_members` 超时，取消位置的 CONTEXT 为父 reading 的外键 `FOR KEY SHARE` 检查。**这证明取消发生于 INSERT/FK 路径，不单独证明存在锁争用或 FK 是全部瓶颈。** 阶段观察器最后完成的是 eligible_count；错误后读回 headers=9、members=2700000、orphans=0，与前九次成功状态一致。

每次成功都经过真实 Store 的 RowsAffected=eligible 检查与 Commit，且 KnownItems=300000。19 次成功的 membership_ref 相同：`sha256:bc008bc4c4771660899c4659b8b1815d14999fce75acc347b54651391a0bb9a1`，与旧 A1 固定集合一致。**5.7M 是由这些成功事务推得，测试未执行结束后的全表成员计数。** 不能把它表述为最终独立全表读回或完整 ordinal 验证。

成功子集 p50=7413.873ms、p95=10700.776ms；包含失败的全部 20 次耗时诊断 p95=10291.166ms，后者不是成功请求指标。原始 `durationReport` 以整数毫秒截断，显示 7413/10700ms；本报告保留纳秒换算精度。17、19 次完整事务超过 10 秒仍成功，因为 workspace 限制针对单条 SQL，并非整个 Store 事务。

连续尝试及最后 checkpoint 的周期为 142499.924ms；尾部显式 checkpoint 118.453ms，单列而不加入请求 p95。测试用例约 142.52s，以 exit 1 如实结束；runner 的 `measurement_complete=false`、`diagnostic_complete=false` 保留失败含义，没有改为成功。

## 实际前态、角色与配置

运行副本源自上一轮已停库模板；复制前后 1599 个文件匹配原 manifest，SHA256 `cc7ec1b048867c96c09a218fd3097ded6ae2a72d3133a72671e4f11539ec5032`。交付时再次核对源模板仍逐字节相同。没有启动、重 seed 或修改旧模板。

模板为性能来源 500000 条 F0，另一来源 20 条可见性事实，0 reading/0 member。固定 cutoff 为 `2026-09-09T03:59:30Z`。性能来源前 70000 条分布在截止前约 19.4 小时，其余 430000 条分布在截止前 8 天再向前约 5 天；项目过滤选出 300000 条。它不是均匀七日/月度分布，也不证明完整历史月份容量。文件校验会影响 OS 缓存，本轮是正常停库副本首条计入，不能称为 OS 冷缓存试验或实时 HTTP 过期资格。

现场身份 `session_user=current_user=xinsight_workspace`，非超级用户，connection_limit=10，statement_timeout=10s，search_path=public，事务只读 off；facts_update=false、members_insert=true。管理员只用于观察/尾部 checkpoint，创建走实际 workspace Store。

PG16.14；max_wal_size=1GB，shared_buffers=128MB，work_mem=4MB，wal_buffers=4MB（512×8kB），checkpoint_timeout=5min、completion_target=0.9；fsync/full_page_writes/synchronous_commit 均 on，wal_level=replica，autovacuum on，I/O 计时 on。FK/unique 约束和触发器状态保存在原始 JSON。没有采用上轮 4GB WAL 候选，也没有放宽持久性或 SQL 限制。现有 3 秒 HTTP 预算没有变化，本切片未执行 HTTP。

## 累计增长后的诊断

前六次均低于 5 秒，随后 13 次成功均高于 5 秒，另有一次取消。分组是观察描述，不是新的剔样或资格算法。

| 中位耗时 ms | 前 6 次成功 | 后续 13 次成功 |
| --- | ---: | ---: |
| 完整创建 | 4265.953 | 7676.671 |
| eligible_count | 47.502 | 289.151 |
| member_insert | 3288.695 | 4079.765 |
| membership_digest | 310.489 | 979.730 |
| coverage | 404.040 | 2337.221 |
| commit | 0.190 | 0.158 |

这些阶段中位数来自各自分布，不能相加当作整条请求耗时。结论是：**持续创建变慢涉及读取/join 与 INSERT，不能只优化或归咎于 checkpoint。** 当前 coverage 查询包含成员→F0→evidence_events 的聚合，以及 capture_gaps 的成员关联；digest 也重新 join F0。尚无本轮晚期实际执行计划，无法确定计划变化、共享缓存周转或 WAL/FK 成本各自的因果份额。

原始统计从创建前到尾部 checkpoint 后的差值为：

- 负载期间日志有 8 次 WAL 触发 checkpoint；加尾部手动 checkpoint，checkpoints_req 增 9、timed 增 0。checkpoint_write_time 增 115823ms 是节奏化写盘阶段的累计时间，不能算成请求阻塞时间。
- WAL 增 4809864710 bytes、FPI 增 272295、wal_buffers_full 增 442357；wal_write_time 增 27869.874ms。wal_sync_time=0 与现场 `wal_sync_method=open_datasync` 并存，不能解释成关闭持久化。
- client backend / normal relation 的 reads 增 13347475、read_time 增 17006.084ms、evictions 增 13516800；这些不是物理磁盘读取次数或单条 SQL 的归因。采样还观察到 WALWrite、DataFileRead 等等待；空等待样本不能证明没有 I/O。
- F0 total 从 373776384 至 373784576 bytes（+8KiB，大小变化本身不证明语义修改）；成员 total 从 131072 至 1521565696 bytes，其中 table=530800640、indexes=990765056。该容量包括累计成功及失败后的物理分配，不是单个 reading 的大小。

PG 统计有发布延迟，并包含采样连接与后台活动。checkpoint 日志、每次阶段计时和前后统计相互补充，不能把统计差值精确分摊至某个请求或阶段。之前 4GB 对 1GB 的六次 A/B 未显示提速仍是有效否定证据，但那不是本轮持续 20 次的配对实验。

## 首次启动失败、资源与保留现场

首次执行在 PG 可接受连接和任何样本产生前失败：默认 TMPDIR 拼接出的 Unix socket 路径为 104 bytes，超过日志明确给出的 103-byte 上限。输出完整保留在 `output/xi-a0-capacity-run`，没有 qualification.json/log。原失败数据副本的 1599 文件在交付时仍与模板完全相同。

只修改 runner 为 `/private/tmp/xi-cap-*` 短根路径并增加路径长度预检，另用 `run-02` 输出。第二次启动才产生本报告的原定 20 次样本；它不是重跑已经失败的性能样本。保留首次失败副本、其二进制和输出，并计入同一个 7GiB 新增上限；20 分钟时钟从首次尝试起算，未重置预算。

| 资源证据 | 实际 |
| --- | ---: |
| 首次尝试开始时可用 | 33124966400 bytes，约 30.85GiB |
| 短路径执行复制前可用 | 30864293888 bytes，约 28.75GiB |
| 复制后可用 | 28678602752 bytes，约 26.71GiB |
| PG 启动前可用 | 28676857856 bytes，约 26.71GiB |
| 135 次负载资源采样最大 RSS | 1075380224 bytes，约 1.002GiB |
| 新增文件逻辑字节峰值（含首次失败副本） | 5934429568 bytes，约 5.527GiB |
| 采样最低可用空间 | 27097776128 bytes，约 25.237GiB |
| 从首次尝试开始至本次停库 | 365.476s，约 6 分 5 秒 |

28GiB 是开始分配新副本前的门槛；复制后和 PG 启动前检查累计新增 <7GiB、剩余 >21GiB。两次边界数值均原样保留，**没有声称复制后仍有 28GiB**。新增统计采用文件逻辑长度并加入构建、输出、cache 增量和首次失败残留；实际可用空间另测。保护是采样/停止机制，不是文件系统硬配额。全程未触发 7GiB、21GiB、4GiB RSS 或 20 分钟停止线。

交付核验四个目录都无 postmaster.pid，pg_controldata 均为 `shut down`：

- 原模板：`/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-checkpoint-run-eeh92u0h/template/data`
- 原 B1：`/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-checkpoint-run-eeh92u0h/B1/data`
- 首次启动失败副本：`/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-checkpoint-run-bjep8yon/qualification/data`
- 本次负载失败副本：`/private/tmp/xi-cap-7qgkhj7u/qualification/data`，原端口 52429，2026-09-09 04:47:28 UTC 正常停库。

没有清理旧现场、失败副本、原用户工作树或 `frontend/node_modules.predecessor-link`。

## 下一切片建议，尚未执行

**先取得晚期 coverage、digest 与 eligible_count 的实际计划和 I/O 证据，再选择一个候选。** 当前 5.7M 保留状态比重新 seed 更接近问题；先准备精确查询参数、同一 workspace 身份、单次只读事务的 EXPLAIN (ANALYZE, BUFFERS, SETTINGS, FORMAT JSON)，保留原 SQL 10 秒上限，逐个记录取消和原始计划，不修改产品查询、索引或统计前态。数据库启动/副本与资源安排单独交经理审定，原失败现场继续保留；此方案不替代已跳过的容量和读取资格。

若计划显示主要为固定集合反复读取/缓存周转，再考虑 **shared_buffers 128→512MB 的单变量诊断**；max_wal_size=1GB、work_mem=4MB，且显式固定 wal_buffers=4MB，避免自动值随 shared_buffers 一起变化。若计划显示基数估计或 join 路径问题，应针对确切查询提出候选，不能用扩大缓存掩盖它。目前提高 shared_buffers 只是待验证假设，未实现、未采用。

不建议现在放宽 HTTP 超时：20 次创建仍失败，500k 尚无本轮证据，不能据前六次推导可用 HTTP 预算。保留现场后的可用空间约 25GiB，低于本次 28GiB 新副本门槛；未来实验须重新核对并审定实际资源方案，不能直接重用 runner 或靠删除旧现场继续。

## 验证与复核入口

本轮实际编译成功；默认跳过的 focused tests 通过，仅证明可编译/入口行为。真实 PG 工作负载如上失败；Python 语法检查通过。Go 源未变，复用准备阶段 vet 和已有不受影响的产品回归，不重跑与此次 runner 改动无关的全套检查。

交付哈希核对通过：原保存仓库 49 个保护文件、3 个继承失败报告；旧 checkpoint 的 30 个原始输出；首次失败 7 个、实际负载 10 个原始输出。旧 A/B 的 40 项绑定在 `a3ae3b4`、准备版 9 项绑定在 `025a397` 均逐项匹配；当前工作树中的已授权 runner/记录及准备期测试变更明确另列，未把旧源码哈希当作当前哈希。

- [原始 20 次结果与角色/设置/统计](../../output/xi-a0-capacity-run-02/qualification.json)
- [原始 PG 日志](../../output/xi-a0-capacity-run-02/qualification-postgres.log) 与 [逐次资源/等待采样](../../output/xi-a0-capacity-run-02/qualification.observations.jsonl)
- [runner 原始退出状态](../../output/xi-a0-capacity-run-02/run.json) 与 [首次启动失败](../../output/xi-a0-capacity-run/run.json)
- [派生计算](../../output/xi-a0-capacity-run-02/analysis.json)、[源/历史保护/停库核验](../../output/xi-a0-capacity-run-02/delivery-verification.json)
- [最终证据文件绑定](../../output/xi-a0-capacity-run-02/evidence-binding.json)，另保留两次执行的原始 output-hashes.json，不回写原始结果。

本地提交后正常交付供经理独立审查。已完成本次连续尝试、失败保存与诊断交付；未完成的 500k、创建后读取验证及完整 HTTP/A0 资格保持明确未通过/未执行状态。
