# X-Insight

[Foigent](https://github.com/foigent) · [UniRouter](https://github.com/foigent/unirouter) · [English（规范版本）](README.md) · 本译文对应 0.2.0-dev.fpv19。

X-Insight 是理解 AI 工作的证据工作台：网关观察到什么、哪些请求产生费用、Agent 报告做了什么，以及人在复核证据后作出了什么判断。

它异步接收来源记录，保留身份、修订、覆盖缺口与权限边界。时间线不证明因果；Agent 报告不等于文件结果已被独立核验；未知金额不按零处理。这些区别会保留在调查过程中。

X-Insight 可独立用于受支持的数据导入、工作活动报告、调查与人工复核。UniRouter 可独立执行和路由获授权模型请求、应用执行策略、记录用量与经济事实。需要从网关执行追踪到获授权证据工作台时，可以组合使用。X-Insight（XI）异步消费 UniRouter（UR）事实，不进入同步请求关键路径。

## 状态与支持范围

当前源码候选为 **0.2.0-dev.fpv19，schema 45**。完整产品源码采用 Apache-2.0，包括访问控制、调查、工作台、连接器与受管安装代码；没有另一个付费功能树。认证、来源/项目授权、发布信任与租户边界继续强制执行。

当前为**单部署租户**的开发候选。受管验证配置要求隔离的 Ubuntu 24.04 amd64、systemd 与独立原生 PostgreSQL 16。仓库保留历史 Ubuntu 22.04 验证通道和冻结 rc13 配置，不能互相替代支持承诺。本页不提供尚未建立的公开稳定下载或无人值守社区安装承诺。Local、CI、签名制品、受管安装、外部集成和 Production 分别验收，详见[发布准备状态](docs/community/release-readiness.md)。

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

1. [本地构建、测试与运行](docs/community/getting-started.md)：先使用合成夹具，离线检查不需要付费模型。
2. [当前中文手册](docs/manual/zh-CN/current.md)：登录、来源接入、阅读、正文、调查、安装与恢复。
3. 接入真实来源或启用正文前，阅读[数据处理边界](docs/community/data-handling.md)。
4. [接入 UniRouter 并核对已验配对](docs/community/getting-started.md#connect-unirouter-and-verify-the-pair)：先阅读 UniRouter 的[源码与安装入口](https://github.com/foigent/unirouter/blob/main/docs/community/getting-started.md)，分别核对两端真实发布状态；各自测试通过不等于联合验收通过。

固定构建工具为 Go 1.25.12、Node.js 22.23.1、npm 10.9.8、Python 3；数据库测试需要 PostgreSQL 16。浏览器测试使用 frontend lockfile 中的 Playwright。发布构建不使用浮动工具链。

## 架构关系

UniRouter 负责执行、准入、路由与经济事实；X-Insight 异步消费证据并承载人员工作台。网关请求不依赖 XI 页面或人工判断同步完成。Go 服务嵌入 React UI 和离线手册；导入、受限读取、工作台写入与管理员动作使用不同数据库权限。详见[架构说明](docs/community/architecture.md)。

## 参与贡献

阅读[贡献指南](CONTRIBUTING.md)与[贡献地图](docs/community/contribution-map.md)。欢迎问题复现、翻译、文档与定向修复。英文文档为规范版本；中文讨论可以直接进行，重要决定补充简短英文摘要，不以英文流利程度筛选贡献者。

非敏感问题可使用 [GitHub issues](https://github.com/foigent/x-insight/issues)。漏洞细节先按[安全说明](SECURITY.md)处理，不假定仓库已启用 Discussions 或私密漏洞报告。

## 许可证

[Apache License 2.0](LICENSE)，版权归 X-Insight contributors。提交采用 [DCO 1.1](https://developercertificate.org/) 签署，不要求 CLA。第三方组件保留[原有许可](THIRD_PARTY_NOTICES.md)。
