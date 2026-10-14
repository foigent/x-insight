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

受支持的 root CLI 在另一个 root-only
`/etc/x-insight/xinsight-connector.env` 原子写入以下 target key，并在准确 IP origin 情况下准备
controller 与 Feed worker 各自的受管出站规则：

```text
XINSIGHT_UNIROUTER_INSTALL_ORIGIN=https://APPROVED_UNIROUTER_ORIGIN
```

不要创建、复制或编辑 identity 文件。deployment ID 不是 bearer credential，但它是
installation/replay 绑定的稳定部署身份；不得按发布版本、服务重启或 Connector 重试轮换。
`deploy/xinsight-connector.env.example` 只说明 target 形状，不是待复制到 `/etc` 的配置。UniRouter
origin 必须是规范化 HTTPS origin：无 userinfo、path、query 或 fragment。

完整启用条件分布在三个 root-owned 配置面：

- `/etc/x-insight/xinsight-connector-id.env` 只提供 installer 生成的稳定 deployment ID；
- `/etc/x-insight/xinsight-connector.env` 提供 CLI 设置的唯一 UniRouter install origin，以及下述可选机器用途引用；
- `/etc/x-insight/trusted-tls-proxy.env` 提供准确
  `XINSIGHT_EXTERNAL_ORIGIN=https://APPROVED_XINSIGHT_ORIGIN`，并按本章网络边界启用可信同机 TLS
  terminator。

`xinsight.service` 依次加载 TLS、identity 与 target 文件。UniRouter install origin 缺失时，即使
stable ID 已生成且已经配置 TLS/external origin，Connector 仍明确 disabled，Web 可正常启动且不会
产生 Connector 流量。一旦 target 文件存在，三项必须同时有效；缺失 ID、缺失 X-Insight external
origin 或任一 origin 非规范 HTTPS 都会 fail closed 并阻止服务启动。配置文件本身不会
activate/enable/start Feed worker；worker 仍只能在独立三阶段 authority 的最后一步启动。

### 独立正文保管与机器用途引用

受管 host 引用按 UID0、精确权限、普通文件且单链接、无特殊权限位和安全父目录校验。秘密文件为 `0600`（已有用途允许 `0400`），公开配置和 unit dropin 为 `0644`；GID 记录实际值，不强制改为0。秘密不给组或其他用户访问，公开配置允许读而不允许写，因此 root:xinsight 控制器无需 CAP_CHOWN。systemd 凭据副本原有 GID、ACL 和只读挂载合同保持。`/etc/x-insight/connector-machines/`、每个 installation 子目录及其 `sources/` 为 `0700`，后续原子写入不会将它们扩为 `0755`；已有更严权限保留。

正文保管不依赖 UR 配对。schema44 的受管首装和升级在原维护锁、operation journal 内准备一次
root-owned `0600`、非符号链接且单链接的 64 hex key。已有 active 或 disabled 绑定始终保留原 key，
包括还有过期密文待清理的数据库；原 key 缺失时必须恢复，禁止生成替代 key。正常首装预加载
server 的正文读取能力。后续 full_content 策略只有在当前进程证明实际加载了同一 key 字节后才能
确认 effective；已加载的普通策略保存不重启服务，无来源配置可等待来源，存在受管来源分区时
同时验证当前 worker。以下 `review-body-config set` 保留为显式维护入口：

```text
sudo /opt/x-insight/current/bin/xinsightctl review-body-config set \
  --key-id APPROVED_KEY_ID --key-file /etc/x-insight/review-body.key --check
sudo /opt/x-insight/current/bin/xinsightctl review-body-config set \
  --key-id APPROVED_KEY_ID --key-file /etc/x-insight/review-body.key
sudo /opt/x-insight/current/bin/xinsightctl review-body-config validate
```

命令只发布 `/etc/x-insight/xinsight-review-body.env` 的 key ID/源文件引用，以及 server/Source Facts
worker 各自的 `20-review-body-credentials.conf`，不生成密钥、写授权或重启服务。两服务取得同一
key 的精确 systemd 凭据副本；worker 的 20 文件在原 10 清空集合后追加 workspace DSN，原
importer/source/content 凭据保持。仅精确两 unit 的只读 ACL `review-body.key` 副本可用，普通
group-readable 文件、错名/错 unit、symlink/hardlink 均拒绝。服务加载须在另行获准的维护窗口
执行。`--check` 不创建 lock/staging、不发请求、不 reload。中断或混合引用阻止启动；同一
完整候选可重放修复，陌生 staging 拒绝。key ID/文件绑定不得通过再次 set 隐式轮换。

机器两用途共用一组 `XINSIGHT_UR_MACHINE_INSTALLATION_ID`、`XINSIGHT_UR_MACHINE_SOURCE_INSTANCE`、
`XINSIGHT_UR_MACHINE_ORG_ID` 和上述唯一 origin。`connector-target-config set-policy` 与
`set-enrollment` 均接收 `--installation-id ID --source-instance INSTANCE --org-id ID
--credential-file ROOT_FILE [--ca-file FILE] [--check]`。分别引用独立 root-owned `0400/0600`
单链接文件，token 格式分别为 `ur-xpm1-<64 lowercase hex>` 与 `ur-xse1-<64 lowercase hex>`；
不在命令参数传 token。controller 直接读取 root 文件，不向 Web 交付机器 bearer，不新增
controller credential 副本。两用途全部缺席即 disabled，身份半组或共享文件拒绝；增设另一
用途保留原用途，已有绑定禁止隐式替换。启用配置不等于 UR 已登记、激活或授予 delegation。
正常连接向导由原后台控制器完成两用途配对、授权范围读回及来源发现；该维护命令本身不替代正常向导。

受管 target 网络与机器配置仅覆盖准确 HTTPS IP origin，由该 IP 生成单一 `/32` 或 `/128` egress；标准 TLS、
系统信任库及现有可选 CA 校验保持，不加代理、重定向或自定义 DNS。hostname origin 仍可用于
原 Connector target，但不声称提供其精确受管 DNS/出站资格。root CLI 的 `set` 对准确 IP
origin 在 `xinsight-connector-controller.service.d` 和 `xinsight-feed.service.d` 分别发布
`19-ur-target-network.conf`，只允许 localhost 与该准确 IP；先核对原 20/40 规则没有不同地址或宽规则。
同 origin 重试可补全中断后的半发布；不同内容或来源绑定不能隐式覆盖。CLI 在 daemon-reload 后
重启 controller，并读回新的 InvocationID 与两 unit 的实际 `IPAddressAllow`，才报告成功；
Feed worker 仍由独立 worker-last 步骤启动。暂停认证与 worker-last 都要求相应 unit 的实际
受管规则已生效，不以 drop-in 文件存在或 worker active 代替联网和经济 feed 读取证据。
正文/机器引用缺席的
旧安装与升级保持；启用后升级保留源文件及引用，目标发行无对应 unit 支持时在切换前拒绝。
`review-body-config disable [--check]`、`connector-target-config disable-machines [--check]`
仅可恢复退役本功能引用，保留密钥和历史正文；需加载禁用状态后才能回退到未支持的发行。
数据库 restore 仍恢复到新数据库；切换前逐项校验未过期正文的 key ID、authenticated identity、
密文与摘要，错误/缺失密钥或副本阻止切换，resume 重新检查。恢复不换 key、不重写历史正文，
过期正文不因恢复获得新期限。完整私有备份包含 PG16 custom dump、原正文 key 与准确包摘要；
此包属于秘密材料，只能交付到明确批准的 owner-only 位置，禁止 Web 上传下载或普通导出。

```text
sudo /opt/x-insight/current/bin/xinsightctl backup --output /APPROVED_PRIVATE_PATH/complete.tar --check
sudo /opt/x-insight/current/bin/xinsightctl backup --output /APPROVED_PRIVATE_PATH/complete.tar
sudo /PATH/TO/VERIFIED/xinsightctl install --artifact /PATH/TO/VERIFIED/release.tar.gz \
  --recovery-bundle /APPROVED_PRIVATE_PATH/complete.tar --recovery-bundle-sha256 sha256:EXACT --check
sudo /opt/x-insight/current/bin/xinsightctl restore --recovery-bundle /APPROVED_PRIVATE_PATH/complete.tar \
  --recovery-bundle-sha256 sha256:EXACT --new-database xinsight_restore_APPROVED --check
```

`--check` 不创建 key、lock、journal、备份或新库。真正执行前沿相同准确参数移除 `--check`；
install 导入的是原 key，数据库随后沿原 restore 新库流程验证并切换，不能覆盖原库。
旧数据库单独备份仍沿 `restore --backup FILE --backup-sha256 sha256:EXACT --new-database NAME`。
外部署恢复保留历史事实及授权读取，旧机器身份和 token 不继承本机运行权限，须重新明确授权。

### 正常连接 UniRouter

在“来源与覆盖 → 连接器”选择“连接 UniRouter”，用当前 XI 平台管理员确认本部署保管设置。
无既有策略时推荐完整正文、90 天、每项目 5 GiB、单条 32 MiB；推荐本身不生效。已有生效设置
按读回保留，可明确复用或修改，初次 CAS 只约束初次生效，不冻结后续获准策略修改。
通过邀请链接到固定 UniRouter，由 UR 自己的当前人类身份确认来源项目、当前/未来覆盖、正文
供数、初始来源策略及后续策略管理委托。权限资格不足时明确处理，不隐式补 grant。
邀请沿原默认 15 分钟期限，过期须重新明确确认；关闭页面不终止已获同意的后台配置。
刷新时仅用当前 XI 登录身份和浏览器保存的非秘密请求/安装标识恢复同一邀请，不保存 Token。
进度区分等待同意、正在配置、需要处理、等待数据和实际开始同步。正文副本收到与服务端
受保护读取是独立证据，服务端 accepted 审计不表示浏览器完整收到了字节。
配对后的正文总开关要求实际 UR 企业默认策略 read/write 委托；没有该资格时先显示限制并
禁用联合保存，由 UR 组织管理员明确补授所需范围，不能以 XI 单方保存代替联合开关。

### 原连接的控制凭据续期与恢复（schema45）

受管连接的控制 token 有 24 小时有效期。原连接保持 active 且授权未撤销时，UniRouter 在当前
token 剩余 6 小时内沿原控制循环自动准备替代 token；XI 只保存摘要，先返回 prepared，待
UniRouter 用已保存的新 token 查询并确认后，XI 才切换唯一 current 摘要并撤销旧 token。
prepared 表示轮换待确认，不能据此判断 UniRouter 已晋升或新事实已到达。暂停、断开及身份、
当前 token 或必需授权被撤销的连接不会自动续期；撤销不能通过恢复操作复活。

若离线跨过有效期，在 XI 原连接页核对“已过期需管理员恢复”后，由具有连接管理权限的 XI
管理员点击“恢复此连接”。页面打开固定 UniRouter 来源的恢复页；UniRouter 管理员须在其服务端
再次确认原安装、项目、范围和来源绑定。XI 签发的恢复材料仅 15 分钟有效，过期后须在 XI
页面再次明确确认；刷新或丢失响应可用同一请求标识重读原材料，不能借重试延长有效期。
恢复还要求 UniRouter 保有准确的旧 secret，票据或旧 secret 单独都不能恢复。流程继续使用
同一 installation、身份和来源绑定，不重建连接，也不修改旧事实、游标或正文密钥。若原
secret 丢失、身份/授权已撤销或来源绑定不一致，本路径会拒绝，不能通过手工补 token 绕过。

`upgrade --check` 在 schema45 目标下只读检查原凭据是否可唯一绑定，并给出检查时间、身份
及 current token 到期、pending 操作和接入状态。过期需要管理员恢复，但不妨碍修复用途的
受管升级；升级成功本身不表示接入已恢复。连接页的凭据诊断、数据新鲜度和正文读取是三个
独立观察面，须分别核对。机器端点和严格请求/回执字段见 [控制凭据 API](../reference/08-cli-api-and-error-contract.md#connector-控制凭据生命周期)。

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
origin，更新 target 文件并为准确 IP 准备两个受管 19 drop-in，无法写入 identity key；写入使用
完成 sync 的 staging 与原子发布，不会留下半文件覆盖当前 target。既有受管升级也会从同一已确认
origin 补齐缺失的 19 文件，`--check` 与 `validate` 不写文件或 reload。每次 Web 启动还会通过 root-only、clean-environment
`ExecStartPre` 再次校验三个文件的 owner/mode/key、deployment ID 形状以及 full target 的 TLS 前件；
任一漂移都会在 Web 进程前失败。上述 Web restart 是独立现场授权动作；CLI 的 controller
restart 只加载出站规则，不启动 Feed worker。

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
`/etc/x-insight/unirouter-feed.key.next`。该文件必须是 root-owned `0600`、regular、
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
