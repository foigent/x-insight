# 试用退出、保留与移交

当前默认：专用 sanitized trial database 和相关备份保留到现场结束后七个自然日，然后由
custodian 明确选择移交或受控退役。X-Insight 不自动删除客户数据、artifact、backup、receipt
或 evidence。

## 现场结束清单

- 冻结准确 release/commit、artifact/manifest hash、key ID、兼容矩阵和手册版本。
- 导出安装/升级/恢复 receipt、Target 监听/readiness 和调查任务结果。
- 将 fixture 与批准 sanitized evidence 分开列出，记录 custodian、路径、数据库、备份和期限。
- 明确继续保留、移交给客户，或退役；未决定时不删除。
- External integration、Production 和独立手册盲测未运行时保持 `Not run`。

## 受控退役规则

删除前先列出所有副本并核对每个准确路径、类型、owner、hash/数据库身份和保留约束。每次只
处理一个明确文件路径或一个明确数据库对象；禁止递归、通配符、批量删除和未经复核的脚本。
每次动作后立即写新的清理证据，不修改或回写历史 receipt/evidence。

目录、PostgreSQL data directory、整个 backup tree 或多份 artifact 需要批量处置时，停止自动
操作，由客户 IT/用户按其变更流程手工完成。保留 receipts、hashes、sanitized evidence 和
persisted normalized evidence，除非 custodian 的明确决定逐项覆盖它们。

## 移交最小包

移交包只包含批准内容：版本/签名 receipt、兼容矩阵、操作 receipt、安全审查后的日志片段、
数据清单和保留决定。不得包含 private signing key、数据库密码、raw prompt/response、未批准
snapshot 或内部开发规划文档。
