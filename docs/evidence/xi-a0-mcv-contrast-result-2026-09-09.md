# XI-A0：digest统计对照有效，coverage参数错误使对照未完成

**本轮执行了18次目标SELECT，但只有digest的9次可用于批准的对照；coverage的9次参数错误，全部排除。** digest证实：相同300000成员基数下，非MCV ID的custom计划估为1行并执行300000次F0索引访问，G/C中位数294.459/1030.703ms。coverage仍缺有效MCV/非MCV对照；原A0和完整创建资格未通过。

经理批准基于`755e16f690f32559f34d54a088f74b6ad3904a90`的新18目标/20分钟只读切片，复用`/private/tmp/xi-pc-hcpwpsza/data`；条件性资源修订为4192MiB累计和23GiB启动，其余512MiB/21.5GiB首次目标、21GiB停止/20GiB保护、RSS4GiB、SQL10s均保留。已独立认可复用场景的2GiB启动至停止缓冲，并按编译后实际计量执行，没有新复制或清理。

## 执行、错误与实际证据边界

1. 离线准备通过编译、OID/分类回归和vet。预检发现输入元数据继承了旧七轮/36次说明，在PG启动前修正，并保留初始输入、绑定及hash清单；SQL正文和当时绑定参数未因这次元数据修正改变。沙箱内`/bin/ps`不可用时尚未启动PG，失败记录保留，随后通过平台审批执行同一已授权命令，没有取消RSS检查。
2. 首次PG启动06:49:55 UTC，真实workspace PID51122；读回19 headers/5700000 members、两个精确ID各300000且同ref。观察器将其他列的布尔MCV也解码为字符串，目标执行数0，测试exit1后正常停库。原始pg_stats显示指定reading_id分类仍成立，因此这是观察器错误，不是无效统计前态或产品SQL失败。
3. 保留该二进制、源码/runner/计划快照和失败输出，改为筛选table/column后才严格解码reading_id字符串，增加混合布尔/数值及非法reading_id类型回归。恢复计时沿用首次06:49:51.391011 UTC起点，累计资源包含第四个二进制和修复成本；没有重置18目标或20分钟预算。
4. 恢复输出`output/xi-a0-plan-cache-mcv-run-02`，端口57884、workspace PID51962；18条目标执行均SQL成功、测试9.24秒/exit0。包括2条auto及16条实际prepared EXPLAIN ANALYZE，全部同PID、SQL/OID/name/prepare_time，强制模式的计数增量正确；统计分类持续符合指定MCV/非MCV。
5. **事后参数语义审查发现本人的映射错误：digest的reading_id确在$1，coverage的$1是subject、reading_id在$2。harness及输入生成却都替换了第一个参数。** coverage自动执行的subject被换成reading ID；强制对照交错改变subject，却始终用MCV reading作为$2。真实角色/PID/参数类型一致没有证明参数业务含义正确；coverage自动结果中授权来源数为0，也暴露了此错误。

所以原始run.json/diagnostic.json的`complete=true`、18次success及测试PASS只表示当时程序完成执行，不是完整有效诊断。保留这些原始值，不回写伪装成执行中已经识别错误；[analysis.json](../../output/xi-a0-plan-cache-mcv-run-02/analysis.json)逐条比对批准参考参数，明确标记9条有效digest与9条无效coverage。此前“coverage没有同样差异”的初步解释已撤回。首次0目标失败及更早1+36次均独立保留，没有合并样本。

## 有效digest结果

两个精确ID为MCV `xwr_vzD1s7lPgy1RE3-Trd5V5Q`和非MCV `xwr_XMf3SdgKSIV7KJ_kxrItBg`；本轮均实际读回300000成员，存储ref同为`sha256:bc008bc4c4771660899c4659b8b1815d14999fce75acc347b54651391a0bb9a1`。成员统计n_distinct=14，MCV分类符合预期；35份选定表统计快照一致，未观察到本轮ANALYZE/autoanalyze变化。

| reading输入 | G1 ms | C1 ms | C2 ms | G2 ms | G中位数 ms | C中位数 ms |
|---|---:|---:|---:|---:|---:|---:|
| MCV | 415.651 | 342.587 | 408.164 | 288.460 | 352.055 | 375.375 |
| 非MCV | 294.396 | 1092.891 | 968.514 | 294.522 | 294.459 | 1030.703 |

真实顺序为G(M),G(N),C(M),C(N),C(N),C(M),G(N),G(M)，并非按表中一行连续执行。两ID各自满足G/C/C/G；样本量小、存在预热波动，不能当作p95或外推一般速度。此前MCV auto准备666.398ms，返回digest与ref一致。EXPLAIN不返回digest值正文，非MCV的存储ref核验不能冒充重新计算后digest值校验。

非MCV custom两次均为Nested Loop：成员主键Index Scan估1行、实际300000行，F0主键Index Scan循环300000次，估算总代价16.99。根shared hit/read分别1307288/195367与1313156/189499，均无temp块。generic总代价206274.81，成员估算379381，走并行Bitmap成员扫描及F0 Hash Join、排序/聚合；其temp写约18797–18799块，但耗时约294ms。MCV custom估算391976并采用相近并行路径，未出现非MCV的1行崩塌。

这直接支持**当前既有行中非MCV选择率低估导致digest差计划**；原最后成功创建digest为973.975ms，与本轮非MCV custom区间接近，属于支持线索。原会话没有计划快照，本轮没有新INSERT、事务内刚写成员或相应脏页/FK状态，不能回填原会话的直接因果证据，也不能据此解释INSERT超时。

coverage的9条执行只作为错误现场保存，不计算可采纳的G/C性能结论。它们的custom计划仍估MCV成员391976，授权来源为空使昂贵gap分支不执行；这些行为与错误绑定相符，不能证明非MCV coverage不慢。上一轮755e16f的有效coverage证据保持其原边界，本轮没有补齐缺口。

## 已完成的离线修复

保留18目标运行的`runtime-source.go.txt`、`runtime-runner.py.txt`、`execution-queries.json`和`execution-plan.md`后，再修复当前源码。通过原批准输入的唯一动态reading参数识别位置（digest=$1，coverage=$2），并在PG连接前把两条SQL的正文/hash、参数类型、reading位置和全部非reading参数逐项与原参考输入比较。subject、过滤器或时间参数变化会拒绝运行。

新的真实输入文件校验、错误subject/过滤器负向测试、混合类型统计回归、OID回归、默认跳过及vet通过。把本次实际错误的execution-queries.json交给修复版离线校验，明确得到预期拒绝`ambiguous reading parameter in coverage`，日志保留，没有启动PG。

修复版二进制`/private/tmp/xi-pc-arg-fix-gt3mi52u/db.test`，SHA256 `8999885eb8ea0e457585cce366fcda67b81c5613e2a7eb1169df0ac6079540e9`；359输入、runner及正确/参考参数绑定见[argument-fix/code-binding.json](../../output/xi-a0-plan-cache-mcv-argument-fix/code-binding.json)。**此版仅离线验证，未执行任何PG目标查询。** 实际18目标二进制为`/private/tmp/xi-pc-mcv-build-dtjm1a56/db.test`，SHA256 `c590d34a794309a053072fa9bba380dee85402f34fb50fedd4b24be36c17196c`，仍保留。

## 资源与停库核验

合并首次0目标运行和恢复运行的10次后台采样、18次目标前检查及边界：最大累计3992458975 bytes，最大RSS982056960 bytes，最低真实空闲25195134976 bytes；所有记录均在4192MiB/4GiB/21GiB内。恢复首条目标前累计3838101215 bytes、余量557529377 bytes，真实空闲25349165056 bytes，满足512MiB和21.5GiB；两次PG启动前均超过23GiB。

恢复自身周期16.214678秒；从首次原始起点到最终检查241.184188秒，修复/停库间隔包含在同一20分钟窗口。源1604文件before/after一致；交付再次核对源和复用实例均shut down。两次PG日志各只有停库checkpoint，没有新增复制、旧场景清理或生产默认修改。观测峰值不是瞬时峰值保证。

再完成参数修复的第五个保留二进制后，累计3876000400 bytes、预算余量519630192 bytes，约495.56MiB；仍未超4192MiB，但比下一轮首次目标所需512MiB少17240720 bytes（约16.44MiB）。真实空闲25204658176 bytes。见[修复后资源快照](../../output/xi-a0-plan-cache-mcv-argument-fix/resource-snapshot.json)；没有为了补差额删除缓存/二进制或放宽门槛。

## 下一步产品候选与qualification建议（未执行）

**先补齐coverage有效证据，再共同决定产品修改。** 本轮“最多18目标”已用尽，且当前首次目标预算余量不足512MiB，所以没有追加样本或重新执行完整18次。建议经理审阅一个仅coverage的9目标新切片：同一副本和两个ID重查后，1次MCV auto加8次交错G/C/C/G，使用修复后的参数合同，必须保持原subject与其他参数。digest已有有效对照，不需重做。

coverage-only入口还需小范围测试/编译；按本轮每次保留构建约36–39MB增量及当前16.44MiB门槛缺口估算，建议新增最多64MiB累计余量至4256MiB（4462739456 bytes），保留23GiB启动/512MiB与21.5GiB首次目标/21GiB停止、RSS4GiB/SQL10s及新的独立20分钟。此项不是现有授权，不能自动采用；应先经理审阅，再以实际编译计量决定是否能运行，不把估算当保证。无效前态仍停止，不换ID或修改统计。

最小产品候选仍限于被证据支持的查询：count单独使用`pgx.QueryExecModeExec`绕开命名缓存；digest可评估在其单条调用周围使用事务局部force_generic_plan并恢复原值，保留SQL/绑定参数、不改变连接全局默认。后者是待测设计，需验证模式恢复、错误回滚、真实workspace结果一致性及额外调用成本，不能直接推广给coverage。两项均未实施，不把诊断强制模式冒充产品路径已验证。

原成功19个样本的INSERT阶段中位数3697.113ms、nearest-rank p95为7082.444ms，3次单独超过5秒；另有第10次INSERT的57014失败。因此，即使count约0.25秒及digest约0.74秒改善可转移，也不能从这组历史数据推断完整创建p95≤5秒。选候选前还需提出真实新建事务内INSERT/coverage计划与写入状态的最小诊断，不能把只读SELECT优化替代剩余瓶颈。

最终qualification保持原要求：最终代码/二进制、验证为空成员的共同模板、真实workspace、1GB/128MB/4MB与持久性、10s SQL不变，20×300k全部成功且p95≤5s，达到6M后500k，再完成summary/top/hourly、选定组全量分页、ordinal/money/ref及实际HTTP/lifecycle。当前HTTP3秒实现预算未改；其决策和完整资格方案仍须结合整体目标共同落定。Store/诊断通过不能代替HTTP或A0。

## 复核入口及完成状态

[原始18次执行](../../output/xi-a0-plan-cache-mcv-run-02/diagnostic.json)、[逐参数有效性审计](../../output/xi-a0-plan-cache-mcv-run-02/analysis.json)、[0目标观察器失败](../../output/xi-a0-plan-cache-mcv-run/diagnostic.log)、[修复后真实输入校验](../../output/xi-a0-plan-cache-mcv-argument-fix/offline-tests.log)、[错误输入被拒绝](../../output/xi-a0-plan-cache-mcv-argument-fix/rejected-runtime-input.log)、[交付核验](../../output/xi-a0-plan-cache-mcv-run-02/delivery-verification.json)和[最终字节绑定](../../output/xi-a0-plan-cache-mcv-run-02/evidence-binding.json)。

交付状态为**部分诊断有效、coverage未完成、两处harness错误已离线修复**；不请求接受完整18目标对照。原13项/TD/R3/A0及D-37-01未改变。只有本地提交，没有push/PR/merge/release/deploy、真实凭据/业务数据、UR联系或跨任务消息。
