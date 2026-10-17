# 升级、迁移、回滚与恢复

升级是 root-only、单节点、受监督维护窗口操作。产品 UI 不持有 systemd、release directory、
credential 或数据库备份权限。

第一客户的升级仍由产品 owner 与 Codex 执行。产品 owner 不操作签名密钥；Codex 先核对准确
GitHub `main`/CI/workflow 和 delivery SHA-256，再运行下面的 preflight 与升级命令。客户不能
自行选择或安装一个未记录的包。

## 正常升级

先按“安装与首次启动”的信任引导，用已核对 hash 的 bootstrap 验签并把 next release 解到
尚不存在的 `/approved/staging/xinsight-NEXT-COMMIT`。跨版本升级必须由这个已验证目标
payload 内的控制器执行，先 preflight：

```text
sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl version --json
sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl upgrade --check --artifact /approved/path/NEXT-RELEASE.tar.gz
sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl upgrade --artifact /approved/path/NEXT-RELEASE.tar.gz
```

preflight 验证签名/revocation、平台、准确支持的来源版本、当前 release 无漂移、schema/config
兼容、PG16 client、磁盘、备份空间、无其他 writer/session、loopback listener，以及当前
root-only `/etc/x-insight/access-accounts.json` 的严格 identity/mode/content。credential
必须先用目标 staging 的 `xinsightctl access validate` 校验；缺失或漂移要在 journal、备份、
迁移和服务变更前 fail closed。升级按顺序持久化 journal、禁用 importer、备份、
expand-first migration、切换 immutable symlink、启动并证明 readiness/代表性读取与匿名
认证门禁，最后写 sanitized receipt。

不得用 `/opt/x-insight/current/bin/xinsightctl upgrade` 代替目标控制器。旧版本控制器只知道
自身发布时存在的 runtime file inventory，可能在数据库和应用升级成功后漏装 next release
新增的 systemd unit。目标控制器仍会严格验证当前与目标 artifact、支持路径和现有受管状态；
使用目标控制器不是跳过 preflight。

若 feed worker 已存在，upgrade/rollback/restore 会先持久化其 present/active/enabled
状态并 stop/drain，再关闭 importer LOGIN；迁移与产品读取验证通过、importer LOGIN 恢复后，
只按原状态恢复受支持版本的 worker。active 与 enabled 独立保留；例如升级前
active+disabled，升级后仍为 active+disabled。fresh install 只安装 unit，不自动 enable/start。

受管 Connector identity/config 与 release 文件分离：`/etc/x-insight/xinsight-connector-id.env` 只能
包含合法 stable deployment ID，`/etc/x-insight/xinsight-connector.env` 只能包含可选的规范 HTTPS
UniRouter origin。`upgrade --check` 会在任何 journal、备份、迁移或服务动作前分别验证已存在的文件；
实际 upgrade 按原字节保留两者，不生成新 ID、不覆盖 target。来自尚无 identity 文件的受支持旧版本时，
preflight 接受缺失状态，upgrade 在安装 runtime files 时只生成一次 deployment ID，仍保持 target 未配置、
worker inactive/disabled。生成使用完成 fsync 的 staging、no-clobber 发布与目录 sync；中断恢复只发布
完整文件，不会让空/半写最终路径阻塞 resume。应用回滚不删除 identity/target，随后 re-upgrade 继续复用
同一 ID 和 target。仅配置 TLS external origin 也是独立有效状态，不会被当成部分 Connector 配置。

本版本只接受 manifest 中准确 allowlist 的 Phase 1 legacy artifact 或准确
`0.2.0-rc.3`/`0.2.0-rc.4`/`0.2.0-rc.5`/`0.2.0-rc.6`/`0.2.0-rc.7`/`0.2.0-rc.8`/`0.2.0-rc.9`/`0.2.0-rc.10`/`0.2.0-rc.11`/`0.2.0-rc.12`/`0.2.0-rc.13`/`0.2.0-rc.14`/`0.2.0-rc.15`、
`0.3.0-rc.2`、`0.3.0-rc.3`、`0.3.0-rc.4`、`0.3.0-rc.5`、`0.3.0-rc.6`、`0.3.0-rc.7`、`0.3.0-rc.8`、`0.3.0-rc.9`、`0.3.0-rc.10`、`0.3.0-rc.11` 或 `0.3.0-rc.12`；commit 一致但 artifact/manifest digest 不一致仍应拒绝。后续仍只允许
manifest 明确列出的来源版本，不支持任意跨版本跳跃。

`0.2.0-rc.3`–`0.2.0-rc.15` 与准确 Phase 1 legacy 来源合法使用
reader/importer/admin 三凭据布局；rc.3 首次引入的目标控制器必须兼容这个来源事实。
`upgrade --check`/`adopt --check` 只读验证三条 DSN、角色、端点、密码隔离和旧权限边界，并在
report 中明确显示 `workspace_bridge_required`，不得生成密码、创建角色或切换 symlink。

实际 adopt/upgrade 在 durable journal 内生成独立 `xinsight_workspace` 密码，原样复制三条旧
DSN 到新的 root-only `0700` credential set，四个文件均为 `0600`。upgrade 必须先完成并验证
数据库备份，再创建/验证 Workspace 最小权限角色并原子切换
`/etc/x-insight/database/current`；旧 credential set 保持不变。角色扩展阶段开始后属于数据库
barrier，只允许 `recover`/`resume`，不允许 `abort` 伪装成未变更。存在但无效的
`postgres-workspace.dsn`、未知来源缺失该文件或任一继承 DSN 漂移都必须 fail closed。
`0.3.0-rc.2`–`0.3.0-rc.12` 来源已经是严格四凭据布局，不触发兼容桥。

一次性签名试点跨版本升级时，preflight 必须同时验证当前/previous release 与 next release。
因此 Codex 会在目标主机上建立只含两次交付公开公钥的临时 transition trust store，逐一核对
key ID、状态、有效期和文件 SHA-256 后再原子安装到固定 trust path；私钥从不进入目标主机，
已有 key 的 revoked/expired 状态也不得改写。该双公钥 store 必须保留到应用回滚验证完成且旧
release 正式退出 rollback window，之后才能以一条明确文件替换操作收窄为当前公钥，并把前后
hash 写入清理证据。不能先覆盖为 next-only trust，否则升级无法证明 previous；也不能在仍需
回滚时提前移除 previous key。这是 User+Codex 的受监督内部步骤，不要求客户管理密钥。

## Phase 1 首次受管接管

真实 Phase 1 目标可能仍使用旧超管 `xinsight`、`POSTGRES_PASSWORD_FILE`、
`/data/x-insight/postgres` 和两键 `xinsight.env`。这不是可以跳过的差异。首次接管只允许产品
owner 与 Codex 在维护窗口执行以下有序桥接：

1. 核对当前 symlink、legacy artifact/manifest、PostgreSQL 16.14 image digest、根
   `PGDATA=/var/lib/postgresql/data`、数据源 `/data/x-insight/postgres` 和 loopback listener；
2. 若 migration path 指向旧 release 目录，只有在两键文件、当前 final Phase 1 identity 与
   readiness 都准确匹配时，才原子归一化为 `/opt/x-insight/current/migrations`；
3. 安装 major 16 的 `pg_dump`、`pg_restore`、`createdb`，先生成 owner-only custom-format
   backup，核对 SHA-256 并通过 `pg_restore --list`；
4. 在目标内生成互不相同的 `xinsight_admin`、`xinsight_reader`、`xinsight_importer` 密码，
   创建 root-only versioned credential set 与 `postgres.env`；不得把密码放入命令参数、日志、
   Markdown 或共享目录；
5. 由现有受核实的 `xinsight` 超管只创建/验证新的 `xinsight_admin`；reader/importer 的创建、
   hardening、grants 与 writer gate 必须交给 `xinsightctl adopt` 的 durable journal；
6. 用已验签 `0.3.0-rc.13` staging 的 `xinsightctl access generate-subject|hash-password` 在交互
   root-only 流程中创建 `/etc/x-insight/access-accounts.json`，核对 regular/single-link/
   root:root `0600` 并运行 `access validate`；不存在默认可登录账户；
7. 先 `adopt --check`，再 `adopt`；成功后服务应保持 stopped，紧接着执行 `upgrade --check`
   与 `upgrade`，不能把已接管但未升级的状态当作可用服务状态。

```text
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl access validate --file /etc/x-insight/access-accounts.json
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl adopt --check --artifact /approved/path/xinsight-v0.3.0-rc.13-linux-amd64-COMMIT.tar.gz --legacy-artifact /approved/path/xinsight-release-linux-amd64-b9e5811cc53a5cb06614956e66a1c54756617f0a.tar.gz
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl adopt --artifact /approved/path/xinsight-v0.3.0-rc.13-linux-amd64-COMMIT.tar.gz --legacy-artifact /approved/path/xinsight-release-linux-amd64-b9e5811cc53a5cb06614956e66a1c54756617f0a.tar.gz
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl upgrade --check --artifact /approved/path/xinsight-v0.3.0-rc.13-linux-amd64-COMMIT.tar.gz
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl upgrade --artifact /approved/path/xinsight-v0.3.0-rc.13-linux-amd64-COMMIT.tar.gz
```

Phase 1 命名 Target 可能保留准确的
`/etc/systemd/system/xinsight.service.d/10-release-layout.conf`。本版本只接受项目已记录的
准确内容，并保留它作为历史配置；同时从签名 payload 安装
`99-xinsight-managed-launcher.conf`，使有效 `ExecStart` 始终指向
`/opt/x-insight/control/xinsight-launcher`。任何其他 active `.conf`、已知文件内容漂移或
非普通文件都会让 preflight 停止。工具还会把既有 `/opt/x-insight/control` 和 systemd
drop-in 目录恢复为 `0755`，而不是只修复新建目录。升级、回滚和恢复 receipt 中的
`service_launcher` 必须为 `pass`；这保证回滚到 Phase 1 时由 launcher 把 systemd reader
credential 转换成旧应用认识的进程环境，而不会把旧应用误判为“数据库未配置”。

接管期间保留旧 `xinsight` 账号、旧 Compose 文件、secret file、备份和 legacy release，以支持
已声明的应用回滚窗口；不得在同一数据目录同时启动旧/新两个 PostgreSQL 容器。新 release 会
把与 `/data/x-insight/postgres` 和根 `PGDATA` 一致的受管 Compose 写入 `/etc/x-insight`，但
应用升级本身不重建正在运行的数据库容器。数据库容器切换或旧超管降权是回滚窗口后的独立维护
动作，不属于首次接管。

## 中断恢复

跨版本 operation 完成前，必须继续使用启动该 operation 的同一个已验证目标控制器；中断点
可能仍在切换 current symlink 之前，不能退回旧 current 控制器：

```text
sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl status
sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl recover
sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl resume
```

`status` 显示当前 managed identity 和 active operation；`recover` 只读判断 durable phase 与
允许动作；`resume` 从已记录边界幂等继续。operation 成功并且 `status` 不再有 active
journal 后，日常状态与后续当前版本操作才切回 `/opt/x-insight/current/bin/xinsightctl`。
只有 `recover` 明确允许且尚未越过不可安全撤销 barrier 时，才可用同一目标控制器执行
`abort`：

```text
sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl abort
```

不要手工删除 active-operation、改 symlink 或编辑 receipt 来伪造完成。

## 应用回滚

当数据库 schema 仍处于 prior application manifest 声明的兼容范围、配置兼容、previous
release 无漂移且备份仍有效时，先检查再切回准确 previous binary：

```text
sudo /opt/x-insight/current/bin/xinsightctl rollback --check --previous
sudo /opt/x-insight/current/bin/xinsightctl rollback --previous --reason-code field_validation
```

允许 reason code 为 `failed_readiness`、`field_validation`、`operator_request`。应用回滚不执行
down migration；数据库保持 expand-first 后的 schema。若 prior binary 不声明兼容，应拒绝，
不能强制切换。

`0.3.0-rc.13` 当前 schema 为 23；既有应用只声明到 schema 20，当前单一 manifest
rollback policy 不按每个来源版本放宽。因此 `0.3.0-rc.13` manifest 的
`application_rollback_compatible` 为 `false`，`rollback --check` 会 fail closed；需要回到
旧应用时必须使用下面的隔离恢复，不能因迁移看似“只有新增列”而扩大旧 manifest 的
`database_schema_min/max`。schema 16–23 包含 Workspace、Agent Access、分析扩展、
bounded-offline provenance 约束、immutable completed operational receipt、不可变 checkpoint
epoch history、Connector installation 与三阶段 control operation；这些 additive
事实也不能扩大旧应用的 rollback 合同。

## 隔离数据库恢复

数据库损坏、迁移后的数据状态必须回到备份，或应用回滚不兼容时，恢复到一个新的
`xinsight_restore_*` 数据库，验证后再原子切换 credential set；不覆盖当前数据库：

```text
sudo /opt/x-insight/current/bin/xinsightctl restore --check --backup /var/backups/x-insight/EXACT.dump --backup-sha256 sha256:EXACT --new-database xinsight_restore_YYYYMMDD
sudo /opt/x-insight/current/bin/xinsightctl restore --backup /var/backups/x-insight/EXACT.dump --backup-sha256 sha256:EXACT --new-database xinsight_restore_YYYYMMDD --reason-code database_recovery
```

允许 reason code 为 `database_recovery`、`field_validation`、`operator_request`。失败时工具必须
恢复原 credential link；原数据库和 backup 保留，直到 custodian 做明确处置。

## PostgreSQL admin credential rotation

`database-admin-credential` 是 root-only 的管理凭据生命周期，不是账号密码、Connector credential、
DATA_RESET 或数据库恢复工具。它只替换四角色 active database set 的 `xinsight_admin` 成员；
`xinsight_reader`、`xinsight_workspace`、`xinsight_importer` 三个 DSN 必须逐字节继承。执行窗口要求主服务
正在运行且 PID 稳定、两个 Feed worker present/inactive/disabled、其他 `xinsight_admin` session 为零。
先取得不含 secret 的 preview：

```text
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl database-admin-credential preview \
  --authorization-ref decision://EXACT-AUTHORITY \
  --reason-code local_tooling_exposure
```

preview 绑定准确 release/commit/schema/config/protocol、active set 和四成员 metadata/canonical digest、
PostgreSQL 16 与 admin role identity、34 表/migration/connector/checkpoint/account/config 指纹，以及精确
worker/PID 状态。它不会输出 DSN、密码或 verifier。批准后把以下严格 JSON 写入 root-owned、single-link、
regular、`0400` 或 `0600` 的绝对路径文件；不要把内容改成 argv 参数：

```json
{
  "schema_version": "xinsight.admin-credential-rotation-request.v1",
  "preview_sha256": "sha256:EXACT-PREVIEW",
  "authorization_ref": "decision://EXACT-AUTHORITY",
  "reason_code": "local_tooling_exposure"
}
```

```text
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl database-admin-credential apply \
  --request-file /root/xinsight-approved/admin-credential-rotation.json
```

successor 先在 managed sets 同父目录的唯一 `0700` staging 中完整写入并同步四个 `0600` 成员，验证三项
继承原字节、admin identity 不变且 secret 不复用后，才原子 rename 到 journal 的最终目录。最终目录一旦
存在只按不可变完成态验证，绝不原地修补。verifier 变更前可以 `xinsightctl abort`；变更后只能
`xinsightctl resume`，不得把 predecessor 重新激活。产品在受控 predecessor transaction 中紧邻更新前
再次确认其他 admin session 为零；更新后原子切换 active set，终止竞态/恢复残留 session，并证明 fresh
successor 成功、fresh predecessor 得到预期 authentication rejection。成功后 predecessor set 只保留为
sealed/inactive，删除由另一个 exact-path retention 操作决定；本生命周期不删除任何文件或目录。

会话静默只统计 PostgreSQL `backend_type=client backend` 的 `xinsight_admin`，不能把 logical replication
launcher 等数据库后台进程算成管理员工作，也不能终止它们。preflight 只做有界只读观察，不会终止已知
并发工作；最终 verifier transaction 会再次原子核对其它 admin client session 为零。若 `ALTER ROLE` 已提交、
但进程在 journal 写入前失联，后续 `abort`/`resume` 必须重新探测 predecessor/successor 认证状态：一旦确认
successor 已成为权威，`abort` 必须拒绝并把操作持久化为 resume-only，`resume` 不得重复更新 verifier。

receipt/journal 只有 alias、metadata 和 digest。`demo.operator` 文件/verifier、产品表、schema ledger、
service identity、Connector credential/config、Cursor/Checkpoint 与 protocol pin 必须不变；主服务不重启，
两个 Feed worker 不启动/enable，poll/traffic 为零。

## 第二轮联调 fresh-database clean baseline

`clean-baseline` 不是通用清库工具，只用于联合裁决后的 full-product 第二轮联调基线。执行前必须
先让双方 traffic quiescent，并在 X-Insight 两个 Feed worker 均 present、inactive、disabled 时
生成 schema-current PostgreSQL 16 custom-format backup，记录绝对路径和 SHA-256，并单独通过
PostgreSQL 16 `pg_restore --list`。rc.7 upgrade 前的 schema-19 backup 或任何未在当前窗口生成的
旧 backup 都不能替代这一步。

DATA_RESET 与 BOOTSTRAP 是两次独立授权、两份 preview 和两份 durable receipt。DATA_RESET 不得
接受 next source/cursor/epoch。先只读预览 fresh DB cutover 与 credential retirement：

在生成 DATA_RESET preview 前，先用当前已验签 binary 读取 release expected contract，并对当前库
做 root-only、只读 introspection：

```text
/opt/x-insight/current/bin/xinsightctl schema-contract expected
sudo /opt/x-insight/current/bin/xinsightctl schema-contract inspect
```

`valid=true` 才表示 named semantic contract、schema 23、PostgreSQL 16 与完整 migration-set 收敛。
`column_layout_matches=false` 只说明物理 ordinal 不同，不参与 authorization；任何
`missing_object`、`unexpected_object`、`semantic_property_drift` 或 release identity mismatch 仍会
给出脱敏对象身份与 digest，并阻止 preview。命令不输出 DDL、行数据或 credential。

```text
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl clean-baseline data-reset-preview \
  --rollback-backup /var/backups/x-insight/EXACT-SCHEMA23.dump \
  --rollback-backup-sha256 sha256:EXACT \
  --new-database xinsight_clean_YYYYMMDD
```

preview 必须逐项给出 34 个 public table 的 `current_rows/protected_rows/rows_to_zero` 与稳定 semantic
digest，并证明 exact schema/FK/trigger/namespace/function/extension/type contract、一个 release
source、两个 release adapter、旧 binding identity 的 raw-free 摘要与历史最大 epoch、账号唯一性，
以及 UniRouter active/staged/operation-state、LiteLLM credential/environment 五个文件的存在性元数据。
LiteLLM environment 属于旧 campaign source/org/project scope。任何 unknown object、catalog drift、
已存在目标数据库、worker 状态变化、backup/hash 漂移、账号不唯一或 credential 文件类型/owner/
mode/link 漂移都停止。准确 DATA_RESET baton 只允许：

```text
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl clean-baseline data-reset-apply \
  --rollback-backup /var/backups/x-insight/EXACT-SCHEMA23.dump \
  --rollback-backup-sha256 sha256:EXACT \
  --new-database xinsight_clean_YYYYMMDD \
  --preview-sha256 sha256:EXACT-PREVIEW \
  --reason-code second_integration_data_reset
```

DATA_RESET 不把 backup restore 到 live 新库，也不对 live 数据执行 TRUNCATE、CASCADE 或手工 history
删除。它只创建全新数据库并运行当前 release migrations；新库保留 schema ledger、release
source/adapter catalog；所有旧 Evidence、Gap、Provenance、
Case、Alert、Analysis、Receipt、Quarantine、Connector state、service identity/token 与 product row
为零，且 `adapter_import_states`、epoch operation/history 必须严格为 `0/0/0`。它以 sealed digest
把 fresh rollback backup、旧 binding identity 摘要与历史最大 epoch 写入 receipt，不复制旧 history
到 live DB。文件访问面只保留 `demo.operator` 的 subject/username/role/disabled/password verifier
原字节，其他账号移除；主服务 PID 必须更换，使旧内存 Session 失效。

在任何 live cutover 前，apply 会在 quiesced 旧库的独占维护边界中把所选 custom-format dump restore
到 operation 唯一命名的临时 `xinsight_restore_*` 数据库。PostgreSQL major、完整 migration ledger、
静态角色授权、schema contract 与 34 张表的逐行稳定 semantic digest 必须和已授权、停稳后重读的
旧库 preview 完全相同。临时 restore 数据库随后必须删除并证明不存在，backup SHA 与
`pg_restore --list` 必须再次通过，才能把 sealed isolated-restore proof 写入 durable DATA_RESET
receipt。restore、摘要比较、临时库删除或 backup 再验证任一失败都 fail closed；不会生成或激活新
credential set 来完成这次临时验证。

同一 operation 内还会把 UniRouter receiver active/staged/operation-state、LiteLLM credential 与
LiteLLM environment 五个 canonical 文件逐一移入 product-owned quarantine；receipt 前失败恢复精确
原状态，receipt 后逐个移除 quarantine 并证明五个 canonical path 均不存在。流程只读取文件元数据，
不读取或输出 credential/config 内容，也不生成 replacement。若出现 symlink、
多 hard-link、owner/mode/type 漂移或 canonical/quarantine 双写，fail closed。
任何不属于当前 operation 的旧 quarantine 文件也会被视为未清 credential state 并停止。

LiteLLM systemd unit 本身保留且继续 inactive+disabled。当前 Paths 合同没有 OpenInference 等价持久
credential/config 文件，因此 DATA_RESET 不虚构新路径；OpenInference 产品数据由全局 fresh database
归零。只有 DATA_RESET journal completed、严格空库/账号/connector credential/config readback 仍与
durable receipt 相同后，
协调者才可单独签发 BOOTSTRAP baton。checkpoint request 必须使用
`xinsight.connector-checkpoint-epoch-prepare.v1`，绑定准确 source system、adapter manifest、cursor key、
worker unit、新 source instance、空 bootstrap Cursor/high-watermark 与 authorization reference；文件必须
absolute、root-owned、single-link、regular、`0400`/`0600`，raw Cursor 不进入 argv、日志或共享证据：

```text
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl clean-baseline checkpoint-bootstrap-preview \
  --data-reset-operation-id EXACT-DATA-RESET-OPERATION \
  --data-reset-receipt-sha256 sha256:EXACT-DATA-RESET-RECEIPT \
  --checkpoint-request-file /root/xinsight-approved/second-integration-checkpoint.json

sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl clean-baseline checkpoint-bootstrap-apply \
  --data-reset-operation-id EXACT-DATA-RESET-OPERATION \
  --data-reset-receipt-sha256 sha256:EXACT-DATA-RESET-RECEIPT \
  --checkpoint-request-file /root/xinsight-approved/second-integration-checkpoint.json \
  --preview-sha256 sha256:EXACT-BOOTSTRAP-PREVIEW \
  --reason-code second_integration_checkpoint_bootstrap
```

BOOTSTRAP preview 输出 canonical checkpoint request SHA-256；该 digest 同时绑定 authorization reference
与完整 request 内容，apply 中 staged root-only request 必须逐字节得到同一 digest。

BOOTSTRAP 从 sealed historical maximum 严格推导 `max+1`，拒绝 receipt 中出现过的 source/cursor
identity，并只写入一个 inactive/idle、零 counter、无 epoch operation/history 的 state。它要求两个
worker 继续 inactive+disabled，五个 connector credential/config path 继续为空，不启动/enable/poll，
也不生成 credential/config。该 UniRouter-only BOOTSTRAP 不生成 LiteLLM replacement；新 receiver
credential 仍由后续独立 credential prepare/register/activate authority 控制。

receipt 前任何失败会删除 operation 的临时 restore 数据库，并恢复旧 database-set link、完整旧账号
文件、importer login 和主服务；新严格空库不删除，保留供取证且不得复用。此时旧库仍只是本次
cutover 的临时 rollback source。durable receipt 已写入后只允许 exact readback `resume`：再次证明
live link 只指向严格空库、retained dump 的 path/SHA/PG16 archive 与 sealed restore proof 未漂移，
并确认旧库没有 session 后精确 `DROP DATABASE` 旧库，再证明旧库与临时 restore 库都不存在且 dump
仍通过验证。缺少 dump 或 restore proof 时，必须在旧库删除前停止。之后才移除含其他账号的单文件
前驱备份与 connector quarantine；不允许 abort 已跨过 receipt 的 cutover。

成功 completed 后，目标只保留这一个已验证 custom-format dump 作为 pre-reset rollback artifact；
不会同时保留 predecessor 数据库或隔离 restore 数据库。该流程不会创建/旋转任何
connector credential/config，不启动/enable worker，也不请求或生成 Feed/test traffic；这些必须由后续
BOOTSTRAP/TRAFFIC baton 分别授权。

## 决策表

| 情况 | 动作 |
| --- | --- |
| operation 中断，journal 完整 | `recover` 后按 allowed action `resume` 或有限 `abort` |
| 新 binary readiness 失败，prior application 仍兼容当前 schema | application rollback |
| 数据需要回到备份，或 prior application 不兼容 | restore-to-new-database |
| 签名、digest、来源版本或当前文件漂移 | 停止；不迁移、不激活 |
| PostgreSQL major 不是 16 | 本版本不自动升级；停止并另行规划 |
