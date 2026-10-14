# Connectors digest 权限问题：独立调查与修复方案

状态更新：**independent-review-16 已批准本方案及旧源预检、管理 checkpoint 调用、生命周期边界补充；Local 实施与验证见 [实施记录](xi-connectors-digest-implementation-record-2026-09-10.md)。** 此项属于 TD-02/TD-04、GX-01 的实际待办，不并入责任子项的已通过数据能力，不声明完整 C 或原 13 项通过。以下保留批准前的调查和方案文字，原审查版本 SHA256 为 `412919490e5fd60db12dd86d016d2e9433718872e57e059b30de684cd94df6c4`，当时的未批准和待验证描述不作为当前状态。

## 事实、推断与待证前态

XI 与经理分别在合成 PG16、实际 runtime roles 下观察到 `/api/v1/connectors` 500，PG 报 `permission denied for function digest`。经理证据为 independent-review-15；XI 旧观察 `output/xi-c-responsibility/connector-permission-observation-20260910.log`。不把合成层观察扩展为真实部署状态。

当前调用链为 `cmd/xinsight-server/main.go` 的 `LoadReaderPostgresDSN → OpenEvidenceStore`，`internal/api/investigation.go:handleConnectorCollection → ListConnectorTrust`。后者在 `internal/db/connector_trust_store.go` 对 cursor/high-watermark 调用 `digest(text,text)`，SQL 只输出存在性和 `sha256:` 摘要，不输出原值。当前合成库目录实测 public 下 `digest(text,text)` 和 `digest(bytea,text)` 均属于 xinsight_admin，reader/workspace/importer 均无 EXECUTE，见 `connector-plan-catalog-20260910.log`。

migration 23 撤销 PUBLIC 的函数执行；`PostgresManager.ApplyGrants` 再撤销 PUBLIC 与三个 runtime role 的所有 public 函数执行，只按现有清单恢复指定邀请兑换函数，未恢复 digest。上述查询、migration 23 和 ApplyGrants 函数正文与责任子项前的 `0d7c3a38272e32cc4d6f5ab6fe5fe17d931ae034` 相同，见 `connector-plan-source-comparison-20260910.json`。这是静态来源对比；尚未运行旧基线，不能据此指定引入提交或宣布全部根因和所有安装前态已经确认。

当前生产源码中另一处 digest 在 `internal/db/workbench_restore.go`，使用 bytea 重建成员摘要；已读 `RestoreToNewDatabase` 调用链，验证器使用 restored.AdminDSN。它不是给 reader/workspace/importer 全部 digest 重载开放权限的证据。测试 SQL 中的 digest 不应自动转化为 runtime 权限需求。

## 推荐最小修正方向与边界

优先验证并补齐**当前正式授权入口**所需的精确函数权利：仅对确实执行上述查询的 xinsight_reader 恢复 schema-qualified `public.digest(text,text)` 的 EXECUTE，并在当前角色验证中检查这一必要条件及其负向边界。保留 SQL 中的哈希计算、输入、NULL/空串区别、算法、编码、前缀及响应字段语义。禁止 GRANT ALL FUNCTIONS、向 PUBLIC/所有 runtime role 普授、SECURITY DEFINER 包装、服务改用 admin DSN 或吞掉错误返回假空列表。

这是拟议变更，不能直接执行。实施前先确认精确函数身份属于预期 pgcrypto 扩展、实参类型/编码和受支持路径。若证据显示真正调用者或重载不同，集中报告差异再调整；不凭近似函数名授权。应用侧计算 SHA-256 是比较备选，会扩大原始 cursor 在应用内的处理范围、带来编码与恢复合同差异，当前不作为默认方案。

目标范围：当前开发候选及其正式支持的安装、adopt/upgrade、恢复/重授权链。已读取 `operations.go` 的 Install/Adopt/Upgrade 和 `RestoreToNewDatabase`、`CreateCleanBaselineDatabase` 对 ApplyGrants 的调用。后续先根据实际 artifact/runtime/upgrade 合同列出精确受支持前态；不得由目录里存在 schema 23/24/25 就推定全部为可升级产品版本。至少包含已批准责任片的 schema 25 前态、新建当前 schema 26，以及 schema 26 已存在但 runtime grant 缺失的合成前态。历史 rc.10、schema 23 不可变身份和 migrations 1—26 不重写。

如果上述受支持生命周期均重新执行当前 ApplyGrants，可优先只改当前 grants/验证器及受影响测试/说明，不为形式完整新增 migration。若实际存在不会重授但应修复的受支持路径，先明确维护入口/新迁移必要性与兼容代价再提交具体方案，不偷偷给运行中的安装补 SQL。

## 批准后的一次完整实施与验收

1. 在单一 XI writer、现有隔离范围建立少量合成数据，绑定当时确认的当前代码与责任前基线；先复现故障和函数 ACL，区分 migration 后、正式 ApplyGrants 后、升级后与恢复后。旧基线至少执行同一最小查询/HTTP 对照，确认前态行为；不复制 A0 数据，不启动 importer/UR。
2. 盘点真实调用者及 overload、函数 extension/owner、相关角色清单与验证器。按确认的最小权利实现，复用正式 grants/maintenance 路径。反复运行 ApplyGrants 必须幂等且不清掉所需权利；故意撤销该必要权利时，正式校验应失败并给出安全可诊断结果。
3. 真实 reader 查询和 human HTTP/页面：覆盖无导入状态、cursor/high-watermark 为 NULL、空字符串、ASCII、中文/非 ASCII 和合法特殊字符。比较旧有成功 admin 查询/独立 SHA-256 计算的精确摘要，保持 NULL 不是空串、`sha256:` 前缀和 no-raw 字段边界。检查响应/错误/日志没有新增原值泄露；零行与错误不混淆。
4. 实际角色负向回归：reader 只能执行必要精确重载，不获得 bytea/其他 pgcrypto/管理函数、CREATE、事实写入、邀请兑换或角色管理权；workspace/importer 不因本修正新增 digest EXECUTE；PUBLIC 仍无对应权限。保留原邀请兑换及工作台事实不可变边界。admin 恢复的 bytea 摘要验证继续正常。
5. 在正式支持前态范围分别检验新安装授权、adopt/upgrade、已有库重授权和原生小备份恢复；恢复后端到端 Connectors 读取成功、数据/摘要/角色一致。已有 check/preview 必须只读；不把直接手写 GRANT 的成功当成生命周期已修复。若某个安装前态不适用，依据当前合同明确排除理由，不能用跳过冒充通过。
6. 完成最少充分的相关 Go/角色/生命周期测试与真实浏览器读取，更新当前合同/文档中实际受影响项。本地提交并输出 commit/tree、角色和函数身份、原始结果及未解决项，一次交经理独立验收。

## 授权、资源与停止条件

本计划只请求后续 Local 合成调查、必要代码/当前 grants 合同修改、适用验证和本地提交，不包含真实账户/凭据、现有服务、Target 或生产权限调整，不包含 push/PR/merge/deploy、UR 联系或新增 writer。批准后仍需在启动前核实独立窗口与可用资源。优先复用已有小 PG/缓存，建议继续沿用原总新增 1 GiB 和主机 20 GiB 储备，计入现有占用后确认足够；这不是新增 1 GiB。若不同生命周期前态无法在预算内保留和验证，只暂停受影响负载，提交实际资源缺口。

A0 输入/ab3c 二进制/模板/三旧目录保持冻结，28 GiB 入场门槛不变，删除未获授权。无旧基线行为、函数身份、受支持生命周期或必要负向证据时，不宣布根因闭环或修复完成；不通过放宽信任/数据保护边界绕过。此计划获审查一致前仅保留提案，不实施权限变更。
