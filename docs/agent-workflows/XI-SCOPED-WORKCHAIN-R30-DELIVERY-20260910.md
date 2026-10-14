# review30 正式受限工作链 · Local 交付

本包实现经理已批准的“费用→获权调查→原子建立复核→更新/历史→原阅读”完整子项。前态为文档提交 `11713960c818144e060db9041a7536d00c4b44a6`；已验 UI 前态为 `6034983c66293dc7338c5643e39c385b7ead38d4`。最终 commit/tree、源码与证据摘要由 `output/xi-scoped-workchain-r30/delivery-binding.json` 绑定。原计划及经理整包裁定保留，不把本地实现称为经理已经验收。

## 行为、授权与兼容

- 正式 `xinsight.local-accounts.v3` 新增 `workbench_investigation_reader` 和 `workbench_investigation_reviewer`，仍为单 Role。v1/v2 与原六角色含义保留，新角色没有全域 Evidence/Workspace 权限。使用现有 subject/grant；资料标签不是第二套 ACL。
- 新角色进入精确方法/路由表，未接入范围的旧入口及未来路由默认拒绝。裸空案、封存/原包导出、全域态势/连接器/会话/发现和管理入口关闭；旧角色入口继续工作。
- 执行关联校验实际 source、project、费用项目、规范 execution、Event ID/hash 与经济锚点；同 request 不跨来源授予权限。SQL 在 LIMIT/队列计数之前判断范围。字段使用显式白名单，未获授权的身份/来源费用/上下文及不可分类文本扣留，原文引用不展示；原 M0、SourceRef 快照和 hash 不改写。
- 案件通过至少一个完整获权依据原子创建。schema30 标签绑定整个持久案件的版本、内容及累计资料要求，包括历史与跨案关系。无标签旧案、混合范围不全、标签失效的案件整案不可见，已知 ID 与不存在均为404。旧全域写不会自动重新分类。撤权后不得重放原幂等 receipt 取得案件资料。
- 前端用同一个正式账号完成工作链；返回保留 reading、贡献选择和分母。导航/重新激活重查当前会话和范围，旧异步响应被丢弃。全部撤权显示明确空范围页；字段扣留不写成来源未采集。

## 并发与恢复

现有 reader 池持授权事务，现有 workspace 池承接业务，顺序固定 reader→workspace；不新增生产池、不扩大连接数/超时，同池配置借连接前拒绝。授权 READ COMMITTED 事务先取表 AccessShare，再取共享 advisory，下一条语句读取授权。grant DML/TRUNCATE 的原生表锁先于排他 advisory；授权锁保持到业务提交及有界响应构造结束，客户端网络不占锁。提交与同服务 Session 撤销通过短临界区排序。

schema29→30 为加法迁移，新增案件资料标签和 grant 同步触发器，更新正式 schema 合同、原生角色权限、就绪版本和恢复检查。原有 A0/A1 阅读、案件与不可变回执不重写；未知旧案不自动加标签。空扩展的隔离事务回退可验证，已有标签时拒绝30→29删表降级；旧二进制恢复使用单独29备份，保留30现场。历史 schema23 和29合同分别保留。

## 验证证据

证据目录为 `output/xi-scoped-workchain-r30/`；实际浏览器快照和截图为 `output/playwright/xi-scoped-workchain-r30/`。

- `production-schema-sixth.log` 及最终回归：正式 v3、Production HTTPS Handler、PG16原生 importer/workspace/reader；连接池1/2/10各24并发完成，取消/错误释放；真实HTTP范围、只读写拒绝、旧入口关闭、撤权/恢复和幂等重放。
- `scope-lock-first.log` 及最终回归：TRUNCATE 两种到达顺序、DML/回滚与取消；不以死锁回滚充当正常完成。
- `isolation-third.log` 及最终回归：同请求跨来源、同执行混合项目、未知项目、费用项目错配、锚点/引用哈希不符、禁止跨范围写入且案件版本不变；混合案部分撤权后整体不可见、旧队列游标拒绝。
- `case-scale-restore-third.log` 及最终回归：500获权/2001隐藏项全部分页及计数；案件 digest 与连接时区无关；schema29旧案保留、原生 pg_dump→新隔离库恢复、同一标签/案件/原子回执恢复；preview/check无副作用，缺失同步触发器与不安全降级拒绝。
- `frontend-final.log`：OpenAPI 66必需路由，121项前端测试，覆盖 statements76.61% / branches67.23% / functions73.46% / lines78.26%，最终构建及3资产 manifest 校验通过。
- 首轮真实浏览器完整链路：reading `xwr_3AZCgtZme8fOdhHvuxN5BA`，project-9000、1请求、¥0.000017；复核版本1→2、Case版本2→4后返回同一 reading/贡献/分母。320px占位滚动条实测 clientWidth=scrollWidth=bodyWidth=304，明暗费用与案件页均通过。撤权提交后重新激活，旧资料关闭。最终资产差异与撤权请求观察另在最终浏览器证据中核验。

- `go-final.log` 完成全仓 `go vet` 与 `go test -p 1 -v -race -coverprofile`：发现5项 schema30就绪/固定计数遗漏，其余执行通过。`go-corrections.log` 的再次vet和定向race复验覆盖这5项修正、版本文本/JSON及受影响的管理行为，全部通过。没有将第一次失败记录改成通过。仅更新buildinfo的schema版本值及对应测试断言；未改变其他已验证生产逻辑。两份相同位置的atomic覆盖块按最大执行计数合并，`go-coverage-verified.log` 达标：总64.5%、API61.1%、DB70.9%、hostctl64.5%、workspace65.9%。
- `frontend-e2e-final.log`：最终OpenAPI校验及既有24项浏览器E2E全部通过，含320px占位场景。
- `http-assets-final.json`：最终真实HTTPS实例 `/readyz` 报 schema30，HTML/JS/CSS三项字节及SHA256均匹配manifest。最终JS为 `index-CMLbZ2Gv.js`，SHA256 `3147dfe2cfd1840e4208dfcacdb66cec65b90b44b9362197d9b028fb559cac7c`。
- `browser-final-origin/update/geometry/revoked.log`：最终资产同正式账号重复完整链路；reading `xwr_zH4PuaTEy4ue_OhDutBjRQ`，1请求/¥0.000017，复核版本1→2、Case版本2→3，返回reading与贡献选择保持。案件/费用×明暗四组320px占位实测304px可用宽度，body/页面scrollWidth=304，最大横滚0。已实际查看最终暗色案件、浅色费用及撤权截图。授权撤销提交后focus重新读取，DOM清空；2秒观察API请求51→51，无业务刷新循环。两次浏览器场景的版本与reading分别记录，不混用证据。

资源收尾与保留数据库核对见 `resource-closeout.json` 和 `database-closeout.json`。原15条数据库目录元数据逐条不变，新增3个库保留；自己的两次Chrome/HTTPS测试正常结束，停止PG49998前无其他客户端。收尾前836条资源采样全部在原边界内：C最大5,353,549,824B、A0最大16,700,372,877B、RSS最大1,553,481,728B、free最小22,998,343,680B。保持C5696MiB、A0<18GiB、free>21GiB、RSS<4GiB及所有原累计起点；采样不冒充连续峰值。最终commit后另记一次资源，经理重操作前仍按同一guard重查现场。

## 保留范围与效率改进

本包仅 Local 合成验证，没有真实账号/凭据/流量、UR操作、push/PR/merge/部署。新增角色未开放的功能、完整 SE-03、原13项、A0资格阻塞、完整A1/TD与最终同一集成版本验收仍保留；既有路径没有被静默降级或退役。

按完整子项授权自主实施，完成后一次回交经理；小实验不再分别生成交付和审批回合。必要权限、迁移、恢复与最终资产验证照常完成。上下文按当前事实与变化读取，复用绑定未变的证据。没有把缓存命中率或累计 token 推算为未经测量的节省金额、耗时或可删验证比例。

失败日志全部保留。首轮原生连接池/请求速率测试、缺少必填字段的合成输入、恢复测试清理顺序、浏览器运行环境统计脚本错误等按原因修正；不可变经济事实保护实际拒绝了测试中错误的 UPDATE 路径，后续使用隔离异常 M0/缺项目夹具验证，没有关闭保护。完整回归另发现 schema30 就绪版本及旧固定计数遗漏，5项均完成定向修复复验；历史schema23发布配置继续拒绝打包当前开发schema。一次自动审批拒绝指出单凭新 Role 判断可能放宽旧入口，最终判断显式要求匹配的当前受限上下文。
