# 全周期预算快照接收与查询

D 按冻结 UR 提交 `a84ac2726e079796ade0af573b575b7c11bbdfbc` 的 `contracts/source-budgets/v1` 接收预算。原始合同、manifest 和5组正向/8类反向向量保存在 `internal/sourcefacts/contract/budgets`。这只说明支持的来源格式，不证明当前 UR 已接入或完整配对验收。

## 在线接收

在 root 所有、0400/0600 的 managed-source-facts 配置中显式使用 `xinsight.managed-source-facts.v3`，增加 `budgets` 数组。每项明确 collection_id、source_system_id、source_instance、org_id、project_id、scope_generation、profiles、endpoint、key_file、allow_private_tunnel_http；其他 A/B 配置不变。A/B 与预算配置合计最多16项，旧连接/凭据不会自动增加预算权。

endpoint 必须为批准来源的 `/api/v1/integrations/xinsight/source/v2/budgets`。来源配置、连接和凭据决定真实授权；XI 的 profiles 是额外上限。每次目录检查实际授权1—3份，收到的 reading 必须恰好匹配目录集合。合法收缩与同一授权代数的真遗漏分别处理。目录/提醒位置仅用于枚举；它们不是金融事件游标，也不累计金额。

```sh
sudo xinsightctl source-facts configure --file /absolute/managed-source-facts.json
sudo xinsightctl source-facts budget-register --collection-id UUID
sudo xinsightctl source-facts budget-preflight --collection-id UUID
sudo xinsightctl source-facts budget-enable --collection-id UUID --revision REVISION
sudo xinsightctl source-facts worker-start
sudo xinsightctl source-facts budget-status --collection-id UUID
sudo xinsightctl source-facts budget-pause --collection-id UUID --revision REVISION
sudo xinsightctl source-facts budget-resume --collection-id UUID --revision REVISION --acknowledge
sudo xinsightctl source-facts budget-revoke --collection-id UUID --revision REVISION
```

管理员登记默认 paused；preflight 只核对来源能力/目录，不保存阅读。持久失败修正后才 acknowledge。暂停隐藏该采集的预算快照；撤销不可逆，后续需新登记。A/B 的游标和许可不随 D 的变化重置。每份完整 source reading、原始字节/hash、目录版本及接收回执在同一事务提交；同身份相同字节幂等，内容冲突拒绝。提醒续页是独立 source reading，不能拼接旧定义或摘要。

## 受控离线接收

使用 `deploy/source-budgets-offline.authorization.json.example`。管理员填写当前精确 target/machine ID、产品版本/commit、schema36、冻结来源提交/manifest 摘要、来源完整身份、profiles、独立 collection UUID、绝对 bundle 路径/完整字节 SHA 和最长2小时有效期。授权及 bundle 均须 root 所有、0400/0600、无符号/硬链接。只支持 development/test/integration；此入口不启用网络采集。

bundle 为 `xinsight.budget-offline-bundle.v1`：`catalog` 是完整单页原始目录，`readings` 是按该目录顺序排列的对象数组，每项有 `alerts_input_cursor` 和原始 `reading`。必须完整覆盖目录；有后续提醒时包含相应新 reading，直至其 next_cursor 为空。目录及 reading 总数各不超过50，总文件不超过10,780,672字节。更大的来源须按正式在线目录/提醒分页接收，不能把不完整离线包标为完整。合法单/双/三 profile 集合都适用。

```sh
sudo xinsightctl source-facts budget-offline-register --authorization-file /absolute/budget-auth.json --target-ref TARGET
sudo xinsightctl source-facts budget-offline-enable --authorization-file /absolute/budget-auth.json --target-ref TARGET --revision REVISION
sudo xinsight-importer unirouter import-budgets --authorization-file /absolute/budget-auth.json --target-ref TARGET --check
sudo xinsight-importer unirouter import-budgets --authorization-file /absolute/budget-auth.json --target-ref TARGET
```

登记/启用由 admin 完成，导入使用 importer DSN。`--check` 使用只读事务核对当前许可、完整输入和已存身份冲突，不预约、不写事实/回执或推进位置；实际导入仍会重新核对。完整 bundle、全部 source reading、目录和离线回执在一个事务提交。重复导入仍需当前有效许可，不新增事实或移动位置。使用相同 budget-pause/revoke/status 管理；已消费后暂停或恢复库中的旧离线授权不能重新启用，须另建当前授权。页面明确显示“受控离线快照”，不称为实时来源。

## 人员阅读、冻结与比较

新增三个默认 false 的字段授权：allow_budget_definition / allow_budget_summary / allow_budget_alert。它们按 source_system_id 和明确来源项目限定；费用明细、人员与旧执行授权保持独立。API 和 CLI 每次读取重新检查当前人员/来源许可；已知 reading/member UUID 不能绕过授权。

入口 `/workbench/cost?view=budgets` 沿现有 Session/CSRF 和六主题导航。POST `/api/v1/workbench/budgets/readings` 固定接收截止、成员、解释和权限版本，GET `/{id}`、`/{id}/items`、`/{id}/items/{memberUUID}/history` 查询。历史只含父阅读截止前已接收的独立 source reading；后到修订不补入旧阅读。阅读有效期30分钟，页大小1—100，过期410、授权改变409。项目预算没有逐请求包含关系证明，拒绝 cost_projects 过滤，不能把同项目当作明细已含于全周期摘要的证明。

```sh
xinsightctl query budgets create --origin https://XI_ORIGIN --username PERSON --request-file /absolute/budget-reading.json
xinsightctl query budgets reading --origin https://XI_ORIGIN --username PERSON --reading READING_ID
xinsightctl query budgets items --origin https://XI_ORIGIN --username PERSON --reading READING_ID --limit 25
xinsightctl query budgets history --origin https://XI_ORIGIN --username PERSON --reading READING_ID --budget-ref MEMBER_UUID
```

CLI 密码只从交互终端读取，沿 HTTPS Session/CSRF/current scope，结束登出，不借用管理员凭据。请求文件只有 `contract_version: "xinsight.budget-reading.v1"` 和 filters（source_ids/source_projects/cost_projects 空数组或适用来源值）；原月周期不受近7天选择影响。

只有同一 source reading 中、当前有权查看的定义和摘要才能比较；不从旧 reading 拼补。金额保留 decimal-string 整数/currency/scale，显示舍入不改变阈值。未设置、0、none、未批准、不支持、覆盖缺口与无权分开。计划差额不是余额、预留或调用额度。提醒的触发证据、状态、投递、人工阅读和源端实际执行分别说明，XI 不更改 UR 配置、不发送外部提醒、不限制调用。

## 升级与恢复

schema35→36 保留原 A/B/I-01 事实和阅读，新增独立 D 表及默认拒绝的字段授权。恢复校验原字节/hash、授权和回执/成员绑定；实际恢复只写新库，暂停来源采集并隐藏当前 D 阅读，旧快照不可变。受控离线授权在新目标必须重新绑定，在线集合恢复须管理员确认并重新获取当前目录。

原扣费预算与企业项目分摊片段按来源原义保留。正式计算费用预算 profile 和独立供应商账单来源仍缺失，对应正向目标未完成；不能改名现有金额或新建模拟账单来代替。
