# X-Insight 当前产品手册

本手册覆盖完整开发候选的访问、来源采集、执行调查、费用、运行证据和复核。Apache-2.0 开源不取消认证、来源同意、字段权限、签名与审计要求，没有独立付费功能版本。

X-Insight（XI）可独立用于受支持的数据导入、工作活动报告、调查与复核。UniRouter（UR）独立执行和路由获授权模型请求、应用执行策略、记录用量与经济事实。组合使用时，XI 异步接收获授权的 UR 事实用于分析和审阅，不进入同步请求关键路径。共享协议与 Integration Control 支撑这条连接，不是用户还须另行选择的产品。

## 版本与支持边界

产品 `0.2.0-dev.fpv19`，手册 `0.2.0-dev.fpv19/manual-r1`。

| 项目 | 当前合同 |
| --- | --- |
| 数据库 schema | 45 |
| 数据库 | PostgreSQL 16，同机 loopback 专用数据库 |
| 应用 | Linux amd64；受管安装使用 systemd |
| 安装器运行主机前置 | Ubuntu 22.04 或 24.04 amd64、systemd；精确制品／拓扑资格另行验证 |
| 原文记载的隔离验证配置 | Ubuntu 24.04 guest、原生 PostgreSQL；不代表当前 FPV19 Target 已验 |
| 待取得的原生主机制品资格 | Ubuntu 22.04、专用固定镜像 PostgreSQL16 容器；真正新装证据待取得 |
| 配置 | `xinsight.config.v1` |
| API | `xinsight.investigation.v1`、`xinsight.access.v1` |
| 默认语言 | English；语言控件可切换简体中文 |

这是开发候选，不表示公共稳定发行或 Production 验收。仓库还保留 Ubuntu 22.04 CI 安装路径和冻结 rc.13 profile，其制品与结果分别管理。安装必须匹配精确 profile、已验签制品、信任权威及主机资格，不能照搬历史说明。

安装前先准备文档要求的系统依赖和专用 loopback PostgreSQL16 数据库。所附 Compose profile 使用固定 PostgreSQL 容器，应用仍以原生 systemd 服务运行。全新安装要求空库和未占用的 XI 应用路径及单元名称。若已有 XI，选择受支持升级路径，或按自己的维护／恢复流程先备份并退役精确的旧应用对象。数据重置、测试根映射或仅删除安装状态文件不构成全新安装；共享服务和数据库须保留。FPV19 精确制品在 Ubuntu22.04／容器 PostgreSQL 上的资格仍待验证，已装系统依赖和已完成证据分别记录。

配置声明的升级来源为 `0.2.0-dev.b2`、`0.2.0-dev.d1`、`0.2.0-dev.d2`、`0.2.0-dev.fpv1`、`0.2.0-dev.fpv2`、`0.2.0-dev.fpv3`、`0.2.0-dev.fpv4`、`0.2.0-dev.fpv5`、`0.2.0-dev.fpv11`、`0.2.0-dev.fpv12`、`0.2.0-dev.fpv13`、`0.2.0-dev.fpv14`、`0.2.0-dev.fpv15`、`0.2.0-dev.fpv16`、`0.2.0-dev.fpv17`、`0.2.0-dev.fpv18`。声明只用于兼容性预检，不表示每条升级路径均已完成安装验证。schema45 相同也不能据此回退旧二进制。

## 构建与本地试用

使用 Go 1.25.12、Node 22.23.1、npm 10.9.8、Python 3 和 PostgreSQL 16。在仓库根目录执行：

```sh
go mod download
npm --prefix frontend ci
node scripts/build-manual.mjs --profile=development-d
npm --prefix frontend run build
python3 scripts/collect-third-party-notices.py --check
go build ./cmd/...
TZ=UTC npm --prefix frontend test
```

`docs/community/getting-started.md` 提供一次性数据库及真实 HTTPS 浏览器夹具流程。夹具创建合成数据与测试账户，不是受管安装或真实来源接入。普通前端浏览器套件使用模拟响应，不能代替数据库与集成证据。不得将集成测试指向有价值的数据库，也不要把真实 DSN 放入命令、截图、issue 或 shell history。

干净 checkout 可使用已准备的依赖制作未签名 payload：

```sh
bash scripts/build-signed-release.sh /absolute/new-preflight --profile=development-d --dry-run
bash scripts/build-signed-release.sh /absolute/new-prepared --profile=development-d --prepare-only
```

dry-run 只读检查身份与工具链；prepare-only 构建二进制和 payload 校验和，不签名，也不生成可安装发行包。源码、前端、双语手册、release 配置、兼容矩阵和第三方许可证必须绑定同一 commit。prepare-result 记录该 commit 与逐文件摘要。

## 安装与账户

受管安装需要明确选定且通过资格验证的主机、空的 PostgreSQL16 专用数据库、独立 admin/reader/workspace/importer 角色、批准的服务账户和可信签名制品。目前没有公共稳定安装器或默认生产账户。制品、bootstrap verifier 和公开 trust store 必须通过批准的发布流程取得；不可信下载内附带的 trust store 本身不能建立信任。

独立核对 bootstrap verifier 与信任权威后，验签并解入全新目录：

```text
xinsight-release verify --artifact /approved/candidate.tar.gz --trust-store /etc/x-insight/trusted-release-keys.json --expected-commit EXACT_COMMIT --expected-version 0.2.0-dev.fpv19
xinsight-release extract --artifact /approved/candidate.tar.gz --trust-store /etc/x-insight/trusted-release-keys.json --destination /approved/staging/candidate --expected-commit EXACT_COMMIT --expected-version 0.2.0-dev.fpv19
sudo /approved/staging/candidate/bin/xinsightctl version --json
```

使用已验签 payload 的部署模板。在 `/etc/x-insight/database/current/` 保留四个独立受保护 DSN 文件：`postgres-admin.dsn`、`postgres-reader.dsn`、`postgres-workspace.dsn`、`postgres-importer.dsn`，指向同一个 loopback 专用数据库和不同角色。不得通过命令参数或服务环境值传递密码。主机工具、owner/mode、数据库身份、监听范围和 profile 均须通过控制器检查。

用已验证控制器生成稳定 subject 和密码 hash。hash-password 必须在交互 TTY 隐藏输入两次，不使用 pipe、环境变量或参数：

```text
sudo /approved/staging/candidate/bin/xinsightctl access generate-subject
sudo /approved/staging/candidate/bin/xinsightctl access hash-password
sudo /approved/staging/candidate/bin/xinsightctl access validate --file /etc/x-insight/access-accounts.json
```

账户文件采用 `schema_version:"xinsight.local-accounts.v3"` 与 `accounts` 数组；每条明确填写 `subject_id`、规范化 `username`、`role`、`disabled`、`password_hash`。使用实际生成值，保留已有人员，不填样例凭据。文件必须为 root 所有、0400/0600、普通单硬链接文件、非 symlink。validate 只输出计数，不输出秘密。只读调查使用 `workbench_investigation_reader`，保存复核使用 `workbench_investigation_reviewer`；旧 `investigator` 不自动取得工作台能力。还须单独配置来源/项目 grant。

```text
sudo /approved/staging/candidate/bin/xinsightctl install --check --artifact /approved/candidate.tar.gz
sudo /approved/staging/candidate/bin/xinsightctl install --artifact /approved/candidate.tar.gz
sudo /opt/x-insight/current/bin/xinsightctl status
```

只在预检通过且部署已获授权后运行实际安装。安装创建受管部署身份，不得复制或编辑；首次安装不会自动接入 UniRouter 或启动未配置 worker。应用和数据库保持 loopback；浏览器登录前配置批准的 HTTPS origin 与可信本地 TLS terminator。未登录产品请求跳转登录，API 返回401；cookie 保持 Secure/HttpOnly/SameSite。浏览器在普通 HTTP 上拒绝提交密码。

## 首次来源接入

管理员从连接与接入开始设置，核对 XI 部署、UniRouter 安装、来源项目、采集能力及留存选择；通过邀请进入 UR 并在 UR 完成同意。历史同意不能覆盖当前来源策略。邀请秘密仅临时使用，不写入 issue 或持久浏览器存储。

设置页区分等待同意、准备连接、等待数据和当前问题。配置已保存不等于进程已加载，已加载不等于实际收到数据。核对来源状态与一条来源身份一致的执行记录。XI 异步接收事实，不是 UR 调用准入的同步依赖。

不存在 XI 留存策略时，设置草稿提出完整正文、90天、每项目5GiB、单条32MiB，须明确确认；已有生效或停用策略继续复用。UR 来源采集独立批准，正常草稿为完整正文、30天、每项目5GiB、单条32MiB，并受实际协议与观察点支持限制。元数据采集不默认授予正文采集或阅读权。企业策略变更须有相应管理员权限，项目/用户例外继续有效。

中断后返回同一请求，查看当前状态再重试；浏览器只保留当前主体的非秘密请求引用。邀请过期须重新审阅同意。不要新建替代来源来掩盖原连接失败。有效控制凭据在原 installation 上续期；跨期恢复要求两端管理员和来源保留的原 secret。撤权或范围漂移不自动恢复。

## 普通用户的范围授权

角色能力与 active grant 同时生效。grant 按能力与范围取并集，窄 grant 不能限制另一个宽 grant。UR 来源项目使用 `project-<project_id>`，不是裸数字。以下只示范形状，所有身份与范围都应替换为实际批准值：

```json
{
  "schema_version": "xinsight.workbench-grant-change.v1",
  "action": "create",
  "grant_id": "11111111-1111-4111-8111-111111111111",
  "subject_id": "xs_1111111111111111",
  "source_system_id": "22222222-2222-4222-8222-222222222222",
  "source_project_ref": "project-42",
  "cost_project_ref": "replace-with-actual-cost-project-ref",
  "expected_revision": 0,
  "capabilities": ["summary", "details"]
}
```

```text
sudo xinsightctl workbench-access apply --file /approved/grant.json --check
sudo xinsightctl workbench-access apply --file /approved/grant.json
sudo xinsightctl workbench-access show --grant-id 11111111-1111-4111-8111-111111111111
```

命令使用受保护 admin 访问。check 只读，apply 重新核对当前状态并将变更与审计原子提交。已有 grant 必须读取当前 revision 后完整 replace 或显式 revoke，不重放旧 create。预算专用能力为 `budget_definition`、`budget_summary`、`budget_alert`，使用准确来源项目和 `cost_project_ref:"*"`，与受限费用明细 grant 分开；预算权不授予所有执行明细。

保留执行关联的资源复核还需准确目标方、资源报告方范围，以及相关 `source_context`、`source_identity`、`source_cost` 字段。预览与提交检查相同的当前权限。C 内容 grant 还携带明确用途，不能用 v1 样例覆盖 v2 内容 grant。权限变化使旧 reading 失效，应建立新阅读，不反复提交旧页面。

## 调查、费用与复核

从总览或使用/目的、运行/执行、安全/复核、费用/使用、来源/覆盖专题开始，选择期间和获准项目后打开执行。先核对来源、execution ID、reading ID、截止、覆盖与证据引用。同一 reading 固定阅读依据，后到来源修订不改写旧阅读。

沿执行查看运行请求、资源、来源事实和相关工作。运行结果与经济事实是不同集合：扣费失败或缺失不能证明执行失败，缺少资源事件不能证明未访问。提供方请求表示提交了什么，不表示模型内部实际使用。相关工作只有真实关系依据满足条件时才合并。

费用保留原币种与精度，不相加不同币种、重叠的来源总额与 XI 明细或独立快照。预算计划差额不是现金余额、预留或调用许可。来源周期保留原 UTC 边界，全局近7天不能改写来源账期。

趋势连接相邻、有效、有已知值的数据点，包括覆盖不完整时已记录的0；缺失、无效或不相邻区间断线。观察到0不证明真实无活动。正负费用分别保留，负值可能来自修正。用鼠标或键盘读取每点准确期间与值。切换语言不改变时区与 ISO 证据时间戳。

复核前检查选中证据与精确关联预览，再保存。reader 不因宽数据 grant 获得写权。失权、过期、无效输入和服务失败分别呈现。保存后打开已存复核并返回原 reading，核对实际保存引用和当前可访问结果，不根据一次点击或旧面板推断成功。语言切换保留主体草稿、筛选和复核引用；来源原文、标识符、协议及 API code 不翻译。

## 正文、留存与缺口

正文采集与阅读分别显式批准。读取要求当前权限、精确用途/引用和未过期副本。正文到期或撤权后，仍获准的元数据可以保留；未知、不可用、无权、不支持和过期不等于空或0，不从旧页面补造正文。

正文密钥须同时由 API 与相关 worker 实际加载，文件存在不等于加载成功。已有密文却丢失原密钥时，必须停止并恢复原件，不能重新生成替代。完整私有备份包含数据库和原正文密钥，按敏感资料保护。

## 升级、恢复与诊断

升级前核对下一制品并解出它的控制器，用目标控制器验证准确前态，不使用旧安装控制器驱动跨版本升级。

```text
sudo /approved/staging/candidate/bin/xinsightctl upgrade --check --artifact /approved/candidate.tar.gz
sudo /approved/staging/candidate/bin/xinsightctl upgrade --artifact /approved/candidate.tar.gz
sudo /approved/staging/candidate/bin/xinsightctl status
```

预检不得修改服务、数据库、current release 或操作记录。实际升级需要批准窗口、精确前态、已核对备份和磁盘余量；保留原 installation、来源身份、授权、游标、密钥和历史事实。同 schema 升级仍需制品与服务生命周期资格。

```text
xinsightctl backup --output /absolute/private-backup.tar --check
xinsightctl backup --output /absolute/private-backup.tar
xinsightctl restore --recovery-bundle /absolute/private-backup.tar --recovery-bundle-sha256 sha256:EXACT_DIGEST --new-database xinsight_restore_approved --check
```

不要覆盖有价值的数据。核对私有备份精确摘要及新的专用数据库/安装目的地，再使用相应批准恢复流程。恢复到其他部署会撤销恢复数据库内的原运行身份和令牌，历史保留，重新接入走当前授权。过期正文仍不可读，历史数据库单独备份不能证明正文密钥已恢复。

操作中断后保留原身份，先看状态，再按该 operation 所需参数使用 `xinsightctl recover` 或 `xinsightctl resume`，不要当作通用重试按钮。分别诊断签名/版本、credential owner、数据库身份/schema、缺密钥、授权拒绝、来源过期和网络问题，不降低检查绕过阻塞。

报告问题时提供准确 commit/profile、系统与工具链、脱敏复现、预期/实际结果，并说明 Local、CI、Artifact、Target 或 Production 证据层。公开 issue 不包含真实正文、DSN、token、密码 hash、私有主机身份或原始日志。仓库私有漏洞报告能力尚未确认启用；可不带漏洞细节请求私下联系。贡献与手动 DCO 签署见根目录 `SECURITY.md` 和 `CONTRIBUTING.md`。
