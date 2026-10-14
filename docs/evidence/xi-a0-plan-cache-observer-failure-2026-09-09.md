# XI-A0：CoW 验证通过，计划观察器失败

**诊断未完成，计划缓存假设仍未验证。** 强制逐文件 CoW 副本及全部前置检查通过；第一条 eligible_count 查询成功返回300000，耗时51.152833ms。随后诊断代码将 prepared 元数据中的字符串 OID 解码为 int，测试退出。没有取得执行后的有效计划计数，也没有执行 digest、coverage、EXPLAIN 或 G/C 对照。

这是本次新增观察器的实现错误，不是产品 SQL 失败、SQL 超时、资源停止或计划缓存根因证据。已修复并完成离线验证；**修复版尚未运行 PG 诊断**。

## 已执行与保留的事实

- 基线为计划提交 `9fd8c3e0eceda1b80f3b131b42d8b58386a335f6`。经理批准设计及条件式24GiB强制CoW资源修订；独立本机8MiB probe验证成功、inode不同、权限/初始摘要相同、双向修改隔离；重复目的路径返回EEXIST且未覆盖。
- 首次准备因本机Python没有os.listxattr在复制前停止。其probe和失败记录保留在 `output/xi-a0-plan-cache-ready`；改用本机SDK确认的Darwin listxattr/getxattr接口后，在 `ready-02` 完成正式准备。不是绕过扩展属性/权限核验。
- 从唯一指定停库源 `/private/tmp/xi-cap-7qgkhj7u/qualification/data` 克隆至 `/private/tmp/xi-pc-hcpwpsza/data`。标准目录遍历加每个普通文件clonefile(flags=0)，无普通复制/硬链接回退。1604文件摘要、全部根/子目录及文件类型、mode/uid/gid/flags/xattr一致；无链接、外部tablespace或ACL，普通文件均与源不同inode。
- 副本在2026-09-09 05:47 UTC启动，端口55076；真实workspace会话PID36483，pgx cache512/CacheStatement，plan_cache_mode=auto，10秒statement_timeout，目标事务READ ONLY。PG配置检查保持1GB WAL、128MB shared_buffers、4MB work_mem、持久性和并行/autovacuum默认。
- 本轮独立读回19 headers、5700000 members；七个目标reading各300000且membership_ref与原记录一致。这是本轮新证据，不回填成此前容量试验已经做过的最终计数。
- 唯一目标执行为第1轮count。执行前prepared列表为空；原始结果是300000。执行后inspect的JSON解码失败：`cannot unmarshal string into Go struct field planCachePrepared.statements.oids of type int`。测试运行约1.17秒、exit1；runner正常停库并重新核验原源全部元数据/摘要相同。

原始 [diagnostic.json](../../output/xi-a0-plan-cache-run/diagnostic.json)、[错误日志](../../output/xi-a0-plan-cache-run/diagnostic.log)、[PG日志](../../output/xi-a0-plan-cache-run/postgres.log) 与 [runner退出记录](../../output/xi-a0-plan-cache-run/run.json) 均原样保存。执行时Go源码另存 [failed-runtime-source.go.txt](../../output/xi-a0-plan-cache-run/failed-runtime-source.go.txt)，与实际二进制的源码绑定一致；执行时计划保存为 [execution-plan.md](../../output/xi-a0-plan-cache-run/execution-plan.md)。未将修复后代码冒充本轮实际运行代码。

## 资源与安全状态

复制后预算计量3727946427 bytes，真实可用26219356160 bytes；当时满足剩余512MiB预算和21.5GiB真实空闲的发样条件。该计量保守计算CoW文件逻辑/分配长度，真实余量另测；不能由前后df波动推导零分配。

运行期合并后台采样、边界和两个同步检查：最大新增3728523963 bytes，最大合计RSS273776640 bytes，最低真实可用26135330816 bytes。从启动执行流程至最终核验10.693573秒。没有触及4GiB新增、21GiB余量、4GiB RSS或20分钟边界。只有一个后台周期采样，不能据这些峰值推断尚未运行的35条查询资源占用。

交付核验原源和新副本均无postmaster.pid，pg_controldata均为`shut down`。原源before/after完整inventory相同；旧模板/B1、原失败副本均未清理。没有产品SQL/索引/default/HTTP变更，没有另一轮创建性能测量、UR联系或其他外部操作。

## 修复与验证

只改新增测试的OID接收类型为 `[]json.Number`，同时接受PG实际字符串编码与数字JSON；仍逐项严格比对text=25、text[]=1009、timestamptz=1184，没有取消类型检查。新增两种编码的定向回归均通过，当前源码go vet通过。

修复版二进制已构建、在激活标志缺失时验证诊断默认跳过，OID回归通过：

`/private/tmp/xi-pc-fix-build-jdy_iwzj/db.test`

SHA256 `aaa6ecbdbc7b27256c0eb8b7ecff59936d9ac96b48211b49d0dda73cb8221a5a`。源码/二进制绑定及真实离线日志在 [fix输出](../../output/xi-a0-plan-cache-fix/code-binding.json)。原失败二进制 `/private/tmp/xi-pc-build-86uyh9ps/db.test` 保留。

完成二进制验证后，仅移除一个本任务在失败后新生成、由go list -test -export精确确认的可重建测试编译cache文件（4242388 bytes）；原缓存、失败二进制和数据目录未删。精确路径、创建时间、摘要和保留二进制证明见 [task-cache-cleanup.json](../../output/xi-a0-plan-cache-fix/task-cache-cleanup.json)，没有批量缓存清理。

## 下一步所需的经理决定

不自动重置七轮：第一次已有一条真实目标执行，旧连接已经结束。将完整七轮与G/C重跑会使本切片累计成为37条，超过已审定36条；只补剩余35条则count的新连接只有六次auto，不能声称完整七次同会话。因此本轮先交失败证据和修复版，不构造特殊恢复协议来拼接计数。

**建议将修复后的运行明确列为一次新的、最多36条/20分钟诊断尝试，复用目前已停库的新副本 `/private/tmp/xi-pc-hcpwpsza/data`，不再创建第二副本。** 原一次执行及原二进制完整保留，新会话/PID、输出、计数单列；原36条诊断设计保持不变，仍只读，10秒SQL限制不变。

05:58:16 UTC补充核算已含两个二进制、全部准备/失败输出和cache增长：累计计量3760322541 bytes，4GiB预算还剩534644755 bytes（约509.877MiB），比512MiB发样门槛少2226157 bytes；真实余量26040172544 bytes仍超过24GiB。详情见 [修复后资源快照](../../output/xi-a0-plan-cache-fix/resource-preflight.json)。没有因缺口小而越过门槛或继续清理现场。

建议经理为新诊断仅将累计新增预算增加32MiB至 **4128MiB（4328521728 bytes）**，保留512MiB发样余量、21GiB真实停止线、20GiB保护量、RSS4GiB和原SQL限制。多出的32MiB覆盖修复版保留成本和新输出，执行前仍重查；此项及新的36条尝试范围尚未获准，当前安全状态为全部停库。本地工程诊断即可处理，不需要用户重新决定产品方向。

## 复核入口

[交付核验](../../output/xi-a0-plan-cache-run/delivery-verification.json) 证明实际运行359个编译输入中，当前仅新增测试因上述修复不同；失败源码快照、原运行二进制和runner绑定一致。准备/执行原始hash清单、此前容量与checkpoint原始输出均核对通过。[派生摘要](../../output/xi-a0-plan-cache-run/analysis.json) 保留资源和单次结果计算。

最终文件绑定见 [evidence-binding.json](../../output/xi-a0-plan-cache-run/evidence-binding.json)。本轮只交付实现、CoW实证、观察器失败与已离线验证的修复；计划缓存根因、500k、后续读取及HTTP/A0/原13项验收仍未完成。没有伪造第六次切换或任何G/C计划结果。
