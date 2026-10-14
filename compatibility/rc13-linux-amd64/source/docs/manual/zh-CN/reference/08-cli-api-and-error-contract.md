# CLI、API 与错误合同

## 版本与主机生命周期 CLI

四个运行二进制支持 `version --json`：`xinsight-server`、`xinsight-importer`、`xinsightctl`、
`xinsight-launcher`。root-only 生命周期命令为：

```text
xinsightctl verify --artifact FILE
xinsightctl status
xinsightctl install [--check] --artifact FILE
xinsightctl adopt [--check] --artifact TARGET --legacy-artifact LEGACY
xinsightctl upgrade [--check] --artifact FILE
xinsightctl rollback [--check] --previous [--reason-code CODE]
xinsightctl restore [--check] --backup FILE --backup-sha256 SHA256 --new-database NAME [--reason-code CODE]
xinsightctl recover
xinsightctl resume
xinsightctl abort
xinsightctl feed-credential prepare [--expected-active-operation-id ID --expected-active-verifier-sha256 HEX]
xinsightctl feed-credential status
xinsightctl feed-credential activate --operation-id ID --verifier-sha256 HEX
xinsightctl feed-credential abort --operation-id ID --verifier-sha256 HEX
xinsightctl connector-target-config validate
xinsightctl connector-target-config set --unirouter-origin HTTPS_ORIGIN
xinsightctl connector-epoch status (--operation-id ID | --source-system-id UUID --adapter-manifest-id UUID --cursor-key KEY --worker-unit UNIT)
xinsightctl connector-epoch prepare --request-file ABSOLUTE_ROOT_OWNED_JSON
xinsightctl connector-epoch activate --operation-id ID --plan-sha256 SHA256
xinsightctl connector-epoch abort --operation-id ID --plan-sha256 SHA256
xinsightctl clean-baseline data-reset-preview --rollback-backup FILE --rollback-backup-sha256 SHA256 --new-database NAME
xinsightctl clean-baseline data-reset-apply --rollback-backup FILE --rollback-backup-sha256 SHA256 --new-database NAME --preview-sha256 SHA256 --reason-code second_integration_data_reset
xinsightctl clean-baseline checkpoint-bootstrap-preview --data-reset-operation-id ID --data-reset-receipt-sha256 SHA256 --checkpoint-request-file ABSOLUTE_ROOT_OWNED_JSON
xinsightctl clean-baseline checkpoint-bootstrap-apply --data-reset-operation-id ID --data-reset-receipt-sha256 SHA256 --checkpoint-request-file ABSOLUTE_ROOT_OWNED_JSON --preview-sha256 SHA256 --reason-code second_integration_checkpoint_bootstrap
xinsightctl database-admin-credential preview --authorization-ref REF --reason-code local_tooling_exposure
xinsightctl database-admin-credential apply --request-file ABSOLUTE_ROOT_OWNED_JSON
xinsight-release campaign-authority derive --artifact SIGNED_RELEASE --trust-store RELEASE_TRUST --expected-commit COMMIT --expected-version VERSION --output-directory NEW_DIRECTORY
xinsight-release campaign-authority verify --artifact SIGNED_RELEASE --trust-store RELEASE_TRUST --product-release PRODUCT_RECEIPT --verifier-release VERIFIER_RECEIPT --expected-commit COMMIT --expected-version VERSION
xinsight-importer unirouter import-campaign-package --operation-id ID --package PACKAGE --n1-release-manifest N1_MANIFEST --n1-verifier N1_VERIFIER --campaign-manifest MANIFEST --unirouter-ack UNI_ACK --xinsight-ack X_ACK --authorization AUTHORIZATION --authorization-signature AUTHORIZATION_SIG --authorization-trust-set AUTHORIZATION_TRUST --source-evidence-trust-set SOURCE_TRUST --component-scope-receipt X_SCOPE --component-verification-receipt X_VERIFICATION --verifier-release X_VERIFIER_RELEASE --product-release X_PRODUCT_RELEASE --release-artifact SIGNED_X_RELEASE --release-trust-store X_RELEASE_TRUST --evidence-schema M0_SCHEMA
```

`--check` 是只读 preflight；mutating command 需要 root。输出 JSON receipt/报告不含 DSN、密码、
token 或 raw evidence。`feed-credential prepare` 的 stdout 只能是严格 Frozen Registration
Bundle V1；`status` 只包含 public registration、文件 metadata、phase 和恢复动作。两个
`connector-target-config` 命令不启动 worker、不访问 UniRouter：`validate` 只读检查 managed
identity/target/TLS 文件的 owner/mode/key allowlist、deployment ID 形状与 full target 的 TLS 前件；
`set` 只以规范 HTTPS origin 原子更新独立 target 文件，无法改写 stable ID。两个
`abort` 属于不同 durable state machine，不能互换。`connector-epoch` 报告只输出 Cursor 与
high-watermark 的 SHA-256；prepare/activate 要求 worker unit 已存在、inactive、disabled，
且数据库处于无错误的 Cursor=high-watermark 边界。prepare request 使用
`xinsight.connector-checkpoint-epoch-prepare.v1`，必须是 absolute、root-owned、single-link、
regular、`0400`/`0600` JSON；raw Cursor 不进入 argv 或报告。prepared operation 会在数据库
写入路径阻止同一 binding 开始 poll，从而与 state row lock 一起关闭人工并发启动竞态。
activate 原子归档旧 epoch 并建立新 epoch，但不启动或 enable worker。

`data-reset-preview` 是只读 full-product fresh-database 预览，不接受 checkpoint request 或任何 next
source/cursor/epoch。它要求准确 PostgreSQL 16/schema 23、fresh backup SHA 与 `pg_restore --list`、
两个 present+inactive+disabled worker、严格 `demo.operator` 账号、尚不存在的 `xinsight_clean_*` 数据库，
并仅以存在性/文件元数据检查 UniRouter active/staged/operation-state、LiteLLM credential/environment
五个精确 path；不读取或输出内容。`data-reset-apply` 使用独立固定 reason，创建并切换
`adapter_import_states/operation/history=0/0/0` 的严格空库，crash-safe 退役这些旧 campaign
credential/config state 但不生成 replacement。LiteLLM systemd unit 保留并保持 inactive+disabled；
当前 Paths 合同没有 OpenInference 等价持久文件，命令不虚构该路径。
apply 还必须把准确 dump 隔离 restore 到 operation 唯一临时库，以完整数据库 semantic digest 证明
与停稳 source 相同并先删除临时库，才写 durable receipt。receipt 后只有 retained dump SHA/archive、
restore proof、strict-empty live readback 与旧库零 session 均重验通过，才精确删除 predecessor 数据库；
成功返回的 `predecessor_database_absent=true`，且只保留该 custom-format dump。缺少 dump/proof 时旧库
不会被删除。

`checkpoint-bootstrap-preview|apply` 必须消费精确 completed DATA_RESET operation/receipt SHA 和另一
固定 reason，才可从 sealed historical maximum 计算 `max+1` 并绑定新 UniRouter
source/cursor/high-watermark。五个 connector credential/config path 必须继续为空；该命令不生成
LiteLLM credential/environment。
preview 的 canonical request SHA-256 还绑定完整 request 与 authorization reference，apply 的 staged
root-only request 必须精确匹配。它只建立一个 inactive/idle、零 counter、无 operation/history 的
state；仍不启动 worker、配置凭据或 poll。两阶段报告不返回 verifier、DSN、credential bytes 或 raw
Cursor，命令存在也不代表操作授权。

本地账户 credential 的离线 root CLI 为：

```text
xinsightctl access generate-subject
xinsightctl access hash-password
xinsightctl access validate --file ABSOLUTE_ACCOUNTS_JSON
xinsightctl service-identity create --name NAME --expires-at UTC_RFC3339
xinsightctl service-identity list
xinsightctl service-identity revoke --id ID --reason REASON
xinsightctl discovery refresh --as-of UTC_RFC3339 --limit 20..1000 --minimum-sample 20..limit
```

`generate-subject` 只生成稳定 opaque subject；`hash-password` 只在交互 TTY 隐藏读取和确认，
不接受 password flag、stdin/pipe 或 environment；`validate` 要求 absolute path，并只输出
schema 与角色/disabled 计数。三者都不会创建 Web 账户或写回 credential 文件。

`service-identity` 与 `discovery refresh` 都是 root-only，并使用分离的 admin/reader/workspace
数据库身份。service token 只在 create 成功响应中显示一次，数据库只保存 digest；不得写入
Markdown、argv、env 或浏览器。Discovery 固定 `as_of`、有界 sample 与 minimum sample，结果
写入产品本地分析层，不写 M0 Evidence。

受管 live worker 的固定入口是：

```text
xinsight-importer unirouter pull-feed
```

它不接受运行参数；endpoint/poll interval 来自受限 env，service key 与 importer DSN
只来自 secret file/systemd credentials。正常情况下由 `xinsight-feed.service` 管理，不手工
在 shell 中拼接 secret。

worker 的 durable failure code 是安全的机器字段。`completed_execution_feed.cursor_expired`、
`source_status_401`、`source_instance_mismatch`、`contract_page_invalid`、
`evidence_conflict` 等 permanent 状态不会推进 cursor；transport/429/5xx/临时 DB 错误做
bounded backoff。修复 permanent 配置后重启 worker 会从原 durable cursor 再尝试一次。

OpenInference 预留的 bounded-offline 入口是：

```text
xinsight-importer openinference import-bundle \
  --authorization-file ABSOLUTE_ROOT_OWNED_JSON --target-ref EXACT_NONPROD_TARGET \
  --bundle ABSOLUTE_PATH --operation-id ID --bundle-id ID \
  --producer-instance ID --org-ref ID --project-ref ID
```

授权文件必须是单 hard link、root-owned、`0400`/`0600` 的严格
`xinsight.openinference-bounded-offline-authorization.v1`。它只接受 `nonproduction`、最多两小时，
并精确绑定 target machine ID SHA-256、当前 release commit/version、bundle path/SHA、operation、
producer、tenant/org/project 与最多 10 条 metadata-only record。任何 prompt/response attribute、
授权中的未知字段、授权或 OTLP 任意层级的重复 JSON 字段、过期或绑定漂移都会在
credential/数据库之前拒绝；值为 false 的四个安全边界字段也必须显式存在。精确重放由
durable operational receipt 收敛；该门禁绝不授权 listener、Feed、Cursor、Checkpoint 或
Production。

外部 bootstrap `xinsight-release` 也支持 `version --json`，并提供 `keygen`、`pack`、`verify`、
`extract`。现场只使用经独立可信路径批准的 bootstrap 执行 `verify`/`extract`；`keygen`/`pack`
属于发布方操作，不在用户主机生成生产 signing key。

发布内 schema contract 只读命令为：

```text
xinsightctl schema-contract expected
sudo xinsightctl schema-contract inspect
```

`expected` 可在无 host state 时读取签名 binary 内嵌的 generated contract；`inspect` 使用 root-held
admin DB credential 做只读 catalog introspection，但只输出 schema/release identity、对象计数、
semantic/layout digest 与 `missing_object`/`unexpected_object`/`semantic_property_drift` 等脱敏原因。
列身份是 `namespace/table/column`，ordinal 只进入非权威 layout digest。

`database-admin-credential` 只允许 root 在独立授权下替换 `xinsight_admin` credential；preview/apply
输出、journal 与 receipt 只含 alias、metadata 和 digest，不含 DSN、密码或 SCRAM verifier。verifier
变更前允许通用 `abort`，变更后只允许通用 `resume`；它不重启服务、不写产品表、不启动 worker 或流量。

`campaign-authority derive|verify` 不建立第二套发布签名。它复用既有签名发布 manifest，并从准确
`bin/xinsight-importer` 条目、schema 23、manual-r13、M0 schema 和编译内 consumer contract
确定性派生两个 sidecar receipt。`import-campaign-package` 把所有 path 只当作有界本地传输定位：准确
release/runtime/product/N1/Source/package/member 验证全部在加载 importer DSN 前完成。replay 复用既有
schema-20 bounded-offline operation receipt；相同 operation/request 为 no-op，request drift 或 Evidence
conflict 失败关闭。命令不含 network endpoint、worker、Cursor、Checkpoint 或 credential surface。

## Investigation API

| 方法与路径 | 用途 |
| --- | --- |
| `GET /healthz` | 进程 liveness |
| `GET /readyz` | DB、schema、PostgreSQL、版本与手册兼容 readiness |
| `GET /api/v1/executions` | bounded execution search |
| `GET /api/v1/executions/{execution_id}` | execution detail |
| `GET /api/v1/executions/{execution_id}/investigation` | outcome Story、来源声明 DAG、completeness 与 unprovable claims |
| `GET /api/v1/executions/{execution_id}/trace-journey` | 同 source/org/project/trace 跨 execution 的来源声明 Span 路径；不改 M0 identity/hash |
| `GET /api/v1/executions/{execution_id}/timeline` | evidence-derived timeline |
| `GET /api/v1/executions/{execution_id}/events/{event_id}` | selected-node/source/provenance detail |
| `GET /api/v1/executions/{execution_id}/gaps` | capture-gap inspection |
| `GET /api/v1/sources` | bounded source options |
| `GET /api/v1/connectors` | Connector trust/read-only lifecycle projection |
| `GET /api/v1/sessions`、`GET /api/v1/sessions/{session_id}` | 来源声明的 Session/Thread 与 execution links |
| `GET /api/v1/compare` | 同一 fixed `as_of` 的 execution 对比；Missing/Partial/0 分离 |
| `GET /api/v1/executions/{execution_id}/feedback` | 来源评价/反馈读取 |
| `GET /api/v1/executions/{execution_id}/versions` | prompt/app/policy/model/connector 版本维度 |
| `GET /api/v1/alerts` | 有界 Alert 列表 |
| `POST /api/v1/alerts/{alert_id}/case` | human-only Alert-to-Case 关联 |
| `GET /api/v1/discovery/hypotheses` | 有证据、限制与 abstention 的候选假设 |
| `GET/POST /api/v1/cases`、`GET /api/v1/cases/{case_id}` | Case 列表、创建和 dossier 读取 |
| `POST /api/v1/cases/{case_id}/{executions,findings,evidence,gaps,annotations}` | human-only Workspace 写入 |
| `POST /api/v1/cases/{case_id}/seal` | immutable Case revision/digest |
| `GET /api/v1/cases/{case_id}/evidence-pack.{json,html}` | sealed deterministic Evidence Pack |
| `GET/POST /api/v1/saved-views` | bounded filter/presentation 保存，不保存结果快照 |
| `GET /api/v1/service-identities`、`POST .../{id}/revoke` | operator 只读列表/撤销；创建只允许 root CLI |
| `POST /api/v1/gaps/{gap_id}/resolution` | human-only Gap 调查状态；不删除或关闭 Evidence Gap |

`/readyz` 还会使用 reader DSN 执行一个有界、零数据返回的代表性读取，准确覆盖
`source_systems`、`evidence_events`、`capture_gaps`、`import_provenance` 和
`import_provenance_events`。成功只返回 `representative_read: "ok"`；任一表的 reader grant
漂移都会令 readiness 失败，不返回表内容。

Search filters 是 `request_id`、`request_id_prefix`、`source_system_id`、`outcome`、`has_gaps`、
`from`、`to`、`limit`、`cursor`。默认/最大 page limit 为 50/100；最大时间跨度 31 天。
时间必须是 UTC RFC3339。cursor 最大 2048 bytes、opaque、绑定 endpoint/filter/execution 与
服务端 `as_of`，有效期 24 小时；它不是授权 token。

## Access API

| 方法与路径 | 用途与权限 |
| --- | --- |
| `POST /api/v1/access/login` | 建立或轮换 Session；公开路径，但只接受同源 HTTPS 严格 JSON |
| `GET /api/v1/access/session` | 返回当前 Session metadata 与 Session-bound CSRF token |
| `POST /api/v1/access/logout` | 撤销当前 Session；要求同源 HTTPS 与 CSRF |
| `GET /api/v1/access/sessions` | operator 查看所有当前内存 Session |
| `POST /api/v1/access/sessions/{session_ref}/revoke` | operator 撤销指定 Session；要求同源 HTTPS 与 CSRF |

除 `/healthz`、`/readyz`、登录页、登录 API 和当前 `index.html` 实际引用的静态资产外，
所有 UI/API 都要求产品 Session。未认证 UI 请求严格 `303 Location: /login`；未认证 API
返回 `401 authentication_required`。`investigator` 只有 Evidence 读取权限；
Session 列表/撤销由 `operator` 独占。

Agent 使用单一 `Authorization: Bearer xib_...`，不能同时携带浏览器 Session cookie。它只允许
`GET /api/v1/executions...` 与 `GET /api/v1/sources`，不能访问 Workspace 写路径、Session
管理、service identity 管理、Discovery refresh 或运行时控制。expired/revoked/invalid token
分别 fail closed，单 identity 另有每秒 5、burst 10 的有界速率门禁。

登录 request body 上限 4 KiB，只接受准确 `username`、`password` 两字段且拒绝 duplicate/
unknown/trailing JSON。失败响应不区分未知 username、错误 password、disabled account 或
不合规账户状态。默认失败门禁为同一规范化 username 15 分钟内 5 次、全进程 1 分钟内
50 次；独立 Argon2id verification admission 最大并发 4。达到任一门禁返回
`429 authentication_rate_limited` 与 bounded `Retry-After`，不会启动新的 Argon2
verification。它与下述通用 API 并发上限 32 是两个独立资源边界。

Session cookie 固定为 `__Host-xinsight_session; Secure; HttpOnly; SameSite=Strict; Path=/`
且无 `Domain`。Session idle/absolute timeout 分别为 30 分钟/8 小时；每个 subject 最多 5 个，
单进程最多 500 个，重启全部失效。登录、logout 和 revoke 的 origin/CSRF 失败分别使用
`same_origin_required` 或 `csrf_validation_failed`，不能通过 HTTP、代理登录页或移除
`Secure` 降级绕过。

## Stable problem contract

所有非 2xx API 使用 `application/problem+json`，包含稳定 `code`、HTTP `status`、安全 title、
`retryable`、correlation ID，并可包含 field reason。不要依赖英文 title 做程序分支。

| HTTP | code | 操作 |
| --- | --- | --- |
| 400 | `invalid_argument`, `invalid_cursor`, `invalid_authentication_request` | 修正输入；不要重试相同请求 |
| 401 | `authentication_required`, `authentication_failed`, `agent_authentication_failed`, `agent_credential_expired`, `agent_credential_revoked` | 建立/重新建立准确的 Session 或 Agent credential；失败不披露 secret |
| 403 | `same_origin_required`, `csrf_validation_failed`, `permission_denied`, `agent_scope_denied` | 核对 HTTPS origin、CSRF、角色或只读 Agent scope；不要放宽门禁 |
| 404 | `execution_not_found`, `event_not_found`, `session_not_found`, `case_not_found`, `route_not_found` | 核对 ID、`as_of` 和路径 |
| 409 | `cursor_mismatch` | 使用原 filter，或重新搜索 |
| 410 | `cursor_expired` | 重新搜索获得新 `as_of` |
| 429 | `request_rate_limited`, `authentication_rate_limited`, `agent_rate_limited` | 按 bounded `Retry-After` 重试 |
| 503 | `temporarily_overloaded`, `database_unavailable`, `access_unavailable`, `agent_access_unavailable`, `workspace_unavailable` | 有界退避并检查 readiness/credential |

单进程 API 并发上限为 32，速率为每秒 20、burst 40。当前这些是单节点防护边界，不是 100x
吞吐或 HA 承诺。
