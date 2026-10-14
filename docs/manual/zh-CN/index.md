# 手册使用说明

本手册只适用于与其一同签名发布的 X-Insight `0.3.0-rc.13`，手册版本为
`0.3.0-rc.13/manual-r13`。先核对发布 manifest、二进制 `version --json`、
兼容矩阵和本页页脚中的版本；任一不一致都应停止安装或升级。

> 当前版本是内部、单部署租户的现场候选，不是公网、多租户或 Production verified 产品。

第一客户采用 `owner_supervised_trial`。你不需要理解或保管签名私钥：产品 owner 只确认版本和
现场窗口，Codex 负责从准确 GitHub `main`/CI 生成、核对、传输、安装和保存 receipt。客户不
能自行升级；第二客户或自助升级前会另行建立正式发布权威。

管理员先阅读“版本与支持边界”“安装与首次启动”“配置、安全与访问”和
“升级、回滚与恢复”。调查操作员可从“执行调查工作流”开始。稳定协议名、
JSON 字段、CLI 参数和 error code 保留英文，以便与实际输出逐字对应。

## 三条操作原则

- 不把 fixture/synthetic 结果称作真实生产证明；批准的 sanitized metadata 也不等于 raw 数据。
- 不从缺失事件推断未观察到的事实；证据不足时显示 `gap` 或 `unknown`。
- 不用递归、通配符或批量删除处理试用数据、备份、receipt 或 evidence。

## 当前数据进入方式

当前生产意图的数据路径是独立 `xinsight-feed.service` 通过固定私有 HTTP endpoint 拉取
`unirouter.completed-execution.v1`。worker 默认不启用；只有写入当前 schema 23、配置私有
endpoint、用 target-local `xinsightctl feed-credential` 生成并原子激活 feed-only token、
绑定 importer credential，并完成受控 activation 后才可启动。raw token 不离开 X target；
UniRouter 只登记严格 public verifier bundle。worker 按 at-least-once 逐项幂等提交，整页
成功后推进 durable cursor，不在 UniRouter 请求热路径上。fixture 与历史 sanitized snapshot
sidecar 仍可用于明确的测试/调查，不是 live transport。

第二轮或后续 campaign 不复用第一轮 delivery epoch。schema 22 的 root-only
`xinsightctl connector-epoch` 在 worker inactive+disabled、Cursor=high-watermark 且无错误时，
先生成 SHA-256 绑定计划，再在同一数据库事务中归档旧 checkpoint epoch 并激活新
source instance/Cursor。可变交付状态仍不进入 M0 identity/hash 或 Event-linked provenance；
epoch 激活也不会自动启动 Feed。

rc.11 还提供受约束的离线 `unirouter import-campaign-package` 路径，用于已经预授权、
content-addressed 的 Source-evidence package。它先验证准确的 X 签名发布 manifest、运行中
importer 字节、产品 scope/verification receipt、Neutral N1 授权、purpose-separated Source trust
和不可变 producer package，之后才加载 importer 数据库绑定。该路径没有 endpoint、worker、
Cursor、Checkpoint 或 credential 输入，也不会把带 `runtime_close_marker_unavailable` 的 partial
member 声称为 complete。

rc.12 引入且当前版本保留 `xinsight-connector-controller.service`：UniRouter 继续调用公开 HTTPS
redeem/poll/result API，Web 只在 result durable commit 后通过固定 root:xinsight Unix socket
发送 typed wake。controller 使用独立 admin DSN credential，并在 Web 之前启动；它不接受任意
path、unit、SQL、shell、URL 或 DSN。worker 仍只在 phase-3 active authority 的 fresh readback 后最后启动。

rc.12 引入且当前版本保留第二个独立、缺省 dormant 的
`xinsight-litellm-feed.service`，从准确私有
HTTPS `/spend/logs/v2` 读取达到 start-time eligibility fence 的 LiteLLM spend-log
metadata；这不声明 row `endTime` terminal finality。它使用独立六字段非 secret config、
独立的非管理员 team observer API key（团队允许 `/spend/logs`，key 只允许
`/spend/logs/v2`）、独立 product registry identity 和独立 durable checkpoint，不修改
completed-execution worker 或 Read API。首次启用、启停、永久错误恢复、key
rotation 和 rc.11 rollback 的准确步骤见“配置、安全与访问”；没有 Target/External 证明时
不能把 Local/CI 实现称为已联调或 Production。

## 当前访问方式

rc.13 新增、当前版本保留 X-Insight 自有的本地账户、内存 Session、RBAC 与最小化审计。所有调查 UI/API
必须先建立产品 Session；`investigator` 可读 Evidence，`operator` 还可查看和撤销 Session。
账户只由 root 通过 systemd credential 离线配置，产品不提供 Web 建号、改密码或改角色。

Session cookie 固定为 `__Host-xinsight_session`，带 `Secure`、`HttpOnly`、
`SameSite=Strict` 和 `Path=/`，无 `Domain`；状态变更还要求同源 HTTPS 与
Session-bound CSRF。当前版本支持一个明确 opt-in 的同机 loopback TLS terminator 合同：固定
`XINSIGHT_EXTERNAL_ORIGIN`，只信任来自 loopback 的准确 forwarded proto/host，并继续让
应用只监听 `127.0.0.1:8080`。Target 登录仍必须逐次证明证书、Host/Origin、Secure cookie、
匿名 303/401、authenticated readback 与公网只开放 80/443；HTTP SSH tunnel、代理自身登录页
或 loopback HTTP 都不是 Target 登录证明。

## 当前调查工作台

schema 16–20 的 Case、Finding、Evidence/Gap citation、sealed Evidence Pack、Saved View、
来源声明的 Session、Compare、Feedback、Alert-to-Case、版本维度、Gap resolution 和
Known/Unknown Discovery 都是产品本地调查对象。它们引用 M0 Evidence 的稳定 ID/hash，不会
改写来源事实；Discovery 输出始终是带 evidence refs、limitations 和 abstention 的假设，不会
自动执行策略或关闭 Gap。

当前调查界面按“运行态势—调查—分析—信任与访问”组织。`/inbox` 的运行态势使用同一组固定
`as_of/from/to` 聚合 Alert、execution、Gap、四个独立覆盖口径和来源声明的 Runtime Path；不会从
Execution 搜索第一页制造全局趋势，也不复制 MaaS 控制台的运营指标。Alert 按 observed 窗口筛选并
读取当前状态，不声称重建历史状态；未关联 execution 的 Alert 不会借用其他 Trace。Connector 当前
trust/checkpoint 与固定 Evidence 统计分开显示，前者不进入事件 hash。

Execution Story 首屏按“发起者—动作—经过系统—终点”展示来源可证明的路径；没有来源 identity 或
declared parent 时明确显示未知，不会用时间相邻补线。桌面因果图和小屏关系列表表达同一组
`edges[]`。完整覆盖、语义说明、Saved View 和 Finding 编辑采用渐进披露；记录了 Gap 或完整性未
闭合时仍默认展开。Connector 页面把来源契约、可变交付运行状态和 Checkpoint 摘要分开，后两者
不冒充 Evidence。

产品仓已固定不可变 Foigent Protocol `v2.0.2` 用于共享 CI 与
source-neutral 合同验证。该 PATCH 使远程 composite Action 可在无 `.git`、无权威仓历史、
无产品侧私有仓凭据的 snapshot 中运行；Event/Connector Schema、vectors、reports、投影输出
和 M0 identity/hash 相对 `v2.0.1` 保持不变。现有 UniRouter/LiteLLM worker 仍使用各自已验证
的 v1 runtime binding。
OpenInference 已有 metadata-only bounded-offline 解析/投影、不可变 completed operational
receipt 与离线导入命令。rc.5 只增加 exact root-owned expiring non-production authorization：
它绑定 target machine ID、release commit/version、bundle path/SHA、operation、producer、scope
和最多 10 条 record；没有授权会在 bundle、credential、数据库之前 fail closed。它不启用
production bootstrap、入站 OTLP listener、Feed、Cursor 或 Checkpoint。Investigation 新增
`Trace Journey` 只读投影，在不改变 M0 request/event/hash 的前提下，以 exact source/scope/trace
跨 execution 展示来源声明的父子 Span 路径。

## 需要保留的证据

每次安装、升级、回滚、恢复和现场调查都应保留：准确 release/commit、manifest 与
artifact SHA-256、bootstrap/trust-store SHA-256、GitHub workflow/CI、签名 key ID、
`xinsightctl` receipt、兼容性/监听检查、使用的数据
proof class，以及 Local、CI、Target、External integration、Production 各自状态。
