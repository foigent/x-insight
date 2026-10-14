# XI-A0 资格资源处置与执行准备（供经理评审）

**推荐按①→②→③的条件顺序退役本任务临时PG副本；本轮只完成只读核实、证据保留和runner准备，未删除、未复制、未启动。** 原空模板与原失败资格源保留。实际处置范围和资格窗口须经理共同落定；不把已有22GiB单次许可扩展到资格运行。

基线commit `0f3a04808d900b5d9ad02f4946a30217ef83614a` / tree `54846a611e2f98d0300c3a1fdd1c61c902046533`。原5次定向交付已经经理独立接受，A0仍未通过。核验仅覆盖经理指定且有本任务创建记录的三个目录，没有扫描用户无关目录找可删内容。

## 精确候选与处置价值

以下编号仅是处置次序，**不代表已获删除许可**。路径指向子目录，不允许扩大到其父级临时根或原工作树。

| 编号／精确目录 | 创建和当前内容证据 | 当前allocated（bytes） | 后续必要性与建议 |
| --- | --- | ---: | --- |
| ① `/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-checkpoint-run-bjep8yon/qualification` | capacity-run/run.json：04:41:22UTC创建；PG因socket路径>103bytes在接受连接前失败，没有目标Store调用。1599个数据文件与原空模板的bytes/SHA256完全一致，另1份PG日志；root仅data、postgres.log | 2169147392 | 新runner改用短根路径，失败证据已完整保存，资格仍从原空模板复制。优先退役此冗余副本 |
| ② `/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-checkpoint-run-eeh92u0h/B1` | checkpoint/run.json：04:01:14–04:01:50UTC的4GB WAL臂；6×300k及后置rollback EXPLAIN，已否定晋级。1604文件，root仅data、postgres.log；当前文件末次修改止于该停库时段 | 2718380032 | 4GB路径未采用，下一资格用1GB空模板，不依赖B1物理布局。原性能/查询/回滚/资源证据保留；第二顺位退役。旁边的template绝不包括在目标中 |
| ③ `/private/tmp/xi-pc-hcpwpsza` | plan-cache-run/run.json记录从原失败资格源逐regular-file clonefile创建；随后已验收只读计划诊断、唯一300k创建、模式校准和最终5次。当前1594文件含9份PG日志；data完整manifest匹配0f3a048保留停库快照，逻辑状态35headers/7180047members | 3796156416 | 原目标要求空成员起点，当前增长副本不是资格源。其已接受诊断无待执行目标；仅若①②后实际free仍不足28GiB，再建议退役。会失去原35个随机reading的物理现场，不能声称可字节级重造 |

三者当前均为普通目录、无symlink/特殊文件、无外部tablespace、无postmaster.pid、pg_controldata为shut down；未发现归属不明的附加文件。①的纯合成来源有完整字节相同证明；②有原复制/测量/停库记录、已知PG布局及同一结束时段mtime；③还有当前完整停库manifest匹配。不能从这些证据推断任意其他目录可处置。

[机器可读盘点](../../output/xi-a0-resource-preparation/resource-inventory.json)与[只读盘点脚本](../../output/xi-a0-resource-preparation/inventory.py)保留三份完整文件内容/权限/元数据manifest。脚本没有删除/启动入口。snapshot记录是本轮新证据，不回写历史输出。

## 仍在引用的路径，以及退役影响

- ①被 `output/xi-a0-capacity-run/run.json`、`output/xi-a0-capacity-run-02/run.json` 的失败留存/资源carry路径引用。后者已结束；旧`xi-a0-checkpoint-diagnostic.py --qualify`有existing run.json保护，不是新资格的执行入口。新 `xi-a0-final-qualification.py`不引用①。退役后历史占用记录仍是当时事实，不改成零。
- ②是旧checkpoint runner的`retained_nonqualifying_arm`，只有重新选4GB反序/诊断才需要复用。该路径已否定，当前方案不含这种复用。新资格只读取仓库B1.json中不随WAL改变的约束/触发器定义作为检查依据；B1.json不会删除，运行数据不来自B1目录。
- ③由 `xi-a0-plan-cache-diagnostic.py` 的恢复/MCV/coverage入口、`xi-a0-auto-explain-diagnostic.py`、`xi-a0-read-mode-diagnostic.py`及旧resource-config引用。它们记录已完成阶段或已保留失败，不能原地重跑补样。删除会使这些“复用原物理副本”的历史入口无法继续；这正是退役的影响。新资格runner只导入旧helper的inventory/allocation/resources/common工具，不调用指向③的旧prepare/execute/source_preflight，也不继承旧累计resource路径。
- 原失败资格 `/private/tmp/xi-cap-7qgkhj7u/qualification/data`（19headers/5.7M原失败现场）仍保留，可支持确有需要的后续诊断；原共同空模板仍是新资格唯一来源。当前不需要继续在35/7180047现场追加负载。

删除旧物理副本不撤销历史结果；也不能让旧目录引用看似仍可用。批准处置后，在新处置记录中逐一登记精确路径、前manifest、实际删除时间/结果和free前后差值；原run.json、code-binding、evidence-binding、raw hashes保持原字节。不会修改旧脚本指向其他数据库以冒充旧现场。

## 必须保留的原始证据与重建边界

所有以下产物和二进制均在三个候选目录之外，已核对可读/哈希。没有为了处置复制第二份同样庞大的PG目录。

| 候选 | 保留内容 | 可重建什么／不能重建什么 |
| --- | --- | --- |
| ① | output/xi-a0-capacity-run 的7项raw manifest、fixture、代码绑定、原socket FATAL日志、零目标run；原template1599清单；本轮socket-failure-manifest.json | 原空模板可恢复逐字节相同的PG数据；长socket失败的日志已保留，无需重演。新的短路径恢复不是原失败运行 |
| ② | output/xi-a0-checkpoint 的30项raw，包括B1.json、B1.log、B1-observations、B1-explain及其观察、B1-postgres、模板匹配、代码/fixture；本轮wal-b1-manifest.json；旧binary `/var/folders/48/r2my3shx3yqb_v8ngzvygg8m0000gn/T/xi-checkpoint-build-bsjbfpem/db.test`（8aa4873c…） | 可从同一空模板和旧固定binary/4GB参数重建同语义6×300k及诊断；reading随机ID、物理页布局、缓存/时间不能保证同一，不把新测量当原B1 |
| ③ | output/xi-a0-plan-cache-*、coverage-*、auto-create-*、read-mode-*原始结果/trace/SQL参数/计划/资源/失败/源码快照/绑定，最终headers前后/ordinal/停库manifest；本轮plan-cache-manifest.json；既有10二进制及最终ab3c1462 | 可从保留的原19/5.7M失败资格源新建诊断副本，按保留fixture/代码重建同语义20/6M→30/6740032→35/7180047过程。随机新ID、物理布局、统计、性能不能字节级复刻。当前没有依赖原35 ID再验证的未完成要求，退役需接受此限制 |

11份目录内PG日志均已在对应仓库output找到字节相同副本；没有未归档日志。本轮同时核验①7项、旧A/B30项和原失败资格10项raw hash，以及三个旧构建绑定的二进制；最终10二进制继续沿已接受证据保留，不在处置目标内。原用户工作树、49保护文件、未跟踪predecessor-link、孵化合同/产物、Go cache、所有output目录都排除。

## 预计空间与最少必要执行次序

09:48:06UTC实际free23583801344bytes，距28GiB（30064771072）缺6480969728bytes约6.04GiB。①②allocated合计4887527424bytes约4.55GiB；即使完全释放，也仍约差1593442304bytes约1.48GiB。③为CoW副本：3796156416只是其文件st_blocks总量，原资格源仍拥有共享块，**不等于可释放量**。三者allocated总和8683683840也不承诺会把free推至按allocated加总得出的数值；①②即使来自普通完整复制也只作预算参考，文件系统/其他进程变化仍须实测。

建议经理批准精确的条件序列后再执行：

1. 每项前重核身份、内容/manifest、停库和日志已保留；有变化或未知数据即停止该项。仅对当前精确子目录使用标准目录移除，不用glob、不扩大父目录、不开发通用清理工具。
2. 先①，再测一次free；不足再②，再测一次。达到28GiB即可停止处置，保留其余副本。
3. 仍不足时，在同次明确批准包含③及丢弃其物理现场的前提下，才处理③，再实测。CoW共享使释放量无法预先保证，不能删完就直接假定条件成立。
4. 三项后仍不足28GiB，保留全部新证据并停止资源动作；不碰空模板/原失败资格源，不扩大到未知目录，不降低7GiB预算或28GiB入口。经理再共同选择明确的额外任务临时资源或有容量的执行位置；本轮没有核实其他候选，也不承诺有额外可释放资源。

这是收回已完成阶段可再生副本占用，不是回收原始证据。移动到同卷废纸篓/改名不能增加free，因此不以这种操作冒充空间解决。上述处置与资格复制/启动本轮均未执行。

## 新资格runner：已准备的精确行为

新增 `scripts/xi-a0-final-qualification.py`，复用原common监测/PG管理函数和原qualify测试；原已绑定runner全部保留。默认仅prepare，`--execute`实现供审查，当前不调用。产物为 `output/xi-a0-final-qualification-ready`，实际结果将进入新的 `output/xi-a0-final-code-qualification`；拒绝覆盖已存在准备/执行。

- 复用 `/private/tmp/xi-read-mode-build-saaq_bb0/db.test`，SHA256 `ab3c14624597b149bea302313c70060306aad2b456152ea3a40425b581d042b9`；361输入逐项核对，不重编译、不改Go/SQL/schema。现候选是schema25：启动后检查完整25项迁移ID/checksum/dirty清单，并比对已实测FK/immutable trigger定义，不猜测只看版本号即可。
- 仅原空模板完整copytree一次。源启动前后完整manifest匹配；副本byte/type/owner/mode一致，无symlink/外部tablespace/ACL。macOS provenance xattr不是PG内容语义，source仍完整保护；不承诺普通copytree与clonefile具有相同xattr复制行为。复制后比较数据bytes/SHA256及真实权限，不弱化源校验。
- 独立7GiB新增，28GiB在分配前及复制前检查；复制后/启动前检查新增<7GiB、free>21GiB、RSS<4GiB及总窗口<20分钟，不错误要求已经分配副本后仍有28GiB。计入新运行根、ready/run、此次资源准备输出和同一Go cache新增长；旧保留副本与二进制已体现在真实free，不按“可删”预减。
- PG16.14，WAL1GB/shared128MB/work4MB/wal_buffers4MB、checkpoint5min/0.9、durability/正常autovacuum与workspace SQL10s不变；短socket目录。管理员启动后必须读回0/0/0/0/500020/500100、mode auto/preload空、迁移/约束/guard一致，现有测试另核真实workspace身份/能力。异常在20次前拒绝。
- 仍使用原 `TestWorkbenchCheckpointDiagnostic` qualify：20×300k所有样本保留，20全成功且nearest-rank p95<=5s才通过；SQL57014只在回滚计数正确后继续原定剩余尝试，不补样。达到20/6M后运行500k及增长后Store读取；即使20次p95失败仍可取得独立容量读数，整体仍失败。测试10分钟context/12分钟binary timeout，外层20分钟不变。
- 任一资源/输入/测试失败保留实际结果，取消owned workspace、fast停止，无重试/删除。成功预期21headers/6500000members，其余四表不变；最后读回精确计数。source停库不启动，退出前重新检查源manifest。`complete`仅指此Store资格切片，不是A0或HTTP通过。

离线检查涵盖28GiB纯边界正反、预算/输入绑定、错误前态拒绝和默认prepare无复制/PG副作用；具体结果见新的ready与resource-preparation产物。它们不替代尚未执行的真实复制、SQL预检、20次性能或HTTP验证。资源方案共同落定且现场达到门槛后才执行。

原500001、HTTP3秒与Store5秒预算问题、增长后10主体600请求/20RPS×30秒及summary/top各300、读取p95<=2s、生命周期/恢复、原13项/TD01—04/D-37-01和UR禁联保持。后续独立XI规划见[接续与债务表](xi-continuation-and-debt-review-2026-09-09.md)，本轮不扩大产品实施。
