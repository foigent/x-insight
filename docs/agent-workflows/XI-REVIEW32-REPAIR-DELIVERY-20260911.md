# review32 修复交付：费用页直达与已知撤权即时关闭

2026-09-11。经理 review32 新增的两个 P2 已在同一 Local 候选完成修复和验证，提交独立验收。前态为 `cdb74f785f1adf9d7e9a666c5ac0be9cc01c29ba`；最终 commit/tree、源文件及证据以 [delivery-binding.json](../../output/xi-route-revocation-review32/delivery-binding.json) 为准。本包与待实施的 R32 运行阅读连续性计划分开。

| 缺陷 | 修改与验证 |
| --- | --- |
| 已登录直接打开费用/使用页 404 | 产品文档白名单增加精确 `/workbench/cost`。正式 reviewer/reader 的 GET/HEAD 返回 200，HEAD 无正文；未知文档保持 404、未知 API 由原生权限层拒绝 403，静态 handler 不把 API 变成 SPA，POST 仍 405。真实 Chrome 的两种入口直达、刷新、新标签成功。 |
| hidden 提前跳过已知撤权，旧内容留到 Session 返回 | `scope-changed` 不受普通 hidden 跳过条件影响，立即清空工作台快照并关闭内容，再重新验证 Session。组件覆盖 hidden/visible、迟到正常 Session、真实 API wrapper 403、重验证失败和无自动重试。浏览器受控 hidden + 已提交精确测试授权撤销 + 原生预览 403，Session 延迟 1500ms 时旧依据/草稿已退出 DOM；visible/focus 后仍关闭，原生 Session 200 空范围后进入无范围页；业务请求在两秒观察内 1→1。 |

## 证据入口

- [native-final.log](../../output/xi-route-revocation-review32/native-final.log)：9 项 Go 顶层测试及两个正式角色子测试，race/default vet 通过；[browser-harness-final.log](../../output/xi-route-revocation-review32/browser-harness-final.log) 使用同一生产 HTTPS handler、schema30、原生 PG 角色和最终嵌入资源，正常结束。
- [frontend-final.log](../../output/xi-route-revocation-review32/frontend-final.log)：132 测试、类型、OpenAPI 与构建通过；覆盖 statements/branches/functions/lines 为 76.90/67.60/73.60/78.43%。现有四视口 [24 项 E2E](../../output/xi-route-revocation-review32/frontend-e2e-final.log) 通过。
- [browser-direct-route.json](../../output/xi-route-revocation-review32/browser-direct-route.json)、[browser-controlled-denial.json](../../output/xi-route-revocation-review32/browser-controlled-denial.json) 保存两个原反例的修复结果。Page Visibility 为受控 getter；HTTP 拒绝和 Session 响应未伪造，未声称复现原生后台标签状态。已查看直达与 hidden 等待截图，后者只显示 Session 验证状态。
- [browser-focus.json](../../output/xi-route-revocation-review32/browser-focus.json)、[browser-update.json](../../output/xi-route-revocation-review32/browser-update.json)、[browser-origin-match.json](../../output/xi-route-revocation-review32/browser-origin-match.json)：同一合成正式账号从 1 请求/17 微元贡献进入执行依据，保留新建与更新草稿、结论及非空采用依据，创建 Case/Finding，保存复核版本 2，并返回完全相同的阅读、成员/解释版本和选中贡献。草稿截图记录撤权前第二次准备，两个阶段各自 JSON 均保留。HTML/JS/CSS 三项 HTTP 字节及哈希与最终 manifest 一致，见 `asset-http-match.json`。
- [unchanged-native-evidence.json](../../output/xi-route-revocation-review32/unchanged-native-evidence.json)：35 份原生权限、历史、锁序、恢复源码未变；上一包 79 份冻结证据哈希完整。复用经理 review32/31 的对应有效结论，不机械重跑迁移/权限全矩阵。`coverage-reuse.json` 对唯一改变的 Go 生产文件 `webui.go` 全部使用新计数，其他未变块复用/取最大；总/API/DB/hostctl/workspace 为 64.46/60.14/70.92/64.50/65.88%，原门槛均过。未宣称本轮全仓 Go 零失败。

## 失败留存与边界

首次原生测试把未知 API 的既有 403 错写为 404，修正测试预期后通过；首次路由观察器误用 CLI 环境不提供的全局 URL，未进入路由检查；随后把多次整页 API 扇出挤入服务原有 20 req/s 窗口导致 429/等待超时，按现有准入节奏分开整页加载后通过，未修改限流。对应首轮脚本/日志分别保留，未重复创建业务对象或重建夹具。初次资源检查在沙箱内因 ps 不允许而失败，随后使用已授权的进程读取；自签名证书探测、负向 401/403/404 也保留。不将这些观察/预期错误计为产品修复。

原累计起点与 C 5696MiB、A0 18GiB、free>21GiB、RSS<4GiB 保持；冻结 131 个样本全部在界内，C 峰值 5,480,980,480 B、RSS 峰值 1,573,322,752 B、最低 free 23,298,744,320 B。采样不等于连续峰值保证。`database-closeout.json` 确认原 18 条数据库目录/ACL 未变、无新增留存库、停止前无其他客户端，PG 与浏览器正常停止；未清理旧现场。

产品只改现有路由与撤权处理条件；schema、权限授予、角色、来源/人工判断、历史、旧深链及恢复协议不变，无新依赖或持久草稿系统。构建正式替换旧 JS，前态仍在父提交。无 push/PR/merge、部署、真实来源/账号/凭据、UR 操作。原 R31 两项保持经理已关闭状态；本包仍待独立验收，通过后才接续已批准 R32 计划。原 13 项及完整 A0/A1/B/C/D/X、TD 和同一集成版本最终验收未完成。
