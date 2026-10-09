# Changelog

## 0.1.0-dev.4 — 2026-10-09 · 开发中

- 新增默认关闭、固定实例与指定 UID 的 Unix socket IPC，双向 SO_PEERCRED 身份校验和保守的端点恢复。
- 新增 executor serve/info、instance 查询及 sandbox --ipc；仍只有隔离操作，无 VPN 数据面。
- Web 可显式接入隔离 IPC；鉴权/CSRF、二次确认、审计与删除互锁，区分纯计划和实际隔离状态。
- 限制帧大小、连接数和操作并发，超时与断开取消，拒绝任意命令/路径和虚假的隧道能力。
- 新增协议/API/跨 UID/真实 HTTP到内核测试；结果见开发记录。已发布 dev.2 标签及资产不变。


## 0.1.0-dev.3 — 2026-10-09 · 开发中

- dev.2 Release 已发布，并完成临时 Ubuntu runner 的首轮真实隔离验证。
- 避免损坏的 FIFO 归属文件、锁文件或 namespace 占位对象在类型校验前造成阻塞，仍严格拒绝非预期对象。
- 补充真实 Linux 活跃进程、锁/重启、文件类型/权限测试和有效 cgroup 配额检查。
- 手动实验限定单核 CPU 配额、512MiB 内存、零 swap、64 tasks，并留存环境/结果证据；7项主测试及6个文件子用例已在临时 Ubuntu runner 实際通过。
- 仍未接通 OpenVPN、DNS 或代理出站；已发布 dev.2 的标签和压缩包不变。


## 0.1.0-dev.2 — 2026-10-09

开发中的隔离底座迭代，不是 VPN 网关发行版。

- 新增稳定实例 ID、SQLite schema v2 迁移、只读隔离计划 API 与出口详情预览。
- 新增独立 Linux sandbox CLI，支持计划、检查、显式准备/清理；仅无外部链路的全阻断 namespace。
- 增加资源归属记录、锁、namespace 身份核验、固定可信命令、结构化 nft 规则核验与有界回滚。
- 增加故障注入、恶意/异常 JSON、迁移与 API 回归、真实 Linux 可选实验和手动 CI 工作流。
- 继续阻止所有真实出口启动；未接入特权 IPC、OpenVPN、DNS、TUN 或代理拨号器。

升级前备份数据目录。旧 dev.1 拒绝读取 schema v2 数据库；回退需恢复对应旧备份。已执行和待执行的验证详见 docs/PROGRESS.md。


## 0.1.0-dev.1 — 2026-10-08

首个本机控制面开发快照。新增 SQLite 持久化、账号与会话、出口配置与凭据、官方节点解析、只读诊断、React 控制台、HTTP/SOCKS5 TCP 协议核心和测试设施。

本版本不提供 Linux VPN 执行器、活动代理、断线保护、真实住宅分类、热备、Xray 或集中式 Agent。所有启动请求明确返回执行器不可用，不应部署为公网代理服务。
