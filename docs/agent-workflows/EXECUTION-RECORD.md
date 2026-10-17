# Current Execution Record

> Public-copy note: private workstation paths and target identifiers use explicit placeholders. The private original is retained. These historical notes are not portable commands or a new qualification result.

## 2026-10-10 — FPV18/schema45 源码与 CI 升级资格候选

在 FPV17 准确 main `164582cab0e7f169c265c2494d04920c61af833d` 上，将下节趋势与只读案件布局修复编为 `0.2.0-dev.fpv18` / `manual-r1` 候选。D 配置、兼容矩阵、手册及发布说明同版，schema contract 仍为45；升级白名单新增准确 FPV17 来源，其他声明来源和 rc13 冻结身份不变。Linux CI 的受管升级车道从固定 FPV17 源码重建 CI 专用前态，先安装并产生原连接、正文、授权及游标，再在活跃 UR 夹具内以候选控制器执行只读 `upgrade --check`、状态快照无副作用比较、同 schema 升级以及服务、来源和历史连续性核对。该车道只形成 CI 合成资格，不代替原 Target 的签名包升级与实际数据验收。

本地固定 Node 22.23.1/npm 10.9.8 下，前端 `check`、38 文件／574 项 Vitest、`build` 和精确 dist 清单通过；清单 SHA-256 `bc38c089a55f70e259fda382cb41df11c5696dbe4cc45ee3d8631dbd164eaf63`。保护摘要只读检查、发布脚本接线及构建自检、CI contract 与资格身份 self-test、升级脚本 shell 语法和 Go 升级子程序定向测试通过。更广的本地 hostctl 测试因当前沙箱拒绝 Unix socket `bind` 而失败，待 GitHub Ubuntu 门验证；此处不声称 Linux CI 或 Target 已通过。

PR #120 首轮前端 CI 在总览组件测试失败：新增断言写死了本机时区的 `2026/09/01 10:00`，Ubuntu UTC 实际标签为 `02:00`；渲染中的已记录 0 与未知完整性均正确。修正断言只核对本测试要证明的已知 0、未知完整性及起止区间存在；`TZ=UTC` 下对应 53 项及完整 38 文件／574 项覆盖率门均通过，四项全局覆盖率为 80.19% statements、74.45% branches、76.35% functions、83.05% lines。原本地检查使用主机时区，故未暴露该测试夹具假设。首轮其余 CI 和修复提交的 CI 回执分别待核，不以本地通过替代。

---

## 2026-10-10 — 七天趋势可读性与只读案件布局本地修复候选

从已交付 main `164582cab0e7f169c265c2494d04920c61af833d` 的同树本地分支修复 FPV17 显示。先把旧行为改成回归断言并观察到 4 项失败：总览遗漏 `complete=false` 的已记录 0、相邻已知桶未连线、专题没有连线、负金额缺少方向；再修源码。总览现在对有效已知值（含 0）按真实相邻时段连线，缺失／非法／不相邻仍断开，覆盖未知另行提示，0 不解释为真实无活动。使用和费用专题改为折线与小点；时间刻度随实测绘图区宽度取舍，首末保留，逐桶焦点／悬停保留精确起止和值。费用正负共享可见零线，负点在下、正点在上，全负也明确列出负／正时段数；大整数用 BigInt 定位而不经浮点金额换算。无 `investigation.write` 时案件列表占满可用宽度，长标识可折行；有创建权限时仍为原双栏。

原本地 Go 浏览器夹具增加默认关闭的 `XINSIGHT_A0_BROWSER_WEEK_TREND_FIXTURE=1`：保留原 12 条正式来源事实及身份，只在本机把事件时间分布到 7 天，由正式阅读自然生成 28 个 6 小时桶；与分页夹具互斥。正式 `completed-execution.v1` 金额 schema 只接受非负字符串，故正负金额只在 mock 浏览器和组件测试验证，不能冒充 PG 正式输入或生产事实。

固定 Node 22.23.1/npm 10.9.8：前一候选全套 Vitest 38 文件／573 项通过；最终只增补首末桶时间非法的回归，先复现 `Intl.format` 的 `RangeError`，再验证非法桶不格式化刻度、无点且断线，Workbench 定向 44 项通过。最终 `check`、正式 `build`／精确 dist 清单通过；固定 Go 1.25.12 的 `internal/api` 定向测试及编译通过。mock Playwright 桌面／390px 共 6 项通过：只读 3 张案件卡片、长标题与说明均在卡片内且整页无横溢，混合正负及全负金额点相对零线方向正确、线段端点与点中心误差小于 1.5px；截图在 `frontend/test-results` 对应测试目录。最终 dist 仅引用 `index-DUlEVDNm.css`、`index-DMFTGWtF.js`，`asset-manifest.json` SHA-256 为 `bc38c089a55f70e259fda382cb41df11c5696dbe4cc45ee3d8631dbd164eaf63`。这些是本地与 mock 证据；原 61712 的新构建替换及 PG／浏览器验收由总体继续，尚不在本记录冒称完成。未推送、发布或操作 Target。

总体随后在原 61712、原 PG／公开合成 `workbench.one` 身份实载最终 `index-DMFTGWtF.js`／`index-DUlEVDNm.css`（同一 manifest SHA-256 `bc38c089a55f70e259fda382cb41df11c5696dbe4cc45ee3d8631dbd164eaf63`）；预览 PID 58375，同原 label，输出 `/private/tmp/xi-ui-reading-20261009/browser-trend-verified-20261010`。输入仍为原 12 条正式来源事实，仅事件时间分散到 7 天。总览实见 48 点／47 线，费用和使用专题各 28 点／27 线／0 旧柱。1648／1024／390px 视口下，绘图区宽 704.52／781／343px，时间标签 5／6／2 个，逐对无碰撞且不越绘图区，文档宽 1633／1009／375px；正文和导航实测 13px。相同图表实现的前一 `fd2` 版本已验小时详情 168 点／167 线、图宽 2352px，可在图内滚到末桶并用键盘读出精确值；最终版本在此之后只追加案件断行和非法刻度保护。预览最终恢复正常宽度、最近 7 天、计算参考口径，供继续体验。

案件只读布局和混合正负／全负费用的证据仍限于上述 mock E2E 与截图，不能解释为原 PG 接受负金额或真实角色案件链。root 第 47 份 §18 记录复盘及持久截图。ECS 仍为 FPV17，本轮未发布。

---

## 2026-10-10 — 总览视觉收尾本地候选

在已验 `aa3fecf` 上，只改前端。六个主模块用原生 `details` 的“说明”集中定义，错误与采集状态保持可见；活动统计明确为来源报告的尝试次数，两个数字区无边框，主视图仅显示非零结果，零分类、关系和机器状态码留在说明，来源设置与全范围活动入口仍可操作。趋势不再使用旧柱图及其 CSS：以时段中点映射真实时间，完整相邻时段才连直线，完整零可绘制；完整性未知的零不画，非零只保留孤立观察点，负金额纳入纵轴。焦点优先于悬停，切换序列清空旧读数，点的精确秒级范围只用于说明与上下文，不改变同一 reading 的专题范围。摘要金额和趋势名称随实际金额口径，默认图下注明“完整性未知”而非断言资料缺失。

固定 Node 22.23.1/npm 10.9.8 下，Overview/App 定向 109 项、`npm run check`、`npm run build`、dist 精确引用和 `git diff --check` 通过；覆盖缺口断线、完整零、未知非零、负费用、焦点／悬停、金额口径及活动筛选／全范围 URL。候选 `ac9102131c85de9ef083db889da8e35281109189` 的清单引用 `index-CGAwlqkF.js`、`index-CuIMSh6Y.css`，SHA-256 `0542ea465e65c8efd5c5cabbda3b8af0a76b9871d6ba5ba7611b548895b145d0`。

总体已在原 61712、原 label／PG／公开合成账号实载该产物：六处说明默认关闭，趋势 Enter 和活动点击均可开关；活动读 6／写改 2、失败 2 醒目，零分类在说明，普通文字 13px。合成样例仅一个非零 12 请求且完整性未知，实绘 1 空心点、0 连线；键盘读出准确秒级区间和值，切费用时旧读数清空。计算参考费用 ¥0.000342、网关已计 ¥0.000270，标题随口径正确；24 小时横轴为 10/9 13:38—10/10 13:38，进入专题仍保留完整 24 小时／12 请求。选原 P2 project9 后活动 URL 保留准确 `from/to/source_ids/project_ids=9`。1648px 文档宽 1633px、高 1618px（前批 1591px），趋势／活动／费用／模型卡分别高 414／322.7／349.5／333.8px；1440／1024／360px 文档宽 1425／1009／345px，各卡无横溢，360px 展开说明及键盘长读数仍可用。连续完整折线和负金额仅有定向组件数据验证，原浏览器合成样例没有这些场景，亦无 ECS／生产结论。旧预览精确 stop/PASS，新 Go 1.25.12 embedded test 与 harness 编译通过；最终恢复正常 1648px、最近 7 天、计算参考、请求趋势、沿用总览活动来源和关闭说明，预览保留。证据输出 `/private/tmp/xi-ui-reading-20261009/browser-overview-line-help-20261010`，总体第47份 §16、第61份当前入口；未修改后台、凭据、生产流量，未推送／发布。

---

## 2026-10-10 — 总览从长报告收为两排观测台的本地候选

前版 `6c7ae51` 的图值、精确导航、轴线与窄屏已经在原预览通过，但在 1648×822 的正常视口中文档仍高 2423px：活动区约 404px、趋势 454px、项目排行 535px、模型排行 504px。问题来自四块分别垂直成段、活动长条把 6 次读取和 2 次写改画得近似等重，以及来源控件和重复解释占高；仅把一张图换成环形不能解决整页密度。

本地将趋势／活动排为桌面 2:1，项目费用／模型请求排为第二行，卡片依内容自然高度对齐，窄屏单列。活动用次数与四类结果的短文字和少量状态色，不靠颜色独立传意；来源筛选、关系口径、范围码、应用归属合入一个次级展开，默认范围、来源采集限制、权限或读取错误仍可见，原 from/to 全范围活动下钻不变。费用用原生 SVG 小环与精确金额列表，分组＋其他＋未归属必须以非负金额对齐完整分母；零额、负额、缺值及不一致时保留数值但禁画环。环段可键盘聚焦、应用维度有对应名称，授权组仍进同一 reading。模型请求条按本卡共同零起点刻度，等数量等条长，占比另按完整请求分母；非法或不一致请求数不画条或份额。用途缺项不占大空卡，有数据仍显示完整构成。待关注与近期详情默认折叠，未删除原内容。

总体对原 61712 预览中的前一构建反馈：正常视口页面高 2423→1770px，首排改善；但键盘聚焦 project-9011 时鼠标仍停在 project-9007 行，读数误显示后者；模型卡 556px、费用卡 516px，第二排仍需收紧。本地改为焦点与悬停分离、焦点优先且失焦恢复悬停，并增加精准交互回归。模型数量、占比、金额同行，费用项目、金额、占比同行；缩小行间空白，按卡片宽度折行环和列表，窄屏模型行自然换行，保留完整数值及原下钻。

总体随后在原 61712 复核前一构建：1648px 页面高 1591px、费用卡 377px、模型卡 334px，焦点读数正确，1024px 与 360px 无页面或卡片溢出；但 1440px 费用内容宽 522.5px 时 540px 折行阈值使费用卡高 575px。最终本地仅把阈值收至 480px，并使切换到应用维度后的可见标题与环形名称一致，保留 360px 金额行规则。固定 Node 22.23.1/npm 10.9.8 下，`npm test -- src/OverviewPage.test.tsx src/App.test.tsx` 103 项、`npm run check`、`npm run build` 与 dist 精确清单、`git diff --check` 通过。测试包含完整金额分母与其他／未归属、负额／零额／不一致禁画、应用环形及可见标题切换、焦点优先与失焦回到悬停、同 reading 分组导航、模型同数等长及负 Other 请求数禁画。最终 `f15b8f837cc702cd4709972892cd70f3c057ba44` 的清单只引用 `index-BRx2VWzh.js`、`index-DrjXf54Z.css`，SHA-256 `bdd7d5697205e5e91056a5e259cf5c090dc6cd3d58cde8e1be480760465c227c`。

最终原 61712 实载验收已通过：1440px 文档宽 1425px，费用环 150px＋列表 360.5px 并排，费用卡回到 377px、模型卡 334px；1648px 文档宽 1633px、高 1591px（旧 2423px），活动卡 338px，13px 字体保持。project-9011 键盘焦点读数为 ¥0.000034／9.9%；应用可见标题、未归属 ¥0.000342／100% 及切回项目均正确。费用 project-9011→req12／¥0.000034，模型01→req01／1 请求／¥0.000023，活动原合成来源 project9 的 URL 保留准确 `from/to/source_ids/project_ids`，均已实际通过。此前同布局 1024px／360px 无横溢，窄屏环可键盘读。Go 1.25.12 embedded test 和 harness 编译通过，三轮替换预览精确 stop/PASS，已恢复正常视口。证据定位：总体第47份 §15、第61份当前入口及 reports 索引；输出 `/private/tmp/xi-ui-reading-20261009/browser-overview-composition-ready-20261010`。本任务未操作 61712、API／后台／凭据／ECS／推送／发布。

---

## 2026-10-10 — 总览图表与导航的原预览最终定向验收

首轮 `f0d4bbf` 的原 61712 浏览器复核确认了图值、授权组精确下钻与 360px 无页面横溢，同时发现趋势提示撑高轴线、零刻度低于柱底线约 40px，以及读写两图桌面各占一整行。`6c7ae51` 仅在原样式和活动卡内修正：趋势轴固定于绘图区，提示不参与轴长；读写图桌面双列／窄屏单列；活动来源和全范围入口同排，期间限制只留一处可见短提示，具体范围码仍折叠。没有改 API、分组导航、覆盖语义或来源结果。

总体随后在原 `https://127.0.0.1:61712/` 以同一 macOS 会话标签、原 PG 合成账号加载最终 `index-BUEVc91r.css`（dist 清单 SHA-256 `5a57e67b86518d286a14ddbb095dcca71d9afc0acfb0d7830fb9eecad85d7af0`）。1440px 视口文档宽 1425px，趋势 axisBottom＝plotBottom＝zeroBottom＝546.49px；360px 视口文档宽 345px，轴与柱底同为 1549.88px，资源读写两图各宽 263px、同 x=41、上下单列，活动工具栏宽 263px，无页面横向溢出。最后恢复正常视口并返回总览。当前输出 `/private/tmp/xi-ui-reading-20261009/browser-overview-charts-final-20261010`；旧 ready 和 charts 预览均按精确 stop 正常 PASS。Go 1.25.12 的 `internal/webui` 检查与预览 harness 编译由总体对最终候选验证。

同一复核中，来源报告读 6＝完成 3／失败 2／可选缺失 1／未知 0，条宽为 50%／33.33%／16.67%；写改 2＝完成 2，条宽 100%。模型 `browser-model-01` 进入 usage/model 的 1 请求／¥0.000023、对应 req01，刷新仍保留该选择；返回再点 `project-9011`，费用为 ¥0.000034、对应 req12。趋势键盘聚焦显示准确起止、12 条已知请求与期间完整性未知；近期列表默认折叠，展开后费用 tab 可用；活动入口保留完整 from/to，活动详情顶端为 0 且焦点正确。最终样式收尾没有重改导航，所以没有重复未变的组导航回归，但再次核了活动入口。

固定 Node 22.23.1/npm 10.9.8 的 Overview/App 定向 94 项、`npm run check`、`npm run build` 与精确 dist 清单通过；最终只引用 `index-B7NM1Z5N.js`、`index-BUEVc91r.css`，未重跑完整 555 项。本轮证据仅为本机合成账号／PG；用途缺项未提供，趋势按时段精确阅读但专题仍取全范围，资源入口也是全范围活动，没有结果过滤、活动时序或自动刷新。没有 ECS、真实模型调用、GitHub 推送、发布或真实流量。本次以原分组／排序／reading 导航测试和最终实测轴线、窄屏布局分别证明逻辑与视觉；覆盖未知仍从响应字段读取，不在 UI 写死。

---

## 2026-10-10 — 总览结果分布、贡献排行与导航本地验证

基于 `b316aac` 改 `OverviewPage`、`App`、总览样式及测试。工作活动结果图仅在来源提供分型且分型合计等于尝试数时绘制；没有分型、合计冲突、已知零三种状态分开。费用项目金额排行默认来源项目；模型／用途仍以请求数排行，分母明确包括其他与未归属。分组按钮传递原阅读 ID、精确分组 ref、冻结期间与筛选；趋势焦点展示原时段起止和值，跳转只进整个阅读期间专题，未实现虚假的时段过滤。首屏直接提示经济资料期间完整性与来源覆盖限制，运行近期记录默认折叠、展开后保留原分页；独立复核队列保留。

固定 Node 22.23.1/npm 10.9.8 的 `npm test -- src/OverviewPage.test.tsx src/App.test.tsx` 94 项、`npm test` 38 文件／555 项、`npm run check`、`npm run build`／精确 dist 清单、`git diff --check` 均通过。dist 仅引用 `index-Tap2u4zd.js` 与 `index-CTSWngdY.css`；清单 SHA-256 `fb094f250346f751bc17c66125d68987508d097b295c8addd4ba058213f326e3`。没有浏览器复验、服务变更、凭据操作、推送或发布；原 61712 的旧版浏览器事实不能作为本批图表验收。

---

## 2026-10-10 — A「专业观测台」原预览最终复验

总体在原 `https://127.0.0.1:61712/` 使用合成账号／PG 加载 `da46e00`，浏览器实际资源为 `index-CuzPbMyN.css` 和 `index-C-Bxaj02.js`。列表选 briefing、左轨选 restricted 均为详情顶端 0、详情获得焦点、临时 `minHeight` 为空；展开完整运行后由 1902.65px 高详情切到 notes，新详情 792.46px、顶端 0、占位释放，旧正文消失。工具返回实读 53 字节且摘要核验通过；完整运行来源报告 4 读＝2 完成＋1 失败＋1 可选不存在、0 写入，框架结束仅为来源状态。360px 时文档宽 345px，无横向溢出；移动菜单展开正常，最后恢复正常视口。总览、活动、费用及手册共享视觉本轮已看，不能外推到全 13 页、全状态或深色主题，也不能作为用户审美接受。

复盘：首次点击列表未把上方新详情带入视口；随后同次运行切换卸载旧三栏，页面骤缩而滚动锚到下方列表。只暂留旧高度把跳动从约 −905px 减到 −158px，却遗留空白。最终统一四种选择入口，在加载时立即清空旧详情／已核验正文并暂留高度；新详情或错误到达时释放占位、定位并聚焦。Local 的 `check`、定向测试和正式 dist 构建与原浏览器结果对应；总体还验证 Go 1.25.12 的 `internal/webui` 检查及预览 harness 编译。当前原预览输出 `/private/tmp/xi-ui-reading-20261009/browser-professional-a-ready-20261010`，同 label、同 61712 入口在当前 macOS 会话持续；旧预览按精确 stop 正常 PASS 退出且日志保留。未推送、未操作 ECS 或发布。

---

## 2026-10-10 — 统一活动选择落点及临时高度释放

总体在原 61712 预览加载 `779d18b` 后实测：列表选择可到详情顶端，但左轨切换后的详情顶端仍为 −158px，原最小高度未释放。当前本地修正复用 `landOnDetail` 和已有选中事件、AbortController 防护：列表、左轨、运行分类、原事件选择共用启动入口，立刻清空旧详情与已核验正文，读取期间暂留高度；所选详情或错误到达的布局提交阶段释放高度并将详情顶端滚入视口、聚焦。列表同事件再次点击只重新定位。旧“左轨不定位”测试假设已改，成功与错误路径均断言高度释放和焦点。

固定 Node 22.23.1/npm 10.9.8 的 `check`、WorkActivity 运行／正文过期 2 文件 5 项定向测试、正式 `build` 与精确 dist 清单 PASS；清单 SHA-256 `f8619a9f5f6b67b376573069552edbeb955fc6b57a4cd131c63171c8faeb318c`。这只证明 Local，最后浏览器位置和可见性仍待总体复验。未触碰服务、权限、ECS、GitHub 或发布。

---

## 2026-10-10 — 同次运行切换的加载高度修正

总体在原 61712 预览加载 `12fe11c` 后实测：列表选 briefing 及再次选同事件，article 焦点和详情顶端均正确；左轨切 restricted 后加载完毕详情顶端约 −905px，视口落在下方列表。推断是旧三栏切为短加载态时页面高度骤减，浏览器滚动锚点转到列表。本地在三个同次运行切换入口先保留当前详情容器高度，再清空旧详情与已核验正文、发起新事件读取；加载占位保持列表位置，网格不拉伸返回按钮。没有保留旧正文，也没有增加导航状态系统。

固定 Node 22.23.1/npm 10.9.8 的 `check`、WorkActivity 运行／正文过期 2 文件 5 项定向测试、正式 `build`／精确 dist 清单 PASS；运行测试补了占位高度和旧详情卸载断言。dist 清单 SHA-256 `b22b64deebaec5afd64879bc186c0a73398b63cf1360b5a121b344f4d99395ad`。浏览器效果由总体在原预览复验后才能结算；本任务没有操作预览、权限、ECS、推送或部署。

---

## 2026-10-10 — A 候选首轮浏览器反馈的本地定向修正

总体在原 61712 预览实际加载 `8f887d9` 后报告三处阅读问题：选择活动详情后视口仍停在列表；模型使用卡 5 行约 424.8px，高度把活动区压低；活动列表每条时间、身份、来源三行约 90px。本地让每次列表选择后的详情 article 滚入视口并接受键盘焦点，包括再次选择已选事件；同次运行内部切换不再次触发该滚动。模型卡去掉重复大标题和装饰条、收紧每行，同时保留名称／请求数／份额／金额及比较口径；活动列表把时间／身份同排、来源下一行，窄屏自然换行。未改请求、筛选、数据、权限、来源结果或正文读取逻辑。

固定 Node 22.23.1/npm 10.9.8 的 `check`、Overview／WorkActivity 2 文件 37 项定向测试与正式 `build`／精确 dist 清单 PASS；运行测试新增列表选择后视口滚动调用、详情焦点、同事件再次选择以及同次运行内部切换不强制滚动断言。dist 清单 SHA-256 `a68119bad1659c379d51c5538ccc2689f33071fa57cfaee5732b80758b0c0806`。本任务未操作浏览器或原预览；最终可见结果、窄屏顺序及溢出仍待总体在同一预览实核。无外部写入、推送或部署。

---

## 2026-10-10 — A「专业观测台」本地视觉构图候选

用户从同日视觉样稿选定 mode-a。原页面的首屏筛选是大表单、重复重分隔线使趋势横占全宽并把构成及资源活动压低；本次在原 `design-tokens.css`／`workbench-layout.css` 上收敛为浅色左导航和右侧紧凑工具栏，移动同一首页品牌链接到左栏而保留路由与权限导航。`OverviewPage.tsx` 仅增加布局容器：原四项跨业务摘要成为轻边框指标带，筛选为工具栏，趋势与模型构成同排、用途和费用贡献在下一行双列；近期记录、活动入口、复核独立当前队列和所有说明仍在原业务分区。`WorkActivityPage.tsx` 将已有活动对象、来源上下文和同次运行事件组织为宽屏三栏；窄屏顺序折叠，原完整运行汇总／分类／分页／原始事件和技术依据保留在展开区。活动记录增加只读列标题并以表格密度排布。样稿合成数据没有进入产品，原 API／DB／认证／授权／正文核验行为及来源报告、失败、可选缺失、未知和独立核验语义未改；无后端源码改动。

固定 Node 22.23.1/npm 10.9.8 的最终 `check`、App／Overview／WorkActivity 4 文件 96 项定向测试、正式 `build` 与 dist 精确清单 PASS。原 WorkActivity 运行用例把执行身份和发起人匹配为同一行；因三栏展示改成右栏独立字段，更新该断言并打开原运行汇总后，同一分页与数据断言 PASS。development-d 手册正式重建，CSS 以共享 token 源码为前缀，5 个锚点、2 个本地资源和原产品内 `/executions` 返回路径核对 PASS。dist 清单 SHA-256 `c791ba8ee912f65a2bf2df248c2865c00ab593564e83000019f00bc1209e34e2`；手册 source digest `sha256:1ac0e7603c648b9ad883642baebd9c285ca509203826475b89267eacb903e247`。这只是 Local 候选，总体尚需在原 61712 预览实看首屏密度、三栏宽度、窄屏顺序和横向溢出；本任务未操作浏览器或预览服务，未推送／PR／签发／部署。

---

## 2026-10-10 — 普通文字统一至导航 13px 的本地候选

用户决定以左侧 `.nav-item` 的实际 13px 为普通文字标准。本次把 `--type-body` 定为唯一普通字号，正文、说明、标签、表格、控件、状态、技术正文和原 12px 辅助文字的源码声明归到该 token；全局 `small` 与离线手册正文／代码／表格／页脚同步。旧 `--type-detail`／`--type-caption` 别名移除。通用 h3/h4 改用独立 14px 标题 token，避免正文降为 13px 时误缩标题。保留其他显式标题、指标数值、左导航及目录层级、左栏底部登录卡片、图标／步骤标记和打印附加链接字号；没有全域 `!important` 覆盖，也未改功能／DOM／授权／数据／版本。普通文字例外只限这些非正文视觉角色，其中导航组标签、窄屏 breadcrumb 和左栏登录卡片保持 12px；`.nav-item` 改为引用同一个 13px token，实际大小不变。

固定 Node 22.23.1/npm 10.9.8 的前端 `check`、既有总览／正文策略／App 3 文件 99 项、正式 `build` 和 dist 精确清单通过；development-d 手册生成与源码 token 前缀、5 个锚点、2 个本地资源及正式 `/executions` 返回路径核对通过。新 dist 清单 SHA-256 `e5d80ea00f1bce2562d668f619223524c4ba3868f600a7870967d5173e7d2968`，手册 source digest `sha256:8cf9cc96e4cf4796b8a606e058c49b0ebf3fc79ac03acf7c192d01d3425f57ac`。本轮未重跑与字体无关的全套测试。总体已完成产品提交3a12979a的浏览器复验：新CSS `index-DIM6YJ_m.css`实际加载，有数据总览、活动列表/详情、费用及手册的正文/表格/标签/控件/说明采样均13px；标题与指标保留原层级。总览和手册360px时document345px，无横溢；桌面恢复并停留总览。Go1.25.12的internal/webui测试通过；首次编译预览二进制受默认沙箱Go缓存权限拒绝，在同一已授权本机范围使用所需原生权限后编译成功，没有修改工具链或产品。原预览stop正常PASS后，沿原launchd标签加载 `/private/tmp/xi-ui-reading-20261009/browser-text13-20261010`，继续同一61712。未操作ECS、凭据、推送或发布。复盘落点是用同一token替换原声明并明确标题例外，实际检查计算字号，不新增全域覆盖规则或测试平台；下节fafa资源属于前一候选。

---

## 2026-10-10 — 全站视觉尺度与离线手册本地候选

原样板把四项摘要与后续趋势、构成、活动、复核分开摆放，业务归属不连贯；页面 24/18/28px 层级与 13px 左导航不协调，旧共享样式仍有 8–11px 辅助文字，独立手册使用深绿背景和最高 52px 标题。这些是样式与信息排列问题，不能靠旧的 552 项功能测试或两页无横向溢出验收推出整体视觉已统一。

本批仅在原样式定义和总览结构修正：`design-tokens.css` 页／分区／指标改 22/16/26px；`workspace-v2.css` 与旧 `styles.css` 的低于 12px 文字升至可读下限，统一共享标题、表格、表单、按钮及 Posture 首页；`workbench-layout.css` 保留导航项 13px 并对齐使用费用、执行查询／详情、复核、连接及账号入口；`runtime.css`、`budget.css` 对齐运行和预算；`OverviewPage.tsx`／`overview.css` 保留首屏四项跨业务摘要，下面把原有趋势／贡献、活动／近期记录、关注／复核分别放在连续业务分区。值、权限、范围、未知与未报告语义及运行分组键未改；复核队列明确不受上方期间与项目筛选。`WorkActivityPage.tsx` 未改，原对象／动作／结果和来源边界继续沿用。`scripts/build-manual.mjs` 从前端共享 token 构建离线 CSS 前缀并计入 source digest，手册改为相同中性色、22px 标题、196px 目录与 800px 阅读栏；development-d 内容、版本／兼容检查、静态分发及签发校验器允许的 `/executions` 返回路径保留。

总体在真实页面中定位三处共享样式缺口：`overview-return` 被 UA 默认按钮样式覆盖；调查事项与正文策略的无专属 h2/h3 仍用 UA 大字号、正文策略短数值字段各拉满桌面行；会话卡片撤销按钮作为 grid item 被拉到约 1100px 宽。增量提交 `adaee7f4c688082c056576e303aaddf14c6b7cc1` 在 `workbench-layout.css` 定义低强调返回控件，`1b9bb8ede9647bf8693c5081ad264ea0b75e0713` 在共享 `workspace-v2.css` 补 16/14px 缺省标题并在原 `styles.css` 建立策略表单布局，`2cf1e6b6ed4a6a82402185f2ae45464d5c6ffec1` 把会话撤销按钮约束为内容宽度与 36px 高度。最终 `f3342712208847a84202b4631bc5b020c5e85016` 将策略表单收为桌面双列、标题／状态／提交跨行、窄屏单列。留存字段多一个单位 select，使同一网格行较高；较短字段的内部 grid 默认拉伸，将容量／单条上限输入拉高并下移，故只给原 label 规则加 `align-content: start`，使输入保留原控件高度并顶部对齐。未改字段、值、请求或撤销逻辑。

固定 `/private/tmp/xi-ui-reading-20261009/toolchain-bin` 的 Node 22.23.1/npm 10.9.8 下，先定向 `check` 与总览／活动 3 文件 40 项通过；稳定后完整 `npm run check`、`npm test`（38 文件／552 项）通过。共享标题／策略表单修正后 `ReviewPolicyPage` 与 `App` 2 文件／64 项定向测试通过；最后会话按钮与策略表单对齐只改 CSS，最终 `check`、`npm run build` 与精确 dist 清单通过，未重复全套 552 项。按 `node scripts/build-manual.mjs --profile=development-d` 重建，`node --check`、生成 HTML 的版本 meta／8 个本地引用与锚点、禁止外链、CSS 以完整共享 token 为前缀等检查通过；未改变 Go 源码。总体另回报 Go 1.25.12 的 `internal/webui` 全测通过，这不是本任务重复运行的结果。最新 dist 清单 SHA-256 `fafa7f806fad13520960f3046026fa932140419206783f5d1827230f78b1ea63`；手册 index／CSS／icon SHA-256 分别为 `91df3045ce7bd0b965e9c78f1708c248c28f5d1af801638fe38a490760cf6568`、`fb84ab6573b904c8ed6284394284d79e8e0d06163a5837e14ed95eb1d18db298`、`134170d1265a2fcbea258dba8db7ca7e9718b41c611a3ffd81d27a2244c4c14e`，手册 source digest 为 `sha256:88c08ac3f09d8c87d08ba24e6c2a52d76bfa61d2b67aa73628c022c7debe728d`。

本机浏览器复核与边界：总体在原 61712 预览按可用状态实看总览／用量／成本／运行／活动及正文、执行查询与详情、队列／案例／发现、访问／身份／连接和手册；广泛覆盖首先基于 `ce9847…` 资源。随后 `891c…` 资源定向复验返回按钮 13px／36px 且无 UA 默认边框、共享 h2/h3 为 16/14px、会话撤销按钮宽 104.83px。最终 `fafa7f80…` 清单的 `index-BDKVi2nu.css` 已在浏览器实际加载：策略表单桌面双列，select 36px／input 37.5px 且顶端对齐；360px 时单列 271px、控件 44px，document 宽 345px ≤ viewport 360px，无横向溢出。预算仅无权限态，案例／会话／发现仅空态，对比仅初始表单，连接设置仅缺控制器错误壳；深色及这些页面的非空／成功态未测，不能称全功能、全状态或用户已接受整体视觉。此为 Local 浏览器证据，CI、Target、Production 无本批新证据。当前同地址预览由 macOS 用户会话的 `com.foigent.xi.visual-preview-20261010` 运行，输出 `/private/tmp/xi-ui-reading-20261009/browser-visual-reviewed-20261010`；不声称开机自启，未改认证 TTL、ECS、凭据或会话策略，未推送／发布。

---

## 2026-10-10 — 总览与工作活动视觉样板本地浏览器复核

产品提交 `0461b501f45bb533d4db207840fcc24b7e52323e` 基于 `f9bda4f2d4d9238b13ad755ef94c4459ae337b08`，只改总览与工作活动列表／详情及前端共用尺寸 token；1920px 浏览器复核发现 AI 用途与费用贡献卡片的直接 `small` 和按钮同行、说明被按钮割开，后续仅两行 CSS 的 `ee1cfa6feda3d4eb77c56fc6e7d09c74269c3121` 令说明独立成行、按钮在其下方。最终 dist 清单 SHA-256 `8f8bd17b89f42b34b4cd327a412d47de89506198f6d59726eae76ccd9d29b185`。API、数据库、权限、筛选／分组键、正文核验和其他主题页未改；24px 页标题、18px 分区、14px 正文／控件、28px 指标、36px 控件形成统一层级，活动主行保留失败／可选缺失／来源报告状态，完整解释可逐条展开。

固定 wrapper `/private/tmp/xi-ui-reading-20261009/toolchain-bin` 的 Node 22.23.1/npm 10.9.8 下，首批 `npm run check`、完整 `npm test`（38 文件／552 项）、结构变更后 4 文件／47 项定向测试与构建通过；最终 CSS 修正后 `npm run check`、总览 1 文件／35 项定向测试、`npm run build`／精确 dist 清单校验、`git diff --check` 通过。早期活动汇总测试因 DOM 标签分离而失败，更新可见结构断言后通过，非产品失败；最终 CSS 修正未重复全套 552 项。

总体以原本机正式 Go handler＋PG16 受限角色、合成 `workbench.one` 登录在 Chrome 实际观察总览 1440/360/1920px、工作活动列表 1440/360px、详情 1440/360px。可见 h1=24px、h2=18px、主要控件 36px／14px，以上宽度无文档横向溢出；运行汇总 4 次读取为 2 完成、1 失败、1 可选不存在，0 写入，框架结束明确不代表业务完成；正文 `ocbody:p2-completed-1` 按钮实读 53 字节且摘要核验通过，360px 正文 client/scroll 均 277px；键盘 Tab 焦点可见。最终修正后重启原 61712 样板，实际加载 `index-DM1GN-7A.js`／`index-B0IwWrNI.css`，1920px 全页复核 AI 用途与费用贡献卡片的说明、按钮均为 block，按钮距说明 12px。其他活动代码未变，沿用前述活动浏览器证据。最终运行输出 `/private/tmp/xi-ui-reading-20261009/browser-visual-final-20261010`、同名 `.log`，session 52249；原人工模式使用显式 stop，当前留供用户查看，不在本记录步骤中停止或清理。

复盘：功能和无溢出检查不能证明贡献卡片在宽屏上可读；1920px 实图发现行内说明与按钮相挤，限定两行 CSS 修复并在同一原样板重载验证。当前只实际看过浅色主题，深色未切换；全部 13 页未完成视觉统一，CI、Target、Production 也没有本样板的新证据。**本地浏览器复核通过不等于用户已接受审美**；待用户反馈后再决定是否推广其他页面。GitHub 推送／CI／发布继续暂缓；本轮无部署、凭据／权限或模型流量操作。

---

## 2026-10-10 — 人工浏览器入口在等待期间超时

用户反馈原本机地址打不开。原 `browser-layout-harness.log` 证明自动测试在 10 月 9 日 23:46 达到 30 分钟上限后退出；原 PG16.14 仍运行。错误在于将自动测试生命周期用于等待人工证书确认，非产品服务故障。

修正落在原 `workbench_browser_postgres_test.go`：显式 `XINSIGHT_A0_BROWSER_MANUAL_ADDR=127.0.0.1:61712` 仅允许本机受限角色人工验收，配合 `go test -timeout=0` 等待既有输出目录的 `stop` 标记；自动测试仍保留 30 分钟上限，CI 拒绝人工模式。地址/模式正负用例通过，原正式 handler 已在该固定端口启动，日志确认 workspace 非超级用户且无 facts UPDATE，TLS 与产品资源保持。运行输出 `/private/tmp/xi-ui-reading-20261009/browser-manual-20261010`，日志为同名 `.log`；验收完成后由总体写入该目录 `stop`，不要在人工等待期间停止。

浏览器重载由“连接被拒绝”进入“隐私设置错误”；用户本人处理后，总体用原合成账号登录，实际完成总览→使用页→刷新→返回、活动运行汇总和两份53/50字节正文核验。总览360/1440/1920宽度与正文360宽度无文档横向溢出，桌面/窄屏截图已观察；运行无测量样本的空态、费用来源默认折叠、原合成审阅账号的执行查询及案件空态也已打开核对。没有非空测量或案件详情的浏览器证据，未扩大为所有13页验收；原源码测试证据范围保持。后续人工交接复用此入口，不能再将自动测试的临时就绪地址当作等待期间持续可用的入口。无 GitHub、签发、Target 或供应商调用。

---

## 2026-10-09 — 工作台层级与用途导航本地候选

- **精确候选与范围：** FPV17/schema45 工作台提交 `65a6a340fdd2d9ac56170147cc94c28d537064b2`，后续工具链／活动 key 修正提交 `6b85939435dfd7935a24a6f38bd2f14f8f649942`，后者 tree `6319f3fd316fa841932cae370bf896ee4de3deb8`；`internal/webui/dist/asset-manifest.json` SHA-256 `230ad479d4840d9466cc94f9e6068f7b0a19b292a6f692be5c12ae88baa9afee`。改动涉及前端、测试、设计、生成资源及 CI 前端工具链接线；不改 API、数据库、权限、受保护运行时或版本。本节是本地候选，不是合并／CI／Target 状态。
- **原设计→入口→差异及原因：**

  | 原设计／客户问题 | 当前入口与本批结果 | 仍有的差异和原因 |
  | --- | --- | --- |
  | 07、DESK-01—06、32／36／v11 的总览先回答使用、运行、费用和待办 | `/inbox` 首屏四项横向摘要→趋势与待办→模型、用途、费用贡献；近期运行独立于费用，工作活动作为下钻入口 | 来源覆盖和资源尝试保留但下移；旧 Posture 优先级不能代替客户工作台（见 A-C 设计第10节） |
  | 用途按本次可读事实解释，未知不等于零 | 总览仅在冻结阅读包含 `purpose` 时显示已采用用途数；使用页按可用维度 `purpose`→有已知操作者时 `account`→`source_project`；旧无选中首屏引用可安全回退 | 真正的用途归属仍依赖人工采用的经济事实；没有此维度时不会虚构分类或 0/19；已选详情、撤权、过期均不静默重建 |
  | 运行、能力、执行、案件、费用以任务问题为先且保留证据 | `/workbench/runtime` 突出可比测量和请求结果，保留全部排除计数与六字段分组键；`/executions`、`/cases`、费用页将长代码及覆盖解释收在披露区 | 四种正式测量结束边界均可读；`http_do_returned` 仅说明 HTTP 调用已返回，不代表业务成功；选中模型仍是引用，不伪称显示名 |
  | 工作过程可核对并进入准确模型请求 | `/work/activity` 仅按 reporter 精确范围＋显式 run_id 汇集当前页；既有当前修订、已匹配 UR 标识的工作活动→runtime 导航与返回保持 | 没有 runtime→工作活动反查、跨页运行全集或真实资源反向关联；runtime 资源面仍仅合成内部记录，不能用时间／文件名推因果 |

- **回归与边界：** 用真实前端 transport 的回归先在旧导航路径复现 `purpose` 403，再在候选上证明不发出该请求；选中用途详情仍拒绝回退。原 shell 默认 Node 23.11.0/npm 10.9.2 的 552 项是历史 Local 结果，原因是本次最初未查到并应用已有固定工具链 wrapper。补核后逐条命令以局部 PATH 使用 `/private/tmp/xi-ui-reading-20261009/toolchain-bin`，实际打印 Node 22.23.1/npm 10.9.8；`npm run check`、`npm test`、`npm run test:coverage`（各 38 文件／552 项）、OpenAPI 100 路径、license 195 锁定条目、`npm run build` 与 dist manifest 校验均 PASS。新 `precheck`／`pretest`／`pretest:coverage`／`prebuild` 只读取 `package.json` 的 `engines`／`packageManager`；错误 Node 23/npm 10.9.2 在四种入口实际返回 1 且停于前置步骤，固定工具链均通过。CI frontend job 在 `npm ci` 前从 `packageManager` 安装指定 npm，原 `test-ci-signed-release-wiring.py` 对顺序和移除负例 PASS；这仍是 Local CI 接线检查，非 GitHub CI 已运行。
- **细小正确性修正：** 同 event_ref 可在不同 reporter 范围出现，旧运行标题 React key 只用 event_ref；现用分组所依赖的完整 reporter＋run 身份 key，同范围和异项目同名事件回归通过。固定工具链覆盖率运行曾暴露总览测试保留卸载 DOM 引用的竞态，测试改为每次异步断言重新选取当前卡片；无产品逻辑变化。PG16 与正式 handler 浏览器验收由总体另取回执，当前未运行；CI、Target、外部集成及 Production 无新证据。无推送、签发、部署、凭据／权限或模型调用。
- **明确未完成：** 缺真实用途供数、runtime→工作活动反查、可审人工比较录入、授权角色页面；这四项不因层级调整而完成。用途当前以 `workbench_purpose_revisions` 的人工命令绑定经济 execution，工作活动仅有来源声明关系和准确 UR 请求匹配；若后续要连接两者，须另定身份／授权／修订合同，不能按时间、同 run 或文件名自动分类。全部 13 页完全对齐仍待后续范围和真实页面验收。

---

## 2026-10-09 — FPV17 工作活动读取本地候选与版本升级预检

- **事实分层：** GitHub 只读回读 `foigent/x-insight` main `05a36eda4ecd8dec79173a8c1e111c680d1d907c`，PR118 已合并；本地 `origin` 是旧 checkout，`github` 才是该仓库远端。第61份当前事实源第2225–2239行记录前批原 xi-reader 对新 run `07c1a367-acaf-413b-b0d6-e4550739663b` 的 8 份工具正文、该 run 与原 run `b10d9149-9818-4268-8685-e344ae48504c` 的 4 份模型正文，总计 12 个不同 content_ref 实际按钮读取并核验字节与摘要；累计18/20次供应商调用，保守费用计算界¥4.354599/¥10，供应商账单未核。本批不重复付费任务，也不把前批 reader 完成算成本批 Target 验收。
- **本地候选与证据：** 分支 `codex/xi-work-activity-reading-20261009` 基线为上述 main；产品与 FPV17/schema45 元数据提交 `5cefc8942f17b221430980571fdeb34243fdf86d`，前一页面修正提交 `ec550163574d4afa2f90fca6bed99ec4d5b89a39`。隔离 PG16.14 的 `TestWorkEventIndependentAdmissionQueryAndLateRelation` 与正式 TLS handler 的 `TestWorkActivityScopedHTTPAndRuntimeRoles` 均 PASS，日志及 SHA-256 见 `/private/tmp/xi-ui-reading-20261009/acceptance-handoff.json`。本地合成 reader 的真实页面核验：360px 的 document/body 均345px，1440px 均1425px，1920px 均1905px；53B 与50B 两份正文实际按钮读取并匹配摘要，切换事件旧正文消失；总览三尺寸无整页横溢，活动→关联 runtime→返回保留期间、筛选与所选事件。浏览器 harness 正常 PASS，隔离 PG16 正常停止，日志和回执保留。这是合成来源、本地 HTTPS 页面证据，非 Target。
- **版本与升级边界：** 受管 `upgrade --check` 在目标产品版本与当前相同且非 legacy 时明确报 `target release is already active`，即不同 commit 的 FPV16 包也拒绝；定向回归 PASS。D 配置、矩阵与内嵌手册现为 `0.2.0-dev.fpv17` / `manual-r1`，升级来源增加准确 `0.2.0-dev.fpv16`，schema45 与冻结 rc13 不变。现有同 schema 跨版本 hostctl 测试补核 check 前后 install state、journal、current symlink、迁移账本不变，再执行 upgrade；同版拒绝、同 schema 路径及跨 schema 无副作用三项定向测试 PASS。干净树 D profile dry-run 输出 `product_version=0.2.0-dev.fpv17`、`generated_assets_verified=false payload_built=false signed=false`；这不是 FPV16→FPV17 已安装 Linux 或 Target 资格。
- **FPV16→FPV17 CI 升级候选：** 已在 `.github/workflows/ci.yml` 的 `fresh_install` 增加独立 `fpv16_upgrade` VM；旧包固定已合并 FPV16 commit `f2b42d60ff8961c1216cbe54ce15008f4bbec1a9`，由旧源码构建、验签并正常安装受管 schema45，明确是 CI 专用重建包而非 Target r2 字节。既有 FPV14 `adopt_existing` 故障/恢复分支保持独立。新分支在 UR 夹具仍在线时运行同一冻结 FPV17 候选的正式 `upgrade --check` 与 `upgrade`：仅在只读 check 快照窗口暂停并恢复来源/经济 worker，实际升级以前后均活跃的 worker 为前态；核对 check 对数据库、配置、原正文密钥、安装指针和服务无副作用，升级后 schema45、原历史/正文密文/授权/游标与原安装身份、原 reader 重新登录后实际正文、目标服务与 worker 新二进制；再由原 UR 源发布新记录，等待 XI 来源/正文及经济 feed 实际读取。固定 UR 夹具会清理子进程环境，因此升级参数经其原有私有 stdin 指向的目录中 0600 限定请求传递；定向子进程测试实际模拟四项清理后的环境，缺少请求时原两模式保持。仅脱敏合成回执和固定失败阶段/退出码可上传，原快照及诊断留在一次性 VM。此为本地已接线且定向测试、Linux/amd64 Go 编译通过的候选，**未运行 GitHub CI**，不能写作安装资格已通过。另需同 commit 的 CI 签名整包资格、获批 Target 同前态 check、升级和浏览器验收。本次未推送、未正式签发、未部署、未读生产凭据。

### 本批四项复盘

1. **来源语义与 actor：** 可选路径不存在只有在资源生命周期明确 `failed` 时归为可选缺失，其他 `read.not_found` 保持未知；事件 actor 是执行身份，来源未给任务发起人时明确写“来源未提供”，不把人类 actor 推断成发起人。数据库分型、页面文案和 OpenAPI 一致。
2. **运行快照与成员变化：** 汇总、选中事件和分页在单一 repeatable-read 快照内形成；游标绑定授权版本、截止时间及成员摘要。后续发生反填修订或成员变化时返回冲突，避免把跨快照拼出的列表说成完整同一运行。
3. **夹具计数：** PG 分页回归的期望值按独立夹具实际事件数推导，覆盖100+后续页和迟到关系，不再以硬编码总数掩盖夹具变化。
4. **正文布局与生成物：** 窄屏长正文仅对已核验正文 `<pre>` 做换行约束，并同步提交 `internal/webui/dist`；最终真实页面按上述三尺寸和两份正文验证，早先只看到加载态的截图不算展示通过。

---

## 2026-10-09 — 工具正文策略刷新空窗本地最小候选（未采用至 M4）

- **边界与改动：** 在交付树现有未提交的只读诊断记录上，仅改 `integrations/openclaw-xinsight/index.mjs` 及其原 `index.test.mjs`。保留共享 outbox 的 60 秒策略确认过期、启动失效、明确撤权／范围变化处理和 `tick` 总 catch；把成功配置后的下次刷新从确认后 60 秒前移到 30 秒，且首次权威 bindings 一经取得便写入共享 outbox，再执行未来 Agent 候选报告及正式二次配置。候选确认本身仍不授予新范围，二次配置只以 XI 正式返回为准；本轮未改其他生产逻辑、M4 包、Target 或历史 run 正文。
- **失败前态与回归：** 以当前最终 `index.test.mjs` 搭配原 HEAD 的 `index.mjs` 在隔离副本运行，两个定向用例确定性 RED：延迟 5 秒的正式配置刷新在旧 60 秒调度下不能在边界前完成，工具 input/output 从 `referenced` 变为 `not_captured`；managed 候选报告未返回时，原实现尚未发布已取得的权威 binding。当前候选同测试 GREEN。原 `index.test.mjs` 另覆盖正式 `tick` 下刷新失败并过期不采、正式 disabled 策略不采、同 run 绑定 revision 变化拒绝沿用、候选已报告但尚未经正式配置批准的新 Agent 不入范围；原 `outbox.test.mjs`／`producer.test.mjs` 仍验证跨 worker 60 秒门槛与当前策略优先。完整插件测试 `node --test integrations/openclaw-xinsight/*.test.mjs` 为 70 PASS／0 FAIL，`bash scripts/verify-protected-runtime.sh` PASS，`git diff --check` PASS；准确日志和候选补丁冻结在 `/private/tmp/xi-body-policy-candidate-20261009/`。
- **安全余量与未结：** 在空闲最多约 5 秒的 tick 调度和单次配置 GET 默认 5 秒超时下，30 秒提前量给正常刷新留出余量；先写 bindings 避免未来候选报告默认最多 15 秒及其二次配置阻挡既有授权映射。串行 tick 若被较长正文投递、续期、网络故障或反复失败占据，仍可超过余量；此时继续在原 60 秒确认门槛停止正文采集并留 gap，不能承诺无条件连续采集。总 catch 对非策略异常也失效策略是独立未证问题，本候选未收缩。原 run 的 8 个缺失正文不可由此源码修复补采；无重放、补发、凭据读取或外部调用。受保护运行时摘要未涉及本两项产品／测试文件，既有只读校验通过；无摘要接受或更新。后续需总体独立复核此精确本地候选，再决定采用和 M4 生命周期操作。

## 2026-10-09 — M4 工具正文策略确认空窗（只读诊断；未实施修正）

- **范围与事实：** 本轮只读核对原 c783 宿主包的 `producer.mjs`、`outbox.mjs`、`index.mjs`、`transport.mjs`（四文件 SHA-256 均与本交付树对应源码相同）及 `/private/tmp/ur-deepseek-probe-dispatch-delivery-20261009/xi-new-run-readback.json`、`body-policy-gap-readback.json`、`m4-history-metadata.json`；未改插件、策略、权限、凭据、流量或运行状态，也未重放 run。唯一补验 run `b10d9149-9818-4268-8685-e344ae48504c` 的 XI 回执有 19 事件、2 条准确 UR 关联、2 个已接受的 `model_input`／`model_output` 正文；四次 `read` 的四个 `tool_input` 与四个 `tool_output` 事件均为 `not_captured`。M4 本地 gap 累计新增 8 次 `body_policy_unconfirmed`，时间集中在 `2026-10-09T04:57:32.071Z` 至 `.135Z`，与工具 hook 时间相符。模型输入 `04:57:24.675Z`、模型输出 `04:57:39.833Z` 都已引用正文。`05:04:30Z` 的稍后快照仍显示 `full_content`／`xi:default:1`／90 天，但它不能反推 `04:57:32Z` 的精确确认时间或排除当时瞬时刷新失败；`receiver_unavailable` 的最后记录为 `04:34:55Z`，不是本轮工具窗口。
- **代码机制与判断：** `outbox.bindings()` 在 `now-confirmed_at_ms >= 60000` 时只移除 `body_capture`、保留事件身份；`producer.observe()` 对缺失的当前策略写 `body_policy_unconfirmed` 并把元数据标为 `not_captured`，不从 run 记忆中的旧 binding 恢复正文。`index.tick()` 在成功 `replaceBindings()` 后才设 `nextConfigAt=Date.now()+60000`，即到策略失效边界才开始下次网络刷新；同一 tick 还串行候选报告、配置二次获取、状态报告和投递，空闲调度通常可再等最多 5 秒。故即使授权策略未改变，正常网络时延也能产生采集空窗；本次“前后模型正文有、恰在边界的八个工具正文无”的形状与此高度一致，但缺少当时的 `confirmed_at_ms`／tick 轨迹，不能把调度竞态认定为唯一已证根因。90 天是已采集正文的保留期，不是宿主的 60 秒采集授权确认期。
- **保护目标与最小建议：** 保留“确认已过期或明确撤权时不采正文”、绑定身份和版本核对、正文投递前再次取正式配置；不要靠延长 60 秒有效期或从 run 缓存复用旧策略来消除空窗。将配置刷新安排在失效前留有网络余量，并让首次成功取得的正式 binding 先原子写入共享 outbox，再做未来 Agent 报告等非关键串行工作；刷新超时仍在现有失效边界按 `not_captured` 留 gap。另审 `index.tick()` 总 catch 对任意异常都 `invalidateBodyPolicy()`：它把非策略性的投递／本地错误也等同授权失效；本次无同窗证据证明走到此分支。若后续收缩该分支，须保留启动、明确 401／403、scope／policy 变化和无有效确认时的立即失效，并单独验证短暂错误下的授权上限。
- **可复现的原测试入口与缺口：** `node --test --test-name-pattern='body policy freshness is shared' integrations/openclaw-xinsight/outbox.test.mjs` 已有精确 60 秒过期与跨 worker 行为；`node --test --test-name-pattern='an ongoing run uses current retention policy' integrations/openclaw-xinsight/producer.test.mjs` 已有未确认策略下元数据保留、正文缺失的行为。现有 `index.test.mjs` 未覆盖“刷新排在 TTL 边界”；实施前应在该原入口加可控时钟／延迟配置响应回归，证明边界前预刷新保住未变策略的工具输入输出，同时验证超时、disabled、scope／revision 改变时仍拒绝采集。本次只读诊断未运行或增添测试，未补发历史缺失正文。

## 2026-10-09 — 原生插件安装已保存但旧源仍被选择：恢复判定修正

- **所有者、授权与基线：** Codex 在原干净交付树上承接本次安装器修正；新分支 `codex/xi-plugin-registry-recovery-20261009` 以已合并 main `b05c37413fb3ae7803608a7f47f020dc5b93ce70`／tree `cb928de87285e192d4e6a4955826e52978a45f27` 为基线。范围为 `integrations/openclaw-xinsight/install.mjs`、原测试替身／回归和本两份既有入口记录；风险按 R2 宿主生命周期状态真实性处理。已授权本地检查、PR／CI／合并；M4/ECS、凭据、模型调用与流量均由总体另行负责。
- **真实反例：** 总体提供的脱敏 `/private/tmp/ur-deepseek-current-flash-20261008/m4-native-registry-after-failed-install.json` 显示首次原生 install 返回 saved+failed 后，配置路径为 `[旧,新]`，原生 registry persisted/current 的 `xi-work-events` 安装记录都指新目录，但所选旧目录行无 owner；`plugins inspect --json` 顶层 `install` 因 ownership 解析失败而缺失。该缺失不能证明新安装未持久化，也不构成首次安装无中断成功。此前替身把 `inspect.install` 直接映射原生安装目录，未覆盖“新记录已保存、旧源仍优先”的真实组合；旧实现恢复时抛 `existing_plugin_source_conflict`。
- **修正与验证：** 安装器通过公开 `plugins registry --json` 独立比较 persisted/current 同一安装记录及插件行，核对原／目标真实目录；只允许来源选择变化造成的本插件局部 stale 过渡态，拒绝缺失、第三来源和冲突记录。记录已指目标时不再调用 install，仍保留精确 `plugins.load.paths` CAS、安装身份／密钥核对、reload 与完成态检查；未知超时或失败不会自动视为成功。修正测试替身后，旧实现在该前态的定向用例以 `existing_plugin_source_conflict` 失败；新实现的普通 failed+saved、外部精确 CAS 后 stale 行、两侧记录冲突与原回归均通过。首次原生 install 如 saved+failed，本次仍立刻报错；新增测试证明下一次同安装 resume 不重复 install，并不声称首次无中断成功。`node --test integrations/openclaw-xinsight/*.test.mjs` 为 67 PASS／0 FAIL，`bash scripts/verify-protected-runtime.sh` 和 `git diff --check` PASS。本修正由 PR117 交付；准确 CI 与合并状态以该 PR 和最终交付回执为准。
- **恢复与未结：** 本源码修正不触碰运行中 c783 插件，首次原生 install 的原失败仍保留。总体单独核对 M4 精确 CAS 和同一正式安装器 resume；本记录不把本地模拟或原生路径选择回执升级为 Gateway 已采用新源码或新事件验收。若本 PR 检查失败，在本分支修正并重跑精确 head；未发布制品，不涉及宿主回滚。
- **后续运行读回与测试边界：** 总体的 `/private/tmp/ur-deepseek-current-flash-20261008/m4-xi-plugin-upgraded.json` 确认原 M4 对 c783 包的正式 resume 为 `activated_waiting_for_events`，原身份、17 Agent、其他配置和两把密钥保持；这不是本 PR 新源码在 M4 运行。之后唯一 run `51e1d47d-d72e-4577-9a20-faf071d3ae78` 已 failed／无 active，脱敏 history 显示三个显式 `optional:false` 的 `read` 被原生参数校验拒绝，`optional:true` 缺失读取正常返回，文件摘要不变；这属于总体验收输入错误，不是插件失败或严格读取成功。现有 producer 测试本来用省略 `optional` 的严格读取，未把 `optional:false` 当合法；补充定向断言：结构化拒绝若以 `after_tool_call.error` 进入 XI，只能产生 `tool.failed`／`resource.read.failed`，不得产生 `resource.read` 完成态。XI 只读回执 `/private/tmp/ur-deepseek-current-flash-20261008/xi-plugin-upgrade-outcome-readback.json` 确认该 run 已接收 25 个事件、15 个已接受正文和 2 条 UR 关联，含三条 `resource.read.failed`、一条 `resource.read.not_found` 与 `task.failed`；这不证明严格读取成功或浏览器正文验收。

## 2026-10-09 — Flash 成功读回与 XI 本地修正候选

1. **真实运行证据：** 总体在原 M4 `xi-acceptance` 上完成 Flash 续轮 run `20cfd856-9053-4f15-907c-43062c260648`，整批新增供应商调用 6/20；成功 run 的两个 UR 请求均为 200、已结算。XI 自动收到 16 事件、8 正文和 2 条准确 UR 关系。原 reviewer 实际读回工具返回 179 B 与同一请求 `provider_request` 22,713 B。证据位于 `/private/tmp/ur-deepseek-current-flash-20261008/` 的 `m4-native-result-continuation.json`、`ur-flash-outcome-final-readback.json`、`account-recovery/xi-history-readback.json`、`xi-browser-body-final.json`。这纠正下节 10 月 7 日“DeepSeek 无路由”的历史前态，不推定所有历史请求或其他模型都成功。
2. **源码候选与风险边界：** PR115 head `e6171cd450f34c3eb4b386f48602f4f80eabc777` 上的本地补丁分别修正 `agent_end` 对模型错误的任务终态误报、可选缺失 `read` 对文件内容的误报，并为同一受管安装加入显式原生插件目录切换。安装器在原生切换前核对旧／新 package 与 manifest、原安装身份、现有两把密钥和 16 KiB 状态持久化能力。首轮本地插件 60 项、Go 合同定向和前端类型／页面定向检查通过；八个产品／测试／合同文件由 CI 路由判为 `release-relevant`。GitHub 交付以 PR116 的准确 PR／CI 回执为准；M4 仍是旧 `27a5` 插件，真实 Gateway 升级及回退尚未验收。旧 XI 事件不可变，新分类只适用于未来来源观察。
3. **PR116 首轮失败与原入口修正：** 初次 head `04f31f28c28d1cc9d13a33dee6eab92899cd9255` 的 [CI 37813137149](https://github.com/foigent/x-insight-private-history/actions/runs/37813137149) 在 `Verify deterministic committed assets` 及签名候选构建处发现前端源改动却未提交新的 `internal/webui/dist`。推送前运行过 `git diff --check`、`scripts/verify-protected-runtime.sh`、插件／Go／前端定向测试与 CI 路由自检；这些入口没有重建后比对已暂存资源。修正补齐生成物，并沿现有前端 CI 入口以 Node 22.23.1／npm 10.9.8、当前锁文件 `npm ci`、`npm run build` 和 `git diff --exit-code -- internal/webui/dist` 核对，另有类型检查和页面定向 3 项通过。受保护运行时门禁未改；首轮失败 CI 保留为失败证据。
4. **安装器替身误判与真实前态：** OpenClaw 2026.9.5 原生 `plugins install --link --force` 对 `plugins.load.paths` 仅追加新目录；旧、新同 ID 显式路径并存时，加载选择仍为旧目录。原替身只改变安装索引 `rootDir`，未模拟路径追加和旧源胜出。修正用实际 `plugins inspect --json` 的 `plugin.rootDir` 与顶层 `install.sourcePath/installPath` 分别检查选中源和安装记录，确认后才通过原生 `config set plugins.load.paths --strict-json --expect-current-json` 精确替换旧目录并保留其他路径；reload 后再检查本地选中源，最后更新受保护状态。安装回复丢失若记录已是目标则跳过重复安装；CAS 冲突、第三来源、旧源仍被 CLI 导入时不写完成状态，正向与回滚均覆盖。Node 22.23.1 下全插件 63 项、最终安装器定向 17 项通过。`inspect --runtime` 是 CLI 进程的导入检查，不是 Gateway 实际采用的独立证明；后者需原生 reload 回执与升级后的新事件。

下节保留 10 月 7 日当时的状态与改动，不作为当前 Flash 路由或插件采用结论。

## 2026-10-07 — 历史前态：M4单 Agent 工作链与 PR115 交付收尾

1. **目标、结果与证据层：** 目标是在原 M4 OpenClaw 2026.9.5 上沿原 UR Key102／org2／project86 验证单 Agent 客户工作链，并交付选定 Agent、宿主插件生命周期和正文代理边界修复。本批仅将 `xi-acceptance` 选为 XI 受管 Agent，新增 XI 插件为 `xi-work-events`；原 16 个 Agent 保持，本机共 17 个 Agent。总体已移除 ECS 额外 OpenClaw，不再需要此前假设的 ECS 专用模型 key。总体回报 GLM 7 次真实供应商调用，文件结果及 reviewer 正文已核验；DeepSeek 现网无可用路由，没有 DeepSeek 供应商成功或拒绝结论。[PR115](https://github.com/foigent/x-insight-private-history/pull/115) head `e6171cd450f34c3eb4b386f48602f4f80eabc777` 的[全部适用 PR CI 37610953975](https://github.com/foigent/x-insight-private-history/actions/runs/37610953975)成功，已合并为 main `83a16a54ba31c244cd6d5e1986d842d904ac3888`／tree `a10ee2105e68a5ae3127336234ecdaa42e467b36`。该 main head 的原生 [CI 37614771582](https://github.com/foigent/x-insight-private-history/actions/runs/37614771582)整体 success：分类、签名资格、新装／旧装资格和 Backend Tests 成功；Frontend、Backend/API、PG16 rotation、FPV9／FPV11 依路由 skipped。CI 的签名候选是非发布资格，不是 Target 安装或正式签发。
2. **偏差与分类：** 原 XI HTTPS NGINX server 的通用 8 KiB 限额先于 Go 的 32 MiB 正文界限返回 413，导致八份原 `model_input` 正文缺口；这是代理配置与真实接收链缺口，不是模型提供方失败。总体另行修复 Target 精确正文 location 后，一份新的 35,784 字节 `model_input` 已接受，原八份不回填。第一次新 PR CI 的旧 `hostctl` replay 锁测试未观察到 `FOR SHARE` waiter；这是测试同步竞态，非七文件代理补丁改变了 DB 产品代码。`Backend Tests` 随前置 Backend job 失败而按合同失败；最终修复后准确 PR CI 两者均通过。DeepSeek 无路由属于 UR pre-provider 路由事实，不改写成供应商拒绝或 XI 正文失败。
3. **错误假设与漏检原因：** 早期把 ECS 新 OpenClaw／新模型 key 当成必要前提，忽略原 M4 及原 UR Key102 已可承载授权范围内的 GLM 文件任务。仅有 Go 正文上限单测和本地插件队列测试，看不到外层 NGINX 的 8 KiB 限制；此前联合安装夹具也未从正式模板抽取精确正文 location。replay 测试让已完成回执的快速第二事务与另起的 `FOR UPDATE` 竞争，不能稳定保证行锁先到；首轮简化虽可通过首事务过期断言，却丢失“预约已通过、重放第二事务锁后过期”的独立负例，因此复核后保留后者。源码合并亦不会自动替换 M4 已载入的旧 `27a5` 插件或 Target 包。
4. **原入口修正及正反例：** PR115 在 `integrations/openclaw-xinsight/install.mjs` 固定选定 Agent，并让原生 install／enable／reload 使用 600 秒生命周期期限、其余调用仍为 60 秒；原 `install.test.mjs` 覆盖未知 Agent、其他 Agent 工作区放凭据、超时后重试和 reload 未确认。`deploy/xinsight-nginx-ip.conf.example` 只放宽精确正文 URL 至 32 MiB、关闭 NGINX 请求缓冲，其他 URL 保持 8 KiB；原 `scripts/qualify-ubuntu22-installed-joint.sh` 在真实 NGINX 的新装／旧装两模式先写非秘密收据再断言：正文 16 KiB／32 MiB 匿名 401、32 MiB＋1 字节 413、普通入口 16 KiB 413，四次 curl 均退出 0。`body-outbox.test.mjs` 证实 413 为终态 `body_too_large` 而非 `body_invalid_item`。`internal/hostctl/source_facts_offline_check_test.go` 的 replay 先排队同一 source advisory lock，再释放预约 INSERT gate，让第二事务在授权行锁之后读取时钟；总体在 Go 1.25.12／PG16.14 对四个子场景 `-race` 实跑 0 SKIP 全通过，并故意移除第二事务 authorize，replay 负例随即失败（`/private/tmp/ur-xi-m4-openclaw-20261007/replay-pg16-verification.json`）。本地插件 53／53、保护 gate、CI 路由自检通过；PR CI 的 Backend/API、Frontend、PG16 rotation、非发布签名资格及两种安装资格全部成功。两份精确 NGINX 收据分别在 `/private/tmp/xi-pr115-ci-e6171cd-20261007/new_install/_temp/xinsight-installed-joint-qualification/nginx-body-ingress.json` 和同根 `adopt_existing/_temp/xinsight-installed-joint-qualification/nginx-body-ingress.json`，摘要同为 `sha256:cd33d0aad06b0a197459f4097b36c3a8b9eb4b2b5e24ac86285cfc910eee750b`；匿名 401 仅证代理放行，不证 32 MiB 密文托管。
5. **剩余与下一动作：** Target 仍是原 FPV16 安装；本机仍载入旧 `27a5` 候选，缺本 PR 的生命周期等待和 413 分类。现有安装器同时固定已安装插件 `rootDir` 与 `installation.json` 的 `plugin_dir`，切换新不可变插件目录会拒绝，升级路径未完成；不能从源码合并或 CI 成功声称 M4 插件已升级。原八份正文不会恢复；reader 浏览器原文尚未验收；DeepSeek 现网无路由，UR 本地修复的外部采用待独立决定。此次只在既有两份项目记录落盘、回读并监看 CI，未连接 Target、读凭据、创建模型请求、签发、部署、修改 M4 安装或再提交代码。

下节保留同日较早的 FPV16 Target 恢复前态；当前工作链状态以上述新条目为准。

## 2026-10-07 — FPV16/schema45 Target升级与原连接恢复

- **运行身份与升级：** 总体按已批准操作单在原XI实例完成FPV15→FPV16/schema44→45；运行源码仍为 `f2b42d60ff8961c1216cbe54ce15008f4bbec1a9`，正式r2 bundle `sha256:e118b5754ee396b21be32db4b47f90d8252cd585ee6e673ca2e91f395ed098ee`、manifest `sha256:bb11511df8074d181ffafc0d6982348ed86ad0fb330fce98fe3a1114eeb1e5bf`、payload `sha256:dabc4384f4e3479b7c29aa5c60a6100955717291ca6ca47af3116858a9a44194`。正式upgrade operation `20261006T223634Z-17c39ac28f348993` completed。此为Target回执，不从源码／CI推断。
- **原连接与实际读取：** XI／UR正式浏览器双端对原安装 `xci_khXsqM6fp6PzPAbV5VDAAw` 同scope恢复，XI `xcr_W4PBgwe2Cr_BNENdTdsJ7w` committed；原identity `xsi_Qnq9bQJWTLjDPRWsekYgHw` 保持，旧token撤销，新token有效至 `2026-10-07T22:44:46.898667Z`。控制器自动启动worker，desired/effective=1；原project86八条stream active、C `legacy_pending=false`、10份正文accepted、原C序号20；新获批project87自动active，无重复配对。原reviewer经正式HTTPS `/work-activity/contents` 读取1817字节，SHA-256 `aac8b1b38735df22395e0c00ed6f9fe2624b89b652630b4902502bda04c67ce0` 匹配；该回执证明受限API读取，不冒称浏览器展示了原文。
- **保持与边界：** 原25条evidence、41条fact、73条observation、52条manifest和52条receipt历史逐行hash保持；原accounts、正文key、ID env和source config文件hash保持。恢复回读未见新observation；尚无新模型请求，不能据原历史或自动worker状态声称跨24小时Target连续性、真实供应商调用或OpenClaw双模型文件工作链完成。OpenClaw安装和浏览器整链仍待原授权内验收；邀请和模型key尚未创建、未产生本轮模型费用。10月3日归档插件的 `integrations/openclaw-xinsight` tree与本次运行源码相同，可复用原批准归档，不需新发布身份。
- **签发偏差与修复：** 首次正式`pack`沿准备命令的commit epoch `1791294257`（2026-10-06T13:44:17Z），早于本轮key `not_before=2026-10-06T22:23:44Z`；`verify`正确拒绝，失败包保留且未入Target。沿同一已批准私钥、同150项源输入，用实际签发epoch `1791325699`（2026-10-06T22:28:19Z）写独立r2包，正式验签后精确删除本轮私钥；没有放宽trust、重建密钥或覆盖失败包。原`build-signed-release.sh`取commit epoch，准备`signing-command-plan.json`也继承了该值；下次签发前应在**原准备／签发入口**核对key `not_before` ≤ manifest `created_at` ≤ 验签时钟＋5分钟，再执行不可变`pack`。这是本次发现的入口前置缺口，本轮只记录，不改冻结运行源码或补发制品。
- **收尾边界：** 本轮UR／XI两条临时SG规则和XI UFW规则均已撤销，其他规则不变。原reviewer的正式HTTPS正文读取成功后，会话注销HTTP 204成功；临时验收脚本误把注销限定为200，已修正断言，未重跑登录／读取，也不归为产品失败。原project87的模型key与OpenClaw邀请均未创建，模型调用数0；后续仍待即时邀请确认、M4新增私有下载位置及既有Sponsor角色决定。
- **证据与剩余：** 原supporting目录`p3-fpv16-original-connection-result-20261007.json`聚合升级、恢复、正文读取、临时网络收尾与未完成边界；M4本批`signing-execution-r2.json`、`original-recovery-result.json`、`original-body-read-result.json`保存分项回执。Target事实由总体执行并回报，本XI文档任务未重新连接Target。后续以原即时管理员邀请、受管OpenClaw2026.9.5安装及两项真实文件任务取得独立证据；单次阶段通过不替代完整浏览器客户工作链。

以下保留上一源码／CI阶段的原日期与截止状态。

## 2026-10-06 — FPV16源码／CI阶段结算；Target与实际连接仍待

- **结果：** PR112合并为main f2b42d60ff8961c1216cbe54ce15008f4bbec1a9，同树PR及精确主线的适用CI／协议成功。主线实际通过两条安装资格；被路由跳过的其他作业使用同树PR结果，不冒称重跑。
- **偏差：** 生命周期driver已经通过，外层成功判定却仍把service_restart／preserved_history当成最后阶段，误判新终态；此前还发生正式角色／schema授权与历史样例混用、重启socket尚未就绪、诊断吞掉真实UR错误类别。
- **原入口修正：** scripts/qualify-ubuntu22-installed-joint.sh的installed_joint_passed检查子进程退出码、合法模式、完整必要阶段及唯一末尾passed结果；scripts/test-ci-signed-release-wiring.py直接抽取真实函数，以历史实际回执证明旧拒绝／新接受，并拒绝缺阶段、重复或非末尾结果、非零退出等负例。权限规则复用workspaceConnectorFunctions，历史schema样例保留；固定错误类别和有界就绪读取落在原实现。
- **验证边界：** 新装及旧连接跨期、丢失响应、恢复、撤销与历史保持均为实际合成Linux CI；不是Target或供应商真实调用。M4已准备150项准确main未签名输入，尚无FPV16签名和Target升级。
- **当前剩余与责任：** 总体2026-10-06 22:42 CST只读回报Target仍FPV15/schema44、原控制身份过期未撤销。UR实际升级前态发现的M124→M125两处入口遗漏已由PR433修正并合并为main `5412769528de1d63328900663d938a96e9dc5aad`；同树PR完整CI37489026277和准确main协议37496389077成功。XI保持本批准确输入；两端Target升级、同连接恢复、原连接收数、正文读取及OpenClaw／浏览器证据仍待实际完成。

以下保留其原日期与截止状态，最新源码／CI以本节为准。


## 2026-10-06 — FPV16/schema45 控制凭据生命周期（当前 Local 候选）

- **PR112 第四轮 CI 与下一候选：** [主 CI 37456275687](https://github.com/foigent/x-insight-private-history/actions/runs/37456275687) 在准确 XI `ccae058eec55fb4db073ed1ae79a38610322047f`、UR `41e580c08fafc4d3ed193784620566934785c01f` 上的 Backend and API Validation、Frontend Tests、PostgreSQL 16 Admin Credential Rotation、Signed V2 Release Qualification 成功；[协议 CI 37456275803](https://github.com/foigent/x-insight-private-history/actions/runs/37456275803) 两项成功。新装与 `adopt_existing` 的安全收据均有 `setup client action=status phase=dial category=socket_missing` 3 次，随后越过 XI 过期状态 GET、XI 管理员恢复签发和 UR 恢复预览；均在 `credential_offline_expiry` 的 UR 恢复确认 POST 返回 HTTP 503，终态 `lifecycle_ur_human_confirmation`。该轮 XI 只识别 `code` 包络，因此将 UR 的 `error` 机器码记为 `unclassified`；`Backend Tests` 汇总门随安装资格失败。旧连接的前驱恢复、升级及历史保持，新装/旧连接的自动轮换和两种丢响应阶段已通过；已准备操作跨期恢复、最终撤销及整条生命周期未执行通过。新轮首次证明控制器重启窗口确有 socket 尚未出现，且有界就绪探测能越过该窗口；它不能反推上一轮 503 的全部底层原因。Local XI `e7c9fc9` 仅让原安全诊断识别 UR 独立 `error` 包络的精确固定机器码，仍拒绝双包络和任意错误文字，未改两产品公共 API。UR 总体在本地 PG16 正式 handler 复核：旧候选恢复预览 200、确认 503，审计 action 长于 `VARCHAR(50)` 触发 SQLSTATE `22001`，操作未落库；修正候选 `ef52a29f2d463a8c5b136f9f18fdd975c88a0c26` 确认 202，同票据同操作重复确认仍保留审计，跨安装 409 不写入。XI 的 installed-service UR checkout、HEAD 校验与 wiring 断言现精确指向该 UR 候选；新组合尚无 CI 或 installed Linux 回执，不据本地 PG 结果宣称联合链通过。
- **PR112 第三轮 CI 与后续 Local 修正：** [主 CI 37452218312](https://github.com/foigent/x-insight-private-history/actions/runs/37452218312) 的 Backend and API Validation、Frontend Tests、PostgreSQL 16 Admin Credential Rotation、Signed V2 Release Qualification 均成功；`Backend Tests` 汇总门因两条安装资格路径失败而失败。新装与 `adopt_existing` 的各自安全收据均在 `credential_offline_expiry` 首次 setup 状态 GET 返回 503／`connector_control_unavailable`，最终阶段为 `lifecycle_expiry_status`；此前两条路径的轮换、丢响应与服务重启阶段已通过，恢复 POST 尚未执行。源码核查发现联合 driver 在重启 `Type=simple` 控制器后仅确认新 InvocationID 和独立 Web 登录，未证明控制器 socket/状态接口就绪；原 Host 路径已有短时就绪证明。Local 提交 `cfb1909` 在重启边界增加 2 秒内同一只读 setup GET 的合法响应证明，只对精确 503／`connector_control_unavailable` 于该短窗口重试，其他 HTTP/非法响应立即失败；过期应有的 `needs_action` 允许，原恢复与负向验收不改。同批后续 Local 增量还在 setup Unix 客户端的 dial/deadline/write/read/decode/response 及 Owner status 的 read_record/read_policy 边界记录固定 action、phase、category 和白名单 SQLSTATE；原安装资格安全导出只聚合精确允许的固定字段，不导出原错误、路径或身份。Go1.25.12 定向 race 测试、vet、原 CI wiring/保护/发布脚本入口与差异检查通过。截至第三轮，修正及新增诊断尚无 installed Linux CI 回执；该轮安全收据没有底层错误，不能断言该轮 503 的根因一定是 socket 启动竞态。
- **PR112 第二轮 CI 与本地修复边界：** [主 CI 37446168330](https://github.com/foigent/x-insight-private-history/actions/runs/37446168330) 的 Frontend、Signed V2、管理员凭据轮换 job 成功；Backend and API job 的 28 个顶层失败集中在 schema45 当前授权列表遗漏、迁移到当前版仍固定按 schema44 计数，以及 M44 历史恢复测试误跑到 M45。Local 提交 `94bf1eb` 补足显式兼容列表、保留 35–44 历史输入和 schema46 负例、让预算授权与 DB admission 共用一个 predicate，并把原版本/授权短检查接在 Backend 与 installed 两个 CI 入口的 Go setup 后。原失败的 API/DB/Hostctl 定向用例在隔离 PG16 和明确 Go1.25.12 下全部 PASS；四包短检查、CI wiring 和定向 vet PASS。第一次本地 PG 尝试缺记录范围，第二次 DSN 缺夹具要求的显式密码，属于夹具输入错误；纠正后通过，不计产品缺陷。本地 PG16 已正常停止，临时 loopback trust 已恢复为 reject。上述结果不等于新 CI 通过。
- **installed 故障诊断（本地候选）：** 本轮新装安全产物的 `credential_offline_expiry/server_error` 只证明该阶段发生 5xx，旧连接安全产物没有可定位的 stage receipt；尚不能归因为宿主锁、seed 文件、SQL 或服务启动窗口。原 Unix 客户端和宿主 seed 边界现仅以固定 action、phase、category 记录 dial/write/read、锁忙、授权/文件、数据库已知 SQLSTATE 与上下文到期，不记录原错误、路径、票据、地址或 SQL 参数；正式 HTTP 合同不变。真实 Engine seed 文件路径的同请求重入、冲突、到期、锁忙以及 Unix 客户端定向测试在 Go1.25.12 下 PASS。仍待新 installed Linux 回执区分实际故障层，不把日志分类或单元测试冒充根因。

- **PR112 首次 CI 定点修复（待重跑）：** [CI 37442036845](https://github.com/foigent/x-insight-private-history/actions/runs/37442036845) 的前端 job 在 TypeScript、OpenAPI、535 项组件测试及 OpenClaw bridge 通过后，被 `npm audit` 新公告拦下：锁文件 `source-map-js` 1.2.1 命中 GHSA-68fv-2mgg-jv7q。仅锁文件更新至官方修复版 1.2.2；准确 Node 22.23.1/npm 10.9.8 下 `npm ci`、完整 `test:coverage`（36 文件/535 测试）、`check`、OpenAPI、audit（0 漏洞）、license 和 build PASS，已提交资产无差异。管理员口令轮换与全新安装分别在 schema45 的 `VerifyRoleBoundaries` 被拒，原因是正式授予 workspace 的 M45 初始化函数漏入 digest 权限校验白名单；现让授予与校验复用精确三函数列表。Backend and API job 两个新 PG 用例在 `ApplyGrants` 前未独立创建 CI 干净集群所需的受限角色；现三个需角色的生命周期用例统一调用正式角色夹具，清理顺序为先删一次性数据库再删本次创建的角色。本地共享 PG16 已核实存在三个受限角色，这解释此前新用例为何越过该缺口；其 Unix socket fixture 不符合正式夹具对预置角色的显式本地 TCP 边界，本轮专用 PG16.14 Unix-only 干净实例中，两条新用例各自在零预置项目角色前态下单独 PASS，已修改入口的原首兑用例也从零角色前态 PASS；每例结束后角色数均回到 0，原始测试日志保存在 `/private/tmp/xi-pr112-clean-pg16.EC2nJn/`。旧本地 PG16 运行已证明 schema44 来源/45 目标完整权限校验、reader/PUBLIC/grant-option 额外权限拒绝及修复、同安装过期恢复。`adopt_existing` 的安全回执只定位到 `candidate_upgrade/failed/operation_failed`，未导出底层错误码，不能归因为已修权限问题；汇总 Backend Tests 是前置任务失败后的合同门失败。保护摘要经复核更新且两套 gate PASS；修复后 CI 尚无回执，不将本地结果写成 CI 通过。

- **PR112 CI fixture pin：** 第三轮 CI 使用准确 UR 输入 `d798af8323543c6dcca346cb62827cd278e206f5`，第四轮使用 `41e580c08fafc4d3ed193784620566934785c01f`；当前 Local 候选将 installed-service 矩阵的 UR checkout `ref`、实际 HEAD 校验与 CI wiring 断言同步更新至已由总体复核的新 HEAD `ef52a29f2d463a8c5b136f9f18fdd975c88a0c26`。新输入尚无 XI CI 回执；原 FPV14 前驱及安装模式语义未改。

- **目标与前态：** FPV15/schema44 已按下节准确制品和 operation 部署；原连接 control identity/token 已到期，升级成功未使它恢复。冻结合同为同一 installation 的自动轮换和双管理员恢复，源码候选从 `fda71f56cc1ff57a9e966a3552c51bc20703ab6b` 出发；FPV16 已有上述局部成功与失败的 CI 回执，尚无完整 CI 通过、正式签名包、Target 或真实流量结论。
- **当前实现：** schema45 建立唯一 current digest、pending/committed/superseded 操作和一次性恢复票据；XI owner 事务以原范围与前驱 CAS 核验并撤旧，服务 HTTP、原 root Unix 控制器、连接页诊断/恢复入口与 `upgrade --check` 只读状态已接入。初次受限 workspace 兑换只经正式 definer 初始化。合同与管理员说明在本仓库 OpenAPI、手册和合成 wire fixture；不修改首兑历史或业务 control fence。
- **本地证据：** PG16 定向用例通过初兑、轮换/确认、失响应重放、过期恢复、过期 pending 取代、受限角色直接写拒绝和锁等待越期拒绝；同库负例逐一拒绝撤销身份、撤销 current/replacement、current 在 prepare 后撤销、断开安装、poll/ack 缺失、来源绑定/origin/project 漂移、同操作换 secret 与失去管理员恢复授权，旧 Bearer 经真实 HTTP 返回 401。poll/ack 缺失只在一次性测试库移除身份权限约束后注入，生产 schema 仍禁止该状态。实际 M43/44/45 库的只读凭据预检通过，状态包含原 source binding；前端类型检查及定向页面测试、API 合同测试通过。原安装联合夹具的目标网络差异和本地责任测试守卫已修，并通过定向测试；保护运行时检查两项 PASS，签包入口测试 13/13 与验包入口测试 16/16 PASS。正式同版 release dry-run 仍要求冻结源码的干净 Git 身份；正式 Linux 双端跨期、CI 和浏览器结果仍按后续准确运行记录，不以此段替代。
- **边界：** 本批未创建现场 secret、改 Target 凭据/权限、发新流量、签包、部署或推送。当前状态可供总体审查源差异；旧 FPV15 已部署事实与历史摘要保留在下节。
- **联合 driver 只读复核：** 到期安排仅在一次性安装夹具中缩短原 identity/current/prepared 的期限，不切 current、操作、授权或历史；不等同生产时间配置。`lifecycleInvariant` 引用的安装、来源绑定、原操作、身份、grant 和旧 source-fact 字段在 schema45 及其前驱迁移中均存在。XI 正式 `POST /api/v1/service-identities/{id}/revoke` 要求 `service_identities.revoke`、会话和 CSRF；联合 UR HTTP 夹具用合成管理员上下文进入正式 handler，仍执行保存角色的事务复核，不是生产浏览器管理员会话。正向恢复只接受 2xx，负向仅接受 403/409/410，401/404/5xx 不算撤销证明。`control_credential_pending` 由每次只读 setup 状态计算，Tick 遇到时跳过推进，不写入 `setup_error_code`，确认后可继续；这些是源码检查，正式 Linux 联调结果仍待实际运行。
- **installed driver 当前源码结论：** 对四个准确输入 `main.go` SHA-256 `1775a00f9cccf8a3cafb8656a0c0f737f755128e458e681f5a01f0cc89335a6d`、`main_test.go` `ff9f00d571a9e6346f35b5a330a096bc66a966a22c4053da7b9a7394db52865c`、`credential_lifecycle.go` `b7176746fc0c6b9ef104f6bfddc329eae4e36fadb80cdb5c63e8d0133db1b405`、`qualify-ubuntu22-installed-joint.sh` `2579f96e10ba68f7adca8ea9edf3021d25d5207bc9a0c79b1948bbdbc6a07906` 只读复核，未发现阻断当前本地提交的静态问题。到期夹具只改隔离安装的原身份/current 与未送达回执的 prepared 期限；正向逐步核同一 current、真实解密正文与新事实，负向不把 401/404/5xx 算成功，最终核正式撤销后仍拒绝。Go driver 测试、vet、shell 语法和 diff 检查 PASS。此前本地联合 PASS 不替代 installed Linux 的五种跨期/失响应场景；该场景仍需 CI 的实际运行回执。
- **本地 PG 资源：** 经核对无其他客户端后，原 XI Unix-only 测试集群 `/private/tmp/xi-cred-pg16.PV8uB2/data` 已用 `pg_ctl -m smart` 正常停止，数据保留；本轮零角色 PG16.14 集群 `/private/tmp/xi-pr112-clean-pg16.EC2nJn/data` 完成独立测试与清理回读后也已正常停止，数据和日志保留。未清理其他 IPC 项或改宿主限额。
- **联合失败定位：** 2026-10-06 联合重跑显示八类来源 worker 与 C 清单成功，但 XI setup `policy_revision=null/applied=false`，正文尝试 `excluded_policy/effective_policy_disabled`。源码确认两处因果缺口：通用 `newSourceFixture` 的 workspace store 未绑定部署身份，正式 `ListPendingConnectorSetups` 因而筛不到安装；schema45 原新装 v1 首兑事务也未调用 current 指针初始化，`ReadConnectorSetup` 会给出 `control_credential_unavailable` 并让 Tick 跳过。联合夹具现与宿主同一 `xid_AAAAAAAAAAAAAAAAAAAAAA` 绑定；v1 首兑在原事务内调用正式受限 definer 初始化（旧 schema 无此函数时沿原行为）。真实受限 workspace PG16 初兑、原重放、安装选择与正式 `connectorsetup.Manager.Tick` 策略生效均通过；包含 v2 与终态负例的同选择器 `-race` PASS。Tick 的测试宿主只提供已加载证明，真实正文接受与读取仍需两端联合重跑，不记作已通过。XI PG 正向用例已实际覆盖 committed 后 current 到期（并让身份到期）再走管理员票据恢复；UR 保存已知 prepared 回执却跨期的情形需 UR 自己的受控时钟验收，不把短 TTL 改写或伪造回执。
- **原新装本地联合恢复：** UR 本地联合 `TestXInsightJointConsumerIntegration` 在修正后的 XI 树上 42.51 秒 PASS（`/private/tmp/ur-xi-control-lifecycle-joint-pg16-after-xi-fix.log`）。该测试调用独立编译的 XI consumer 和正式 UR HTTP handler；XI 联合断言核验 C `accepted`、解密摘要及带会话的 HTTPS 正文读取。它只证明本地隔离 PG/合成来源链，非 installed Linux 跨期生命周期、Target 或真实客户数据验收。
- **原到期安装迁移回归：** 真实 PG16 从 schema44 正式受限 workspace v1 首兑建立唯一 token、原来源绑定和 ACK；将同一身份/token 置于到期前态后，只读预检报告 `recovery_required`。正式 M45 迁移精确回填该 token 为 current，不改原安装、邀请、来源绑定或 control operation；随后同安装票据签发/解析、恢复 prepare/confirm 成功，新 bearer 可用、旧 bearer 撤销，原快照仍相同。`TestControlCredentialSchema44ExpiredPredecessorPostgres16` 的 `-race` 定向执行 PASS，已纳入原 CI PG16 步骤；这证明本地前态迁移链，尚非 Target 数据库升级。

### 本批五项复盘

1. **结果：** 形成可审源码、schema45 合同与局部真实 PG/UI/协议证据；持续接入尚待双端和已安装运行链。
2. **偏差：** 旧 CI 只覆盖短时有效身份，原页面的 custody_loading 遮住凭据到期；本地联合夹具还遗漏正式网络发布输入。
3. **原因：** 初次安装成功被隐含当作长时间有效；诊断和升级预检只看配置/历史，未把当前凭据代际及期限作为单独观察面。
4. **修正：** 生命周期进入原控制循环与 owner 事务，连接页和原 `upgrade --check` 暴露准确状态；PG16/CI 入口增加受限身份及跨期断言，联合夹具复用正式 target set 路径。
5. **未结：** 双端 installed Linux 需跨期新事实/正文、两处中断、旧 bearer 拒绝、撤销拒绝及重启连续性；同版 CI、签发与经批准 Target 验收各自另取回执。

## 2026-10-06 — FPV15 已部署，持续连接身份到期阻塞（前态）

- 总体仍为本批唯一源码写入者；两项目原执行窗口负责只读核查。源码 main `fda71f56cc1ff57a9e966a3552c51bc20703ab6b` / tree `ff444700a5b5f9d0ceea81331f16abb75f4fa507`，[PR111](https://github.com/foigent/x-insight-private-history/pull/111) 已合并。R13 PR 主/协议及准确 main [CI37361043494](https://github.com/foigent/x-insight-private-history/actions/runs/37361043494)、[协议37361043548](https://github.com/foigent/x-insight-private-history/actions/runs/37361043548) 全部适用检查成功；协议首次 Go 依赖代理 HTTP/2 故障，仅失败作业重跑通过，源码未变。
- **Artifact/Target：** FPV15 bundle `sha256:4988fe090114224bb1c391311452df4c8266c98a19db8a156dad98dbba2c6c28`，37,931,666 bytes。原实例正式 `upgrade --check` 十项通过；前后服务、配置及受验数据库状态相同。升级 operation `20261005T235338Z-43bbf98befb1b984` completed，FPV14→FPV15/schema44；正式私有备份 `sha256:8dca828ccdff99c6d6a0f1773f16214aca3cb2284d74d77bcaa6ae56471c191a` 实际文件摘要匹配。主服务、controller、feed 均运行新制品；controller 加载正式三目录写入边界，CAP0/NNP/ProtectSystem=strict 保持。
- **保持项：** 原账号、正文 key、部署身份与数据库 schema/实例保持；25 条 evidence、41 条 source facts、73 条 observations、52 条 manifests、52 条 receipts 原行逐项摘要相同。来源仍处恢复后暂停，不能把升级就绪当作接入完成。
- **当前确定阻塞：** 原安装的 control identity 状态 active、poll/ack 完整，但其 24 小时期限于 `2026-10-05T16:47:11.680889Z` 到期；关联 token 同期到期且未撤销。`internal/db/connector_managed_read.go` 正确拒绝过期身份，managed.Tick/projects.Refresh 因而尚未进入原 worker 恢复。setup 独立记录的 custody_loading 并非完整根因。未修改期限、重建安装或直接恢复数据库状态。
- **待办：** 原 UR/XI 窗口只读核查控制身份期限来源、原安装的正式恢复/续期路径及到期 UI。未实施新的信任生命周期方案；新授权边界须先由用户决定。旧连接接续、真实 OpenClaw 双模型及浏览器验收未完成；现网仍安全保留原历史，无本轮真实模型调用。

### 本次阶段复盘与落点

1. **结果边界：** 已完成 GitHub/CI、签发、正式升级和历史保持；接入目标仍未完成。
2. **错误假设：** CI 的有效控制身份前态，被隐含当成延续数天的 Target 同样前态；升级前检查只验证主机/制品/原来源历史，没有核对连接身份剩余有效期。总体承担目标前态核对遗漏；过期是正常拒绝，不能放宽认证来掩盖。
3. **已有检查为何没发现：** 原已安装旧连接 CI 资格验证恢复暂停和接管，但不覆盖已连接身份到期；当前页面也缺少该状态的明确恢复提示。源码与 Target 到期证据支持这个缺口，不能归因模型路由或电脑。
4. **已落地修正：** 本轮操作预检的安全 assert 在 Target 执行前改为不可被 Python 优化关闭的显式拒绝，并用 optimize=2 负例验证；摘要统一采用正式 sha256: 格式。具体控制身份时间/权限/撤销元数据已只读核对并进入当前入口。后续身份生命周期修复仍须落到原接口、状态页和原测试，不以本段文字声称修复完成。
5. **下次相关验收：** 先核原安装/身份/权限/期限，再分别验证有效、临近到期、已到期、已撤销、断线跨期、响应丢失及恢复后的历史保持；只实施最终决定所需用例，不另建平台。当前收尾规则与最新跨项目授权以第61份记录为准。

## 2026-10-06 — FPV15 PR 冻结时记录（历史）

- 目标：管理员确认后，现有/新建连接和未来项目自动采集事实及正文；沿正式服务限制完成安装、升级恢复、重启与实际读取，再继续已授权的真实工作链验收。
- 总体负责：本次 UR/XI 协调任务；XI 实施/复核沿现有 `XI执行-客户工作链-20260920`。当前源码候选为 [PR111](https://github.com/foigent/x-insight-private-history/pull/111) 的 R13 测试修正，产品修复为 R11 `df27880f25455d46c60b957997799d375fd99f4e`；前一轮 R10 head `e7ed5a6b8aec3dc70ecb0786794410407cfda83d`；FPV15/schema44，基线 `a10aa9cf517ce187a95f5c7e20be0f8c8fdbe6c6`。
- 当前 CI：R11 [37353091290](https://github.com/foigent/x-insight-private-history/actions/runs/37353091290) 的新装及准确 FPV14 备份/恢复/升级/旧连接接续 Linux 链均通过；三组真实 PG 接管用例亦通过。R12 [37357500464](https://github.com/foigent/x-insight-private-history/actions/runs/37357500464) 前置回归在4分39秒暴露新增 HTTP 测试的另一处错误：邀请与 UR 正文实际读取已通过，但测试把能力检查次数误定为2，正式路径为4。R13 按受验行为要求能力检查为正、正文读取为1，切换后及本地副本缺失时仍严格要求两类UR请求均为0；不改产品或权限。此时钟/调用次数夹具遗漏由作者及复核承担，静态通过不等于PG验证。最终实际CI仍待。
- Target 边界：最近现场证据为 2026-10-05 的 FPV14/schema44；正式 controller 发布 systemd 配置受只读目录限制。FPV15 尚未合并、正式签发或部署；本次文档更新没有重新探测或修改 Target。
- 下一动作：R13 测试修正通过本地编译和复核后推送，收取同一候选全部适用 CI 结果；通过后按已授权 GitHub 流程交付。新正式签名身份与精确升级方案需单独核对授权，不能从 CI 成功推导为已部署。

### 本批复盘及已落实的改进

1. **目标与结果：** 运行链仍在验收；本地修复及协议通过分别记录，不以局部通过结算接入目标。UR 固定联合夹具已合并 [PR430](https://github.com/foigent/unirouter/pull/430)，main `7ab84f1d` 的 [CI37338248954](https://github.com/foigent/unirouter/actions/runs/37338248954) 已通过；UR 产品部署未因此改变。
2. **偏差与原因：** 保护摘要再次遗漏属于交付漏检；workspace 测试曾使用管理员连接属于夹具失真；来源项目缺少正文配额绑定属于产品自动步骤缺失，测试手工补绑定又掩盖了它。记录教训后没有把检查接入原交付入口；本文件及 XI-CURRENT 顶部也未持续更新，使旧任务状态继续成为恢复入口。总体负责交付闭合，不能由用户反复提醒代替。
3. **原入口修正：** `scripts/build-signed-release.sh` 与 Backend CI 共同调用 `scripts/verify-protected-runtime.sh`，执行已有两套保护检查，绝不自动接受新摘要；`internal/testpostgres/guard.go` 在 CI 必需 PG 模式缺 DSN 时拒绝启动；正文 PG/联合夹具用正式角色准备与 `current_user` 断言；`internal/db/content_custody_transition.go` 的筛选和锁后完成复用同一条件；managed project 在产品路径幂等建立或保留准确项目映射，移除测试手工补偿。
4. **验证与未验证：** 本地先证实旧入口漏拦，再验证修复后拒绝，两套真实旧摘要反例也拒绝且不刷新。对应原检查是 `scripts/test-build-signed-release.py` 的保护入口/真实摘要用例、`internal/testpostgres/guard_test.go` 的必需 PG 缺配置用例，以及 `scripts/test-ci-signed-release-wiring.py`。R10 的 `internal/db/review_body_ur_copy_postgres_test.go` 与 `content_custody_transition_postgres_test.go` 真实执行通过；但后者构造 `bounded` 页，未覆盖实际 UR 的正常未知覆盖语义。R11 保留原场景并加入真实 unknown 页、历史暂时不可用、分页及已知 gap 阻断；`internal/api/content_custody_transition_postgres_test.go` 经实际 HTTPS 验证同一历史引用切换前读 UR、切换后读 XI、副本缺失拒绝且不回退。R11 中三个 DB 场景已实际通过；新增 HTTP 用例的准备阶段失败，尚未验证其回源断言。R12 已修复时间错误并前置实际回归，R13 再纠正能力调用次数与受验目标无关的过窄断言，仍由必需 CI 实跑；不把源码复核当成执行通过。
5. **R11 语义收缩：** 筛选与锁后完成仍共用一份 SQL，只接受明确 bounded/无 gap 或准确 unknown/capture_health_unknown；保留来源未知状态，不把接通改写为完整覆盖。所有未过期且有 representation 的历史条目都须有准确本地副本，临时不可用不能被跳过；仅旧迁移队列允许正式 retryable/source_unavailable 重试，其余权限、完整性和已知缺口保护不变。
6. **完成与下一次执行：** 长任务结束时沿 [现有协议的复盘完成步骤](AGENT-PROTOCOL.md#long-task-completion-and-retrospective) 更新此摘要，给出实际结果、偏差、原因、必要修正及剩余项。下一次相关修改先读上述入口；同类再发先查已落地入口为何漏拦。独立优化另列，不新增记录系统或凭空保证不再出错。本批还需 CI、正式交付、Target 接入及真实工作链结果。

以下为保留原日期与验证边界的历史记录。

## Local Implementation Complete — Phase 4 operator access plane

Work ID: `XI-PHASE4-OPERATOR-ACCESS-20260729`

Owner: Codex task `/root/xinsight_access_owner` (sole X-Insight repository
writer for this batch)

Branch/base: `codex/phase4-operator-access` from clean remote main
`e6c1bd3e3d6e6433b5e5717dedd50488dcf521ee`
(tree `1da84e19b81d52697acedc01f60446782e058023`).

Status: Local implementation, owner verification, sole-writer handback, and
root-coordinator independent review complete. At this evidence freeze the
working tree remains uncommitted and is ready for the authorized root local
commit. No push, PR, merge, deployment, credential creation, target runtime,
database, or traffic action was performed by this batch.

Authority:

- coordinator decision
  `2026-07-29-xinsight-operator-access-plane-decision.md`, SHA-256
  `e767ae5262a10e4e1f8e03e1092501c894031add7ceb7f5e2f45e2d34a147a31`;
- approved account authority is one root-provisioned, read-only systemd
  credential containing strict versioned JSON and Argon2id password hashes;
- product-owned opaque in-memory sessions with investigator/operator RBAC; and
- future OIDC and reverse-proxy transport are seams only, not implementations
  or authority in this batch.

Risk: R3 authentication, authorization, session, CSRF, security-header,
credential-boundary, and minimized-audit enforcement.

Implementation boundary:

1. strict local-account configuration with stable opaque subject IDs, unique
   normalized usernames, investigator/operator roles, disabled state, and
   Argon2id hashes only;
2. bounded 256-bit opaque sessions stored by one-way digest, 30-minute idle and
   eight-hour absolute expiry, login rotation, logout, operator list/revoke,
   and process-restart invalidation;
3. UI/API authentication, investigator/operator RBAC, session-bound
   synchronizer CSRF, same-origin checks, generic login failure responses,
   failure-rate limiting, security headers, and metadata-minimized audit;
4. frontend login/session/logout/operator-session controls, versioned OpenAPI,
   systemd credential wiring, host/release verification, operator manual,
   version identity, and tests; and
5. unchanged reader-only Web database authority, source filters, schema 14,
   importer/feed/credential boundaries, and loopback listener.

Stop conditions:

- any requirement for a Web database writer, migration, external IdP or
  customer registration, egress, proxy authentication, persistent session or
  account storage, Web credential management, public ingress, real credential,
  deployment, target database/runtime, or traffic;
- inability to fail closed on invalid account authority, prevent cross-origin
  state changes, minimize audit metadata, or preserve reader-only/source
  boundaries; or
- a P0/P1 finding after focused remediation.

Rollback: revert this isolated access-plane change before deployment. Schema 14
and evidence data are unchanged, so no database or data rollback is part of
this batch. Any separately provisioned future account credential must be
handled by an explicitly authorized operator procedure, not by repository
cleanup.

Verification plan:

- focused access config/hash/session/RBAC/CSRF/origin/rate-limit/audit/API and
  frontend tests, including local TLS cookie proof;
- full and race Go regression, vet/static/module/format/diff checks, frontend
  test/check/build, release bundle/verifier/manual checks, and CI
  router/contract self-tests;
- real Local PostgreSQL reader-only regression if an already available safe
  instance can be identified without creating or mutating an unauthorized
  database; and
- explicit reviewer checklist covering authentication bypass, cookie/session
  lifecycle, CSRF/origin, RBAC, audit leakage, credential loading,
  reader-only preservation, and release/manual identity. Owner-run checks are
  self-verification, not independent acceptance.

Evidence:

- Local evidence:
  `docs/evidence/prd2/phase4-operator-access-plane-local-evidence-2026-07-29.md`.
- Evidence SHA-256:
  `e3be814ee2e6744ef4ede422fd2657a3eccddf9a00c589476bb524cd2af06769`.
- Exact Go `1.25.11` full test, access/API/Web UI race, rc.13 host-control
  race, vet, module tidy-diff, release-focused tests, and diff checks: PASS.
- Exact Node `22.23.1` with npm `10.9.8`: typecheck, five consecutive complete
  13-of-13 frontend test suites after the root test-only wait correction,
  production build/dist validation, 11-path OpenAPI validation, and 177-entry
  license validation: PASS.
- Mock-only Chromium desktop/narrow E2E: 4 of 4 PASS. This is not HTTPS,
  Target, or real-login evidence.
- Isolated loopback PostgreSQL `16.14`: integration, representative reader
  grant drift, reader write/DDL rejection, backup/restore, UniRouter exact
  source filter, and LiteLLM checkpoint/scope binding: PASS. The temporary
  server is stopped.
- CI routing self-test: 11 cases PASS. Contract gate: 8 cases PASS. Workflow
  YAML, changed shell syntax, release/access JSON, generated manual, and release
  identity checks: PASS.
- Root independent review found no remaining P0/P1 and re-passed focused Go,
  frontend, mock-only browser, diff/static/secret/deletion/migration checks.
- Signed build/verifier and live CI remain for the root coordinator's later
  clean post-commit tree. Target HTTPS, real credential/login, deployment,
  External integration, and Production remain unverified and were not
  attempted.

Handoff: `/root/xinsight_access_owner` performed no further repository writes
after final read-only status, diff, and secret checks. Sole-writer authority
returned to `/root`.

## Prior Work — Receiver-generated completed-feed credential

Work ID: `FV-REAL-FEED-V1-RECEIVER-CREDENTIAL-XINSIGHT-20260726`

Owner: Codex task `019f9374-6248-7a21-9c5f-ae30d251d3ff`

Branch/base: corrective branch `codex/feed-credential-auth-preflight` from
merged main `7cd7814a8e0de1af0f16f9854d18ba55b08a65a7`
(tree `7c327e2355b1496e1458477302f7ade17dacc0f8`). The original implementation
was squash-merged as PR #71 immediately before the material pre-merge review
finding arrived; no release or deployment followed that merge.

Status: authentication-barrier correction active; release and deployment are
frozen until the corrected exact tree passes PR and main qualification. The
unused RSA-OAEP handoff
receiver was retired before implementation: its exact target private slot was
destroyed, the three exact public/local handoff artifacts were removed, no
ciphertext existed, and Development remained feed-worker inactive/disabled
with the live key absent. Local code/test/release work may proceed; no
Development `feed-credential prepare`, worker activation or feed request is
allowed until the later sequenced baton.

Authority:

- authoritative shared ledger SHA-256
  `f8ef2ca331990b0aa1933d0ea61a4b92f5fd717af43110e9c7b1547e04fdf48b`;
- accepted bilateral decision
  `2026-07-26-receiver-generated-feed-credential-accepted.md`, final SHA-256
  `c07be727e036b135c78a30a5c0571678f6d6330fc9e1cc6ad23da9a4955c0138`;
- Frozen Registration Bundle V1 schema
  `unirouter.xinsight.feed-credential-registration.v1`; and
- exact verifier domain `unirouter.xinsight.feed-credential.v1`, followed by
  one NUL byte and the exact raw token.

Implementation boundary:

1. root-only `xinsightctl feed-credential prepare|status|activate|abort`;
2. locally generated `ur-xf1-` plus 64 lowercase hexadecimal characters from
   32 `crypto/rand` bytes, with an independent 16-byte operation ID;
3. strict six-field public registration bundle and no raw-token output;
4. exclusive, no-follow, root-owned `0600`, regular, single-link `.next`
   staging plus durable nonsecret operation state;
5. exact operation/verifier binding and resumable prepare/activate/abort crash
   handling;
6. before replacement, staged-token authentication through the existing exact
   worker runtime config and HTTP source, requiring only strict authenticated
   paused retryable `503`; all non-proof results preserve live/next/state; and
7. atomic replacement followed by `systemctl restart xinsight-feed.service`
   only after that authentication barrier. HTTP source transport rules,
   endpoint, DTO, cursor and database protocols remain unchanged.

Prohibited and unconsumed:

- no plaintext/RSA envelope, browser/clipboard token movement, mTLS, cloud
  secret manager or callback/control API;
- no Development credential preparation, live key creation, worker start,
  polling, database reset, broad backfill, customer/Production data or
  request/provider traffic during implementation and dormant tooling
  deployment; and
- no claim of live integration/UI closure before the later staged
  registration, paused-503 proof, promotion/resume and bounded API/UI readback.

Evidence:

- [receiver-generated credential Local evidence](../evidence/prd2/receiver-generated-feed-credential-v1-local-2026-07-26.md).

## Prior Work — Real completed-execution pull V1

Work ID: `FV-REAL-FEED-V1-XINSIGHT-IMPLEMENTATION-20260725`

Owner: Codex task `019f9374-6248-7a21-9c5f-ae30d251d3ff`

Branch/base: `codex/completed-execution-feed-v1` from
`3adc06698df99016709acce8f1b8a897b69cf36e`

Status: accepted implementation scope active. Strict receiver, source client,
durable worker state, at-least-once committer, lifecycle wiring, release
identity and complete Local verification are implemented. PR/CI/merge remain
pending. Deployment, credentials, live activation and bounded readback must
wait for the joint deployment baton.

Authority:

- accepted reopen SHA-256
  `fb05cb118e7ea737896cfbe18f0cc1ff41d3201566af27f2f3485373385e23c1`;
- accepted UniRouter commit
  `0fe054cb7b5e2b1b8081b0eb8dd454a669772804`; and
- accepted inner schema SHA-256
  `4ae2897d4f8470c38624dfe21c54d72eaf6d995d2c9578c4420c94d244424fb2`.

Implementation boundary:

1. one fixed-endpoint, HTTPS-by-default pull source with feed-only bearer
   credential, no environment proxy/redirect and bounded transport;
2. one strict immutable revision-1 byte receiver mapped into the existing
   source-neutral evidence model;
3. durable source instance, activation cursor/high-watermark, current cursor/
   high-watermark, worker state and safe counters in `adapter_import_states`;
4. existing idempotent evidence persistence per item followed by a separate
   cursor-after-page transaction;
5. one writer-capable `xinsight-feed.service`, while `xinsight-server` remains
   reader-only and loopback-only; and
6. release/migration/manual/lifecycle changes for schema 13 and
   `0.2.0-rc.7/manual-r7`.

Correctness decisions:

- at-least-once, not exactly-once; partial-page persistence is acceptable;
- delayed outbox materialization after the opaque activation cursor is valid
  even if terminal/source timestamps precede activation;
- a nonempty page must advance the cursor and a post-activation empty page
  must not;
- per-item provenance leaves `source_watermark` and `import_watermark` empty
  because page high-watermark and next cursor are owned only by the durable
  worker state after `CommitPage`; and
- Local persistence completion is not described as measured investigation API
  readback.

Prohibited and unconsumed:

- no retired artifact/import route, public push collector, direct UniRouter
  database access, CDC, customer/Production data, late revisions, pre-billing
  rejection, broad backfill or HA/leader framework;
- no real database selection/creation/mutation, endpoint connection, service
  key binding, worker process start, deployment or external request during
  Local implementation; and
- no Target, External integration or Production claim.

Evidence:

- [runtime contract](../reference/completed-execution-pull-v1.md);
- [Local implementation evidence](../evidence/prd2/completed-execution-feed-v1-local-implementation-2026-07-25.md).

## Prior Work — PRD2 unique call-log public execution outcome

Work ID: `XI-PRD2-FV-X-UNIQUE-CALL-LOG-OUTCOME-20260721`

Owner: Codex

Publication branch/base: `codex/prd2-unique-call-log-outcome` from clean main
`92108a36bb5d5406b08bef0d3dcca248b650d353`

Risk: R2 query-time public outcome semantics. No schema, migration, persisted
field, enum or API shape changes. Previously stored recovered UniRouter
executions may be reclassified by queries after deployment without rewriting
their evidence rows.

Status: Implementation and Local non-database verification complete;
`scope_acceptance=accept_exact` under SHA-bound User
authorization `UR-XI-FRESH-VALUE-AUTH-20260721-02` at
`PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-21-prd2-fresh-controlled-existing-ecs-and-final-outcome-authorization.md`,
verified SHA-256
`d0464491727493939ad788698171f16474c6298db851502e04db77758315043f`.
The parent decision and input owner response were also verified at
`6d8e32f33c59bb10799c629b0d3c929b6ff4980f520c074252d48f70c3628eb3`
and
`263f8e7b7a13b635d8b0520bd7b3fa6372611f499860801012a52e142d9cb2dd`.
Cross-project Binding remains inactive.

Scope:

1. For an all-UniRouter execution with exactly one normalized
   `source_event_type=call_log`, use that event's normalized outcome as the
   public summary/detail/filter outcome.
2. Retain current `failure > partial > success > unknown` precedence when
   call-log count is zero or greater than one and for non-UniRouter executions.
3. Preserve failed attempts, retry, fallback, gaps and every event-level
   outcome in timeline and selected-event detail.
4. Prove identical replay remains duplicate/no-op and that the existing UI can
   truthfully show a success execution badge with a failed-attempt timeline.
5. Update only the directly affected engineering contract, PRD2 plan and this
   execution record, then publish one branch/commit/PR/required-CI/merge chain
   and the exact sanitized owner response.

Implementation boundary:

- one query-rollup change in `internal/db/investigation_store.go`;
- focused PostgreSQL store/API and existing frontend regression changes;
- no importer, receiver, schema, migration, artifact, manifest, data-policy,
  transport, retention or UniRouter change; and
- no `is_stream`, route/retry/fallback-index, recovery-state, badge or dashboard
  expansion.

Compatibility note: outcome is calculated at query time. Existing recovered
executions with one successful call log may move from `failure` to `success`
filters after upgrade. Fixed `as_of` bounds eligible data but does not freeze
projection code, so an in-flight cursor traversal should restart across this
semantic deployment. No stored evidence is mutated.

Stop and rollback:

- Stop if the rule requires any schema/API/artifact/data-policy/UI expansion,
  cannot retain failed evidence, changes behavior beyond the authorized
  query-time reclassification, or leaves a P0/P1 after focused remediation.
- Stop before all UniRouter/environment/source/credential, real artifact/import,
  controlled-traffic, N=1, Target, Production, live-feed, Core-Intake or
  second-source actions.
- Rollback is a normal X-Insight revert PR restoring the prior query rollup; no
  data rollback or cleanup is required because this batch does not rewrite
  evidence. The retained Local synthetic database remains outside this batch's
  deletion authority.

Verification plan:

- focused DB proof for unique success, unique failure, zero/multiple call logs,
  non-UniRouter fallback, summary/detail/filter consistency, retained timeline
  and duplicate/no-op replay;
- HTTP API proof for the same recovered success summary/detail/filters and
  retained failed/retry/fallback evidence;
- frontend proof that final success and failed-attempt evidence coexist;
- full/race Go, frontend test/check/build, vet, module/diff/format and CI router/
  contract gates; and
- one independent read-only R2 diff review. Owner-run checks are
  self-verification, not independent acceptance.

Verification at publication-candidate authoring:

- `go vet ./...`, full `go test -count=1 ./...` and full
  `go test -race -count=1 ./...`: pass;
- frontend Vitest: 1 file / 7 tests pass; TypeScript check and production
  build: pass;
- CI change-router and contract-gate self-tests: 9/9 and 8/8 pass;
- `go mod tidy -diff`, fixture JSON syntax, Go formatting and
  `git diff --check`: pass;
- focused DB/API packages compile, but their PostgreSQL cases are explicitly
  skipped locally because this sandbox cannot connect to the running Local
  PostgreSQL socket and has no Docker binary; required CI's PostgreSQL 16
  service is therefore the actual execution gate for those focused cases; and
- independent read-only diff review returned `P0=0`/`P1=0`/`P2=1`. Its only
  P2 identified the fixture's immutable pre-Decision-B `expected_observation`;
  the planning follow-up now labels that text as a superseded historical
  baseline without rewriting fixture bytes. Owner checks remain
  self-verification, not independent acceptance.

Required GitHub PR/CI/merge and the immutable shared response remain the
publication gates; this mutable record does not pre-claim those identities.

## Closed Work — PRD2 fresh-controlled value proof fixture check

Work ID: `XI-PRD2-FV-X-FIXTURE-CHECK-20260721`

Owner: Codex

Branch/base: `codex/prd2-fresh-value-fixture` from clean main
`652140154786363ab5f8bd589e2609928d6d9deb`

Risk: R2 execution-outcome and investigation-read-model semantics plus R1
synthetic fixture/planning evidence. This work may change only the existing
execution-outcome aggregation if a fixture proves that a recovered fallback
success is misreported. It may not add a schema, field, migration, API/UI
surface, policy, retention rule or transport behavior.

Status: Fixture verification complete; `DECISION_REQUIRED` under SHA-bound
User authorization
`UR-XI-FRESH-VALUE-AUTH-20260721-01` at
`PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-21-prd2-fresh-controlled-product-value-proof-authorization.md`,
verified SHA-256
`6d8e32f33c59bb10799c629b0d3c929b6ff4980f520c074252d48f70c3628eb3`.
Cross-project Binding remains pending. No controlled traffic, source read,
artifact, transfer or import is active.

Scope:

1. Run a synthetic failed-attempt then recovered-success fixture through the
   current normalized persistence/search boundary and record the exact final
   execution outcome.
2. Verify whether stream and route/retry/fallback facts are present in the
   normalized API/UI allowlist or only in restricted raw-source projection.
3. If and only if the recovered-success fixture violates the authorized final
   outcome semantics, implement the smallest aggregation correction while
   retaining failed-attempt timeline evidence.
4. Record isolated Local trial-database readiness without generating, reading,
   transferring or importing a real manifest/artifact.
5. Link the User decision and owner response from the authoritative PRD2
   planning chain, publish through one X-Insight branch/PR/required-CI/merge
   flow and author the exact sanitized shared response.

Initial static finding:

- the current search projection uses `failure > partial > success > unknown`
  precedence across every event in an execution, so an attempt-level failure
  can override a later successful call-log event;
- the existing engineering contract freezes that precedence generically, while
  the new User authorization names recovered fallback as a final-success case
  and conditionally authorizes only a narrow aggregation correction; and
- `is_stream` and trace metadata are retained only inside the restricted
  `raw_source_excerpt`, while first-class route/retry/fallback event and
  decision fields remain available when the producer emits them as normalized
  source fields.

Verified fixture result:

- `fixtures/unirouter_snapshot/fresh_value_recovered_fallback.json` is a
  synthetic-only, metadata-only fixture with one successful streaming call-log
  event and four trace events: route selection, failed provider attempt, retry
  scheduling and fallback selection. It contains no prompt, response, API key,
  authentication header, real identity, raw IP or raw upstream-error body.
- importer verification accepted all five normalized events and classified an
  identical rerun as five duplicate no-ops;
- PostgreSQL persistence/search/timeline verification retained both the failed
  attempt and later successful call-log event, while the current summary
  returned `failure` exactly as the existing worst-event contract specifies;
- the HTTP API returned the same contracted `failure` summary and five timeline
  nodes, exposed structured routing/provider/retry/failover decisions, and did
  not expose `raw_source_excerpt`, `is_stream`, route/retry/fallback metadata or
  a generic `metadata` object; and
- the existing frontend chronology/causality fixture now also proves normalized
  retry/failover event and decision visibility while keeping `is_stream` and
  route metadata absent. No stream-specific normalized/UI fact is available,
  so stream visibility remains a formal gap.

No outcome implementation changed. The current behavior is not a violation of
the accepted P2-0 contract; changing it would alter the public meaning of
`outcome`. The pending User choice and owner recommendation are recorded in the
PRD2 field-validation plan. Current classification is `P0=0`, `P1=1` for that
product-contract choice and `P2=1` for stream visibility. Controlled traffic
remains blocked.

Fixture harness corrections during self-verification:

- the first fixture timestamps were later than the Local database clock, so the
  synthetic timestamps were moved earlier without changing event order or
  semantics; and
- the first API fixture query supplied a future `to` value, which correctly
  failed the fixed-`as_of` input boundary; the test now uses the current UTC
  upper bound. These were test-fixture errors, not product failures.

Local database state: dedicated database `xinsight_fv_fixture_20260721` on the
existing Local PostgreSQL 18 service, synthetic rows only, retained pending
disposition, review due `2026-07-28`. It is not Target, Production, a real
artifact import or the future PostgreSQL 16 isolated field-trial database. No
deletion is authorized.

Focused self-verification completed:

- JSON syntax validation: pass;
- adapter fixture/import/rerun/visibility test: pass;
- dedicated PostgreSQL store fixture/outcome/timeline test: pass after the
  fixture-clock correction;
- HTTP API fixture/outcome/detail-allowlist test: pass after the query-bound
  correction; and
- frontend chronology/causality plus route/retry/failover normalized-visibility
  test: pass.

Broad Local self-verification completed:

- focused importer/DB/API fixture tests repeated twice: pass;
- `go vet ./...`: pass;
- full `go test -count=1 ./...`: pass;
- full `go test -race -count=1 ./...`: pass;
- frontend TypeScript check, six-test Vitest suite and production build: pass;
- CI change-router and contract-gate self-tests: 9/9 and 8/8 pass;
- `go mod tidy -diff`, JSON syntax, Go formatting and `git diff --check`:
  pass.

Independent read-only final-diff review: implementation/document diff
`P0=0`/`P1=0`/`P2=0`. It separately confirmed the pending product-decision
`P1=1` and existing stream-visibility gap `P2=1`; its focused adapter and
frontend checks passed. The reviewer did not have the Local PostgreSQL DSN and
therefore did not independently rerun DB/API tests. Owner-run checks remain
self-verification, not independent acceptance. PR #66 tested head
`e094157274a14204c3048cf092d6509719ed4614` in successful run
`29802458090`; the identical tree merged as main
`92108a36bb5d5406b08bef0d3dcca248b650d353`, and exact-main run
`29802611367` succeeded.

Stop and rollback:

- Stop with `DECISION_REQUIRED` if the fixture cannot establish one correct
  outcome without a new product choice, or if a fix needs any expanded schema,
  field, API/UI, data-policy, retention or transport surface.
- Stop before every UniRouter/source/cloud/credential/private-data/MaaS/
  artifact/import/Target/deployment/Production/live-feed/Core-Intake/second-
  source action.
- Any code rollback is a normal X-Insight revert PR. Synthetic Local test rows
  are not source, artifact, Target or Production evidence and no deletion is
  authorized by this work item.

Verification plan:

- focused adapter mapping assertions for stream and normalized route/retry/
  fallback visibility;
- focused PostgreSQL investigation-store fixture for failed attempt followed by
  recovered call-log success, including retained failed event detail;
- applicable adapter/DB/API/frontend tests plus broad `go vet`, full/race Go
  tests, frontend tests, CI router/contract self-tests and diff/module checks;
- required GitHub PR CI, merge and clean aligned-main verification; and
- optional independent read-only review. Owner-run tests remain
  self-verification, not independent acceptance.

## Closed Work — PRD2 internal-real raw-IP receiver compatibility

Work ID: `XI-PRD2-IR-IP-COMPATIBILITY-20260720`

Owner: Codex

Branch/base: `codex/prd2-ip-compatibility` from clean main
`cf2c5d4a2a6e7641e3f47df1938783f10da54fee`

Risk: R3 data-admission/redaction enforcement. This batch changes only the
authorized X-Insight receiver/preflight tests and planning/status records. It
does not access a source, artifact, cloud host, database, credential or private
value and performs no Target, deployment, Production, live-feed, B2a, B2b or
B3 action.

Status: Complete. SHA-bound User authorization
`UR-XI-IR-AUTH-20260720-02`, request
`7517496d8a40aa824f2c96391c8363e6eaf8533a11652cecbfaa57e92d467d22`
and UniRouter implementation response
`e4524057a0fd5591054552d5d55a0c782b35ee9131c0968fb013f9335eac8205`
were verified before implementation. UniRouter PR #225 merged producer commit
`c3930e3086299bc0ea3c0727b81262de85c501b1` as main
`d43fef6eed92323da0880f0f6e0e8d35e388e8e0`; required CI attempt 2 passed.

Scope:

1. Require the exact dual-boolean `safety.ip_handling` declaration for the new
   strict internal-real path while retaining the historical single-boolean
   declaration only for `false/false` artifacts.
2. Accept only `false/false` and `true/true`; reject `true/false` and
   `false/true` before normalized-event construction or persistence.
3. Prove raw `ip_address` and representative aliases remain unknown-field
   rejected without echoing a raw-IP sentinel.
4. Prove normalized evidence persists only `ip_address_was_present` and
   `ip_address_redacted`; add no schema, migration or artifact field.
5. Reconcile the narrow non-Production contract and nonclaims in the exact
   authorized planning/status chain, then publish through one branch/PR/CI/
   merge chain and one immutable shared compatibility response.

Initial static finding:

- strict preflight currently accepts only the historical declaration and
  rejects `true/false`, but does not reject `false/true`;
- the manifest-bound internal-real CLI requires successful preflight and binds
  the exact artifact hash before any persistence, so both mismatch states can
  be stopped in the authorized path without changing the historical general
  importer compatibility surface; and
- JSON decoding already uses `DisallowUnknownFields`, and normalized call-log
  evidence already contains only the two approved IP-related boolean fields.

Stop and rollback:

- Stop before any schema/new-field change, UniRouter write, source/cloud/data/
  credential access, artifact/import/Target operation, policy expansion or B3.
- Code rollback is a normal X-Insight revert PR. No runtime/data rollback or
  cleanup exists because tests are fixture-only and this work writes no data.
- B2b remains dependency-blocked until this batch is merged, its immutable
  response is externally hashed and `P0=0`/`P1=0`.

Verification plan:

- focused adapter tests, including exact declaration/history matrix, both
  mismatch states, unknown raw fields/aliases, no-echo and persisted-booleans;
- `go test ./internal/adapters/unirouter -count=1`;
- `go vet ./...`, `go test ./...`, `go test -race ./...` and
  `git diff --check`;
- current CI router/contract self-tests and required GitHub PR CI; and
- one independent read-only R3 review. Owner-run tests remain self-verification,
  not independent acceptance.

Implementation result:

- `sanitized_preflight.go` now freezes the strict and historical declarations,
  accepts strict `false/false` and `true/true`, rejects both mismatch states,
  and restricts the historical declaration to all-call-log `false/false`.
- focused tests cover the complete declaration/state matrix, raw
  `ip_address` plus representative aliases, a no-echo sentinel and normalized
  persistence of only the two approved booleans.
- required static review found no authorized-path defect in
  `import_snapshot.go`; it remains unchanged at SHA-256
  `e734bd8c93bfee5bbd7d7d9ae49ff14f5e4e09c0db2c862040628a24c5df0f5e`.
  The strict CLI already requires successful manifest preflight and binds the
  exact artifact hash before persistence. Tightening the general manifest-free
  importer would instead break the accepted historical compatibility surface.

Local self-verification on 2026-07-20:

- the pre-change focused matrix failed on the expected receiver gaps: the new
  dual declaration was unapproved, `false/true` lacked a presence finding and
  historical positive proof lacked a dual-declaration finding;
- the post-change focused matrix and
  `go test ./internal/adapters/unirouter -count=1` passed;
- `go vet ./...`, `go test ./...` and `go test -race ./...` passed;
- `python3 -B scripts/ci-change-router.py --self-test` passed 9 cases and
  `python3 -B scripts/ci-contract-gate.py --self-test` passed 8 cases;
- `go mod tidy -diff` and `git diff --check` returned no differences/errors;
  and
- all inputs were repository fixtures or synthetic temporary files. No source,
  private value, manifest/artifact generation, import or environment action
  occurred.

Independent read-only subreview:

- an independent reviewer examined the complete eight-path working diff from
  base `cf2c5d4...`, bound before this result paragraph was recorded as SHA-256
  `a27f4d1192cfce077a4da3aaed10c92aa1a39c8b0303d6e7353c209d00ead795`;
- result: `P0=0`, `P1=0`, `P2=0`, eligible to enter PR;
- the reviewer independently reran the focused matrix, adapter/full/race tests,
  vet, formatting/module/diff checks and both CI self-tests, and confirmed the
  five planning/status documents preserve every source/artifact/Target/
  Production/B2/B3 nonclaim; and
- this is a read-only code/document review. It is not execution-owner self-
  verification, product acceptance, source/artifact evidence, Target evidence
  or independent acceptance.

CI/merge state: PR #65 tested head
`9dacc47aa60682ca34691e165db256cb9613f1ab` in successful run
`29716667273` and merged the identical tree as main
`652140154786363ab5f8bd589e2609928d6d9deb`; exact-main run `29716852626`
also succeeded. The immutable shared compatibility response records the final
identities and `P0=0`/`P1=0`/`P2=0`.

Parent-decision nonclaims preserved for final response: this adaptation does
not prove a source IP was deleted or modified, and the two booleans are not
general DLP proof. It also does not prove source/artifact/Target/Production/B3
acceptance or that a database principal lacks base-column read capability.

## Closed Work — PRD2 internal-real preflight and UniRouter cooperation boundary

Work ID: `XI-PRD2-INTERNAL-REAL-PREFLIGHT-20260719`

Owner: Codex

Branch/base: `codex/prd2-internal-real-preflight` from clean main
`220796d77ad4c3784071897e85b63f09bad39c92`

Risk: R3 data-admission/redaction guard plus R2 CLI/manual/planning semantics.
No real-data read/import, Target write, deployment, credential or shared
artifact operation is active.

Status: X-Insight receiver preparation merged and CI verified; final
independent read-only code review reports P0=0/P1=0/P2=0. The bounded shared
UniRouter inventory request is published as `Open` / `Not binding`; no
internal-real source export, artifact, trial database, import or Target write
has occurred

Authorization:

- after the exact synthetic UR-E proof closed, the User asked whether project-
  owned data could be tested;
- Codex proposed one isolated batch of at most 20 UniRouter development/test
  executions with no customer data or raw content; and
- the User answered `可以继续` on 2026-07-19. This authorizes X-Insight-owned
  receiver preparation. A minimum bounded UniRouter read-only inventory
  requires the shared Open request and owner-authored response; export still
  requires a later exact binding decision.

Scope:

1. Distinguish internal-real non-customer development metadata from synthetic
   and customer sanitized metadata.
2. Add a non-persisting snapshot-v2 preflight with immutable
   20-request/200-source-row ceilings, exact manifest-byte/hash/request/scenario
   binding, source/org reconciliation and value-aware findings that do not
   echo metadata keys or untrusted artifact field values in the JSON report.
   Filesystem stderr may include the operator-supplied private input path.
3. Freeze the dedicated encrypted local PostgreSQL 16 trial database,
   retention, first-write/replay/API/UI and stop conditions.
4. Publish X-Insight code/docs through normal PR/CI.
5. Create one fact-only UniRouter inventory request. The request scopes only
   the minimum bounded read-only inventory needed to answer and requires
   UniRouter to confirm its project-local owner authority; X-Insight stops
   before source access, and no export/artifact/Target write is permitted until
   the response and exact binding activation decision exist.

Current facts and risks:

- UniRouter producer code can select explicit manifest request IDs and excludes
  raw prompt/response/API-key plaintext/raw IP/raw upstream error text.
- It has no 20-ID hard cap, no org binding in the manifest and no repeatable-
  read transaction across call-log/trace queries.
- It exports several reference/free-text-capable values unchanged.
- X-Insight's prior key-name guard is not semantic DLP and normalized trace
  metadata is persisted in `raw_source_excerpt`, although the read API/UI do
  not return it.
- The current named Target is documented unencrypted/delete-with-instance and
  remains fixture/synthetic-only. Read-only local checks confirmed FileVault
  `On` and Homebrew PostgreSQL 16.14 installed; no trial database exists yet.

Current Local self-verification:

- focused adapter/importer tests: pass, including exact manifest binding,
  immutable caps, full persisted-string classes, metadata key/value no-echo,
  source-ref/table/row identity, per-request org-bound source coverage and
  deterministic in-place-modification/path-swap rejection;
- `GOOS=linux GOARCH=amd64 ... go vet
  ./internal/adapters/unirouter ./cmd/xinsight-importer`: pass;
- full `go vet ./...`, `go test -count=1 ./...` and
  `go test -race -count=1 ./...`: pass;
- fixture conformance: 11/11 pass;
- CI change-router self-test: 9 cases pass; contract-gate self-test: 8 cases
  pass;
- frontend Vitest: 1 file / 6 tests pass; production build, TypeScript check,
  six-path OpenAPI check and 177-entry license check pass;
- Chinese manual regeneration: pass, source SHA-256
  `0fa455fe006ed601d75fe4978dbec658e882597e76b8dcc27d8a1f0924a0cb1d`;
- changed/new Markdown link check: 8 files / 100 relative links / 0
  missing;
- X-Insight's hard-coded metadata intake allowlist exactly matches the observed
  UniRouter producer sanitizer allowlist; and
- bounded credential-pattern scan over all changed/new code and documentation:
  no matches; and
- exact final PR scope: 17 intended files; `git diff --check`: pass;
- same-size in-place file-mutation and path-swap tests: 100 consecutive focused
  iterations pass; and
- the PostgreSQL-backed v2 persistence/replay/API regression that had aged
  outside the default 24-hour search window: 2 consecutive runs pass against
  a temporary loopback PostgreSQL 16 database using the unmodified mapped
  artifact events and an explicit bounded historical search range. The
  temporary server was stopped after verification.

The independent read-only code review found three initial P1 groups: pathname
TOCTOU, blocked-report top-level value echo and incomplete source-ref/per-
request-org truth binding. The execution owner corrected all three plus added
same-process preflight-to-import artifact-hash binding. Its second pass found
two remaining P1s (operator-approved proof bypass and path-reloaded discovery)
and two P2s (whitespace-only values and overbroad no-path wording). The owner
then required successful manifest preflight for operator-approved provenance,
derived discovery from the already bound snapshot, blocked whitespace-only
values and corrected the stderr boundary. The final review reported P0=0,
P1=0 and P2=0. A final one-line tightening of producer-declared
`source_event_hash` to raw lowercase 64-hex was re-reviewed with the same
result. Reviewer execution is independent code review, not runtime or Target
acceptance.

GitHub publication:

- implementation branch: `codex/prd2-internal-real-preflight`;
- commits: `d14dd2aa25b29d32650be7152f97deab553da878` and
  `b0494afd6f8cfc6d18d34e61c2cd095fdc5aea70`;
- PR: #63;
- first PR CI run `29683448192` failed on two test-only portability/time-window
  assumptions: same-length file replacement could share visible Linux file
  timestamps, and the fixed v2 fixture had aged beyond the API's default
  24-hour range. The owner added a second bounded content-digest read and made
  the PostgreSQL test query the exact unmodified event time range; production
  search defaults and artifact provenance were not relaxed;
- final PR CI run `29684113519`: success. Change Classification, Frontend
  Tests, Backend and API Validation and the final Backend Tests contract gate
  passed; PR-only Signed V2 Release Qualification and Fresh Install
  Qualification were skipped as designed;
- squash merge: `cd537c246d75cc149b8064b24dcefb4521cabdb3`; and
- post-merge local repository state: clean `main == origin/main == cd537c2...`.

Shared cooperation publication:

- Open request:
  `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/requests/2026-07-19-x-insight-prd2-internal-real-metadata-inventory-request.md`;
- request SHA-256:
  `fd5cda949c635fb9b19a26e5fa2d149645ee8f289342cf73151540450869a1af`;
- the Open request scopes UniRouter's requested response to the minimum bounded
  read-only inventory and requires UniRouter to confirm its project-local read
  authority. It grants no source write, code change, manifest/artifact
  generation, transfer, import, Target mutation or cleanup.

Remaining gates:

- UniRouter owner-authored response to the Open inventory request; and
- if that response is acceptable, one exact limited cross-project decision
  through both projects' normal planning/PR/required-CI/merged-main chain.

No internal-real manifest/artifact generation, transfer, trial-database
creation or persistence may precede the counterpart response and binding
activation.

Rollback: revert the code/docs through a new PR. The preflight writes no data.
No internal-real cleanup is currently required because no trial database, real
artifact, receipt or backup has been created. The synthetic CI-fix regression
cluster directory `/private/tmp/xinsight-ci-fix-pg.jrfGsE` remains retained
with its PostgreSQL server stopped; directory cleanup is not authorized by
this work and it is not the planned internal-real trial database.

Detailed plan:
[PRD2 Internal-Real UniRouter Metadata Validation Plan](../planning/prd2-internal-real-metadata-validation-plan-2026-07-19.md).

## Closed Work — UR-E exact synthetic Target execution and evidence

Work ID: `XI-PRD2-UR-E-BOUNDED-SYNTHETIC-20260719`

Owner: Codex

Evidence publication branch/base: `codex/ur-e-stage-b` from clean main
`480f5d8f406b312509ae5cde6b5791e856685cd0`

Risk: R2 evidence/status publication. The authorized R4 named non-production
Target write is complete; no further Target mutation is active in this branch.

Status: the shared decision is `Decision accepted` / `Binding` for the exact
bounded synthetic sidecar execution. UniRouter completed Stage A with a fresh
manifest and artifact, published its sanitized evidence through PR #223 /
docs-only CI `29674988916` / merge `df3e41d...`, and released the sequential
baton. X-Insight Stage A.5 reverified receiver, Target, release/database and
pre-existence gates. After three read-only static reviews reported P0=0/P1=0,
the execution owner completed exactly one manifest transfer, one snapshot
transfer, one first-write, one identical-byte replay, bounded API readback,
real product UI readback and final read-only reconciliation. No deployment,
configuration, credential, release or customer-data change occurred.

Scope:

- freeze one fresh manifest-bound synthetic snapshot-v2 producer-to-consumer
  proof without reopening the deferred live feed;
- keep UniRouter's code-bearing producer identity and later clean generation
  HEAD as separate facts;
- define a conditional two-stage flow: UniRouter generation/handoff first,
  then one X-Insight first-write and one identical-byte replay;
- limit promotion to the exact scenario inventory actually present;
- retain normalized synthetic evidence, receipts and hashes while giving every
  named temporary file an exact disposition; and
- record the formal UniRouter response and planning-chain actions required
  before execution.

Verified bounded result:

> External integration verified — execution-owner verified, exact manifest-
> bound synthetic sidecar scenario only.

This is not independent acceptance, the manual-guided `N=1` field task,
customer/sanitized-real data, live-feed freshness, Production evidence or a
claim that the whole UniRouter/X-Insight integration is complete. The binding
runtime remains operator-mediated sidecar-only.

Detailed plan:
[PRD2 UR-E Bounded Synthetic Sidecar Execution Plan](../planning/prd2-ur-e-bounded-synthetic-sidecar-execution-plan-2026-07-19.md).

Execution evidence:
[PRD2 UR-E Exact Synthetic External Integration Evidence](../evidence/prd2/prd2-ur-e-exact-synthetic-external-integration-evidence-2026-07-19.md).

Shared governance records:

- answered request:
  `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/requests/2026-07-19-x-insight-prd2-v2-sidecar-external-integration-artifact-request.md`;
- accepted binding joint decision:
  `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/2026-07-19-prd2-ur-e-bounded-synthetic-sidecar-execution.md`;
- shared decision rules/index:
  `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/README.md` and
  `PRIVATE_HOME/Desktop/projects/PRIVATE_COOPERATION_ARCHIVE/decisions/README.md`.

Activation-branch Local validation on 2026-07-19:

- `git diff --check`: passed;
- `python3 -B scripts/ci-change-router.py --self-test`: 9 cases passed;
- `python3 -B scripts/ci-contract-gate.py --self-test`: 8 cases passed;
- changed X-Insight/shared Markdown local-link check: 82 links resolved;
- governance invariant check: 8 checks passed, including absent UniRouter
  response/handoff, proposed/not-binding state, Stage A.5 state transitions,
  exact producer diff predicate and non-Target local receiver; and
- bounded credential-pattern scan: no matches.

Activation verification on 2026-07-19:

- UniRouter `HEAD = main = origin/main = 9a94d549...`, clean; PR #222 and routed
  CI `29672169049` verified as docs-only publication;
- response final SHA-256 `12857bd7...`, Python 3.14.5 interpreter SHA
  `2477b47f...` and mechanism code SHA `934a2036...` matched;
- exact `EEXIST` probe stopped with exit `73` and left `/dev/null` unchanged;
  one generic success probe produced a regular `0600`, 213-byte expected-hash
  file, which was then individually removed and verified absent;
- the self-hash P1 was corrected by recording the response SHA in external
  Activation Evidence rather than inside the response itself;
- shared decision/root/decision-response indexes and both requests were
  reconciled; and
- all four future sender/receiver paths plus the future sanitized handoff record
  remained absent. No artifact operation occurred.

Two independent read-only reviews inspected the X-Insight diff and shared
request/decision/governance records. Initial review identified activation,
single-writer handoff, Target-temporary-write and producer-identity
ambiguities. The execution owner corrected them by requiring merged-main
planning links, an atomic accepted/binding transition, an exact Stage A.5
handoff record, non-Target local receipt before Target writes, decision-bound
Target paths and a complete `docs/**`-only generation diff predicate. Final
reviews reported P0=0 and P1=0. Reviewers wrote no files, ran no Target or
UniRouter mutation and do not constitute independent runtime acceptance.

The later read-only amended-response audit found the circular self-hash wording
as one P1. The execution owner corrected that governance condition before
activation. The reviewer statically verified the response and PR/CI identities
but did not run the mechanism; the local probes above remain owner
self-verification.

Stage A and Stage B verification on 2026-07-19:

- UniRouter generated request `req-ur-e-v2-synthetic-20260719-001`, manifest
  SHA-256 `b1c096df...` and fresh snapshot SHA-256 `412c9b64...`; the exact
  inventory was 1 call log, 0 trace events and 1
  `not_available_from_source` missing-trace gap;
- X-Insight recomputed sender/receiver equality and passed the read-only
  release/database/path preflight before Target placement;
- the manifest and snapshot were each transferred exactly once. A snapshot
  absence-check quoting error failed closed before snapshot `scp`; read-only
  diagnosis proved the path still absent, and no import or database write
  occurred on that path;
- first-write returned `committed_exact` with 2 event inserts, 1 gap insert and
  0 conflicts; identical replay returned `unchanged_exact` with 0 inserts,
  2 duplicates and 0 conflicts;
- canonical comparison proved every prior row unchanged, only the expected
  new event/gap/provenance/link rows present and replay fully non-mutating;
- bounded APIs returned exactly 2 events and 1 gap with exact token/cost,
  sidecar/proof/assurance and source/provenance facts;
- the actual Target Chinese UI passed exact search, detail, two-node timeline,
  usage node, gap node, source/provenance and capture-gap inspection; and
- final read-only reconciliation confirmed all six evidence-file identities,
  database fingerprints, systemd readiness and unchanged rc.6/PostgreSQL/
  loopback boundary. Temporary files are retained for review; normalized
  synthetic evidence is durable proof and no cleanup occurred.

The frozen shared handoff SHA-256 is
`88dc35e019b649e78f59e3dc6e0d76c6c3bb6a896fd2dbce44634a652b7cba35`.
Three read-only reviewers passed the locked pre-execution gate with P0=0/P1=0,
but none performed Target/UI runtime acceptance. The execution result remains
owner self-verification.

Detailed activation evidence:
[UR-E joint activation evidence](../evidence/prd2/ur-e-joint-activation-2026-07-19.md).

## Closed Work — UR-E cross-project governance correction

Work ID: `XI-PRD2-UR-E-GOVERNANCE-20260719`

Owner: Codex

Branch/base: `codex/ur-e-governance-correction` from clean main
`6b1ea9002a7a8769473d3ceb4b180bf44d6ee6c4`

Risk: R2 documentation status-truth and cross-project ownership correction; no
runtime, schema, deployment, credential, Target or artifact operation

Status: Complete for X-Insight and shared-workspace governance correction,
Local verification, GitHub PR/CI, merge and exact-main docs-only CI

Scope:

- remove the X-Insight-authored update from the UniRouter-owned formal response;
- replace the invalid binding `UR-E` claim with an X-Insight-owned Open request;
- reconcile historical/current source-registry and Target evidence wording;
- update the shared index and X-Insight authoritative planning/evidence chain;
  and
- stop before artifact generation, delivery, import or any UniRouter repository
  change.

Verified governance result: `UR-E` is a proposed X-Insight no-claim gate, not an
accepted UniRouter task or binding dependency. The Open request authorizes
nothing. A UniRouter response, accepted joint decision and links from both
project planning chains are required before any exact artifact can be requested
or delivered. The accepted sidecar-only boundary remains binding and the live
feed remains deferred.

Local validation on 2026-07-19:

- `git diff --check`: passed;
- `python3 -B scripts/ci-change-router.py --self-test`: 9 cases passed;
- `python3 -B scripts/ci-contract-gate.py --self-test`: 8 cases passed;
- changed X-Insight/shared Markdown local-link check: 101 links resolved; and
- bounded credential-pattern scan: no matches.

An independent read-only final review inspected the current X-Insight diff and
the four changed shared-workspace records. It found no new P0/P1 and confirmed
the two reported P1 and three P2 findings closed. It did not write files,
execute tests, access the UniRouter repository/Target or constitute independent
runtime acceptance. Local validation above remains execution-owner
self-verification.

Publication result:

- correction head `694722d2f80ae83430acf07925a37217f5b1273b` opened PR
  #56;
- PR CI run `29666597183` passed Change Classification and the protected
  Backend contract gate; Backend/API, Frontend, Signed V2 and Fresh Install
  jobs were skipped under the exact `safe-docs-only` route;
- PR #56 squash-merged as docs-only main
  `7b44ba3355549aac9e3b5605fdaed452896b024c`; and
- exact-main CI run `29666626401` passed the same bounded docs-only route and
  skipped the four expensive jobs.

This docs-only merge does not replace the code-bearing rc.6 release identity
`0ca502f99181173e3542592ae5a018a5bfc43e0c` or create a new deployable artifact.
The next cross-project state is to await UniRouter's accept/decline/defer
response to the Open request; no artifact work is authorized while that state
is pending.

Detailed record:
[UR-E cross-project governance correction](../evidence/prd2/ur-e-cross-project-governance-correction-2026-07-19.md).

## Closed Work — rc.6 owner-supervised Target and v2 synthetic proof

Work ID: `XI-PRD2-RC6-TARGET-20260718`

Owner: Codex

Risk: R4 owner-supervised release, named non-production Target upgrade and
browser proof

Status: Complete for exact rc.6 publication, named non-production Target
upgrade/rollback readiness and exact v2 synthetic sidecar/API/UI proof.
External integration, customer-site field usability and Production remain
separate and pending/out of scope.

Release prerequisite: branch `codex/rc6-target-prerequisite` from clean
`40f900e14285fb9fb7f22fe6959cda6968280d95`; PR #54 merged exact main
`0ca502f99181173e3542592ae5a018a5bfc43e0c`. Exact-main CI `29648815418`
passed Backend, Signed V2 and Fresh Install qualification. Owner-supervised
workflow `29648976737` published artifact ID `8430900609`; downloaded ZIP
SHA-256
`bd80cd65aa368acc9b1523d7bb68819e364f132e8e876652a595827b5fb623f3`.
The strict seven-file delivery retained no private key and bound the exact
main/CI identities.

Target result: operation `20260718T220854Z-1f5aed3059a2daac` upgraded exact
rc.5 to rc.6. The 51,530-byte PostgreSQL backup passed a 103-entry catalog
check and SHA-256
`dc0a9393fdab20a8b39d2b0f278d93ae9cf708d18866f153076212b698a82290`.
All eight upgrade/activation checks passed. The previous-release rollback
check passed all five checks with `mutating=false`; no actual rollback or down
migration ran. PostgreSQL container ID/StartedAt and both loopback listeners
were preserved. Transition rc.5+rc.6 trust remains intentionally retained
until the rollback window closes.

V2 sidecar result: exact fixture SHA-256
`93bc4c8448f1f838668f9dab10cbba070f0df550665b16d73bb494066fe8d6a1`
first persisted 2 events including 1 gap; identical replay returned 2
duplicate/no-op and 0 conflicts. The post-import evidence fingerprint is 6/6,
`sha256:bb6a9c55c70e8f93c0f677badcf8973c3c4ed05866551b996962f3ea5883f328`.
The API bound exact persistence hash/idempotency, retained exact v2 micro-CNY
and field-group provenance, and explicitly reported synthetic/gap/incomplete
freshness with External and Production false.

Fail-closed owner evidence: attempt 1 stopped before import on an incorrect
Docker server-address expectation. A password-safe diagnostic proved the exact
host-loopback DSN and container addresses. Attempt 2 completed first/replay and
then stopped on a Python microsecond parser rejecting a valid nine-digit Go
timestamp. Attempt 3 was continuation-only, made importer mutation unreachable,
parsed integer nanoseconds, validated retained root-only reports/receipts and
completed API proof without re-importing. No partial artifact was deleted,
overwritten or presented as success.

Independent read-only static review found no P0/P1 before the attempt 2 import
or attempt 3 continuation. It did not execute the scripts, connect to Target,
write the repository or constitute independent Target acceptance. All Target
execution and browser evidence remains execution-owner self-verification.

Real browser result: the approved Session Manager/OpenSSH explicit-loopback
tunnel returned one exact synthetic execution; search-to-detail, two-node
timeline, usage/gap selected nodes, source/provenance, capture-gap panel, all
seven semantic states, `CNY 0.000007 (7 micro-CNY)` and same-origin
rc.6/manual-r6 Chinese manual rendered. One deliberately future-dated `to`
value exercised the stable 400/error UI; correcting it returned 200 throughout
the valid flow. Browser and tunnel were closed and local port 18080 was free.

P0: none observed. P1: none open for the accepted exact rc.6 Target/synthetic
scope. P2: the UI relies on the stable server error for a future `to` value
instead of warning before submit.

Next coordination state: `UR-E` is an X-Insight-owned proposed no-claim gate,
not an accepted UniRouter task or binding dependency. X-Insight opened a
separate non-binding request on 2026-07-19; it authorizes no artifact generation
or delivery. Only after a UniRouter response, an accepted joint decision and
links from both project planning chains may a new artifact be requested and its
exact import/receipt/readback/UI proof promote the scenarios actually present
to External integration. The accepted manual-guided `N=1` User tasks and
customer-site host/custody facts remain separate X-Insight field-entry gates.
The live feed remains deferred; no endpoint, credential, poller or UniRouter
code was added.

Detailed immutable evidence:
[rc.6 named Target, v2 sidecar and UI evidence](../evidence/prd2/p2-4-rc6-named-target-v2-sidecar-and-ui-evidence-2026-07-19.md)
and
[UR-E cross-project governance correction](../evidence/prd2/ur-e-cross-project-governance-correction-2026-07-19.md).

## Closed Work — UniRouter snapshot v2 micro-CNY sidecar

Work ID: `XI-V1-LEDGER-V2-SIDECAR-20260718`

Owner: Codex

Risk: R3 cross-repository monetary schema, mapper-version, persistence and
readback compatibility

Status: Complete for X-Insight-owned implementation, Local verification, PR CI
and main CI with synthetic metadata-only data. At this work's closure,
owner-supervised publication and rc.6 Target were separate and pending; the
later exact rc.6 Target result is now recorded in the preceding work. External
integration and Production remain separate and pending

Baseline: branch `codex/v1-xi-ledger-v2-money` from
`a0c7ab9cf46ba126e090a9694ba66ed0f49a2f7f`. The existing uncommitted money
patch was preserved, reviewed and completed in place; no reset or unrelated
rewrite was performed.

Delivery: source commit `a5f64c011b032a670845009be1f40d796a47d629`
passed PR #52 CI run `29645951982`; PR #52 was squash-merged to `main` as
`0462e698775488d86e1263debf64990239464340`. Exact-main push CI run
`29646094307` passed.

Release identity: this code-bearing change advances the candidate to
`0.2.0-rc.6` / `0.2.0-rc.6/manual-r6` and accepts exact rc.5 as an upgrade
source. It does not overwrite the immutable, named-Target-verified rc.5
identity. Rc.6 publication and Target status remain separate gates; PR and
main CI were the verified scope at this work's closure. The later exact rc.6
publication and Target result are recorded in the preceding work.

Implemented:

1. strict v1 cents versus complete v2
   `cost_micros/currency/amount_scale/money_schema` validation, with no mixed,
   partial, null or unknown money fields;
2. exact string/JSONB/API/UI handling and `BigInt` micro-CNY display;
3. distinct stable v1/v2 adapter manifests, monotonic known v1/v2 source-
   version registry handling, no-version replay compatibility and fail-closed
   unknown source/known manifest-version transitions;
4. validated exported event building instead of malformed-snapshot panic;
5. OpenAPI and M0 schema agreement;
6. an exact golden fixture whose SHA-256 matches the actual UniRouter producer
   artifact;
7. PostgreSQL-backed v1-to-v2-to-late-v1/no-version, first-write, replay,
   fresh/versionless unknown-transition rejection, concurrent v1/v2 registry
   convergence, search and API regression, plus required-CI first/replay CLI
   imports.

Local result: the actual X-Insight CLI persisted the exact synthetic v2
artifact as 2 events including 1 capture gap; an identical replay produced
2 duplicate/no-op, 0 inserts and 0 conflicts. SQL and event API retained
`"cost_micros":"7"`, `CNY`, `1000000` and `micro-cny.v1`. The actual
loopback UI displayed `CNY 0.000007 (7 micro-CNY)` and did not expose
`raw_source_excerpt`.

Local verification: uncached full
`go test -v -race -count=1 -coverprofile` passed at 65.1% statement coverage;
the destructive PostgreSQL 16 host-role qualifier was intentionally skipped
locally because the local service is PostgreSQL 18, then passed in required PR
CI on PostgreSQL 16.
Go vet, fixture conformance, CI-router/contract self-tests, TypeScript, OpenAPI,
component, dependency/license, deterministic build, mock-browser 4/4 and real-
API browser 1/1 checks passed. The temporary server listened only on loopback;
its PostgreSQL-18 readiness rejection was expected and is not Target evidence.
The exact task-owned test databases, two `0600` receipts and temporary coverage
file were then removed one identity at a time after their hashes/results were
recorded.

CI result: PR run `29645951982` passed pinned frontend, Chromium,
backend/API, PostgreSQL 16 host-role/backup/isolated-restore, v2 first/replay
CLI imports and the event-specific contract gate. Main run `29646094307`
correctly skipped the already-complete frontend/backend jobs and passed signed
candidate reproducibility, exact Phase 1 transition/recovery on a clean VM and
fresh install on a second clean VM. CI signing identities and artifacts are
explicitly non-publishable; no owner-supervised release or Target deployment
was performed.

Boundary at this work's closure: this verified only the accepted
operator-mediated sidecar with synthetic data. It did not verify the named
Target, Production, customer data or a live UniRouter feed. The then-existing
named-Target result belonged to exact rc.5, not this rc.6 candidate. Main CI
covered the exact Phase 1 artifact-to-rc.6 transition on a clean VM and a fresh
rc.6 install; it did not prove the existing named Target's in-place
rc.5-to-rc.6 path. That preflight and upgrade were mandatory before the later
rc.6 Target classification recorded in the preceding work.
Pull mode remains default-disabled/mock-only and the whole live-feed slice is
deferred from the first formal version under the accepted 2026-07-18 decision.
No endpoint, credential, cursor/replay product, continuous polling or p95 gate
was added.

Evidence:
[v2 sidecar local evidence](../evidence/prd2/v1-xi-ledger-v2-sidecar-local-evidence-2026-07-18.md).

Work ID: `XI-PRD2-RC5-TARGET-CLOSE-20260714`
Owner: Codex
Risk: R4 named non-production Target release lifecycle, rollback and product UI
proof
Status: Complete for X-Insight-owned P2-4 release and named-Target scope. Exact
`0.2.0-rc.5` passed PR/main CI, owner-supervised publication, Target upgrade,
application rollback, re-upgrade, explicit restart, bounded search-to-detail
browser proof and release-matched Chinese manual inspection. External
integration, live UniRouter feed, `N=1` field usability and Production remain
separate and pending/out of scope as recorded below
Base and exact release source: `main` at
`27e05df26ff7976301a245f9120176e6ca70430d`
Branch: `codex/prd2-rc5-target-evidence`
Release PR: [#49](https://github.com/foigent/x-insight-private-history/pull/49)
Source CI: [29348263612](https://github.com/foigent/x-insight-private-history/actions/runs/29348263612)
Owner workflow:
[29348552251](https://github.com/foigent/x-insight-private-history/actions/runs/29348552251)

## Closed Scope

1. Fixed the rc.4 P1 that applied the current managed-launcher required-file
   list to the exact historical rc.3 previous bundle. The signed manifest's
   product version now selects the bounded historical exception; current and
   unknown versions still fail closed.
2. Reverified the strict seven-file rc.5 delivery, hashes, one-run public key,
   exact-main CI binding and absence of a retained private key.
3. Verified exact rc.3 and rc.5 bundles under a two-key transition store, then
   completed rc.5 upgrade, real application rollback and re-upgrade on the
   named Target.
4. Preserved the PostgreSQL 16 container across the lifecycle, preserved the
   evidence-integrity fingerprint and narrowed active trust back to rc.5 only
   after the rollback window.
5. Enabled the Alibaba Cloud Session Manager management channel without adding
   security-group ingress. Post-proof inspection found that the tested direct
   macOS CLI forward listened on a wildcard socket; it was stopped and
   prohibited. A replacement using the CLI only as an OpenSSH stdio proxy plus
   an explicit `-L 127.0.0.1:18080:127.0.0.1:8080` bind passed listener,
   readiness and UI checks.
6. Proved real bounded search, search-to-detail navigation, evidence-derived
   timeline, selected-node/source/provenance detail, capture gaps, seven
   semantic labels and the rc.5/manual-r5 Chinese manual.

## Claim Boundary

- Target is verified only for exact rc.5 on the named non-production,
  owner-supervised, loopback and approved-metadata scope.
- The retained UniRouter sidecar evidence remains `unclassified` with explicit
  sync/timeline gaps. It is not a live-feed or fresh External integration
  claim.
- Rc.5 has no product login. Approved cloud/SSH credentials plus the active
  verified tunnel are the accepted single-principal access boundary; public
  exposure, multi-user X-IAM and audit-on-read are not implemented.
- P0: none. The rc.4 P1 is closed. The direct-CLI wildcard-listener P1 is
  closed for the current operator workflow by termination, prohibition and a
  verified loopback-only OpenSSH replacement. One transient tunnel read
  recovered through the UI's bounded retry path and is recorded as P2
  operational evidence.
- Phase 1 remains closed. The two temporary snapshot files are still due on
  2026-07-17 and were not touched early.

Detailed exact identities, operation IDs, receipts, hashes and browser results
are in
[P2-4 rc.5 named Target lifecycle and UI evidence](../evidence/prd2/p2-4-rc5-named-target-lifecycle-and-ui-evidence-2026-07-14.md).
The post-proof listener correction and approved replacement are in
[P2-4 Target access listener correction](../evidence/prd2/p2-4-target-access-listener-correction-2026-07-14.md).

# Prior Execution Record: XI-PRD2-NAMED-TARGET-20260714

Work ID: `XI-PRD2-NAMED-TARGET-20260714`
Owner: Codex
Risk: R4 named non-production Target release adoption, database migration,
service activation and rollback readiness
Status: In progress; corrected storage/credential bridge passed exact-main CI,
Target credential preparation and legacy adoption completed, but
`0.2.0-rc.2` upgrade failed closed at release staging because target-created
files inherited restrictive `umask 077`. Bounded abort restored the exact
Phase 1 service and role login state; schema remains 9. `0.2.0-rc.3` permission
correction is in local verification and gains no status before new CI/release/
Target evidence
Base: `main` at `894d72560ab93ae3fcc200ab0cb76ec1fc55068f`
Branches: `codex/prd2-named-target-validation`,
`codex/prd2-admin-bridge-ci-fix`, `codex/prd2-restrictive-umask-fix`
First rejected Target candidate: owner-supervised `0.2.0-rc.1` from
`a77ba1de2064d6b297c1010de708019e46fd61cb`; later `main` commits are
documentation-only and do not change the product payload
Rejected Target candidate: owner-supervised `0.2.0-rc.2` from
`894d72560ab93ae3fcc200ab0cb76ec1fc55068f`; exact-main CI and delivery passed,
but Target `stage_release` failed before migration/activation
Current corrective candidate: `0.2.0-rc.3` on this branch; not yet merged,
published or Target verified

## Active Scope

1. Requalify and publish a new seven-file `0.2.0-rc.3` owner-supervised delivery
   from exact current main and matching successful push CI.
2. Upgrade the already-adopted managed legacy installation to the accepted
   PRD2 release through `xinsightctl` only; do not repeat `adopt`.
3. Keep X-Insight and PostgreSQL loopback-only and use a short-lived approved
   private management path; do not add a public security-group ingress rule.
4. Use only existing fixture/synthetic evidence or separately approved
   sanitized metadata because the named Target disks are not encrypted.
5. Verify service restart, listeners, health/readiness, execution UI, timeline,
   node/source/provenance/capture-gap views, Chinese manual, lifecycle receipt
   and application rollback readiness on the named Target.
6. Record Target evidence separately from Local, CI, External integration and
   Production. Do not infer live UniRouter integration or customer acceptance.

## Authorization And Stops

- The User selected the named Alibaba Cloud ECS as the PRD2 Target and had
  already authorized continued PRD2 execution until a genuine User decision
  or UniRouter-cooperation boundary.
- This batch changes X-Insight on that named non-production Target only. It
  does not change UniRouter, X-IAM, Tool Gateway, cloud firewall rules or the
  shared cross-project contract boundary.
- The two Phase 1 Gate A snapshot artifacts are due 2026-07-17. Current date is
  2026-07-14, so they remain untouched.
- Stop on any delivery/hash/signature/legacy-identity mismatch, unexpected
  workload or listener, unapproved data, failed backup/preflight, non-resumable
  journal, or rollback-compatibility failure.
- Target mutations use exact paths and append-only evidence. No recursive,
  wildcard or batch deletion is permitted.

## Read-Only Target Preflight

- The named ECS is running Ubuntu 22.04 x86_64 with 4 vCPU, approximately
  8 GiB memory and a mounted 50 GiB data volume at `/data/x-insight`.
- Cloud security-group ingress has no rules; host UFW defaults to deny incoming.
  X-Insight listens on `127.0.0.1:8080` and PostgreSQL 16 Compose listens on
  `127.0.0.1:5432`.
- `xinsight.service` is enabled and active. Its current release pointer is the
  exact Phase 1 commit `b9e5811cc53a5cb06614956e66a1c54756617f0a`;
  `/healthz` and `/readyz` return HTTP 200 while PRD2 UI/manual routes correctly
  remain absent before upgrade.
- The PostgreSQL container is healthy and uses dedicated persisted storage.
  Target disks are unencrypted and have no automatic snapshot policy, so this
  run remains fixture/synthetic-only unless an approved sanitized dataset is
  explicitly supplied.
- The downloaded owner-supervised delivery has exactly seven files and passes
  `SHA256SUMS`. Linux executable verification and extraction remain Target-side
  gates.

## Current Stage

1. Make signed payload extraction, newly created lifecycle directories and
   copied systemd/Compose runtime files independent of operator umask.
2. Bump the failed candidate to `0.2.0-rc.3`, regenerate the release-matched
   manual and add process-level `umask 077` regression coverage.
3. Merge only after PR/main CI, produce a new exact-main owner-supervised
   delivery and repeat Target-side trust verification with its new identity.
4. Preserve the current managed-legacy state; run `upgrade --check` and
   `upgrade` directly after every corrected gate passes, without re-adoption.
5. Execute post-activation checks and persist sanitized Target evidence before
   any final status promotion.

## Storage/Bridge Candidate Local Verification

- The managed Compose, host-control data path and both supported-host
  qualification scripts now use `/data/x-insight/postgres`, root
  `PGDATA=/var/lib/postgresql/data` and fixed container name
  `xinsight-postgres`.
- The legacy-host qualification now starts the actual Phase 1 role/password-file
  profile, makes and validates a pre-bridge backup, creates only the managed
  admin bridge, then leaves reader/importer creation and grants to the durable
  `adopt` journal.
- Local `go test ./...`, `go test -race ./...`, `go vet ./...`, frontend type
  check, six frontend unit tests, four desktop/narrow browser flows, OpenAPI,
  177 locked-license entries, high-severity dependency audit, frontend
  production build, Bash syntax, actionlint v1.7.7, `git diff --check`, nine CI
  router cases and eight aggregate-gate cases pass. These are execution-owner
  self-tests, not supported-host CI or Target acceptance.
- Corrected fresh-install and legacy adopt/upgrade/rollback/re-upgrade/restore
  qualification remain pending exact `main` CI; no release or Target status is
  inferred from local checks.

## First Corrected Main Qualification Attempt

- PR `#45` passed release-relevant source CI `29335229676` and merged as
  `637971eb1a2570a236608113acff9e0928b5078a`.
- Main CI `29335436110` passed change routing, reproducible signed build, exact
  Phase 1 artifact reproduction and signed payload verification, then failed
  closed before `adopt` in the legacy-host qualification.
- The failure was a qualification-only SQL assertion that selected PostgreSQL
  catalog column `rolsuper` without a `FROM pg_roles` source. The role had been
  created and the Phase 1 service remained active; no product Target was
  touched. The assertion now obtains the connected role's boolean from
  `pg_roles` and still requires exact `xinsight_admin|true` output.
- Fresh-install qualification was correctly not started after the upstream
  failure. A new PR/main run is required; the failed run grants no CI, release
  or Target status.

## Corrected Main And RC.2 Delivery

- PR `#46` passed release-relevant source CI `29335667662` and merged the
  qualification assertion fix as exact current main
  `894d72560ab93ae3fcc200ab0cb76ec1fc55068f`.
- Main CI `29335953131` passed signed-v2 reproducibility, exact legacy bridge,
  adopt/upgrade, external SIGKILL recovery, rollback/re-upgrade/isolated
  restore, a second clean-VM fresh install and the aggregate gate.
- Owner-supervised workflow `29336389137` passed for that exact main and
  published artifact `8312130217`. The strict seven-file delivery, checksums,
  Ed25519 signature and Linux extraction were reverified before Target use.
- This closes only the corrected Local/CI/publishing gates. It does not override
  a later Target result or establish External integration/Production.

## RC.2 Target Adoption And Safe Abort

- A fresh Target staging directory passed strict delivery, signature, binary,
  manual, Compose and legacy identity checks. The system trust store was
  atomically advanced only after the previous trust was preserved.
- The backed-up credential bridge created the versioned three-role credential
  set and only the required admin bridge before durable adoption. `adopt`
  completed as operation `20260714T133920Z-477373b9e079a228`; the installation
  is now a managed legacy state at schema 9.
- `upgrade` operation `20260714T133925Z-e1a466a0206b23f1` stopped at
  `stage_release`: Cloud Assistant's root shell used `umask 077`, and the new
  release root, `bin/` directory and binaries inherited `0700` instead of the
  signed `0755` contract. Only `stop_service` and `exclusive_maintenance` had
  completed; no backup, migration or activation occurred.
- The allowed bounded `abort` restored importer/reader/admin login behavior,
  the exact Phase 1 current pointer and an active/ready service. Schema remains
  9. The failed release tree, operation journal, receipts and both staging
  attempts remain preserved; no deletion concealed the failure.
- `0.2.0-rc.2` is rejected for Target use. The next attempt must use a new
  version, commit, one-run trust identity and staging path, and must upgrade
  directly from managed legacy without repeating `adopt`.

## RC.3 Corrective Local Verification

- Signed payload extraction and a complete lifecycle install both pass under a
  process-level `umask 077`; release roots/directories and `bin/` remain
  `0755`, regular payload/runtime files remain `0644`, and the new
  `/opt/x-insight` parent remains traversable.
- `go test ./...`, `go test -race ./...` and `go vet ./...` pass. The host
  lifecycle suite includes the restrictive-umask regression and all existing
  failure/recovery cases.
- Frontend type checking, six unit tests, production build and four
  desktop/narrow Playwright flows pass. OpenAPI reports six required paths,
  licenses report 177 locked entries and `npm audit --audit-level=high` reports
  zero vulnerabilities.
- Eleven fixture-conformance cases, Bash syntax/guard checks, actionlint
  v1.7.7, nine CI-router cases and eight aggregate-gate cases pass.
- Two consecutive manual generations produced identical HTML/CSS/SVG hashes;
  the generated manual reports `0.2.0-rc.3/manual-r3`.

These are execution-owner self-tests. PR/main CI, a new owner-supervised
delivery and the named Target rerun remain mandatory.

## Target Pre-Adoption Findings

- The seven-file `a77ba1de...` delivery, Actions ZIP digests, exact Phase 1
  legacy archive, Ed25519 signature, five binary identities, schema `11`,
  manual identity and legacy allowlist all passed Target-side verification.
- The first extraction attempt inherited an operator-set `umask 077`; the
  release verifier correctly rejected `0700/0600` payload modes. A new
  destination extracted with the release-required `0755/0644` modes and
  verified successfully. The failed destination remains preserved.
- `adopt --check` first exposed a legacy environment path pinned to old release
  `4e188350...` while `/opt/x-insight/current` is exact final Phase 1
  `b9e5811...`. The two-key root-only file was atomically normalized to
  `/opt/x-insight/current/migrations`; before/after SHA-256 values were
  retained and the active service remained ready.
- The next fail-closed check found no three-role credential set. The actual
  Phase 1 database uses legacy superuser `xinsight`, whereas the managed
  contract requires `xinsight_admin`, `xinsight_reader` and
  `xinsight_importer`. No role or credential-set mutation has occurred.
- Host PostgreSQL 16.14 clients were installed from the official PGDG Jammy
  repository after checking key fingerprint
  `B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8`. A root-only custom-format backup
  was created before any future role bridge and passed exact SHA-256 plus
  `pg_restore --list` verification.
- The blocking P1 is the release/Target storage mismatch: the active exact
  PostgreSQL 16.14 container uses `PGDATA=/var/lib/postgresql/data` over
  `/data/x-insight/postgres`, while the signed candidate Compose declares
  `/var/lib/x-insight/postgres` and nested `PGDATA=.../pgdata`. Starting that
  candidate Compose could initialize an empty cluster. The execution owner
  stopped before role creation, adopt, service stop or database migration.

# Prior Execution Record: XI-PRD2-SUPPORTED-HOST-SIGKILL-20260714

Work ID: `XI-PRD2-SUPPORTED-HOST-SIGKILL-20260714`
Owner: Codex
Risk: R4 release lifecycle and abrupt-process-death recovery; disposable CI
hosts only, no Target mutation
Status: Complete for X-Insight-owned supported-profile recovery proof and exact
post-recovery owner-supervised delivery; named Target, User tasks and UniRouter
field evidence pending
Base: `main` at `1dcdfe78c7ba7e44b2133e4ede36042ef07c1415`
Branches: `codex/prd2-supported-host-sigkill-proof`,
`codex/prd2-sigkill-marker-permission-fix`,
`codex/prd2-sigkill-wait-trap-fix`

## Active Scope

1. Close the supported-host P2-4 crash-recovery sample without compiling a
   test-only crash hook into `xinsightctl`.
2. Exercise real external process death at one after-action/before-phase-
   completion boundary and prove durable inspect/recover/resume behavior.
3. Add phase-complete recovery coverage across install, adopt, upgrade,
   application rollback and restore.
4. Preserve optimized CI routing: release-relevant PRs run source tests only;
   exact release-relevant main runs own signed/legacy/crash and fresh-install
   qualification.
5. Generate the exact post-recovery `owner_supervised_trial` delivery before an
   evidence-only main commit makes the safe-docs run ineligible as a release
   source.
6. Stop before customer-host access, approved customer data, live UniRouter
   behavior or User field-task results are inferred.

## Authorization And Stops

- The User authorized continued PRD2 execution until a genuine User decision or
  UniRouter-cooperation boundary and requested CI optimization without reducing
  project safety.
- This work changes only X-Insight. It does not change UniRouter, X-IAM, Tool
  Gateway, the shared cooperation workspace, a customer Target or credentials.
- The two Phase 1 Gate A snapshot artifacts are due 2026-07-17. Current date is
  2026-07-14, so they remain untouched.
- The external SIGKILL harness refuses use outside the explicitly disposable CI
  profile and targets only the verified isolated process group.

## Implemented And Verified

- PR #40 merged external process-group SIGKILL qualification and 53 durable
  phase cases as `dcdc2c3ce14998cb4ff17ad46b1329f8340d9119`.
- Exact-main run `29300398720` correctly exposed an unreadable root-to-runner PID
  marker. PR #41 fixed atomic root-owned mode-`0444` publication and merged as
  `e9afd27051c59fdb7f172acf7c660618d895c632`.
- Exact-main run `29300748994` then correctly exposed the global `ERR` trap on
  expected wait status 137. PR #42 captured/asserted that status explicitly and
  merged as `a77ba1de2064d6b297c1010de708019e46fd61cb`.
- Final exact-main run `29301064665` passed signed/legacy/SIGKILL qualification,
  same-operation recover/resume, later rollback/re-upgrade/restore and separate
  clean-VM fresh installation. The aggregate gate passed; Frontend and
  Backend/API correctly skipped on main.
- Owner-supervised workflow `29301384798` accepted that exact main/source CI,
  rechecked the strict boundary and published artifact `8298646384`. The
  downloaded ZIP digest and seven-file contents passed execution-owner
  inspection; the one explicit temporary ZIP was then deleted and confirmed
  absent.
- Local `go test ./...`, `go vet ./...`, actionlint, shell guard/syntax checks,
  9 router cases, 8 aggregate-gate cases and all 53 lifecycle phase cases pass.

Detailed facts and hashes are in
[P2-4 supported-host SIGKILL recovery evidence](../evidence/prd2/p2-4-supported-host-sigkill-recovery-evidence-2026-07-14.md).
These checks are execution-owner self-verification, not independent acceptance.
They close the supported-profile process-death sample, not literal host power
loss, Target, External integration or Production.

## Next External Gates

1. Named-Target rc.5 qualification is complete; confirm the actual customer-site
   host/access/maintenance-window facts if that host differs from the qualified
   Target.
2. Run the accepted manual-guided install/recovery/investigation tasks with the
   User and record the `N=1` field result.
3. If a later joint decision accepts X-Insight's proposed `UR-E` gate, obtain
   the specifically authorized sidecar artifact plus integration window before
   claiming current External integration. No current UniRouter obligation is
   implied. The live-feed slice is deferred from the first formal version.

# Prior Execution Record: XI-PRD2-OWNER-SUPERVISED-TRIAL-RELEASE-20260714

Work ID: `XI-PRD2-OWNER-SUPERVISED-TRIAL-RELEASE-20260714`
Owner: Codex
Risk: R3 release trust, artifact provenance and field-bootstrap boundary; no
target mutation
Status: Complete for X-Insight-owned implementation, routed PR/main CI and
exact owner-supervised GitHub delivery; named Target and field proof pending
Base: `main` at `2669b35c9b0e835068981adc645ba73e23c8f789`
Branch: `codex/prd2-owner-supervised-trial`

## Active Scope

1. Record the User's acceptance of an owner-supervised first-customer trial:
   the User and Codex perform every install/upgrade, the customer cannot
   self-upgrade, and no long-lived offline signing key is created for PRD2.
2. Add a manually triggered, exact-main GitHub release path that generates one
   ephemeral Ed25519 identity, builds the existing signed-v2 bundle, removes
   the private-key file before upload, and publishes only the artifact,
   bootstrap verifier, public trust store, checksums and a bounded trial-release
   record.
3. Keep ordinary PR/push CI artifacts explicitly non-publishable. The new path
   is classified only as `owner_supervised_trial`, not a stable commercial,
   unattended, self-service or Production release.
4. Simplify the release-matched Chinese manual so the User never handles a key
   or signing command; Codex verifies the exact main commit, green CI, GitHub
   run identity and SHA-256 values before every field operation.
5. Continue to stop before a named Target mutation, approved customer data or
   live UniRouter endpoint/auth/cursor/revision behavior is inferred.
6. Optimize CI without weakening branch or release controls: route safe docs,
   cancel stale PR runs, keep main release qualification non-cancellable,
   aggregate the protected check, merge redundant release setup and cache only
   locked dependency/browser downloads.

## Authorization And Stops

- The User accepted this trial posture on 2026-07-14 and instructed Codex to
  continue PRD2 until a genuine product decision or UniRouter cooperation is
  required.
- This batch changes only X-Insight. It does not change UniRouter, X-IAM, the
  shared cross-project decision workspace, a customer host, credentials or
  retained evidence.
- The two Phase 1 Gate A snapshot artifacts are due 2026-07-17. Current date is
  2026-07-14, so they remain untouched.
- The ephemeral key is not a persistent publisher identity. The trial release
  remains valid only inside the recorded User+Codex supervised bootstrap; a
  second customer, delegated installer or self-service/unattended upgrade
  requires a newly accepted persistent KMS/HSM, keyless or offline authority.

## Local Evidence

The bounded implementation and owner self-verification are recorded in
[P2-4 owner-supervised trial Local evidence](../evidence/prd2/p2-4-owner-supervised-trial-release-local-evidence-2026-07-14.md).
Exact frozen-toolchain CI and canonical `main` workflow publication are now
verified. Named Target and User task evidence remain pending and must be
recorded separately.

PR #37 run `29286487988` exposed and correctly blocked a latent signed-v2
gzip-footer parsing defect after the exact build/reproducibility checks passed.
The focused fix drains and validates the bounded gzip/tar end state while still
rejecting appended bytes/members. Exact-toolchain local merge-ref reproduction
and regression tests pass. Replacement run `29287222439`, attempt 3, passed all
five existing jobs after GitHub billing was restored; this closes the external
billing blocker but does not yet verify the new routed workflow.

The CI routing implementation and acceptance boundary are recorded in
[CI routing and release qualification contract](../architecture/ci-routing-and-release-qualification-2026-07-14.md).
Router self-tests, current-PR classification, YAML parsing and `git diff
--check` pass locally. Release-relevant PR run `29298496412`, exact-main run
`29298660052` and owner-supervised run `29298897133` passed with the required
complementary job routes. PR #37 merged as
`3884f6d7c9a8de04bb558c41975f44f3c511c671`.

The exact workflow artifact ID, API digest, seven-file hashes and download
recheck are recorded in
[P2-4 owner-supervised trial main delivery evidence](../evidence/prd2/p2-4-owner-supervised-trial-main-delivery-evidence-2026-07-14.md).
Safe-docs-only PR #38 run `29299294613` passed classification and the protected
aggregate gate while all four expensive jobs skipped. The evidence merge became
`0485a60bdbe296f65ad9e21698b2163e0f532166`; safe-docs-only main run
`29299387213` passed the same route. All CI matrix rows are now executed without
blocking or broadening the already verified release.

# Prior Execution Record: XI-PRD2-P2-4-RELEASE-UPGRADE-MANUAL-20260714

Work ID: `XI-PRD2-P2-4-RELEASE-UPGRADE-MANUAL-20260714`
Owner: Codex
Risk: R4 release trust, host/database mutation and field recovery boundary
Status: Complete for the X-Insight-owned P2-4 implementation slice; merged and
main CI verified; named Target and field release authority remain separate gates
Base: `main` at `a0194e7`
Branch: `codex/prd2-p2-4a-release-foundation`
PR: [#35](https://github.com/foigent/x-insight-private-history/pull/35) merged as
`7d56a36fcce255be152621a39c48645be6d7549c`

## Active Scope

1. Produce the P2-4 signed SemVer release foundation and deterministic Linux
   build/verify pipeline without changing the Phase 1 legacy build script.
2. Implement root-only, durable install/adopt/upgrade/application rollback/
   restore/recover lifecycle for the accepted Ubuntu 22.04/PostgreSQL 16 profile.
3. Deliver the release-matched Simplified Chinese source/offline manual and
   same-origin product-help path.
4. Stop before a field signing authority/bootstrap trust receipt, named Target,
   customer host or UniRouter live-feed dependency is inferred.

## Authorization And Stops

- The User authorized X-Insight-owned PRD2 execution until a genuine product
  decision or UniRouter-cooperation boundary. This work changes X-Insight only.
- No UniRouter code/runtime/contract, X-IAM, Tool Gateway, customer target,
  credential or approved evidence was changed.
- The two Phase 1 Gate A snapshot artifacts are due 2026-07-17. Current date is
  2026-07-14, so they remain untouched.
- CI uses an ephemeral signing key and labels its artifact non-publishable. A
  customer/field artifact requires User acceptance of signing key custody,
  public trust distribution and the out-of-band bootstrap receipt.

## Implemented And Verified Locally And In CI

- Commits `a8706dd` through `180d6aa` implement build/schema/config/trust
  foundations, recoverable host lifecycle, clean-host qualifiers and corrected
  release-matched manual commands. Exact Phase 1 artifact/manifest allowlisting,
  every durable phase boundary, legacy→N→rollback→re-upgrade and isolated
  restore failure/recovery tests pass.
- Added signed-v2 pack/verify/exclusive-extract tooling, versioned bootstrap
  identity and a build pipeline for four runtime binaries plus the external
  bootstrap verifier. The pipeline freezes Go 1.25.11, Node 22.23.1 and npm
  10.9.8 and double-builds on Ubuntu 22.04 CI.
- Added 12 `zh-CN` Markdown chapters and deterministic offline HTML/CSS/SVG,
  embedded `/manual/zh-CN/`, product navigation and release/manual identity.
- Full `go test ./...`, TypeScript, six component tests, production frontend
  build and four desktop/narrow Playwright flows pass. Manual double-generation
  hashes match; real-browser desktop/390px inspection has no console error,
  no page overflow and no off-origin request.
- Local host toolchain is Go 1.25.0, Node 23.11.0 and npm 10.9.2, so it is not
  used to claim the exact signed release build. Exact-toolchain signed evidence
  belongs to the new CI job.
- GitHub Actions run
  [29278281737](https://github.com/foigent/x-insight-private-history/actions/runs/29278281737)
  passed Backend, Frontend, exact Phase 1 artifact reproduction, signed-v2
  double-build, exact legacy adopt/upgrade/application rollback/re-upgrade/
  isolated restore on one clean Ubuntu 22.04 VM, and fresh install plus
  synthetic fixture import/readback on a second clean VM. Both host result
  schemas classify themselves `ci_clean_vm_not_target`.
- The non-publishable PR merge-ref candidate was
  `0f82712ebb3d498d3eed6c64fdf4697b4de37801`, bundle SHA-256
  `7fbef7fbfac319384b5d9241df62b35cba6a0476f14671c7186fa30d239d9eef`
  and manual source SHA-256
  `ba776572e96aa5f7132fb8e023777cd3d1130edbb7252ecbc205b5e91d37f581`.
  It uses an ephemeral CI key and is not a field artifact.
- PR #35 squash-merged to `main` as
  `7d56a36fcce255be152621a39c48645be6d7549c`. Main push run
  [29279421355](https://github.com/foigent/x-insight-private-history/actions/runs/29279421355)
  repeated all five jobs successfully, including both clean-VM host paths.

These are execution-owner self-tests, not independent acceptance. A supported-
host SIGKILL/power-loss sample, named Target, field trust authority, manual-
guided User tasks, External integration and Production remain Pending.

# Prior Execution Record: XI-PRD2-MOCK-PULL-CLOSURE-20260713

Work ID: `XI-PRD2-MOCK-PULL-CLOSURE-20260713`
Owner: Codex
Risk: R3 cross-project data, revision and no-raw boundary
Status: Complete; Local and CI verified; PR merged
Base: `main` at `2e83cf7d78e22c6b60af0450b0140de50102687c`
Branch: `codex/prd2-mock-pull-closure`

## Active Scope

1. Close the X-Insight-owned deterministic mock evidence required by
   PRD2-A13 without adding a live UniRouter transport.
2. Prove activation/source binding, monotonic revisions, duplicate replay,
   page failure isolation, no-raw snapshot boundaries and diagnostic
   terminal-to-search visibility through the real PostgreSQL projection.
3. Keep cursor/revision state process-local and test-only until a joint live
   contract defines durable replay/reset/authentication semantics.

## Authorization And Stops

- The User authorized X-Insight-owned PRD2 implementation through the first
  genuine product-decision or UniRouter-cooperation boundary.
- No UniRouter code/runtime, endpoint, credential, customer target or Phase 1
  evidence was changed. The accepted sidecar contract remains binding.
- The two retained Gate A JSON snapshots are not due for controlled deletion
  until 2026-07-17 and remain untouched.

## Implemented And Verified Locally

- Added exact mock activation/source/cursor/authorization binding and rejects
  pre-activation history, source changes and non-progressing paged cursors.
- Added one-execution snapshot, exact-byte hash, false safety-declaration and
  sensitive-key enforcement.
- Added monotonic revision/completeness checks, exact duplicate no-op behavior,
  per-page commit/cursor isolation and concurrent-poll rejection.
- Added a real PostgreSQL persistence/search test. The focused Local run
  observed `p95=2.011092s` against the diagnostic `10s` target while preserving
  `timeline_complete=false` and `sync_state=gap`.
- Full Go race tests, vet and 11/11 fixture conformance pass locally.
- PR #33 Actions run `29260630135` passed frontend, PostgreSQL 16 backend and
  Go 1.25.11 Linux release jobs. The merge-ref artifact SHA-256 is
  `87a23331cce6cf012470dba2e5232158ffc78ee86e6ac915392ffdb9ecfcd02e`.

Detailed evidence is in
[P2-0 mock pull closure Local evidence](../evidence/prd2/p2-0-mock-pull-closure-local-evidence-2026-07-13.md).
This is owner self-verification, not independent acceptance. Live UniRouter,
Target, External integration, field usability and Production remain Pending.

GitHub delivery:

- PR [#33](https://github.com/foigent/x-insight-private-history/pull/33) passed Actions runs
  `29260630135` and `29260908364`;
- branch commits were `872dd2051900de617c50ac8e3f1d7ccfc00890c5`
  and `0f2dc415b23ece67bc5f69c74b62f989ac5e0772`; and
- PR #33 squash-merged to `main` as
  `cc084b396a50aa032f74e8eb8d4dd5d127d468a6`.

# Prior Execution Record: XI-PRD2-P2-2-P2-3-INVESTIGATION-UI-20260713

Work ID: `XI-PRD2-P2-2-P2-3-INVESTIGATION-UI-20260713`
Owner: Codex
Risk: R2 investigation truth, accessibility and same-origin UI compatibility;
R3 no-raw/proof/provenance presentation boundary
Status: Complete; Local and CI verified; primary PR merged
Base: `main` at `b754100a4eee9024707fe9c189c71ee579966a04`
Branch: `codex/prd2-p2-2-investigation-ui`

## Active Scope

1. Implement the P2-2 search/detail/timeline/selected-node application over the
   merged P2-1 bounded APIs.
2. Complete P2-3 source/provenance/freshness/capture-gap presentation and all
   seven accepted semantic labels without adding a UI fact ledger.
3. Cover loading, empty, error, degraded, narrow-browser, hostile-response and
   no-raw behavior with component and real-browser evidence.
4. Exercise the product first with deterministic semantic fixtures, then with
   real local Go API/PostgreSQL and retained approved historical UniRouter
   normalized evidence.
5. Keep live completed-execution transport disabled until UniRouter accepts a
   versioned endpoint/auth/cursor/revision contract.

## Authorization And Stops

- The User authorized X-Insight-owned PRD2 implementation through the first
  genuine product-decision or UniRouter-cooperation boundary.
- This batch changes X-Insight only. It does not modify UniRouter, X-IAM,
  customer data, target services, credentials or the accepted sidecar runtime
  contract.
- The retained Phase 1 `xinsight_gatea` database was used read-only. A named
  PRD2 database copy received migrations 10/11 for browser investigation; the
  source remained at nine migrations, 12 events and five gaps.
- Legacy approved evidence without P2-1 immutable import provenance remains
  `unclassified` in the API/UI. Historical approval is not reconstructed from
  names or operator knowledge.
- The two retained Gate A JSON snapshots are not due for controlled deletion
  until 2026-07-17 and remain untouched.

## Implemented In P2-2/P2-3

- Replaced the P2-0 foundation placeholder with embedded same-origin `zh-CN`
  execution search and opaque execution-detail routing.
- Added all accepted filters, fixed-snapshot cursor navigation, safe Chinese
  problem guidance and source-list-specific degraded behavior.
- Added execution proof/assurance/freshness facts, revision/watermark display,
  evidence chronology and explicit declared-parent-only presentation.
- Added selected-event field groups, seven-state provenance labels, allowlisted
  source/hash/schema/fidelity facts and no-raw payload-policy copy.
- Added independently paged confirmed/candidate capture-gap inspection and
  associated-event navigation.
- Added accessible loading/empty/error states, skip navigation, visible focus,
  text-plus-code badges, desktop and narrow responsive layouts and reduced-
  motion behavior.
- Added six component tests, deterministic desktop/narrow Playwright coverage,
  an opt-in real-API browser test, and GitHub Chromium execution after the
  deterministic embedded-asset check.

## Local Verification

Owner self-verification is recorded in
[P2-2/P2-3 investigation UI Local evidence](../evidence/prd2/p2-2-p2-3-investigation-ui-local-evidence-2026-07-13.md).
Key results:

- exact Node `22.23.1` and npm `10.9.8` TypeScript/OpenAPI/component/audit/
  license/build checks: pass; six component tests pass;
- deterministic desktop and 390px narrow Playwright search-to-node tests: 2/2
  pass using normal pointer activation;
- two exact-toolchain builds returned identical HTML/JS/CSS SHA-256 values;
- commit `bde01a0260ff166fe969aba1ff0fd65468f93353` passed the local
  commit-addressed Linux release builder/verifier with Go `1.25.0`; the GitHub
  Go `1.25.11` PR merge-ref release evidence is recorded below;
- real UI/Go API/local PostgreSQL 18 browser path: pass with no console errors
  and no raw API request;
- approved historical Gate A denial and success narratives both completed
  search-to-gap/source browser investigation on the migrated PRD2 copy;
- `go vet ./...`, full Go tests, full race tests and 11/11 fixture conformance:
  pass.

Deterministic semantic fixtures are not integration evidence. The historical
Gate A runs were previously approved Phase 1 evidence, not a new External
integration run. PostgreSQL 16 Target, field usability, current live UniRouter
feed and Production remain Pending. All Codex-run checks are owner self-
verification, not independent acceptance.

GitHub delivery:

- PR [#31](https://github.com/foigent/x-insight-private-history/pull/31) passed `Frontend
  Tests`, PostgreSQL 16 `Backend Tests` and Go 1.25.11 `Linux Release Artifact`
  in Actions run `29258723199`;
- the PR merge-ref release artifact SHA-256 was
  `3771175f280190a8749060c109684504e336f118c6c23a943aceea7f091e1148` and its
  manifest SHA-256 was
  `ac79968d751c9f7e05d55fac61b88d0fc46fa230ee73578a96e94581e0712085`;
- the branch commits were `bde01a0260ff166fe969aba1ff0fd65468f93353`
  and `9d2e0a2d1ec3373d1b8c116c03d0ba7605c583d1`; and
- PR #31 squash-merged to `main` as
  `582e9692fc0d35f2e93c4105a891e09207bb5f8d`.

The CI release identifies the pull-request merge ref, not a promoted SemVer
field artifact for the squash commit. P2-4 owns that qualification.

# Prior Execution Record: XI-PRD2-P2-1-INVESTIGATION-API-20260713

Work ID: `XI-PRD2-P2-1-INVESTIGATION-API-20260713`
Owner: Codex
Risk: R3 migration/import provenance and bounded read-path correctness; R2
versioned internal API compatibility and diagnostic performance
Status: Complete; Local and CI verified; primary PR merged
Base: `main` at `4b6dca44a0947c22bd4a6059a0c96fbb475c4da8`
Branch: `codex/prd2-p2-1-investigation-api`

## Active Scope

1. Implement the six P2-0 OpenAPI read paths over persisted evidence without
   turning execution summaries or timeline nodes into a second fact ledger.
2. Add collision-safe persisted execution identity, immutable import
   provenance, fixed-`as_of` keyset paging, safe typed event DTOs and explicit
   freshness/proof fallbacks.
3. Enforce the accepted snapshot, event, database-pool, writer, HTTP,
   concurrency, rate and query bounds before the feature UI depends on them.
4. Verify chronology versus declared causality, source collisions, cursor/error
   behavior, no-raw projection and the accepted diagnostic 1x data shape.
5. Keep live completed-execution transport disabled pending UniRouter's
   acceptance of the proposed versioned contract.

## Authorization And Stops

- The User authorized X-Insight-owned PRD2 implementation through the first
  genuine product-decision or UniRouter-cooperation boundary.
- This batch changes X-Insight only. It does not modify UniRouter, X-IAM, a
  target host, customer data, credentials, retained Phase 1 evidence or the
  jointly accepted sidecar-only runtime boundary.
- Operator-approved proof requires an artifact hash, approval-manifest hash and
  exchange ID. Synthetic imports remain explicitly synthetic; legacy rows do
  not silently become synthetic or approved evidence.
- Live pull endpoint, authentication, cursor/revision and activation behavior
  remain stopped at the joint-contract boundary.
- The two retained Gate A JSON snapshots are not due for controlled deletion
  until 2026-07-17 and remain untouched.

## Implemented In P2-1

- Migrations 10 and 11 add immutable `import_provenance`, append-only
  provenance/event associations, optional first-provenance event links, a
  rebuildable source-qualified `execution_id`, deterministic backfill and
  measured search/timeline/gap indexes. Duplicate/no-op artifacts can advance
  their own watermark without rewriting the original event fact.
- UniRouter normalized event IDs are source/adapter-qualified outside the
  accepted legacy default pair, preventing identical source refs from colliding
  across sources while preserving legacy duplicate/no-op behavior.
- Import persistence now has a single deployment writer lock, maximum 10,000
  events, maximum 256 KiB normalized event size and explicit proof/assurance
  validation. Snapshot intake is bounded at 64 MiB, 1,000 request IDs, 10,000
  normalized events and 10,000 capture gaps.
- Snapshot validation, event mapping, event hashing and artifact provenance now
  share one bounded artifact read; persistence does not reopen a possibly
  replaced path, and Gate A discovery detects an intervening file change.
- The six internal PRD2 read paths implement exact/prefix request search, time,
  source, outcome and gap filters; exact counts; keyset cursors; execution,
  timeline, selected-event, gap and source detail; and stable problem responses.
- Timeline order is effective time, ingestion time and event UUID. A parent is
  emitted only for one unambiguous, explicitly declared `parent_span_id` in the
  same execution; chronological proximity never creates causality.
- Event detail is an allowlisted typed projection and never returns
  `raw_source_excerpt`. Unlinked or mixed-class evidence returns
  `unclassified` rather than an invented real/synthetic claim.
- Sidecar evidence is `stale` absent a live freshness assertion; explicit gaps
  force `gap`; gap-bearing snapshots set `timeline_complete=false`.
- API requests have an 8 KiB URI limit, 20 request/s rate with burst 40,
  concurrency 32 and a three-second deadline. The PostgreSQL pool is capped at
  10 open and five idle connections.

## Local Verification And Diagnostic Evidence

Owner self-verification on 2026-07-13 is recorded in
[P2-1 local backend and 1x diagnostic](../evidence/prd2/p2-1-local-backend-and-1x-diagnostic-2026-07-13.md).
Key results before final branch regression are:

- fresh migration 0 through 11 in 0.77 seconds and provenance-linked
  import/readback: pass;
- same request ID across two sources remains two executions: pass;
- fixed-snapshot search/timeline paging has no observed duplicate: pass;
- reversed-clock declared parent resolves while a chronological sibling gains
  no false parent: pass;
- error, rate, concurrency, size/count and no-raw boundary tests: pass;
- diagnostic data loaded locally: 100,000 executions, 1,000,000 events and
  200,000 gaps;
- after replacing the generic-plan-prone optional predicate with persisted
  execution identity and fixed-shape SQL, then adding the database-visibility
  fence and append-only provenance coverage, 20 warm sequential searches
  returned 200 with p50 `192.124 ms`, p95 `247.093 ms`, max `247.465 ms`;
- corresponding detail p95 `4.183 ms`, timeline p95 `3.615 ms` and paced gap
  p95 `4.219 ms`; an unpaced series crossing the shared rate budget returned
  bounded 429; and
- on the local PostgreSQL 18 diagnostic database, migration-10 backfill took
  `80.469 s`, constraint validation `47.002 s` and index creation `4.691 s`.
  Migration 11's provenance-association backfill/index pass took approximately
  24 seconds on the same legacy dataset. These are maintenance-window inputs,
  not PostgreSQL 16 VM qualification or a no-downtime migration claim.

Local, CI, PostgreSQL 16 VM qualification, Target, External integration, field
usability and Production remain separate. All Codex-run checks are owner
self-verification, not independent acceptance.

GitHub delivery completed before P2-2/P2-3 began:

- local branch commit
  `26b2c95f69ec7c0c3a19d77d8f9a2be815eed6d9`;
- PR [#30](https://github.com/foigent/x-insight-private-history/pull/30): squash-merged after
  `Frontend Tests`, `PostgreSQL 16 Backend` and
  `Linux Go 1.25.11 Release` all passed; and
- squash merge commit
  `b754100a4eee9024707fe9c189c71ee579966a04`, reconciled on local `main` and
  `origin/main` before the UI branch began.

# Prior Execution Record: XI-PRD2-P2-0-FOUNDATION-20260713

Work ID: `XI-PRD2-P2-0-FOUNDATION-20260713`
Owner: Codex
Risk: R2 API/UI contract and toolchain; R3 schema, bounded ingestion and
cross-project data boundary
Status: Complete; Local and CI verified; primary PR merged
Base: `main` at `76f3f0ebe9e3a83598fe17af5067fe9ccd003789`
Branch: `codex/prd2-p2-0-foundation`

## Active Scope

1. Reconcile the accepted PRD2 implementation-kickoff defaults with the Phase 2
   priority, Batch 0 register, field plan, manual plan, runtime-fact record and
   decision backlog without rewriting the accepted Phase 1 boundary.
2. Freeze the P2-0 execution identity, search/timeline/gap API, cursor/error,
   freshness/revision, resource-limit, frontend toolchain and same-origin
   packaging contracts.
3. Add the minimal React/TypeScript build/test/Go-embedding skeleton needed by
   later P2-2/P2-3 work; no investigation feature is claimed complete in this
   foundation batch.
4. Publish an X-Insight-owned completed-execution pull-feed candidate and a
   cross-project review request. The live UniRouter endpoint remains disabled
   and unimplemented until UniRouter accepts a versioned contract.
5. Keep the existing operator-mediated sidecar import as the only currently
   implemented integration and as the PRD2 fallback path.

## Authorization And Stops

- The User accepted the bounded PRD2 defaults and authorized implementation on
  2026-07-13, instructing Codex to continue until a real product decision or
  UniRouter cooperation is required.
- No UniRouter repository, runtime, target, credential, retained artifact or
  Phase 1 evidence is modified in this batch.
- Stop the live pull-integration path before assuming an endpoint, auth method,
  cursor, revision or activation behavior that UniRouter has not accepted.
- The two retained Gate A JSON snapshots are not due for controlled deletion
  until 2026-07-17 and remain untouched.

## Planned Verification

- focused frontend contract/component/build checks;
- Go embed/server regression, formatting, vet, full and race tests;
- fixture conformance and PostgreSQL migration/read smoke where applicable;
- documentation link/status/contract reconciliation;
- dependency/license/audit and deterministic asset checks; and
- GitHub PR/CI before merge.

All Codex-run checks are owner self-verification, not independent acceptance.
Local, CI, Target, External integration, field usability and Production remain
separate states.

## Implemented In This Foundation Batch

- Reconciled B0-D23 through B0-D31 across the Phase 2 priority, Batch 0
  register, PRD2-M1 plan, manual plan, runtime status and decision backlog.
- Added the exact P2-0 engineering contract and six-path OpenAPI 3.1 contract.
- Pinned Node `22.23.1`, npm `10.9.8`, React `19.2.7`, TypeScript `7.0.2`,
  Vite `8.1.4` and test/validation dependencies in an exact lockfile.
- Added a `zh-CN` React foundation shell built into committed deterministic
  assets and embedded in the Go server under same-origin product routes.
- Made server listen-address validation loopback-only, added bounded server
  timeouts/header size and applied CSP/permissions/no-sniff/frame/referrer
  headers to UI.
- Added the `xe1_` source-qualified execution-ID primitive, strict versioned
  cursor primitive and stable `application/problem+json` error primitive;
  invalid UTF-8 and Unicode control characters are rejected at identity/feed
  boundaries.
- Removed raw store error strings from legacy read-error responses.
- Added a strict pull-feed consumer that supports only `disabled` and
  deterministic `mock`; any live mode fails with contract-not-accepted.
- Published a shared decision-proposed feed candidate and review request. No
  UniRouter repository/runtime change or accepted decision is claimed.

Search/detail/timeline/gap collection APIs, DB projections/migrations, feature
UI, real pull transport, upgrade tooling and the full Chinese manual were not
part of P2-0. P2-0 closed as the contract/toolchain foundation; later PRD2
batches own those deliverables.

## Local Verification Result

Owner self-verification on 2026-07-13:

- exact Node/npm TypeScript check: pass;
- OpenAPI 3.1 validation with six required paths: pass;
- Vitest component tests: 5/5 pass;
- npm audit: zero known vulnerabilities at audit time;
- locked dependency license allowlist: 177 package entries pass;
- two consecutive Vite builds produced identical three-file hashes;
- focused Go tests for web UI, API, execution ID, cursor and mock pull: pass;
- `go vet ./...`: pass;
- `go test -count=1 ./...`: pass;
- `go test -race -count=1 ./...`: pass;
- fixture conformance: 11/11 pass;
- dirty-tree throwaway Linux release build/verification: pass; it is not a
  trusted commit-addressed release artifact and its hash is not promoted;
- relative Markdown links across 88 files: pass;
- GitHub Actions YAML parse and `git diff --check`: pass; and
- real-browser Playwright desktop/mobile inspection: correct `zh-CN` semantic
  structure, responsive layout, route isolation and zero console errors or
  warnings. The browser pass found a missing favicon request; the shell was
  corrected and rechecked.

No PostgreSQL migration was added in this batch. Target, External integration,
field usability and Production were not run. The shared feed is `Decision
proposed`, not accepted. The two Phase 1 temporary snapshots remain untouched
before 2026-07-17.

GitHub delivery:

- branch `codex/prd2-p2-0-foundation`, head commit
  `4cc7410` before squash;
- PR [#29](https://github.com/foigent/x-insight-private-history/pull/29): squash-merged;
- GitHub Actions `Frontend Tests`, `Backend Tests` and `Linux Release Artifact`
  all passed; and
- squash merge commit
  `4b6dca44a0947c22bd4a6059a0c96fbb475c4da8`, reconciled on local `main` and
  `origin/main` before P2-1 began.

# Prior Execution Record: XI-PRD2-M1-FIELD-CLARIFICATIONS-20260713

Work ID: `XI-PRD2-M1-FIELD-CLARIFICATIONS-20260713`
Owner: Codex
Risk: R1 PRD2 decision clarification and status synchronization; docs only
Status: Complete; Local and CI verified; primary PR merged
Base: `main` at `65812c7b3861318a72e54a2977cf2cdd28557f19`
Branch: `codex/prd2-field-clarifications`

## Active Scope

1. Record the User's acceptance of the exact PRD2-M1 implementation/release
   plan and authorization to begin P2-0.
2. Explain the joint UniRouter/X-Insight field window without creating a release
   dependency or automatic integration claim.
3. Explain and bound the field OS/PostgreSQL/air-gap and fixture/sanitized-data
   choices inside PRD2 only.
4. Record the actual field group: user/customer IT plus the User (`N=2`), with
   Codex as facilitator/recorder rather than a human participant or independent
   acceptance reviewer.

## Decision Result

- Exact PRD2-M1 plan: accepted 2026-07-13; P2-0 authorized; implementation not
  started by this documentation update.
- Joint field window: the same planned operator/customer test period as
  UniRouter, with independent `XI-R` and X-Insight-proposed `UR-E` gates and
  manual sidecar evidence; the latter was not an accepted UniRouter obligation,
  and UniRouter does not wait for X-Insight.
- Recommended deployment default pending customer-IT confirmation: Ubuntu
  22.04 x86_64; systemd X-Insight; dedicated local pinned PostgreSQL 16
  Docker/Compose; loopback-only; approved private artifact upload; not a full
  air-gap bundle.
- Recommended evidence sequence: fixture-only smoke first, then, only after a
  separate counterpart response, joint decision and data/custody authorization,
  one approved sanitized artifact under the proposed `UR-E` gate in a dedicated
  encrypted trial database.
- Recommended retention input pending User confirmation: field session plus
  seven calendar days, followed by explicit custodian handoff or controlled
  retirement; no automatic or bulk file deletion.
- Remaining field inputs are only the desired joint period, customer-IT host
  confirmation and retention confirmation. No multi-user, managed-PostgreSQL,
  full-air-gap, continuous-transport or other PRD2 expansion is introduced.

## Verification And Delivery

Local verification on 2026-07-13:

- `go vet ./...`: pass;
- `go test -count=1 ./...`: pass;
- `go test -race -count=1 ./...`: pass;
- `go run ./cmd/xinsight-importer fixtures run`: pass, 11/11 fixtures;
- `git diff --check`: pass;
- relative Markdown link check: pass;
- acceptance/status reconciliation grep: pass; and
- sensitive credential-format scan: pass.

GitHub delivery:

- primary branch `codex/prd2-field-clarifications`; head commit
  `03013ca60ebe2013ce5f6cabf644cef4dd3a1d56`;
- PR [#27](https://github.com/foigent/x-insight-private-history/pull/27): squash-merged;
- GitHub Actions run
  [29243306540](https://github.com/foigent/x-insight-private-history/actions/runs/29243306540):
  pass; `Backend Tests` completed in 55 seconds and `Linux Release Artifact`
  completed in 49 seconds; and
- squash merge commit `12936a267af8ef17149ac7ee05cb3826ac86e09f`;
  local `main` and `origin/main` were reconciled to that commit before this
  status-only update.

Owner tests and reconciliation are self-verification, not independent
acceptance. Target, External integration, field usability and Production were
not run.

This batch changes X-Insight documentation only. No product code,
UniRouter/shared/X-IAM file, target, external evidence or retained artifact is
modified or deleted. The two Phase 1 temporary snapshot artifacts remain
untouched because their controlled-retirement date is 2026-07-17.

# Prior Execution Record: XI-PRD2-M1-FIELD-PLAN-20260713

Work ID: `XI-PRD2-M1-FIELD-PLAN-20260713`
Owner: Codex
Risk: R2 product, release and field-validation planning; docs only
Status: Complete; Local and CI verified; primary PR merged
Base: `main` at `a8c25167202d0dd9180d9437f44dcdb25fca48fc`
Branch: `codex/prd2-field-validation-plan`

## Active Scope

1. Record the User's acceptance of B0-D09 through B0-D22 boundary defaults
   without converting unresolved parameters or unimplemented runtimes into
   completed facts.
2. Define PRD2-M1 so X-Insight can field-test a complete initial investigation
   UI beside UniRouter using the current controlled sidecar boundary.
3. Define a simple but recoverable single-node user upgrade path.
4. Answer whether a Chinese product manual exists and define its release-bound
   implementation plan.
5. Keep all changes in X-Insight documentation; do not modify UniRouter, X-IAM
   or the shared cooperation workspace.

## Boundaries And Evidence Plan

- This batch plans implementation; it does not add product code, migrations,
  frontend dependencies, release tooling or runtime integration.
- All accepted product capabilities are in PRD2-M1: search, execution detail/
  timeline, selected-node detail, source/provenance and capture gaps.
- Current integration remains loopback-only, metadata-only and operator-
  mediated sidecar. No automatic transport, deep link or Production claim is
  authorized.
- D09-D22 acceptance freezes boundary direction only. Exact SLOs/limits, action
  classes, spool behavior, privacy/versioned contracts and Tool Gateway build/
  reuse/defer remain unresolved.
- The current Phase 1 release primitives are not described as an implemented
  customer upgrader. The current repository has no complete Chinese product
  manual.
- Three agents supplied read-only specialist findings. Codex is the only
  writer and execution owner; owner tests remain self-verification.
- No deletion is authorized. The two Gate A snapshots are not due before
  2026-07-17 and remain untouched.

## Planned Verification

- Reconcile accepted/proposed/implemented wording across the Batch 0 register,
  Phase 2 priority, boundary/innovation records, README and operations index.
- Verify relative Markdown links and final diff hygiene.
- Run Go vet/tests/race, fixture conformance and sensitive-pattern scans as
  proportional repository regression.
- Require GitHub CI before merge and record Local, CI, VM qualification,
  Target, External integration, field-pilot metrics and Production separately.

## Result And Decision Status

- Recorded B0-D09 through B0-D22 as accepted boundary defaults while preserving
  unresolved numeric SLO/limit, action-class, spool, privacy/semantic and Tool
  Gateway implementation choices. No dependent runtime is claimed.
- Prepared/proposed PRD2-M1 as a gate-driven initial field investigation
  release with all accepted search/detail/timeline/node/source/provenance/gap UI
  capabilities. Implementation still requires explicit User acceptance/
  authorization of this exact plan.
- Selected TypeScript + React inside the accepted same-origin Go-embedded
  package; exact dependency/tool versions remain a first-code freeze.
- Defined a minimal import-provenance/freshness/assurance fact source and fixed
  sidecar-not-live wording so the UI cannot invent a real/synthetic or current-
  state claim from absent schema facts.
- Kept the existing target fixture/synthetic-only. Approved real sanitized
  metadata requires a dedicated newly qualified database/target with encrypted
  data/backups, custody, retention, restore, trial exit and cleanup evidence.
- Defined the proposed root-only `xinsightctl` install/adopt/preflight/upgrade/
  rollback/restore boundary: exact signed legacy source, shared writer lock,
  durable crash-recovery journal, forward-compatible migration, backup and
  restore-to-new-database. This is a design, not an implemented upgrader.
- Confirmed there is no complete Chinese product manual. Defined release-bound
  Markdown/offline HTML, legacy-to-first-SemVer task proof, blind usability,
  authenticity, reproducibility, air-gap separation and lifecycle gates.
- Split readiness into Local, CI, VM qualification, Target, External
  integration, field-pilot metric and Production. Current unmerged UniRouter
  work is excluded; a fresh external claim requires a separately accepted
  cross-project decision and the exact artifact it authorizes. `UR-E` is only
  X-Insight's proposed label until that governance chain exists.
- No new verified Phase 1 P0/P1 was found. Conditional PRD2 field-release P0
  gates do not reopen Phase 1.

## Local Verification

- Current X-Insight base/main/origin-main at start:
  `a8c25167202d0dd9180d9437f44dcdb25fca48fc`; no open PR existed.
- Relative Markdown link check across the repository: PASS.
- Accepted/proposed/implemented and React/status reconciliation grep: PASS.
- Sensitive credential-format scan over changed/new records: PASS.
- `git diff --check`: PASS.
- `go vet ./...`: PASS.
- `go test -count=1 ./...`: PASS.
- `go test -race -count=1 ./...`: PASS.
- `go run ./cmd/xinsight-importer fixtures run`: PASS; 11 passed, 0 failed.
- Peirce, Avicenna and Gibbs performed read-only challenge reviews. Their
  findings drove corrections to upgrade recovery, data custody, manual gates,
  proof/freshness assurance and decision status. They did not write the
  repository and their reviews are not described as independent acceptance.
- Owner tests and reconciliation are self-verification.

## GitHub And Runtime Status

- Primary branch: `codex/prd2-field-validation-plan`; head commit
  `222150cd79d4f35f998290ed94a708edc992863b`.
- PR [#25](https://github.com/foigent/x-insight-private-history/pull/25): merged after both
  required jobs passed.
- GitHub Actions run
  [29242073651](https://github.com/foigent/x-insight-private-history/actions/runs/29242073651):
  PASS. `Backend Tests` completed in 49 seconds and `Linux Release Artifact`
  completed in 47 seconds.
- Squash merge commit:
  `a0d7d971701968ab3897d20ef2d32fb84395fabb`. Local `main` and
  `origin/main` were reconciled to that commit before this status-only update.
- UniRouter `HEAD`, local `main` and `origin/main` all resolve to
  `f9925779f1da7681b07282be5d500773a687d3b9`, but its checked-out
  `codex/prd6-stream-policy-termination` worktree contains another owner's
  uncommitted code/docs/migrations. It was read-only and excluded from current
  contract truth; no UniRouter/shared/X-IAM file was modified.
- No snapshot, release, backup, evidence, database or other file was deleted.
  The two Gate A temporary artifacts are not due before 2026-07-17.
- Target, External integration and Production were not run. Field usability is
  only a proposed pilot metric and was not run.

# Prior Execution Record: XI-PHASE2-BATCH0-BOUNDARY-INNOVATION-20260713

Work ID: `XI-PHASE2-BATCH0-BOUNDARY-INNOVATION-20260713`
Owner: Codex
Risk: R2 product and cross-project architecture decision input; docs only
Status: Complete; Local and CI verified; merged
Base: `main` at `b287874e544fa96ff0af9751ea6dda57d05e9cf6`
Branch: `codex/phase2-batch0-boundary-innovation`

## Active Scope

1. Record the User's acceptance of B0-D01 through B0-D08 while preserving the
   distinction between an accepted decision and implemented runtime behavior.
2. Explain B0-D02 through B0-D05 and the prior decision questions 2 through 5,
   including rationale, benefits, costs and competitor versus project-specific
   source basis.
3. Recommend the logical/deployment boundaries for a future X-IAM and Tool
   Gateway, including current-permission checks and cache enable/read/invalidate
   authority.
4. Define a hard latency/compute/lifecycle decision matrix for UniRouter,
   X-IAM, Tool Gateway and X-Insight.
5. Design X-Insight-owned semantic anti-collusion, shadow-evaluation feedback
   and additional killer-feature candidates with sequence, safety, evidence and
   decision gates.

## Boundaries And Evidence Plan

- This batch changes X-Insight documentation only. It does not modify
  UniRouter, the non-Git X-IAM corpus, or shared cross-project decision files.
- B0-D01 through B0-D08 become accepted product decisions, not implemented
  features. B0-D09 through B0-D12 and the new B0-D13 through B0-D22 remain open
  unless the User separately accepts them.
- X-IAM and Tool Gateway are architecture candidates, not current deployable
  X-Insight components. No service, policy engine, cache, token issuer or tool
  runtime is created.
- No automatic route activation, shadow provider call, exporter/outbox,
  receipt transport, durable job, raw payload capture, policy enforcement,
  target deployment or production claim is authorized.
- Semantic innovation must remain compatible with the current metadata-only
  boundary or explicitly identify the future tenant opt-in/privacy contract it
  would require.
- Stable UniRouter `main` is evidence input. Another owner's dirty
  `codex/prd6-guardrail-kernel` worktree must remain untouched.
- Three agents may provide read-only challenge findings. Codex remains the only
  writer and execution owner; owner checks are self-verification.
- No batch or recursive deletion is authorized. The two Gate A snapshot
  artifacts remain untouched before their 2026-07-17 deadline.

## Planned Verification

- Reconcile all current/accepted/proposed/implemented wording against Phase 2
  priority, Batch 0 register, runtime capability status, current X-Insight code,
  stable UniRouter main and the X-IAM source review.
- Attribute competitive/standards claims to official or primary sources and
  keep architectural inference explicit.
- Verify local Markdown links, run broad repository regression appropriate to
  R2, inspect the final diff and require GitHub CI before merge.

## Result And Decision Status

- Recorded B0-D01 through B0-D08 as accepted product decisions. No API, UI,
  schema projection, framework dependency or runtime feature is claimed as
  implemented. React remains the candidate component framework until the
  engineering dependency freeze.
- Kept B0-D09 through B0-D12 open and added refined B0-D13 through B0-D22 as
  open decision inputs. These later rows gate only their dependent expansion;
  they do not all block the bounded D01-D08 search/timeline slice.
- Preserved the detailed rationale, benefits, costs and source basis for
  D02-D05. Also answered the prior architecture questions 2-5 covering root of
  trust, Agent identity units, revocation semantics/SLO and delegation custody.
- Recommended deferring X-IAM. If later justified, it is an independent
  identity/policy control plane that may share packaging/infrastructure but
  isolates process, database, keys and authorization authority; shared
  infrastructure failure remains explicit.
- Placed enforcement at the true sink/action owner. A Tool Gateway is one
  optional sink-local PEP deployment, not part of X-IAM or X-Insight. The local
  PDP computes contextual authorization and the PEP enforces it.
- Defined cache enable/read/invalidate/cleanup authority. Epoch invalidation
  takes effect only after PEP observation within an accepted SLO; physical
  deletion is asynchronous. A future X-Insight authorization preview is
  non-binding and cannot clear another system's cache.
- Added the latency/compute/lifecycle Golden Laws and ownership split for
  redaction, claim assurance, token/cost alignment and suspicious coordination.
- Designed Coalition Lens A0/A1/A2, Shadow Eval-Back, Policy/Authority Digital
  Twin, Policy Proof Chain, Authority Drift Radar and the Unprovable Action Map
  with sequences, failure controls, acceptance evidence and explicit privacy/
  authority contracts.
- Current P0: none under the accepted loopback, sanitized, manual-sidecar
  posture. Phase 1 remains closed. The new findings are open decision gates or
  conditional future release gates, not deployed vulnerabilities.

## Authorization, Rollback, Cleanup And Gaps

- The User authorized this analysis, project-document update and the standing
  X-Insight branch/commit/PR/CI/merge flow. This record does not authorize
  product code, target deployment, X-IAM/Tool Gateway creation, external
  transport, raw/semantic evidence custody, paid shadow calls or automatic
  route/policy activation.
- Rollback is documentation-only: revert the eventual X-Insight documentation
  commit. No migration, data, runtime or target rollback exists for this batch.
- No deletion or retention cleanup was performed. The two Gate A temporary
  snapshots are not due before 2026-07-17 and remain untouched.
- UniRouter remained on another owner's dirty
  `codex/prd6-guardrail-kernel` worktree. Only committed `main` at
  `fbfde8816a6a6676cd0c65a489659d246c5541fd` was used as runtime fact; no
  UniRouter, X-IAM or shared-cooperation file was modified.
- Local, CI, Target, external-integration and Production states remain separate.
  Target, external integration and Production were not run or claimed.

## Local Verification

- Reconciliation against X-Insight `main` at
  `b287874e544fa96ff0af9751ea6dda57d05e9cf6`, current code/runtime status,
  stable UniRouter `main` and the X-IAM source review: PASS.
- Analysis validation: PASS after correcting arbitrary evidence counts,
  provider nondeterminism, causal overstatement, outdated competitor scope,
  PDP/PEP/entitlement conflation, epoch propagation, cache eligibility,
  mandatory-receipt custody and route-versus-policy approval authority.
- Relative Markdown link check across the nine changed/new records: PASS.
- Official/primary-source redirect check: PASS; all 29 unique referenced URLs
  returned HTTP 2xx/3xx after the current Microsoft Purview source was used.
- `go vet ./...`: PASS.
- `go test -count=1 ./...`: PASS.
- `go test -race -count=1 ./...`: PASS.
- `go run ./cmd/xinsight-importer fixtures run`: PASS; 11 passed, 0 failed.
- Secret-pattern scan over the new decision reports: PASS.
- `git diff --check`: PASS before GitHub integration.
- Owner tests are self-verification. Peirce, Avicenna and Gibbs performed
  read-only challenge reviews and confirmed no remaining blocker after the
  corrections; they did not write the repository and this is not described as
  independent acceptance.

## GitHub And Target State

- GitHub CLI `2.73.0`: authenticated to `foigent/x-insight`; default branch
  `main`; no open PR existed before this batch's publication.
- Branch: `codex/phase2-batch0-boundary-innovation`.
- PR [#23](https://github.com/foigent/x-insight-private-history/pull/23): merged. Head commit:
  `b3eaec2fdb16ee6c0e5f512aa012be09fd46f02b`.
- GitHub Actions run
  [29237871021](https://github.com/foigent/x-insight-private-history/actions/runs/29237871021):
  PASS. `Backend Tests` and `Linux Release Artifact` completed successfully.
- Squash merge commit:
  `62696244ae3d996c68af0a0afd1aab23916a86ac`. Local `main` and
  `origin/main` were reconciled to that commit before this status-only update.
- Target deployment: not in scope and not run.
- External integration: not run; reviewed sidecar evidence is fact input only.
- Production: not in scope; not verified.

# Prior Execution Record: XI-PHASE2-BATCH0-ARCH-AUDIT-20260713

Work ID: `XI-PHASE2-BATCH0-ARCH-AUDIT-20260713`
Owner: Codex
Risk: R2 product and cross-project architecture decision input; docs only
Status: Complete; Local and CI verified; merged
Base: `main` at `e43add8e0184ba1eb059484053bc5e9d9f4cf6ed`
Branch: `codex/phase2-batch0-architecture-audit`

## Active Scope

1. Explain the rationale, trade-offs, and competitor versus project-specific
   sources behind the seven open Phase 2 product recommendations.
2. Preserve the first competitive-alignment result as a discussion-ready,
   evidence-attributed project document with proposed, deferred, and
   decision-gated work kept separate.
3. Audit the UniRouter / X-IAM / X-Insight architecture for anti-patterns and
   100x-scale failure modes, including Agent/Subagent identity and authorization
   amplification.
4. Convert the audit into bounded Batch 0 decision inputs, staged mitigation
   recommendations, acceptance evidence, and explicit questions for the User.

## Boundaries And Evidence Plan

- This batch changes X-Insight documentation only. It does not change
  UniRouter, the non-Git X-IAM research corpus, or shared cross-project decision
  files.
- UniRouter currently has another owner's uncommitted PRD6 work; that work is
  not treated as merged architecture truth and must not be modified here.
- No product feature, schema, migration, runtime integration, deployment,
  retention cleanup, or Phase 1 gate is changed by this audit.
- Findings must distinguish current implementation facts, accepted boundaries,
  architectural inference, and 100x stress hypotheses. Competitor claims use
  reviewed official sources.
- Independent read-only checks may challenge the owner analysis. The owner's
  tests and document checks remain self-verification, not independent review.
- No batch or recursive deletion is authorized. The two Gate A snapshot
  artifacts remain untouched before their 2026-07-17 deadline.

## Result And Severity

- Preserved the competitive analysis and explicit recommendation 2–8 rationale
  in a dated discussion record, including benefits, costs, competitor versus
  project source basis, revisit triggers, and MVP/advanced/long-tail priority.
- Completed a four-quadrant audit of the verified current boundary and
  hypothetical 100x/Agent-fanout failure modes. X-IAM is recorded as research,
  not an implemented third runtime.
- Converted both reports into a proposed Batch 0 decision register with scope,
  non-goals, API/UI/schema and cross-product boundaries, security/data/
  compatibility risks, acceptance evidence, severity, and staged Batch 1
  implementation order.
- Current Phase 1 P0/P1: none; Phase 1 remains closed.
- Current Phase 2 P0: none under the accepted loopback, sanitized,
  operator-mediated sidecar posture.
- Conditional P0 before a future 100x/HA/Production claim: overload isolation,
  authority/billing invariants, no synchronous X-IAM/X-Insight dependency,
  mandatory evidence survivability, and selected DB/Redis failover/restore.
- P1 for proposed Batch 1: bounded execution/cursor/error identity,
  importer/DB/HTTP limits, chronology-versus-causality semantics, proof-class
  separation, and a reproducible workload/SLO/failure contract.

## Authorization, Rollback, Cleanup And Gaps

- The User authorized this analysis, project-document update, branch, commit,
  PR, required GitHub CI, merge, and status reconciliation. No X-Insight feature
  implementation or target deployment is authorized by this record.
- Rollback is documentation-only: revert the eventual X-Insight documentation
  commit. No migration, data, runtime or target rollback exists for this batch.
- No cleanup was performed. The two Gate A temporary snapshot artifacts are not
  due before 2026-07-17 and remain untouched.
- The reports do not prove a 100x workload, HA, external integration, Target or
  Production status. The twelve Batch 0 decision rows remain proposed until the
  User accepts or revises them.
- UniRouter remained on another owner's dirty
  `codex/prd6-guardrail-kernel` worktree. Only stable `main` at
  `fbfde8816a6a6676cd0c65a489659d246c5541fd` was used as implemented truth; no
  UniRouter or shared decision file was modified.

## Local Verification

- Reconciliation against the Phase 1 closeout, Phase 2 priority,
  runtime-capability status, UI candidates, current X-Insight code, stable
  UniRouter main and X-IAM source: PASS.
- Manual source reconciliation: PASS. Every recommended item has rationale,
  benefits, costs, source basis, status, dependencies, revisit conditions and
  executable acceptance evidence.
- Relative Markdown link existence check across all changed routing/report
  files: PASS; no broken local path found.
- Official-source link check with redirect-following `curl`: PASS; all 17
  referenced competitor/distributed-system source URLs returned HTTP 200 on
  2026-07-13.
- `go vet ./...`: PASS.
- `go test -count=1 ./...`: PASS.
- `go test -race -count=1 ./...`: PASS.
- `go run ./cmd/xinsight-importer fixtures run`: PASS; 11 passed, 0 failed.
- `git diff --check`: PASS before GitHub integration.
- Local owner checks are self-verification. Peirce, Avicenna and Gibbs supplied
  read-only challenge findings over X-Insight, stable UniRouter main and X-IAM
  source respectively; they did not write the repository or provide a separate
  acceptance claim.

## GitHub And Target State

- GitHub CLI `2.73.0`: authenticated to `foigent/x-insight`; default branch
  `main`; no open PR existed before this batch's integration.
- PR: `#21`, head commit
  `07afd89b2841cb9ed70d0bdccbbfb86afcc4dbbf`.
- Required GitHub CI run `29233545788`: PASS. `Backend Tests` and
  `Linux Release Artifact` both completed successfully.
- Squash merge: `ffdb0238316d451f294cea506c111fe5ca748954`.
- Post-merge reconciliation: local `main`, `origin/main` and GitHub PR merge
  commit all matched `ffdb0238316d451f294cea506c111fe5ca748954`; the worktree
  was clean before this status-only record update.
- Target deployment: not in scope and not run.
- External integration: not run; existing accepted sidecar evidence is source
  input only.
- Production: not in scope; not verified.

# Prior Execution Record: XI-PHASE1-TARGET-CLOSEOUT-20260710

Work ID: `XI-PHASE1-TARGET-CLOSEOUT-20260710`
Owner: Codex
Risk: R4 authorized non-production target correction, deployment, and acceptance
Status: Complete; Phase 1 and named non-production target verified
Base: `main` at `b9e5811cc53a5cb06614956e66a1c54756617f0a`
Branch: `codex/phase1-final-reconciliation`

## Completed Scope

1. Applied the User's explicit owner override; Atlas was not a required
   handoff.
2. Reconfirmed the named X-Insight ECS identity, private management path,
   zero-ingress security group, artifact identity, and rollback conditions.
3. Corrected the environment-file, host-firewall, and effective `noatime`
   discrepancies from the first deployment.
4. Deployed the exact main-CI artifact as an immutable release while preserving
   the prior release pointer.
5. Ran the complete target acceptance matrix, retained root-owned logs,
   reconciled all Phase 1 gates, and closed with no unresolved P0/P1 or target
   discrepancy.

## Authorization And Ownership

On 2026-07-10 the User explicitly directed Codex to stop delegating subsequent
work to Atlas and to perform the remaining checks and operations itself. This
supersedes the operator assignment in the prepared Atlas request without
expanding its safety boundary: the target remains non-production and the
prohibitions on public ingress, production/customer data, destructive storage
or database actions, target GitHub credentials, raw payload capture, and paid
external calls remain in force.

## Audit Decision

- P0: none found.
- P1: the missing CI-built release artifact was fixed and deployed.
- P1 follow-up: target preflight found that CI selected floating Go `1.25`
  rather than an exact patch. PR #16 merged as `382279f...`, pinning Go
  `1.25.11` for backend/release jobs and making the release verifier enforce
  that identity.
- P1 follow-up: target activation found that the archive omitted directory
  entries. PR #17 merged as `b9e5811...`, packaging and verifying deterministic
  `0755` directory metadata.
- P1 follow-up: the source-build unit's root-binary path did not match the
  release `bin/` layout. A systemd drop-in now targets
  `current/bin/xinsight-server`.
- P1: the Codex-owned final target pass and independent reconciliation are
  complete. No unresolved P1 remains.
- P2: migration advisory locking, import size limits, durable jobs,
  compatibility policy, auth/tenant controls, retention/deletion/audit,
  receipt authenticity, signed releases, HA/DR, and broader adapters remain
  Phase 2 or production-hardening work, not Phase 1 blockers.

Audit record:
`docs/architecture/phase-1-closeout-audit-2026-07-10.md`.

## Current External State

- The named non-production ECS is `Target verified` on exact CI artifact
  commit `b9e5811cc53a5cb06614956e66a1c54756617f0a`.
- Cloud Assistant is active. One dedicated security group has zero rules; UFW
  is active with default-deny inbound/default-allow outbound. X-Insight and
  PostgreSQL expose only their loopback listeners.
- The environment file is `root:root` mode `0600`. The 50 GiB ext4 data
  disk uses an effective `noatime` mount and is approximately 1% full.
- PostgreSQL 16 is healthy on its reconciled immutable image digest, persists
  under the dedicated data disk, and contains nine migration versions.
- The exact release passed 1,709-part transfer verification, archive/manifest
  and 51 component hashes, toolchain/OS/architecture metadata, directory modes,
  11 fixture checks, two zero-change migration runs, activation, and restart.
- A new approved synthetic fixture inserted 2 events and 1 capture gap; its
  rerun produced 2 duplicate/no-op outcomes. Existing authorization-denied
  readback remains 2 events/2 gaps.
- The first activation failed on the old unit path; the second failed on
  `0700` directories. Both restored the prior symlink and readiness before the
  corrected release was activated. No failed release is active.
- The prior release and failed staging/release attempts are retained without
  bulk cleanup. Storage remains unencrypted, delete-with-instance, and without
  automated snapshots, so fixture/synthetic data is still the maximum scope.

## Verification

- Local release build:
  `XINSIGHT_RELEASE_ALLOW_DIRTY=1 scripts/build-release.sh dist/local2` PASS
  for throwaway dirty-tree validation. It produced the same archive and
  manifest SHA-256 as the earlier `dist/local` run.
- Local release verification:
  `scripts/verify-release.sh dist/local2/xinsight-release-linux-amd64-5c2e1ece59e521cae049a95dc21690ce6eac11c0.tar.gz`
  PASS; file count 52; required binaries, migrations, schemas, fixtures,
  manifest, and checksums present.
- Script syntax: `bash -n scripts/build-release.sh` PASS;
  `bash -n scripts/verify-release.sh` PASS.
- Go static/regression: `go vet ./...` PASS; `go test ./...` PASS;
  `go test -race -count=1 ./...` PASS.
- Fixture conformance: `go run ./cmd/xinsight-importer fixtures run` PASS,
  11 passed and 0 failed.
- Diff hygiene: `git diff --check` PASS.
- Local database-backed verification was not run because this workstation has
  no `XINSIGHT_POSTGRES_DSN` and no Docker binary. GitHub CI with PostgreSQL is
  still the required database-backed result.
- PR: `#14`, squash-merged as
  `7a99594ccac065ccce5882738f3b566daa10f8d1`.
- Main CI: GitHub Actions run `29102216455` PASS on
  `7a99594ccac065ccce5882738f3b566daa10f8d1`.
  `Backend Tests` PASS with PostgreSQL service, migrations, persistent snapshot
  import, and read API smoke. `Linux Release Artifact` PASS.
- GitHub Actions artifact wrapper:
  `xinsight-release-linux-amd64-7a99594ccac065ccce5882738f3b566daa10f8d1`,
  API artifact digest
  `sha256:1c67e1f8922e4304cea2a2be9b7f1dc5f56ebfbc65af90d02089ef7382732974`.
- Release tarball:
  `xinsight-release-linux-amd64-7a99594ccac065ccce5882738f3b566daa10f8d1.tar.gz`,
  SHA-256
  `sha256:34afdb9a2b13c5d216cf65281367292d026b7ed2f7b71fa20a477d3bde65a948`.
- Release manifest SHA-256:
  `sha256:36ee2a1e0bba67cb502f3f4c3333a78abaf2667ef150b5bab9b9c03af3f17115`.
- Downloaded Actions artifact zip contained exactly one release tarball; local
  `scripts/verify-release.sh` PASS on the downloaded tarball with file count
  52.
- Exact-toolchain follow-up: verifier syntax PASS; the existing artifact passes
  with expected Go `go1.25.11` and fails with expected Go `go1.25.12`.
- Follow-up regression: `go vet ./...`, `go test -count=1 ./...`,
  `go test -race -count=1 ./...`, and fixture conformance all PASS. Workflow
  YAML parses and `git diff --check` passes.
- PR #16 main CI run `29107272996` PASS on
  `382279ffffef13f520a735d31735f75c517ce0fb`; its exact-toolchain artifact
  passed local commit/toolchain/content verification before target transfer.
- Directory-metadata follow-up: two dirty-tree throwaway builds produced the
  same archive SHA-256; the new verifier passes both and rejects the prior
  file-only artifact with `archive directory entries mismatch`.
- Directory-metadata regression: build/verifier syntax, `go vet ./...`,
  `go test -count=1 ./...`, `go test -race -count=1 ./...`, fixture
  conformance, workflow YAML parse, and `git diff --check` all PASS.
- PR #17 merged as `b9e5811cc53a5cb06614956e66a1c54756617f0a`.
  Main CI run `29109235304` PASS with PostgreSQL and the Linux release job.
- Final Actions wrapper digest:
  `sha256:c1acdf6b4f5a755322f0ae75f70051dea584421684fecc66db37856394ddc8f2`.
- Final artifact:
  `xinsight-release-linux-amd64-b9e5811cc53a5cb06614956e66a1c54756617f0a.tar.gz`,
  SHA-256
  `sha256:3e3794338d690fe5b12de46600e17d455baeceab0b79300af260baf78d0346b1`.
- Final manifest SHA-256:
  `sha256:aaa53c2af46f59c328ff3271fa518a20bb0551fd7bdc02a0463ff925a43a6bc1`.
- Target log checksum manifest covers 12 root-owned logs and has SHA-256
  `37b9d81679c74ad0ac4e8b0d5e11d5f85319c4ea2f0f07289f09ed2615398690`.
- Independent final reconciliation re-read cloud zero-ingress state, active
  Cloud Assistant, exact current release, systemd drop-in, UFW, mount, database
  health, both readback paths, listeners, and the retained-log manifest: PASS.
- The shared runtime-contract response now records `Target verified` and keeps
  the fixture/synthetic-only and non-production limitations explicit.

## Next Execution

Merge this final documentation reconciliation and require the resulting main CI
to pass. No Phase 1 implementation or target action remains. Phase 2 scope must
be selected separately and must not inherit a production claim from this
fixture/synthetic-only target verification.

## Prior Completed Work

### XI-PHASE1-CLOSEOUT-HANDOFF-20260710

Work ID: `XI-PHASE1-CLOSEOUT-HANDOFF-20260710`
Owner: Codex
Risk: R2 architecture/status truth; reconciles an R4 non-production deployment
Status: merged as `5c2e1ec` / PR #13
Base: `origin/main` at `9172633e257f521682026442363869b26c81af5e`
Branch: `codex/phase1-closeout-handoff`

Prepared the Phase 1 closeout gate, runtime-contract capability status, Atlas
final target pass request, and new-task handoff after PR #13 merged.

### XI-ECS-DOC-GOVERNANCE-20260710

Work ID: `XI-ECS-DOC-GOVERNANCE-20260710`
Owner: Codex
Risk: R2 documentation truth; references an R4 external non-production deployment
Status: merged as `9172633` / PR #12
Base: `origin/main` at `4e1883503084a31cf685f924660a416d2871adfb`
Branch: `codex/xinsight-deployment-doc-governance`

Established the project-owned Alibaba Cloud single-node target runbook,
separated stable deployment requirements from operator communication, and
selected bounded chunked Cloud Assistant transfer for the source archive
without public ingress or target GitHub credentials.

### XI-UR-B1W-TARGET-RERUN-20260710

Work ID: `XI-UR-B1W-TARGET-RERUN-20260710`
Owner: Codex
Risk: R4 authorized non-production deployment and controlled external integration
Status: Target verified; external integration persisted/read back; repository evidence complete
Base: X-Insight `origin/main` at `5f0fa49`; UniRouter `origin/main` at
`e823abc`, containing Batch 1W `931400a`
Branch: `codex/unirouter-target-rerun`
Authorization: `UR-XI-GATE-A-20260710-02`

## Active Scope

1. Establish a local SSH alias and agent-backed key for the confirmed UniRouter
   Alibaba Cloud ECS development/test instance without recording credentials or
   network addresses in Git.
2. Deploy the latest CI-passing UniRouter `main` content containing Batch 1W by
   rebuilding only stable/canary backend images from a clean release directory.
3. Preserve the existing `.env`, database, Redis, monitoring, WAL, and immutable
   Gate A artifacts; record current image identities and provide image rollback.
4. Run exactly one synthetic `model_permission_denied` request after health
   checks. It must return `403`, echo the exact request ID, emit the expected
   metadata-only decision trace, and make no upstream provider call.
5. Disable temporary Gate A API keys 36 and 37 after the rerun and record only
   key IDs and disabled state.

## Authorization And Stops

- The User reassigned execution ownership to Codex and stated `同意相关授权` on
  2026-07-10. The executable boundary is recorded in the shared decision for
  `UR-XI-GATE-A-20260710-02`.
- Stop before deployment if the target cannot be confirmed as non-production,
  the current environment secrets cannot be preserved, the release commit lacks
  Batch 1W, or image rollback cannot be established.
- Stop before the controlled request if stable/canary health fails. Roll back to
  the recorded pre-deploy images and do not send the request.
- Paid MiMo/provider calls are not part of this authorization. The denied request
  must terminate inside UniRouter before relay.

## Access And Pre-Deploy Facts

- The `unirouter-dev` SSH alias and Keychain/agent identity are configured
  locally. Direct TCP/22 currently times out before authentication.
- Alibaba Cloud Assistant is active and supports an authorized management path.
- The target is running Ubuntu 22.04 with Docker 29.6.0 and Compose 5.2.0.
- Existing backend, canary, frontend, PostgreSQL, Redis, Nginx, Prometheus, and
  Grafana containers are running; backend readiness and frontend checks pass.
- The server deployment directory is a historical rsync working tree with many
  modified/untracked files and is not safe for direct `git pull` deployment.
- The root filesystem is 40 GB with approximately 22 GB free. No X-Insight
  runtime will be added to this instance in this batch.
- The current security group and container port publication are broader than
  least privilege. Network hardening is a separate change because applying it
  without a verified access path could lock out the environment.

## Deployment And Rollback Plan

- Clone/check out exact UniRouter commit `e823abc` into a new clean release
  directory and verify it contains `931400a`.
- Copy the existing `.env` into the release directory with mode `0600`; do not
  print or persist any secret values.
- Tag current stable/canary image IDs as rollback images.
- Build only `backend` and `backend-canary` with `RUNTIME_COMMIT=e823abc`, then
  recreate those two services under Compose project `unirouter`.
- Verify stable/canary readiness, deployed runtime metadata, existing API-key
  smoke, and no unexpected database migration. On failure, retag the recorded
  images and force-recreate stable/canary before proceeding no further.

## Actual Result

- Direct SSH remains blocked before authentication, so target work used Alibaba
  Cloud Assistant after environment identity and non-production purpose were
  confirmed.
- A clean cloud clone was unavailable because the repository is private and the
  host has no GitHub credential. Exact server backend inputs were therefore
  compared file-by-file with deployed baseline `b2ba892`; they matched. The two
  Batch 1W backend files and the complete target manifest then matched
  `e823abc` exactly.
- Cloud Docker compilation was stopped before service switch because the host
  could not reach the Go module proxy. A locally cross-compiled static Linux
  binary was encrypted in transit, verified as SHA-256
  `8a27eb8042623897d009ec7ac29fb9050cbdde73de1444e3641c43217a3b5157`,
  and placed into an image derived from the recorded pre-deploy backend image.
- Stable and canary now run the same new image with target runtime commit
  `e823abc293f996264763c6c1aca19d3d4d6e705f`. Both readiness checks pass. The
  schema remains `000018_prd5_structural_evidence_fields`; no migration ran.
- Rollback images are retained under timestamped stable/canary tags. No rollback
  was required.

## Controlled Rerun

- Authorization: `UR-XI-GATE-A-20260710-02`.
- Exact request ID: `gatea-model-denied-b1w-283194e67a0c3ff9`.
- Result: HTTP `403`; returned `X-Request-ID` matched.
- Source evidence: `auth.model_not_allowed`, stage `auth`, status `403`,
  `capture_gap=permission_denied`, `decision_kind=auth`,
  `decision_result=rejected`, and
  `decision_reason=auth_error.model_not_allowed` for API-key ID 37.
- Side-effect checks: zero call logs, zero billing freezes, zero transactions,
  and zero provider-stage events. No paid upstream request occurred.
- Temporary API-key IDs 36 and 37 are both confirmed `disabled`.

The raw key from the first run was not retained. The controlled rerun therefore
rotated only temporary key 37 to a process-local random credential, sent one
request, and disabled both temporary keys on every exit path. No credential was
written to Git, shared evidence, or command output.

## New Exchange And Receipt

- Exchange ID: `uxchg_8deae233291214154815c336c9278983`.
- Snapshot SHA-256:
  `3c0b13241eb369ca096115f420e73d1ae35c3e6ac4b794cb997f56cf09c7339c`.
- Approval manifest SHA-256:
  `8cc3d3b54bd8c0220747c0f19cf49cb6f34bcb2a68373a0074ad60dea153c0f4`.
- Receipt SHA-256:
  `16cbac6030f5f3e08f6df73593a5ef61c5395cde73e816d6e10f7f08ab81ea36`.
- Snapshot source: one trace row, zero call-log rows, and one generated
  `call_logs/not_available_from_source` gap.
- X-Insight mapping: two accepted events, zero rejected/quarantined; first
  persistence inserted two, second persistence returned two duplicate/no-op,
  zero idempotency conflict; readback returned two events and two gap records.
- Receipt status: `persisted` with warning `source_capture_gaps_present`.

The remaining call-log gap is expected for this pre-billing denial path. It does
not negate the now-observed auth decision. X-Insight ECS deployment remains
unverified and was not part of this cycle.

## Infrastructure Findings

- The ECS security group admits all TCP ports from the public internet, and
  Postgres, Redis, Prometheus, Grafana, backend, canary, and frontend bind to
  public interfaces. An unknown connection reached a temporary upload listener
  before the authorized client. This is direct exposure evidence, not a
  theoretical configuration concern.
- Do not change security-group rules until a verified management path is kept
  open. Remediation should use a new least-privilege security group, private VPC
  paths, and a monitored cutover.
- The target is below the deployment runbook's 50 GB disk recommendation and has
  limited free memory. Do not add X-Insight to this host.
- The live deployment directory is an rsync working tree, not a trustworthy Git
  checkout. Future releases need immutable artifacts and deployment manifests.

## Process Exception

During creation of a new isolated release staging directory, a pattern-based
command removed two copied `.DS_Store` files. No source, runtime, evidence, or
user data was affected, but this did not follow the repository rule requiring
one explicit deletion path at a time. It must not be repeated.

## Remaining Work

- Delete both temporary Gate A snapshot artifact copies by `2026-07-17` under
  the accepted retention decision. Inventory and verify each exact path/hash,
  delete one explicit path at a time, and retain sanitized contracts, receipts,
  hashes, and reconciliation evidence.
- Do not delete persisted normalized evidence or receipt records under this
  artifact-cleanup authorization; their lifecycle remains a later versioned
  retention/deletion contract.
- Phase 1 has no other open implementation or target-reconciliation work.

## Prior Completed Work

### XI-GATE-A-REAL-IMPORT-20260710

Work ID: `XI-GATE-A-REAL-IMPORT-20260710`
Owner: Codex task `019f3b3f-ffa8-7360-80eb-807d5641e287`
Risk: R3 evidence-redaction boundary plus R4 external integration evidence
Status: CI verified; merged; local external integration verified
Base: `origin/main` at `999c488`
Branch: `codex/gate-a-real-import`
Merged: `4a562a6` / PR #9

## Scope

1. Import the immutable UniRouter Gate A exchange
   `uxchg_f7de31e541516893a0d9b31cc0f3892a`.
2. Diagnose and fix X-Insight quarantine behavior found only with the real
   snapshot.
3. Persist accepted events to a dedicated local non-production Postgres
   database, read both request IDs back, and create the requested receipt.
4. Adopt the single execution owner workflow while preserving CI, evidence,
   authorization, and independent review boundaries.

## Artifact Identity

- Snapshot SHA-256:
  `5b1d52b1e8bd9b47f14de74670eb4404ed8862c32cdf323d86766446cdf438e8`
- Approval manifest SHA-256:
  `sha256:d2f51fc61fe84dedb8b1d1556c5a4f135b50711346327cf6fbcdd19d60337d68`
- Source instance: `unirouter-dev-gate-a-2026-07-10`
- Request IDs: one successful non-stream request and one denied diagnostic
  request, as listed in the immutable shared snapshot.

## Finding And Change

The first real import accepted 8 events and quarantined 2 with
`disallowed_payload_content`. Both quarantined trace events contained only
sanitized token-count metadata. The detector used substring matching, so
`completion_tokens` matched the blocked fragment `completion`.

The fix distinguishes governed metadata references/counts from raw payload and
credential-bearing keys. `prompt_tokens`, `completion_tokens`, API-key IDs/refs,
and authorization IDs/refs/hashes are allowed. Raw prompt, completion, messages,
request/response bodies, credentials, passwords, and secrets remain blocked.

## Local Verification

- Focused evidence/importer tests: PASS.
- Real snapshot in-memory import after fix: 10 accepted, 0 rejected, 0
  quarantined; rerun 10 duplicate/no-op.
- Migrations applied to dedicated local non-production database: 9.
- First persistence: 10 inserted, 0 duplicate/no-op, 0 conflict, 0 rejected.
- Second persistence: 0 inserted, 10 duplicate/no-op, 0 conflict, 0 rejected.
- Readback: 10 events and 3 capture gaps across both request IDs.
- Receipt status: `persisted`.

The local database connection contains no customer credential and is not
recorded in shared evidence. The receipt contains only the approved sanitized
exchange summary.

## GitHub Integration

- PR: `#9` (`codex/gate-a-real-import` -> `main`).
- CI run: `29055691226`.
- `Backend Tests`: PASS, including formatting, vet, race/full tests, fixture
  conformance, Postgres migrations, persistent snapshot import, read API smoke,
  and coverage upload.
- Final CI run `29055761311`: PASS.
- Squash merge: `4a562a6` / PR #9.

## Known Source Gaps

- The denied request contains two explicit `not_available_from_source` gaps and
  no source event. It is diagnostic evidence, not a successful correlated denial
  trace.
- The successful request contains one `provider_not_returned` gap for upstream
  request identity.
- UniRouter Batch 1W fixes the denied pre-relay evidence path in code and CI, but
  its target deployment and a newly authorized rerun are not part of this
  immutable exchange.

## External And Target State

- External integration is locally verified for this immutable snapshot:
  discovery, normalization, persistence, readback, idempotency, and receipt.
- X-Insight target/ECS deployment is not verified.
- No ECS SSH alias or target X-Insight deployment route is available to this
  task.
- A future denied-scenario rerun requires a new or amended User authorization.

## Rollback And Cleanup

- Code rollback: revert through a new PR; do not bypass branch protection.
- Local database: dedicated non-production evidence only; no destructive cleanup
  is required for this batch.
- Shared snapshot: delete seven days after receipt reconciliation according to
  authorization `UR-XI-GATE-A-20260710-01`; retain sanitized receipt and evidence
  records.

## Reviewer Checklist

1. Verify token-count and reference allowlisting cannot allow raw prompt,
   completion, message, credential, secret, or request/response body keys.
2. Run focused tests, full tests, race tests, and fixture conformance.
3. Re-run the immutable snapshot and compare artifact hashes and counts.
4. Verify first/second persistence and request-ID readback evidence.
5. Confirm the denied scenario remains an explicit source gap and is not
   presented as a successful decision event.
6. Confirm no DSN, raw payload, API key, IP, or upstream error entered Git or the
   shared response.


## 2026-09-09 XI-A0 continuation — active Local implementation

- Sole writer: XI-A0 task 01a08361-94fa-73a3-9a71-f2e2507f08c4, replacing idle prior owner after explicit supervisor handoff.
- Worktree: /private/tmp/xinsight-xi-a0-20260909; branch codex/xi-a0-reading-chain; base e393e258140b798b3b8479d7db29b9c46db0d407.
- Approved continuation: existing reading-chain plan plus missing migration, recovery, historical saved-operation replay, real HTTP and browser verification. R3: authorization, money, migration.
- Initial file binding: output/xi-a0/continuation-baseline.json. Original saved checkout's 49 protected hashes matched on takeover.
- Preserve all three failed performance reports; 300k reading p95 remains above 5000ms. Store-layer peak is not HTTP evidence.
- Explicit pause: immutable economic fact plus narrow membership redesign was rejected by automatic approval review; no implementation or indirect retry without informed approval.
- Only isolated synthetic Local work and local commits authorized. No push, PR, merge, signing, release, deployment, real sources, credentials, user-data removal, or UR communication. D-37-01 remains undecided.
- Final Local results: Go1.25.11 full race suite, vet, fixture11/11; combined actual full/PG-role coverage clears existing floors (total63.4%, hostctl64.2%). Earlier Go1.25.0 missing-covdata failure retained.
- Frontend locked Node22.23.1/npm10.9.8 build, coverage, OpenAPI/licenses, 18 legacy mock browser cases and separate real production TLS/embedded browser flow pass. Runtime asset-manifest parity passes.
- PG16 role/schema24/clean+historical migration/backup restore, saved replay and current revoke negatives pass. Original saved-project49 protected hashes and all3 inherited performance failure reports remain unchanged.
- Capacity HTTP reads:600 calls/10 subjects/about20RPS, request p95=76ms, zero errors; readings prepared through Store. HTTP500001 returns422 with zero residual writes. These do not qualify creation.
- A0 acceptance remains blocked:300k p95=19848ms best inherited result versus5000ms target; observed70k HTTP creation hits existing3s limit and returns503. No threshold, load, durability, constraint or timeout weakening.
- Reviewable matrix/TD/R3 and boundaries: docs/evidence/xi-a0-local-delivery-2026-09-09.md. Rejected-design decision only: docs/planning/xi-a0-blocked-performance-design-2026-09-09.md. Exact byte binding: output/xi-a0/evidence-binding.json.
- Synthetic test clusters and browser harness started by this continuation were stopped; predecessor PG service and preserved dependency symlink were left intact. Local commit only; no A0 acceptance or CI/Target/External/Production claim.


## 2026-09-09 XI-A0 UI independent-review repairs — Local delivery

- Manager explicitly confirmed the bounded UI repair plan after independent counterexamples; same sole owner and /private/tmp/xinsight-xi-a0-20260909 worktree. Base 1ffd5de985edfd544bcef5cf2d347f6a0c42256e.
- Fixed full-period trend overview/hourly detail, reading-time coverage explanations, and atomic successful view/error recovery including detail paging. Production Go/SQL/schema/roles/timeouts unchanged; Go edits are synthetic browser harness only.
- Local: frontend58 tests/coverage/build, real production browser24 period-width-theme combinations plus hourly/paging/expiry/revocation/recovery, original18 browser cases, focused Go race/webui/vet and asset parity pass. Unaffected full Go and PG role/upgrade/restore evidence reused as authorized.
- Delivery/A0/TD/R3: docs/evidence/xi-a0-ui-repair-delivery-2026-09-09.md. New byte binding: output/xi-a0-ui-repair/evidence-binding.json. Original49 protected files and3 performance failure JSON unchanged.
- A0 remains blocked on creation performance; no timeout adjustment or storage candidate attempted. Three-second HTTP budget is current implementation, not a new permanent user target; original5-second criterion and usable real HTTP creation remain. Storage redesign still awaits informed approval after actual automatic rejection.
- Local commit only; no push, PR, merge, deployment, real sources/credentials, user-data deletion or UR communication. Disposable test instances stopped; predecessor symlink preserved.


## 2026-09-09 XI-A0 approved isolated storage candidate — Local failed-candidate delivery

- User explicitly approved the reviewed immutable economic facts / three-column membership design, preserving original acceptance and authorization boundaries. This supersedes the earlier approval pause; it does not claim performance or manager acceptance.
- XI remains sole writer in /private/tmp/xinsight-xi-a0-20260909 on codex/xi-a0-reading-chain, base 6306c44505fc2fff9173a23c7c103b536ed43061. R3: migration25, anchored hashes, immutable facts, A0 interpretation dispatch, query joins and synthetic migration/roles/restore/performance regression.
- Preserve schema24 migration bytes, historical rc.13/schema23 contracts, failed performance evidence and original49 files. Rollback: exact isolated backup to a new database with matching binary; no in-place destructive downgrade or real reset.
- Local implementation/tests/commits only. No push/PR/merge/release/deploy/live sources, credentials/permissions changes outside disposable synthetic test roles, user data removal or UR communication. D-37-01 pending.

- Implemented schema25 anchored immutable F0 / three-column members, transactional all-field/order/hash prechecks, explicit A0 interpretation dispatch, joined queries, restore binding validation and ordinary immutable guards. Historical schema23 release and schema24 migration bytes unchanged.
- Actual runtime-role HTTP exposed denied pgcrypto.digest; fixed with equivalent pg_catalog.sha256, without adding permissions. Small creation, cancellation rollback, pagination/money, revoke and restore negatives pass.
- Performance remains FAILED: same-PG-config20×300k Store p95=10095ms, tuple-order candidate9660ms;500k16628ms. Final7-day/30-day real-role HTTP20×300k and500k all503 at unchanged3s with rollback.10 HTTP-created70k readings/10 subjects/600 read calls~19.94RPS, p9594ms, zero read errors;500001 HTTP422/six-table zero residual. No timeout, durability, constraint or load weakening.
- Role/schema25/historical restore qualification and actual-role browser3 tests/24 layout combinations pass. Frontend58 tests/coverage pass. Full Go race/coverage has one obsolete buildinfo literal failure; test-only fix and affected package rerun pass, all other packages passed. Combined coverage floors, vet and fixture11/11 pass; initial failed log retained. No expensive unchanged lifecycle rerun after the test-only fix.
- Delivery matrix, TD/R3, exact results and limits: docs/evidence/xi-a0-storage-candidate-2026-09-09.md. Evidence binding: output/xi-a0-storage/evidence-binding.json. Cumulative storage is not a per-reading allocation measurement; high-volume HTTP downstream validation remains blocked by creation. A0 and original13-item parent remain unaccepted.
- All self-created services stopped; original49 and3 inherited failure hashes match; predecessor link/service preserved. No external writes, push/PR/merge/release/deploy, real credentials/data/API or UR contact. Manager independent review and D-37-01 remain pending.

## 2026-09-09 XI-A0 checkpoint diagnostic — active approved Local slice

- Same sole owner/task/worktree, base033a19678a38dec6aacd9dcbb8466645259ba016. Manager approved plan SHA25641ab9ccfbac6ef1c2da22b97957fabf9bf3f8603e900564c3285a1f103a04804 and confirmed independent test instances stopped. No new user approval is needed within this slice.
- R1 test/runner work only: common stopped full-cluster template, actual workspace role, A max_wal_size1GB versus B4GB; unchanged product SQL/indexes/constraints/durability/role timeouts. First pair6+6, conditional reversed pair and500k probe only as reviewed. Full qualification remains a subsequent joint decision.
- Enforce12GiB new disk,20GiB remaining floor,4GiB conservative combined RSS and20-minute diagnostic window. Only this run's exact temporary clones may be cleaned after verified output preservation; failures stopped and retained. Original evidence, existing instances/worktrees/services and predecessor link untouched.
- Results pending. No deployment/default adoption, CI/Target/External/Production claim, UR contact or external writes authorized.

- Slice completed: A1/B1 each6 successful300k; median4242.704/4393.766ms, max4442.823/5268.576ms, tail144.702/228.837ms, cycle amortized4208.811/4392.477ms.4GB reduced WAL checkpoint count2→0 during the six transactions but did not improve latency; reverse-pair criteria false, no reverse/500k/full qualification run.
- Actual workspace role/10s timeout, identical1599-file stopped templates, common final-code binary, unchanged durability/constraints/settings except WAL cap verified. Read probes retain exact totals; single probes are not read p95 or HTTP qualification. About141s total, sampled maxRSS919MiB/new disk4.59GiB; all self-created instances stopped, A1 cleaned after evidence preservation, template/B1 retained.
- Delivery: docs/evidence/xi-a0-checkpoint-diagnostic-2026-09-09.md; raw output/xi-a0-checkpoint. Original49 and3 failure hashes match. Test compilation, real PG diagnostics, vet and Python syntax pass; no product SQL or config-default change. A0 remains unaccepted. Next recommendation is1GB final-code20×300k/500k capacity before HTTP budget/qualification review; not yet executed.

## 2026-09-09 XI-A0 1GB capacity slice — preparation ready, resource revision pending

- Manager independently accepted a3ae3b4 and explicitly authorized20×300k at actual workspace/1GB, then500k at retained6M, before HTTP budget/qualification. Same sole owner and worktree; R1 test/runner changes only; unchanged product SQL/indexes/roles/durability/HTTP/SQL limits.
- Current free disk about31.10GiB is below the approved32GiB startup threshold. No capacity PG/load started. Manager requested independent smaller-budget analysis without old-scene cleanup or user reapproval.
- Prepared --qualify mode, fixed20 attempts/first included/failure retention,500k six-table rollback evidence, phase/checkpoint/member growth and post-create money/reference/order/full-selected-group paging checks. Offline compile/default-skip tests, vet, Python syntax and compile-only runner pass; real PG execution remains pending.
- Proposed read-only reuse of unchanged1599-file stopped template into one new instance:7GiB incremental ceiling,28GiB start,21GiB stop buffer preserving20GiB free,4GiB RSS/20min unchanged. Runner still enforces old32GiB/12GiB rules until manager accepts revision. Detailed facts and component budget: docs/planning/xi-a0-capacity-resource-review-2026-09-09.md; preparation bindings: output/xi-a0-capacity.

## 2026-09-09 XI-A0 1GB capacity slice — approved execution

- Manager independently reviewed025a397 and resource-plan SHA256a0bd3fa68a814e3e9c3b3d275286b67cbfe7e3921899a1d6cb75e30994a9d060, explicitly approving7GiB new/28GiB start/21GiB stop,4GiB RSS and20-minute bounds. This supersedes the preceding resource pause; no further user approval required.
- Apply those limits only to --qualify, preserve reviewed preparation outputs, and record execution separately under output/xi-a0-capacity-run. Recheck current disk before copy, after copy and before starting the new PG; old template remains a read-only source. Execute exactly the authorized20 attempts then conditional500k at retained6M, with unchanged product SQL/indexes/HTTP budget and real workspace10s SQL limit.
- Actual results pending. Same sole owner/worktree, no manager load overlap; UR and all external prohibited actions remain excluded.

- Completed the predeclared20 attempts at actual workspace/1GB:19 success, sample10 INSERT SQLSTATE57014 at10291.166ms; rollback headers9/members2700000/orphans0 verified. Successful subset n19 p95=10700.776ms, not20-success qualification and above5000ms. Derived committed members5.7M; final full-table readback was not reached. No replacement samples.
- The reviewed fail-fast branch skipped500k because6M was not reached, and also skipped subsequent summary/top/hourly/full-group pagination/ordinal/money checks. These checks remain unexecuted; Store-only results do not qualify HTTP or A0. Tail checkpoint118.453ms; full20-attempt cycle142499.924ms; runner/test exit1 preserved.
- Initial PG startup failed before any workload because the default TMPDIR socket path exceeded103bytes. Retained that exact clone/log/output under output/xi-a0-capacity-run; shortened the new runner root with a path preflight. Actual workload output is output/xi-a0-capacity-run-02. First-failure copy/build/output and elapsed time stayed within the original7GiB/20min accounting; no performance sample was retried.
- Sampled peak combinedRSS1.002GiB/new logical bytes5.527GiB/minimum free25.237GiB;365.476s from first attempt to final shutdown.28GiB entry check passed before copying; after-copy/start checks enforced7GiB new and21GiB remaining. Four relevant template/B1/new/failure clusters stopped, all retained; oldtemplate and startup-failure clone still match1599 original files.
- Runtime binary SHA256f6829111ca6c0f6e79aa598b9e1ecdedbf9515b5bfdc676b57b661b85764a88b and358 compiled inputs exactly match reviewed preparation. Original49/3 failure hashes and old30 raw checkpoint outputs match; preparation/A-B identities preserved at their commits. Python syntax/compile/default-skip tests pass; unchanged Go vet evidence reused.
- Late coverage/digest/count slowdowns accompany INSERT growth; eight WAL checkpoints do not establish a single cause. Recommend bounded late-state query-plan diagnosis before any shared-buffer/SQL candidate or HTTP budget decision; not executed. Delivery: docs/evidence/xi-a0-capacity-result-2026-09-09.md, final binding output/xi-a0-capacity-run-02/evidence-binding.json. A0 and original13 items remain unaccepted; local commit only and all external/UR exclusions unchanged.

## 2026-09-09 XI-A0 plan-cache diagnosis — plan for manager review

- Manager independently accepted7595d34 failed-run evidence, not A0. Prioritize the competing pgx/PG custom-to-generic hypothesis: count steps at execution6 although it does not read members; digest/coverage step at7. Verified local pgx5.10.0 cache512/CacheStatement and PG16 official PREPARE behavior; old session mode remains unknown.
- Prepared exact source SQL/typed parameter mapping,1604-file stopped failure-source manifest and current resource snapshot in output/xi-a0-plan-cache-plan. Plan: one copied late-state cluster, actual workspace/same pinned connection,7 auto rounds without intervening EXPLAIN, then actual prepared EXPLAIN and G/C/C/G diagnostic contrasts; at most36 target SELECT executions under original10s SQL limits. No product SQL/config/HTTP change.
- Proposed4GiB new/25GiB entry/21GiB stop with explicit copy/temp/runtime/output budget; copy actual3541.297MiB leaves only554.7MiB, so completion is conditional on resource observations. Post-copy gate requires512MiB incremental room and21.5GiB free. Do not clean old scenes or change PG defaults to fit. Same4GiB RSS/20min bounds.
- Detailed reviewable plan: docs/planning/xi-a0-plan-cache-diagnostic-2026-09-09.md. No new PG copy/start, query workload or runner/test implementation in this plan-only turn; execution awaits manager agreement. Original13/TD, UR exclusion and all external authorization limits unchanged.

## 2026-09-09 XI-A0 plan-cache implementation — observer failed, fix prepared

- Manager accepted9fd8c3e design and conditionally authorized native per-fileCoW/24GiB entry, with4GiB new/21GiB stop/512MiB post-copy reserve/RSS4GiB/20min unchanged. Independently verified local clonefile isolation and no fallback, then implemented separate test/runner; productSQL/defaults untouched.
- First preparation stopped before PG copy because Python lacksos.listxattr; retained failure/probe and used verified native Darwin xattr interfaces inready-02. One1604-file clone to/private/tmp/xi-pc-hcpwpsza/data verified bytes/types/permissions/xattrs and distinct inodes; source unchanged, no links/tablespaces/ACL. Only that clone started.
- Actualworkspace/PID36483/cache512/CacheStatement/auto/10s/readonly verified; baseline19headers/5.7M and seven300k readings read back. Firstcount returned300000 in51.152833ms, then observer JSON OID-string-to-int decoding failed. No digest/coverage/EXPLAIN orG/C execution; no hypothesis established. Raw1-target failure retained, testexit1 and normalstop; sourcebefore/after identical.
- Fixed observer withjson.Number and strictOID comparison; regression for strings/numbers, default-skip andvet pass. FixedbinarySHAaaa6ecbdbc7b27256c0eb8b7ecff59936d9ac96b48211b49d0dda73cb8221a5a retained separately, not run againstPG. Failedbinary/source/plan preserved. Removed only one newly generated reproducible4242388-byte test cache archive, exactproof retained; no originalcache/faileddata cleanup.
- Run peaknew3728523963bytes/RSS273776640bytes/minfree26135330816bytes,10.694seconds; source andclone stopped. No automatic newseven-round sequence: one existing execution plusfresh36 would exceed reviewed36 andPID continuity is lost. Recommend manager authorize separate36/20min on sameclone plus32MiB cumulative budget to4128MiB, retaining512MiB/21GiB/20GiB guards; currentfixed-prep margin509.877MiB is below512. No secondrun started.
- Delivery: docs/evidence/xi-a0-plan-cache-observer-failure-2026-09-09.md; runtime outputxi-a0-plan-cache-run, fixed offlineoutputxi-a0-plan-cache-fix. Original13/TD/A0 remain unaccepted, UR/external exclusions unchanged; local commit only.

## 2026-09-09 XI-A0 prepared-plan recovery — authorized fresh diagnosis

- Manager independently accepted9c9deec failure/CoW evidence and fixed binary, authorizing a fresh36-target/20min read-only sequence on the same stopped/private/tmp/xi-pc-hcpwpsza/data, without another copy. Old one-target session/outputs/binary remain separate.
- Authorized cumulative limit4128MiB, including retained clone/two binaries/all preparation-failure-fix-new outputs/cache growth. Preserve24GiB startup,512MiB and21.5GiB pre-query gate,21GiB stop/20GiB reserve,RSS4GiB,SQL10s. Only runner/record changes; exact fixed359-source binary reused.
- New explicit--resume entry andoutputxi-a0-plan-cache-run-02, newPG log andPID; baseline/statistics/cache boundaries recorded afresh. Results pending. No productSQL/default/HTTP changes, original13/TD andUR/external exclusions unchanged.

- Fresh diagnosis completed:36/36 target SELECTs,21 auto plus15 actual-prepared EXPLAIN, sameworkspacePID43322/cache512/10s/readonly; fixedbinarySHAaaa6ecbdbc7b27256c0eb8b7ecff59936d9ac96b48211b49d0dda73cb8221a5a and359 inputs match. Test13.80s/exit0; whole newcycle21.417806s. Old failed session's one target remains excluded.
- count auto execution6 switchescustom→generic; firstfive44–51ms, sixth435.698/seventh280.461ms. G/C medians302.796/47.745ms; generic estimates300000-row join as1 and scans grants300000times. digest/coverage remaincustom7/7; G/C medians265.185/281.186 and166.519/413.850ms. Coverage generic avoids an empty-result branch's member/F0 build; blanketforcecustom is unsupported.
- Allseven readingIDs already occur in member MCV statistics;39 selected-table snapshots unchanged,actual19headers/5.7M. Original fresh-ID INSERT/dirty-buffer conditions and late digest973.975ms/coverage2325.257ms are not reproduced. Next reviewed proposal: existingMCV/nonMCV300k pair, digest/coverage only18targets; no such follow-up executed. Count-onlyQueryExecModeExec is an unimplemented local candidate, insufficient alone for A0; later full20×300k/conditional500k/read/HTTP qualification remains required.
- Observed peaknew3862288365bytes/RSS1137360896bytes/minfree25758519296bytes; startup24GiB and512MiB/21.5GiB first-target gates passed, runtime21GiB/4GiBRSS/4128MiB/20min bounds held. Original1604-file source unchanged; source and reusedclone shut down, alloldscenes/binaries preserved. No newcopy/cleanup/productSQL/PGdefault/HTTPchange or external/UR activity.
- Delivery: docs/evidence/xi-a0-plan-cache-diagnostic-result-2026-09-09.md; raw/derived outputxi-a0-plan-cache-run-02 and final evidence-binding.json. Original49/3failures match; previous37binding matches9c9deec,34bytes unchanged and3authorized runner/plan/record edits. Local diagnosis complete; original13/TD/R3/A0 and D-37-01 remain unaccepted/pending. Local commit only.

## 2026-09-09 XI-A0 MCV/non-MCV contrast — approved Local slice

- Manager independently accepted755e16f diagnosis, notA0, and approved one18-target/20min digest/coverage contrast on the same stopped/private/tmp/xi-pc-hcpwpsza/data. No additionalcopy, original1+36 targets/outputs remain separate. ExactMCV/nonMCVIDs,300k/sameref/preclassification must verify; invalidstate stops comparison without ANALYZE/substitution.
- Independently accept reuse-only23GiB startup and4192MiB cumulative budget: no newcopy,2GiB to21GiBstop,64MiB incremental allowance subject to actual compile/cache/output accounting.512MiB/21.5GiBfirst-target gate,21GiBstop/20GiBreserve,RSS4GiB/SQL10s/20min remain. Allold/newbinaries/outputs/cachegrowth retained and counted; no cleanup or automatic budget expansion.
- Implement existing harness's bounded cohort mode and explicit offline/run entries; bind exactSQL/parameters/binary, sameworkspaceconnection,2auto preparations plus16 actual-prepared EXPLAIN in interleaved/reversedG/C/C/G. Results pending. Product changes/fullqualification remain later decisions; original13/TD/A0 andUR/external exclusions unchanged.

- First cohort start read bothIDs300k/sameref, then observer failed before anytarget while decoding unrelated booleanMCV asstring. Rawstate confirmsclassification unchanged; PG stopped normally. Preservefailedbinary/source/runner/plan/output; fix selection-before-decoding withmixedbool/numeric andinvalidreadingIDtype regression. Recovery may proceed onlywithinoriginal06:49:51UTC20min and18-targetbudget, allfourbinaries/cache/output counted; otherwise stopload. Separatefix/run-02 paths, nooverwrite/copy/cleanup.

- Recovery executed18/18 SQL successfully in9.24s atworkspacePID51962; originalwindow241.184188s includingfailedstart/fix. Post-runsemantic audit foundcoverage9 invalid: replaced$1subject, readingactually$2. Onlydigest9 acceptedasvaliddiagnostic; rawcomplete/PASS retained withexplicitderivedinvalidity, noadditionalPGsamples after18budget. This is anownedharness/inputerror, notnegativecoverageperformanceevidence.
- DigestMCV G/Cmedians352.055/375.375ms; nonMCV294.459/1030.703ms. NonMCVcustom estimates1 vs300000rows anddoes300000F0indexprobes, whilegenericparallelhashpath~294ms;35tablestatssnapshots unchanged. Existingrowselectivityhypothesis supportedfordigest only; originalfreshtransaction/INSERT andcoverageunresolved.
- Preservedactualruntimeinputs/source/runner/plan/binaries; correctedreadingparameterdetection andstrictcomparisonofallnonreadingargumentsagainstapprovedreference. Realcorrectedinput/subject-filternegatives/OID/mixedMCV/defaultskip/vetpass;actualbadinputrejectedoffline. CorrectedbinarySHA8999885eb8ea0e457585cce366fcda67b81c5613e2a7eb1169df0ac6079540e9 isofflineonly.
- Runpeaknew3992458975bytes/RSS982056960bytes/minfree25195134976bytes,allrecordedguardsheld; sourceunchangedandbothclustersstopped. Fifthbinary/finalfix bringsnew3876000400bytes/remaining519630192bytes,below512MiBnext-targetgateby17240720bytes. No cleanup,budgetexpansion,productchangeornewcopy. Proposemanagerreviewcoverage-only9 targets withmeasuredresourceplan; noextraexecutionauthorizedorperformed.
- Deliverydocs/evidence/xi-a0-mcv-contrast-result-2026-09-09.md andoutputxi-a0-plan-cache-mcv-run-02 finalbinding. Partialdiagnosticdeliveryonly,coveragecontrastunfinished; original13/TD/R3/A0/D-37-01 pendingandUR/externalexclusionsunchanged. Count/digestlocalcandidatesnotimplemented; historicalINSERTalonep957082.444ms and3samples>5s preventclaimingthesereadchangeswouldqualifycreation.

## 2026-09-09 XI-A0 coverage-only contrast — approved Local slice

- Manager independently accepted2d773fa digest9/failed-evidence only and approvedcoverage-only9targets/new20min on same stoppedclone. Currentdraft retained after platformcapacityinterruption; no PG start or newcoverageoutput before resumption. Exact$2reading substitution andallnonreadingarguments must matchoriginalreference.
- Newauto completecolumns/row must equal755e16 sameMCV validbaseline (authorizedsourcecount1), matchedbyfullarguments andboundfilehash. Preserveactual/expected andstopifdifferent. Then8interleaved/reversedG/C/C/G EXPLAIN; original1+36+18excluded,nodigestrepeat.
- Approved4256MiB cumulative includes5old+newbinary/alloutputs/cachegrowth,23GiBstartup,512MiB/21.5GiBfirst-target,21GiBstop/20GiBreserve,RSS4GiB/SQL10s. Recheckactualpostcompile/start/firsttarget;no copy,cleanup,automaticbudgetincrease orproductcandidateadoption. Results pending; originalacceptance andUR/external boundaries preserved.

- Coverage-only completed9/9 validtargets atworkspacePID58773; exact$2reading andunchangedsubject/otherargs verified, auto completecolumns/nine-fieldrow equals755e16 validMCVround7 includingauthorizedsourcecount1. SamepreparedSQL/OID/name/prepare_time andforcedmodecounters/restoration match;19selectedtablestats unchanged. Oldinvalid9remainexcluded.
- MCV G/Cmedians363.342/419.362ms withfirstGwarmupvariation; nonMCV172.324/2506.404ms. NonMCVcustom estimates1vs300000members ineachoftwoscans,600000F0+300000evidenceindexprobes; genericparallelpath avoidsrepeatedprobes. Supports currentexistingrowstatisticsmechanism forcoverage alongsidepriorvaliddigest; originalnewwrite/INSERT/sessioncausalitystillunproven.
- Fixedruntimebinary39c8c8c5adfdfe7b595438f3353a1c290eb467e8c8853652cc53ccfde3e026d7/359inputs,compile/input-baseline/semanticnegatives/parameter/OID/mixedstats/defaultskip/vetpass. Test9.15s,fullcycle16.181741s. Sampledpeaknew3980479459bytes/RSS290422784bytes/minfree25088008192bytes;first-targetremaining549598237bytes,allguardsheld. Source1604unchanged;source/reusedclone shut down,allsixbinaries retained.
- Deliverydocs/evidence/xi-a0-coverage-contrast-result-2026-09-09.md andoutputxi-a0-coverage-run. RecommendmanagerreviewoneactualStore300kcreatewithinstalledauto_explain (librarypresence/docsverified,notloaded), boundnewsyntheticwrite/resourceplan, thenselectcountExec +localizeddigest/coveragegenericcandidateandINSERT-specificworkfromevidence. No newwrite/prototype/auto_explainexecution performed; original20×300k/5s/500k/read/HTTP/13/TD/A0/UR/externalboundariesunchanged. Local commit only.

## 2026-09-09 XI-A0 one actual Store creation — approved Local diagnostic

- Manager accepted d6ae76f coverage9 and explicitly approved one300k new-ID Store creation at retained19/5.7M using installedauto_explain; no additional copy/retry/product/external/UR action. Success retained20/6M is a new diagnostic prestate, not original20-sample/500k qualification.
- Bound proposal adopted at docs/planning/xi-a0-auto-explain-create-2026-09-09.md: cumulative4480MiB,23GiBentry,512MiB/21.5GiBfirstcreate,21GiBstop/20GiBreserve,RSS4GiB/SQL10s/new20min. All old/new binaries/cache/output retained and charged. Actual workspace light parameter/log preflight is mandatory before sole creation. Preparation and results pending.

- First preflight stopped at0creates: restricted pg_settings omitted GUC_SUPERUSER_ONLY session_preload_libraries. Existing workspace actual plans/module settings proved loading; no permission change. Preserved executed test/runner/plan and a552c00d binary. Corrected observation via existing diagnosticadmin SHOW, kept fullworkspace parameter/analyze preflight, new360-input binary0c1d3be4; recovered within original08:00:33UTC20min/4480MiB window, counting both new plus sixold binaries.
- Sole actual Store creation succeeded atPID65901/port60869 with complete lightparameterpreflight. 7actualSQLs exactlymatchproductsource andfullarguments, allnewpreparedcustom1/generic0. NewIDxwr_9kVW5mAm_-NntzgtjeDoRQ;19/5.7M→20/6M, originalheaderspreserved, other4tablecountsunchanged, new300k andfullref/coverage return+storedreadback equalvalidbaseline. No500k/replacement/qualificationcount.
- Instrumentedtotal10403.924ms;INSERT5468.094,digest1025.485,coverage3616.706,commit0.154. Actualnewtransactiondigest/coverage estimate1vs300k andperformF0rowprobes;coverage has2×300kheapfetch plus600kF0/300kevidence/300kgapindexloops. INSERTtwoexternal sorts,2×300kFKcalls,rootWAL119928329bytes;withoutnodetiming nofalseFK/sorttimeattribution. ClusterwindowWAL350936399bytes/buffersfull35571/write2071.763ms,notrootstatementcost;noin-windowcheckpoint.
- RecoverytestPASS10.97s/cycle18.476s/originalwindow190.136s;20recordedresourcechecksallpass,peaknew4118075591bytes/RSS506888192/minfree24580730880. Source1604unchangedandbothclustersstopped;PGshutdownnativeWALrecycle/removalretainedinlogs,nomanualcleanup. Fullreportdocs/evidence/xi-a0-auto-explain-create-result-2026-09-09.md;proposereadmodecandidatewithsmall/largeguards,thenuninstrumentedcreationandconditional128/512sharedbufferdiagnosticwithwal_buffers4fixed. Noproductcandidateadopted;originalqualification/HTTP/13/TD/UR/externalboundariesunchanged. Completed Local diagnostic delivery; final byte binding and local commit, no external write.

## 2026-09-09 XI-A0 read-mode candidate — authorized implementation and bounded calibration

- Manager accepted7dcbf77 realcreateevidence andauthorizedcountExec/transactionlocalgenericcandidatewithrepresentative0/small/70k/300knewwriteverification. Reuse20/6Msyntheticinstance;noINSERT/ctid/FK/schema/storage/defaultchange,auto_explainoff;shared128/512remainsunapprovedfollowup.
- Newplan docs/planning/xi-a0-read-mode-candidate-2026-09-09.md declaresindependent20min,8pairedcreates+2emptyrestorationcreates+1smallcancel,max10newheaders/740032committedmembers. Whole4480MiB/23GiBentry/512MiBfirstlarge/21.5GiBfree/21GiBstop/20GiBreserve/RSS4GiB/SQL10sunchanged. Initialfreebelow23GiB;noPGstarted,independentcode/offlineworkcontinues.
- CountExecimplementedwithunchangedSQL/text[]contract;scopedgenerichelperrestorespriortransactionmodeandrollbackhandleserrors. Ordinaryentrykeepsderivedmodeunchangeduntilmeasurementselectspolicy;privatecommoncreationcoresupportscontrolledcomparison,nopreselectedthreshold. Binding/offline/calibrationresults pending.

- Calibration completed8/8normalcreationarms plus2emptyrestores/1expectedStorecontextcancel andseparatebackend57014samePIDrollback. Result30headers/6740032members;allref/coverage/readback/money/ordinal/array/time/deniedsubjectcheckspassed,auto_explainoff. 0A/G8.572/697.078ms,16A/G5.684/418.234ms,70kA/G3073.615/1411.156ms,300kG/A4796.395/8653.320ms;totaldifferencesincludeINSERTvariation. Derivedpair70k805.675→451.286ms,300k3660.353→598.826ms.
- Adopt conservative observed70000-member boundary, notoptimalcrossoverclaim;ordinaryentrynowselectslocalgenericonlyat/above70000. Saveexactcalibrationproduct/test/runner/plansnapshotsbeforefinalsourcechanges. Finalordinaryentry0/16/69999/70000/300000verificationpreparedunderindependent20min/same4480MiB,notyetexecutedbecausepostcalibrationfreebelow23GiB;noautomaticthresholdrelaxation,cleanuporadditionalPGstart.

- Finalcandidatebinaryab3c1462/361inputscompiled;70000policyboundary/offlineWorkbenchtests/vetpass. Calibrationbinaryee1bfc26andexactexecutedsnapshotsretained;15countExecdrivercalls/221traceentriesnormalizedforstdlibresult-formatoptions,actualargs/preparedincrement/semantic/cancelchecksverified.45resourcechecksallpass;peaknew4045184336/RSS730038272/minfree23789649920,sourceunchangedandclustersstopped.
- Finalordinaryentry5-sampleverificationNOTexecuted:preparedfree23778684928bytes,23GiBgap917377024;new4056435387bytes/remaining641185093byteswithin4480MiB. Reportdocs/evidence/xi-a0-read-mode-candidate-result-2026-09-09.md proposesmanagerreviewonlyfinalstartup23→22GiBwithallotherguardsunchanged,nochangeimplemented. Currentcandidatepartialdelivery,notA0/p95/HTTP/500kclosure;original13/TD/UR/externalboundsunchanged.

- Delivery08:54:11UTCverifiedboth361-inputbindings,10retainedbinaries,calibrationsnapshots,53previousboundfilesat7dcbf77,49originalprotectedfiles/3historicalfailurefiles,source1604unchangedandstoppedinstances. Finalstartupnegativeofflinecheckprovesfree23GiB-minus1launchesnosubprocess/output. Latestnew4057033403/remaining640587077/free23774425088/startupgap921636864bytes;finalruntimeabsent. Partialcandidateandresourceproposalcommittedlocally,nopushorclaimoffinalverification.

## 2026-09-09 XI-A0 final ordinary-entry verification — approved single-run 22GiB gate

- Manager accepted4360de03 and explicitly approved5 ordinary-entry creates0/16/69999/70000/300000, with only this run's startup23→22GiB. Added separate execution preparation/entry; preserved originalfinal-ready/23GiBnonexecution evidence, reusedab3c1462 binary/361inputs without Go change or rebuild. Other4480MiB/first-large512MiB+21.5GiB/21GiBstop/20GiBreserve/RSS4GiB/SQL10s/settings/constraints unchanged.22GiB and legacy23GiB positive/negative offline gate checks passed.
- Final5/5 ordinary-entry creates succeeded atworkspacePID77565, auto_explainoff: totalms0=7.405,16=6.228,69999=3489.429,70000=1747.682,300000=4287.654.95traceentries/5countExec matchedexactSQL/businessarguments. First3auto/last2localgeneric, samePID/restoredauto, derivedpreparedG2/C3. Fullref/coverage/readback/money/independentF0ref/ordinal verified.30/6740032→35/7180047;original30completeheadersunchanged,exact5newIDs,other4countsunchanged,nofailure/replacement.
- Fullcycle09:19:06.457727–09:19:27.149700UTC20.691973s,GoPASS11.99s.32recordedresourcechecksallpass;peaknew4293110459/RSS681394176/minfree23485755392bytes. Source1604unchangedandbothinstancesstopped,10binariesretained. Reportdocs/evidence/xi-a0-read-mode-final-result-2026-09-09.md andrawoutput/xi-a0-read-mode-final-run. This is single-sampleLocal candidateverification,not20-samplep95/A0/HTTPqualification.
- Derivedreportcorrectspriorcalibration12Storecalls:11reachedheader(10committed+1cancelled),1no-grantdeniedbeforeheader. Oldrawplanned_store_calls11andhistoricalrecordsunchanged.
- Submitteddocs/planning/xi-a0-final-code-qualification-plan-2026-09-09.md only;originalemptytemplate1599filescurrentmatch,stillstopped. Reusefinalbinary/existingqualifytest proposed;singlecopy7GiBincrement/28GiBstartup,09:22:15UTCfree23598960640/gap6465810432bytes. Needsmanagerjointagreementandresourcewindowbeforeexecution;noclone/qualification/512MBexperiment now. Original20allpass+p95<=5s,500k/500001,grownreads/HTTP,13/TD01–04/D37/UR/externalboundariesremain.

## 2026-09-09 XI-A0 qualification resource disposition — preparation only

- Manager independently accepted0f3a048 five-call final-entry delivery, notA0, and requested a read-only plan for three precisely identified task-generated PG directories plus bound qualification preparation. No deletion/copy/PG startup/qualification or new product batch authorized in this preparation; UR/external/D-37 boundaries retained.
- Inventoried only socket-failure qualification, oldWAL B1, and completedplan-cache root. All stopped/noPID/no symlinks/specialfiles/tablespaces. First1599datafiles exactlyequal originalemptytemplate; B1 creation/6samples/rollbackEXPLAIN/stop andmtime match retainedrecords; plan-cachedata matches acceptedfinal35/7180047manifest. All11rootPGlogs have byte-identical repository copies. Preserved newfullmanifests and original7/30/10raw manifests; allthree legacybinarybindings match. No candidate removed.
- Submitteddocs/planning/xi-a0-qualification-resource-disposition-2026-09-09.md: conditional①→②→③ exact-path retirement, remeasurefree aftereach, stopat28GiB; do not addallocated as guaranteedreclaim, especiallyCoW③.09:48:06UTCfree23583801344/gap6480969728bytes; firsttwoallocated4887527424stillleave1593442304iffullyfreed. Retainemptytemplate/original19/5.7Mfailedsource, allrawoutputs/10binaries/protectedwork; losingoldrandomIDs/physicalstatecannotbeclaimedbyte-rebuildable. Ifallthreeinsufficient,stopandjointlychoosefurtherresourcepath,nobudgetreduction/unknowncleanup.
- Newscriptxi-a0-final-qualification.py defaultprepare completed, reusingab3c1462/361inputs,noGochange/recompile. Newreadybindstemplate1599/current25migrationchecksums/fixture/runner/limits.7GiBnew/28GiBbeforeallocation/21GiBstop/20GiBreserve/RSS4GiB/20min; onefullstoppedcopy, shortsocket, originalqualifytest20×300kthenconditional500k/grownStorereads, samePG/durability/SQL10s.16offlineguard/input/copymetadata checks passed including actualentry28GiB-minus1 zeroallocation/launch; binarydefaultskip passed. Realcopy/SQLpreflight/20samplequalification notexecuted.
- Submitteddocs/planning/xi-continuation-and-debt-review-2026-09-09.md with exact第38/13-source mapping, proposedA1/B/C/D/X independentseams andblockedsource/realintegrationconditions, TD01–04 resolved/retained/pending/exitcriteria. CurrentA0stillrequires20allpass+p95<=5s,500k/500001,HTTP/grown600requestpeak/lifecycle/restore; noA0orparentcompletionclaim. No cross-task message or external write; localpreparationcommit only.

## 2026-09-09 XI-A0 exact resource action — automatic approval blocked before execution

- Manager independently accepted8f911d4 and explicitly approved exact ordered removal of three inventoried task-generated PG copies, then one bound qualification if actualfree>=28GiB. Fullmanager-decision/review/offline evidence read from independent-review-13. Runner/Go/binary/limits unchanged; no recompile.
- Firstpath qualification underxi-checkpoint-run-bjep8yon rechecked against exactinode176614362/dev16777234, complete1600file/type/permission/metadata manifest, stoppedcontrol, and preservedPGlog. Native `/bin/rm -r -- <exactpath>` was rejected by automatic approval before process creation. Rejection treats manager decision as untrusted transcript evidence and requires explicit user authorization for irreversible directory deletion. No alternate command/workaround, no other removal attempted. Post-rejection fullmanifest/inode unchanged; allthree candidates retained.
- Free23566422016bytes,28GiBgap6498349056bytes after rejection. No qualifyingcopy/PGstartup/workload/output; originalsource/template/binaries/records retained. Exactthree-path conditional approval question sent to user throughplatforminput; response pending. This external approval blocker does not invalidate preparation or permit reducedbudget.
- Completedindependentrecordfix: continuation-map batches now distinguish historicalkickoff_status=not_dispatched fromcurrent_status. A0isinprogresswithresourceapprovalblocker; A1/B/C/D/Xplanned_not_started. All13requirements/acceptancerefs/TDmapping retained andchecked againstunchangedoriginaltask-plan. Newdisposition/run.json,approval-rejection.json andindependent-work-check.json preservefacts; sourcefilesunchanged.

## 2026-09-09 XI-C responsibility and history — independent proposal only

- Manager acknowledged the deletion approval blocker and will handle the explicit user deletion request in the manager task; the blocked-state correction/evidence is committed at 085b9fe. No removal retry, changed command, repeated approval question or cross-task message in this turn; no qualification, copy, PG startup or product change.
- Selected one concrete next slice from the continuation plan: existing Finding responsibility/deadline registration with atomic current projection, append-only action history, human permission/CSRF, optimistic concurrency and idempotent receipts. Delivered docs/planning/xi-c-responsibility-history-proposal-2026-09-09.md for manager review, not implementation authorization or acceptance.
- Targeted current source confirms absent structured responsibility/actions, local account candidate API and client version/idempotency checks; existing Case lock/seal, immutable v1 bytes, public transport and current host role/schema contracts provide reuse points. Case permissions are currently deployment-role based; all existing human Evidence roles can investigate, while workbench_reader cannot read Cases. Project scope and a read-only Case persona remain parent work, not silently assumed capabilities.
- Proposal maps every original P2-A01–14/S01–09 area, with responsibility subcases of P2-A08/10 as the local focus; retains full SE-03/GX-01, 13 requirements, TD-01–04 and A/B/C/E dependencies. New source-derived review/queue, end/reopen, new-evidence reassessment and real integration remain pending. Proposed future isolated checkout and small test DB avoid the A0 performance/template dependency; 1 GiB incremental budget is a preflight proposal, not a tested guarantee.
- This turn only performs source/document readback and hash/diff checks. No product tests, compile, account/credential change, new worktree or database. The fixed A0 candidate remains ab3c1462 with 361 bound inputs; qualification runner, 9 preparation bindings, original continuation status and template metadata bindings remain unchanged. Current physical template bytes are not rehashed by this plan-only turn. Delivery checks and hashes are under output/xi-c-responsibility-history-proposal/; local commit only, no push/merge/release/UR action.


## 2026-09-10 XI-C Finding review and follow-up — implementation and local evidence; coverage gate pending

- Implemented review20-approved complete lifecycle from fe39ebd0 / product baseline fcda883: atomic initial review/creation receipt, separate verdict/progress/responsibility, four finite closures/reopen, typed adopted Gap contents and read-time changes, immutable history, v3 seal and atomic unreviewed continuation. Schema27 is additive; old52 migration files, formal23/rc.13 identity and v1/v2 stored bytes remain. New investigation_reader has only evidence.read; existing roles/accounts stay unchanged. Development guide is separate from the release-matched manual.
- Actual isolated PG16.14/production roles/TLS-human tests cover26→27/legacy9/current23/rc.10 contract, old account/session/receipt compatibility, native backup/restore, readonly check, rollback/idempotency/committed-response-loss, old/new command races, concurrent duplicate cycles, Gap version/content races, seal and strict HTTP negatives. Host/signature/runtime lifecycle substitutes remain explicit. Real embedded UI completed selected evidence/gap→atomic review→responsibility→progress→end/reopen→changed Gap→review→seal/continue→reader; old62827-byte v3 SHA0f0619363af7e7591c147ba4fb03ce54c79a6001ab48bdd93d9789712c91c10c preserved. Final light/dark/narrow assets viewed and HTTP hashes match local dist.
- Frontend85 tests/coverage thresholds, typecheck/OpenAPI62paths/license195/build and18E2E pass. Final37 Go packages covered by selected completed race results, final vet/gofmt/module verification pass. Both failed whole commands remain failed: first ended on historical output diagnostic Go archive; later was resource-stopped. Archive only gained build-ignore; exact old bytes survive in connectors-baseline-probe_test.go.original.txt (SHA54f105f07260a22af186bdbfcbbb4b050d30da43343229eb4e897c124e0eba67). Final package/input/profile mapping excludes partial packages and duplicate coverage blocks.
- Required unchanged Go coverage gate was actually run and FAILS under the mandated no-DSN whole-test conditions: total48.6<63, api48.1<53, db3.1<69, hostctl55.7<64; workspace62.8>=46. Native scoped PASS is not substituted for coverage data. No lowered gate, invented counters, full PG/remote CI load or full-package acceptance claim. This remaining acceptance gap is delivered with complete source/evidence for manager review.
- Original1600MiB cap was crossed at22:59:44Z (1677922304bytes), guard exit75 stopped the load; manager explicitly added256MiB to cumulative1856MiB/1946157056, preserving baseline/shared retained allocations and20GiB floor. Subsequent checks use amended guard; no retained-scene cleanup or baseline reset. A0 361-source/9-preparation bindings/fixedbinary and three old paths preserved, original worktree clean; local test PG/browser stopped. Free-space now exceeds original A0 entry but A0 remains unrun pending current package review and a fresh nonoverlapping qualification window.
- Detailed implementation, failures, acceptance mapping, valid log chain and remaining scope: docs/planning/xi-c-review-lifecycle-implementation-record-2026-09-10.md. One owner completed authorized subitem work without per-experiment approval; failures/observer rework and unnecessary repeated whole-test cost remain visible. No measured token/time savings claim. Final binding output/xi-c-responsibility/assessment-evidence-binding-final.json accompanies local commit only. FullC/source/queue/A1/B/D/X/original13/TD final acceptance andD-37-01 remain; noUR/external/realcredential/traffic/release/deploy actions.


## 2026-09-10 XI-C review21 coverage supplement — original Local floors pass

- Executed the approved complete six-package supplement on existing PG49998 with two verified-new synthetic DBs, Go1.25.11/PG16.14 and unchanged cumulative accounting. Corrected schema23 forward-migration count and only the CI ephemeral backend host/local-fixture environment; no production source, formal contract/migration or frontend asset change.
- First regular five-package command failed solely at the existing-role fixture precondition; four completed packages remain valid. Safely reused explicitly local nonprivileged/no-membership synthetic roles without dropping or altering old roles. Full DB then passed66.9%, followed by meaningful persisted assessment/responsibility/history/seal/continuation/restore tests and final full DB race PASS6.909s/70.9%. All raw failed/intermediate results retained.
- Original hostctl native qualification plus all ConnectorDigest paths PASS20.178s. Five complete PG profiles replace old package profiles; exact-input hostctl native/full-noDSN union and31 unchanged package results produce19622 unique blocks,17627/27531 statements. Unchanged coverage gate exit0: total64.0/API58.7/DB70.9/hostctl64.5/workspace62.8 against63/53/69/64/46. Final DB vet, YAML semantic diff and gofmt pass. This is assembled Local package evidence, not one all-green whole-repository command or remoteCI/Artifact/Target success.
- End observer assumed retired hostctl predecessor DB still existed and failed. Existing qualification source proves native DATA_RESET predecessor retirement; one read-only catalog-aware readback corrected the observer, with no test rerun/recreation/cleanup. Final PG49998 stopped, regular DB and native clean successor retained, no other clients. Observer cost/error is recorded, not hidden.
- Review21 amended shared cumulative limit2368MiB;57 window samples max1979232256/minfree38417821696bytes, all within this window; previous cap stop preserved. Final source/freeze readback confirms A0 361+9/fixedbinary,52 old migrations/formal identity, original worktree clean, template/three old paths retained. No A0/browser/adminrotation/N1/UR/external operations. Full result and provenance under coverage-20260910; updated existing implementation record. Same owner local delivery, pending manager final acceptance; no per-test approval loops or unmeasured savings claim.

## 2026-09-10 XI-C investigation queue — complete Local package for unified acceptance

- Implemented review23-approved deployment queue: separate current/created/basis-change views, full authorized-set statistics and stable pagination, shared detail/queue basis and overdue (including nil progress), own saved-filter CRUD/version/retry/tombstone, human permissions/CSRF and actual embedded UI. Schema28 is additive; original53 migration files/formal release/old contract unchanged. Synthetic v1/v2 historical wire packs and actual v3 retain exact stored bytes through upgrade/native restore.
- Correct-target five-package PG race results, native lifecycle and corrected whole-run package evidence pass unchanged coverage floors: total64.3/API59.7/DB70.4/hostctl64.5/workspace65.9. 201 unique records/5pages;2001 mixed set HTTP148.788834ms. Locked Node22.23.1/npm10.9.8 passes93 tests,coverage/typecheck/OpenAPI65/license195/build/18E2E; actual production handler readyz/schema28/deep-link and3asset hashes match; actual UI lifecycle and6viewport/theme observations retained.
- Database target incident: initial base DSN selected this task's synthetic maintenance postgres, where direct legacy tests migrated to28 and wrote synthetic data; observed123evidence_events has no before snapshot, so not claimed entirely new. Retained affected maintenance/old DBs and all raw failures; final full5PG regressions use newly created xic_queue_reg_20260910. Manager notified once as scope deviation, no per-command approval loop.
- Manager-required TD correction added test-only entry guard to6packages, checking independent scope and live cluster/database identity before m.Run. Four maintenance/retained targets each rejected in6packages before direct writes/factory creation; logical dumps/catalog unchanged. Correct direct/factory/native and actual mismatchedOID negative pass; original CI ephemeral-service contract retained. Native supplement's initial wrong pg_dump PATH failure retained and only affected test rerun. Detail/limitations in docs/evidence/xi-c-investigation-queue-local-2026-09-10.md.
- 1039 validation resource samples all within unchanged2880MiB cap/A0original18GiB/free>21GiB: Cpeak2851999744B/A0peak15041940554B/minfree31576039424B. PG49998/browser stopped; old scenes retained; noA0/runtime/external/UR/credential/push/release actions. Final manager acceptance pending. Complete-subitem execution, targeted reads and valid-evidence reuse adopted; necessary verification retained, no measured token/time saving claim.

## 2026-09-10 XI-C review24 — three reproduced queue defects corrected

- Manager rejected c8f0301 complete queue on three real counterexamples; all accepted as applicable. Fixed version binding to exact evaluation instant plus complete filtered rows/statistics, allowing genuinely excluded edits while rejecting membership/statistic changes. Fixed explicit saved-filter reread with visible latest-version reconciliation, preserved drafts, update/delete recovery, deletion handling and out-of-order/editor-generation protection.
- Original manager counterexamples plus custom-period time tamper, equivalent timezone/10-minute continuation, statistics-only change and exact overdue boundary pass. Full DB/API/webui race and vet pass; 201/5pages and2001 mixed HTTP143.862333ms. Reassembled unchanged floors pass total64.3/API59.7/DB70.2/hostctl64.5/workspace65.9. Locked frontend98tests/coverage/typecheck/build/18E2E pass; unchanged OpenAPI/license/native lifecycle and59 historical contracts reused with explicit hashes.
- Actual synthetic production-handler Chrome: update1→409, reread/reconcile→update2 succeeds revision3; concurrent revision4, delete3→409, reread/load→delete4 succeeds revision5. Drafts retained without selection switching; external deletion retains draft; reader Case write403. Six changed-region viewport/theme screenshots fit; final HTML/JS/CSS and schema28 readiness verified by actualHTTP. CLI environment/observer failures retained separately; no original product failure reclassified.
- Guard updated first to3136MiB with unchanged origins/A0/free bounds and explicit manager review24 allocation. Formal170samples allwithin: Cpeak3052646400/A0peak15185399275/minfree30724227072B. PG49998 and browser stopped, original5DBs/old incidents retained; noA0/UR/external/push/release actions. Incremental report docs/evidence/xi-c-queue-r24-corrections-2026-09-10.md; unified independent acceptance pending.

## 2026-09-10 XI-C review25 accepted; A0/A1 incremental readiness only

- Manager independently accepted417daf1/tree61cbcbb deployment queue/own-filter Local subitem and closed review24's three defects. Earlier failed candidate remains failed; completeC/SE-03/original13/TD are not closed. User's ongoing authorization covers sending complete owner deliveries to the manager task without repeated approval.
- Delivered one incremental plan, docs/planning/xi-a0-a1-execution-readiness-2026-09-10.md. Five A0/C core files remain byte-identical; current54 migration files through28 are bound. Next implementation starts isolated from acceptedC, next migration29 or the next accepted sequential baseline; no branch merge or original checkout overwrite.
- Current read-only resource accounting includes review24 and25 once. Proposed fullA0 resource estimate explicitly retains failed physical state and existing RestoreToNewDatabase semantics: normal5.5GiB plus isolated recovery3.5GiB; currentfree cannot conservatively cover it. Proposed24GiB A0/31GiB free entry/sharedC3648MiB/new60-minute window are not applied. Exact two-copy conversion still needs package decision; no archive/current full physical comparison is claimed.
- OriginalA1/document46 and fixedlocalUR25f5c1f contract map three typed profiles to concreteXI gaps: legacy terminal decoder, empty-bootstrap rejection, per-profile checkpoint, source revision/identity/amount association and immutable reading interpretation/restore. No materialUR contract disagreement found. Recommended independently executable completeA1 synthetic consumer-to-reading subitem, proposedC3904MiB/new45-minute window; originalpurpose/manual correction/I03/real integration/fullA1 remain. This plan does not implement a consumer or contactUR.
- This turn only read source/control/allocation and created planning evidence, then checked hashes/readback/diff. No build/test/PG start/migration/copy/load, old-directory removal, cap/origin change, real credentials/API/traffic or push/release/deploy. Complete-subitem delivery and unchanged evidence reuse continue; no measured token/time saving claim.

## 2026-09-10 XI shared workbench — review28 UI package

- Continued accepted A1 a1af2be in the same isolated worktree with the complete review28 UI authorization. Unified shell/navigation/tokens, wide-screen comparison and contribution preview; retained current-role reading revalidation, queue filters/conflict recovery and actual case/execution return context. No schema, production Go, source contract or role permissions changed.
- Delivery and explicit remaining scope: [XI-SHARED-WORKBENCH-R28-DELIVERY-20260910.md](XI-SHARED-WORKBENCH-R28-DELIVERY-20260910.md). The current exclusive workbench/investigation roles remain exclusive; no combined-role end-to-end acceptance is claimed. A0/A1/B/D/original13 remain independently pending.
- UI failures and observer corrections remain in output/xi-ui-workbench-r28 and output/playwright/xi-ui-workbench-r28. Final binding identifies exact source/assets/evidence; old backend/native evidence is reused only where production files remain unchanged. User-authorized complete delivery goes directly to the manager without repeated approval.

## 2026-09-10 XI shared workbench — review29 narrow-screen correction

- Removed both body 320px minimums; page layout now fits actual available width. Regression checks use clientWidth and measured maximum page scroll, including an explicitly verified 15px scrollbar gutter. No overflow masking or smaller fonts.
- Actual synthetic production UI: cost/preview 320 gives clientWidth=scrollWidth=305 and max scroll0, same63requests/¥0.000270/reading; queue860px table inside241px container scrolls619px to its last column without page overflow. Both actual handlers serve the exact3 final assets. Existing18E2E and new6occupied-width tests pass; unchanged behavior/backend evidence is reused by500file hash binding.
- [Complete Local return](XI-OVERFLOW-R29-DELIVERY-20260910.md), with a separate next-permission-package plan only. Old candidate438f37a, manager counterexample and observer failures retained. Existing roles and original cumulative limits unchanged;14old database rows preserved, own Chrome/PG stopped. One complete handoff uses the user's standing manager-delivery authorization.


## 2026-09-10 XI review30 — formal scoped workchain Local package

- Implemented the complete approved v3 scoped reader/reviewer package: source/project/cost/field authorization, closed route allowlist, whole-case cumulative classification, grant/session ordering, schema30/native recovery and the same-account cost→investigation→review/history→original-reading UI. Existing roles, M0 hashes, A0/A1 reading semantics and original receipts retain their meaning; unclassified legacy cases and scoped seal/export remain closed.
- Two existing pools in reader→workspace order, small/normal native pools and TRUNCATE/DML/cancel barriers passed; no expanded pool or timeout. Mixed-scope rejection/rollback, partial revocation, receipt replay, 500 visible/2001 hidden queue, timezone-stable case binding and native29→30 backup/restore passed.
- Full Go vet/race run found five current-schema/readiness/count omissions. Corrected metadata and exact expectations, then targeted race/metadata verification passed; retained the failed run and reused unaffected results. Combined coverage total64.5/API61.1/DB70.9/hostctl64.5/workspace65.9 meets unchanged floors. Frontend121tests/coverage/build, finalOpenAPI and24E2E pass.
- Final Production HTTPS/native-v3 browser repeated creation, review1→2/history and unchanged reading xwr_zH4PuaTEy4ue_OhDutBjRQ/project-9000/1request/17micro-CNY on index-CMLbZ2Gv.js. Four final320px/theme views measured304px available/page/body width and maxScrollX0. Committed revocation closes prior data; 2s observation has51→51API requests. Three actualHTTP assets match manifest; schema30readyz200.
- Original15database catalog rows unchanged,3newretained; ownbrowser/HTTPS/PG stopped. Resource samples remain within approved5696MiB C and originalA0/free/RSS bounds; exact final binding and retained failures in output/xi-scoped-workchain-r30. Complete-subitem authorization and one delivery to manager adopted; no quantified token/time savings claimed. Original13/SE-03/A0/A1/TD and integrated-version acceptance are not closed; no push/PR/merge/deploy/UR or real account/data/credential work.

### 2026-09-10 R31 scoped preference methods and focus draft preservation — verification pending

- Manager independent review31 rejected R30 on personal queue-filter PUT/DELETE and unchanged-scope focus losing drafts. Implemented the exact method allowlist, background normal focus/pageshow checks, authorization-identity remount and stale-response invalidation; no schema, grant, lock or restore code changed.
- Frontend 130 tests pass (coverage 76.69/67.30/73.49/78.33), type/OpenAPI/final embedded build pass, 24 E2E pass. Initial unsupported DOM matchers and readonly test-fixture type error are retained in output/xi-scoped-workchain-r31; corrected tests pass.
- Fresh native API and final embedded real-account workchain remain unverified. Free headroom fell to roughly 50–60 MiB over the original 21 GiB floor. Manager confirmed resource gap and explicitly holds PG/browser stopped pending disposition; no original cap/origin/floor changed, no cleanup.
- Read-only exact resource options and full path allocation inventory: output/xi-scoped-workchain-r31/resource-options.md and temporary-path-inventory.json. Reusable caches provide only 5.09 MiB; retained hostctl/Go failure or unclassified scenes are not deletion candidates. Sent options to the authorized manager task; this is not a completed R31 delivery.

### 2026-09-11 R31 resumed under original limits — complete local repair candidate

- Original resource conditions recovered; 20.5GiB proposal withdrawn without approval or implementation. Reused exact stopped PG/system/base identity, no cleanup or budget-origin changes.
- Both formal scoped roles passed personal-filter CRUD, ownership/CSRF/Origin/read-only boundaries, stale-version recovery and replay under native PG and race. Final embedded UI passed new/update review draft retention across focus/pageshow, review version2, exact reading/contribution return, real personal-filter update/delete and committed revocation; no-scope DOM and 52→52 business requests over2s.
- 130 frontend tests/coverage/type/OpenAPI/build and 24 E2E retained; fresh changed-file-only Go coverage plus unchanged-source reuse passes original floors. One Fetch.status observer mistake preserved and corrected in the same scene; no business replay or product fix needed.
- Browser/harness closed normally; PG stopped with all18 original database catalog/ACL rows unchanged, no new retained DB and no clients. Full delivery entry: XI-SCOPED-WORKCHAIN-R31-DELIVERY-20260911.md. Manager independent acceptance pending; R32 code not started.

### 2026-09-11 review32 complete local repair candidate — manager acceptance pending

- Review32 independently closed original R31 preference and ordinary focus-draft findings, but returned exact cost document routes and hidden known-denial handling. Added only `/workbench/cost` to the document allowlist and made known scope invalidation clear retained content immediately regardless of visibility.
- 132 frontend tests/type/OpenAPI/build, existing 24 four-viewport E2E, and 9 targeted Go top-level tests with native reviewer/reader/race pass. Controlled hidden + real native 403 closes prior DOM before delayed Session returns; late Session and recovery failure covered. Same-account create/review-v2/normal-focus/exact-reading-return and three HTTP asset hashes pass.
- Reused 35 unchanged native files and 79 verified prior evidence files; changed webui.go uses only fresh coverage counts, all original floors pass. First API-status expectation and URL/rate-limit observer errors retained; no false full-suite or native-background claim.
- Browser/harness/PG stopped normally, original 18 DB catalog/ACL rows unchanged and no new retained DB; original resource origins and limits unchanged. Delivery entry: XI-REVIEW32-REPAIR-DELIVERY-20260911.md. Complete repair prepared for one independent review; R32 feature implementation excluded and awaits acceptance.

### 2026-09-11 R32 running reading continuity — complete local frontend candidate

- Manager review33 accepted 5869a780's scoped same-account Local subpackage and directly authorized the reviewed R32 plan under original limits. A0 load remains blocked; no new writer, worktree or approval loop.
- Added selected execution preview and shared URL/history references for fixed filters/cutoff/cursors/selection/view. Same-execution history, cross-request origin return, off-first-batch event lookup, missing cursor stack, changed-scope cursor recovery and stale-response guards are covered; canonical cutoff does not discard unsent filter input.
- 143 frontend tests/type/OpenAPI pass; four-project browser functional suite29pass/3duplicate-visual-skips. Final six-width light/dark visual pass includes real contrast assertions; final build and7embedded WebUI tests pass. Go/API/SQL/roles/schema unchanged;35native source hashes and81prior frozen evidence files verified and accepted evidence reused.
- Initial nullable type access, malformed test source_ref fixture and E2E bracket error retained; actual screenshot readability defects corrected using existing theme tokens. No PG/A0 startup or manual cleanup; original cumulative guard unchanged. Full entry: XI-RUNNING-READING-R32-DELIVERY-20260911.md. Independent R32 acceptance and formal-account/final integrated acceptance remain pending; original13/TD not closed.
