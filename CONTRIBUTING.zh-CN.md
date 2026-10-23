# 贡献指南

[English（规范版本）](CONTRIBUTING.md) · 对应 0.2.0-dev.fpv20。

从具体问题、可复现示例或期望的用户结果开始。小修复和文档纠正可以直接提交 PR；涉及来源合同、授权、数据库迁移、发布信任或安装支持的大改动，先讨论设计。

阅读[开发与验证指南](docs/community/development.md)及[贡献地图](docs/community/contribution-map.md)；使用发布包的用户则从[安装路线](docs/community/getting-started.md)开始。贡献使用独立分支与合成、最小化夹具。保留无关修改，不提交 Token、账号导出、客户正文、私有目标地址或运维记录。按受影响路径运行必要检查，跳过不算通过，Local 不替代 CI、Target、外部集成或 Production。

英文无后缀文档为规范版本；中文使用 `.zh-CN.md` 并链接英文版本。语言专属嵌入手册分别位于 `en-US` 和 `zh-CN`。译文滞后时明确标注过期并链接规范版本。欢迎中文讨论，重要决定补充简短英文摘要，不以英文流利程度作为门槛。

贡献采用 [DCO 1.1](https://developercertificate.org/)。阅读并确认有权提交后使用 `git commit -s`，在提交中公开你选择的真实姓名与可联系邮箱。签署证明提交权利，不转让版权；没有 CLA。维护者人工检查，不因本指南自动启用 DCO gate 或修改分支保护。

PR 说明问题、结果、实际验证、兼容影响及未完成边界。问题分类沿用 `bug`、`enhancement`、`documentation`、`good first issue`、`help wanted`；这些文件不表示远端标签已经配置。参与时遵守[行为准则](CODE_OF_CONDUCT.md)，漏洞报告遵循[安全说明](SECURITY.md)。
