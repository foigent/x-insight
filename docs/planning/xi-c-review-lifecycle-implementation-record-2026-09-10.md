# Finding 复核与跟进生命周期：实施与验证

状态：实现、原生数据库专项、真实页面链和按包 race/vet 验证已完成。review21 批准的完整 PG 补验现已使原 Go 覆盖率全部门槛通过（total64.0%、API58.7%、DB70.9%、hostctl64.5%、workspace62.8%）。此前无DSN门槛失败及中止记录保留；最新补验见下节。本包待经理最终独立验收，Local 结果不等于远端 CI、Artifact 或 Target 通过。

本包按 `independent-review-20/implementation-decision.md` 实施。批准方案为 `fe39ebd0b7be631972b4c1b506e172aaa99f13ec` / tree `8424b9e23895227f3090fdf9807ba5ed93f5009f`，方案 SHA256 `ae09085d1b695613a7ec01f9b60bbfcfdee1b6d8ba8622e78863c2c4dd9b0014`。产品基线 `fcda8832c8e5bf6e74f60277a66e91ab92723420` / tree `a5bfefacd79c06210c792eb3c0960991f6d0a447`。原批准方案字节未改；本记录报告实施事实。

## 产品结果与实现边界

有权操作者从实际所选 Evidence/Gap 开始复核：预览只读，一个正式命令原子创建可选新 Case、Finding、引用、初始复核/明确未开始进度、历史与创建回执。支持选择已有开放 Case；创建身份绑定当前部署、human actor、操作类型和 operation_id，不依赖尚未生成的 ID。相同命令重试返回原记录；冲突、无权、陈旧依据和提交失败不留下半记录。

复核结论、跟进进度和既有责任各有明确含义。未记录不被补成待复核/未开始；四种结束分别校验人工核验范围与时间及引用、反证排除、另一 Finding 的重复依据、或剩余未知。结束和有原因重开不自动改变结论；指派不赋权。原 classification/status、责任版本、`/review` 协议与旧回执保留。

`ReviewBasis` 区分当时已知与本次采用引用，保存实际采用的 GapResolution 版本、state/note/author/time，未记录解决内容显式为 recorded=false/version=0。一个 SQL 语句读取全部所选 Gap 行，避免同一 Gap 的多种关系拼出两个版本。预览和写入共用解析器，正式提交再次核对摘要；历史、封存和恢复使用已存快照，不连接当前解决行重建过去。当前读取单独提示变化或不可获得；不会覆盖旧结论、结束动作或旧包。

新投影、追加动作和创建回执使用 schema27 的三张有限表。新旧写入共用 Case 行锁和 Case.version；跨 Case 按固定顺序加锁，重复并入另用一个事务级图锁防止互不重叠的并发写入形成环。历史按各自固定版本上界分页，在页面按 Case.version 组合，没有复制责任状态/历史。续办建立新未复核、未登记责任的 Finding，指向旧封存 revision/digest；旧 sealed Case 不解封、旧包不变。

新增 `investigation_reader` 仅有 `evidence.read`，支持 v2 账号 schema；v1 和原五角色权限不变。8 个新 HTTP 路由逐一执行 human/权限约束，POST 预览同样检查 Origin/CSRF；命令拒绝额外/重复字段与伪造 actor。页面隐藏只读者的所有写入口，后端仍独立拒绝写入。本包没有引入项目级 Case ACL 或真实账号/权限修改。

EvidencePack v3 包含复核/进度及实际采用内容，v1/v2 仍按原存储 JSON 字节读取。迁移1—26的52个现有文件及正式 schema23 contract 均未改。开发 schema contract 由实际 PG27 生成，语义摘要 `sha256:05fa1cde6572f917ca11bdc1d7763aca49dd264e8f04936cb789eff3b5ff180c`；开发 buildinfo/测试跟随27，正式 rc.13/schema23 发布身份未变。无删除历史的 down 迁移。

使用说明见 `docs/manual/development/finding-review-and-followup.md`。正式离线手册绑定 rc.13/schema23，因此本包说明放在明确标注版本边界的开发候选指南，正式手册源码/生成物保持原版本，不宣称既有发布已获得新能力。

浏览器遇到结果未知时在 sessionStorage 保留精确命令，刷新后复用原 operation_id；收到回执后另行 GET 当前值，冲突要求重读，权限/导航变化不会把旧响应写成当前状态。存储数据有有限形状校验，坏记录保留并报错。真实暗色窄屏检查发现的新表单和旧 dossier 色彩问题已修正，最终证据来自重新构建的嵌入资产，未绕过 CSP。

## 验收对应与有效证据

下表路径均相对本工作树。真实数据库证据使用既有 PG16.14 / loopback49998、小规模合成记录、生产角色及正式 TLS/human handler；没有真实来源、客户或模型 API。hostctl 生命周期中的 PG、迁移和 pg_dump/restore 是真实执行，签名包、host 服务操作和 runtime identity 使用现有合成夹具，不能当作正式 Artifact/Target。

| 批准验收 | 本次证明与证据 |
| --- | --- |
| P2-A05/10、S04/06/09、P2-M03 | 新 Case/已有 Case 原子开始、同动作并发/重试、异内容冲突、中途提交故障无半记录、实际提交后响应丢失恢复。`assessment-native-expanded-01.log` 中相关独立测试通过；`assessment-native-race-final.log` 再以 race 验证原子/竞争/负向；真实 UI 见下节。 |
| P2-A02/07/09、S04/06 | 结论/进度分离、四种结束及拒绝不足依据、理由重开。领域测试、`TestAssessmentProductionLifecycleAndPermissions`、`TestAssessmentRelevantReferencesAndFiniteClosures`；前端85项测试中的引用选择和结束表单用例。 |
| P2-A08/10、S05/06 | 责任/期限与复核衔接、旧新命令共享版本冲突、回执不冒充当前值、同 key 不重复写。`assessment-native-race-final.log` 与完整 UI 链。 |
| P2-A11、S07 | 相关新增引用、已知未选、无关 Case 变化、Gap 新内容、同 Gap 双关系版本一致；Gap 更新与复核/结束/封存交错、seal 获锁后新写拒绝、跨 Case 并发重复成环拒绝。`assessment-native-expanded-01.log`、`assessment-native-race-final.log`。v3及续办保留原字节见真实 UI。 |
| P2-A12、S04/05/08 | 实际只读账号/会话/页面及 HTTP403；旧 v1 账号和迁移前会话兼容、原角色不扩权、Agent 拒绝、失联后退出会话重试401、CSRF/Origin/跨对象/actor/旧 URL/游标负向零写入。上述原生日志及 `assessment-native-race-final.log`。 |
| P2-A13、S08 | 实际26→27保留责任/v2原字节和旧会话，重开生产连接；原生备份恢复包含新回执/v3/Gap旧内容，read-only check不改变事实，篡改摘要/内容拒绝。`assessment-native-expanded-01.log` 的升级测试及 `assessment-native-restore-upgrade-02.log`。同后者覆盖 legacy9→27、23→27两个中断/恢复、rc.10合同和原生恢复/只读preview；日志一处旧文案写 target26，实际测试 artifact 参数为27，后续源码日志已更正，原日志未改。 |
| P2-A14、S09本地人工链 | 实际嵌入页面从 Evidence/Gap→原子开始→责任→跟进→结束/重开→Gap变化→重新复核→封存续办→只读回读。刷新/失联及最终亮色桌面/暗色窄屏已实际查看；详见下节。 |

`assessment-native-expanded-01.log` 的整组曾因恢复夹具数据库名过长而退出失败，不能将整份日志标为通过；其他逐项 PASS 保留，修正短前缀后的恢复/升级完整组另见 `assessment-native-restore-upgrade-02.log`。初次范围/角色/内容负向并未因此跳过。

## 真实页面、导出与最终资产

主链在 `output/playwright/xi-c-assessment-20260910/`。关键成功证据：`11-retry-after-refresh.log`、`15-responsibility.log`、`18-progress-start.log`、`21-ended.log`、`24-reopened.log`、`25-changed-basis.log`、`28-rereview.log`、`30-sealed.log`、`33-continuation.log`、`38-reader-verified.log`、`39-reader-result.log`。失败观察日志原样保留，不能只看 CLI 退出码判断成功。

创建动作 `f16e71a1-3b6f-428a-8d86-80eb522d5630` 实际提交后由浏览器路由丢弃响应；刷新和同命令重试最终返回同一 Case `xc_ONNRxTgWxUh-jtNaJZK3OA` / Finding `xf_5Cx3yFhC5TC6ebOGR1aAPg`。Gap 更新通过正式 human HTTP 路径完成，页面先保留旧内容并显示变化，再显式采用版本2。旧 Case 在 revision8 封存，含5个 assessment 动作及既有责任。

续办到已有开放 Case `xc_svdz5TtyGF1ZG6XUE7CLvQ`，新 Finding `xf_pPWJWVos38Ayw5NZtbxd7w` 的复核、进度、责任均未记录。只读者回读完整历史，创建/动作/封存/Gap写入全部403。原62827-byte封存 JSON 下载为 `sealed-v3.json`，SHA256 `0f0619363af7e7591c147ba4fb03ce54c79a6001ab48bdd93d9789712c91c10c`，续办前后旧包一致。

主题修复后用同一生产 harness 的有限预置记录验证最终嵌入构建，不重复已经通过的整条业务链。最终目录 `output/playwright/xi-c-assessment-theme-final-20260910/`，`dark-narrow-basis.png`、`dark-narrow-editor.png`、`light-desktop-editor.png`；`04-basis-assets.log`、`05-editor-final.log`、`06-index-parity.log` 证明实际 HTTP 资产与本地构建一致，窄屏无横向溢出。更早主题截图是修复前诊断，不能充当最终视觉通过证据。

| 最终嵌入文件 | SHA256 |
| --- | --- |
| `internal/webui/dist/index.html` | `1feef1300ab5c81703de0a8456d40956aadd2edd9907cab4cd90a97bde6bb6d3` |
| `internal/webui/dist/assets/index-CyVihCU5.js` | `4b425d14274f221f670a2b35488c699d92d31753217a1a6087da5f71d0aaa6d7` |
| `internal/webui/dist/assets/index-VXmGQ7cQ.css` | `be1bffdc31d6005e812c3e8352fe4c15cea781da9761fddb0858aac26eb11720` |

## 回归、失败分类与执行优化

前端使用既有 Node22.23.1/npm10.9.8：`assessment-frontend-coverage-final.log` 为7文件85测试通过，Statements74.63%、Branches65.62%、Functions72.60%、Lines76.55%，满足既有门槛。`assessment-frontend-checks-final.log` 包含 typecheck、62条必需 OpenAPI 路径和195个锁定依赖 license 检查；最终构建 `assessment-frontend-build-final-02.log`；已有浏览器回归 `assessment-frontend-e2e-final.log` 为18/18通过。没有安装依赖。

后端使用既有 Go1.25.11、GOPROXY=off、原 GOCACHE/TMPDIR/GOTMPDIR，`-p 1 -ldflags=-w` 只控制并行与测试符号。真实PG专项明确选择测试；整仓 race/coverage/vet 不提供 DSN 或专用测试开关，不能由本地无DSN覆盖率冒充 CI PostgreSQL覆盖率门槛。

`assessment-go-race-01.log` 包含 sandbox 禁止监听和开发 schema 版本断言未同步；后者已更正为27，正式版本不变。获本地监听授权后 `assessment-go-race-02.log` 中全部产品包通过 race/coverage，但末尾历史 `output/xi-c-responsibility/connectors-baseline-probe_test.go` 被当作独立测试包，因引用 api 内部 helper 编译失败。该片段现在仅加 `//go:build ignore`；原完整字节保存于 `connectors-baseline-probe_test.go.original.txt`，SHA256 `54f105f07260a22af186bdbfcbbb4b050d30da43343229eb4e897c124e0eba67`。没有屏蔽任何产品测试，也没有改写旧绑定或失败日志。

之后再次整仓运行并非最低成本路径：前次未变产品包可核对后复用；必须按实际测试开始时间和依赖图核对，不能只看日志结束时间。最终包图还识别出运行期间修改的API负向/浏览器测试夹具、DB与releasebundle的开发schema断言，以及主题嵌入资产。资产的间接依赖包括hostctl，也须补证。`assessment-go-race-final.log` 在已完成包无产品失败时被资源守卫中止，整条命令未通过。最终按包补验与覆盖检查结果如下；不以分段通过隐藏中止状态。

最终37个产品包的集合由 `go list -json ./...` 确认：11包复用race-02，22包采用被中止final中已经完成的结果，releasebundle/webui采用 `assessment-go-race-affected.log`，hostctl及schema-contract命令包采用 `assessment-go-race-hostctl.log`。后者真实完成hostctl race267.906s；所有有测试包通过，无测试包明确标注。最终源/测试/嵌入输入摘要、修改时间与实际依赖图保存在 `assessment-go-reuse-inputs.json`；go.mod/go.sum未变，`go mod verify`通过。当前摘要结合保留修改/执行顺序及文件修改时间佐证复用，不声称事后获得当时未保存的历史内容摘要。

`assessment-go-coverage-provenance.json` 明确每包只选一份最新已完成profile，排除中止包和错误目录；`assessment-coverage-combined.out` 共19622个唯一block，没有重复叠加计数，也没有为未采coverage的原生专项虚构数据。最终 `go vet -p 1 ./...`、gofmt与源码/文档差异检查通过；原始工具日志保留其表格空白和末尾空行，不为格式检查改写原始证据。正式手册用原生成器重建后与HEAD字节完全相同（`assessment-manual-final.log`），开发指南不改写正式手册版本。

**b780fbbb 交付时原覆盖率门槛未通过。** 当时真实执行未修改的 `scripts/check-go-coverage.py`，结果 `assessment-go-coverage-gate-final.log`：total48.6% <63%、api48.1% <53%、db3.1% <69%、hostctl55.7% <64%；workspace62.8% ≥46%。退出1。CI在配置PG服务的测试条件下检查这些门槛，而当时授权明确整仓不带DSN/专用开关；真实PG仅有选定专项，因此数据不能证明CI覆盖门槛或无回退，也不能据此独立断定新增代码造成覆盖率回退。该准确缺口促成 review21 完整补验授权；原日志和绑定未改写，最新结果如下。

浏览器准备中曾发生结果检查早于请求结束、APIRequest相对URL无base、CLI VM没有TextEncoder及全页截图超时；修正观察方式后保留了真实成功链。暗色可读性则是真实产品缺陷，已修复并实际复核。CSP拒绝内联样式探针后使用正式重建路径，未降低CSP。上述返工属于本包成本，未逐实验要求经理设计或批准。

本次优化保留迁移、权限、恢复、并发和真实页面这些必要验证，压缩协调轮次、重复上下文和无变化测试；完整工作包内由同一owner收敛，只在完整交付或实质阻塞时主动通知经理。没有优化前后同条件耗时/token对照，不能声称节省比例。

## review21 完整覆盖率补验

批准方案 `docs/planning/xi-c-review-lifecycle-coverage-plan-2026-09-10.md`（SHA256 `7d44c55133d6638003dcd64963bda5d20c927927a0c1c4b309f321da874467e9`）和只读事实（SHA256 `b718e14d40611efe5a94894c5164b66f6c14aa7e537f802fded5fc97d3685379`）字节保持不变；其中“待批”是提交时状态。`independent-review-21/coverage-decision.md` 随后批准同一 owner 完成六包补验、必要小测试修正和本地提交。新增证据统一位于 `output/xi-c-responsibility/coverage-20260910/`。

修正两处已存在于产品基线的前置不匹配：schema23 精确前态升级当前集合的断言从固定2改为当前迁移校验数量减23，保留 single/duplicate/conflict 与数据语义；CI 只改 backend_validation 自有临时 PG 的主机为127.0.0.1并显式开启原本地合成夹具标志。生产源码、正式版本、迁移与资产没有变化，也未触发远端CI。

首轮 `regular-01.log` 中 API、checkpoint 与两 Feed 包完整通过；DB 因 Connector 夹具假设固定角色不存在而失败，整条命令仍记失败。复用集群已有合成角色，因此夹具仅在明确127.0.0.1/本地资格开关下核验非特权、NOINHERIT、无角色成员关系后复用，保留LOGIN/凭据且不注册旧角色删除；新角色的原创建/清理路径不变。`db-02.log` 整包通过但66.9%尚未达69%；随后补上已有责任 Store 的真实组合行为。最终 `db-03.log` 全包 race 通过（6.909s，70.9%）。

新增小规模 Store 集成测试复用一个 Evidence/Gap 和已迁移夹具，覆盖只读预览、陈旧依据零半记录、原子创建/重复回执/异内容拒绝，责任与复核共享 Case 版本但保持独立状态，失效账号历史姓名保留、逾期随跟进结束消失，Gap 当前2→3不改历史采用内容、重新复核不自动重开，固定上界历史分页、封存回执重放、续办到已有开放Case且未伪造新复核，以及责任/复核恢复校验与篡改内容拒绝。没有新增 Skip 或为抬比例修改生产实现。

`hostctl-01.log` 完整选定原生资格与全部 ConnectorDigest 通过（20.178s），含实际角色权限、备份/新库恢复、schema9/23/rc.10前态与DATA_RESET。它自身profile为27.4%；与最终输入相同的原完整无DSN hostctl profile按唯一block取已覆盖并集，最终64.5%。五个普通PG包采用各自最新完整profile替换，另外31包保留有效结果。340份原Go输入均已核对，只有三份DB测试变更和一份新增DB测试；go.mod/go.sum、其余包源码/测试及前端嵌入输入不变，原日志/profile摘要均匹配。

`provenance.json` 记录37包选择、最终343份Go/模块输入、原始profile摘要、复用限制和19,622个唯一block。`combined.out` 27,531条语句中17,627条真实覆盖，原脚本 `gate.log` 退出0：total64.0≥63、API58.7≥53、DB70.9≥69、hostctl64.5≥64、workspace62.8≥46。没有改变 `-coverpkg`、分母、门槛或给未采集专项补写计数。这是多次完成包的可核对组合结果，不是一次完整整仓命令通过。`vet.log` 为最终DB vet通过；YAML解析、仅目标job环境变化、gofmt、源码/文档差异检查通过。前端/真实UI输入未变，复用既有有效证据。

结束观察器曾错误假设原hostctl库还存在，读回失败；源 `database_integration_test.go:787` 与已通过资格表明该新建前驱库由原DATA_RESET生命周期退役，不能据此判产品失败。保留 `observer-correction.json`；仅重启49998补一次按实际数据库目录的只读状态核对并再次正常停止，没有重跑负载、重建库或另做清理。最终保留普通库 `xic_cov_reg_20260910` 和原生创建的 `xinsight_clean_qualification_7eee4153b294`，无其他client连接；三种应用角色仍非特权/NOINHERIT，所有状态见 `database-final-before-stop.json`。这次观察器返工仍属可压缩成本，不掩盖或宣称零返工。

review21 累计上限为2,368MiB（2,483,027,968 bytes），不重置原起点。当前补验窗口57个采样最大1,979,232,256、最小free38,417,821,696 bytes，全部在本窗口上限/20GiB底线内；此前22:59:44Z越界保留。最终快照1,944,358,912/free38,459,752,448，尚余538,669,056 bytes供收尾与经理复核；样本不声称连续峰值。冻结回读确认A0全部361+9输入/固定binary、旧52迁移/正式文件一致，模板及三旧目录存在，原工作树干净、PG已停。未运行A0、浏览器、admin rotation、真实组件或外部N1获取；本地同门槛通过与远端CI/发布/Target边界分开。

## 资源、冻结与待办

review20 批准原累计上限1,600MiB（1,677,721,600 bytes），owner与经理共享，baseline SHA256 `bf8e3b9087608b56d5ca85a776c5eefcaa7dfa64c4e28b3ef41806aea9f47c4a` 和缓存/git计量起点不变。22:59:44Z守卫采样到1,677,922,304 bytes，随即停止负载；23:00:45Z回读1,678,069,760 bytes、free38,772,797,440。未清理保留目录、重设baseline或自行扩大额度。经理在 `independent-review-20/resource-amendment.md` 批准增加256MiB，有效累计总上限改为1,856MiB（1,946,157,056 bytes），继续供owner和经理合用；guard随后应用该上限，原baseline不变。1秒采样不是连续峰值证明。

`assessment-freeze-final.json` 核对A0的361源码绑定、9准备绑定和固定二进制 `ab3c14624597b149bea302313c70060306aad2b456152ea3a40425b581d042b9` 一致，模板和三旧目录保留；旧52迁移/formal23一致，原工作树干净。PG49998及本包测试浏览器已停止，没有A0负载或既有服务改动。冻结核对证明绑定/存在性，不冒称本次重做模板物理全量校验。

本包只覆盖P2-M03及其必需的只读/依据/续办/恢复；P2-A01完整队列、A03/04/06合格来源、A14真实来源父级与后续P2-M02/M04、跨项目权限、A1/B/D/X、原13项及最终集成验收保留。TD-01：新增能力不冒称修复旧缺陷；TD-02：复用Case事务/引用解析，源事实与人工复核/责任权威分开；TD-03：旧责任协议、v1/v2与正式前态因现实兼容义务保留，新路径没有复制旧状态；TD-04：本包证据与未完成父级分别记录。A0原28GiB资格入口和D-37-01未关闭。经理资源补充决定指出当前空闲已超过28GiB，旧三目录删除不再是空间足够时的必需前置；仍须先完成本包独立验收，再重核冻结输入/前态/空间，安排不重叠的原A0资格窗口。三目录继续保留，删除仍未获批准。

当前未push/PR/merge/release/deploy、未联系UR、未调用真实来源/模型/客户API、未改真实凭据权限/流量或删除用户现场。完整子项仍须经理独立验收。
