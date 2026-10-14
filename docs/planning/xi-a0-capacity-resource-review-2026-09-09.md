# XI-A0 20×300k／500k：准备完成与资源预算复核

状态：**测试和runner已准备、离线检查通过；容量负载尚未运行。** 总负责人已批准本切片的工作内容，当前只待审定更小资源预算。没有重新请求用户批准，也没有越过现行32GiB启动门槛。

代码基线 `a3ae3b4fb496f6edcc09e80c958ebdef0dab5fc1`。本次仅修改测试与runner；产品SQL、schema、索引、权限和HTTP/SQL时限未改。原4GB否定结论与全部原始证据保留。

## 当前阻塞与建议

2026-09-09 04:30:16 UTC的只读核对：可用33397485568 bytes，约31.10GiB，比当前32GiB启动门槛少约0.90GiB。不是实际试跑失败，没有新PG实例或部分创建状态。

**建议只读复用已核验的停止模板，新增资源预算改为7GiB，启动至少28GiB可用；新增达到7GiB、剩余降至21GiB或RSS达到4GiB即停止发新负载。** 21GiB停止线为原20GiB保留量留1GiB观测/停止缓冲；不降低持久性、原验收或20GiB余量。整个受监控运行仍最多20分钟，只有一个新PG实例和一个创建事务。实际执行前重查，当前31.10GiB能满足建议的28GiB前置条件。

这是依据下列文件与实测占用提出的本地工程预算调整；**现有runner仍执行32GiB/12GiB约束，尚未改为建议值**。经理审定后才落实相应参数并开始负载，不通过清理旧现场凑空间，也不等待空间自然回升反复试跑。

## 可复用模板与并存成本

只读复制源：`/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-checkpoint-run-eeh92u0h/template/data`。

- 1599个文件全部匹配上一轮保留manifest；manifest SHA256 `cc7ec1b048867c96c09a218fd3097ded6ae2a72d3133a72671e4f11539ec5032`，无postmaster.pid，pg_controldata回读状态为`shut down`。详情见[resource-preflight.json](../../output/xi-a0-capacity/resource-preflight.json)。只核对文件和control信息，没有启动该模板。
- 逻辑总量2169079161 bytes（约2.02GiB），其中pg_wal恰为1GiB，其余约1.02GiB；`du`实际分配约2.05GiB。完整复制包含事务状态、索引、角色与WAL，不能只复制表文件。
- 模板仍为性能来源500000条F0、另一来源20条可见性事实、0 header和0 member；准确源ID、固定cutoff、事实ID/ctid、索引、统计与已有合成角色保持上一轮前态。产品代码未变，schema与角色身份将在新副本运行时再次读回。
- 旧template和B1目前分别占约2.05/2.55GiB，已经包含在当前可用空间读数中。两者都保留，不重新计作本次新增，也不清理。仅把模板复制一次到本次新建精确目录；旧模板不启动、不重seed、不修改、不删除。
- 固定cutoff与测试时钟沿用fixture，因此可与原A1的初态比较；它不是实时HTTP或真实过期资格。完整文件校验会影响OS页缓存，继续明确“正常停库副本、首条计入、非OS冷缓存”，不flush主机缓存。

## 新增峰值预算的依据

原单对每臂1.8M成员的增长为A465764352 / B466165760 bytes，折算6.5M仅约1.68GB，不能据此保证线性增长。另一个旧tuple报告在更多reading和回滚诊断分配后，成员总量1894563840 bytes（约1.76GiB）。这些只是容量估算依据，不作为本轮实际结果。

| 新增组成 | 预算 | 依据与余量 |
| --- | ---: | --- |
| 单一运行副本的非WAL基础数据 | 1.10GiB | 已测约1.02GiB逻辑、约1.05GiB实际分配，不产生第二份新seed模板 |
| 6M成员后500k及其索引/失败物理分配 | 3.00GiB | 明显高于上述约1.6–1.8GiB的既有量级；覆盖随机reading ID/索引增长及取消后的分配，不清理来维持结果 |
| 当前运行副本WAL | 2.00GiB | 配置仍1GB；旧A/B六次观测目录均1GB，按2倍留量。max_wal_size是软值，不能视为物理硬上限 |
| 本次构建、缓存增量、JSON/日志与读探针输出 | 0.50GiB | 新二进制18057986 bytes（约17.2MiB），上一轮全部输出约2.7MB；读取一组完整分页会增加输出，仍有较大余量。已有Go cache不删，编译增量计入此项 |
| 文件系统及其他临时余量 | 0.40GiB | 覆盖分配粒度和未精确建模的临时I/O |
| **新增合计** | **7.00GiB** | 一份新运行副本；旧template/B1已占用空间另列且保留 |

按当前31.10GiB可用，即使新增占满7GiB，仍约24.10GiB。建议28GiB启动线覆盖7GiB预算、20GiB保留及1GiB停止缓冲。资源监测仍是采样和停止保护，不冒充文件系统配额；外部空间变化、WAL超预期或RSS达到阈值都会终止负载并保留失败现场。runner已记录编译前后Go cache增量，并将其作为固定增量计入负载阶段预算，不只统计PG目录；既有cache基数不删除、不重算为本次新增。

## 已完成的必要准备

- runner新增`--qualify`入口，使用独立输出`output/xi-a0-capacity`；仅`--qualify`执行编译/离线检查，带`--execute`才可能创建实例。旧A/B输出不覆盖。当前未调用带`--execute`的容量入口。
- 新副本启动前重新核对旧模板停库状态及全文件manifest，复制后再核对。新副本使用1GB WAL、128MB shared_buffers、4MB work_mem及原持久性、角色、约束与10秒单SQL时限，actual workspace Store计时。
- 预先声明20次300k，首条计入、无逐条删除/预热剔除，完整阶段与成功/错误保存。SQL取消且回滚状态正确时继续剩余原定尝试，不补成功样本；其他异常停止。任一失败都令资格不通过，没有6M成功成员时不会用更小前态测500k。
- 20次结束记录自然checkpoint和尾部显式checkpoint成本，确认20 headers/6M members后再测一次500k；六表前后计数、完整事务耗时/SQLSTATE及取消零残留单列。尾部写盘/校验不加入请求p95，样本间间隔仍可由起止时间独立计算。
- 创建后核对全部reading成员数及ordinal连续性、固定membership_ref、300k/500k金额；summary/top/小时series探针与一个贡献组全部分页放在连续创建之后。分页采用一次独立批量引用对照，校验Evidence锚点、事件时间、ordinal顺序、游标边界、无重复/漏行及A0 unknown字段；没有逐行全表查询。
- 原p95算法与≤5000ms不变；20条全部成功是资格前提，成功子集的p95另有明确标签。500k失败不丢弃300k结果，也不延长10秒或使用管理员补成功。API/HTTP预算、500001、峰值、历史和恢复资格仍待后续，不在本次准备中冒称完成。

离线验证实际执行：`go test ./internal/db -run '^TestWorkbenchCheckpoint' -count=1`（编译/默认跳过）、`go vet ./internal/db`、Python语法检查及`python3 scripts/xi-a0-checkpoint-diagnostic.py --qualify`均通过。新二进制SHA256为 `f6829111ca6c0f6e79aa598b9e1ecdedbf9515b5bfdc676b57b661b85764a88b`，编译源及runner绑定见[code-binding.json](../../output/xi-a0-capacity/code-binding.json)。这不替代尚未运行的真实PG容量验证。

本次准备结束后正常交付供经理审查；唯一依赖未决资源条件的步骤是启动容量负载。UR禁联、全部外部禁止项、原13项与TD边界保持。
