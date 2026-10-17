# 来源、溯源与采集缺口

## 七种字段状态

| 标签 | 含义 | 正确说法 |
| --- | --- | --- |
| `direct` | 值由规范化来源直接提供 | “来源事件直接提供该值” |
| `derived` | X-Insight 由已保存字段按确定规则派生 | “X-Insight 从已记录字段派生” |
| `declared` | 来源声明该关系/属性，尚非独立证明 | “来源声明 parent/policy” |
| `suppressed` | 按批准策略有意不展示或不捕获 | “该字段被策略抑制” |
| `gap` | 已存在具体缺失/不可获得证据 | “存在 capture gap” |
| `unknown` | 当前证据不能确定 | “当前未知” |
| `not_applicable` | 该 field group 对此事件不适用 | “此事件不适用该字段组” |

`suppressed` 不等于值不存在，`gap` 不等于系统失败，`unknown` 不等于默认允许或拒绝，
`not_applicable` 也不能用于隐藏本应存在的证据。

## Source inspection

Source 面板用于核对 `source_system_id`、source type/display name/version、adapter manifest ID、
schema version、source event identity/ref、event hash、source event hash 和 fidelity。预留的
source type/schema 字段只是兼容性信息，不证明存在可运行 adapter。

hash 证明保存字节或规范化事件的一致性，不证明发布者真实性、语义完整性或因果关系；发布
artifact 的真实性由独立 Ed25519 trust/signature 链负责。

## Capture gap

Gap 面板区分 confirmed gap 与 candidate gap，并显示 scope、source ref、时间和可用原因。
常见类型包括 `permission_denied`、`not_available_from_source`、`not_instrumented`、
`sampled_out`、`retention_expired`。candidate 只能作为排查方向，不能升级成确认事实。

`freshness.sync_state=gap` 或 `timeline_complete=false` 表示当前 timeline 不能作为完整过程。此时
可报告已观察事实，但必须同时报告缺口和未知，不得补画“最可能”的 node。

## Proof class

`synthetic_fixture` 只证明产品路径和预期 UI/API 行为。批准 sanitized metadata 可证明特定现场
输入经过受控路径被保存和读取，但不自动成为 External integration 或 Production verified。
状态提升必须有准确 target、contract、artifact 和独立记录。
