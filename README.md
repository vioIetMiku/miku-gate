<p align="center">
  <img src="docs/assets/mikugate-logo.jpg" alt="MikuGate 图标" width="20%">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/mikugate-wordmark-dark.svg">
    <img src="docs/assets/mikugate-wordmark-light.svg" alt="MikuGate" width="55%">
  </picture>
</p>
<p align="center">
  <a href="https://github.com/vioIetMiku/miku-gate/actions/workflows/release.yml"><img src="https://img.shields.io/github/actions/workflow/status/vioIetMiku/miku-gate/release.yml?style=flat-square&amp;logo=github&amp;label=CI" alt="Release 工作流状态"></a>
  <a href="https://github.com/vioIetMiku/miku-gate/releases/latest"><img src="https://img.shields.io/github/v/release/vioIetMiku/miku-gate?style=flat-square&amp;logo=github&amp;color=14b8a6" alt="最新正式版本"></a>
  <a href="https://github.com/vioIetMiku/miku-gate/stargazers"><img src="https://img.shields.io/github/stars/vioIetMiku/miku-gate?style=flat-square&amp;logo=github&amp;color=0ea5e9" alt="GitHub Stars"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/vioIetMiku/miku-gate?style=flat-square&amp;color=22c55e" alt="MIT 许可证"></a>
</p>
<p align="center">
  <a href="#install-native"><img src="https://img.shields.io/badge/platform-Linux-f0b429?style=flat-square&amp;logo=linux&amp;logoColor=white" alt="支持 Linux"></a>
  <a href="#install-docker"><img src="https://img.shields.io/badge/deploy-Docker-2496ed?style=flat-square&amp;logo=docker&amp;logoColor=white" alt="支持 Docker 部署"></a>
  <img src="https://img.shields.io/badge/arch-amd64%20%7C%20arm64-0e7490?style=flat-square" alt="支持 amd64 与 arm64">
</p>

<p align="center">
  <a href="#quick-start">快速开始</a> ·
  <a href="#features">功能特点</a> ·
  <a href="#install-docker">Docker 部署</a> ·
  <a href="#install-remote">前后端分离</a> ·
  <a href="docs/FRONTEND.md">使用指南</a> ·
  <a href="https://github.com/vioIetMiku/miku-gate/releases/latest">下载发布</a>
</p>

MikuGate 是基于 VPN Gate 公共中继的自托管多出口管理工具：在自己的 Linux VPS 上按国家创建出口，通过网页面板或终端管理节点，并生成客户端可用的订阅。支持原生安装与 Docker 部署，也支持把前端和后端放在不同服务器。

<a id="features"></a>

## 功能特点

| 功能 | 可以做什么 |
| --- | --- |
| 多国家出口 | 按国家批量创建、查看和管理 VPN Gate 出口 |
| 网络隔离与重连 | 使用网络命名空间隔离出口，连接异常时尝试自动恢复 |
| 面板与终端 | 使用网页面板，或通过 `miku`、`miku quick`、`miku sub` 管理与获取订阅 |
| 多格式订阅 | 为支持的客户端生成订阅与节点链接 |
| 两种部署方式 | 原生一键安装，或使用 Docker 运行独立前端与后端 |
| 前后端分离 | 前端可以独立启停、更新，也能部署到另一台服务器 |
| 自动端口 | Docker 部署遇到端口冲突时自动避让，实际地址以 `info` 输出为准 |

## VPN Gate 是什么？

[VPN Gate](https://www.vpngate.net/) 是日本筑波大学开展的学术实验项目，由世界各地志愿者提供公共 VPN 中继。Miku Gate 使用其公开的 OpenVPN 配置，为自己的 Linux VPS 管理多个出口。

VPN Gate 不属于 Miku Gate。节点数量、国家、在线时间随时变化，**不保证住宅 IP、固定出口、在线率或严格隐私，也不是无日志服务**。请阅读[官方介绍](https://www.vpngate.net/en/about_overview.aspx)与[日志和反滥用政策](https://www.vpngate.net/en/about_abuse.aspx)，遵守当地法律与服务商规则。

## 感谢 fanout

特别感谢 **[byJoey/fanout](https://github.com/byJoey/fanout)**。Miku Gate 基于其按国家批量出口、网络命名空间隔离和自动重连能力，再加入 Miku 界面、多格式订阅、终端一键配置、Docker 与前后端分离。

基线提交：`d7ce5224caf3469b19876982abdf8d1be16c4a8b`。原 MIT 许可证和作者署名保留，见 [LICENSE](LICENSE)、[上游说明](README.fanout.md) 与 [源码来源](PROVENANCE.md)。

**代码版本：v0.2.7**，正式状态以 [Releases](https://github.com/vioIetMiku/miku-gate/releases/latest) 为准。支持 Linux amd64 / arm64；完整安装包已编译，VPS 无需安装 Go。

<a id="quick-start"></a>

## 快速开始

**只选一条路线。** 所有命令在服务器的 **root Bash 终端**执行；已经是 root 就直接运行，普通用户先执行 `sudo -i`。下面的环境安装命令适用于 **Debian 12 / 13、Ubuntu 22.04 / 24.04 / 26.04**，架构为 amd64 或 arm64；新 VPS 推荐 Ubuntu 24.04。无需在宿主机安装 Go 或 Node.js。

| 你的需求 | 从哪里开始 | 最后访问哪里 |
| --- | --- | --- |
| 一台 VPS，直接安装 | [方式一：原生一键安装](#install-native) | `miku info` 输出的地址 |
| 一台 VPS，Docker 前后端分离 | [方式二：Docker 同机安装](#install-docker) | `frontend info` 输出的地址 |
| 两台服务器，前端和后端分别部署 | [方式三：跨服务器安装](#install-remote) | 前端域名 + 后端随机路径 |
| 手动下载、校验安装包 | [方式四：手动安装](#install-offline) | 按所选部署模式查看 |

**前后端分离不一定要两台机器。** 同机 Docker 安装已经会启动独立前端和后端，通常选方式二即可。跨机时，A 运行 VPN 与节点，B 只提供网页和 API 代理。

<a id="install-native"></a>

## 方式一：原生一键安装（推荐）

### 1. 运行安装命令

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/mikugate.sh)
```

安装器下载正式包、校验文件并准备运行依赖，随后启动系统服务。输入 **1** 使用网页面板，输入 **2** 或直接回车打开终端菜单（选国家 / 切换 IP），输入 **3** 执行默认国家一键配置。原生安装不需要 Docker。

### 2. 打开面板，获取订阅

```bash
miku info   # 查看完整管理地址和登录口令
miku quick  # 按提示创建节点；没有节点域名可以直接回车
miku sub    # 查看当前可用订阅和节点链接
```

浏览器打开 `miku info` 输出的**完整地址，保留端口和随机路径**。在服务商安全组和防火墙放行实际管理端口，以及后续创建的节点入口端口。订阅包含访问凭据，请自行保管。

日常用 `miku` 打开菜单；`miku status` 查看运行状态，`miku password` 重置口令，`miku backup` 备份。升级步骤见[更新与维护](#maintenance)。

<a id="install-docker"></a>

## 方式二：Docker 同机安装（推荐的分离部署）

一台 VPS，三步完成：**装环境 → 运行安装命令 → 打开前端地址**。后端使用 rootful Docker、host 网络和 TUN；独立 Nginx 前端由安装器一并启动。

### 1. 准备终端

使用 Debian 12 / 13 或 Ubuntu 22.04+，以 root 登录。如果没有 curl，先执行：

```bash
apt-get -qq update && apt-get -y -qq install curl ca-certificates
```

v0.2.7 的 Docker 一键安装会自动准备 Docker Engine、Compose v2、Python 及 TUN 检查；已经可用的 Docker 会复用。安装过程只显示阶段进度，每 15 秒提示仍在进行；失败会显示日志位置和错误摘要。其他发行版需要先装好 Docker / Compose / Python 3.10+。官方环境参考：[Docker Engine](https://docs.docker.com/engine/install/)、[Compose v2](https://docs.docker.com/compose/install/linux/)。

<a id="docker-env"></a>

<details>
<summary>只准备 Docker 环境（用于跨机 B，只装前端）</summary>

在 B 的 root Bash 执行，先装基础工具，再下载并运行本项目的环境安装脚本；`MIKU_FRONTEND_ONLY=1` 跳过后端才需要的 TUN 检查：

```bash
(
set -eu
apt-get -qq update && apt-get -y -qq install curl ca-certificates python3
setup_dir=$(mktemp -d)
trap 'rm -rf -- "$setup_dir"' EXIT
curl -fsSL --retry 3 https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/scripts/setup-docker.sh -o "$setup_dir/setup-docker.sh"
curl -fsSL --retry 3 https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/scripts/progress.py -o "$setup_dir/progress.py"
MIKU_FRONTEND_ONLY=1 python3 -B "$setup_dir/progress.py" '正在准备 Docker 环境' bash "$setup_dir/setup-docker.sh"
)
```

</details>

### 2. 一条命令安装前端和后端

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/mikugate.sh) --docker
```

安装器验证正式包，把文件保存到 `/opt/miku-gate-docker`，构建或复用后端镜像，然后自动启动独立前端。**同机不需要再装前端，也不用填写后端 URL、随机路径或单独创建前端账号。**

### 3. 输入 miku 打开菜单

安装成功时直接显示 **前端完整地址、后端地址、登录口令**。以后在 VPS 的任意目录输入：

```bash
miku           # 打开分组菜单，操作后可返回继续选择
miku info      # 再次查看前端地址与登录口令
miku countries # 选择国家与数量创建节点
miku list      # 查看当前出口、IP 和状态
miku swap      # 选择已有出口，切换同国家的其他节点
miku sub       # 查看订阅与节点链接
```

菜单包含出口查看、选国家、切换 IP、一键配置、订阅、密码、服务启停、日志和备份。`miku quick` 仍保留默认的一键配置。若机器上已有其他原生 `miku` 命令，安装器会保留原命令，Docker 菜单用 `miku-docker`。

浏览器打开 `miku info` 显示的**前端完整地址**，保留随机路径，使用输出的登录口令。端口占用时会自动避让，不要固定照抄 8899 / 8900。

同机 HTTP 地址可直接使用。要给前端绑定 HTTPS 域名，按[前端域名与 HTTPS](#frontend-https)操作。

<a id="install-remote"></a>

## 方式三：跨服务器安装（A 后端、B 前端，推荐使用有内网穿透的两台vps搭建，维护麻烦极极极极极极极极极其不推荐）

这条路线针对**新安装**。先把域名 `api.你的域名` 的 DNS A 记录指向 A，把 `panel.你的域名` 指向 B；两台都放行 TCP **80 / 443**，不要保留指向错误服务器的 AAAA 记录。下面通过 Caddy 容器自动申请和续期 HTTPS 证书，不需要手动购买或上传证书。

**A 在后端安装时自动准备环境；B 先执行[前端环境安装块](#docker-env)**，B 无需 TUN。服务器若已有 Nginx / Caddy 占用 80 / 443，应在已有反代中添加站点，避免启动第二个占用相同端口的服务。

### A：只安装后端

在 **服务器 A** 执行下面整块。命令下载 v0.2.7 完整包、校验 SHA256、解压，再只部署后端：

```bash
(
  set -eu
  package_dir=$(mktemp -d /tmp/mikugate-backend.XXXXXX)
  cd "$package_dir"
  release_url='https://github.com/vioIetMiku/miku-gate/releases/download/v0.2.7'
  curl -fL --retry 3 "$release_url/miku-gate-vps-v0.2.7.tar.gz" -o miku-gate-vps-v0.2.7.tar.gz
  curl -fL --retry 3 "$release_url/SHA256SUMS" -o SHA256SUMS
  grep '  miku-gate-vps-v0.2.7.tar.gz$' SHA256SUMS | sha256sum -c -
  tar --no-same-owner -xzf miku-gate-vps-v0.2.7.tar.gz
  cd miku-gate
  bash install.sh --docker --backend-only
)
```

后端会保存到 `/opt/miku-gate-docker`。为 A 配置 HTTPS：以下先输入你自己的后端域名，再自动读取实际端口和路径，生成 Caddy 配置并启动容器。

```bash
(
set -eu
cd /opt/miku-gate-docker
read -rp '后端域名（例如 api.example.com，不带 https://）：' backend_domain
backend_port=$(python3 -c 'import json; print(json.load(open("docker-data/settings.json"))["port"])')
backend_path=$(cat docker-data/basepath)
install -d -m 755 /opt/miku-gate-backend-https
printf '%s {\n  reverse_proxy 127.0.0.1:%s\n}\n' "$backend_domain" "$backend_port" > /opt/miku-gate-backend-https/Caddyfile
docker run -d --name miku-backend-https --restart unless-stopped --network host \
  -v /opt/miku-gate-backend-https/Caddyfile:/etc/caddy/Caddyfile:ro \
  -v miku-backend-caddy-data:/data -v miku-backend-caddy-config:/config caddy:2
bash install-docker.sh backend info
printf '\n给前端服务器 B 使用的完整后端地址：https://%s/%s/\n' "$backend_domain" "${backend_path#/}"
)
```

记录最后打印的 **HTTPS 后端完整地址**和登录口令。等待域名证书生效，可运行 `docker logs --tail 50 miku-backend-https` 查看签发状态。这一方案保持后端内部 HTTP，由 Caddy 处理公网 HTTPS；不用再在后台为同一个管理入口重复配置 TLS。

### B：只安装前端，填两个地址

在 **服务器 B** 下载前端专用包并校验，不需要完整后端包：

```bash
(
  set -eu
  package_dir=$(mktemp -d /tmp/mikugate-frontend.XXXXXX)
  cd "$package_dir"
  release_url='https://github.com/vioIetMiku/miku-gate/releases/download/v0.2.7'
  curl -fL --retry 3 "$release_url/miku-gate-frontend-v0.2.7.tar.gz" -o miku-gate-frontend-v0.2.7.tar.gz
  curl -fL --retry 3 "$release_url/SHA256SUMS" -o SHA256SUMS
  grep '  miku-gate-frontend-v0.2.7.tar.gz$' SHA256SUMS | sha256sum -c -
  test ! -e /opt/miku-gate-frontend || { echo '目录已存在，请按更新流程操作'; exit 1; }
  install -d -m 755 /opt/miku-gate-frontend
  tar --no-same-owner -xzf miku-gate-frontend-v0.2.7.tar.gz --strip-components=1 -C /opt/miku-gate-frontend
)
```

接着输入 **B 的前端域名**和 **A 刚打印的 HTTPS 后端完整地址**。下面会自动拆出地址、端口和随机路径，生成 `.env`，不用手动填写六个配置项：

```bash
(
set -eu
cd /opt/miku-gate-frontend
python3 -c '
from pathlib import Path
from urllib.parse import urlsplit
import re
config = Path(".env")
assert not config.exists(), ".env 已存在，请编辑已有文件，避免覆盖"
domain = input("前端域名（例如 panel.example.com，不带 https://）：").strip()
backend = urlsplit(input("粘贴 A 的完整后端地址（https://域名/随机路径/）：").strip())
assert re.fullmatch(r"[A-Za-z0-9.-]+", domain), "前端请只填域名"
assert backend.scheme == "https" and backend.hostname and not backend.username and not backend.password, "后端必须是 HTTPS 地址"
assert not backend.query and not backend.fragment, "后端地址不要包含查询参数或片段"
assert re.fullmatch(r"[A-Za-z0-9.:-]+", backend.netloc), "请使用域名或 IPv4 后端地址"
assert backend.port is None or 1 <= backend.port <= 65535, "后端端口无效"
base = backend.path.strip("/")
assert re.fullmatch(r"[A-Za-z0-9_-]*", base), "后端随机路径格式不正确"
config.write_text("\n".join([
    "MIKU_FRONTEND_LISTEN=127.0.0.1",
    "MIKU_FRONTEND_PORT=",
    "MIKU_FRONTEND_ORIGIN=https://" + domain,
    "MIKU_BACKEND_URL=https://" + backend.netloc,
    "MIKU_BACKEND_BASE_PATH=/" + base,
    "MIKU_BACKEND_HOST=" + backend.netloc,
]) + "\n")
config.chmod(0o600)
print("已生成 .env，下一步启动前端。")
'
python3 -B scripts/frontend.py start
python3 -B scripts/frontend.py info
)
```

最后为 B 启用 HTTPS，域名和端口自动从刚才的配置读取：

```bash
(
set -eu
cd /opt/miku-gate-frontend
frontend_domain=$(sed -n 's|^MIKU_FRONTEND_ORIGIN=https://||p' .env)
frontend_port=$(python3 -c 'import json; print(json.load(open("frontend/.runtime/state.json"))["port"])')
install -d -m 755 /opt/miku-gate-frontend-https
printf '%s {\n  reverse_proxy 127.0.0.1:%s\n}\n' "$frontend_domain" "$frontend_port" > /opt/miku-gate-frontend-https/Caddyfile
docker run -d --name miku-frontend-https --restart unless-stopped --network host \
  -v /opt/miku-gate-frontend-https/Caddyfile:/etc/caddy/Caddyfile:ro \
  -v miku-frontend-caddy-data:/data -v miku-frontend-caddy-config:/config caddy:2
python3 -B scripts/frontend.py info
)
```

浏览器打开最后打印的 `https://前端域名/随机路径/`，使用 **A 的登录口令**。B 无需另建账号，也不用复制 A 的口令文件、私钥或 `docker-data`。Caddy 的安装、启动和证书续期由上面的容器完成，相关行为见 [Caddy 官方 Docker 镜像](https://hub.docker.com/_/caddy)与[自动 HTTPS](https://caddyserver.com/docs/automatic-https)。

**验证是否完成：**A 的 `backend status` 正常；B 的 `python3 -B scripts/frontend.py status` 正常；浏览器能登录前端并读取节点。后端实际创建的节点端口仍在 A 上放行。终端 `sub` 使用后端发布地址，需要相应可达；网页订阅使用当前前端入口。

<a id="frontend-https"></a>

## 同机前端绑定域名与 HTTPS（可选）

原生 / 同机 Docker 可以先使用 IP 地址，不必购买域名。以下给**方式二的独立前端**添加域名；先将域名 DNS 指向 VPS，放行 TCP 80 / 443，并确保这两个端口未被其他 Web 服务占用。

```bash
(
set -eu
cd /opt/miku-gate-docker
python3 -c '
from pathlib import Path
import re
domain = input("前端域名（例如 panel.example.com）：").strip()
assert re.fullmatch(r"[A-Za-z0-9.-]+", domain), "请只填域名"
p = Path(".env")
text = p.read_text() if p.exists() else Path(".env.example").read_text()
if p.exists():
    from datetime import datetime
    backup = Path(".env.backup-" + datetime.now().strftime("%Y%m%d%H%M%S%f"))
    backup.write_text(text)
    backup.chmod(0o600)
keys = {"MIKU_FRONTEND_LISTEN", "MIKU_FRONTEND_ORIGIN"}
lines = [line for line in text.splitlines() if line.split("=", 1)[0].strip() not in keys]
lines += ["MIKU_FRONTEND_LISTEN=127.0.0.1", "MIKU_FRONTEND_ORIGIN=https://" + domain]
p.write_text("\n".join(lines) + "\n")
p.chmod(0o600)
'
bash install-docker.sh frontend restart
bash install-docker.sh frontend info
)
```

接着直接执行下面整块，启动 Caddy 并自动申请证书。它读取当前真实端口，不需要自行填写证书路径：

```bash
(
set -eu
cd /opt/miku-gate-docker
frontend_domain=$(sed -n 's|^MIKU_FRONTEND_ORIGIN=https://||p' .env)
frontend_port=$(python3 -c 'import json; print(json.load(open("frontend/.runtime/state.json"))["port"])')
install -d -m 755 /opt/miku-gate-frontend-https
printf '%s {\n  reverse_proxy 127.0.0.1:%s\n}\n' "$frontend_domain" "$frontend_port" > /opt/miku-gate-frontend-https/Caddyfile
docker run -d --name miku-frontend-https --restart unless-stopped --network host \
  -v /opt/miku-gate-frontend-https/Caddyfile:/etc/caddy/Caddyfile:ro \
  -v miku-frontend-caddy-data:/data -v miku-frontend-caddy-config:/config caddy:2
bash install-docker.sh frontend info
)
```

已有反代的用户将完整路径转发到 `frontend info` 的本机端口即可，配置参考 [FRONTEND.md](docs/FRONTEND.md#5-配置-https)。

<a id="maintenance"></a>

## 更新与维护

### Docker 同机：在固定目录管理

```bash
cd /opt/miku-gate-docker
bash install-docker.sh frontend restart  # 只重启前端，后端与节点继续运行
bash install-docker.sh frontend logs     # 前端日志
bash install-docker.sh backend logs      # 后端日志
bash install-docker.sh backup            # 备份后端数据；.env 与反代配置另存
bash install-docker.sh stop              # 停止前后两端
bash install-docker.sh start             # 启动前后两端
```

仅停止一端时使用 `frontend stop` 或 `backend stop`。跨机 B 的前端在 `/opt/miku-gate-frontend` 使用 `python3 -B scripts/frontend.py restart / status / logs / stop`，每次只选一个子命令。A 的节点维护仍在后端部署目录执行。

**后端端口或随机路径变了：**同机运行 `bash install-docker.sh frontend restart`；跨机修改 B 的 `.env` 后执行 `python3 -B scripts/frontend.py restart`。前端不会自动发现远程后端的配置变化。

**Caddy 上游端口变了：**按对应 HTTPS 段重新读取实际端口、重新生成 `Caddyfile`，然后运行下面对应的一条，已有容器不用再执行 `docker run`：

```bash
docker exec miku-backend-https caddy reload --config /etc/caddy/Caddyfile
docker exec miku-frontend-https caddy reload --config /etc/caddy/Caddyfile
```

Caddy 的证书保存在各自的 `*-caddy-data` Docker 卷中，保留这些卷可保留账户和证书。Caddy 自动续期覆盖的是本节反代域名，Miku 后端内部签发的节点证书仍按应用原有方式维护。

### 更新应用与独立前端

- **原生：**先 `miku backup`；下载新完整包并解压，在新包目录运行 `bash install.sh --update`，然后 `miku status`、`miku info`。旧版 `miku update` 有文件数量限制，使用新包更新器更稳妥。
- **Docker：**先备份，再将新完整包的程序文件更新到部署目录，保留 `.env`、`docker-data`、`docker-certs`、`docker-backups`，运行 `bash install-docker.sh update`；仅后端用 `backend update`。
- **前端：**编辑或更新 `frontend/public/` 后，在其项目目录执行以下命令，无需编译 Go：

```bash
python3 -B scripts/export-frontend.py --refresh-manifest
python3 -B scripts/export-frontend.py
python3 -B scripts/frontend.py restart
```

`--refresh-manifest` 只用于自己明确修改的资源；下载完整包后应保持原有清单并校验。默认 exporter 不覆盖静态页面，`--from-backend` 会重新导入内嵌页面，使用前保留自定义修改。

### 安装速度与常见问题

完整 Release 包已有 amd64 / arm64 程序，VPS 不需要编译 Go。首次 Docker 安装仍要下载依赖和镜像；以后构建输入未变时复用镜像。仅需更新网页时只重启前端，避免重建后端。主动刷新后端基础镜像及依赖可用 `MIKU_DOCKER_REFRESH=1 bash install-docker.sh update`。

| 遇到的问题 | 直接检查 |
| --- | --- |
| 根目录 404 | 使用 `info` 的完整随机路径地址 |
| 登录 403 | `.env` 中 `MIKU_FRONTEND_ORIGIN` 与浏览器协议、域名、端口一致；不含随机路径 |
| API 502 / 504 | 后端状态、实际地址、路径、证书、服务器间网络；改配置后重启前端 |
| 证书未签发 | DNS 指向、80 / 443 安全组及防火墙、是否被其他服务占用；查看对应 Caddy 容器日志 |
| 不是 8899 / 8900 | 自动避让属正常现象，以 `info` 的实际端口为准 |
| 管理页面能开，节点不能连 | 在 A 放行节点实际入口端口，管理端口与节点端口分别配置 |
| 缺少 TUN | 在 VPS 服务商面板开启；只有前端的 B 不需要 TUN |

<a id="install-offline"></a>

## 方式四：手动下载与安装

从 [v0.2.7 Release](https://github.com/vioIetMiku/miku-gate/releases/tag/v0.2.7) 下载对应附件及 `SHA256SUMS`；通过 SCP / SFTP 上传到 VPS 的同一目录，再运行以下命令。GitHub 自动生成的 Source code 包不含预编译程序，优先选表中的正式附件。

| 文件 | 用途 |
| --- | --- |
| `miku` | 含两种架构的单文件安装器 |
| `miku-gate-vps-v0.2.7.tar.gz` | VPS 完整安装包 |
| `miku-gate-v0.2.7.zip` | 源码及两种架构程序；解压 ZIP 可先运行 `apt-get install -y unzip` |
| `miku-gate-frontend-v0.2.7.tar.gz` | 跨机 B 使用的前端专用包 |
| `install-online.sh` | 在线安装器 |
| `SHA256SUMS` | 附件摘要清单 |

使用单文件 `miku`：

```bash
grep '  miku$' SHA256SUMS | sha256sum -c - && bash miku --verify
bash miku                    # 原生安装
# Docker 用户用 bash miku --docker 代替上一行
```

使用完整 tar.gz：

```bash
grep '  miku-gate-vps-v0.2.7.tar.gz$' SHA256SUMS | sha256sum -c - && \
tar --no-same-owner -xzf miku-gate-vps-v0.2.7.tar.gz
cd miku-gate
bash install.sh              # 原生安装
# Docker 用户用 bash install.sh --docker 代替上一行
# 只装后端用 bash install.sh --docker --backend-only
```

**校验失败就停止，重新下载后再安装。**手动包也需要前面对应路线的环境；原生完成后运行 `miku info`，Docker 完成后在 `/opt/miku-gate-docker` 查看 `frontend info` 和 `backend info`。


## 构建与验证

源码构建需要 Go 1.26+。原生依赖与固定核心见 [CORE_ASSETS.json](CORE_ASSETS.json)。发布要求 Go race/vet、Python 维护/端口/前端回归、真实 Nginx 代理、两种页面 DOM、双架构源码构建、Docker 端口冲突/登录/持久化，以及所有附件和资源摘要验证通过。

Windows 本地静态检查不等于 Linux 容器、真实 VPN Gate 隧道或证书签发验收。完整细节见 [前后端分离指南](docs/FRONTEND.md)、[Docker 指南](docs/DOCKER.md)、[连接策略](docs/CONNECTION_POLICY.md)、[变更记录](CHANGELOG.md) 与 [验证记录](docs/VALIDATION.md)。

国旗许可证见 [FLAG_ICONS_LICENSE](FLAG_ICONS_LICENSE)，插画与字标见 [ARTWORK.md](ARTWORK.md)。项目不代表与角色权利人存在官方合作或背书。

## v0.2.7 变化

- 原生与 Docker 统一分组终端菜单；Docker 安装宿主机 `miku` 快捷入口。
- 支持选国家创建、查看出口 IP、选择槽位切换 IP、查看后台任务与订阅。
- Docker 自动准备环境，默认安静进度；登录口令与实际前端地址在安装结束显示。
- 软件包 / 官方核心缓存、下载重试、菜单改动复用核心层；`MIKU_VERBOSE=1` 可查看完整安装输出。

- Docker 默认两个独立 Compose 项目：非特权 Nginx 静态前端 + Go 后端。浏览器通过前端同源访问登录、API 和订阅，不是开放任意跨域后台接口。
- 后端负责认证、VPN、入口、节点与数据；前端仅提供静态文件和代理，重启 / 停止前端不会重启后端或节点。
- 后端优先 8899、前端优先 8900，冲突自动避让，实际端口持久化；已健康运行的前端重复启动不会被自己的端口占用逼走。
- 原生和仅后端模式继续支持内嵌页面。保留镜像构建指纹缓存、显式基础镜像刷新、quick/sub 和 v0.2.4 连接恢复策略。
- 新增前端专用包，支持前端放另一台服务器；主程序和兼容助手从源码重建为 0.2.7，不复用输入包中的旧二进制。

## 💖 赞助商

感谢以下赞助商为 **Miku Gate** 的持续开发、测试与运行提供支持。

<div align="center">

<table>
  <tr>
    <td align="center" width="520">
      <br>
      <a href="https://sponsorship.forztn.com/github.com/vioIetMiku/miku-gate">
        <strong>☁️ ForZTN</strong>
      </a>
      <br><br>
      <strong>Open Source Infrastructure Sponsor</strong>
      <br>
      <sub>为 Miku Gate 提供服务器资源与开源项目基础设施支持</sub>
      <br><br>
      <a href="https://sponsorship.forztn.com/github.com/vioIetMiku/miku-gate">
        🔗 查看 ForZTN Sponsorship
      </a>
      <br><br>
    </td>
  </tr>
</table>

</div>

## Star History

<p align="center">
  <a href="https://www.star-history.com/?repos=vioietmiku%2Fmiku-gate&amp;type=date&amp;legend=top-left">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=vioIetMiku/miku-gate&amp;type=Date&amp;theme=dark&amp;legend=top-left">
      <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=vioIetMiku/miku-gate&amp;type=Date&amp;legend=top-left">
      <img alt="MikuGate GitHub Star 数随时间变化的折线图" src="https://api.star-history.com/svg?repos=vioIetMiku/miku-gate&amp;type=Date&amp;legend=top-left" width="800">
    </picture>
  </a>
</p>
