# 执行调查工作流

## 登录与 Session

只从已批准的单一内部 HTTPS origin 打开 X-Insight。登录表单只接受管理员离线分配的
username/password；浏览器和服务端审计都不应显示密码、hash、Session cookie 或 CSRF token。
登录成功会建立新的产品 Session，并使同一浏览器提交的旧 Session 失效。

Session 的 idle timeout 为 30 分钟、absolute timeout 为 8 小时；服务进程重启会使全部
Session 失效。结束调查时使用产品内“退出登录”，不要只关闭标签页。`investigator` 只能访问
Evidence；`operator` 可在“会话管理”查看最小化 Session metadata，并按非 secret
`session_ref` 撤销 Session。该页面不是账户、密码、角色、数据库或 worker 管理界面。

登录页若运行在 HTTP、跨 origin、证书/hostname 未验证的入口，必须停止；不要关闭浏览器
cookie 安全属性，也不要把 SSH HTTP tunnel 或代理登录页当作 X-Insight 产品登录。

## 导航与页面层级

桌面左侧导航按“总览”“使用与目的”“运行与执行”“安全与复核”“费用与使用”“来源与覆盖”组织。
当前角色与字段授权决定实际可见入口；无费用权限的运行读者不会因此获得费用页。窄屏使用页首“菜单”按钮打开抽屉；选择页面后抽屉自动关闭。
不要把当前登录角色、`按当前授权范围展示资料` 或“来源/导入/投影/因果”边界当成可关闭的装饰。

“运行态势”不是 MaaS 指标副本。先固定时间与来源范围，再从开放 Alert、Evidence Gap、四个独立
覆盖口径和来源声明的 Runtime Path 下钻。execution/Gap/coverage 使用固定 Evidence 快照；
Connector trust/checkpoint 和 Case/Saved View 是当前工作区状态。Alert 的 observed 时间受窗口约束，
但当前 state 不等于历史状态重建。没有关联 execution 时应进入 Discovery 或关联 Case，不得用另一
条 execution 的 Trace 补位。

## 数据准备

首次产品演示优先使用仓库 fixture。真实现场元数据须经客户批准；正文读取默认关闭，仅在来源显式启用、绑定核验通过且当前人员具备范围及 human_review 用途授权时开放。元数据许可不自动授予正文权限。UniRouter/LiteLLM pull workers 虽已作为独立 dormant service 打包，但没有当前
Target activation receipt 时不得启动；受控 sidecar import 仍须走下述 manifest/preflight。
OpenInference 只允许由产品 owner/Codex 在命名非生产目标执行 exact bounded-offline import。
每次必须有 root-owned、最多两小时、绑定 target/build/bundle/operation/scope 的授权 manifest；
bundle 必须是 metadata-only 且不含 prompt/response attribute。没有该授权时，命令在 bundle、
credential 和数据库之前 fail closed。它不是 live collector，也不授权 Feed 或 Production。

内部真实开发数据或客户批准的 sanitized metadata 在任何持久化前，必须先运行不写数据库的
限定预检：

```text
/opt/x-insight/current/bin/xinsight-importer unirouter preflight-sanitized \
  --snapshot /approved/path/sanitized-snapshot.json \
  --approval-manifest /approved/path/approval-manifest.json \
  --max-request-ids 20 \
  --max-events 200
```

只有 `status=pass` 才能进入后续导入。预检会读取并计算真实 approval manifest 的 SHA-256，
核对 snapshot v2 中声明的 hash、source instance、批准 request/scenario、批次上限、每个 request
的 org-bound source row、单一 org、counts、artifact hash、source ref 与 payload policy，并对
明显的 email、phone、IP、URL、credential 和未批准自由文本输出不含 metadata key 或 artifact
字段原值的固定 finding path/code。JSON report 不保存输入路径；文件系统失败的 stderr 可能
包含 operator 提供的精确路径，只能留在私有执行日志。它是额外的 fail-closed 护栏，不等于
通用 DLP，也不能替代精确数据授权和私下逐字段复核。`source_event_hash` 仅检查为
producer-declared 64-hex；X-Insight 不把它表述成本地重算结果。

```text
sudo env XINSIGHT_IMPORTER_POSTGRES_DSN_FILE=/etc/x-insight/database/current/postgres-importer.dsn \
  /opt/x-insight/current/bin/xinsight-importer unirouter gate-a-check \
  --snapshot /approved/path/sanitized-snapshot.json \
  --approval-manifest /approved/path/approval-manifest.json \
  --schema /opt/x-insight/current/schemas/m0/evidence-event.schema.json \
  --receipt-schema /opt/x-insight/current/schemas/integration/import-receipt.schema.json \
  --persist \
  --receipt-output /approved/path/import-receipt.json
```

输入与 receipt 路径必须是逐一批准的明确路径。重复导入相同 snapshot 应保持幂等；失败或
quarantine 不能被描述成已持久化成功。`--receipt-output` 只属于 `gate-a-check`；该路径必须
尚不存在，工具拒绝覆盖既有 receipt。

sanitized batch 的 `gate-a-check` 必须携带同一 manifest。工具会在同一进程先完成预检，再将
实际 import bytes 的 SHA-256 与预检 hash 比较；两次读取间发生任何 byte 变化都会在持久化前
停止。省略 manifest 的既有兼容路径不能用于本流程，也不能产生 internal-real 验证结论。
只有本次实际 manifest preflight 通过，持久化 provenance 才能标为 `operator_approved`；snapshot
自报非空 hash/exchange ID 不足以提升证明等级。

## 搜索 execution

在 `/executions` 使用 request ID、request ID prefix、source、outcome、has gaps 和时间范围。
默认窗口为服务端 `as_of` 向前 24 小时，最大跨度 31 天，每页最多 100。搜索结果显示 proof
class、freshness、事件数和 gap 数；它们是固定 `as_of` 下的视图。

Saved View 创建位于折叠的次级动作中，只保存筛选与呈现方式。结果在紧凑桌面或移动宽度会从表格
改为带字段标签的卡片，但 source、outcome、Evidence 状态、payload 和 Gap 语义不变；无需横向
拖动页面才能读取完整记录。`0 无已记录缺口` 是当前快照的中性陈述，不是来源完整性保证。

如果没有结果：先核对时间范围、source 和 proof class，再核对 import receipt 与 readiness。
不要假设“无结果”表示 UniRouter 没有执行。

## 阅读 Story、DAG 与 Chronology

先读 outcome-first Story：谁/哪个系统发起、做了什么、来源明确声明经过哪些节点、在哪里结束，
以及哪些结论受 Gap 限制。再检查 causal DAG；边只能来自 API `edges[]` 与来源 parent 声明，
unresolved parent/orphan 必须可见。Chronology 只回答时间顺序，不把相邻 node 画成因果。

Story 中“发起者—动作—经过系统—终点”四步摘要只使用 Evidence identity、request identity 和
来源声明父子边。如果发起者、完整路径或终态关系未声明，对应步骤显示未知/未解析；不要把该提示
解释为系统故障。桌面 DAG 使用完整内容宽度，小屏使用等价的顺序关系列表，不会为了塞入 SVG
而缩小到不可读。两种呈现都选择同一个 Evidence node，并驱动同一详情面板。

若来源提供同一 exact source/org/project/trace 的多个 Span，`Trace Journey` 会跨 M0 execution
显示来源声明路径。每个节点仍保留自己的 execution/event/hash；点击跨 execution 节点会进入它
自己的 fixed `as_of` Evidence。只有唯一可解析 `parent_span_id` 才连边；同 trace、时间接近或
相同模型都不足以证明因果；未解析节点之间显示断点而不是箭头，来源声明形成 cycle 时完整性
失败。`source_timeline_complete=false` 表示 bounded export 外仍可能有未知 Span，即使可见 parent
全部解析也不能写成完整业务链路。

四个 completeness 轴分别是 Source、Import、Projection、Causal。任一为 partial/unknown 时，
不能用其他轴的 current 状态替代。freshness、Cursor、high-watermark、retry 与 ingestion time
属于运行状态，不进入 M0 hash 或 event-linked provenance。

四轴完整且没有记录 Gap 时，“证据覆盖与完整性”默认折叠，只显示阶段计数和四轴结论；存在 Gap
或任一轴未闭合时默认展开。折叠不删除证据，也不扩大完整性声明。

## 打开 detail 与 timeline

选择 execution 后先看摘要：request/source/outcome、事件/gap 数、payload capture mode 和
freshness。timeline 按 `effective_at` 稳定排序；时间异常与 unresolved declared parent 单独标记。
相邻 node 不是自动因果。

分页继续使用服务端返回的 opaque cursor 和同一个 `as_of`。cursor 过期时回到搜索重新建立
快照，不拼接或修改 cursor。

## 检查 selected node

选择一个 timeline node，依次核对：

1. event type、stage、severity 和多个时间；
2. correlation、identity、capability、decision、usage、outcome、payload policy；
3. 每个 field group 的 provenance 标签；
4. source system/version、schema、source event/ref 和 hashes；
5. declared parent 是否 resolved，以及是否存在对应 capture gap。

执行的内容页先显示已授权清单；用户选择“读取并核验正文”后，服务端重查当前权限和准确来源绑定，核验保存字节的长度及摘要后展示。打开事件节点不会自动读取正文。无权、未采集、过期、删除或完整性失败分别显示状态，不用缺失正文推断执行事实；撤权后不能沿用旧阅读或迟到响应继续显示正文。

Case 引用卡片默认显示缩短的技术引用；展开后才能查看并复制完整 event hash。缩短只改变视觉
层级，不改变 seal 输入、citation snapshot 或 Evidence identity。

## 写结论

调查记录至少写 execution ID、固定 `as_of`、source/proof class、关键 event IDs、provenance、
gap/freshness、观察事实和未知项。结论用“证据显示”“来源声明”“X-Insight 派生”“当前未知”
区分，不把未解析 parent 或缺失 node 写成已证实串谋、故障原因或策略绕过。

## Case、Finding 与 Evidence Pack

把需要共同解释的 execution 加入 Case，再创建 Finding。每个 Finding 明确选择
`evidence_backed`、`deterministic_derived`、`hypothesis` 或 `unknown`，并逐条关联支持、反驳、
上下文 Evidence 和限制/阻断 Gap。Case-level 引用与 Finding-level 引用不同，不应混用。

封存前核对 title/statement、relation、Evidence ID/hash snapshot、Gap affected claim、Annotation
和 Alert link。seal 后生成固定 revision/digest；JSON 与 print-first HTML 应按相同 snapshot
导出。用户输入会原样进入 pack，禁止粘贴 raw prompt、credential 或未经批准的来源内容。

“添加 Finding”默认折叠，避免编辑控件与已保存结论竞争。展开编辑器仍必须选择声明分类；产品
不会把手工陈述静默提升为 `evidence_backed` 或 `deterministic_derived`。

## Session、Compare 与 Saved View

只有来源提供稳定 Session/Thread identity 时才展示 Session；时间接近不创建会话。Compare 必须
使用同一固定 `as_of`，版本/用量/时延/完整性字段中的 `Missing`、`Partial` 与 `0` 分开解释。
Saved View 只保存有界 filters 与 presentation，不保存结果快照；重新打开时仍要核对新的
`as_of` 与 freshness。

## Connector 信任与交付状态

Connector 页面分三层读取：来源契约（source、Manifest、Fidelity、payload、idempotency）、
交付运行状态（worker、import、page/commit/duplicate-noop）和 Checkpoint 摘要（只显示 digest/
是否存在，不暴露可变 cursor）。来源契约可兼容不代表 worker 正在运行；worker degraded 也不
改写历史 M0 Evidence。Cursor、high-watermark、page time、retry 与 ingestion time 只属于
checkpoint/status 或非事件关联回执。

## Discovery 假设

Coverage、process、topic、anomaly、coalition 与 authority-drift 输出只用于发现调查方向。先看
method version、sample/minimum、support score、Evidence/execution refs、limitations；样本不足时
必须显示 abstention。调查员可以把候选写入 Finding，但必须保留其假设分类，不能自动关闭 Gap、
改变路由或触发 enforcement。
