# 工作台范围授权（后继开发候选）

本入口补充正式本地账户配置与工作台的来源/项目授权操作；尚未表示已发行或已有客户部署。沿用现有单Role、`workbench_access_grants`和读写隔离，未新增IAM或SSO。

账户由主机root使用`xinsightctl access`的受支持TTY流程配置到`/etc/x-insight/access-accounts.json`。费用→执行→资源→复核→返回使用`xinsight.local-accounts.v3`的`workbench_investigation_reviewer`；只读使用`workbench_investigation_reader`。旧`investigator`不自动取得工作台权限；用途修订角色也不自动取得复核写权。账户必须启用，grant只补来源/项目/字段范围，不能扩张角色能力。

主机root准备0600、单硬链接、非symlink的绝对路径JSON文件。下面ID均是示例，必须替换为实际稳定subject、已登记source和新grant UUID；项目用实际引用，只有有意授全部项目才写`"*"`。省略、null、空项目不会自动扩大范围。JSON字段名必须使用下文精确的小写拼写；大小写别名、语义重复和未知字段均拒绝。

下面是“可看整个来源项目预算”的grant，只授予预算能力，不授予全项目明细：

```json
{
  "schema_version": "xinsight.workbench-grant-change.v1",
  "action": "create",
  "grant_id": "11111111-1111-1111-1111-111111111111",
  "subject_id": "xs_1111111111111111",
  "source_system_id": "22222222-2222-2222-2222-222222222222",
  "source_project_ref": "project-42",
  "cost_project_ref": "*",
  "expected_revision": 0,
  "capabilities": ["budget_definition", "budget_summary", "budget_alert"]
}
```

```text
sudo xinsightctl workbench-access apply --file /approved/grant.json --check
sudo xinsightctl workbench-access apply --file /approved/grant.json
sudo xinsightctl workbench-access show --grant-id 11111111-1111-1111-1111-111111111111
```

UniRouter 的来源项目引用使用 `project-<project_id>`（如 `project-42`），不是裸数字。预算是来源项目的全周期快照，没有可证明的费用项目分摊；预算权限要求 `cost_project_ref:"*"`，并用准确的 `source_project_ref` 限定来源项目。如果同一人同时需要“完整来源预算”和“仅某费用项目的汇总/明细”，保留上面的预算专用grant，再使用另一个ID配置下面的明细专用grant（费用项目引用须替换成实际已有引用，不能从来源项目编号推定）：

```json
{
  "schema_version": "xinsight.workbench-grant-change.v1",
  "action": "create",
  "grant_id": "33333333-3333-3333-3333-333333333333",
  "subject_id": "xs_1111111111111111",
  "source_system_id": "22222222-2222-2222-2222-222222222222",
  "source_project_ref": "project-42",
  "cost_project_ref": "replace-with-actual-cost-project-ref",
  "expected_revision": 0,
  "capabilities": ["summary", "details"]
}
```

两个grant使用不同文件，分别执行上述check/apply/show。预算专用grant的all-cost只配预算能力；明细grant仅在指定费用项目授予summary/details。二者仍同时受同一账户role限制。

**同一用户的有效grant按能力和范围取并集，窄grant不能限制另一个宽grant。** 如果已经照旧示例授予`summary/details`且`cost_project_ref:"*"`，添加窄grant仍会保留全来源项目明细权限。应先show该宽grant取得当前revision，再以`replace`、该ID及准确原范围，将其完整capabilities改成预算专用列表；或明确revoke不再需要的宽grant。其他仍覆盖同一明细的宽grant也须按实际权限需求分别处理，然后再增加窄的明细grant。不要把新建窄grant当成deny规则，也不能把受限费用明细当作整个来源预算。

`--check`只读当前账户、source、grant范围冲突和revision，不创建操作文件、不修改grant/audit。它不是锁定后续执行的批准：apply重新检查当前状态。使用当前安装的受保护admin DSN；命令不接收DSN、密码或普通会话凭据，普通用户不能调用此运维入口。

`create`要求revision0和不存在的grant ID。`replace`要求show得到的当前revision；其显式subject/source/project和完整capabilities列表替代该grant配置，不做追加式默认扩权。可用能力还包括`purpose_read`和`purpose_review`，但动作仍同时受账户role约束；辅助字段能力必须与至少一种摘要/详情/覆盖/筛选/运行/资源/预算能力一起授予。未知、重复能力及重复/未知JSON字段拒绝。

撤销时使用`action:"revoke"`、当前revision、准确原subject/source/project和`capabilities:[]`。撤销保留旧grant身份及历史，将active设false并推进revision；即使账户已禁用/移除也可撤销。恢复已撤销权限需显式replace，不能靠重放旧请求。不要将客户提供的JSON直接以root运行，运维配置者须核对精确范围。

读者与复核写事务继续使用原grant共享锁；配置变更按同一表锁→advisory锁顺序等待其结束，再检查并更新。变更、revision及`access_audit_records`中的root操作者、前后状态在同一事务提交；审计失败、取消、冲突均不部分生效。重试前show当前状态并重新准备expected_revision，不跳过并发检查。原授权状态和审计沿既有备份/恢复，不额外保存第二份权限状态。`--check`不保证来源正在供数或角色的全部客户链已通过。


## 普通新账户：资源与执行关联复核

以下是已批准安装上的正式配置路径，使用现有账户文件和 CLI，不需要体验专用 helper 或手工 SQL。账户配置、权限范围和服务重启须由获准管理员执行；以下示例不构成扩权批准。数据前提是来源、经济执行、运行请求及同请求资源已经通过各自正式导入路径登记，配置账户本身不会生成这些数据。

1. 在目标主机的受保护交互终端，对每个人分别运行 `xinsightctl access generate-subject` 和 `xinsightctl access hash-password`。subject 是稳定身份；password 仅从 TTY 隐藏读取，hash 也只能进入 root-only 配置编辑过程，不能放入 argv、环境、日志或聊天。CLI 不提供 `access create` 或写回账户文件命令。
2. 管理员保留现有账户，在 `/etc/x-insight/access-accounts.json` 的账户集合中追加人员条目，使用 `schema_version:"xinsight.local-accounts.v3"`。每个条目显式提供 `subject_id`、规范化 `username`、`role`、`disabled:false` 和有效 `password_hash`；复核人角色为 `workbench_investigation_reviewer`，读者为 `workbench_investigation_reader`。账户保持 root 所有、0400/0600、普通单硬链接文件；不得用全新示例覆盖现有人员。
3. 执行 `sudo /opt/x-insight/current/bin/xinsightctl access validate --file /etc/x-insight/access-accounts.json`。输出只有 schema、总数、各角色数及 disabled 数；可用 `workbench_investigation_reviewer_count` 与 `workbench_investigation_reader_count` 核对角色，没有 username、subject 或 hash。计数包含禁用人员，必须结合 `disabled_count` 和受保护原配置核对启用状态。校验不写文件、不加载服务；按既有配置流程原子替换并在已批准窗口重启后，新人员才能登录，原内存 Session 会失效。
4. 对每位人员分别准备下面两类精确 grant 文件。目标来源与资源报告方可能不同，不能只配置其中一侧。所有 UUID、subject 和项目都是示例，替换为已登记的实际身份；每个人、每条 grant 用不同 ID。目标来源例子有意允许 `project-42` 中全部费用项目；需要费用项目限制时用实际 cost_project_ref，不从来源项目编号推导。报告方的 resource grant 用其实际来源项目，`*` 表示该项目内不按费用项目过滤。

目标执行来源 grant（费用→执行→运行请求→资源，并保留执行关联）：

```json
{
  "schema_version": "xinsight.workbench-grant-change.v1",
  "action": "create",
  "grant_id": "44444444-4444-4444-8444-444444444444",
  "subject_id": "xs_1111111111111111",
  "source_system_id": "22222222-2222-4222-8222-222222222222",
  "source_project_ref": "project-42",
  "cost_project_ref": "*",
  "expected_revision": 0,
  "capabilities": ["summary", "details", "coverage", "preferences", "source_runtime", "source_context", "source_identity", "source_cost"]
}
```

资源报告方 grant：

```json
{
  "schema_version": "xinsight.workbench-grant-change.v1",
  "action": "create",
  "grant_id": "55555555-5555-4555-8555-555555555555",
  "subject_id": "xs_1111111111111111",
  "source_system_id": "33333333-3333-4333-8333-333333333333",
  "source_project_ref": "project-42",
  "cost_project_ref": "*",
  "expected_revision": 0,
  "capabilities": ["resource"]
}
```

三个 source 字段授权会增加该人员可读的来源上下文、身份和费用范围，不是默认初始化项。应明确确认其用途和精确范围；普通资源复核带执行关联须完整字段，缺失时 preview/Create 均拒绝。若只批准资源复核，可由用户显式取消“同时保留本次执行阅读关联”，不会保存该执行关联；不能把这一结果当成带执行关联的同一功能验收。C 内容来源的合法精确覆盖例外保持，必须覆盖每个 execution/as_of；本示例不授内容读取或正文查看权限。能读取已保存案件的 reader 同样须满足案件的来源字段与资源双方分类；reader 角色不会因字段 grant 获得复核写权。

每个文件须 root 所有、0600、单硬链接且非 symlink，分别执行：

```text
sudo /opt/x-insight/current/bin/xinsightctl workbench-access apply --file /approved/target-grant.json --check
sudo /opt/x-insight/current/bin/xinsightctl workbench-access apply --file /approved/target-grant.json
sudo /opt/x-insight/current/bin/xinsightctl workbench-access show --grant-id 44444444-4444-4444-8444-444444444444
sudo /opt/x-insight/current/bin/xinsightctl workbench-access apply --file /approved/reporter-grant.json --check
sudo /opt/x-insight/current/bin/xinsightctl workbench-access apply --file /approved/reporter-grant.json
sudo /opt/x-insight/current/bin/xinsightctl workbench-access show --grant-id 55555555-5555-4555-8555-555555555555
```

已有 grant 不用 create 覆盖：先 show 当前 revision，按原范围和批准的完整 capabilities 准备 replace，再 check/apply/show。本文 grant v1 例子不用于替换含 C 内容权限的 v2 grant；该类变更必须保留并明确核对 `allow_content` 与 `allowed_content_purposes`，不能隐式清除或追加。配置变更使旧 reading 失效，人员应登录并建立当前新阅读；不能以旧页面反复提交来验证新权限。

普通人员验收沿 HTTPS UI：reviewer 从费用中精确执行进入同请求资源，选定资源并保留执行关联，preview 后确认创建；核案件中的资源、execution/as_of 与复核历史。reader 应能 GET 同案件和历史，但写复核仍被拒绝。经批准重启后重新登录，再核持久关联与历史。以上 CLI 的 check/apply/show 与角色计数只证明配置步骤，不替代真实 PG、普通用户整链及重启验收；源未供数、资源双方范围不符或阅读过期须分别处理，不通过自动补权限消除错误。

## 隔离环境合成资源记录

schema37新增受限`xinsight-importer import-synthetic-resources --authorization-file /绝对路径/授权.json --target-ref 精确目标 [--check]`。仅接收`synthetic:true`的内部资源元数据，不执行资源访问、不授予模型或真实资源权限，也不开放生产I-03。原UR `import-source-facts`不接受此profile。

管理员沿原`source-facts offline-register`、`offline-enable`、`offline-status`、`offline-disable`、`offline-revoke`管理同一登记；登记默认disabled，enable需当前revision。授权文件和批文件须root拥有、0400/0600、无符号/硬链接，秘密不入报告。入口使用既有importer DSN；普通reader/workspace不能登记或导入。

授权版本`xinsight.source-facts-offline-authorization.v3`、profile `xinsight.synthetic-resource-access.v1`，保留原授权字段名；三个UR协议字段必须明确为空，不能把内部报告者标为UR。绑定本机ID摘要、安装版本/commit、schema37、实际数据库名/OID/cluster system identifier、完整报告方和完整目标方来源/instance/org/project、批摘要和绝对路径、操作ID及最长两小时有效期，无通配范围。新字段为`target_database_name`、`target_database_oid`、`target_database_system_id`、`target_source_system_id`、`target_source_instance`、`target_org_id`、`target_project_id`。管理员应从实际连接读取数据库身份；导入端重新核对。`source_schema_sha256`绑定发行源码的`internal/sourcefacts/contract/resource-access.schema.json`。

批格式为`{"schema_version":"xinsight.synthetic-resource-bundle.v1","records":[…]}`，最多50条且小于现有离线批大小限制。记录使用`xinsight.resource-access.internal.v1`；必须精确使用字段名，包含`synthetic:true`与显式`withdrawn`。报告、目标、修订和相邻hash按既有资源链校验；未知内容使用合同允许的unknown，缺前驱保留为缺口，不补造前驱。非法项使整批、操作绑定及回执一起回滚；check不预留操作或写入数据。相同操作重放仍检查当前授权，成功回执保持不变。

资源查看继续同时要求报告方resource范围和目标方当前范围。导入许可撤销只阻止后续导入；人员撤权阻止阅读；记录撤回用来源新修订表达，三者不能混用。备份恢复保留记录、链和人工工作并暂停导入授权；恢复到新数据库后必须签发新的目标绑定与授权ID，旧文件不能仅enable重新使用。

原内部资格入口仍只接受既定测试库名；本入口不会放宽它。旧记录读取保留原解码兼容，新写入使用严格字段校验，避免历史受支持数据因字段规范收紧而无法恢复。完整安装、浏览器链及恢复验收以本次候选实际运行证据为准。

### 资源依据的后续变化

复核保留当时明确采用的资源修订及其原始来源摘要。资源后来新增修订或声明撤回时，案件的复核与跟进区域、调查队列会显示当前提示，并进入“依据变化待复核”判断；不会自动替换依据、重写原结论或重开跟进。新修订的双方权限不足时，只提示当前状态不可用，不展示新修订编号或摘要。历史封存仍保留当时采用的内容。
