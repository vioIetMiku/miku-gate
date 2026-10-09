# Miku Gate

**本地优先的多出口代理控制台。当前开发版本：`0.1.0-dev.4`。**

> 当前可运行的是控制面开发原型，不是可用于上网的 VPN 网关。
>
> 已有真实的账号认证、SQLite 持久化、出口配置 API 和 React 管理界面；已加入实验性的独立 Linux 全阻断沙箱助手，但尚未接通 VPN 数据面，因此没有活动隧道、没有代理监听，也没有经过验证的隧道断线防泄漏保护。点击“启动”会明确返回 `409 executor_unavailable`，不会伪造连接成功。

> 升级提示：dev.2 增加稳定实例 ID，数据库 schema 升为 2。升级前停止旧服务并备份数据目录；dev.1 不能直接读取升级后的数据库。

已发布压缩包：[v0.1.0-dev.2 预发布](https://github.com/vioIetMiku/miku-gate/releases/tag/v0.1.0-dev.2)。main 分支继续开发，不替换该标签或已发布资产。

## 已经能做什么

- 本机初始化单管理员，随机密码仅显示一次，不提供公共默认口令。
- 登录、退出、8 小时会话、CSRF 校验、登录限速、回环 Host/Origin 检查。
- 新建、编辑、删除出口配置；独立随机代理凭据、凭据重置、端口配置唯一性检查。
- 修改名称、地区和策略时保留入口端口，最多保存 100 个出口配置。
- 持久化事件记录：数据库保留最近 500 条，界面展示最近 100 条。
- 手动获取 VPN Gate 官方 HTTPS 列表，进行大小限制、CSV 解析、OpenVPN 指令白名单校验。
- 节点源失败保留原缓存，候选不被自动标记为已连接、住宅或可信。
- 系统环境只读诊断；深浅主题、中文界面、桌面与移动布局。
- 出口隔离计划预览；稳定的实例 / 出口资源标识；计划不执行系统命令。
- 独立 Linux sandbox plan / inspect / prepare / cleanup 命令：仅全阻断隔离实验，显式 --apply、归属校验和失败回滚；dev.2 首轮与 dev.3 低资源边界实验已通过临时 Ubuntu runner 验证；不是 VPN 或 VPS 验收，见 [验证记录](docs/validation/2026-10-09-linux-quarantine.md)。
- 独立的 HTTP CONNECT / HTTP 转发 / SOCKS5 TCP 协议库及测试，**尚未连接到任何真实监听器**。

## 尚未实现

- 长驻 VPN 协调器、TUN、OpenVPN 与现有代理核心的集成（隔离 IPC 已实现）。
- 真正的 VPN 建连、隧道 DNS、内核断线保护、自动换线。
- 节点实测、住宅类型情报、严格住宅策略的运行时执行。
- SOCKS5 UDP、Xray、节点订阅、热备和多服务器管理。
- 生产安装器、在线升级、备份恢复和 Docker 发布。

保存的“住宅优先 / 严格住宅”等策略目前仅为配置，不能当作已经执行的筛选结果。界面的配置端口是预留信息，不代表操作系统端口已检查可用或已经监听。

## 在当前 Windows 工作区运行

先打开 PowerShell，进入项目目录：

```powershell
Set-Location 'F:\codex\vpngate\miku-gate'
```

本工作区已经准备了项目内 Go 工具链、前端依赖和 Windows 构建产物；没有修改系统 PATH。首次使用自己的数据目录时执行：

```powershell
.\scripts\run.ps1 -Action init
.\scripts\run.ps1 -Action serve
```

`init` 会输出管理员用户名和一次性随机密码，请保存。浏览器打开 [本机控制台](http://127.0.0.1:3939)，使用该密码登录。已有账号时再次 `init` 会拒绝覆盖。当前尚无密码重置命令，请不要丢失初始化密码。

退出服务使用 `Ctrl+C`。默认数据目录是项目下的 `.data`；不同数据目录拥有独立账号和配置。

修改代码后重新构建：

```powershell
.\scripts\run.ps1 -Action build
.\scripts\run.ps1 -Action test
.\scripts\run.ps1 -Action doctor
```

构建或替换 Windows 可执行文件前，请先停止正在运行的同一路径程序。

## 在新环境从源码构建

要求：

- Go **1.26 或更新**（由当前依赖最低版本要求决定）。
- Node.js 22.12+ 或 24，及 npm。
- 单机控制台不需要 root、OpenVPN 或 TUN；运行 Linux 控制服务时拒绝 root 用户。
- 真正的 VPN 网络功能尚未发布，安装 OpenVPN 也不会让当前开发版自动具备隧道能力。

在项目根目录执行：

```bash
npm ci --prefix web
npm --prefix web run build
go build -trimpath -o bin/miku-gate ./cmd/miku-gate
./bin/miku-gate init
./bin/miku-gate serve
```

前端构建结果嵌入 Go 二进制，运行时无需 Node.js。Windows 编译时将输出文件名改为 `bin/miku-gate.exe`，或使用 PowerShell 脚本。

不能连接 Go 官方模块代理时，可由操作者明确选择可信镜像；不要关闭 `GOSUMDB`、TLS 校验或依赖完整性检查。

## 前端开发

保持后端在 `127.0.0.1:3939` 运行，然后在另一个终端执行：

```bash
npm --prefix web run dev
```

Vite 把 `/api` 请求转发给本机 Go 后端。前端热更新仅用于开发；正式构建预览要在构建后重启后端并刷新页面。

## 测试

项目根目录：

```bash
go test ./...
go vet ./...
npm --prefix web test
npm --prefix web run format:check
node scripts/smoke.mjs
```

`smoke.mjs` 必须在构建后执行。它启动真实 Go HTTP 服务，使用隔离临时数据库，检查登录、CSRF、出口 CRUD、凭据、网络计划与 CLI 一致性、显式执行闸门、拒绝假启动、审计与退出；结束时关闭自己启动的服务。不访问公共 VPN 节点，不修改网络设置。

浏览器回归：

```bash
npm --prefix web run test:e2e
```

- 本地默认使用已安装的 Chrome。Windows 可通过 `MIKU_BROWSER_CHANNEL=msedge` 选择 Edge。
- CI 使用 Playwright 自带 Chromium，先安装对应浏览器及依赖。
- 浏览器测试使用独立数据库和 `127.0.0.1:3940`；要求该端口空闲。
- 测试截图位于 `artifacts`，测试身份和数据库位于 `.cache`，均不纳入 Git。
- 截图中的节点配置是测试数据，不是真实连接或真实性能结果。

当前验证结果及未完成项目见 [开发进度](docs/PROGRESS.md)。交叉编译成功不代表 Linux 运行、TUN 或防泄漏验证成功。

## 可选的本机隔离 IPC

dev.4 新增固定实例、指定非 root UID 的 Unix socket 助手。需管理员显式授权，并给普通用户控制台添加 --sandbox-ipc；默认不开启。界面区分计划与实际隔离，变更需二次确认，原有 VPN 启动闸门不解除。

完整命令、身份边界和故障处理见 [隔离 IPC 手册](docs/EXECUTOR_IPC.md)。已经发布的 dev.2 压缩包不包含这部分新功能。

## Linux 隔离实验

新功能仅创建无外部链路的全阻断 namespace，不建立隧道或代理。先在出口详情查看计划；普通 go test 不会运行特权内核实验。使用条件、手动命令、清理边界与可选的 IPv4/IPv6 实验见 [Linux 沙箱手册](docs/LINUX_SANDBOX.md)。

## 默认安全边界

- 服务只绑定回环 IP；当前版本不接受公网绑定。需要远程查看时使用受保护的 SSH 转发，不要自行公开该开发服务。
- 默认 HTTP 仅用于本机。没有宣称已具备生产 HTTPS、反向代理信任配置或多租户隔离。
- 管理密码、代理密码用 Argon2id 摘要保存；随机会话 token 在数据库中仅保存摘要。CSRF token 与会话分离。
- 代理密码只在创建／重置时返回一次。日志和普通读取接口不返回密码摘要或 VPN 配置正文。
- `.data`、`.cache` 和备份应视为敏感本机文件；数据库没有全盘加密，包含会话信息、节点配置及可能的内嵌 VPN 客户端证书/密钥。
- Unix 创建目录/文件时使用限制权限；Windows 用户需要依赖自己的账户和目录 ACL，不能把 Unix 权限位当成 Windows 访问控制保证。
- HTTP/SOCKS5 协议库要求显式注入拨号器和认证器，不包含隐式系统直连后备。未来接入执行器时仍需验证目标策略、DNS、namespace 和实际路由。
- 不记录访问 URL、请求正文或浏览内容。本开发版根本不承载业务代理流量。

## 项目结构

```text
cmd/miku-gate/       CLI 与 HTTP 服务入口
internal/domain/    出口、节点、事件及输入约束
internal/store/     SQLite、账号、会话、配置、缓存与事件
internal/security/  随机 token、摘要与密码哈希
internal/httpapi/   认证、CSRF、API、嵌入式前端服务
internal/vpngate/   官方列表获取、CSV 与配置校验
internal/egress/    IPv4 公网目标策略的纯函数
internal/proxy/     独立、未接线的 HTTP / SOCKS5 TCP 核心
internal/netexec/   隔离计划、Linux 全阻断沙箱、归属与故障实验
internal/executoripc/ 指定 UID 的本机 IPC、端点归属和跨权限测试
internal/diagnostics/ 只读环境检查
web/                React / TypeScript 界面与前端测试
scripts/            构建、真实 HTTP smoke 与浏览器测试辅助
```

## 下一步

先运行真实 Linux 隔离实验，随后补齐特权 IPC、受控 OpenVPN 测试拓扑与 namespace 内的 DNS / 拨号器；验证隧道断线与重启边界后，再接通代理核心和管理 API。

- [阶段任务与验收清单 · Issue #1](https://github.com/vioIetMiku/miku-gate/issues/1)
- [完整产品规划](docs/PRODUCT_PLAN.md)
- [当前 API](docs/API.md)
- [验证与开发状态](docs/PROGRESS.md)
- [依赖与来源说明](docs/DEPENDENCIES.md)

当前实现为独立编写，没有把三个参考仓库的源码直接拼接进来。分发许可证尚未选定，不应将此开发快照当成已经完成许可审查的对外发行版。
