# 配置、安全与访问

## 配置与 secret 边界

`/etc/x-insight/xinsight.env` 只允许 `XINSIGHT_MIGRATIONS_DIR=/opt/x-insight/current/migrations`。
Reader、Workspace、Importer 和 Admin DSN 分别保存在 `/etc/x-insight/database/sets/...` 的 root-only
文件中；`current` 符号链接原子选择当前 credential set。systemd 只通过
`LoadCredential` 把 reader 与最小权限 workspace DSN 交给服务。Web 服务用 reader 查询
Evidence，用 workspace 写入 Case/Finding/Annotation/Gap resolution 等产品本地调查记录；
workspace 角色不能修改 Evidence identity/hash、Connector 交付状态或迁移账本。Web 服务不得
获得 importer/admin DSN、feed key 或 Source key。

### One-click Connector target 配置

受管安装会 generate-once 创建 `/etc/x-insight/xinsight-connector-id.env`，mode 为 `0600`，并在
install/upgrade/re-upgrade 中校验后原字节保留。该 installer-owned identity 文件只允许一个 key：

```text
XINSIGHT_CONNECTOR_DEPLOYMENT_ID=xid_INSTALLER_GENERATED_22_BASE64URL_CHARS
```

受支持的 root CLI 只在另一个 root-only `/etc/x-insight/xinsight-connector.env` 原子写入一个 target
key：

```text
XINSIGHT_UNIROUTER_INSTALL_ORIGIN=https://APPROVED_UNIROUTER_ORIGIN
```

不要创建、复制或编辑 identity 文件。deployment ID 不是 bearer credential，但它是
installation/replay 绑定的稳定部署身份；不得按发布版本、服务重启或 Connector 重试轮换。
`deploy/xinsight-connector.env.example` 只说明 target 形状，不是待复制到 `/etc` 的配置。UniRouter
origin 必须是规范化 HTTPS origin：无 userinfo、path、query 或 fragment。

完整启用条件分布在三个 root-owned 配置面：

- `/etc/x-insight/xinsight-connector-id.env` 只提供 installer 生成的稳定 deployment ID；
- `/etc/x-insight/xinsight-connector.env` 只提供 CLI 设置的 UniRouter install origin；
- `/etc/x-insight/trusted-tls-proxy.env` 提供准确
  `XINSIGHT_EXTERNAL_ORIGIN=https://APPROVED_XINSIGHT_ORIGIN`，并按本章网络边界启用可信同机 TLS
  terminator。

`xinsight.service` 依次加载 TLS、identity 与 target 文件。UniRouter install origin 缺失时，即使
stable ID 已生成且已经配置 TLS/external origin，Connector 仍明确 disabled，Web 可正常启动且不会
产生 Connector 流量。一旦 target 文件存在，三项必须同时有效；缺失 ID、缺失 X-Insight external
origin 或任一 origin 非规范 HTTPS 都会 fail closed 并阻止服务启动。配置文件本身不会
activate/enable/start Feed worker；worker 仍只能在独立三阶段 authority 的最后一步启动。

受支持的应用顺序如下；先完成并核对本章后述的 trusted TLS 文件，再由准确 current release CLI
验证稳定 ID/owner/mode/key allowlist 与 TLS external origin，最后原子设置 target：

```text
sudo /opt/x-insight/current/bin/xinsightctl connector-target-config validate
sudo /opt/x-insight/current/bin/xinsightctl connector-target-config set \
  --unirouter-origin https://APPROVED_UNIROUTER_ORIGIN
sudo /opt/x-insight/current/bin/xinsightctl connector-target-config validate
sudo systemctl restart xinsight.service
```

不要用编辑器、`sed`、重定向或复制模板改写 managed Connector 文件。`set` 只接受规范 HTTPS
origin，只更新 target 文件，无法写入 identity key；写入使用完成 sync 的 staging 与原子 rename，
不会留下半文件覆盖当前 target。每次 Web 启动还会通过 root-only、clean-environment
`ExecStartPre` 再次校验三个文件的 owner/mode/key、deployment ID 形状以及 full target 的 TLS 前件；
任一漂移都会在 Web 进程前失败。上述 restart 是独立现场授权动作；仅写配置仍不启动 worker 或发送
流量。

## 本地账户、Session 与 RBAC

当前版本的账户 authority 是一个 root-provisioned、只读的 systemd credential：
`/etc/x-insight/access-accounts.json`。`xinsight.service` 只以
`LoadCredential=access-accounts.json:/etc/x-insight/access-accounts.json` 读取它；不允许
把账户 JSON、password 或 hash 放进 `xinsight.env`、其他 environment、argv、仓库、
receipt、日志或工单。

文件必须是 root 或实际 service EUID 所有的 regular、single-link、非 symlink 文件，
mode 只能是 `0400` 或 `0600`。大小上限 64 KiB、账户上限 100；服务启动时会在 open
前后复核 identity/metadata，并对 duplicate key、unknown field、trailing JSON、重复 subject、
重复规范化 username 或不合规 hash fail closed。没有默认账户；空账户集也会拒绝启动。

严格形状如下；占位 hash 不能直接使用：

```json
{
  "schema_version": "xinsight.local-accounts.v1",
  "accounts": [
    {
      "subject_id": "xs_REPLACE_WITH_32_LOWER_HEX",
      "username": "operator.one",
      "role": "operator",
      "disabled": false,
      "password_hash": "$argon2id$v=19$m=19456,t=2,p=1$REPLACE_WITH_SALT$REPLACE_WITH_HASH"
    }
  ]
}
```

`subject_id` 必须匹配 `xs_[a-z0-9]{16,64}`，且以后不因 username 变化而复用给另一身份。
username 必须是已经 trim/lowercase 的 3..64 字符 ASCII
`[a-z0-9._-]`；角色只允许 `investigator|operator|access_operator|platform_admin`。每个账户的五个字段都必须显式存在；
`disabled` 必须是 JSON boolean，缺失、`null` 或字符串都会 fail closed，不能用隐含默认值。
密码 hash 只接受准确 Argon2id v19：
memory `19456` KiB、iterations `2`、parallelism `1`、16-byte salt、32-byte key。

离线 root workflow：

```text
sudo /opt/x-insight/current/bin/xinsightctl access generate-subject
sudo /opt/x-insight/current/bin/xinsightctl access hash-password
sudo /opt/x-insight/current/bin/xinsightctl access validate \
  --file /etc/x-insight/access-accounts.json
```

`hash-password` 只在交互 TTY 隐藏读取两次 password；stdin/pipe、flag、environment 和
非交互输入都会拒绝。密码最少 12 bytes、最多 1024 bytes。命令只输出 Argon2id PHC
string；该 hash 仍是敏感配置材料，只能在同一目标主机的 root-only 编辑流程中处理，不得进入
shell history、command substitution、共享 clipboard/transcript、截图、日志或聊天。
`validate` 只输出 schema 与角色/disabled 计数，不输出 username、subject 或 hash。

配置变更只能由另行授权的 root operator 原子写入并复核 owner/mode/regular/nlink 后重启
服务；产品没有 Web 建号、password reset、改角色或写回文件能力。`disabled=true` 会阻止新
登录，但既有内存 Session 必须通过 restart 或 operator revoke 失效；因此禁用账户的受控
procedure 应同时处理既有 Session。

产品 Session：

- cookie value 为 256-bit 随机 opaque token，服务端只以 SHA-256 digest 作 key；
- cookie 固定名 `__Host-xinsight_session`，`Secure`、`HttpOnly`、
  `SameSite=Strict`、`Path=/`、无 `Domain`；
- 每次成功登录都轮换 cookie 并撤销请求携带的旧 Session；
- idle timeout 30 分钟、absolute timeout 8 小时、每 subject 最多 5 个、单进程最多 500 个；
- Session 只在内存中，进程重启全部失效；logout 与 operator revoke 会立即撤销；
- 所有认证状态变更要求单一同源 HTTPS `Origin`、同源 fetch、严格 JSON，以及
  Session-bound `X-CSRF-Token`。

登录失败对 unknown username、错误 password 与 disabled account 使用同一响应。默认门禁为
每个规范化 username 在 15 分钟窗口内最多 5 次失败、全进程在 1 分钟窗口内最多 50 次失败；
另有独立的 Argon2id verification admission，默认最多 4 个并发。达到任一边界都在进入新的
password verification 前返回 `429 authentication_rate_limited`；这与调查 API 的 32 并发
上限相互独立，不能用扩大通用 API 容量替代 password verification 的内存边界。

角色边界：

| 角色 | 权限 |
| --- | --- |
| `investigator` | `evidence.read`、`evidence.cite`、`investigation.write` |
| `operator` | `evidence.read`、`evidence.cite`、`investigation.write`、`access.sessions.read`、`access.sessions.revoke`、`connector.installations.manage` |
| `access_operator` | `evidence.read`、`evidence.cite`、`investigation.write`、`access.sessions.read`、`access.sessions.revoke` |
| `platform_admin` | `evidence.read`、`evidence.cite`、`investigation.write`、`access.sessions.read`、`access.sessions.revoke`、`service_identities.read`、`service_identities.revoke`、`connector.installations.manage` |

只有 `operator` 与 `platform_admin` 具有 `connector.installations.manage`；创建 Connector invitation
还要求同源 HTTPS 与 Session-bound CSRF mutation authorization。四个角色都没有 migration、
importer/feed writer、DB admin、Source credential、runtime
启停、Evidence 写/删或账户管理权限。Access audit 只记录 event/time、已知 stable subject、
role、permission、result 和 correlation；不得包含 raw unknown username、IP、password/hash、
Session/CSRF、DSN/key、request body 或 Evidence 内容。

`/etc/x-insight/xinsight-feed.env` 允许原有三个非 secret 值和一个可选的映射模式：

```text
XINSIGHT_UNIROUTER_FEED_ENDPOINT=https://PRIVATE-UNIROUTER-HOST/api/v1/integrations/xinsight/completed-executions
XINSIGHT_UNIROUTER_FEED_POLL_INTERVAL=5s
XINSIGHT_UNIROUTER_FEED_ALLOW_PRIVATE_TUNNEL_HTTP=false
XINSIGHT_UNIROUTER_FEED_MAPPING_MODE=legacy
```

映射模式缺省或设为 `legacy` 时只执行现有 direct DTO→M0 路径，且不会初始化共享
Reference projector。`shadow` 仍只由 legacy 路径写入 EvidenceStore，并在内存中执行
Reference Envelope→M0 精确比较；任何 mismatch、projection error 或比较遥测输出失败都
不得阻断 legacy 收敛和正常 cursor 推进，但 mismatch/error 会令当前 worker 进程的
promotion eligibility 保持 false，直到进程重启。`neutral_canary` 先比较两条路径，只有
精确一致时由 neutral 路径作为唯一 writer；mismatch/error 在任何 item 写入和 page
checkpoint 之前 permanent fail closed。三种模式共用同一 HTTP feed、worker、
EvidenceStore、durable cursor/high-watermark 和 credential；切换模式不重置或重写任何状态。

`xinsight-feed.service` 通过 systemd credentials 单独读取
`postgres-importer.dsn` 与 `unirouter-feed.key`。service key 必须是 project-bound
`service` key、purpose=`xinsight.feed.read`，并由 UniRouter 绑定非空 network/IP
allowlist；不得交给 model-gateway route。不要把 key 写进 env 文件。

### LiteLLM Reference Connector

LiteLLM 使用独立的 `/etc/x-insight/xinsight-litellm-feed.env`，其中准确只有六个非 secret
字段：

```text
XINSIGHT_LITELLM_FEED_ENDPOINT=https://PRIVATE-LITELLM-HOST/spend/logs/v2
XINSIGHT_LITELLM_FEED_POLL_INTERVAL=30s
XINSIGHT_LITELLM_FEED_SOURCE_INSTANCE=STABLE-LITELLM-INSTANCE
XINSIGHT_LITELLM_FEED_TENANT_REF=
XINSIGHT_LITELLM_FEED_ORG_REF=CANONICAL-DECIMAL-ORG
XINSIGHT_LITELLM_FEED_PROJECT_REF=CANONICAL-DECIMAL-PROJECT
```

- `ENDPOINT` 必须是准确 HTTPS URL、无 userinfo/query/fragment，且 path 只能是
  `/spend/logs/v2`；worker 禁用代理与 redirect，并使用 system root 验证证书。
- `POLL_INTERVAL` 为 `1s..1m`，缺省 `30s`；它不改变 60 秒 start-time eligibility
  delay。每轮 `F=floor_microsecond(T-60s)`，实际 HTTP inclusive `end_date`
  `Q=floor_second(F)`；`Q < startTime <= F` 延后到重叠轮次，row `endTime`
  不参与查询 fence 或 checkpoint。
- `SOURCE_INSTANCE` 是 1..128 字节的稳定 UTF-8 source instance，不能在同一 durable
  checkpoint 上更换。
- `TENANT_REF` 可为空；非空时是 1..128 字节 UTF-8，并成为不可漂移的 scope binding。
- `ORG_REF` 与 `PROJECT_REF` 必须是无前导零的正十进制字符串，并与唯一产品 registry/
  checkpoint scope 精确一致。

被连接的 Source 必须准确固定为 LiteLLM `v1.93.0`、容器 digest
`sha256:a1745e629abfb17d434426ff48b115f54f4f4c4a0f5af241de569e93c63c411e`，
以及官方 Source commit
`052b5a2169d8d3082e1d66e69f200a72b0c1e274`，并在其受管配置中显式保留：

```yaml
litellm_settings:
  turn_off_message_logging: true
  log_raw_request_response: false
```

这两个 Source 侧设置不能由 X sanitizer 替代。组件资格使用唯一 prompt/response sentinel，
在真实请求和真实 `/spend/logs/v2` 持久化完成后，分别扫描 LiteLLM Source DB、LiteLLM
容器日志、`/spend/logs/v2` 原始响应和 X Evidence；任一层出现 sentinel 都必须 fail closed，
不得启用 worker 或以“X 最终没有保存 raw 内容”降级放行。
同一四层扫描还必须把当次生成的目标 traffic key、observer key 和其他团队 traffic key
原始值作为 secret sentinels，逐一证明明文出现次数为零；断言不得打印这些 key。

X 读取 Source 的身份必须是 LiteLLM 非管理员团队成员，并同时满足两个独立门禁：

- 该成员所属团队准确包含 `team_member_permissions=["/spend/logs"]`；
- 该成员的独立 observer key 准确包含 `allowed_routes=["/spend/logs/v2"]`。

observer key 只能读取该成员获准团队的 spend logs，不能调用
`/v1/chat/completions` 或其他 LLM/management route。模型流量必须使用另一把 team-bound
traffic key；LiteLLM master key 只能用于受控的团队/成员/key 初始化或清理，严禁作为
X credential、模型流量 key 或 `/spend/logs/v2` 日常读取 key。组件资格必须额外证明：
observer 精确读取目标团队两条记录、LLM route 返回拒绝、第二个未授权团队的一条记录不会
出现在 observer 响应或 X Evidence 中。

组件资格中的 observer LLM 负向调用只允许一次请求、零重试。正常合格结果是严格 JSON
HTTP `403`。仅对上述准确 `v1.93.0` 与准确容器 digest，若这一个负向调用返回
`RemoteDisconnected`，可以记录为版本绑定的 `pinned fail-closed degraded error
semantics`，但不得称为 HTTP `403`，也不得将本地 outcome marker 冒充 Source response。
此例外之后必须立即证明 LiteLLM 与确定性上游进程健康、同一 observer key 的
`/spend/logs/v2` 白名单读取成功、Source SpendLogs 仍为 `0`、上游 chat POST 计数仍为
`0`，否则组件资格失败。LiteLLM 版本、容器 digest、官方 Source commit 或负向测试形状
发生任何变化时，必须删除该例外并恢复严格 JSON HTTP `403` 资格证明；不得把这个 CI
资格例外扩展为生产 Connector transport 语义。

observer API key 只能位于 `/etc/x-insight/litellm-feed.key`，由
`xinsight-litellm-feed.service` 通过独立 systemd credential
`litellm-feed.key` 读取。源文件和 systemd credential copy 都必须是 root 或实际 service
EUID 所有的 regular、`0400` 或 `0600`、`nlink=1` 文件；symlink、hard link、group/world
权限及其他 mode 都会 fail closed。key 不得进入上述六字段、argv、日志、receipt 或仓库。

fresh install/upgrade 只安装 unit；缺省状态是 loaded、inactive、disabled，且不创建 env/key、
不发请求。第一次启用顺序固定为：

1. 在 root-only 编辑器中写入并逐字段核对六个非 secret 值；
2. 在 LiteLLM 侧创建/核对非管理员团队成员、`/spend/logs` 团队权限和仅允许
   `/spend/logs/v2` 的独立 observer key；以 root 创建准确 key 文件，核对
   owner/mode/regular/nlink，不输出内容，且明确拒绝 master/traffic key；
3. 核对 importer DSN credential、私有 HTTPS 路由、证书和 product registry；
4. 先执行 `systemctl start xinsight-litellm-feed.service`，不要求 enable；
5. 用 `systemctl status`、sanitized journal、durable Connector status 和 Read API 证明 activation
   forward、完整 page 后 checkpoint 推进及 evidence 可见。

日常启停只使用：

```text
sudo systemctl status xinsight-litellm-feed.service --no-pager
sudo systemctl stop xinsight-litellm-feed.service
sudo systemctl start xinsight-litellm-feed.service
```

retryable transport/408/429/5xx 使用 bounded backoff。schema、scope、cursor、content conflict、
认证等永久错误会让当前进程 fail closed；修正准确根因后必须
`systemctl restart xinsight-litellm-feed.service`，不能清空或手工推进 checkpoint。
LiteLLM key 不热加载：轮换时先停止或隔离 source，原子替换准确 key 文件，再 restart 并做
同一 scope/checkpoint 的只读证明。

rc.12 应用回滚到 rc.11 时，生命周期会 stop 并移除 rc.11 不支持的
`xinsight-litellm-feed.service` unit，再 daemon-reload；它故意保留
`xinsight-litellm-feed.env`、`litellm-feed.key` 与数据库中的 durable checkpoint，且不会启动
任何替代 worker。重新升级到支持版本前不得手工运行 importer 或复用这些状态到其他 scope。

### Receiver-generated feed credential

feed credential 必须在准确 X-Insight target 上由 root 本地生成。UniRouter 只接收
`prepare` 输出的 Frozen Registration Bundle V1，不接收 raw token：

```text
sudo /opt/x-insight/current/bin/xinsightctl feed-credential prepare
sudo /opt/x-insight/current/bin/xinsightctl feed-credential status
```

`prepare` 的 stdout 是严格六字段 public JSON：`schema_version`、`token_format`、
`verifier_scheme`、`key_prefix`、`verifier_sha256`、`operation_id`。原始 token 是
32-byte `crypto/rand` 值编码成准确 `ur-xf1-` + 64 个小写十六进制字符，只写入
`/etc/x-insight/unirouter-feed.key.next`。该文件必须是 root:root `0600`、regular、
single-link、非 symlink；非 secret 可恢复状态位于
`/var/lib/x-insight/control/feed-credential-operation.json`。

初次激活顺序固定为：X `prepare`；Uni 在 paused 状态登记 public bundle；X 使用 `.next`
中的 staged token 对准确私有 endpoint 做认证预检，并且只接受严格
`completed_execution_feed.connection_paused`、`retryable=true` 的 `503` envelope；
随后才使用 bundle 中的准确 operation/verifier 原子替换并 restart；Uni 再 promote 并
resume：

```text
sudo /opt/x-insight/current/bin/xinsightctl feed-credential activate \
  --operation-id PUBLIC_OPERATION_ID \
  --verifier-sha256 PUBLIC_VERIFIER_SHA256
```

`activate` 使用 `xinsight-feed.env` 中与 worker 相同的固定 endpoint、system-root TLS、
no-proxy/no-redirect 和 bounded timeout 语义，在 process memory 中使用 `.next` token。
`401`、`403`、`200`、非严格 envelope 或 transport failure 都必须在替换前失败，保留旧
live credential、`.next` 和 prepared state；systemd unit 已 active 也不能替代该证明。
预检通过后才原子替换 `/etc/x-insight/unirouter-feed.key` 并执行
`systemctl restart xinsight-feed.service`；它不把 secret 放进 argv/env，也不 enable unit。
如果进程在 replace/restart/state-finalize 之间中断，用同一 operation/verifier 重跑
`activate`，不得生成新 token。若 activation barrier 尚未跨越，可准确取消 pending：

```text
sudo /opt/x-insight/current/bin/xinsightctl feed-credential abort \
  --operation-id PUBLIC_OPERATION_ID \
  --verifier-sha256 PUBLIC_VERIFIER_SHA256
```

rotation 时旧 live credential 必须保持 active；暂停 Uni source 后，用 status 中的准确
active operation/verifier 绑定 replacement prepare：

```text
sudo /opt/x-insight/current/bin/xinsightctl feed-credential prepare \
  --expected-active-operation-id CURRENT_PUBLIC_OPERATION_ID \
  --expected-active-verifier-sha256 CURRENT_PUBLIC_VERIFIER_SHA256
```

随后仍按“Uni stage paused → X staged-token authenticated paused 503 →
atomic activate/restart → Uni promote/revoke old/resume”完成。永久 401 会让 worker
fail closed 并保持 durable cursor；
修复 credential 后的 restart 从同一 cursor 重试，不得 reset/backfill 数据库。

不要把 DSN、密码、token 或 raw evidence 写入：

- release artifact、手册、receipt、install-state 或 operation journal；
- shell history、命令参数、systemd Environment、截图或工单正文；
- Git 仓库、共享 cooperation 目录或未经批准的诊断包。

## 网络与访问

X-Insight 进程和 PostgreSQL 都必须 loopback-only。当前版本的身份验证、Session 与上述 RBAC
仍由 X-Insight 自己执行；TLS terminator 不是身份 authority，也不能注入用户/角色 assertion。

默认不信任任何 forwarded header。只有 root 创建严格
`/etc/x-insight/trusted-tls-proxy.env` 并重启服务后，才启用同机代理：

```text
XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback
XINSIGHT_EXTERNAL_ORIGIN=https://APPROVED_HOST_OR_IP
```

文件只能有这两个非 secret 值，必须 root:root regular、非 symlink、mode `0600`。origin 必须
是无 path/query/fragment/userinfo 的规范化 HTTPS origin。后端只接受 loopback remote、唯一
`X-Forwarded-Proto: https`、唯一且与 origin 相同的 `X-Forwarded-Host`、准确 Host/Origin、
same-origin fetch 和空通用 `Forwarded`。Nginx 必须覆盖而不是追加客户端 forwarded headers，
其他 server block/default host 必须拒绝。任一不匹配都按非 TLS fail closed。

Target 还要验证 certificate/key ownership 与自动 renewal、Nginx request bounds、应用/DB
listener 仍为 loopback、公共安全组和 UFW 仅 80/443、HTTP 只做 ACME/HTTPS redirect，以及
旧 release rollback。HTTP SSH tunnel、一般私网、代理自身 login page 或 localhost HTTP 都
不等于 X-Insight Secure-cookie Target 证明，也不得通过去掉 `Secure` 或放宽
same-origin/CSRF 来绕过。

feed endpoint 默认必须是固定 HTTPS URL。只有部署证据能够证明固定 literal address
承载于已认证的 loopback/RFC1918/Tailscale/ULA 私有 tunnel 时，才可把
`XINSIGHT_UNIROUTER_FEED_ALLOW_PRIVATE_TUNNEL_HTTP` 明确设为 `true`；一般私网地址本身不
等于已认证 tunnel。worker 禁用环境代理与 redirect，并限制 connect/header/request
timeout、response header、page body 与 page count。systemd 默认拒绝公网 egress；实际
endpoint 与 tunnel 属性仍须在受控部署中逐字核对。

## 数据准入

fixture/synthetic 用于功能证明，不含真实用户事实。internal-real development metadata 是
项目自有非生产环境中真实发生、但不含客户事实的执行元数据；customer sanitized metadata
则是客户批准后保留调查所需最小字段的真实来源元数据。后两类都必须明确字段范围、
custodian、用途、保留时间和退出动作，并使用独立 trial database。三类都不得包含 raw
prompt/response。

在导入前检查：

- source 与 proof class 可追溯；request/trace/actor/model/policy 标识已按批准规则处理；
- 自由文本、header、body、tool argument、附件和 secret 已排除；
- 文件权限和传入路径满足客户政策；导入 receipt 不复制原始 payload；
- 历史 internal-real/customer sanitized artifact 与其 exact approval manifest 先共同通过
  `unirouter preflight-sanitized`；live pull 则必须通过固定 DTO、payload SHA、source
  instance、opaque cursor/high-watermark 与 contracted-pull provenance，不得携带 artifact/
  approval-manifest/exchange 字段；
- 不因存在 `source_type` 或 schema 预留字段而声称已实现 adapter。

## 缓存与清理权限

X-Insight 当前没有用户权限缓存控制面。谁可启用、读取、清除身份/权限缓存由 X-IAM
或客户 IAM 的 authority 决定；X-Insight 只记录获准输入的结果/证据。任何未来 cache
invalidation contract 必须版本化、可审计，不能让调查 UI 获得实时授权写权限。
