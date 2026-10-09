# Miku Gate

**本地优先的多出口代理控制台。当前开发版本：`0.1.0-dev.7`。**

> 当前可运行的是带认证的本机代理上游网关：HTTP/SOCKS入口 → 用户提供的SOCKS5、HTTP或HTTPS代理。完整VPN网关仍在开发。
>
> 保存上游后，“启动”会实际验证代理/TLS/DNS路径，再开放固定的127.0.0.1认证入口；失败不会使用宿主直连后备。OpenVPN目前仍是已通过私有实验的构件，生产执行器、Xray/sing-box加密入口、域名与交互安装向导尚未完成。未配置上游时仍返回 `409 executor_unavailable`，不会冒充VPN已连接。

> 升级提示：dev.6 数据库 schema 升为 3，上游凭据用独立的数据库同名 `.key` 文件加密。升级前停止旧服务并备份完整数据目录（数据库、WAL及密钥）；旧版本不能直接打开升级后的数据库。丢失密钥时不重新生成冒充恢复。

已发布压缩包：[v0.1.0-dev.6 代理上游预览](https://github.com/vioIetMiku/miku-gate/releases/tag/v0.1.0-dev.6)。提供Linux amd64/arm64、Windows amd64及校验文件；[实际VPS/发行验收](docs/validation/2026-10-10-proxy-runtime.md)。旧dev.2标签与资产保留，main继续完整网关开发。

## 如何使用本机代理上游

用普通用户运行 `miku-gate init --data .data`，保存一次性管理员密码，再运行 `miku-gate serve --data .data`。打开 `http://127.0.0.1:3939`，创建出口并保存入口凭据，然后点击“设置上游”，填写自己有权使用的代理服务器。保存后明确点击启动，验证通过才开始监听。

入口需要用户名/密码，支持HTTP CONNECT、普通HTTP转发和SOCKS5 TCP。默认最多同时运行3个出口（不是容量承诺），用`--max-active`调整；`--control-plane-only`可关闭运行时。运行中修改上游、凭据、删除或隔离操作都会拒绝，需先停止。重启后不从配置伪造活动状态，也不自动恢复出口。

上游DNS/TLS探测不代表已测公网出口IP或住宅属性。地区是配置标签，严格住宅策略在缺乏证据时拒绝启动。域名/IP公网控制台与VPS自动安装仍待后续阶段；当前远程管理使用SSH转发，不要直接公开明文管理或代理端口。

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
- 独立 Linux sandbox plan / inspect / prepare / cleanup 命令：仅全阻断隔离实验，显式 --apply、归属校验和失败回滚；dev.2 首轮与 dev.3 低资源边界实验已通过临时 Ubuntu runner 验证；该早期记录不是VPN/VPS验收，见 [验证记录](docs/validation/2026-10-09-linux-quarantine.md)；后续目标VPS受控复核见 [独立记录](docs/validation/2026-10-09-vps-lab.md)。
- 独立的 HTTP CONNECT / HTTP 转发 / SOCKS5 TCP 协议库及测试，已在私有测试监听上接通HTTP/SOCKS/HTTPS上游与隧道外DoH，**现已接入显式的生产Web启动路径（仅代理上游，OpenVPN仍未接线）**。

## 尚未实现

- 长驻 VPN 协调器、TUN、OpenVPN 与现有代理核心的集成（隔离 IPC 已实现）。
- 真正的 VPN 建连、隧道 DNS、内核断线保护、自动换线。
- 节点实测、住宅类型情报、严格住宅策略的运行时执行。
- SOCKS5 UDP、Xray、节点订阅、热备和多服务器管理。
- 生产安装器、在线升级、备份恢复和 Docker 发布。

地区与住宅优先仍是配置意图，不是分类证明；严格住宅在没有证据时拒绝启动。只有运行时确实完成校验和绑定后才显示已监听，保存端口本身不代表可用。

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

## 受控 OpenVPN 实验

dev.5 新增规范化配置、接口绑定DNS/TCP拨号器和全隔离的真OpenVPN测试拓扑，尚未接入Web启动或公网代理监听。见 [受控实验手册](docs/CONTROLLED_OPENVPN.md)。真实mTLS、DNS/TCP和断线阻断已通过 [受控Linux验收](docs/validation/2026-10-09-controlled-openvpn.md)；随后在用户授权VPS以单核/384MiB限额完成 [三组受控复核](docs/validation/2026-10-09-vps-lab.md)，并修复`/run` noexec兼容问题。两者都不是生产公网代理验收，操作配方见 [VPS实测手册](docs/VPS_LAB.md)。

## 可选的本机隔离 IPC

dev.4 新增固定实例、指定非 root UID 的 Unix socket 助手。需管理员显式授权，并给普通用户控制台添加 --sandbox-ipc；默认不开启。界面区分计划与实际隔离，变更需二次确认，原有 VPN 启动闸门不解除。

完整命令、身份边界和故障处理见 [隔离 IPC 手册](docs/EXECUTOR_IPC.md)；[真实跨UID验收记录](docs/validation/2026-10-09-executor-ipc.md)已完成。已经发布的 dev.2 压缩包不包含这部分新功能。

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
internal/tunnel/     受控OpenVPN配置、worker拨号器和隔离真隧道实验
internal/diagnostics/ 只读环境检查
web/                React / TypeScript 界面与前端测试
scripts/            构建、真实 HTTP smoke 与浏览器测试辅助
```

## 下一步

隔离、受限IPC及受控OpenVPN/DNS/TCP实验已通过临时Linux环境验收。下一步完成生产权限分离、隧道生命周期协调与worker连接交接；补充更多故障测试后，接通真实代理入口与管理API。

- [阶段任务与验收清单 · Issue #1](https://github.com/vioIetMiku/miku-gate/issues/1)
- [完整产品规划](docs/PRODUCT_PLAN.md)
- [当前 API](docs/API.md)
- [验证与开发状态](docs/PROGRESS.md)
- [依赖与来源说明](docs/DEPENDENCIES.md)

当前实现为独立编写，没有把三个参考仓库的源码直接拼接进来。分发许可证尚未选定，不应将此开发快照当成已经完成许可审查的对外发行版。

完整终态要求与验收矩阵见 [DELIVERY_PLAN.md](docs/DELIVERY_PLAN.md)。正在开发的代理上游与密钥边界见 [UPSTREAM_PROXY.md](docs/UPSTREAM_PROXY.md)。

VPN Gate官方配置的显式旧数据通道支持与静态节点检查见 [VPNGATE_COMPATIBILITY.md](docs/VPNGATE_COMPATIBILITY.md)；main/dev.7开发中，不等于公共节点已连通。
