# XI-C 责任登记子项：实施与验证记录

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

状态：**independent-review-16 已接受 `03fe6147830f8c25316a8537e292e5ce77c4246c` 的责任、期限和原子历史完整子项，限 Local 合成层；review-15 的 P2 页面恢复问题关闭。父级 XI-C、SE-03/GX-01 与原 13 项未通过。** 下文保留当时实施和补交证据；后续 Connectors 权限修复单独记录。

## 身份与边界

- 权威：经理 `independent-review-14/manager-decision.md`，SHA256 `91d5c10ada86d9e3f8d785705f0b03d927db88a247b102cf5b0628b9966af05b`；实施方案见同目录 `xi-c-responsibility-history-proposal-2026-09-09.md`。
- 基线 `0d7c3a38272e32cc4d6f5ab6fe5fe17d931ae034`；工作树 `/private/tmp/xinsight-xi-c-responsibility-20260909`，分支 `codex/xi-c-responsibility-history`。`e4eb27da4f043ca40f6d8d083174331a066734ae` 是此前资源阻塞 WIP，不能当作本次交付。
- 当前补交由 `output/xi-c-responsibility/recovery-evidence-binding-20260910.json` 绑定；旧 `final-evidence-binding-20260910.json` 只绑定 e1c72db 首轮对象，不能套用于新页面。最终 commit/tree 以本次本地提交和经理通知为准，避免自身 commit 循环。
- 唯一 XI writer，原用户工作树未改；未使用新任务/agent，未联系 UR，未调用真实来源 API、凭据或用户服务；无 push/PR/merge/release/deploy。经理已明确授权完成包与实质阻塞的主动通知。

## 2026-09-10 页面恢复补交（independent-review-15）

已完整读取经理意见（SHA256 `277d4eaf9d88a3e85f50d6e0ecd72b939b63bc5764f9adff117aff30ad20aa9b`）及 review-summary.json，认可反例：责任 v1/Case29 的草稿遇另一 Finding 将 Case 更新到30，服务端409正确拒绝，但旧页面仍展示“重试保存”。本次只改 `FindingResponsibility.tsx`、对应测试及重新生成的嵌入资产；API、事务、迁移、角色与恢复实现未变。

| 实际结果 | 当前提示与恢复动作 | 已执行证据 |
| --- | --- | --- |
| 明确409版本冲突 | 明确本次未保存，保留锁定草稿供核对，主动作“重新读取并核对”，无携旧版本的提交按钮；重读后按当前值重新作决定，使用新操作身份。Case sealed 的明确409另给封存提示，重读后不再开编辑器。 | 浏览器 `known-conflict.log`、`conflict-reread-save.log`：精确复现责任v1/Case29→30，冲突未增加目标历史；重读后 expected Finding=1/Case=30，保存v2成功。组件另覆盖封存分支。 |
| 提交响应未知 | 提示尚未确认结果，锁定内容，主动作“原样重试同一操作”；保留原 operation_id 与完整请求。 | `unknown-response-retry.log`：真实v3提交后中断响应，再真实追加v4；原样重试返回v3完全相同字节，显示当前v4，历史4条，未重复且未用旧回执覆盖当前值。 |
| 已确认保存，当前读取失败 | 明确已保存的责任版本；主动作“重新读取当前记录”，只发GET，不再POST。 | `confirmed-read-recovery.log`：v5已保存后故意中断真实读取，恢复后当前v5/历史5；包含保存及恢复全过程仅1次POST。 |
| 恢复时权限失效 | 清除当前投影、历史、草稿、原回执提示和恢复动作，沿用统一401跳转。 | `confirmed-recovery-revoked.log`：v6已保存但读取失败后实际logout204；恢复GET401→login，整个过程仅1次POST。组件另验证403清除。 |

以上浏览器日志在 `output/playwright/xi-c-recovery-20260910-01/`，使用新的合成数据库、production TLS handler、正式runtime roles和本次新嵌入资产；route.fetch 先请求真实服务器，再有意中断交付，不用伪造成功响应替代提交。三张状态截图已实际查看；完整控制台仅出现预期401/409和受控connection reset。harness正常PASS，任务Chrome关闭，专用PG49998停止，数据保留。

`recovery-frontend-tests-20260910.log`：6文件69测试通过（责任组件8项，含401/403）；`recovery-frontend-build-20260910.log`：TypeScript/build通过，3项精确asset manifest，JS为 `index-5B-eCnli.js`；`recovery-webui-20260910.log`：嵌入资产Go检查通过。初次定向5项结果保存在 `recovery-component-20260910-01.log`，随后新增3项按完整69项复验；本轮没有产品测试失败。

执行环境和资源预算沿用下文。实际命令：固定Node/npm运行 `--prefix frontend test`、`--prefix frontend run build`；固定Go运行 `go test ./internal/webui -count=1`。真实浏览器使用原 `TestResponsibilityProductionBrowserHarness`，`XINSIGHT_RESPONSIBILITY_BROWSER_OUTPUT` 指向上述新目录；其他DSN/工具链/资源guard和opt-in沿用既有受支持入口。没有重跑未变的数据/迁移历史：经理已在e1c72db独立PG复验10项，并另验实际PG进程重启的原回执/封存包逐字节保留；复用条件是本次后端、schema和历史迁移字节一致，新binding逐一核对。

Connectors另见 [独立调查与修复计划](xi-connectors-digest-investigation-proposal-2026-09-10.md)。本轮只读确认两个digest重载在合成库的runtime ACL均无EXECUTE，以及相关查询/migration23/ApplyGrants函数正文与责任前基线相同；没有旧基线运行复现，不把静态对比说成引入定位。未授权实施该计划，未扩大任何函数权限或改变哈希语义。TD-02/04、GX-01与全部父级剩余项保持。

下文保留e1c72db首轮实现与证据；首轮“已完成Local”不等于经理已验收，页面恢复行为以本节和新binding为准。

## 交付行为

既有 Finding 卡片增加“责任与期限”。可选择已加载本地账号、明确未分配、可选期限/IANA 时区和修改依据；区分从未记录、未分配、未设期限、账号失效与登记期限已过。登记不赋予访问权、不发送通知、不修改原 classification/status，也不表示风险已消除或已验证修复。

migration 26 增加当前投影和追加历史两表。服务端绑定 human、时间及有限动作；Case 父行锁串行化新动作和封存，投影/历史/回执/Case 版本同事务保存。同账号、对象与 operation_id 的相同规范化请求返回原回执；异内容冲突。原回执与当前投影分开读取；原操作重试先找已提交回执，再检查新动作的 open/版本/新负责人条件，每次 HTTP 仍重新鉴权。

新封存包使用 v2，保存责任投影、当时显示名及完整有序历史，不夹带当前账号可用性。v1 原 JSON 字节、摘要和 HTML 表达继续保留；未知版本显式拒绝。既有 1—25 的 50 个迁移文件、schema 23 身份和 rc.10 权限常量保留。down 不删除责任历史，恢复沿用正式备份/新库恢复路径。

当前 schema 合同已由真实迁移重新生成并替换为 26：45 tables、524 columns、332 constraints、132 indexes、5 triggers、5 functions；语义 SHA256 `72c2211a45b39958789aa3480f8fe74c01e99ee9e7bd2e2b1604ccb14e21d442`。候选 `schema-26-20260910-01.json`；旧 `schema-26.json` 是过期历史，不作为本次合同。

## 实际验证与证据

以下日志除浏览器项均在 `output/xi-c-responsibility/`。无 DSN 的包回归与专用 PG 用例分开，未运行大数据/A0 负载。

| 验证 | 结果与原始证据 |
| --- | --- |
| 全新库及 25→26 | `responsibility-postgres-20260910-01.log`：旧 Finding 无责任补推；原 classification/status、v1 非规范化原字节/hash/HTML 不变；未知 v3 JSON、HTML、恢复均拒绝。 |
| 事务/重试 | 同上：两连接同键同内容仅一条历史；不同键同版本冲突；同键异内容一个成功一个冲突；延迟提交约束失败及取消全部回滚；响应提交后丢失，重试返回原字节。 |
| 封存竞争 | 同上：责任先提交/封存先提交双向竞争，以及旧备注与封存竞争；新动作不得写入 sealed Case，已提交原动作仍能重放。 |
| 实际权限 | 同上及 `roles-20260910-02.log`：production workspace/importer/reader 角色与非 superuser 连接；历史 UPDATE/DELETE/TRUNCATE 拒绝，独立 ACL 检查无权；实际 Agent、无写权会话、伪造 actor、跨 Case、错/无 CSRF、跨源、有限输入和已撤销 Cookie 拒绝。 |
| 账号加载 | 同上：当前账号状态以已加载配置为准；替换 access service 后停用/移除生效，历史身份/显示名及原回执保留；未替换的 provider 不假装即时感知变更。 |
| 原生备份恢复 | 同上：PG16 `Backup/Verify/RestoreToNewDatabase` 实际小库备份约 235957 bytes；恢复后 schema/roles、投影/历史、原回执和封存摘要一致；损坏投影、请求摘要、封存摘要被拒绝；只读验证与 PreviewCleanBaseline 前后 schema/事实 fingerprint 不变。 |
| 普通应用重启 | `restart-legacy-regression-20260910.log`：同库关闭并重新创建 WorkspaceStore、production handler/access service；旧 session 401，新 human 登录后原回执字节完全一致，版本 1/一条历史保留。此项没有重启 PG 进程。既有 canonical Case/Pack/Agent/Discovery 持久化测试也通过。 |
| Go 回归 | `go-regression-20260910.log`：workspace/access/api/db/buildinfo/webui 通过；hostctl 环境错误分类见下。修正任务临时目录 group 后，完整 hostctl 和新嵌入 webui 见 `hostctl-regression-20260910-02.log`，全部通过。`go-vet-20260910.log` 退出 0。 |
| 前端/接口合同 | `frontend-check-20260910.log`、`frontend-tests-20260910.log`：TypeScript 及 6 文件 66 测试通过；`frontend-build-20260910.log` build 成功并生成精确 3 项 manifest；当前 OpenAPI 54 必需路径及 DTO 校验见仍适用的 `openapi-resource-pause.log`。 |
| 浏览器真实生产链 | `browser-harness-20260910.log` PASS；TLS production handler、runtime roles、新嵌入 UI，两个独立 Chrome profile、实际 human session。具体行为见下表，未以 mock API 代替。 |

浏览器证据目录：`output/playwright/xi-c-20260910-01/`。所用数据均为本任务合成数据。

| 场景 | 结果/日志 |
| --- | --- |
| 保存与刷新、时区 | `save-refresh.log`：DST 重复时间必须明确选择，UTC-05 对应 `2026-11-01T06:30:00Z`；保存并刷新后身份、期限与时区一致。不存在的当地时间曾在实际页面验证拒绝；定向时间测试另覆盖此边界。`responsibility-saved.png` 已实际查看，内容可读完整。 |
| 历史 | `history.log`：23 条，首批 20、第二批 3，固定 through_version=23，无遗漏/重复，末页无 next。 |
| 失效与两人冲突 | `disabled-and-draft.log`、`second-human-save.log`、`two-human-conflict.log`：失效账号仍显示历史身份；第二名 human 保存 v2，第一人旧 v1 草稿 409；显式重读、新 operation 后保存 v3 未分配。 |
| 丢响应且已有更新 | `lost-response-newer-current.log`：真实 POST 提交 v4 后丢连接，受控后续真实保存 v5；页面原身份重试得到完全相同的 v4 回执，并独立读取 v5，明确显示“动作已保存为责任版本 4；当前版本 5。”，历史仍 5 条。后续 v5 控制动作使用第一名 human 会话，未冒充第二名操作者。 |
| 导航竞态 | `navigation-race.log`：延迟旧 Finding 的实际 200 响应，导航第二 Case 后释放；第二 Case 未记录状态未被旧响应覆盖。 |
| 封存精确包 | `sealed-pack-02.log`：revision 34，JSON/HTML 200，3 条责任投影/29 条动作；精确下载 JSON 摘要与 sealed_digest 相同：`sha256:89d45586124cf9646fe6d2e80ba3eadccf75b1268c59a606d505350212809c55`。历史失效 subject 保留，封存无编辑按钮。 |
| 撤销与被指派 reader | `revoked-session.log`：实际 logout 204 后 review 401，缓存责任和历史清除；`assigned-reader-forbidden-02.log`：reader 打开 Case 被现有 UI 路由至 workbench，实际 Case/review/history HTTP 全为 403，责任登记未赋权。 |

## 失败分类与已知独立问题

- 历史 `compile-initial.log` 的 GOSUMDB=off 与工具链校验冲突，去掉该设置后复用已有 Go；未安装工具或绕过校验。早期 HTTP client/Jar 和前端 DOM 清理等测试输入修正及原失败日志保留于 WIP；后续受影响真实测试已通过。
- `schema-resume-01.log` 中真实 schema 生成测试已经 PASS，wrapper 随后遇到 Go 正在删除临时子目录导致 `du` 报不存在。guard 只容许已消失后代的这一错误，仍验证计量根路径并拒绝其他计量错误，异常时结束自己的 child process group；离线正常/消失/权限错误分类和后续实际运行已检查。未为此重复生成整套 schema。
- 首轮角色用例预期权限错误 42501，reader 实际先返回只读事务 25006。用例接受两种拒绝，并增加独立 has_table_privilege=false 断言；产品权限未放宽，定向复验通过。
- 首轮完整 hostctl 有 24 项因为本任务 TMPDIR 属组 0、测试进程组 20，触发现有所有权保护。仅将本任务新建 `.../runtime.../tmp` 的 group 改为进程 group 20，新子目录继承正确；`tmp-group-repair.json` 与完整复验保留。没有递归 chown、共享路径或产品保护变更。
- 浏览器 `sealed-pack.log` 曾等候错误的精确显示文本而超时，正确显示包含 subject ID；`assigned-reader-forbidden.log` 曾假设 UI 会发 Case 请求而超时，实际现有路由已将 reader 转向 workbench。修正观察假设后的 `-02.log` 保存真实断言；初次失败未覆盖。
- 浏览器 Console 的 401、403、409、connection reset 对应本次有意验证的边界；合成自签名证书握手日志亦保留。另有两次 `/api/v1/connectors` 500，PG 日志明确为 `permission denied for function digest`，查询来自既有 `internal/db/connector_trust_store.go`。本片未改该查询，未添加 digest EXECUTE；记录为独立既有路径权限问题，未将 Connectors 或全产品判定通过。原始摘录见 `connector-permission-observation-20260910.log`。此问题不影响本片责任路径已完成的实际操作。

## 命令、资源和结束状态

Go 1.25.11；`GOTOOLCHAIN=go1.25.11 GOCACHE=/private/tmp/xinsight-go-cache GOPROXY=off`，TMPDIR/GOTMPDIR 均为 `/private/tmp/xi-c-responsibility-runtime-20260909/tmp`，不设置 GOSUMDB=off。真实 PG 测试额外使用 `XINSIGHT_RESPONSIBILITY_LOCAL_TEST=1` 和专用 loopback 49998 的 synthetic admin DSN；没有真实凭据。PG16.14 工具来自 `/opt/homebrew/opt/postgresql@16/bin`。可复跑的主要命令（前两项为汇总入口，实际分次执行结果见上表）：

```sh
python3 scripts/xi-c-resource-guard.py -- go test ./internal/api -run '^TestResponsibility' -count=1 -timeout 5m -v
python3 scripts/xi-c-resource-guard.py -- go test ./internal/workspace ./internal/access ./internal/api ./internal/db ./internal/hostctl ./internal/buildinfo ./internal/webui -count=1
python3 scripts/xi-c-resource-guard.py -- go test ./internal/hostctl ./internal/webui -count=1
python3 scripts/xi-c-resource-guard.py -- go test ./internal/api ./internal/db -run '^(TestResponsibilityOrdinaryApplicationRestart|TestWorkspaceStorePersistsCanonicalCasePackAgentAndDiscoveryState)$' -count=1 -timeout 90s -v
python3 scripts/xi-c-resource-guard.py -- go vet ./internal/workspace ./internal/access ./internal/api ./internal/db ./internal/hostctl ./internal/buildinfo ./internal/webui
```

前两类命令分别按上文有/无 DSN 分开；schema opt-in 为 `XINSIGHT_RESPONSIBILITY_SCHEMA_OUTPUT=/private/tmp/xinsight-xi-c-responsibility-20260909/output/xi-c-responsibility/schema-26-20260910-01.json`；浏览器 opt-in 为 `XINSIGHT_RESPONSIBILITY_BROWSER=1`、`XINSIGHT_RESPONSIBILITY_BROWSER_OUTPUT=/private/tmp/xinsight-xi-c-responsibility-20260909/output/playwright/xi-c-20260910-01`。复跑需使用新输出路径，保留本次候选和证据。浏览器 harness 使用 `-run '^TestResponsibilityProductionBrowserHarness$' -count=1 -timeout 30m -v`，完成后写其唯一输出目录的 stop，正常 PASS 并精确清理新测试数据库。

前端使用现有缓存 Node 22.23.1/npm 10.9.8：`PRIVATE_HOME/.npm/_npx/ed214ae0ceb6e0e4/node_modules/node/bin/node` 与同目录树 `npm/bin/npm-cli.js`；执行 check、test、build。复用已复制依赖，无安装。真实浏览器使用既有 `@playwright/cli` 缓存及已安装 Chrome；两个 profile/daemon/cache 都在专用 runtime，只有筛选的非秘密证据进入提交。

资源曾于 2026-09-09T12:10Z 因主机空闲低于 20 GiB 停止；经理 2026-09-10 00:35（上海）确认恢复窗口后继续。恢复用原生 pg_ctl 启动保留数据；旧 start-pg.py 是 init-only，不能用于恢复。仍守总新增 1 GiB、主机空闲至少 20 GiB，不下调 A0 的 28 GiB 入场条件。

2026-09-09T17:14:42Z 结束采样：新增 644964352 bytes（约 615 MiB），空闲 27738468352 bytes（约 25.83 GiB），在预算内。专用 PG 49998 已正常停止，两个任务 Chrome session 已关闭，数据/profile/原始证据保留。`final-freeze-check-20260910.json` 核对 A0 361 输入/9 准备绑定/ab3c 二进制无差异、旧迁移 50 文件无差异、三处待授权目录和模板保留、A0 formal qualification 未运行、原工作树 clean。没有清理用户数据。

## 原验收覆盖与剩余项

本片完成 P2-A08/S05、P2-A10/S06 的责任登记部分，以及 A01 详情期限、A02 旧义不变、A09 未支持终态拒绝、A11 封存/原动作重试、A12 当前部署权限与会话、A13 Local 升级/重启/备份恢复、A14 持久化责任更新的适用验证。各编号的整条验收仍按原方案保留，未用局部通过替代。

父级仍需 A/B 身份、范围、关系与合格证据；SE-02 仍需 UR/资源 owner 的真实观察/effect 来源。完整队列/计数与分页、来源隐私和健康、线索生成/去重、明确复核结论、反证/缺证、跨范围关联、有依据结束/重开、新观察后重评、项目级 Case 权限/只读复核角色、受限来源链、源中断/重放及实际安装/部署恢复均未由此片证明。原 13 项最终还需经理确认同一集成版本验收。

| 原技术债定义 | 本次状态 |
| --- | --- |
| TD-01 可核差异 | 提供基线到交付的实际代码/合同/生成物差异与上述测试；新增能力不等于旧债全部清除。 |
| TD-02 权威和重复逻辑 | 当前投影为唯一当前值，追加表为历史/回执，同事务；不另存备注镜像、不复制来源事实/费用解释；复用公共 HTTP/权限。 |
| TD-03 兼容与退出 | 旧 Finding/annotation/v1/历史迁移/rc.10 仍有受支持依赖，继续保留；本片新增此前没有的能力，没有旧功能退役目标。新路径 Local 可用，适用兼容已验，不能声称完成架构替代。 |
| TD-04 已解/保留/待办 | 本片并发、封存、幂等与升级恢复已验；完整队列、项目范围权限、来源资格、新证据重评和真实维护 D-37-01 保留；另记录上述 Connectors digest 权限观察，不宣称零债务。 |

协作优化已采用：完整授权子项内自主完成普通实验/输入修正；验证观察器的假设后再做高成本操作；定向读取并复用与当前内容仍绑定的证据；一次整理完整包交经理。此前资源阻塞已主动通知一次，完成包通知使用 send_message_to_thread 直接送达；不将本任务自身最终回复代替主管通知。
