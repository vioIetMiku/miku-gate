# Miku Gate

## VPN Gate 是什么？

[VPN Gate](https://www.vpngate.net/) 是日本筑波大学开展的学术实验项目，汇集世界各地志愿者提供的公共 VPN 中继。你可以从它的[服务器列表](https://www.vpngate.net/en/)获取公开连接配置；Miku Gate 使用其中的 **OpenVPN** 节点，为自己的 Linux VPS 配置和管理多个出口。

VPN Gate 不属于 Miku Gate，我们不运营这些中继。志愿者节点的数量、国家、在线时间和连通性随时变化，**不能保证住宅 IP、固定出口或持续可用，也不是无日志服务**。请先阅读[项目介绍](https://www.vpngate.net/en/about_overview.aspx)与[日志及反滥用政策](https://www.vpngate.net/en/about_abuse.aspx)，遵守当地法律与服务商规则，不用于滥用行为或需要严格保密的通信。

## 致谢 fanout

特别感谢 **[byJoey/fanout](https://github.com/byJoey/fanout)**。Miku Gate 的当前单页版本直接基于该项目，保留按国家批量创建出口、网络命名空间隔离、自动重连及节点管理的基础能力，再加入 Miku 界面、自建核心、多格式订阅、快捷域名证书绑定、延迟测试、vpngate自定义源、ASN归属地检测展示、 ip属性展示、断线重连次数展示提醒与 VPS 维护流程。

原始基线提交为 `d7ce5224caf3469b19876982abdf8d1be16c4a8b`；原作者署名与 MIT 许可证完整保留。详见 [LICENSE](LICENSE)、[上游 README](README.fanout.md) 和 [源码来源](PROVENANCE.md)。

---

<p align="center">
  <img src="assets/miku-hero-v1.png" alt="Miku Gate 插画" width="300">
</p>

<h1 align="center">世界，等你连接。</h1>
<p align="center">把 VPN Gate 出口、代理入口和日常维护，放进一个自己的控制台。</p>

<p align="center">
  <a href="https://github.com/vioIetMiku/miku-gate/releases/latest">下载正式版</a> ·
  <a href="#vps-一键安装">VPS 一键安装</a> ·
  <a href="#第一次使用">第一次使用</a> ·
  <a href="CHANGELOG.md">更新记录</a>
</p>

**当前正式版：`v0.2.0`**，基于 `dev34`。支持 Linux **amd64 / arm64**，管理界面支持白天 / 夜间主题、移动端布局和展示 / 管理模式。安装包已经编译，**VPS 不需要 Go**。

> 升级提醒：`v0.2.0` 使用基于 fanout 的单页架构。旧版任务、远程 Agent、多账号和高级记录不会完整迁移；安装器仅迁移已识别旧安装的可用入口、普通 OpenVPN 出口及相关凭据，升级前请备份。不要将“正式 Release”理解为对所有 VPS、面板与 VPN Gate 节点均已完成实机验证。

## 主要功能

| 功能 | 说明 |
| --- | --- |
| 批量出口 | 按国家及数量创建 OpenVPN 出口，自动选线、重连与补位；换线尽量保持端口、凭据、UUID 和分享链接 |
| 多来源节点池 | 官方接口、公开 CSV、开源快照、近期 TCP 检测记录与 Worker 镜像；可添加公开来源并调整顺序 |
| 自建或已有面板 | 自建 Xray / sing-box；检测并适配 3x-ui、S-UI、xray-cf-lite，不会安装这些外部面板 |
| 入口与客户端 | 自建模式下同一监听端口可按客户端绑定不同出口；面板模式按出口复制入口 |
| 认证代理 | 同一端口支持认证 SOCKS5 / HTTP，HTTP 支持 CONNECT；不是开放代理 |
| 节点与订阅 | 通用 Base64、V2Ray/v2rayN、Clash/Mihomo YAML、sing-box JSON；仅导出有效的本机节点 |
| 出口信息 | 国家、地区、ASN、归属商、IP 类型与来源说明；住宅属性是规则推断，不是认证 |
| 连通与延迟 | 有界并发健康检查、同国家换线与地域 / ASN 偏好；30 秒刷新限频和断线稳定性提示 |
| 日常运维 | 随机管理路径、登录口令、域名 / 证书管理、备份、更新、修复与旧版回滚 |

## VPS 一键安装

### 安装要求

- Linux VPS，amd64（x86_64）或 arm64（aarch64），使用 **systemd 或 OpenRC**。
- root 权限，可用的 `/dev/net/tun`，以及创建网络命名空间和修改 iptables 的权限。权限不足的容器不适用。
- 可连接 GitHub、官方核心下载源和 VPN Gate。准备 Bash、curl、CA 证书；缺少 Python、OpenVPN 等运行依赖时安装器会通过包管理器补齐。
- 在服务商安全组和防火墙放行管理端口及实际创建的入口 / 代理端口。管理端口默认 `8899`，占用时自动选择空闲端口。

### 公开仓库的一行安装

在 VPS 的 **root Bash 终端**运行。脚本读取最新正式 Release，拒绝草稿和预发布版本，校验下载包 SHA256、内部清单、权限与文件路径后安装：

```bash
bash -c 'set -e; d=$(mktemp -d); trap '\''rm -rf -- "$d"'\'' EXIT; curl --proto "=https" --tlsv1.2 -fsSL https://github.com/vioIetMiku/miku-gate/releases/latest/download/install-online.sh -o "$d/install-online.sh"; bash "$d/install-online.sh"'
```

脚本是仓库中的 [install-online.sh](install-online.sh)，也作为 Release 附件提供。执行前可先下载审阅。不要把安装脚本交给来源不明的镜像站，也不要用关闭 TLS 校验的方式解决下载错误。

### 私有仓库安装（当前仓库适用）

如果仓库仍为私有，上面的公开链接会返回 404。准备一个有本仓库 **Contents: Read** 权限的 GitHub fine-grained personal access token，在 VPS 的 root Bash 终端运行下面整段；令牌通过隐藏输入读取，不会作为明文命令写入历史，也不会传给实际服务安装器：

```bash
(
  set -e
  read -rsp 'GitHub token: ' GITHUB_TOKEN; printf '\n'
  export GITHUB_TOKEN
  d=$(mktemp -d)
  trap 'rm -rf -- "$d"; unset GITHUB_TOKEN' EXIT
  curl --proto '=https' --tlsv1.2 -fsSL \
    -H "Authorization: Bearer $GITHUB_TOKEN" \
    -H 'Accept: application/vnd.github.raw+json' \
    'https://api.github.com/repos/vioIetMiku/miku-gate/contents/install-online.sh?ref=v0.2.0' \
    -o "$d/install-online.sh"
  bash "$d/install-online.sh"
)
```

访问令牌仅用于读取当前仓库的 Release。自定义管理端口时，在执行脚本前设置 `export WEB_PORT=9900`；锁定版本使用 `export MIKU_VERSION=v0.2.0`。已经下载脚本时，也可先运行 `bash install-online.sh --download-only /root/miku-packages`，只下载并校验，不安装服务。

### 手动安装 / 离线传包

在 [Release](https://github.com/vioIetMiku/miku-gate/releases/tag/v0.2.0) 下载以下文件。私有仓库需要先登录 GitHub：

| 文件 | 用途 |
| --- | --- |
| `miku-gate-vps-v0.2.0.tar.gz` | VPS 通用安装包，包含两种架构的主程序、旧版导出程序与维护工具 |
| `miku-gate-v0.2.0.zip` | 完整源码 + 两种架构安装二进制；不是 GitHub 自动生成的纯源码压缩包 |
| `install-online.sh` | 最新正式版在线安装入口 |
| `SHA256SUMS` | 上述三个附件的 SHA256 校验清单 |

将 VPS 包和校验文件放到同一目录后运行：

```bash
grep '  miku-gate-vps-v0.2.0.tar.gz$' SHA256SUMS | sha256sum -c -
sudo tar --no-same-owner -xzf miku-gate-vps-v0.2.0.tar.gz
sudo bash miku-fanout/install.sh
```

内部目录 `miku-fanout/` 是兼容现有安装器的名称，不影响项目名 Miku Gate。完整 ZIP 包解压后也是这个目录；直接运行其中的 `install.sh` 即可。手动解包时请使用 root 所有、不可被其他用户写入的文件，不要跳过校验。

## 第一次使用

1. 安装完成后记下打印的 **管理地址和访问口令**；也可以运行 `miku info` 再查看。
2. 浏览器打开 `http://VPS_IP:端口/随机路径/`。建议尽快使用 `miku domain` 绑定域名并启用 HTTPS；随机路径不能代替 TLS。
3. 在界面前部确认入口：检测已有面板，或使用自建 Xray。没有模板时可选 **自动创建 VLESS REALITY 入口**。
4. 点击 **添加出口**，选择国家、数量与入口模板。程序获取 OpenVPN 配置、创建隧道、分配端口并绑定出口。
5. 连通后复制节点或认证 SOCKS5 / HTTP 地址，或打开订阅弹窗选择客户端格式。
6. 需要换线时使用切换 / 重连；VPN 已通但入口失败时查看原因并使用 **修复入口**。

自建核心可在设置中切换 Xray / sing-box，切换前会校验配置。**XHTTP 使用 Xray**；不要假定所有客户端和订阅格式支持全部传输协议。本机链接识别会校验发布地址、端口、凭据和传输参数，不支持把任意外部代理作为入口导入。

### 域名与证书

- 在 DNS 设置域名 A 记录指向 VPS，然后执行 `miku domain`，或在网页右上角管理域名与证书。
- 自动签发需要公网可访问 TCP 80；也可复用已登记的 Certbot 证书。
- 可更换域名，输入 `none` 解绑。REALITY 使用自己的握手目标，不需要管理域名证书。
- 改密码、核心、域名或维护服务可能重启入口与 VPN，客户端需要重新连接。

### 客户端格式限制

- **Clash / Mihomo**：使用支持 VLESS / REALITY 的 Mihomo 内核。
- **V2Ray / v2rayN**：REALITY 使用 Xray 内核；VMess 使用标准 Base64 JSON 分享格式。
- **XHTTP**：当前请使用通用 / V2Ray 格式；不支持的订阅格式会返回明确错误，而不是丢掉传输参数。
- **sing-box JSON**：包含本地 `127.0.0.1:7890` 混合代理入口与节点选择器。使用整张证书 SHA256 固定指纹时需要 sing-box 1.15+ 客户端，不会通过禁用证书校验伪装兼容。

## 维护、备份与更新

```bash
miku                 # 快捷菜单
miku info            # 管理地址与当前口令
miku status          # 服务状态
miku password        # 本机 root 重置口令，无需旧口令
miku domain          # 绑定 / 更换 / 解绑域名
miku repair          # 补齐依赖与核心，重启服务
miku port            # 修改管理端口
miku start
miku stop
miku restart
miku logs
miku backup          # 停机一致性备份；完成后恢复原运行状态
miku update /root/miku-gate-vps-v0.2.0.tar.gz
miku rollback        # 仅用于已登记的旧架构迁移，恢复切换前配套快照
miku uninstall       # 停止并取消自启，保留程序、数据与备份
```

- 数据：`/var/lib/miku`；程序与维护脚本：`/usr/local/lib/miku`；服务名：`miku`；快捷菜单：`/usr/local/bin/miku`。
- 更新前先运行 `miku backup`；下载校验后的新 VPS 包，再执行 `miku update /绝对路径/安装包.tar.gz`。也可重新运行一键脚本，由已有安装识别流程执行更新。
- 备份含登录口令、节点身份、订阅凭据和证书私钥，权限为 `600`；不要提交到 Git，也不要把管理地址 / 订阅地址发到公开渠道。
- `miku rollback` **不是任意版本回滚器**，只适用于从已识别旧 Miku 架构切换时创建的配套快照。

### 从旧 Miku 升级

安装器识别已有登记信息，先停旧服务并快照数据库、密钥、证书、程序与菜单，再用原普通账号导出兼容数据。自建入口、普通 OpenVPN 出口和代理凭据可迁移，入口端口、UUID / 密码与 REALITY 密钥尽量保留；外部面板通过重新检测继续管理。

空出口、带前置代理的出口和旧高级记录不会自动转换为新配置，留在旧版快照中。切换失败会尝试恢复配套旧服务；成功后仍可 `miku rollback`。回滚后新版数据保留，请勿混跑两套服务。

## 常见问题

| 情况 | 处理建议 |
| --- | --- |
| GitHub 返回 404 | 检查仓库是否私有、令牌是否有本仓库 Contents 读取权限、标签与附件是否存在 |
| SHA256 / 清单校验失败 | 停止安装，重新从原 Release 下载；不要手动改清单绕过检查 |
| 没有 TUN / 无法创建 netns | 检查 VPS 内核、虚拟化权限与容器限制；安装器不能凭空提供宿主机权限 |
| 管理页面无法打开 | 运行 `miku info` / `miku status` / `miku logs`，核对实际端口、随机路径、防火墙和安全组 |
| 有节点但 OpenVPN 不通 | 公开清单及 TCP 预检不等于 OpenVPN 可用；换候选、查 VPS 出站限制与日志 |
| IP 类型与预期不同 | 类型由查询来源和规则推断，可能过期或误判；查看来源 / 置信度，不保证住宅 IP |
| 总延迟与客户端测速不同 | 面板显示浏览器到 VPS 的 HTTP RTT + VPS 到当前 VPN Gate 的测量 / 参考值，不是完整客户端链路测速 |

## 从源码构建

构建环境使用 Linux、Go **1.26+**、Python **3.9+**。安装包本身不要求 VPS 上有 Go；主程序依赖 Linux 网络能力，不是 Windows 桌面客户端。

```bash
go test -race -c -o /tmp/miku-gate-tests .
sudo /tmp/miku-gate-tests -test.count=1 -test.timeout=5m
go vet ./...
(cd compat/miku-export && go test ./... && go vet ./...)
(cd scripts && sudo python3 -B -m unittest -v test_maintenance test_bootstrap)
python3 scripts/build-release.py --version 0.2.0 --label v0.2.0 --output dist
```

构建脚本从源码重新编译两种架构的主程序和兼容导出程序，生成 VPS 包、完整 ZIP、在线安装脚本、`BUILD.json` 与 `SHA256SUMS`。Git 标签 `vX.Y.Z` 触发自动检查、构建及正式发布；检查失败不会发布正式 Release。

## 许可证与来源

- 基于 [byJoey/fanout](https://github.com/byJoey/fanout)，原始 MIT 许可证与 `Joey (byJoey)` 署名保留。
- Xray、sing-box 在安装时从各自官方发布下载，版本和 SHA256 固定于 [CORE_ASSETS.json](CORE_ASSETS.json)，分别适用其自身许可证。
- 视觉素材说明：[ARTWORK.md](ARTWORK.md)；界面设计说明：[UI_DESIGN.md](UI_DESIGN.md)。插画为生成素材，不代表与角色权利人存在授权、合作或背书。
- 完整来源与迭代说明：[PROVENANCE.md](PROVENANCE.md)、[开发历史](docs/DEV_HISTORY.md)、[更新记录](CHANGELOG.md)。
