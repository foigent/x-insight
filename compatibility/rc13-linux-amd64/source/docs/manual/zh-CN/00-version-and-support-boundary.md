# 版本与支持边界

## 版本身份

| 项目 | 当前值 |
| --- | --- |
| 产品版本 | `0.3.0-rc.13` |
| 手册版本 | `0.3.0-rc.13/manual-r13` |
| 数据库 schema | `23` |
| 配置 schema | `xinsight.config.v1` |
| Investigation API | `xinsight.investigation.v1` |
| Access API | `xinsight.access.v1` |
| Evidence schema | `m0.1` |
| Import receipt | `xinsight.import-receipt.v1`、`foigent.operational-receipt.v2`、`xinsight.unirouter-campaign-package-import-receipt.v1` |
| 发布渠道 | `release_candidate` |

准确 commit、构建时间、Go 工具链和手册版本以每个二进制的
`version --json` 及签名 manifest 为准，不从文件名猜测。

## 唯一受支持现场形态

- Ubuntu 22.04 LTS、Linux `amd64`/x86_64、systemd。
- 同一私有主机上的独立 PostgreSQL 16 Compose，数据库仅监听 `127.0.0.1:5432`。
- PostgreSQL 数据根固定为 `/data/x-insight/postgres`，容器内 `PGDATA` 固定为
  `/var/lib/postgresql/data`；不得把已有根目录当成新的 `pgdata/` 子目录重新初始化。
- `xinsight-server` 由 systemd 管理，保持 reader-only 且仅监听 `127.0.0.1:8080`。
- `xinsight-connector-controller.service` 以 root 运行，只从独立 admin DSN systemd credential
  读取数据库 authority，并在固定 `/run/x-insight/connector-controller.sock` 提供
  `root:xinsight`、`0660` 的 typed wake/inspect 边界；Web 不获得 admin DSN、systemd、SQL、shell、
  任意 path/unit/URL 能力。该服务必须在 `xinsight.service` 前启动。
- 受管 installer 以 root-only `0600` generate-once 创建独立
  `/etc/x-insight/xinsight-connector-id.env`；upgrade/rollback/re-upgrade 原字节保留同一 stable
  deployment ID。受支持的 root CLI 只在另一个 `/etc/x-insight/xinsight-connector.env` 写入规范
  HTTPS UniRouter install origin，不能改写 identity。只有 target 与独立 TLS 配置中的 X-Insight
  external origin 同时有效时，Connector invitation 才启用；仅有 ID 或 ID+TLS 均保持 disabled。
- `xinsight-server` 还必须读取独立的 root-provisioned
  `/etc/x-insight/access-accounts.json` systemd credential；它只含严格
  `xinsight.local-accounts.v1`、稳定 subject、规范化 username、角色、disabled 状态和
  Argon2id hash，不含明文密码。
- 独立 `xinsight-feed.service` 使用 importer role，从一个固定的 UniRouter 私有
  completed-execution endpoint 拉取 metadata；它不开放入站 listener，也不复用 server role。
- feed token 由 X-Insight target 本地生成；UniRouter 只登记 Frozen Registration Bundle V1
  的 prefix 与 verifier，不接收 raw token。credential prepare/activate/rotation 只能由
  root `xinsightctl feed-credential` 生命周期完成。
- 新联调 campaign 不得手工删除或覆盖旧 Cursor。root 只能在 worker 已存在、inactive、disabled，
  且数据库状态为无错误的 Cursor=high-watermark 边界时使用 `xinsightctl connector-epoch`；旧 epoch
  完整归档且不可变，新 epoch 原子激活后仍不会自动启用或启动 worker。
- 独立 `xinsight-litellm-feed.service` 使用同一 importer role、独立 LiteLLM API key 和独立
  source/adapter/cursor identity，从准确私有 HTTPS `/spend/logs/v2` 拉取达到
  start-time eligibility fence 的 metadata；这不是 row `endTime` terminal finality
  声明。unit 缺省 dormant，不与 completed-execution worker 共用 config、credential 或
  checkpoint，也不打包 LiteLLM source infrastructure。
- Investigation UI/API 由 X-Insight 本身的 Session/RBAC 保护；`investigator` 只有
  `evidence.read`，`operator` 另有 Session 列表与撤销权限。Web 服务只获得 reader 与受限
  workspace 数据库角色；workspace 只能写产品本地调查记录，不能修改 Evidence、Connector
  状态或 migration ledger。Web 服务没有 importer、admin、账户管理或运行时控制权限。
- 认证状态变更只接受同源 HTTPS。当前版本支持 direct TLS，或显式启用的同机 loopback TLS
  terminator：`XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback` 与唯一
  `XINSIGHT_EXTERNAL_ORIGIN=https://HOST_OR_IP` 必须同时存在；代理必须覆盖并固定
  `X-Forwarded-Proto`/`X-Forwarded-Host`，应用继续只监听 loopback。HTTP tunnel、代理登录页
  或未固定 origin 的 forwarded header 不能冒充产品认证证明。
- 不是完全 air-gapped：发布包、信任根和已固定 PostgreSQL image 可经批准的私有路径传入。
- 只使用 fixture/synthetic、严格 live contract metadata 或获得批准的历史 sanitized
  metadata；不保存 raw prompt/response。
- 第一客户为 `owner_supervised_trial`：产品 owner 与 Codex 执行所有安装/升级；一次性签名私钥不交付、不保留，客户不能自助升级。这不是持久商业发行权威。

其他 Linux、ARM、外部 PostgreSQL、Kubernetes、HA/DR 或完整 air-gap 不能由上述结果推导，
需要独立资格验证。

## 状态词不能互换

| 状态 | 只证明什么 |
| --- | --- |
| Local | 当前开发工作区内的测试或演练通过 |
| CI | GitHub CI 的冻结 runner、工具链和服务矩阵通过 |
| Target | 指定非生产现场主机上的准确 artifact 和路径通过 |
| External integration | 与准确外部组件和 versioned contract 的联调通过 |
| Production | 生产边界、负载、安全、运行和恢复要求通过 |

本候选的 Target、External integration 和 Production 状态以随版本发布说明及现场 receipt
为准。没有记录就是未验证，不能以 Local/CI 替代。

## 明确不支持

- X-Insight 应用或 PostgreSQL 直接公网监听、多租户 SaaS、Production verified、零停机或
  自动跨大版本 PostgreSQL 升级。经批准的同机 HTTPS terminator 只能代理 reader-only 产品
  UI/API，不能代理数据库、importer、feed、admin 或主机控制面。
- New API、one-api、policy simulation、通用 durable import jobs、public collectors、第三方
  代码插件运行时。当前版本保留 completed-execution、LiteLLM spend-log 与 UniRouter
  Execution Evidence V2 三个已验证运行时注册；OpenInference 只允许 root-owned、短时、绑定
  exact target/build/bundle/operation 的非生产 bounded-offline metadata 导入。没有该 manifest、
  任一绑定漂移或任何 listener/feed/Cursor/Checkpoint 扩权都 fail closed；它不是通用插件运行时。
- raw payload storage、自动 receipt transport、自动 exporter/outbox、durable deep link。
- pre-billing rejection、late revision、public POST collector/push、direct UniRouter DB、
  CDC、customer backfill，以及把 approval-manifest/artifact 当作 live transport。
- 多 worker HA/leader election；V1 只支持一个受管 pull worker。
- 从 UI 执行 root 权限安装或升级。
- OIDC/SSO、可信代理身份 assertion、X-IAM runtime、Web 创建账户/重置密码/改角色、
  persistent Session 或多节点 Session 共享。当前只保留未来 provider seam。
- 第二客户、第三方安装或无人值守/自助升级；这些场景必须先决定持久 KMS/HSM、keyless 或 offline 发布权威。
