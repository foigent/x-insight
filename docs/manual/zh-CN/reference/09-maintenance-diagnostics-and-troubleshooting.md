# 维护、诊断与排障

## 先区分三类健康

- `systemctl status`：进程是否由 systemd 运行。
- `/healthz`：HTTP 进程 liveness。
- `/readyz`：数据库可达且产品/schema/PostgreSQL/manual identity 兼容，并且 reader 对五张
  代表性调查表的有界零数据读取通过。

进程 running 但 readiness 失败时不能对外称服务可用。

## 安全诊断命令

```text
sudo /opt/x-insight/current/bin/xinsightctl status
sudo /opt/x-insight/current/bin/xinsightctl access validate \
  --file /etc/x-insight/access-accounts.json
sudo /opt/x-insight/current/bin/xinsightctl recover
sudo systemctl status xinsight.service --no-pager
sudo systemctl status xinsight-feed.service --no-pager
sudo systemctl status xinsight-litellm-feed.service --no-pager
sudo journalctl -u xinsight.service --since "30 minutes ago" --no-pager
sudo journalctl -u xinsight-feed.service --since "30 minutes ago" --no-pager
sudo journalctl -u xinsight-litellm-feed.service --since "30 minutes ago" --no-pager
curl --fail --silent --show-error http://127.0.0.1:8080/readyz
ss -ltn
sudo docker compose -f /etc/x-insight/postgres.compose.yml ps
```

分享输出前逐行审查并移除未批准 host/user/path/identifier；不要收集 environment、credential
文件、数据库 dump、raw snapshot 或完整自由文本日志。当前没有自动诊断上传器。

## 常见问题

| 现象 | 首查 | 不应做 |
| --- | --- | --- |
| owner-supervised delivery 多/少文件或 SHA-256 不一致 | 准确 GitHub workflow、main CI、commit、`owner-supervised-trial-release.json` 和 `SHA256SUMS` | 重新生成 key、删除额外文件后继续或改 checksum |
| `release signature is not trusted` | key ID、trust store owner/mode、active/revoked/time | 替换 digest 或跳过验签 |
| `fresh install refuses existing...` | install-state/current/service/env/launcher 的准确路径 | 删除未知安装状态后重试 |
| `database credentials...invalid` | absolute current link、当前四个普通文件、role/host/db/password 分离；旧三凭据来源只允许由 rc.3 目标控制器在明确 bridge preflight 中识别 | 手工补 workspace DSN、把 DSN 放进环境/日志或绕过 journal |
| `another xinsightctl operation is active` | `status` 与 `recover` | 删除 lock/journal |
| 服务因 access credential 拒绝启动 | 文件必须 absolute、root/service EUID owned、regular、single-link、非 symlink、mode 为 `0400` 或 `0600`；再运行 staging/current 匹配版本的 `access validate` | 打印 JSON/hash、放宽 mode 或创建默认可用账户 |
| `access validate` 报 missing/unknown/duplicate | 严格核对 schema、每个账户五个必填字段（包括 boolean `disabled`）、稳定 subject、规范化 username、role 与 Argon2id 参数 | 猜测默认值、忽略 unknown field 或手工改 validator |
| UI 未登录循环到 `/login` | 核对是否确有 X-Insight HTTPS origin、Secure cookie、产品 Session；匿名 UI 应是 no-store 303，匿名 API 应是严格 401 problem | 用 HTTP tunnel、代理登录页或移除 `Secure` 绕过 |
| 登录 `same_origin_required` / `csrf_validation_failed` | 核对 direct TLS、准确 `Origin`/Host、same-origin fetch 和最新 Session-bound CSRF | 信任任意 forwarded header、关闭 Origin/CSRF |
| 登录 `authentication_failed` | 在 root-only 流程核对账户是否 disabled、username/hash 和服务是否重启；响应故意不披露具体原因 | 从响应推断用户是否存在，或把 password/hash 写入日志 |
| 登录 `authentication_rate_limited` | 遵守 `Retry-After`，等待 per-principal/global window；核对是否有爆破或并发 Argon2 压力 | 并发重试、扩大验证并发或重启循环规避门禁 |
| Session 意外失效 | 核对 30 分钟 idle、8 小时 absolute、operator revoke、账户禁用后的受控 restart/revoke、服务重启 | 把内存 Session 当持久授权或多节点共享状态 |
| readiness `database_unavailable` | PG container、loopback、reader credential、schema | 将 PostgreSQL 暴露公网 |
| readiness `database_unavailable` 且 schema/版本正确 | 核对 reader 对六张代表性表（含 `connector_operational_receipts`）的 `SELECT` grant；逐表恢复准确 reader 权限 | 改为 writer/admin DSN 或跳过代表性读取 |
| `cursor_expired` | 返回搜索页重建快照 | 修改/解码后续用 cursor |
| feed `completed_execution_feed.cursor_expired` | 保留 durable state，核对 UniRouter retention 与受控重新 activation 决策 | 清空/伪造 cursor 或全表 backfill |
| feed `source_status_401` | 核对 feed-only service key 的 active/project/purpose/network binding | 把普通 model key 或 admin key 写进 env |
| feed `contract_*` / `evidence_conflict` | 保留 payload SHA 与 worker status，停止推进 cursor，核对双方准确 contract/commit | 跳过 schema、改写 payload 或手工推进 cursor |
| LiteLLM `source_status_401` / `source_status_403` | 保留 key metadata 与 checkpoint，核对独立 API key、HTTPS endpoint 和 scope，再 restart worker | 把 key 放入 env/argv，或用 UniRouter feed key |
| LiteLLM `source_status_408` / `429` / retryable `5xx` | 核对 bounded backoff、source health 与 20 页/2000 行/8 MiB 窗口 | 重启循环、放宽 window 或并行启动第二 worker |
| LiteLLM `source_*_invalid` / `mapping_scope_mismatch` / `evidence_conflict` | stop worker，保留 raw-free sanitized error、registry identity 和 checkpoint；修复后 restart | 修改 source row、清 checkpoint、跨 scope 复用状态 |
| LiteLLM unit absent after rc.12→rc.11 rollback | 这是受支持的降级结果；env/key/checkpoint 应保留，等待重新升级 | 从旧 release 手工运行 importer 或复制 unit |
| timeline 不完整 | freshness、gap、import receipt、source watermark | 补造缺失 node 或因果 |
| upgrade activation proof 失败 | receipt/journal、listener、version/schema/manual | 手工改 current symlink |

## Receipt 与历史

安装状态、operation journal、receipts、backups 和 immutable releases 位于 manifest/工具定义的
固定 host paths。只通过 `xinsightctl` 状态和恢复动作处理，不能编辑 JSON 把 failed 改成
completed。历史 receipt 是 append-only 证据；新的修复或清理写新记录。

## 升级前后最低核对

升级前保存 current identity、preflight、access credential 只读 validation、evidence
fingerprint、backup path/hash。升级后保存 receipt、`readyz` 的
`representative_read: "ok"`、listener、匿名 UI no-store `303 /login`、匿名 API 严格六字段
`401 authentication_required` 和 fingerprint。rc.14 还要核对准确 external origin、loopback
代理来源、证书剩余有效期与自动续期、HSTS、Secure cookie、authenticated
search/detail/Investigation/Chronology/Proof，以及公网只开放 80/443。不能退化成 HTTP 登录
来补证。application rollback 或 restore 后按目标版本的历史访问合同重新核对：rc.13/rc.14
要求匿名 303/401；rc.12 及更早版本仍使用其历史匿名代表性读取证明，二者不得混用。
