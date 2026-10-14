# XI-A0 最终普通入口5次验证结果

**本轮5/5普通入口创建成功；最终候选定向验证完成，A0及20次p95资格仍未通过。** 仅本轮启动线按经理批准从23改为22GiB，其他资源/SQL/持久性/权限/数据约束保持。本地没有Go改动、重编译或新副本，没有资格重跑或512MB实验。

基线commit `4360de03ec4fa1fc81e153333e3d3532ed4dd3cf`、tree `effe80bb53f36215f73abebc8379bbed5b20e0ae`。复用 `/private/tmp/xi-read-mode-build-saaq_bb0/db.test`，SHA256 `ab3c14624597b149bea302313c70060306aad2b456152ea3a40425b581d042b9`，361输入逐项匹配。当前runner/plan与复用二进制独立绑定在[新准备](../../output/xi-a0-read-mode-final-execution-ready/code-binding.json)，原final-ready及旧23GiB未执行证据原字节保留。正向22GiB、22GiB少1byte拒绝、512MiB预算少1byte拒绝和旧23GiB入口正反向均离线通过，没有启动PG。

## 实际普通入口、模式与耗时

实际PG16.14、workspace PID77565、port62781、pgx默认CacheStatement/cache512；无auto_explain，真实非super workspace SQL10s。固定原fixture、正常 `createWorkbenchReading` 入口，只使用既有阶段observer，无selector覆盖。该入口与public Store wrapper共用，但本轮没有HTTP请求。

| 成员 | 派生模式 | 完整创建ms | count | INSERT | digest | coverage |
| ---: | --- | ---: | ---: | ---: | ---: | ---: |
| 0 | auto | 7.405 | 0.903 | 0.451 | 0.693 | 2.765 |
| 16 | auto | 6.228 | 1.406 | 0.679 | 0.232 | 2.835 |
| 69999 | auto | 3489.429 | 423.469 | 1766.435 | 256.093 | 1042.228 |
| 70000 | generic | 1747.682 | 58.588 | 831.641 | 245.981 | 609.435 |
| 300000 | generic | 4287.654 | 44.478 | 3483.207 | 372.464 | 385.792 |

前3次均未切换模式；后2次在INSERT后局部force_generic_plan，只覆盖digest/coverage，在header收尾前恢复auto。同一PID/auto进入及退出；prepared同名/SQL/OID/prepare_time保持，digest和coverage各自计数从G0/C1、G0/C2、G0/C3到G1/C3、G2/C3。5次count均有QueryExecModeExec驱动选项，未进入named prepared缓存；其余实际业务参数与原SQL的subject/time/source[]/project[]/cost[]顺序精确匹配。stdlib前置的QueryResultFormatsByOID已按真实源码规范化，不将它误读为$1。

[95条原始trace及样本](../../output/xi-a0-read-mode-final-run/diagnostic.json)、[可复算审计](../../output/xi-a0-read-mode-final-run/analyze.py)、[审计结果](../../output/xi-a0-read-mode-final-run/analysis.json)覆盖以上结论。4.288秒是本次300k单样本，不是p95；69999与70000的count/INSERT耗时也变化，不能将总时间差全归于模式阈值。>=70000仍是已测有益规模的保守起点，不声称最优临界值。

## 状态和语义

六表计数按header/member/saved_filter/operation/F0/evidence顺序从 `[30,6740032,0,0,500020,500100]` 到 `[35,7180047,0,0,500020,500100]`。原30个header的完整行逐字段不变，新增正好下列5个ID、440015成员，无失败或替换样本。

| 成员 | 新reading ID | 完整membership_ref |
| ---: | --- | --- |
| 0 | `xwr_k79wec52CwR0OZHbTxtH5w` | `sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` |
| 16 | `xwr_Lus69sslVUBzxGhOPcrD8Q` | `sha256:83a342a50ff6ae912850af2bdd7e87212211bf9471d00c24909fb50d50636e6b` |
| 69999 | `xwr_3qgkb19B7l6TQhLuN_nxiQ` | `sha256:f09cd1086034a778691eb68de9e505bd87bfb2723238de32ac5b749a53b32795` |
| 70000 | `xwr_7PmvXlngpkUfK-XR5xyVEw` | `sha256:eb4eb2c7be9c674f4a67ccda03ac2de9f419f1cf166a9c622d3e5bf55d6b4d3a` |
| 300000 | `xwr_AKZpqBzwCeIahIciN3FjNQ` | `sha256:bc008bc4c4771660899c4659b8b1815d14999fce75acc347b54651391a0bb9a1` |

每条returned/readback coverage及完整ref一致，并与不依赖member表、直接按F0时间/项目/排序计算的独立ref一致；summary count和两种精确micros均等于成员数。计时后独立ordinal查询按F0 `(occurred_at,id)` 排序检查，5条mismatch均0，非空ordinal为1…N，空成员min/max为空。原header快照及[ordinal原始结果](../../output/xi-a0-read-mode-final-run/ordinal-readback.json)保留。

选定表统计中只有member的n_live_tup6733321→7173336、n_mod_since_analyze2540032→2980047变化，reltuples/last_autoanalyze等未变；估计计数不冒充上面的实际精确计数。成员table+index1790091264→1903951872bytes，增113860608bytes；WAL目录起止均771751936bytes。没有在此短窗口内观测到自然checkpoint；日志只有正常停库checkpoint（25段回收），不能声称持续checkpoint压力合格。

## 资源、停库及证据边界

独立窗口 `2026-09-09T09:19:06.457727+00:00` 至 `2026-09-09T09:19:27.149700+00:00`，共20.691973秒；Go测试PASS11.99秒。32条记录资源检查全部在界内：峰值累计4293110459bytes，峰值RSS681394176bytes，最低free23485755392bytes。startup free23751757824满足22GiB；累计4058249915。首次69999前累计4058299067、剩余639321413bytes（>=512MiB），free23751442432（>=21.5GiB）。持续4480MiB/21GiB停止/20GiB保留/RSS4GiB/SQL10s全部保持；采样不是磁盘硬配额。

复用实例已fast shutdown并回读shut down。原资格来源1604文件运行前后完全一致、始终停库；新执行准备显式计入原final-ready和新目录，10个既有二进制保留。原44绑定在4360逐项成立，允许的本轮runner/plan/执行记录增量在新交付绑定中单列。原始输出哈希不回写，新增审计/提案/交付校验由新总绑定覆盖。

## 上轮调用数量纠正与后续

上轮校准trace实际为**12次repeatable-read Store调用**：8次配对+2次空成员恢复+1次context取消+1次无grant拒绝。11次到达header，其中10提交、1取消；无grant在header前拒绝。旧raw `planned_store_calls=11`漏计已授权拒绝检查，旧原始记录及报告保留，本派生报告和脚本明确纠正。本轮恰为5调用/5header/5提交，无额外拒绝或恢复创建。

已完成[原共同空成员模板资格方案](../planning/xi-a0-final-code-qualification-plan-2026-09-09.md)及只读预检，尚未执行。建议复用同一最终二进制，单一新资格副本、7GiB新增/28GiB启动；09:22:15UTC实际free23598960640，缺6465810432bytes约6.02GiB，须与经理共同落定再执行。原20×300k全成功且p95<=5s、500k/500001、增长后读取/HTTP、13项/TD01—04/D-37-01及UR禁联继续保留。本轮只提供Local定向成功，不替代A0或更高环境证据。
