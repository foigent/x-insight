# 安装与首次启动

本章描述受控现场候选的单机安装，不是面向未知环境的一键安装器。安装由 Codex 与产品 owner
执行；需要主机、访问或策略事实时向客户 IT 提出明确要求。

> 第一客户采用 `owner_supervised_trial`：产品 owner 不创建、不保存、也不输入签名私钥。
> Codex 从准确 GitHub `foigent/x-insight` 的 `main`/CI 发布记录取得并核对安装包，客户不能自行升级。

## 安装前客户条件

- 一台 Ubuntu 22.04 x86_64 私有主机，root/sudo、systemd、`ss`、Docker Engine 与 Compose plugin。
- 已通过批准私有路径提供准确的 owner-supervised delivery 和 pinned PostgreSQL 16 image。
- PostgreSQL 16 client 的 `pg_dump`、`pg_restore`、`createdb` 可用且 major 为 16。
- X-Insight 应用和 PostgreSQL 不开放公网监听；如需现场公网登录，只允许经批准的同机 HTTPS
  terminator 暴露 80/443，并固定唯一 external origin。
- 已确定 trial data custodian、sanitized 数据范围、七天退出保留和现场时间窗。

## 第一客户的信任引导

你不需要生成密钥、保管 U 盘或执行签名命令。Codex 在 GitHub 手动发布流程中完成以下工作：

1. 只接受准确的当前 `main` commit 和同一 commit 已成功的 `CI` push run；
2. 自动生成一次性 Ed25519 key 并构建 signed-v2 release；
3. 在组装交付目录前删除私钥；
4. 只交付 artifact、bootstrap verifier、公开 trust store、build result、边界说明、`SHA256SUMS` 和 `owner-supervised-trial-release.json`；
5. 在本地和目标主机分别核对 commit、workflow run 与 SHA-256。

这套公开 trust store 只证明本次受监督交付，不会授权未来版本。第二客户、第三方安装或无人值守/
自助升级前，必须另行建立持久 KMS/HSM、keyless 或 offline 发布权威。

交付下载后先核对严格文件集与 checksums。下面命令由 Codex 执行，产品 owner 只确认目标版本和
现场窗口：

```text
sha256sum --check SHA256SUMS
chmod 0755 xinsight-release-linux-amd64-COMMIT
sudo install -o root -g root -m 0644 trusted-release-keys.json /etc/x-insight/trusted-release-keys.json
```

`owner-supervised-trial-release.json` 中的 artifact、bootstrap 和 trust-store SHA-256 必须与准确
GitHub workflow run 记录以及目标主机计算结果一致。任一不一致都停止安装。私钥文件不在允许的
七文件 delivery 中；发现额外文件也停止。`xinsightctl` 不单独引入，而是从已验签 release
payload 中获得。

## 验签并解出现场 payload

先用已核对 hash 的 bootstrap 工具做只读验签，再解到一个尚不存在的绝对目录；`extract` 拒绝覆盖已有
路径，并在验签失败时不解包：

```text
xinsight-release version --json
xinsight-release verify --artifact /approved/path/xinsight-v0.3.0-rc.13-linux-amd64-COMMIT.tar.gz --trust-store /etc/x-insight/trusted-release-keys.json --expected-commit COMMIT --expected-version 0.3.0-rc.13
xinsight-release extract --artifact /approved/path/xinsight-v0.3.0-rc.13-linux-amd64-COMMIT.tar.gz --trust-store /etc/x-insight/trusted-release-keys.json --destination /approved/staging/xinsight-0.3.0-rc.13-COMMIT --expected-commit COMMIT --expected-version 0.3.0-rc.13
```

随后核对 staging 内 `bin/xinsightctl version --json` 与 manifest 完全一致。后续模板和安装命令
都只使用这个已验证 staging，不从其他 archive 或工作区复制。

## 准备服务账号与 PostgreSQL

创建无登录的 `xinsight` 系统账号。把发布包内经过验证的 `postgres.compose.yml` 和
`postgres.env.example` 作为模板放到 `/etc/x-insight`。`postgres.env` 必须为 root-only，
将所有 `REPLACE_WITH_FIELD_SECRET` 替换为现场生成的唯一强密码。

```text
sudo useradd --system --home /var/lib/x-insight --shell /usr/sbin/nologin xinsight
sudo install -d -m 0755 /etc/x-insight /var/lib/x-insight /data/x-insight
sudo install -d -m 0700 /etc/x-insight/database/sets/initial
sudo install -d -m 0700 /data/x-insight/postgres
POSTGRES_IMAGE='postgres:16.14-alpine3.24@sha256:57c72fd2a128e416c7fcc499958864df5301e940bca0a56f58fddf30ffc07777'
sudo docker image inspect "$POSTGRES_IMAGE" >/dev/null
POSTGRES_UID="$(sudo docker run --rm --entrypoint id "$POSTGRES_IMAGE" -u postgres)"
POSTGRES_GID="$(sudo docker run --rm --entrypoint id "$POSTGRES_IMAGE" -g postgres)"
if ! [[ "$POSTGRES_UID" =~ ^[1-9][0-9]*$ ]] || ! [[ "$POSTGRES_GID" =~ ^[1-9][0-9]*$ ]]; then
  echo '固定 PostgreSQL image 未返回有效的 postgres UID/GID' >&2
  exit 1
fi
sudo chown -- "$POSTGRES_UID:$POSTGRES_GID" /data/x-insight/postgres
sudo docker compose -f /etc/x-insight/postgres.compose.yml config
sudo docker compose -f /etc/x-insight/postgres.compose.yml up -d
```

如果目录已存在，不重复创建或覆盖；先核对 owner、mode 和内容。PostgreSQL bind mount 的
owner 必须与固定 image 内的 `postgres` UID/GID 一致，不能凭其他 tag 的 UID 猜测，也不能
使用递归 `chown`。如果 `docker image inspect` 失败，先通过批准路径取得并核对上述完整 digest，
不要改用浮动 tag。

在 root-only 编辑器中分别创建下列四个单行 DSN 文件，密码必须互不相同，主机必须是
`127.0.0.1`，数据库必须是同一个空的专用 `xinsight` 数据库：

```text
/etc/x-insight/database/sets/initial/postgres-admin.dsn
/etc/x-insight/database/sets/initial/postgres-reader.dsn
/etc/x-insight/database/sets/initial/postgres-workspace.dsn
/etc/x-insight/database/sets/initial/postgres-importer.dsn
```

对应用户固定为 `xinsight_admin`、`xinsight_reader`、`xinsight_workspace`、
`xinsight_importer`。将四个文件设为
`0600`、目录设为 `0700`，再让 `/etc/x-insight/database/current` 成为指向该绝对目录的
符号链接。不要把 DSN 放进 shell history、systemd Environment 或产品日志。

## 准备 root-only 本地账户 credential

当前版本在任何 `install --check`、`adopt --check` 或 `upgrade --check` 前都要求准确
`/etc/x-insight/access-accounts.json`。它不是发布包内的默认账户文件，也不能由 Web 首次启动
向导生成。只使用已验签 staging 内的 CLI，在目标主机交互式生成稳定 subject 与 Argon2id
hash：

```text
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl access generate-subject
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl access hash-password
```

`hash-password` 必须在交互 TTY 隐藏读取两次密码；不要使用 pipe、stdin redirect、
environment、命令参数、command substitution、共享 clipboard 或 transcript。随后在同一
主机的 root-only 编辑流程中按“配置、安全与访问”的严格 JSON 形状创建文件，并核对它是
root:root、regular、single-link、非 symlink、`0600`。占位 hash、空账户集、缺失
`disabled` 或默认可登录账户都不允许。最后只输出非秘密计数做校验：

```text
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl access validate \
  --file /etc/x-insight/access-accounts.json
```

若此 credential 缺失、权限/identity 漂移或 JSON 不严格，preflight 必须在创建 operation、
备份、迁移、切换 release 或启停服务前停止。

## 只读检查与安装

使用已验证 staging 内的工具再次验证 artifact，不在 preflight 失败后继续：

```text
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl version --json
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl verify --artifact /approved/path/xinsight-v0.3.0-rc.13-linux-amd64-COMMIT.tar.gz
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl install --check --artifact /approved/path/xinsight-v0.3.0-rc.13-linux-amd64-COMMIT.tar.gz
sudo /approved/staging/xinsight-0.3.0-rc.13-COMMIT/bin/xinsightctl install --artifact /approved/path/xinsight-v0.3.0-rc.13-linux-amd64-COMMIT.tar.gz
```

`install --check` 要求准确 Ubuntu/CPU、可信签名、空 PostgreSQL 16 数据库、四个独立角色
凭据、严格本地账户 credential、host tools、磁盘余量和无既存 managed/unmanaged 安装。
任何失败都应先解决明确检查，不能通过改名、忽略 digest 或放宽 listener 绕过。

成功的首次 `install` 还会以 root-only `0600` 创建
`/etc/x-insight/xinsight-connector-id.env`，其中只有一次生成的
`XINSIGHT_CONNECTOR_DEPLOYMENT_ID=xid_...`。不要手工创建、复制或编辑该 identity 文件，也不要在
后续配置、升级、回滚或重升级时替换该值。此时 UniRouter target 尚未配置，Connector invitation 与
Feed worker 都保持 inactive；安装不会 enable/start worker，也不会发起 UniRouter 请求。

## 首次启动验证

```text
sudo /opt/x-insight/current/bin/xinsightctl status
sudo systemctl status xinsight-connector-controller.service --no-pager
sudo systemctl status xinsight.service --no-pager
curl --fail --silent --show-error http://127.0.0.1:8080/readyz
curl --silent --show-error --max-redirs 0 --dump-header - --output /dev/null http://127.0.0.1:8080/executions
curl --silent --show-error --max-redirs 0 --dump-header - http://127.0.0.1:8080/api/v1/executions?limit=1
ss -ltn
```

只允许 `127.0.0.1:8080` 和 `127.0.0.1:5432`；`0.0.0.0`、`::` 或公网地址是阻断。
`xinsight-connector-controller.service` 必须先于 Web active，固定 Unix socket 必须为
`root:xinsight`、`0660`，且 controller 只加载 admin DSN credential。`readyz` 必须同时匹配产品、schema、PostgreSQL major、手册版本和
`representative_read: "ok"`。匿名 `/executions` 必须只返回 no-store
`303 Location: /login`；匿名
Investigation API 必须返回 `401 authentication_required` 且不含 Evidence。安装 receipt
还必须有 `representative_read` 与 `anonymous_access_boundary` 两项 `pass`。

这一步只证明 loopback readiness 和匿名访问门禁。若启用同机 TLS terminator，先从已验签
payload 的 `trusted-tls-proxy.env.example` 与 `xinsight-nginx-ip.conf.example` 生成两个
target-specific root-owned 文件，不修改模板本身：

```text
/etc/x-insight/trusted-tls-proxy.env
/etc/nginx/sites-available/x-insight
```

前者只能含 `XINSIGHT_TRUSTED_TLS_PROXY_MODE=loopback` 和唯一规范化
`XINSIGHT_EXTERNAL_ORIGIN=https://HOST_OR_IP`。Nginx 必须覆盖 forwarded proto/host、清空
通用 `Forwarded`、只代理到 `127.0.0.1:8080`，并把 HTTP 限定为 ACME challenge 后重定向
HTTPS。证书必须公开可信且自动续期；若使用 Let’s Encrypt IP certificate，它是约六天的
short-lived certificate，必须核验续期 timer 与 reload hook。公网安全组和 UFW 只允许
80/443，应用 8080、PostgreSQL 5432 和 SSH 不新增公网规则。

只配置上述 TLS/external origin 不会启用 Connector，也不会阻止 Web 启动。完整 Connector target
配置另见“配置、安全与访问”：使用 root-only `connector-target-config set` 保留 installer 生成的
identity 文件，并在独立 `/etc/x-insight/xinsight-connector.env` 原子写入批准的 UniRouter HTTPS
origin；不要直接编辑任一 managed Connector 文件。缺少 target 时系统明确保持 disabled。

Target 验收必须从外部浏览器证明 `/login`、Secure/HttpOnly/SameSite cookie、HSTS、同源
login/logout、authenticated search/detail/Investigation/Chronology/Proof、匿名 303/401、
证书有效期和续期 dry-run。HTTP SSH tunnel、localhost HTTP、一般私网或代理自身登录页都不能
建立该证明。手册身份从已验签 payload 的
`/opt/x-insight/current/manual/zh-CN/index.html` 核对。
