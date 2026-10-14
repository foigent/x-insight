# XI-A0 checkpoint 对照结果：4GB 未获得创建延迟收益

已完成经理批准的首对诊断。**不采用4GB作为候选默认，不进入反序对或条件性500k探针。** 两臂各6次300k均提交成功，但B的中位数、最大值及含尾部checkpoint的摊销成本均未优于A，不满足计划中的20%方向筛选条件。A0和原13项目标仍未验收；这不是20次p95或HTTP资格通过。

## 运行身份与范围

- 单一执行Owner：`01a08361-94fa-73a3-9a71-f2e2507f08c4`；工作树 `/private/tmp/xinsight-xi-a0-20260909`；产品基线 `033a19678a38dec6aacd9dcbb8466645259ba016`，tree `6e834e254b1b69438b38f44b4d2781be022eb870`。
- 经理批准[计划](../planning/xi-a0-checkpoint-performance-plan-2026-09-09.md)的SHA256为 `41ab9ccfbac6ef1c2da22b97957fabf9bf3f8603e900564c3285a1f103a04804`；执行时一致。代码仅新增诊断测试/runner，并在原seed测试增加保留完整临时模板的分支，产品SQL、schema、索引、权限、HTTP/SQL预算均未修改。
- A/B使用同一Go测试可执行文件，SHA256 `8aa4873c05e8dd699c1d38bbdccaf090e2f00c91e0e98a3c28559e4a96a8bac0`。全部编译输入、runner和计划绑定见[code-binding.json](../../output/xi-a0-checkpoint/code-binding.json)。实测后重新计算，编译输入与二进制均未漂移。运行时包含当前 `pg_catalog.sha256` 路径。
- PG16.14、Go1.25.11、GOMAXPROCS10。每个臂均实际连接 `session_user=current_user=xinsight_workspace`、非超级用户、连接上限10、statement_timeout=10s、可INSERT成员但不可UPDATE F0。`EnsureRoles/ApplyGrants/VerifyRoleBoundaries`均使用现有hostctl实现，通过后才生成模板副本。密码仅用于已批准的合成角色，在内存生成；未使用真实凭据。
- 逐项记录的PG参数中，A/B设置值仅 `max_wal_size` 不同：1024/4096MB。shared_buffers128MB、work_mem4MB、checkpoint_timeout300s、checkpoint_completion_target0.9、fsync/full_page_writes/synchronous_commit=on、wal_level=replica及正常autovacuum保持一致；观测开关两臂均开启。约束定义/validated状态、触发器及身份读回一致，F0 guard为ALWAYS。
- 完整静止模板1599个文件逐字节SHA256核对通过，两臂manifest均绑定 `cc7ec1b048867c96c09a218fd3097ded6ae2a72d3133a72671e4f11539ec5032`。模板有性能来源500000条F0、另20条可见性事实，headers/members均为0。不是重新随机seed的两个数据库。

## 实测结果

原始结果：[A1](../../output/xi-a0-checkpoint/A1.json)、[B1](../../output/xi-a0-checkpoint/B1.json)、[运行记录](../../output/xi-a0-checkpoint/run.json)、[差分分析](../../output/xi-a0-checkpoint/analysis.json)。下表均为毫秒；中位数不是p95。

| 量 | A1：1GB | B1：4GB |
| --- | ---: | ---: |
| 第1次300k | 4442.823 | 4575.479 |
| 第2次300k | 4096.123 | 4718.608 |
| 第3次300k | 4330.726 | 3667.616 |
| 第4次300k | 3680.778 | 3675.570 |
| 第5次300k | 4393.451 | 4212.053 |
| 第6次300k | 4154.683 | 5268.576 |
| 完整创建中位数 | 4242.704 | 4393.766 |
| 完整创建最大值 | 4442.823 | 5268.576 |
| 尾部显式checkpoint | 144.702 | 228.837 |
| 首次开始至尾部完成／6 | 4208.811 | 4392.477 |
| 成功／失败 | 6／0 | 6／0 |

B中位数约升3.6%，最大值约升18.6%，含尾部成本约升4.4%。此单次配对不足以宣称4GB普遍更慢，但足以判定没有达到本计划要求的改善，不追加反序对来找更好结果。反序、500k容量与完整资格均未运行。

每臂6个reading持续保留1.8M成员。12个reading的membership_ref完全相同：`sha256:bc008bc4c4771660899c4659b8b1815d14999fce75acc347b54651391a0bb9a1`；coverage均为300000。单次成员/索引增长在每个sample的storage中；累计成员总大小从131072 bytes增加至A465895424 / B466296832 bytes。F0总大小两臂均保持373776384 bytes。上述累计值不称为单个reading成本。

## checkpoint 假设得到什么支持

创建六次期间，A日志明确有2次 `checkpoint starting: wal`，B为0；两个臂都在最后执行一次显式checkpoint，并记录了其成本。A/B的整个实例日志另含EXPLAIN和停止时checkpoint，不把它们归入六次创建。[A日志](../../output/xi-a0-checkpoint/A1-postgres.log)、[B日志](../../output/xi-a0-checkpoint/B1-postgres.log)。

从开始到尾部结束的累计统计快照差分：WAL约1.504GB→1.032GB，FPI86462→26336；backend写缓冲块281658→286984，backend fsync均0。WAL量/FPI降低与更少checkpoint相符，但创建INSERT中位数3283→3521ms，digest304→313ms，coverage423→473ms，未见延迟收益。不要相加阶段中位数来重构完整中位数。

统计存在发布滞后，以上差分不能声称是严格逐事务计量；完整原始快照和约1秒采样均保留在JSON及observations.jsonl。实际 `wal_sync_method=open_datasync`，wal_sync_time=0不代表关闭同步写；持久化设置读回保持开启。等待采样有WALWrite、DataFileRead、DataFileWrite，也有无等待的运行状态；这个粒度不足以将尾延迟归因于单一等待。

六次之后的单条INSERT EXPLAIN：A5177.928ms、B4635.744ms；其中fact FK2586.202/2090.057ms、reading FK599.838/582.569ms，各调用300000次。它们属于后置、会写入且回滚的诊断，有仪表开销及不同缓存状态；**B的EXPLAIN更快不能覆盖完整创建与尾部成本没有改善的结果**。原始计划见[A EXPLAIN](../../output/xi-a0-checkpoint/A1-explain.json)、[B EXPLAIN](../../output/xi-a0-checkpoint/B1-explain.json)。

## 读取与数据分布

两臂均在第六个300k reading上、尾部checkpoint完成后，以实际workspace身份做等量Store读取探针：

| 读取探针 | A（ms） | B（ms） | 内容检查 |
| --- | ---: | ---: | --- |
| summary | 359.166 | 596.338 | 300000条、精确金额300000 |
| model contribution top10 | 634.907 | 1479.759 | 分母300000；各项、Other及未归属金额守恒 |
| 30天小时series | 345.778 | 679.761 | 720桶，84个非空桶，总和300000 |
| 同一首贡献组的连续两页详情 | 120.696 | 1222.699 | 200行无重复、金额1、Evidence hash存在，仍有后续页 |

这些是每臂一次Store探针，不是读取p95、全量分页验收或10主体HTTP峰值。B探针更慢，当前没有读侧收益依据，也不能从一次探针宣称稳定退化。所有实际结果与耗时均保留在A/B JSON的read_probe中。

窗口仍为30天，性能来源的事件仍聚集于最近约19.4小时及8天前的一段时间；720桶只有84个非空桶与此一致。模板全表min_time包含另一个来源的7月可见性fixture，不能用全表min/max来声称性能来源覆盖整月。没有为了获得改善更改数据分布，也没有增加未要求的均匀30天全量负载。

## 资源、停止、清理与验证

- 整个诊断从2026-09-09 03:59:29 UTC至04:01:51 UTC，约141秒，包含seed、完整副本、角色设置、两臂、尾部、读取及两份EXPLAIN。性能负载未与经理的独立测试重叠。
- 116条资源采样的最大RSS合计963526656 bytes（约919MiB），新资源占用最大4929073680 bytes（约4.59GiB），剩余磁盘最低34960449536 bytes（约32.56GiB）；均未触及计划阈值。这是采样最大值，不冒充连续硬配额或精确瞬时峰值。
- 新建根目录 `/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-checkpoint-run-eeh92u0h`。template、A1、B1都正常停止；最终再次读取，均无postmaster.pid。A1在日志/结果/模板校验记录保留后清理，精确删除路径写入run.json；template与不具备晋级条件的B1现场保留。
- 实际执行 `python3 scripts/xi-a0-checkpoint-diagnostic.py --execute` 成功退出0。`go test ./internal/db -run '^TestWorkbenchCheckpoint' -count=1`编译通过、未设置实验开关时跳过；随后同一测试二进制在实际PG中完成seed、角色、A/B测量及EXPLAIN，均成功。`go vet ./internal/db`与Python语法检查通过。实现过程中首轮编译发现遗漏testing import，修正后才开始PG实验；未产生失败的性能候选重跑。
- 源码/文档差异检查通过。完整暂存差异检查只指出原始initdb.log的末尾空行；保留原始工具输出字节和既有哈希，未为消除格式提示改写日志。
- 本次为测试/runner R1变更，未重跑无关前端、全项目R3或发布流程；原有适用证据仍保留，不将本次自测称为独立审查。CI/Artifact/Target/External Integration/Production均无新增验收。
- [保护校验](../../output/xi-a0-checkpoint/protection-check.json)：原保存项目49份受保护文件及3份继承失败报告哈希均匹配；上一轮114份绑定文件中仅本次有意修改的EXECUTION-RECORD和benchmark测试文件不同，其余112份未变。旧JSON、旧日志、旧绑定、schema24迁移字节与历史发布合同未改。
- 无UR联系、真实源/流量/凭据、外部消息、推送、合并、发布、部署、原实例或用户数据清理。仅本地测试改动和证据交付。

## 独立判断与建议的下一步

1. **停止这条4GB优化路径。** 当前证据支持“减少checkpoint和WAL并未解决创建耗时”，不支持继续把WAL放大或同时增加缓存。保持现有1GB配置；不修改正式默认。
2. **新的关键信息是1GB在共同模板/最终代码/真实角色下，前六次已约3.7–4.4秒。** 这与旧20次p95约9.7秒不矛盾：样本数、成员累计规模、模板正常停库/后台写完成、整库校验带来的OS缓存状态及最终SHA函数代码均不同。不能把差距直接归给任何一个原因，也不能省略原20次门槛。
3. **建议下一步先固定1GB和最终代码，补齐20次实际workspace创建及真实500k容量证据，再决定创建HTTP端点预算和完整HTTP资格顺序。** 重点记录成员增长后的尾部、自然checkpoint与完整事务，不先选另一组调参。现有3秒HTTP与本轮约4秒Store仍不相容；合理预算需要完整300k/500k容量、取消/回滚和HTTP开销支撑，延时本身不能代替原5秒验收。

该下一步提交总负责人审查，未在本切片执行；不构成新增用户批准要求。最终20×300k、HTTP201/500k/500001、10主体读取峰值、完整分页/恢复、TD-01至TD-04及13项目标边界全部保留。
