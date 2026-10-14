# Connectors digest：实施与完整子项交付

状态：**review18 的 SET ROLE 漏检及同类角色转授路径已补修，完整 Local 补验通过，交经理独立验收。** `589b25a`、`4eda968` 均不能视为 Connectors 已接受。责任子项已由 independent-review-16 接受；本项不代表完整 XI-C、GX-01、TD-02/TD-04 或原 13 项通过。

## 2026-09-10 review18：完整权限路径补交

认可经理独立反例：额外 helper 持有 text digest grant option，reader 的成员关系 INHERIT FALSE 但 SET TRUE 时，当前身份无 grant option，实际登录仍可 SET ROLE helper 后转授。它依赖额外成员关系漂移，不是默认安装自行提权。修正继续放在源/目标共用 `verifyConnectorDigestPrivileges`；ApplyGrants、迁移和成员关系均不写改。

共用校验先枚举登录能够 SET 到的身份（包括自身），按每个身份的有效 EXECUTE / GRANT OPTION 检查原 public 函数白名单；白名单仍按最初 runtime 账号区别 reader/workspace/importer。另落实原运行账号不得管理角色的边界：拒绝可 SET 到的 SUPERUSER/CREATEROLE，以及这些身份能自动继承的 ADMIN grantor；ADMIN 可以转授仅有普通 EXECUTE 的角色，不能只查对象 grant option。角色 ADMIN 对 SUPERUSER 目标本身不足以授权转授，按 PG16 的实际规则处理。pre23 原 PUBLIC 函数兼容分支保留；角色管理检查也适用于旧源，不将旧函数 PUBLIC 合同解释为允许角色管理。

PG16.14 的 [角色授权实现](https://github.com/postgres/postgres/blob/REL_16_14/src/backend/commands/user.c#L2040) 和 [权限实现](https://github.com/postgres/postgres/blob/REL_16_14/src/backend/utils/adt/acl.c#L4690) 区分 mere membership、SET 可达身份与实际 grantor。`pg_has_role(..., 'MEMBER WITH ADMIN OPTION')` 会经过不可继承、不可切换的成员关系，单独使用会误拒绝；本次采用 SET + USAGE 与直接 ADMIN 边组合，覆盖默认和显式 GRANTED BY，不自建递归图。

- 原生 PG16.14 / loopback49998 已完成12场景：继承、直接/传递 SET、SET 后继承、ADMIN 普通执行转授、SET 后 ADMIN、继承 ADMIN（隐式及显式 grantor）、不可达关系和正常普通执行。真实 runtime 登录操作与有效权限读回一致，3个新角色/1个新 schema 已清理。`role-path-native-20260910-04.log`、`role-path-native-result-20260910-04.json`。这是合成 SQL 函数的原生权限语义证据，尚不是编译后产品验证器通过。
- 新增 `TestConnectorDigestUsableRolePaths` 的15个场景调用真实源/目标验证器，全部通过；覆盖上述可达路径、INHERIT 不等于允许 SET、bytea 与 workspace/importer text 越权，以及 ApplyGrants 不改变成员关系。既有继承用例的安全基线明确 SET FALSE，避免把原来漏检路径误当正常对照。真实 runtime 仍能在特意构造的越权配置中执行/转授，源/目标验证器现在明确拒绝该配置；修复的是校验漏检，没有静默修剪成员关系。`role-path-validator-20260910.log`。
- 保留原生探针初次 SET 目标选择错误、第二次 ADMIN 查询误判，以及第三次 sandbox 阻止 loopback 连接的日志/源码。第三次失败在 CREATE SCHEMA 前发生，旧日志的固定 cleanup 文案不代表实际创建/删除；第四次才完成全部12项。未掩盖诊断返工，未改产品安全门槛。

补验前原基线累计新增约1,047.8MB，距1GiB约25MiB。`go test -n` 确认 hostctl 测试归档和测试入口必须重新编译；上一轮归档缓存增长10,567,680 bytes，经理运行瞬时阶段增量约25.1MB，原余量不足以同时容纳新缓存和相近验证负载。经理已在 `independent-review-18/resource-amendment.md` 一次批准增加64MiB至1,088MiB（1,140,850,688 bytes），由 owner 完整补验与经理独立复核合用。原 baseline 文件及计量起点未改，原 guard 明示有效上限，继续计入主管保留目录并保持20GiB空闲；没有自动清理或另设额度。批准前只做低负载准备，批准后才执行编译/数据库测试。实测采样及审批文件摘要见本轮 evidence binding，不把采样最大值当连续峰值证明。

同批补验全部完成：原 direct/inherited/identity、正常 reader 与合法缺权源；schema23→26 的 CheckUpgrade 只读、两个持久中断点和 Resume；schema9 legacy adopt；rc.10 历史合同、原生新库恢复及管理摘要/只读 preview。`role-path-lifecycle-regression-20260910.log`。这组使用真实 PG、迁移、备份/恢复和原角色，签名包/host/runtime 证明仍为合成，不能升级为正式 Artifact 或 Target 证据。`go vet ./internal/hostctl` 通过；构建、语法与差异检查通过。15项新场景已经通过后没有在后续生命周期组重复运行。

完整验证命令保持 Go1.25.11、GOPROXY=off、既有 GOCACHE/TMPDIR/GOTMPDIR 与原资源 guard：`go test -ldflags=-w ./internal/hostctl -run '^TestConnectorDigestUsableRolePaths$' -count=1 -timeout 3m -v`；随后 `go test -ldflags=-w ./internal/hostctl -run '^TestConnectorDigest(SourceTargetAndNegativePrivileges|InheritedGrantOption|LegacyAdopt|EngineCheckAndResume|NativeRestoreCheckpointPreviewAndRC10)$' -count=1 -timeout 3m -v`；最后 `go vet ./internal/hostctl`。仅省略测试二进制 DWARF，不改变产品、断言或正式制品。

本轮新角色/schema/database 数量读回均为0，reader NOINHERIT、runtime 非管理员/无成员关系恢复，PG49998正常停止。未变化的13项来源、37份 HTTP/页面等既有证据已按摘要核对后复用，没有重跑浏览器/前端；A0的361源码、9准备文件和固定二进制一致，模板与三旧目录均保留，原工作树干净。`role-path-cleanup-readback-20260910.json`、`role-path-pg-stop-20260910.log`、`role-path-freeze-reuse-20260910.json`。仅完整包或实质阻塞通知经理；不按实验增加材料层或批准轮次。最终来源与证据绑定为 `output/xi-c-responsibility/role-path-evidence-binding-20260910.json`，commit/tree 随完整通知给出。

## 2026-09-10 review17 补交：有效可转授权

完整读取经理决定、独立探针和前提/清理说明，认可 P2。XI 另写合成回归，先在原 `589b25a` 验证器上重现：明确 INHERIT TRUE 的 helper 提供 digest text grant option，实际 reader 能把 EXECUTE 授给另一新 NOLOGIN 角色，而源/目标检查均返回 nil。`output/xi-c-responsibility/inherited-before-20260910.log` 保留失败；普通 NOINHERIT 不会自动获得此继承权利，不把额外成员关系前提误说成正常安装行为。

最小修正位于 `internal/hostctl/digest_privileges.go`：可转授权分支改用 `has_function_privilege(role_oid, function_oid, 'EXECUTE WITH GRANT OPTION')`，与普通执行权一样检查有效权限，覆盖继承，不再只匹配直接 ACL grantee。原函数白名单、PUBLIC、身份、源/目标和 pre23/rc.10 分支保持；ApplyGrants 及所有角色成员关系写操作没有修改。

补验结果：

- 同一真实 reader 继承/转授权前提下，源与目标现在返回精确权限边界错误；直接 grant option、其他原有越权/身份反例继续被拒绝。正常 reader 普通 EXECUTE 可用；缺普通权利的合法旧源可进入修复，缺权目标仍被拒绝。再次 ApplyGrants 后异常成员关系仍在且检查仍拒绝，证明产品没有静默修改用户成员关系。`inherited-after-20260910.log`。
- 重新执行正式 Engine 的 schema23→26 CheckUpgrade，以及 migrate_database / apply_role_grants 两个中断和恢复入口，均通过；前态只读、函数异常拒绝、缺权正式重授与原 fingerprint 检查保留。`inherited-upgrade-regression-20260910.log`。仍是真实 PG/维护/备份、合成签名包和模拟 host/runtime identity 的证据层。
- `go vet ./internal/hostctl` 完成。查询、HTTP、grants、迁移、恢复和前端实现均未变化，正常权限配置也已重验，故按已核摘要复用上一轮实际 reader HTTP200/安全错误/no-raw、Chrome、原生恢复及未受影响回归；本轮未重新启动 Chrome，不将历史页面证据说成本轮重跑。
- 本轮新 helper/recipient 及测试数据库按原生生命周期清理，读回数量均为0，reader 默认 NOINHERIT 保持；旧目录未删除。专用49998正常停止。`inherited-cleanup-readback-20260910.json`、`inherited-pg-stop-20260910.log`。

资源 guard 现在把主管保留目录 `/private/tmp/xi-manager-c-review15-zgqxucqr` 的实际79,544,320 bytes计入同一原始预算；没有修改 baseline、1GiB上限或20GiB储备。回归前段使用默认测试构建；两条 Engine 恢复测试用 `go test -ldflags=-w` 仅省略测试二进制 DWARF 调试信息，减少临时磁盘，源代码、测试断言、PG限制和正式制品均未改变。本段最大合并采样1,067,958,272 bytes，无预算违规；预提交采样和最终绑定见 `inherited-evidence-binding-20260910.json`。

本补交绑定与复用条件由 `output/xi-c-responsibility/inherited-evidence-binding-20260910.json` 给出，最终 commit/tree 随通知提供。以下为上一轮完整记录；它的通过结果不覆盖本轮已补齐的继承反例。

## 问题、修复与边界

真实 reader 查询 Connectors 时，既有 ApplyGrants 撤销了函数执行权限，却没有恢复查询必需的 text digest，导致 500。在责任子项前精确基线 `0d7c3a38272e32cc4d6f5ab6fe5fe17d931ae034` 的实际生产角色路径上重现了 SQLSTATE 42501 / permission denied for function digest。这里只证明该前态已有故障，不宣称定位了最早引入提交。

本次基于 `03fe6147830f8c25316a8537e292e5ce77c4246c`，仅由原 XI writer 在 `/private/tmp/xinsight-xi-c-responsibility-20260909`、`codex/xi-c-responsibility-history` 实施。授权为经理 independent-review-16 及所批准原方案。最终源码/证据摘要见 `output/xi-c-responsibility/connectors-evidence-binding-20260910.json`；最终 commit/tree 随交付通知提供，避免自身哈希循环。

- 正式 ApplyGrants 在事务内核对 `public.digest(text,text)` 的 pgcrypto 扩展归属、schema、owner、C 入口及既有函数属性，只恢复 xinsight_reader 的精确 EXECUTE。查询显式绑定 public schema；SQL 的 NULL/空串、UTF-8 输入、sha256/hex/前缀和响应字段保持不变。
- 目标 VerifyRoleBoundaries 必须有 reader text 权利，并拒绝 PUBLIC、其他 runtime role、bytea/其他 public 函数的越权或可转授权；原表/列/角色、邀请兑换边界继续执行。
- 升级源先执行原角色合同。schema23 及之后的源允许缺少本次 reader text 权利，仍拒绝越权与异常函数身份；pre23 保留原先 PUBLIC 函数合同，不提前套用 migration23 之后的目标约束。rc.10 的历史表 UPDATE 合同按精确产品身份保留。
- 复用 Install、Adopt、Upgrade、Resume 和 RestoreToNewDatabase 的现有授权动作。既有 runPhase 在恢复时重执行动作，因此恢复既能重授缺失权限，也会重新拒绝异常函数身份。没有新增迁移、维护入口、SECURITY DEFINER 包装或应用侧 raw cursor 哈希路径。

管理职责保持：`db/workbench_restore.go` 的 bytea 摘要及 `hostctl/clean_baseline_database.go` 的 checkpoint 历史/身份 text 摘要仍经 AdminDSN。它们不构成 workspace/importer 获得 digest 权限的理由。

## 验证结果

路径均相对于本工作树。实际 PG16.14 为专用 loopback 49998 的合成实例；用户账号、角色凭据、数据和签名测试包均为本地测试生成。没有真实认证提供方、来源或模型 API。

| 验证 | 结果与证据 |
| --- | --- |
| 精确旧基线 | 从 0d7c3a3 提取原 affected source、依赖、migrations/fixtures/contracts；实际 reader 查询重现 42501，且无 digest EXECUTE。`output/xi-c-responsibility/connectors-old-baseline-20260910-03.log`。|
| 权限与身份 | schema25 本地前态可进入修复，目标缺权失败；当前 ApplyGrants 幂等。reader bytea/hmac、workspace/importer digest、PUBLIC、grant option、reader 事实写入均被源/目标检查拒绝；SECURITY DEFINER、退出扩展、错 owner 被源/目标和 ApplyGrants 拒绝；实际角色连接及 admin bytea 执行符合边界。`connectors-postgres-final-20260910.log`。|
| 字符与 HTTP | 真正空列表、NULL、空串、ASCII、中文/é/emoji、引号/换行/制表符等合法数据库文本；actual reader、admin 查询和独立 Go SHA-256 精确一致。获权 human HTTP200；workbench-only 账号仍403；撤销 digest 后是无原值的500，不冒充空列表；正式 grants 修复后恢复。`connectors-reader-http-20260910-02.log`，及最终 `connectors-browser-harness-20260910.log`。|
| 安装与 legacy adopt | 正式 Engine 安装实际 schema23；真实 schema9 历史 ledger 经 CheckAdopt/Adopt 绑定、重授，再升级到26。主机操作和 HTTP runtime identity 使用现有替身，PostgreSQL、迁移、维护锁及 pg_dump 为真实执行。`connectors-postgres-final-20260910.log`。|
| 升级、check、失败与恢复 | 缺 reader text 权利的 schema23 源通过 CheckUpgrade；检查不修改 ledger、journal、role ACL 或 host 服务。真实越权/函数异常被拒绝。在 migrate_database 与 apply_role_grants 两个持久边界中断；Recover 只读，Resume 对异常身份返回明确拒绝，恢复正常身份且仍缺所需 grant 时由正式动作重授并成功，证据 fingerprint 不变。`connectors-postgres-final-20260910.log`。|
| 原生恢复与管理摘要 | current26 原生 pg_dump/createdb/pg_restore --no-privileges、新库重授及恢复验证通过；恢复前后真实 reader 的完整 Connectors 投影相同。admin checkpoint 文本摘要准确；以 default_transaction_read_only=on 强制执行 PreviewCleanBaseline 成功，前后预览相同；原库状态不变。`connectors-postgres-final-20260910.log`。|
| 兼容与合同 | 实际 schema20 rc.10 原表权限前态、目标收敛和负向边界复用已有 qualifier 并通过。当前生成 schema contract 在 grants 前后相同。迁移1—26、正式 schema23 身份、开发 schema26 manifest 和 release metadata 没有改写；见 freeze check。|
| 实际页面 | 同一受限 reader + production TLS handler + human operator + 当前嵌入页面：200、5条卡片、1个 NULL cursor、1个空串摘要；存在性与摘要正确，原值不显示。截图已实际查看。`output/playwright/xi-connectors-20260910-01/response-summary.log`、`connectors.png`。|
| 回归 | `go test ./internal/hostctl ./internal/api ./internal/db ./internal/webui -count=1 -timeout 8m` 全过；此组未配置 PG，数据库实测在前述独立日志。`go vet ./internal/hostctl ./internal/api ./internal/db` 过。`connectors-backend-unit-20260910.log`、`connectors-vet-20260910.log`。原69项前端测试/构建按未改源码与资产的03fe614条件复用，不声称重跑。|

最后一轮 PG 全组使用 `go test ./internal/hostctl -run '^TestConnectorDigest' -count=1 -timeout 3m -v`。浏览器运行同一 `TestConnectorDigestReaderHTTPAndRedaction`，由明确 stop 文件退出。环境为已有 Go1.25.11、GOPROXY=off、专用 GOCACHE/TMPDIR/GOTMPDIR；各命令均经原 `xi-c-resource-guard.py`，没有新增安装或改全局配置。

## 版本与证据层

正式 release-config 仍是 0.3.0-rc.13/schema23，其现有支持源列表未变。schema25 是责任开发片的本地前态；schema26 是当前开发候选。本轮 schema23→26 与 legacy9→26 使用载入真实 migration/contract 字节的合成签名测试包，包含占位 host binary 与模拟服务证明；它们不是正式发布制品，也没有增加 schema25/26 的公开升级承诺。正式发布、Linux Target 和真实外部集成仍须未来对应授权与制品验收。

保留 rc.10 和 pre23 分支的理由是现有正式支持合同，退出须正式收缩对应受支持版本；本轮没有这项授权或产品决定。原 migration23 的撤权保持不变，由当前正式 ApplyGrants 补足精确权利，避免重写历史。旧故障证据保留用于来源对照，不保留第二套运行时修复路径。

## 失败归类与执行优化

所有初次失败日志保留。旧基线首次缺提取的 contracts、第二次未启用 fixture 的生产角色开关；HTTP 首次用 workbench-only 人类账号，403 正确，已纳入负向测试。新生命周期 fixture 先后暴露 unused import、显式合成 admin password 要求、checkpoint 输入约束、签名包合同/rollback 元数据要求和 maintenance 拒绝闲置 admin 连接；均修正测试夹具，未下调产品要求。随后真实完整路径通过。不要把这些准备失败计作产品权限修复失败，亦不从最终 PASS 隐去返工成本。

按本次优化建议，实验在完整子项授权内自主收敛，未逐实验切成“材料—经理复核”；复用既有签名、生命周期、浏览器和角色测试工具，不新建观察器/协调系统；仅完整包或实质阻塞才通知经理。必要的权限、迁移、恢复与只读验证保留。上下文摘要只承载当前授权、身份、证据入口和未完成项；历史细节按需回查。本轮没有做优化前后耗时/token 对照，不能给出节省比例。

## 资源、现场和剩余项

保持原1GiB总新增预算（包含已有占用）、20GiB空闲底线；未重设基线。结束时专用 PostgreSQL 和 `xi-connectors` Chrome 已正常停止，原实例/profile、旧基线提取和所有失败场景保留。实际采样、冻结核对、准确字节和日志绑定见 `connectors-freeze-check-20260910.json`、`connectors-evidence-binding-20260910.json` 与 `resources.jsonl`。采样不是连续峰值证明，也不是 A0 性能资格。

A0 输入/二进制/模板与三旧目录冻结；未运行 A0 workload，未清理用户数据，原工作树保持干净。本次只有本地提交；没有 push/PR/merge/release/deploy、真实服务/流量/凭据操作、UR 联系、新 writer 或模型变更。经理完成独立验收后才关闭此 Connectors 子项；完整 XI-C、原13项及相关 TD 父级继续待验，D-37-01与A0资源/删除授权保持原状态。
