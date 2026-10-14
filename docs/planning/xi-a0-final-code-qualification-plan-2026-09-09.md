# XI-A0 最终代码：共同空成员模板资格复测方案

状态：**本轮只提交方案，未复制、未启动、未运行资格负载。须与经理共同落定代码/资源及可用窗口后执行。** 最终普通入口5次验证已完成，见[本轮结果](../evidence/xi-a0-read-mode-final-result-2026-09-09.md)。本次22GiB许可仅覆盖那5次，不能用于资格复测。

## 精确代码和最小实施

产品与测试输入仍为4360de03ec4fa1fc81e153333e3d3532ed4dd3cf中的361个输入，当前没有Go修改。建议直接复用 `/private/tmp/xi-read-mode-build-saaq_bb0/db.test`，SHA256 `ab3c14624597b149bea302313c70060306aad2b456152ea3a40425b581d042b9`；该二进制已包含现有 `TestWorkbenchCheckpointDiagnostic` 的qualify入口。测试文件 `internal/db/workbench_checkpoint_postgres_test.go` SHA256 `f2482af2d64804fef87a17e687b37d0570d28114c71fe076e12214b98934e165`。产品保持count局部Exec、成员>=70000时digest/coverage事务局部generic，其他SQL/ctid/FK/immutable/schema保持不变。

评审后只需新增一个有独立输出的runner入口，复用checkpoint runner既有完整停库复制、role/settings校验、监测/停库逻辑；不直接重跑旧`--qualify`（它会命中已存在的run.json，且准备逻辑会重新编译）。新入口复用上述二进制，绑定361输入、runner、fixture/模板manifest与本方案；启动时显式固定wal_buffers=4MB，补充启动后0 header/0 member及其他四表精确计数、模式auto/preload空和完整schema/role/guard前态校验。结果目录建议 `output/xi-a0-final-code-qualification`，运行根只新建一个 `/private/tmp/xi-final-qual-*` 精确登记目录，拒绝已有结果，保留全部失败记录。当前未实施这个入口。

正式被测命令为该二进制 `-test.run '^TestWorkbenchCheckpointDiagnostic$' -test.v -test.timeout 12m`，环境 `XINSIGHT_A0_CHECKPOINT_MODE=qualify`、`XINSIGHT_A0_EXPECT_WAL_MB=1024`，fixture为原共同模板fixture；DSN只指向新隔离loopback实例及原合成角色，不涉及真实凭据。测试内部context仍10分钟，整个复制/实例/观测/停库窗口最多20分钟，不自动延长。

## 同一初态与保留要求

只读源：`/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-checkpoint-run-eeh92u0h/template/data`。本轮09:22:15 UTC重新核验：shut down、无PID、1599文件均匹配原manifest `cc7ec1b048867c96c09a218fd3097ded6ae2a72d3133a72671e4f11539ec5032`。逻辑2169079161bytes，分配2185322496bytes（约2.035GiB）。[只读预检](../../output/xi-a0-read-mode-final-run/qualification-proposal-preflight.json)记录精确路径和数值。

fixture SHA256 `450b53b4a85d4c3d51e7cddcd45be6a0f82d88033f3f8e2a8da0c00cc5782691`，cutoff `2026-09-09T03:59:30Z`；性能来源500000 F0、另一来源20条事实，原模板0 header/0 member。全表F0/evidence预期500020/500100、saved_filter/operation均0，必须在副本实际读回，不将历史描述冒充新运行数据。300k项目/7天70k的聚集分布与原template相同，不改seed、不ANALYZE、不预热剔除、不flush主机缓存。声明“同一停库模板、新PG进程、首条计入”，不是OS冷缓存。

沿用既有完整 `shutil.copytree` 路径，只复制一次完整停库数据目录（含WAL），源前后及副本启动前校验字节；不为压低预算假设APFS共享块免费。原空模板、原失败资格 `/private/tmp/xi-cap-7qgkhj7u/qualification/data`、当前已增长至35/7180047的 `/private/tmp/xi-pc-hcpwpsza/data`、旧B1与全部证据/二进制均保留、停库，不清理、不复用增长后实例冒充空初态。若采用其他通道/卷或CoW预算，须另给实际计量与复制语义证据再共同决定。

## 建议资源：独立7GiB新增、28GiB启动

继续采用原资格资源复核中的7GiB/28GiB方案，避免为了当前空间压低写入预算。此预算是新资格副本和新产物的增量；旧现场已占据当前磁盘，不删除也不隐藏。它是待共同采用的新阶段预算，不延长或替换已结束的4480MiB最终验证预算。

| 新增组成 | 上限估算 |
| --- | ---: |
| 单副本非WAL基础数据 | 1.10GiB |
| 6M后再500k成员、索引及失败物理分配 | 3.00GiB |
| 副本WAL（1GB配置为软值） | 2.00GiB |
| 输出、分页对照、可能的构建/cache增量 | 0.50GiB |
| 临时I/O、文件系统分配与余量 | 0.40GiB |
| 合计 | 7.00GiB=7516192768bytes |

二进制不重编译；0.50GiB项保守保留，不因当前无需构建而挪作扩大负载。本次440015成员实增113860608bytes约108.6MiB，只是估算参考，不保证6.5M线性增长。新增按路径logical/allocated较大者加cache真实增量计费，不只数逻辑文件；所有旧数据仍通过真实free约束体现。开始前/复制前至少28GiB=30064771072bytes，新增达到7GiB或free降至21GiB或合计RSS达到4GiB即取消当前负载、停止后续、快停并保留。20GiB为保留底线，21GiB为采样/停止缓冲；不是文件系统硬配额。

本轮09:22:15 UTC free23598960640bytes约21.98GiB，距28GiB尚缺6465810432bytes约6.02GiB。**当前不具备此方案启动条件。** 需要经理共同确定有足够空间的执行窗口/位置；不自动清理旧现场，不在后台轮询等空间，不将单次22GiB例外推广。执行前重查漂移和性能窗口，XI一次只运行一个新PG实例及一个Store创建事务。

固定PG16.14、WAL1GB/shared128MB/work4MB/wal_buffers4MB、checkpoint5min/0.9、durability/FK/immutable/schema及正常autovacuum；无auto_explain。实际workspace非super、SQL10s和连接限制保持。管理员仅执行合成实例管理、统计与预先声明的尾部checkpoint/校验，不为被测SQL补成功或放宽时限。本轮不做512MB对照。

## 完整运行口径及剩余验收

现有qualify入口预先声明20次300k，首条计入、全部reading保留，nearest-rank p95<=5000ms且20次全成功才通过该Store门槛。若发生SQL57014，仅在实际确认header/member计数等于此前提交数、无残留后继续原定剩余尝试；失败保留、不补样，整组仍不合格。其他错误/语义/资源问题立即停止。保存所有起止/完整时间/阶段/SQLSTATE、成员增长、自然checkpoint和尾部checkpoint成本；大量内容检查在20次后，不能用间隔稀释负载。

20次全部提交、确有20 headers/6M成员时，现有入口另测一次500k，然后在增长后实例上验证300k的summary/contribution/小时series/完整贡献组分页及500k金额、所有reading成员数/ordinal。注意现有代码在20次全成功但p95超标时仍会执行500k和读探针，再将测试标为失败；这是独立容量证据，不能把p95失败变成通过。500k失败保存零残留，不能由管理员或延时补样；前置不足则跳过并明确缺口。

此入口不包含500001与真实HTTP。它们继续是原目标中必须完成的独立剩余项：500001→Store ErrTooLarge/HTTP422零残留；真实HTTP正常/取消/过期/撤权/生命周期及备份恢复；增长后10主体、600请求、20RPS×30秒、summary/top各300与p95<=2s，300k读取/series/分页正确性；原创建HTTP3秒预算与Store5秒要求的不相容问题须基于最终数据由双方议定，不自动扩大。原13项、TD-01—04、D-37-01及UR禁联、外部凭据/流量/发布边界全部保留。本提案不宣称A0、CI、Artifact、Target、External Integration或Production通过。
