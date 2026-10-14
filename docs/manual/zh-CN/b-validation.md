# 当前 B 隔离候选：能力与运行工作台

## 版本与适用范围

本页对应 `0.2.0-dev.b2`，手册版本为 `0.2.0-dev.b2/manual-r1`。它用于已授权的 XI
隔离验证环境。先核对签名 manifest、二进制 `version --json` 与本页身份；文件名不能代替核验。

| 项目 | 当前值 |
| --- | --- |
| 数据库 schema | 35 |
| PostgreSQL | 16，专用实例或数据库，仅同机 loopback |
| 验证主机 | 既有 Ubuntu 24.04 / Linux amd64 XI guest，systemd |
| 配置 schema | `xinsight.config.v1` |
| Investigation / Access API | `xinsight.investigation.v1` / `xinsight.access.v1` |
| 本候选允许的升级前态 | 精确 F4 `0.2.0-dev.f4` / schema32 或 B1 `0.2.0-dev.b1` / schema35 制品与安装状态 |
| 应用回滚 | 不声明 schema35 可运行旧应用；数据库恢复必须显式执行 |

本包的签名、原生安装、升级和恢复是否通过，以相同 commit、制品摘要和环境的执行回执为准。
源码中的 rc.13 发布配置与历史手册仍保留原身份，不构成本候选的发布授权或升级白名单。
当前没有客户发布、真实增强来源、完整 F 配对或最终 V 通过的声明。

## 从六个页面进入同一条工作链

实际入口由登录角色和字段授权决定；界面未显示某一页不等于来源没有对应事实。

| 页面 | 主要工作与下钻入口 |
| --- | --- |
| 总览 | 从需要解释的执行、Alert 或已有 Trace 进入执行详情 |
| 使用与目的 | 固定期间与贡献集合，再查看所选贡献的依据 |
| 运行与执行 | 查看请求、运行观察、能力、策略与目的地；可切换能力专题 |
| 安全与复核 | 从调查队列、案件及其关联执行进入详情 |
| 费用与使用 | 沿既有经济事实查看金额、用量、贡献和依据 |
| 来源与覆盖 | 核对已登记连接器状态，再按该来源查看执行 |

执行详情共用 Story、Causal graph、Chronology、Proof、系统与数据五个视图。
从运行或费用阅读下钻再返回时，保留原阅读、期间、筛选与所选项；过期、撤权或会话失效须明确处理，
不能把新建阅读伪装为原分母。旧费用与复核阅读、A v1 和已有执行深链继续保持各自合同。

## 阅读、请求与当前观察

先选择时间范围、来源及项目，再读取历史。阅读固定接收截止时间、授权范围、成员与口径。
“进入时间已知”和“进入时间未知”是不同集合；未知时间不得猜测填入当天或补成零耗时。
请求使用来源、实例、组织、项目和请求标识的完整身份，同名请求不自动合并。

未产生 I-01 经济事实的请求仍可在运行页出现。没有金额权限或没有经济关联，不等于费用为零；
观察、attempt、节点或图邻居不会增加经济分母，也不会另建金额权威。
耗时按有依据的 measurement 类型、单位、样本数与版本解释；不平均分组 P95，缺失值不当作零。

“当前租约观察”与历史阅读分开。界面依据来源给出的有效期判断当前性；迟到响应不能延长租约，
分页中的不同观察时间也不能合成一个虚构的同时状态。过期、断源、暂停或撤权后，保留适用的
历史依据并标明当前未知；没有出现在快照中不能推断请求结束。

能力声明、当次能力证据、策略配置、执行结果及已观察到的效果分开阅读。
shadow/enforce 不等于策略已改变输出；本地发送或响应头也不能证明远端收到全部输入或输出。
地区、输入绑定、所有权或实际目的地没有证据时显示未知。

## 全部分支与系统资料

路径摘要只用于快速阅读。展开“全部可见分支与关系”后，桌面图和手机列表保留同一组当前可见
节点及显式关系；每个节点均可用键盘进入，并沿用原截止时间。缺边不按时间补连，未出现在当前
节点集合中的端点不通过关系列表补出。折叠、展开及页面返回不改变成员或费用。

“系统与数据”展示独立获得授权的资源记录、版本、转换说明和冻结的输入引用。
企业知识库、个人电脑资料、未知账户归属和撤回状态分开呈现；“输入引用相符”只说明引用匹配，
不证明内容已经独立核验或远端已收到。迟到撤回不覆盖旧阅读原文，但可能要求重评；新阅读可显示
撤回后的状态。缺少前驱的记录可以显示缺链，匹配的后到记录只使新阅读完整；已知相邻版本的哈希矛盾拒绝写入，旧阅读保持原截止时的链状态。当前授权撤销会使旧入口失效，恢复备份也不会重新授予当前访问资格。

资源领域目前只有明确合成的本地正向验证。没有具备资格的生产 I-03 导入入口；合成企业/PC
资料不能替代真实资源、输入传输或增强来源的供数证据。

## 权限与人员 CLI

运行、attempt、能力、策略和目的地按独立字段授权读取；A 身份、路由、经济事实和资源资料另行
授权。采集权限与人员查看权限分开，默认不因为新版本增加字段而自动授予它们。
后端使用真实 reader、workspace、importer、admin 角色；Web 不获得导入、账户维护或数据库管理能力。

人员查询沿同一 HTTPS Session、CSRF 和当前授权边界执行。密码通过受支持的交互输入取得，不能
放进命令参数、日志或报告。例如，使用已获得的阅读 ID 查询原集合：

```text
xinsightctl query runtime summary --origin https://APPROVED_HOST --username REVIEWER --reading xrt_READING_ID --entry-time known
```

`query runtime` 还提供 create、reading、requests、request、observations、runtime、attempts、capabilities、
policies、destinations、associations、resources、measurements、dimensions 和 current 子项。
只使用对应子项允许的参数；当前快照与历史阅读参数不能互换。人员 CLI 不写入资源或经济事实。

HTTP 查询使用 `/api/v1/runtime/readings` 及该阅读下的受限子资源；返回中的 reading ID、成员摘要、
合同与口径版本应沿调用链保留。401 需要重新认证；403 表示当前请求不获准；409/410 需要按明确
失效状态处理。429 应遵循服务端 Retry-After，不能通过抬高权限、关闭限流或保留失效内容绕过。

## 接收、安装、升级与恢复

A v1 不隐式接收 B。明确版本的 A+B 配置最多列出 16 条有界流，每条仍保留自己的 scope、profile、
cursor 与生命周期；当前库存不推进事件 cursor。B 离线授权还绑定目标 schema35、精确构建、文件
摘要、时间范围、目标和 operation，默认禁用；检查通过不产生之后导入的永久许可。

只有已有授权覆盖的精确 XI 目标才可执行管理操作。先核对测试信任文件、已验证的控制器、安装
状态、数据库身份、可用空间与恢复路径。fresh install 只接受空专用 PostgreSQL 16 数据库；升级
使用已验证的目标控制器，并保留原 schema32 数据与备份，不把空库当成含数据升级证明。

```text
sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl install --check --artifact /approved/path/BUNDLE
sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl install --artifact /approved/path/BUNDLE
sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl upgrade --check --artifact /approved/path/BUNDLE
sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl upgrade --artifact /approved/path/BUNDLE
```

`--check` 是只读预检，不是安装或迁移通过。发生中断时保留 operation 与失败现场；按同一目标
控制器的 `recover`、`resume` 及实际状态恢复，不能覆盖失败的不可变候选。
恢复使用准确的 PostgreSQL custom-format 备份、摘要及新的 `xinsight_restore_*` 数据库名：

```text
sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl restore --check --backup /approved/path/BACKUP --backup-sha256 sha256:DIGEST --new-database xinsight_restore_QUALIFICATION
sudo /approved/staging/xinsight-NEXT-COMMIT/bin/xinsightctl restore --backup /approved/path/BACKUP --backup-sha256 sha256:DIGEST --new-database xinsight_restore_QUALIFICATION --reason-code field_validation
```

恢复后核对事实、阅读成员、版本与 hash；采集暂停、离线授权禁用、撤权不复活，缓存中的租约不
当作实时状态。应用回滚、数据库恢复和重新启用采集是不同动作，后两者不因恢复回执而自动获准。
缺表、权限、触发器、原文、摘要、cursor 或制品绑定不符时保留准确失败，不降级校验继续。
