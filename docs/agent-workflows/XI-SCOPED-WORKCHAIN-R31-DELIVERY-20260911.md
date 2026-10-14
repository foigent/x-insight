# R31 交付：本人筛选维护与正常重验证的草稿保留

2026-09-11（Asia/Shanghai）。两项主管反例已在同一代码与最终嵌入资源上完成修复、自测和真实浏览器工作链，交回主管独立复验；尚不表示主管已验收或整版通过。前态为 `1100b8a4a802a00a2ac349798374af61accc9624`，最终 commit/tree、源文件与证据哈希以 [delivery-binding.json](../../output/xi-scoped-workchain-r31/delivery-binding.json) 为准。

## 修复与结果

| 主管反例 | 最终行为与实际验证 |
| --- | --- |
| R31-01：两种正式受限角色能新建本人调查筛选，PUT/DELETE却403 | 受限路由按真实接口放行单项PUT/DELETE，并继续把它视为本人偏好而非复核写权限。正式v3 reviewer/reader、原生PG角色与生产HTTP handler均通过新建、更新、删除、版本冲突后重读恢复和幂等；他人对象404，缺CSRF/错误Origin403，只读复核写仍403。真实界面另完成同账号筛选1→2→3（删除墓碑），旧问题关闭。 |
| R31-02：同角色同scope_revision的focus清空复核草稿 | 普通focus/pageshow重验证保留挂载组件；确认拒绝仍立即关闭旧内容。主体、Session、角色、权限或范围版本改变会清空旧视图并拒收旧响应，活动时间变化不触发重建。真实新建和更新复核编辑器各经focus/pageshow后，全部已填字段、结论与采用依据保持；原Session/role/scope不变。 |

同一正式合成账号完成：费用贡献（1请求/17微元）→执行依据→填写草稿→两种重验证→原子创建Case/Finding→复核版本2→返回原阅读→本人筛选新建/更新/删除→真实撤权。返回前后的阅读 `xwr_sbuRd8QkxaRM5Dao1NV7nw`、成员/解释版本、选中贡献及金额完全一致。案件 `xc_zVk86ka1SgjZDrlX2AbHFg` / Finding `xf_NZMibt88f9f4FIlKgR0e5w` 的当前范围撤销后API返回403 `scope_denied`，页面只显示无授权范围与退出入口；2秒观察内业务请求52→52，无旧内容回放。

来源仍为明确合成正式v3账号、原生PostgreSQL角色、生产HTTPS handler与真实Chrome；不是客户账号、真实来源或生产部署。截图记录当时滚动位置，完整草稿值比较与资源哈希见JSON。

## 验证入口及复用

- [native-api-final.log](../../output/xi-scoped-workchain-r31/native-api-final.log)：4项定向Go测试及两个角色子测试通过，`-race -count=1`，`go test`默认vet检查通过；浏览器夹具另在race下正常PASS，见 `browser-harness-final.log`。未重跑全仓Go。
- [frontend-final.log](../../output/xi-scoped-workchain-r31/frontend-final.log)：130测试、类型、OpenAPI及最终嵌入构建通过；前端覆盖statements/branches/functions/lines为76.69/67.30/73.49/78.33%。`frontend-e2e-final.log`：四视口24项通过。产品前端源码在这些检查后未再修改。
- [browser-focus-final.json](../../output/xi-scoped-workchain-r31/browser-focus-final.json)：新建草稿全字段/选择保留，两次会话重验证成功；服务实际返回的HTML/JS/CSS共3项与最终manifest字节和SHA-256逐一相符。`browser-update-final.json`保存更新草稿、版本2及原阅读逐值相等结果；`browser-filter-final.json`保存真实UI更新/删除响应；`browser-revoked-final.json`保存真实拒绝、DOM退出和有界无循环观察。
- [go-coverage-verified.log](../../output/xi-scoped-workchain-r31/go-coverage-verified.log)：总64.4/API60.1/DB70.9/hostctl64.5/workspace65.9%，原门槛全过。21029个块的复用规则记录于 `coverage-reuse.json`：唯一改动的Go生产文件 `scoped_workchain.go` **只用本次计数**；未改源码沿用R30已独立审阅证据并合并本次覆盖。没有把R30首次全仓5项失败或本轮定向执行说成新全仓零失败。
- `unchanged-native-evidence.json`及本次哈希核对确认授权存储、案件历史、锁序、schema/原生恢复相关35文件未变，复用review31独立通过结论；不重新做与方法路由/前端生命周期无关的大规模迁移、性能或恢复实验。

失败记录全部保留：首次前端测试使用未加载的DOM断言、随后测试夹具readonly类型问题已修正并复验；撤权观察器首次把浏览器Fetch Response.status属性写成方法，失败于读数阶段，在同一已撤权现场修正为属性读取后通过。该修正没有重建夹具、重复业务写入或改产品代码；错误日志 `browser-revoked-final.log` 与成功日志 `browser-revoked-corrected.log` 分开留存。自签名本地HTTPS的浏览器证书探测握手日志、预期401/403网络记录也原样保留。

## 资源、兼容与完成边界

原21GiB空闲底线和C 5696MiB/A0 18GiB/RSS 4GiB及计费起点均保持。20.5GiB提案已撤回，从未批准或实施；资源自然恢复后按经理裁定接续，未清理任何旧现场。精确采样及收尾见 `resource-summary.json` / `resource-closeout.json`，采样不能冒称连续峰值保证。

截至PG收尾累计记录 264 个资源样本，全部在原守卫内；C采样峰值 5,394,350,080 B，RSS峰值 1,647,165,440 B。PG收尾时C 5,343,412,224 B，free 23,748,100,096 B。

[database-closeout.json](../../output/xi-scoped-workchain-r31/database-closeout.json)确认开工前18条数据库目录/ACL原样保留、无新增留存库、无其他客户端，原实例system ID `7683496851946968342` / port49998正常停止；本轮浏览器已关闭。既有原生夹具按其原生命周期正常收尾，未手工删除现有数据、缓存、工作树或失败材料。

本次修的是R30新包的两项回归，不将它们重复报作旧技术债整体消除。个人偏好/复核写、当前授权/读取快照、来源事实/人工判断、历史版本和原深链继续分离；schema30、角色与恢复协议未改。旧构建资源由正式构建替换，前态保存在父提交；无新增依赖、授权旁路或持久草稿系统。

无push/PR/merge、部署、真实凭据或来源、UR动作。R32仅有已批准计划，不含任何产品实现，也不进入本次候选；须R31主管独立验收通过且原资源条件满足后才自主接续。完整A0/A1/B/C/D/X、原13项、来源条件、性能和同一集成版本最终验收仍待，不以本修复包代替。
