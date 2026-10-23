# X-Insight

[Foigent](https://github.com/foigent) · [UniRouter](https://github.com/foigent/unirouter) · [English（规范版本）](README.md) · 本译文对应 0.2.0-dev.fpv20。

X-Insight 是理解 AI 工作的证据工作台：网关观察到什么、哪些请求产生费用、Agent 报告做了什么，以及人在复核证据后作出了什么判断。

它异步接收来源记录，保留身份、修订、覆盖缺口与权限边界。时间线不证明因果；Agent 报告不等于文件结果已被独立核验；未知金额不按零处理。这些区别会保留在调查过程中。

X-Insight 可独立用于受支持的数据导入、工作活动报告、调查与人工复核。UniRouter 可独立执行和路由获授权模型请求、应用执行策略、记录用量与经济事实。需要从网关执行追踪到获授权证据工作台时，可以组合使用。X-Insight（XI）异步消费 UniRouter（UR）事实，不进入同步请求关键路径。

## 状态与支持范围

源码已按 **Apache-2.0 公开**。已发布预览版为 **0.2.0-dev.fpv20，schema 46**：[七份发布附件](https://github.com/foigent/x-insight/releases/tag/v0.2.0-dev.fpv20)与[安装入口](docs/community/getting-started.md)。完整产品源码包括访问控制、调查、工作台、连接器与受管安装代码，没有另一个付费功能树。认证、来源/项目授权、发布信任与租户边界继续强制执行。

当前为**单部署租户**的开发预览版，不是稳定版或 Production 发布。截至 2026-10-23，精确 FPV20 签名包已通过 Linux 制品资格、在一台既有 Ubuntu 22.04 amd64／固定容器 PostgreSQL 16 主机上的空库普通首装，以及 owner 正常 HTTPS 访问。普通已安装 UR→XI 配对和首条事实验收仍待完成。CLI 接受 Ubuntu 22.04／24.04 amd64，但一台主机通过不代表所有拓扑都已验收。源码、制品、主机和集成分别记录在[状态矩阵](docs/community/release-readiness.md)。

UI 默认英文，可以切换并保存简体中文偏好。同版离线手册位于 `/manual/en-US/` 与 `/manual/zh-CN/`。切换语言不改变 API 代码、来源身份、原始证据、币种或已保存阅读范围。

## 可以做什么

- 在固定、已授权的阅读中查看请求用途、CNY 费用、趋势、贡献和预算，核对成员与覆盖限制。
- 追踪请求结果、重试、逐次尝试、目的地与政策观察，不把尝试费用重复加到父请求。
- 查看执行证据、来源声明的 Trace 关系、采集缺口、来源与修订。
- 查看 Agent 报告的工作、资源读取与修改、模型关系，分别判断来源报告与独立核验。
- 主动读取获授权正文并核对完整字节与摘要，分别呈现采集、来源引用、XI 副本、到期和阅读权限。
- 建立调查、采用准确证据修订、记录人工判断、责任跟进与复核历史。
- 接入受支持的 UniRouter feed、来源事实与 OpenClaw 工作活动；协议兼容与真实启用仍须单独核对。

## 从这里开始

**安装 → 自己设置密码 → 登录 → 连接 UR → 确认授权与首条事实。**

1. 使用[签名预览包安装路线](docs/community/getting-started.md)：先核下载与主机条件，安装完成前由你通过隐藏输入设置自己的密码。使用已发布包不需要 Go、Node.js 或源码构建。
2. 按[普通接入与首条事实检查](docs/community/getting-started.md#connect-unirouter-and-verify-the-pair)完成两端管理员各自的确认；连接管理权限不等于每个普通用户的数据权限。
3. 同时保留[中文手册](docs/manual/zh-CN/current.md)、[FPV20 安装勘误](docs/community/releases/v0.2.0-dev.fpv20.md#fpv20-installation-erratum--fpv20-安装勘误)和[数据处理边界](docs/community/data-handling.md)。

源码构建、合成夹具、数据库／浏览器检查及历史配对配方放在次级[开发指南](docs/community/development.md)。这些是贡献者验证入口，不是额外的客户安装步骤。

## 架构关系

UniRouter 负责执行、准入、路由与经济事实；X-Insight 异步消费证据并承载人员工作台。网关请求不依赖 XI 页面或人工判断同步完成。Go 服务嵌入 React UI 和离线手册；导入、受限读取、工作台写入与管理员动作使用不同数据库权限。详见[架构说明](docs/community/architecture.md)。共享协议和兼容源码支撑真实适配器、发行检查、升级测试及许可来源，不是需要另装的产品；历史设计和验证记录保留为次级参考。

## 参与贡献

阅读[贡献指南](CONTRIBUTING.md)与[贡献地图](docs/community/contribution-map.md)。欢迎问题复现、翻译、文档与定向修复。英文文档为规范版本；中文讨论可以直接进行，重要决定补充简短英文摘要，不以英文流利程度筛选贡献者。

非敏感问题可使用 [GitHub issues](https://github.com/foigent/x-insight/issues)。漏洞细节先按[安全说明](SECURITY.md)处理，不假定仓库已启用 Discussions 或私密漏洞报告。

## 许可证

[Apache License 2.0](LICENSE)，版权归 X-Insight contributors。提交采用 [DCO 1.1](https://developercertificate.org/) 签署，不要求 CLA。第三方组件保留[原有许可](THIRD_PARTY_NOTICES.md)。
