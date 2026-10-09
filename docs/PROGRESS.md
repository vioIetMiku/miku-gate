# 开发进度与验证记录

日期：2026-10-09。当前代码版本：0.1.0-dev.4（开发中）；已发布快照：v0.1.0-dev.2。**这是控制面 + 实验性隔离底座，尚不是能代理上网的 VPN 网关。**

## 本轮 IPC 开发

- 已实现限定操作、固定实例、指定非 root UID 的 Unix socket IPC；默认关闭、双向 peer credential 与 root 路径校验。
- 已实现 executor serve/info、instance 查询、sandbox --ipc，以及显式接入的 Web 状态/确认操作/审计/删除互锁。
- 已加入跨 UID、伪服务器拒绝、重启恢复和真实 HTTP→IPC→内核实验，等待 dev.4 的远端实测结果。
- 本机 Go 测试/vet、37项真实 HTTP/CLI smoke、前端6项单元测试、TypeScript/Vite构建、Chrome完整旅程已通过。IPC解码器5秒有界fuzz执行637,737次无崩溃。Linux结果按提交单独追加。
- 这仍不是 OpenVPN 或代理出口；已发布 dev.2 资产未替换。

## 先前交付

| 模块 | 当前状态 | 实际范围 |
| --- | --- | --- |
| 上一版备份 | 已推送 | GitHub 私有仓库 [vioIetMiku/miku-gate](https://github.com/vioIetMiku/miku-gate)，v0.1.0-dev.1 保留指向 b8e68b3 |
| 发布与开发 | dev.2 已发布，dev.3 开发中 | [Release](https://github.com/vioIetMiku/miku-gate/releases/tag/v0.1.0-dev.2) 提供三个平台压缩包和校验文件；[Issue #1](https://github.com/vioIetMiku/miku-gate/issues/1) 跟踪后续，已发布资产不被替换 |
| 稳定实例身份 | 已实现并回归 | SQLite schema v2；从 v1 迁移保留出口 ID/端口，重启保持实例 ID，损坏或丢失的 v2 身份拒绝自动替换 |
| 网络计划 | 已实现并回归 | 认证后只读 API、CLI、出口详情预览共用计划；不执行命令，不创建文件，不改变出口状态 |
| Linux 隔离助手 | dev.2 真实 Linux 隔离实验通过 | 独立 root CLI；仅不联网的 namespace + 内部 inet 全阻断规则；临时 Ubuntu runner 验证，不是实际 VPS 或 VPN 验收 |
| 资源归属与回滚 | 已实现并单元故障注入 | 归属记录、锁、namespace 身份核验、显式执行、幂等、预算、未知资源拒绝接管、独立超时回滚 |
| nft 规则核验 | 已实现并单元/fuzz 测试 | 结构化检查表/链/规则，拒绝错误 owner、额外规则、dormant 标志、重复 JSON 键和畸形输出 |
| 浏览器回归 | 本机 Chrome 已实际通过 | 登录、创建、计划预览、规则展开、重置凭据、编辑、删除、阻止假启动、诊断、主题、移动布局、退出与会话撤销 |
| Linux 实验设施 | dev.2 三项内核实验已实际通过 | 两个与宿主断开的 namespace，IPv4/IPv6 正反对照、丢包计数、归属替换、失败回滚及宿主快照对比 |
| 特权 CI | dev.2 与 dev.3 实验均通过 | 临时 Ubuntu runner；dev.3 的7项主测试和6个文件子用例通过，实际读回单核 CPU/512MiB/零 swap/64 tasks 限制 |

原有账号与会话、出口配置与独立凭据、官方节点缓存、受限解析、审计日志和未接线的 HTTP/SOCKS5 TCP 协议核心继续保留。

## 已实际完成的验证

dev.2 发布快照与 dev.3 补充代码分别有独立 CI 记录。dev.3 测试提交为 c214584979e018023a66692432b966af3e8d240b；后续仅文档记录提交不替代该测试来源。

| 验证 | 结果与边界 |
| --- | --- |
| go test -count=1 ./... | Windows 本机通过；覆盖计划、状态机、存储迁移、认证、API、协议核心等。Linux 专有实机测试不在此结果中 |
| go vet ./... | Windows 目标通过；Linux amd64 目标也通过 |
| nft JSON 校验器 fuzz | 配置 5 秒，287,578 次执行，无崩溃；不是完整安全证明 |
| npm --prefix web test | 6 项前端单元测试通过 |
| TypeScript / Vite 构建 | 通过 |
| node scripts/smoke.mjs | 30 项真实 HTTP/CLI 检查通过；隔离数据库、真实服务，无 VPN 或代理监听 |
| Playwright + 本机 Chrome | 1 条完整旅程通过；不是 mock API。新增网络计划、凭据重置和删除检查，桌面/390px 布局与滚动弹窗关闭按钮均有断言 |
| Windows amd64 二进制 | 构建通过 |
| Linux amd64 / arm64 二进制 | 交叉编译通过，不等于 Linux 运行验证 |
| Linux 内核实验测试二进制 | dev.2 已在 Ubuntu runner 实际执行三项并通过；[日志与版本边界](validation/2026-10-09-linux-quarantine.md) |
| GitHub control-plane 工作流 | dev.2、dev.3 各自通过 Go race、前端、30 项 smoke 与 Chromium 浏览器旅程 |
| dev.3 低资源内核实验 | 7项主测试及6个异常文件子用例通过；核验实际 cgroup 配额、进程不误杀、重启/锁、FIFO/符号链接/权限等边界 |

首轮浏览器回归发现原有 getByLabel 精确定位无法命中包含下拉选项的标签；依据实际无障碍树改用 combobox 的可访问名称后通过。截图检查还发现长弹窗关闭按钮会滚出视口，已改为吸顶标题栏，并在桌面和移动宽度重新验证。

可复核截图位于本机 artifacts 目录，包括登录、隔离计划、计划移动布局、概览、深色概览与移动概览。图中的出口是隔离测试配置，不是真实 VPN 连接。测试账号、数据库、trace、构建产物和截图均保持 Git 忽略；不上传账号密码或 SSH 凭据。

## 仍未验证或未实现

- 已有临时 Ubuntu runner 的隔离验证，仍未在目标 VPS 验收。dev.3 的资源预算与文件安全实验已在临时 runner 单独验证；不能替代目标 VPS 或真实 VPN 的验收。
- dev.4 已有实验性隔离 IPC，尚无 VPN 常驻协调器、受控 OpenVPN 服务器、TUN、业务 DNS 或 namespace 内拨号器。
- 尚无真实代理监听、实际出口 IP、隧道断线保护、恢复切换、住宅类型实测、热备、Xray 或多机 Agent。
- 尚未完成完整安全审计、依赖漏洞审计、发行许可证审查或生产安装/升级验证。
- 浏览器自动化不等于手机真机、读屏器或完整可访问性验收。

所有控制台 start 请求继续返回 409 executor_unavailable；active_count 为 0，executor_available / protection_verified 为 false。独立 CLI 的 quarantine_verified 也不能作为真实 VPN 防泄漏的证明。

## VPS 访问边界

用户已授权测试机。只进行了 SSH 握手级检查，停在未知主机公钥校验；读取了待核对的 ED25519 指纹，但没有接受它、写入信任库或登录服务器。没有安装软件、修改路由/防火墙、启停服务或部署程序。

继续实测前，需要通过 Netcatty 已信任记录或 VPS 厂商控制台核对主机公钥。无需再次提供密码，不把未知公钥当成已验证身份。

## 下一步

1. 核对可信 SSH 主机指纹；只读确认 Linux 发行版、能力、工具及现有网络环境。测试资源预算为 1 核 / 1GB，采用本机构建、远程单任务验收，不在 VPS 安装构建工具链。
2. 将已通过的受控隔离用例用于目标环境复核；先前的临时 runner 证据见验证记录，避免重新把交叉编译当运行证明。
3. 完成 dev.4 IPC 的实际跨权限验收后，继续规范化 OpenVPN 配置、受控隧道和 namespace 内 DNS/拨号器。
4. 通过隧道断线、DNS、IPv6、崩溃恢复与资源冲突测试后，才接通现有代理核心并解除启动闸门。

使用说明见 [LINUX_SANDBOX.md](LINUX_SANDBOX.md)，完整网络契约见 [NETWORK_EXECUTOR.md](NETWORK_EXECUTOR.md)。升级前备份旧数据目录；schema v2 不能由旧 dev.1 直接降级打开。
