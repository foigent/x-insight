# C 调查队列完整子项：Local 交付与验收依据

本包落实 manager independent-review-23 批准的 C P2-M04 部署范围队列，基于 `71a86a22edc8e84726b4dd2963983a3dc5d2b10c`，工作树 `/private/tmp/xinsight-xi-c-responsibility-20260909`。本地实现与必要验证已完成，提交经理统一独立验收；不代表整个 C、A0、原始 13 项或 Target/Production 已完成。

## 实现与语义

- 当前跟进、期间新建、依据变化分别统计；保留原分类、Case 状态、复核结论与跟进进度。封存不自动结束跟进，依据变化不自动重开或改写历史。
- 队列从当前部署内有权读取的完整集合筛选、统计、排序及分页，无最近 50/100 条截断。读取事务、固定时间和集合版本共同约束分页；身份、权限版本、筛选或相关数据变化使旧游标拒绝，界面显式刷新。
- 队列与详情共用依据组装、依据变化及逾期规则。已到期但尚无进度的 Finding 也计算逾期，仍保留“未记录”。当前身份、停用责任人、相关引用新增、采用 Gap 内容变化及引用不可用有各自解释。
- 个人筛选提供创建、更新、删除和持久回执。身份由会话确定，行版本及操作 ID 约束并发；相同命令重试返回原回执，再读取当前投影。软删除保留回执，不能重放旧回执覆盖后来版本。读取 Case 的人类角色仅新增个人偏好权限；调查只读者仍无 Case 写权限，不能读取他人的偏好。
- schema28 新增个人筛选投影及回执表、原生恢复校验和运行角色最小授权。开发 schema/build 元数据及开发使用说明同步更新，正式发布身份保持原值。
- 真实嵌入前端提供队列入口、三种视图、筛选、加载更多、指定 Finding 详情与返回原筛选、偏好 CRUD 和未知结果恢复。HTTP 及 OpenAPI 使用严格字段与权限校验。

## 验证结果与可复核文件

证据根目录为 `output/xi-c-queue/`。`delivery-binding.json` 绑定源码、本文、执行记录与原始证据；本地提交身份由交付消息给出。

| 验收面 | 实际结果 | 主要证据 |
| --- | --- | --- |
| 完整集合与响应时间 | 201 条五页无重复遗漏；2001 条混合日期/状态/依据，当前 1999、新建 1001、变化 1；实际 HTTP 148.788834ms，低于本地 3s 门槛 | `final-production-probe.log`、API 测试 |
| 共享语义和稳定分页 | 无进度逾期与详情一致；Gap 变化、封存仍在跟进、坏引用不可用；固定时间、多页稳定、跨身份/筛选拒绝、相关数据变化 409 | `isolated-postgres.log`、API/DB 定向测试 |
| 个人偏好与权限 | 自有 CRUD、操作重试、删除后原回执、并发相同命令/版本冲突、CSRF/伪造归属/重复 JSON/登出旧地址拒绝、跨身份不可读 | API/DB 测试、`hostctl-native.log`、浏览器 reader 结果 |
| 迁移、旧包和原生恢复 | 实际 PG16 schema27→28、既有 v1/v2/v3 包原字节保持、偏好与墓碑/回执恢复、错误投影恢复拒绝、只读预览无副作用、最小权限验证 | `hostctl-native.log`、`target-guard-positive-hostctl-final.log` |
| Go/race/coverage | 完整包证据按原覆盖门槛组合；total64.3/API59.7/DB70.4/hostctl64.5/workspace65.9，分别高于63/53/69/64/46；目标防护包95.7% | `coverage-gate-final.log`、`coverage-provenance.json`、`assemble-coverage.py` |
| 静态检查 | 最终受影响测试包及防护包 vet、修改 Go 文件 gofmt、源码与文档差异检查通过 | `go-vet-final.log`、`target-guard-vet.log`、交付检查 |
| 前端门槛 | Node22.23.1/npm10.9.8：93/93 测试，覆盖率 statements75.26/branches66.36/functions72.56/lines76.97；typecheck、OpenAPI65路径、license195包、build、18/18 E2E 通过 | `frontend-pinned-results.json`、`pinned-*.log` |
| 真实生产处理器本地探针 | TLS handler 下 schema28 `/readyz`=200，队列深链 HTML、CSS、JS 与锁定 runtime 构建逐字节一致 | `final-production-probe.log`、asset manifest |
| 真实嵌入界面 | 合成202条集合实际翻页，变更项到详情、跟进预览/保存、返回、偏好生命周期、登出重登及调查只读者自有偏好；1440/1920/1024×明暗六张最终图，无页面横向溢出 | `output/playwright/xi-c-queue-final/`、`xi-c-queue-verified/` |

v1/v2 是在新的可丢弃前态库内按受支持历史 wire 格式植入并重算摘要的合成包，v3 由当前 Store 创建；不是声称当前 Store 原生生成旧格式。迁移及恢复校验绑定这些已存字节。旧 migrations1–27 共53个文件、正式 release 文件和历史 clean-baseline 合同共59项逐字节保持，见 `preserved-source-contracts.json`。

Go 证据不是“一次全库命令全绿”：初次全库运行只有 release 开发迁移计数断言失败，修正后完整对应包通过；五个完整 PG 包最终在正确专用库上全部通过。最终组合仅替换完整包、对相同源码块合并 hostctl 覆盖，再加入测试专用防护包；没有降低门槛、伪造命中或把失败命令改称通过。TestMain 防护只增加测试入口，未改变已经验证的产品函数体；其正向、负向、原生恢复及 vet 单独通过，复用相同产品输入的原覆盖结果。

## 数据库目标偏差与 TD 修正

初次五包回归错误地将 `XINSIGHT_POSTGRES_DSN` 指向本任务 C 合成实例的维护库 `postgres`。旧测试中存在直接向 DSN 库执行迁移与合成写入的路径，因此该库进入 schema28；事后读取为 `postgres|xinsight_admin|49998|28|123`，其中123为当时的 evidence_events 数。实例为 `/private/tmp/xi-c-responsibility-runtime-20260909/data`，PG16.14、127.0.0.1:49998、system ID `7683496851946968342`。

**前态缺口**：没有误配运行前的维护库内容快照，不能将123条全部断言为本次新增，不能证明此前为空或物理字节不变。已知执行动作包括直接迁移/写合成记录，以及原工厂在同一实例新建并按其既有生命周期清理自己的临时库。没有据此重置、删除或回滚维护库，也未清理旧留存库。原始误配输出保留，不计作正确隔离目标上的回归通过。

已改用本包明确新建的 `xic_queue_reg_20260910`，OID `407392`。五包完整回归与最终 HTTP 探针均使用该目标；当时未保留独立的每次进程环境转储，这是过程绑定的局限。后续目标防护补充以独立 scope JSON、实际集群/库身份和运行脚本明确绑定该目标，直接 DSN 旧测试、自动建库及原生恢复再次通过。

针对 TD“执行器目标验证不足”，六个 PG 测试包现于 `TestMain` 调用 `internal/testpostgres`：本地合成模式需独立目标记录，解析后的主机、端口、库、用户、fallback/运行参数以及实时数据库 OID/集群 ID 不符则在 `m.Run` 前拒绝。维护库不能成为 scope，外部遗留 hostctl DSN 也会拒绝；原生测试从合格工厂内派生新库。现有 GitHub Actions 临时服务保持原建库/角色合同；无 DSN 离线测试不连接数据库。防护范围是这一明确选择的本地模式，不声称约束人工 SQL 或替代授权。

实际分别误指 `postgres`、`xic_cov_reg_20260910`、`xinsight_clean_qualification_7eee4153b294`、`xinsight_clean_qualification_fa905082ec1e`：每个目标的六个包均在测试体前拒绝，未执行直接写入或 CREATE DATABASE。四库逻辑 dump SHA及数据库目录前后完全一致，见 `target-guard-protected-before.json`、`target-guard-protected-after.json` 和四份 negative 日志；dump 摘要仅排除 pg_dump 的随机 restrict 标记。另测缺失/损坏 scope、DSN 覆盖参数、维护库 scope、遗留恢复 DSN，以及实际 OID 不符拒绝。此后防护行为已验证，历史事件及前态证据缺口继续保留。

最终该实例仅保留上述四库及专用新库，正常停止；停止前只有当前观察连接。见 `database-final-after-guard.log`、`postgres-stop-after-guard.log`、`postgres-control-after-guard.log`。没有部署、真实数据、外部凭据或 UR 操作。

## 失败保留与效率调整

观察器/环境错误保留且不冒充产品失败或有效性能样本：最初默认 PG18 工具与 PG16 数据不兼容、管理员名称猜测、旧 metadata/count27 断言、无效进度转换测试 fixture、嵌入深链未接入、暗色样式对比问题均按各自原因修正。最后浏览器 observer 的相对 `/readyz` 请求因无 baseURL 失败，`final-smoke.txt` 保持失败；真实生产 handler 的准备状态与 asset SHA 由独立实际 HTTP 回归补足。锁定 Node/npm 重跑六个前端门槛后 dist 完全一致。

目标防护补充中，沙箱阻止 PG 共享内存/网络访问，按既有授权使用平台提权后完成；正向 native 补测第一次未将 PG16 放在 PATH 首位，Backup 前置检查拒绝，只重跑该失败子项并通过，旧失败日志保留。没有因此重复完整产品/浏览器验证。原始工具日志含工具生成的行尾空白，按原字节留存；源码和文档独立通过 whitespace 检查。

本包采用完整子项授权和完成后统一验收，范围内失败由执行者定位修正，仅数据库目标偏差作为实质范围事件即时上报。针对“上下文重、观察器返工、过细协调”的建议，实际采用定向回读、相同源码证据复用、聚合交付与既有工具。迁移、权限、恢复、封存及必要门槛继续保留。未重算用户所引 token 统计，未测量优化前后同类任务，不声称已节省某个 token 比例或耗时。

## 资源与剩余边界

批准上限 C2880MiB=3019898880B、A0原始口径18GiB、空闲空间>21GiB，均保持原 cache/git/manager 留存计费起点。验证窗口03:14:02–04:33:35UTC共1039个样本全部在界内，C峰值2851999744B、A0计费峰值15041940554B、最低空闲31576039424B。见 `resource-summary.json`；之后提交阶段读数另见交付消息，不回写或删除历史样本。

A0 固定 HEAD `5f23135aba3fd792c58f5035a717b4106ca650cc` 未改变，原工作树保持干净，A0 未与 C 并行启动。C 数据库及全部本包浏览器验证实例已停止；原失败目录和数据库留存。

本包范围仅为部署级 Case/Finding 调查队列。S01 项目 ACL、完整 P2-A12、安全来源/真实 S09 效果、M01/M02 来源、A0 最终资格及原始13项/TD整体验收仍有独立工作。本包不新增这些权限或宣告完成；A0须经新的资源与非重叠运行窗口决定后再执行。没有 push、PR、merge、release、deploy 或跨产品动作。
