# 产品概览与术语

X-Insight 是异步调查与证据解释平面。它从持久化 evidence events 派生 execution、timeline、
selected-node、source/provenance 和 capture-gap 视图；UI node 不是新的事实源，也不会回写
来源事实。

## 核心对象

| 术语 | 含义 | 不代表 |
| --- | --- | --- |
| execution | 由稳定相关标识聚合的一次执行视图 | 新生成的一份来源日志 |
| evidence event | 带来源、时间、字段组和 provenance 的规范化证据 | 对完整真实过程的保证 |
| timeline | 按规范化有效时间稳定排序的 evidence events | 已证明的因果链 |
| declared parent | 来源声明的父事件关系 | X-Insight 独立证明的因果关系 |
| source | evidence 的来源系统与版本身份 | 预留字段已经是 adapter |
| provenance | 字段如何获得或为何不可见 | 字段值必然正确 |
| capture gap | 已知缺失、不可获得、无权限或未采集的证据边界 | 对缺失内容的猜测 |
| proof class | `synthetic_fixture` 或批准证据类别 | Production verified |
| freshness | source/import/search 水位和完整性状态 | 自动实时同步承诺 |
| story | outcome-first 的确定性执行摘要 | LLM 补写的原因或意图 |
| journey summary | 发起者、动作、来源声明系统路径与终点的四步摘要 | 对缺失身份或关系的猜测 |
| causal DAG | 只由来源声明 parent 构造的可导航图 | 把时间相邻节点推断为因果 |
| case | 调查员管理的本地 dossier | 新的 Evidence 或对来源事实的改写 |
| finding | 带分类、状态和明确 Evidence/Gap citation 的调查陈述 | 自动成为已证实事实 |
| evidence pack | Case 某个 sealed revision 的确定性 JSON/HTML 导出 | raw source dump 或可变 live view |
| authoritative session | 来源明确提供的 Session/Thread 及 execution link | 按时间接近自动聚类的会话 |
| discovery hypothesis | 有 support score、Evidence refs、limitations/abstention 的候选模式 | 校准概率、策略结论或自动执行指令 |
| connector trust view | 分离展示来源契约、交付运行状态和 Checkpoint 摘要 | 把 cursor、retry 或 worker 状态写入 Evidence |

## 时间与因果

`effective_at` 用于产品 timeline 的稳定顺序；`occurred_at`、`ingested_at` 和来源时间用于
解释采集过程。多个时间不一致时，UI 会标注，而不是改写来源。declared parent 未解析时，
仍保留声明和 gap，不把相邻事件画成已证实因果。

## 数据最小化

当前产品只面向 metadata。任何 request ID、actor、model、policy、usage、错误和来源引用都
必须先通过批准的 sanitized 规则。`metadata_only` 描述捕获模式，不保证每个字段都安全；
custodian 仍需审查输入。

## Known / Unknown 纪律

调查 UI 把声明分为直接观察、来源声明、确定性派生、假设与未知。Capture Gap 是可查询的一等
Evidence 边界；设置 resolution state 只记录调查进展，不删除 Gap，也不把未知变成已知。
Compare 中 `Missing`、`Partial` 与数值 `0` 不可互换。Discovery 证据不足时必须 abstain，不能
为了填满图表生成结论。

## 产品本地对象与不可变 Evidence

Case、Finding、Annotation、Saved View、service identity、Feedback、Alert link 和 Gap
resolution 存在于产品本地 Workspace/分析层。Evidence citation 同时保存稳定 ID、hash/source
ref snapshot 与 relation；Finding 可以分别关联 supports、contradicts、context 或 limits 的
Evidence/Gap。sealed Case 固定 revision、digest 与 pack bytes，之后不能静默编辑。
