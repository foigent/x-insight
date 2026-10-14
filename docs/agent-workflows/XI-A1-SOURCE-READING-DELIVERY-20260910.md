# A1 三 profile 本地消费与阅读交付

经理 review26 授权的完整子包已实现并完成本地验证，待经理统一验收。交付范围是固定 UR 合同与保留合成 HTTP 导出的消费、持久化、关联、版本化阅读和生产 Handler/UI；不代表真实 UR-XI 集成、完整 A1、A0 或原13项验收完成。

实施目录 `/private/tmp/xinsight-xi-a1-source-reading-20260910`，分支 `codex/xi-a1-source-reading`；隔离基点 `5368eafa1b76e0c7841a2344d3f07316afc864e0`，产品基线 `417daf1eb0a88f9abbcc3b3c8e9726eb7dfae152`。提交后的精确 commit/tree 与证据摘要绑定在 `output/xi-a1-source-reading/delivery-binding.json`。功能和操作说明见同目录的 `XI-A1-SOURCE-READING-20260910.md`。

## 已交付行为

- 三 profile 独立采集注册，复用现有有界 HTTP 通道；专用凭据文件显式传入，不回退 I-01 凭据。每次消费一页，核对 capabilities、范围、generation、字节和摘要，原子提交文档与 checkpoint。无隐式重置或重试。
- 原来源字节、Envelope 和修订不可变；合法缺前驱可推进，矛盾或非法页不推进。同一来源跨 generation 串行校验，精确重放幂等。
- profile 修订与所引用 I-01 修订独立。按 producer/org/project/request/revision/hash 关联；阶段和 profile 不增加调用或费用，媒体采用独立经济状态。
- 来源先到、经济成员为零时仍显示来源和缺口。迟到经济事实、补齐前驱及修订只进入显式新阅读；旧阅读的成员、解释、摘要与金额不变。
- 三类字段权限默认拒绝，与采集权限、旧动作权限和角色分离。应用/账户分组、计数和明细按当前授权过滤；撤权使旧阅读失效，页面清除受限内容。旧导出未扩展新字段。
- schema28→29 新增四表及六个 ALWAYS 保护触发器，补齐原 runtime grants、恢复校验和 clean-baseline 表分类。54份历史迁移及正式 schema contract 字节不变，仅更新开发合同。

## 验证与证据

证据根目录为 `output/xi-a1-source-reading/`。Go1.25.11、PG16.14、Node22.23.1、npm10.9.8；没有安装新工具或启动 UR。

| 验证 | 结果与主要证据 |
| --- | --- |
| 固定合同 | 8正/9负 source vectors、14正/7负 page vectors；`source-contract-third.log`。四份合同逐字节匹配固定 UR commit `25f5c1fdfb6b5376496c34e9b9cd295efbaf17cd`。 |
| 保留 HTTP 导出 | 9份生产 UR Handler 合成导出按归档原字节重放，覆盖 chat 与 grant/legacy media 分支；全部 DB 包最终 `-race` 通过，见 `db-final-scope.log`。原 R1–R5 charged=0、calculated=105000000 micros；chat 只计一条 I-01、700 micros。 |
| 事务与权限 | 来源先到/I-01先到、迟到、缺前驱、跨代冲突、整页原子性、int64边界、当前字段权限、封存后并发追加负向均通过。生产 API 整包最终 `-race` 通过，见 `api-final.log`。 |
| 升级与恢复 | `hostctl-final.log`：实际 schema28→29、默认拒绝保留 A0 摘要、真实 importer/workspace/reader 角色、native pg_dump 与隔离 RestoreToNewDatabase、旧与新阅读保持、check/preview/source 无副作用、篡改拒绝。亦复验原 schema9及23升级路径。主机二进制/运行证明仍为测试模拟，不作为发布资格。 |
| Schema | 从最终实际 PG 生成开发合同，见 `manifest-schema-final.log`；migration set `sha256:46b7d2a728ef6b36b8d602a9ec20ff35fdc1e90943a4ecb2150405244d31b45a`，semantic `sha256:6fa17f77783802451cc8a50ef2087165a4bf6f78ea4c16a2812a49eb8dbf73a4`。 |
| Go 回归与覆盖 | 完整回归后修复四个旧 schema 测试预期；最终完整 API、DB、buildinfo，以及受影响 hostctl 测试和 native 生命周期均通过。总64.3%、API59.8%、DB70.8%、hostctl64.5%、workspace65.9%，全部保持原门槛。`coverage-provenance.json` 明确逐包来源与相同代码块合并；不把原失败的 `go-full-second.log` 宣称为整轮成功。 |
| 静态检查 | `go vet -p 1 ./...`、`go mod verify`、37份变更 Go 文件 gofmt、源码及文档 `git diff --check` 通过。原始测试日志保留工具输出的尾随空格，不为格式检查改写证据。 |
| 前端 | check、OpenAPI、licenses、105项单元测试、coverage、build、18项原 E2E 全通过。coverage statements75.70%、branches66.64%、functions73.13%、lines77.42%；见 `frontend-candidate-results.json` 及对应日志。 |
| 实际浏览器 | 生产 Handler/嵌入 UI/真实运行角色：26份来源且0经济事实明确显示“不代表零费用”；迟到700 micros后旧阅读仍为0，新阅读为1条/700；账户分组与三 profile 明细一致；撤权后旧页清空，新阅读14份且无账户选项。1440桌面及390深色无横向溢出。见 `browser-verification.json` 与 `output/playwright/xi-a1-source-reading/`。最终夹具正常 PASS并删除其自身测试DB，`browser-final-harness.log`。 |

## 发现、修复与保留现场

实质修复包括：clean-baseline 漏计四张新表；修订参数被 PG 推断为 int4；仅用行锁无法阻止旧 repeatable-read 快照在封存后追加成员。最后一项先由反例复现，改为封存时对父阅读做值不变的 UPDATE 形成 MVCC 新版本，过期写入发生序列化失败，再完成负向复验和原生恢复检查。

执行中也出现了可避免的返工：临时目录过长/组归属不符、旧 schema 测试预期、前端测试夹具不完整、浏览器观察查询错用页面元素、初次浏览器夹具使用 Go 默认10分钟超时。原日志保留；复用已有短 TMPDIR 和固定运行时，补齐实际前置条件，最终浏览器用与夹具30分钟期限相容的 `-timeout=35m` 正常收尾。没有把这些执行失败归为产品通过，也没有为修复它们扩大外部授权。

早期草稿schema29测试DB和超时浏览器测试DB保留、不再复用。仅退役本次新建、可再生的147MB编译临时目录，精确范围和验证见 `compiler-scratch-retirement.json`；未删除原工作树、旧证据或保护数据库。

## 资源与安全状态

继续使用原 C 累计计量起点，计入新工作树和经理 review24/25/26，没有重置基线。截至提交前505次记录，C最大3,920,187,392 B < 4,093,640,704 B；A0最大15,742,412,477 B <18GiB；实际可用空间最小28,436,856,832 B >21GiB；采样 RSS 最大1,578,926,080 B <4GiB。所有记录在限额内，详见 `resource-summary.json`。

原 C、A0及用户工作树无 tracked 改动；保护数据库目录和系统ID `7683496851946968342` 保持。浏览器已关闭，隔离 PG 已正常停止。原 A0 大副本/迁移/性能资格未重启；无真实来源、凭据、流量或外部 API 调用，无 push、PR、merge、release 或 deploy。提交后资源读数另存交付绑定。

## 对优化建议的落实

建议方向有效：完整子项一次授权、实现与必要验证连续执行、完成后一次提交经理验收；使用稳定路径和结构化观察数据，按变更复用仍有效的证据，减少大段历史材料回灌。迁移、权限、恢复和封存负向验证保留，因为本包实际发现了相关缺陷。

主任务日志按 unique response_id、截至2026-09-09 19:29:59去重复算：656次请求，输入中位数151825.5 tokens，累计缓存占比93.8741%，输出696249，其中推理249196。与用户数字存在统计口径或截止差异，但支持上下文很重的判断；仅凭这些总量不能确定浪费比例或延迟主因。原始统计见 `coordination-cost-check.json`。本包采用一次完整经理交付，不为优化另建协调层。

目的声明/人工纠正、I-03活动输入、真实来源接入、同一集成候选、原13项验收、A0性能及大副本资源阻塞保持开放。用户已持续授权将交付摘要发送到经理任务“X-insight派发并验收迭代任务”，后续同范围交付不再重复请求审批。
