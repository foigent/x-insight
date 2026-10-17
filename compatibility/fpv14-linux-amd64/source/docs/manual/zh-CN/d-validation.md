# 当前 D 隔离候选：预算与来源依据

本页记录 FPV14/schema44 的零 capability 凭据写入与明确重新授权后的来源恢复修复候选，沿用已有预算、来源、资源与受控内容工作台。产品源码、前端、手册及安装包尚待绑定到同一冻结候选；本页不表示已构建或签发制品。

## 版本与适用范围

本页对应 `0.2.0-dev.fpv14`，手册版本 `0.2.0-dev.fpv14/manual-r1`。只适用于已授权的 XI 隔离验证环境；正式签名、安装、升级和恢复资格以相同 commit/制品/环境的回执为准，源码中的候选名称不表示已验收。

| 项目 | 当前值 |
| --- | --- |
| 数据库 schema | 44 |
| PostgreSQL | 16，同机 loopback 专用数据库 |
| 主机 | 既有 Ubuntu 24.04 / Linux amd64 XI guest，systemd |
| 配置 | `xinsight.config.v1`；显式预算采集使用 managed-source-facts v3 |
| 升级前态 | 精确 B2 `0.2.0-dev.b2` / schema35；D1 `0.2.0-dev.d1`、D2 `0.2.0-dev.d2` 、FPV1 `0.2.0-dev.fpv1` 、FPV2 `0.2.0-dev.fpv2` 、FPV3 `0.2.0-dev.fpv3` 或 FPV4 `0.2.0-dev.fpv4` / schema36 制品与安装状态；本地 FPV5 `0.2.0-dev.fpv5` / schema37 |
| 应用回滚 | 不声明旧应用可运行 schema44；数据库恢复必须显式执行 |

当前不声明客户发布、UR 真实增强来源、完整 F 配对、原 A0/S 或最终 V 通过。历史 B 和 rc.13 手册、配置及回执保留原身份，不构成本候选的升级白名单。

## 正常接入与正文恢复

管理员从“连接与接入”的“开始设置”进入正常接入。没有 XI 留存策略时，页面提出完整正文、90 天、每项目 5 GiB、单条 32 MiB 的草稿；只有人类明确确认并完成 UR 同意，后台才应用期望。已有生效策略（包括停用）继续复用。UR 来源采集独立约束复制：没有企业策略时须在 UR 最终确认中选择来源数值；正常推荐为完整正文、30 天、每项目 5 GiB、单条 32 MiB，并使用实际来源支持的协议及观察点。组织管理员明确授予企业策略读写后，才能从 XI 联合管理正文开关；项目及用户例外继续保留。

设置页分别显示等待同意、准备连接、等待数据和当前问题；历史同意不冻结以后修改的当前策略。浏览器只保留当前主体的非秘密请求引用与同意内容以恢复丢响应，邀请令牌仅用于临时跳转与请求，不进入持久浏览器存储。原邀请过期后须重新审阅确认，不能延长旧邀请。

正常安装及升级在原主机锁与操作回执内准备正文密钥；已有或停用密钥继续使用。存在任何密文时，丢失原密钥必须停止并恢复原件，不能生成替代密钥。启用完整正文前检查实际 API 及已配置来源 worker 所加载的相同密钥；文件存在或配置就绪不能代表已加载。已经加载时不重复重启服务。

完整私有备份包含数据库与原正文密钥，按私有文件保管；恢复先核对准确包摘要，在新数据库与独立安装根导入原密钥，再读取未到期正文。已到期正文仍不可读；来源端已经到期不改变 XI 自己的期限。恢复到不同部署会撤销还原数据库中的原运行身份和令牌，历史记录保留，重新接入须走当前授权。旧数据库单独备份仍按原兼容路径处理，不能据此宣称正文密钥已恢复。

```sh
xinsightctl backup --output /absolute/private-backup.tar --check
xinsightctl install --artifact /absolute/signed-artifact.tar.gz --recovery-bundle /absolute/private-backup.tar --recovery-bundle-sha256 sha256:EXACT_DIGEST --check
xinsightctl restore --recovery-bundle /absolute/private-backup.tar --recovery-bundle-sha256 sha256:EXACT_DIGEST --new-database xinsight_restore_approved --check
```

上述路径与摘要须替换为已批准的准确对象。当前本地实现、隔离 PostgreSQL 与 HTTPS 夹具证据不表示 Linux 服务生命周期、签名制品或 Target 已通过。

## FPV10：工作事件与复核正文

本地 schema41 增加 OpenClaw 工作事件接收，用原始事件的范围、对象、时间和结果展示近期工作；缺少动作证据时仍显示缺口，不推断模型已完成修改。schema42 增加按授权与有效期限制的 XI 复核正文副本，XI 留存策略与 UR 来源采集策略分别约束实际复制。schema43 增加受管工作事件安装邀请：首次登记候选 Agent 须经管理员批准；对同一已授权安装，未来 Agent 可按已批准的自动纳入策略建立独立 XI 项目和 collection，不能据此推断 UR 项目。schema44 增加原接收端接管、独立机器候选回执和来源登记，保留原 epoch、游标、来源键和正文引用；v3 两端使用各自主机时间，不跨主机排序。正文复制到 XI 后按 XI 权限和留存期读取，原 UR 来源元数据保持原样。当前源码和本地测试不能代表 Target 验收或签名制品资格。

## FPV9：测试文件资料与修改结果（历史）

在已授权的隔离测试阅读中，“系统与数据 → 这次工作”展示 OpenClaw 原生运行时报告的读取、修改对象和状态。五轮 UR 请求只有在准确的关系回执与原生事件/响应身份相符时合并为一次工作；无法确认关系的动作只留在触发请求下。文件修改的“目标检查通过”来自独立测试检查器的前后完整文件摘要与字节检查，不等于资源系统审计，也不证明模型内部实际使用了提交内容。

读取原文、提供方请求和前后差异均需当前 C 内容读取权限，并按准确请求、内容引用及有效期按需读取；无权或到期时仍可看获准的动作元数据，但不能从旧正文推断结果。提供方请求表示只说明网关向提供方提交了什么。本地 schema40 增加 `xinsight.resource-action.v1` 元数据，首批只准 test/development 的受控离线导入；CC15 真实 OpenClaw/UR/XI/浏览器整链与同版制品资格尚未通过。

## FPV8：执行关联预览与提交权限一致

资源复核保留执行阅读关联时，预览和提交共同检查当前权限：除精确的内容来源覆盖例外外，关联执行须同时具备 source_context、source_identity、source_cost。缺权限时预览即拒绝，不再先给出可确认预览再在提交时拒绝。有效 C 来源必须覆盖同一 execution 的每个 as_of；既有相同 tuple 仍可复用，不以任意内容引用放行其他截止。显式不保留执行关联的资源复核继续按资源双方权限处理。

新账户的正式角色配置、逐来源授权与验收顺序见下一章“工作台范围授权”。access validate 补齐 v2/v3 角色计数，不改变角色能力或替用户授权。FPV8 当时的数据库 schema39、API、内容与资源合同和升级前态白名单保持；候选版本推进不表示新增升级来源已获验证。

## 在六个工作主题中查看预算

总览、使用与目的、运行与执行、安全与复核、费用与使用、来源与覆盖保留原入口。运行请求、能力、资源资料与 I-01 金额仍是不同证据集合。预算从“费用与使用”的“查看全周期预算”进入；访问取决于当前人员与字段授权。

页面分别显示来源预算、同口径全周期已知小计、计划差额、来源周期和截止、覆盖状态及提醒。原月度周期使用来源 UTC 边界，不能被全局近7天筛选替换。币种/精度分别保留，不相加或隐式换算；显示百分比的舍入不改变源阈值。

- 预算1000、同口径完整已用600，计划差额为400；已用1200则为-200。差额不表示余额、预留或还能调用多少。
- 未设置、显式零、停用、不支持、未批准、无权、覆盖不足分别说明。中途接入或恢复缺口不能当作可信零。
- 来源全周期摘要600与 XI 局部明细200不能相加成800。当前来源未给出逐请求包含关系，也不能仅凭两个金额说缺明细400。
- 单次来源 reading 的定义、摘要和提醒按各自授权出现。缺定义权时不会通过摘要显示其分母或计划差额；不会从旧版本拼补。
- 预算1000改为1500不会改写旧 source reading；旧提醒继续说明其当时已用/预算证据。当前超额、提醒resolved、已投递、人工已读和实际调用限制是不同事实。

点击“查看依据”检查来源阅读 ID、版本、截止、收到时间、当前可访问文档/hash，再查看各次独立来源快照。返回时重新检查当前访问；撤权或阅读过期后不继续显示旧金额。历史仅包含本次 XI 阅读截止前已收到的快照，后到修订不会把旧阅读补绿。提醒续页保留为新的来源快照，不把不同阅读合并成一个财务状态。

受控离线输入明确标为“受控离线快照”，不表示当前在线状态。来源没有正式计算费用预算 profile，独立供应商账单来源也未绑定；两项正向目标仍开放。现有原扣费与计算费用不能改名为独立账单。XI 不更改来源预算、不发外部提醒、不阻断调用。

## 登记、权限和 CLI

来源采集与人员阅读分开授权。旧连接/凭据不自动增加 D；管理员显式登记 collection 和 profiles，实际来源许可可为1—3份非空组合。字段授权 allow_budget_definition / allow_budget_summary / allow_budget_alert 默认拒绝；来源项目限制仍适用。预算权不授予全部费用、人员或执行明细。

在线目录和提醒最多50项/页，每份文档最多64KiB；修改许可/绑定后须重新检查目录。暂停或撤销预算集合不会重置 A/B 事件游标。受控离线授权须绑定当前目标、机器、精确产品版本/commit、实际目标 schema44、冻结来源合同和完整 bundle 摘要，有效期最长2小时。离线登记默认暂停，由管理员启用、importer 单事务接收；check 无写入。已消费的离线授权暂停/恢复后需新登记，不能当作在线恢复许可。

完整命令、JSON 字段与边界见随包来源说明。常用查询：

```sh
xinsightctl query budgets create --origin https://XI_ORIGIN --username PERSON --request-file /absolute/budget-reading.json
xinsightctl query budgets items --origin https://XI_ORIGIN --username PERSON --reading READING_ID --limit 25
xinsightctl query budgets history --origin https://XI_ORIGIN --username PERSON --reading READING_ID --budget-ref MEMBER_UUID
```

CLI 沿人员 HTTPS Session 与 CSRF，密码仅从交互终端读取，结束登出。旧阅读、JSON 和 CLI 都重查当前权限。API 与页面阅读有效期30分钟；过期410、授权变化409、范围不足403，按提示重新建立当前有权阅读，不伪装成旧结果。

## 未签名准备与信任边界

构建入口是 `scripts/build-signed-release.sh`。默认仍选择历史 rc13；开发准备必须显式选择 `--profile=development-d --prepare-only`。只读 `--dry-run` 仅核对干净源码、工具链与交付配置，不生成文件、不读取签名密钥、不编译，也不证明生成物或安装可用。prepare-only 使用已备依赖，生成未签名 payload、bootstrap 工具和 `prepare-result.json`；结果明确为 unsigned、不可安装，没有签名 manifest 或正式包。它仍要求干净源码和准确工具链，不能把带 WIP 的 HEAD 当作制品身份。

当前代码的 CI、owner release、conformance、`build-release.sh` 与签名构建/验签入口使用 Go 1.25.12、Node 22.23.1、npm 10.9.8。CI 中复现准确 Phase 1 制品是唯一历史例外：先由 setup-go 切到 Go 1.25.11，以冻结 commit `b9e5811cc53a5cb06614956e66a1c54756617f0a` 的旧脚本重建并核对原制品 SHA256，再恢复 Go 1.25.12 验证当前 reader；历史摘要不因当前工具链更新而改变。`go.mod` 的 `go 1.25.0` 仍是模块最低要求。验签入口核对签名 manifest 的 `build_toolchain`，并要求四个产品二进制与 bootstrap 的 `version --json` 报告同一 Go 身份。历史制品保持原记录，旧工具链的测试结果不能代替新候选的制品与目标资格。

开发配置、矩阵和 payload 均使用 schema44 的 `development_schema_contract.json`。产品版本、commit、构建时间、Go 和手册版本沿现有构建参数注入；数据库范围保持编译期44。冻结后须重新构建并核对前端、manual、四个产品二进制和 bootstrap 的同版身份；当前本地准备不能代替此步骤。

## 安装、升级与恢复

以下是同版签名包形成后、在已批准隔离主机上使用的操作顺序。本地未签名目录不满足入口条件，不能直接复制为受管安装。开发配置仍沿原 Ubuntu 24.04 / amd64 / systemd / 同机 loopback PostgreSQL 16 隔离验证范围；新的精确目标、容量和操作窗口由总体核定，不从本页推定已批准。

1. 从已批准渠道核对准确包、bootstrap verifier、公开 trust store 的摘要与来源。不要使用包内自带公钥自证，也不复用历史 rc13 的发布身份。使用经独立核验的 `xinsight-release verify`，指定 `--artifact`、`--trust-store`、`--expected-commit` 和 `--expected-version 0.2.0-dev.fpv14`；再用 `extract` 的 `--destination` 解到不存在的绝对目录。核对四个二进制 `version --json`、内嵌手册、webui manifest，以及 `xinsightctl schema-contract expected --raw` 与签名 payload 的一致性。
2. 首装前明确专用空库、服务账号和四角色凭据：admin 管理结构与授权，importer 接收来源，workspace 建立冻结阅读/调查对象，reader 只读。四份 DSN 位于 `/etc/x-insight/database/sets/APPROVED_SET/` 的 `postgres-{admin,reader,workspace,importer}.dsn`，目录0700、文件0600，由 `/etc/x-insight/database/current` 指向该绝对目录；不得覆盖现有凭据集。沿包内配置模板及正式 access CLI 准备人员访问、当前授权和批准的 HTTPS origin。凭据、权限与 HTTPS 配置由对应批准步骤提供，本页不授权生成或替换。
3. 使用该目标包中已验签的控制器先运行 `install --check --artifact ...`，再执行 `install --artifact ...`；已有安装先核准下表的准确前态、备份和安装状态，再以目标控制器运行 `upgrade --check --artifact ...`、`upgrade --artifact ...`。不用现运行的旧控制器驱动跨版本升级。check 前后核对数据库、序列、配置、安装槽和运行状态无变化。已有 UR 加装 XI 仍需独立的许可、连接启用与普通用户整链验收；安装成功不自动启用 Feed 或扩大授权。
4. 失败先保留精确候选、备份、operation journal 和回执，以同一已验证目标控制器执行 `recover`，根据报告执行 `resume` 或受支持的 `abort`；不 reset、不手改 SQL、不覆盖旧槽。数据库恢复先 `restore --check --backup /absolute/EXACT.dump --backup-sha256 sha256:EXACT --new-database NEW_APPROVED_NAME`，再按批准动作恢复到新库；保留原库及失败现场，核验财务事实、人工记录、历史引用和摘要。恢复后采集保持暂停，当前可见性重查授权，离线授权重新绑定实际schema与目标。
5. 本候选 `application_rollback_compatible=false`。必须实际验证回退入口准确拒绝，不使用强制 schema 降级；如需旧版本恢复，另核对应备份、旧包和新隔离库，不能静默丢弃39期间新增记录。

### 保留的升级来源与待验状态

原八项 `supported_upgrade_from_versions` 保持原声明，受管 `0.2.0-dev.fpv11` 来源保留，本候选保留 `0.2.0-dev.fpv12`，并另列当前已部署的 `0.2.0-dev.fpv13` / schema44 正常升级来源。FPV4只是首条验证顺序，不缩小其他来源的支持边界。下表“包摘要已核”只指现存包字节与原 pack 记录一致，不表示该来源到 FPV14 已通过。

以下保留历史 FPV9→FPV11/schema43 的独立入口；它不构成 FPV14 升级资格。原八项不含 Target 的未受管直运行 FPV9。FPV11 增加独立 `upgrade-direct-fpv9` 一次性入口，不把该前态解释为已签名 V2 或通用升级来源。管理员须提供仅含准确系统 ID、数据库名、两项运行二进制路径、旧服务与配置摘要及单个已审阅公网源站 IP 的根持有 `source-proof` 文件（常规文件、0600、单硬链接）。三个运行服务的进程路径与两项二进制摘要会在 check 中分别验证。先以验签目标控制器运行 `upgrade-direct-fpv9 --check --artifact ABSOLUTE_BUNDLE --source-proof ABSOLUTE_PROOF`，核对只读前态和目标身份，再按另行批准的操作执行不带 `--check` 的相同命令。该入口只接受本次固定 FPV9 commit/二进制/40 条迁移摘要与 FPV11/schema43 签名目标；三个旧程序路径覆盖项会留恢复副本并退役，只将旧配置中已放行且与来源配置相符的单个 IP 带入受管 unit，正文及来源凭据引用保持原位。旧 unit 中的 TLS 代理设置须与已准备的受管环境文件逐值相等；若 `/etc/x-insight/xinsight.env` 已存在则 check 拒绝，避免迁移后才发现配置冲突。跨越迁移屏障后以 `recover`/`resume` 续办，不把未签名旧程序列为可回滚目标；需要回到 schema40 时仅可在新库验证准确备份与旧程序。源码与本地测试尚未构成 Target 资格，原八项声明不变。

`source-proof` 的固定字段为 `schema_version`（`xinsight.unmanaged-fpv9-source.v1`）、`product_version`、`commit`、`database_name`、`system_identifier`、`egress_allow_cidr`、`server_binary`、`worker_binary` 和 `files`。`files` 必须恰含 `service_unit`、`feed_unit`、`source_facts_unit`、`service_content_dropin`、`service_delivery_dropin`、`feed_delivery_dropin`、`source_facts_credentials_dropin`、`source_facts_delivery_dropin`、`source_facts_config` 九项，每项为 `sha256:` 前缀的文件摘要。只读取摘要，不把密钥正文放入证明文件或回执；目标公钥信任库、受管 TLS 代理配置与签名包需由独立批准步骤准备。

| 声明来源 | 数据库前态 | 当前证据及缺口 |
| --- | --- | --- |
| `0.2.0-dev.b2` | 35 | 包摘要已核；35→44整包升级、check及恢复待验 |
| `0.2.0-dev.d1` | 36 | 包摘要已核；36→44整包升级、check及恢复待验 |
| `0.2.0-dev.d2` | 36 | 包摘要已核；36→44整包升级、check及恢复待验 |
| `0.2.0-dev.fpv1` | 36 | 包摘要已核；36→44整包升级、check及恢复待验 |
| `0.2.0-dev.fpv2` | 36 | 包摘要已核；36→44整包升级、check及恢复待验 |
| `0.2.0-dev.fpv3` | 36 | 包摘要已核；36→44整包升级、check及恢复待验 |
| `0.2.0-dev.fpv4` | 36 | 包摘要已核，首验前态；36→44整包升级、check及恢复待验 |
| `0.2.0-dev.fpv5` | 37 | 尚未找到准确安装包；声明保留，取证及整包链待验 |
| `0.2.0-dev.fpv12` | 44 | 保留原受管来源及资格身份 |
| `0.2.0-dev.fpv13` | 44 | 当前已部署前态；FPV14 保持 schema44，正式升级 check 待总体执行 |
| `0.2.0-dev.fpv11` | 43 | 正常受管 43→44：本地原状态迁移、恢复前向迁移已验；签名整包与 Linux CI 待验 |

FPV4首验包为 `xinsight-v0.2.0-dev.fpv4-0fa3977e7a51-linux-amd64.tar.gz`，SHA256 为 `5d9256b1d83ef0e6710f3b153f3816562149f7720f3b14c1434051f43241405d`。FPV6/schema38目前也缺准确安装包证据；它不在原八项声明内，本次不自动增列或删除任何既有身份。既有36/37/38→39数据库迁移测试与九个历史角色策略身份测试均不能替代目标 schema44 的整包链；35→44还须覆盖其真实前态。

### FPV14 凭据写入与恢复接续边界

root:xinsight 控制器无需新增 CAP_CHOWN。host 文件按 UID0、秘密精确0400/0600或公开0644、单链接、安全父目录和原子发布保护；GID记录实际值，systemd凭据的原生ACL/GID/只读挂载要求保持。机器凭据目录保持0700。

当前重新授权的来源范围须匹配原配置、来源/项目/类别/版本、新同意、目的授权及authenticated preflight。恢复暂停只有精确 restore_requires_authorization 原因可通过原 resume/ack/CAS 接续；普通暂停、撤销、scope_changed 和其他未获新授权来源不恢复。既有消费者先在集合暂停时准备，匹配正文策略生效并实际加载原密钥后才能恢复集合。原stream/C游标、epoch/HWM、ID和历史保留；预算当前快照沿原resume语义重新刷新。预览、状态与check只读，不自动恢复旧来源。

FPV13包及历史 FPV11→FPV12 fixture身份保留。原历史CI不替代本候选的真实Linux零cap回归、签名制品及获批FPV13→FPV14 check/Target验收。

### Campaign package 的同版验收缺口

D/F的 pack、verify 和 hostctl 安装不调用 campaign authority。Campaign authority 的本地实现保留历史 schema23 factory，并允许编译期 min=max 的精确当前代际（本候选为44）；输入清单不能自行定义当前代际或扩张范围。历史schema20仍仅可解析，不获得执行权。签名清单、M0、N1、component scope、运行版本与执行文件的精确绑定继续适用，23与44的身份不能混用。历史包、样例、签名和摘要保持原字节。本地一次性签名夹具及离线校验不代表真实CLI、同版签名制品到schema44 PostgreSQL的正式导入已获资格。

这个缺口属于原第三批 P4/P6：签名包中同版 campaign consumer/verifier 的身份派生、与冻结 N1及UR component scope 的绑定、正式导入及负向拒绝须有对应证据。历史整链测试通过、普通 Feed/J1通过或本次安装准备，均不关闭该项；本说明不将其降为可选功能，也不宣称整体交付完成。通用 `scripts/verify-signed-release.sh ARTIFACT TRUST_STORE ABSOLUTE_DESTINATION --profile=development-d` 显式选择 D 的版本、手册、矩阵和 schema contract；默认仍为 rc13。它保留完整签名、campaign authority、五个二进制、WebUI、手册和主机模板检查，profile、元数据、工具链或 payload 不一致均拒绝。离线入口夹具通过只证明控制流与检查，不代替同版真实签名包及 schema44 PostgreSQL 正式导入资格，不能跳过失败宣称完整验包通过。

以下保留 FPV5/6 当时的增量说明；其中历史前态、授权格式和待验状态不等于 FPV14 当前环境事实。

## FPV5/schema37候选增量

schema37仅扩展内部合成资源离线授权，不改D金额或预算语义。B v2可验证历史35/36授权；实际37安装仍须新37目标绑定。D离线接受36/37格式并核对实际数据库schema，旧36授权不能用于37库导入。原v1/v2字段与摘要保持。36→37、原权限、A/B/D数据和复核的保留及正式安装恢复仍以本候选实际证据为准；当前候选配置不等于发行或验收通过。资源管理入口见[工作台范围管理](admin/10-workbench-access.md#隔离环境合成资源记录)。

## FPV6/schema38：资源依据进入复核

系统与数据中的资源卡片可直接选择具名修订作为支持、反证或背景，经服务端预览后进入原Case/Finding/Review。历史依据保留完整报告方/目标范围、请求、修订/hash及来源声明，不依赖短期reading，不自动替换为后续修订；记录已撤回不等于导入许可或人员授权撤销。资源声明仍不能证明真实内容、转换效果或外发许可。

复核、队列、人工核验结束、封存与续办沿原流程；可只采用资源，也可与Evidence/Gap混合。在线操作按报告方资源权限及目标实际profile/项目权限检查；没有新的自授权入口。备份恢复核历史引用、原记录hash及动作链，即使历史复核者现已撤权仍可恢复；恢复后的读取按当前权限拒绝。原无资源命令/basis/封存字节保持，含资源的basis/pack分别使用v2/v4。

正式ASUS前态仍为FPV4/schema36；schema37只是本地已接受实现的额外迁移前态。38候选同时检查36→38和37→38，旧导入授权须重新绑定实际schema与目标。当前源码/Local证据不代表正式安装或完整F/PX/V已通过。
