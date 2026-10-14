# XI-A0：一次真实 Store 创建的 auto_explain 诊断

经理已接受 d6ae76f 的 coverage-only9，并明确授权在同一隔离合成实例 `/private/tmp/xi-pc-hcpwpsza/data` 的19headers/5.7M前态执行唯一一次正常300k新ID创建。复用产品 createWorkbenchReading 及已有阶段观察器，不复制创建SQL，不补样。原 `/private/tmp/xi-cap-7qgkhj7u/qualification/data` 停库只读；不新增副本、不删除旧现场、不修改产品或全局默认、不执行外部操作。成功保留20headers/6M新前态；失败核对原19/5.7M和相关表/headers零残留。无论结果都不触发500k或进入原20次资格。

本轮独立输出 `output/xi-a0-auto-create-ready` / `output/xi-a0-auto-create-run`，新二进制，累计计量沿用coverage的全部21路径、同一cache基线，增加新build/ready/run；源库非新增磁盘不重复计量。初始4480MiB=4697620480bytes，23GiB启动；首次大创建需512MiB累计余量和21.5GiB真实free；21GiB停止、20GiB保护、RSS4GiB、独立20min，SQL仍10s。准备阶段重新核算全部磁盘组件，任何条件不满足不执行大创建。估计额外构建48、member/index96、WAL256、temp160、日志8、余量128MiB；WAL软限制及所有停止/输出开销仍受累计限制，超出不扩容或清理。

仅本次PG启动参数：session_preload_libraries=auto_explain、log_min_duration=0、log_parameter_max_length=-1、sample_rate=1、analyze/buffers/wal/settings/triggers=on、JSON、timing=off、nested=off。原WAL1GB/shared128MB/work4MB、持久性/约束/trigger、实际workspace权限不变。库绑定 `/opt/homebrew/Cellar/postgresql@16/16.14/lib/postgresql/auto_explain.dylib`，53376bytes，SHA e880e306b60a240cfdee2ff6a653f92ff59c63a0e67fd9c7e0fcc70687b78cf6。精确启动参数/库/代码/二进制/输入绑定在ready。

同一workspace缓存512/CacheStatement/auto，先轻量text+array+timestamptz参数SELECT，验证返回和实际日志PID、SQL、完整参数及实际rows/buffer/WAL；缺失则停在0次创建。查询结束读取原始日志，PG16 JSON对象解析按照官方实现，离线负向覆盖PID/SQL/参数/实际行和截断。prepared与统计检查在产品调用外进行，释放唯一连接再调用产品，前后核对PID。请求使用原fixture和原产品请求构造器；新ID由产品生成，实际参数来自自动日志。完整日志保留；失败查询可能无完整计划，不能回填。阶段计时含仪器开销，关闭timing后只报告实际具备的数据。

正常或失败均fast停库并保留日志/原始输出，核对原源1604文件不变；资源触线取消隔离workspace负载后停库，不重跑。新输出存在即拒绝覆盖或自动执行；容量/前置失败只暂停依赖负载。交付真实创建结果、SQL计划、参数/缓存/统计变化、阶段/提交与资源证据，并独立提出下一产品候选供共同决策；本轮不实施候选。原13项/TD/A0/HTTP、20×300k全成功p95≤5秒及后续资格边界不变。

官方核对：[PG16 auto_explain](https://www.postgresql.org/docs/16/auto-explain.html)，[PG16实现](https://github.com/postgres/postgres/blob/REL_16_STABLE/contrib/auto_explain/auto_explain.c)。模块自动记录完整参数，禁用逐节点timing降低开销但不能消除；trigger调用次数仍可见，不能声称取得未开启的节点/trigger耗时。

## 前置观测修正（保持原窗口和唯一创建上限）

首次执行08:00:33UTC开始，workspace PID65343，参数前置前即因受限pg_settings未返回session_preload_libraries而停止；创建0次，原源未变，实例正常停库。workspace日志已记录实际auto_explain计划和全部模块设置，不能将受限视图缺行解释为模块未加载。保留原binary、输出和执行时Go/runner/计划快照。

仅修正启动选项的读取位置：现有诊断admin读取SHOW session_preload_libraries，workspace仍必须通过完整text/array/time参数、SQL/PID、实际rows/buffer/WAL日志前置，不给workspace增权或跳过模块验证。新准备/输出-02、新binary保留并累计计量；如恢复执行，仍使用首次08:00:33的同一20分钟窗口，不新增创建尝试、数据副本或预算。恢复前核对原失败0创建、停库及源；完整前置再次失败则停在0创建并保留。
