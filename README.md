# Miku Gate

## VPN Gate 是什么？

[VPN Gate](https://www.vpngate.net/) 是日本筑波大学开展的学术实验项目，由世界各地志愿者提供公共 VPN 中继。Miku Gate 使用其公开的 OpenVPN 配置，为自己的 Linux VPS 管理多个出口。

VPN Gate 不属于 Miku Gate。节点数量、国家、在线时间随时变化，**不保证住宅 IP、固定出口、在线率或严格隐私，也不是无日志服务**。请阅读[官方介绍](https://www.vpngate.net/en/about_overview.aspx)与[日志和反滥用政策](https://www.vpngate.net/en/about_abuse.aspx)，遵守当地法律与服务商规则。

## 感谢 fanout

特别感谢 **[byJoey/fanout](https://github.com/byJoey/fanout)**。Miku Gate 基于其按国家批量出口、网络命名空间隔离和自动重连能力，再加入 Miku 界面、多格式订阅、终端一键配置、Docker 与前后端分离。

基线提交：`d7ce5224caf3469b19876982abdf8d1be16c4a8b`。原 MIT 许可证和作者署名保留，见 [LICENSE](LICENSE)、[上游说明](README.fanout.md) 与 [源码来源](PROVENANCE.md)。

<p align="center"><img src="src/assets/miku-hero-v1.png" alt="Miku Gate 插画" width="260"></p>
<h2 align="center">世界，等你连接。</h2>

**代码版本：v0.2.6**，正式状态以 [Releases](https://github.com/vioIetMiku/miku-gate/releases/latest) 为准。支持 Linux amd64 / arm64；完整安装包已编译，VPS 无需安装 Go。

## v0.2.6 变化

- Docker 默认两个独立 Compose 项目：非特权 Nginx 静态前端 + Go 后端。浏览器通过前端同源访问登录、API 和订阅，不是开放任意跨域后台接口。
- 后端负责认证、VPN、入口、节点与数据；前端仅提供静态文件和代理，重启 / 停止前端不会重启后端或节点。
- 后端优先 8899、前端优先 8900，冲突自动避让，实际端口持久化；已健康运行的前端重复启动不会被自己的端口占用逼走。
- 原生和仅后端模式继续支持内嵌页面。保留镜像构建指纹缓存、显式基础镜像刷新、quick/sub 和 v0.2.4 连接恢复策略。
- 新增前端专用包，支持前端放另一台服务器；主程序和兼容助手从源码重建为 0.2.6，不复用输入包中的旧二进制。

## 先准备 Docker、Compose 与 Python

官方安装链接：

- [Docker Engine 总览](https://docs.docker.com/engine/install/)
- [Ubuntu](https://docs.docker.com/engine/install/ubuntu/) / [Debian](https://docs.docker.com/engine/install/debian/)
- [Compose 插件安装（docker compose）](https://docs.docker.com/compose/install/linux/)，不是旧独立命令 docker-compose。

```bash
docker info
docker compose version
python3 --version   # 前端启动器要求 3.10+
```

后端需要 Linux、rootful Docker、TUN、netns 与 iptables，使用 **host 网络 + privileged**，能够影响宿主网络；不支持 rootless / Docker Desktop。前端不需要 TUN 或 privileged，但当前 Compose 也使用 Linux host 网络。

**不要与同机原生 Miku 服务同时运行，不直接挂载原生 /var/lib/miku 做迁移。** 前后端同机默认需要两个管理端口，节点监听端口仍是独立配置。

## 快速安装

### Docker 默认前后端分离

在 VPS 的 root Bash 中执行：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/mikugate.sh) --docker
```

新正式 Release 生效后，入口下载并校验最新包，部署到 `/opt/miku-gate-docker`，启动后端和独立前端。不会自动安装 Docker。复制打印的**前端完整地址，包括随机路径**，用后端生成的口令登录。

手动下载 [v0.2.6 完整包](https://github.com/vioIetMiku/miku-gate/releases/tag/v0.2.6) 与 SHA256SUMS：

```bash
grep '  miku-gate-vps-v0.2.6.tar.gz$' SHA256SUMS | sha256sum -c -
sudo tar --no-same-owner -xzf miku-gate-vps-v0.2.6.tar.gz
cd miku-gate
sudo bash install.sh --docker
cd /opt/miku-gate-docker
sudo bash install-docker.sh frontend info
```

完整 ZIP 也包含二进制；GitHub 自动生成的纯源码压缩包不含 bin，需要在容器内源码构建。独立自解压文件 `miku` 可运行 `sudo bash miku --docker`。

### 原生安装 / 仅后端

```bash
sudo bash install.sh                         # 原生 systemd / OpenRC
sudo bash install.sh --docker --backend-only # Docker 只启动后端
```

`--docker --split` 是兼容别名，默认 `--docker` 已经分离。在线入口使用 `--docker` 即可，无需附加 `--split`。原生安装与仅后端仍有内嵌页面，但不要同机运行两套后端。

## 同机前后端分离：怎么用？

```text
浏览器 -> 独立 Nginx 前端（HTML/CSS/JS + 同源代理）
                      -> Go 后端（认证/API/订阅/VPN/节点）
                                      -> VPN Gate 出口
```

| 服务 | 配置 | 优先端口 | 权限 / 挂载 |
| --- | --- | --- | --- |
| 后端 miku | compose.yaml | 8899 | privileged / TUN；读写后端数据与证书 |
| 前端 frontend | compose.frontend.yaml | 8900 | UID 101、只读、cap_drop ALL；仅公开资源和生成配置 |

同机前端不必手填后端 URL / 路径；保持对应变量为空，启动器读取真实配置：

```bash
cd /opt/miku-gate-docker
sudo bash install-docker.sh up
sudo bash install-docker.sh frontend info
sudo bash install-docker.sh backend info
```

登录口令由后端维护，不需要在前端注册账号；前端不存后台密码，也不挂载后端私钥。根路径 404 属于随机路径保护，使用 info 打印的完整入口。

### 独立启停与管理

```bash
sudo bash install-docker.sh frontend start
sudo bash install-docker.sh frontend restart # 不重启后端 / 节点
sudo bash install-docker.sh frontend check   # 只校验候选 Nginx 配置及资源
sudo bash install-docker.sh frontend info
sudo bash install-docker.sh frontend status
sudo bash install-docker.sh frontend logs
sudo bash install-docker.sh frontend stop
sudo bash install-docker.sh frontend down
sudo bash install-docker.sh backend status
sudo bash install-docker.sh backend logs
sudo bash install-docker.sh backend stop
sudo bash install-docker.sh backend start
```

顶层 `stop` / `down` 处理两端；只停后端请带 backend，只停前端请带 frontend。quick/sub/password 仍操作后端，顶层 logs 默认看后端。

## 自动端口与防火墙

- 后端首次优先 8899，前端优先 8900；重启时优先保存的实际端口。
- WEB_PORT / MIKU_FRONTEND_PORT 是优先值，冲突会选择后续空闲端口，并避开已登记节点端口；IPv4 / IPv6 都检查。
- 合法范围 1024–65535；80、非整数、65536 等配置直接报错，不伪装为端口冲突。
- 后端保存到 `docker-data/settings.json`；前端保存到 `frontend/.runtime/state.json`，不用手改生成文件。
- 以 info 打印的实际地址为准。自动选端口**不修改**云安全组、防火墙和外层反代上游。
- 网页订阅走前端同源；终端 sub 使用后端发布地址，如需客户端可达，还要放行后端端口。节点入口 / 代理端口分别放行。

## 前端 HTTPS 与外层反向代理

前端容器只提供 HTTP，由已有 Nginx / Caddy 终止公网 HTTPS；前端内部监听 loopback：

```dotenv
MIKU_FRONTEND_LISTEN=127.0.0.1
MIKU_FRONTEND_PORT=
MIKU_FRONTEND_ORIGIN=https://panel.example.com
```

```bash
sudo bash install-docker.sh frontend restart
sudo bash install-docker.sh frontend info
```

外层 Nginx 配置示例，**将 ACTUAL_FRONTEND_PORT 替换为上一步输出的实际端口**，并使用自己的有效证书：

```nginx
server {
    listen 443 ssl;
    server_name panel.example.com;
    ssl_certificate /etc/letsencrypt/live/panel.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/panel.example.com/privkey.pem;
    location / {
        proxy_pass http://127.0.0.1:ACTUAL_FRONTEND_PORT;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

必须转发完整请求路径，不删除随机前缀。最终入口为 `https://panel.example.com/真实随机路径/`。Origin 只填源地址、不含随机路径；它决定浏览器来源校验和 Cookie Secure 标志，不要改成通配符。

网页“管理域名 / 证书”操作的是后端或节点，不是外层前端 TLS。前端 HTTPS 证书由外层反代维护。内部端口自动改变时，外层代理的上游也要手动同步。

## 前端部署到另一台服务器

### 第一步：后端 VPS

后端需要 TUN、rootful Docker 和实际节点端口。只安装后端：

```bash
sudo bash install.sh --docker --backend-only
cd /opt/miku-gate-docker
sudo bash install-docker.sh backend info
```

记录真实后端源地址和随机路径。例如 `https://api.example.com:9443/abc123/` 只是格式示例，端口和路径必须换成 info 的结果。跨机建议使用 HTTPS，证书应被前端信任；自己的 CA 要加入前端镜像信任库，不能关闭 TLS 校验。

### 第二步：前端服务器

前端机只需 Linux、Docker/Compose 和 Python 3.10+；不需要 TUN、Go、后端程序或后端私钥。下载前端专用包和 SHA256SUMS：

```bash
grep '  miku-gate-frontend-v0.2.6.tar.gz$' SHA256SUMS | sha256sum -c -
tar --no-same-owner -xzf miku-gate-frontend-v0.2.6.tar.gz
cd miku-gate
cp -n .env.example .env
```

编辑前端机 .env，将后端源地址与随机路径分开填写：

```dotenv
MIKU_FRONTEND_LISTEN=127.0.0.1
MIKU_FRONTEND_PORT=
MIKU_FRONTEND_ORIGIN=https://panel.example.com
MIKU_BACKEND_URL=https://api.example.com:9443
MIKU_BACKEND_BASE_PATH=/abc123
MIKU_BACKEND_HOST=api.example.com:9443
```

```bash
python3 -B scripts/frontend.py start
python3 -B scripts/frontend.py info
```

按前一节给前端配置外层 HTTPS 代理。用户打开 `https://panel.example.com/abc123/`，输入后端生成的口令。服务器间只需要前端能访问后端管理接口，VPN 隧道和节点监听仍在后端 VPS。

| 参数 | 填写方式 |
| --- | --- |
| MIKU_FRONTEND_ORIGIN | 浏览器访问的公网源，含协议和非标准公网端口，不含随机路径 |
| MIKU_BACKEND_URL | 前端连接后端的源地址，不含随机路径 |
| MIKU_BACKEND_BASE_PATH | 后端真实随机路径；确实无前缀时填 / |
| MIKU_BACKEND_HOST | 后端 Host / TLS 校验域名，可带端口；通常与证书域名一致 |
| MIKU_FRONTEND_PORT | 内部优先端口，留空记住实际端口、首次尝试 8900 |
| MIKU_FRONTEND_LISTEN | IPv4 监听地址；有同机外层反代时建议 127.0.0.1 |

不要把后端 .env、docker-data、证书私钥或另一台机器的 `.runtime` 整包复制到前端机。通过前端反代访问的用户会共享部分后端基于来源的登录冷却计数，避免反复尝试错误口令。

### 后端改端口 / 路径 / 域名后怎么办？

顶层 start/restart/update 会在后端就绪后同步同机前端。网页直接改后端配置、只操作 backend、或 Docker 自动重启后换了端口，需执行：

```bash
sudo bash install-docker.sh frontend restart
sudo bash install-docker.sh frontend info
```

远程前端先更新本机 .env，再运行 `python3 -B scripts/frontend.py restart`。本版没有后台配置监听器，不会自动发现远端路径改变。设置页的后端端口与路径不是前端监听设置，不要混淆。

## 独立更新前端

直接编辑 `frontend/public/` 的 HTML/CSS/JS，无需重编译 Go。修改后：

```bash
python3 -B scripts/export-frontend.py --refresh-manifest
python3 -B scripts/export-frontend.py
sudo bash install-docker.sh frontend restart
```

默认 exporter **只校验、不覆盖**；`--from-backend` 才会用 Go 内嵌兼容页重新导入，可能覆盖分离版提示，使用前另存改动。前端没有 Node.js 运行依赖，国旗资源本地提供。

启动器先校验资源与 Nginx 候选配置，通过后替换运行配置；失败保留旧配置，重建失败会恢复旧配置并尝试恢复旧实例。前端重启不会主动重启后端或已运行的节点。

## 升级、备份与数据

### 从 Docker v0.2.5 升级

1. 先在旧项目执行 `sudo bash install-docker.sh backup`，另存 .env、反代配置和自定义前端文件。
2. 下载并校验 v0.2.6 完整包，把新程序和部署文件更新到原项目目录；保留 docker-data、docker-certs、docker-backups 和 .env。
3. 运行 `sudo bash install-docker.sh update`。新版默认启动独立前端，用 frontend info 获取实际入口；只后端部署请用 backend update。

不需要删除数据或 --reinstall。旧原生维护工具的 miku update 有包内文件数限制，**从旧原生版升级推荐解压新包后执行 `sudo bash install.sh --update`**，不要用旧维护工具硬导入大包。新维护工具已提高有界清单上限，但仍做路径、权限和摘要验证。

| 目录 | 用途 |
| --- | --- |
| docker-data/ | 口令、节点、订阅、核心与证书，敏感 |
| docker-certs/ | Certbot 账户和私钥，敏感 |
| docker-backups/ | 停机一致性备份，权限 600，敏感 |
| frontend/public/ | 独立静态资源，可自行发布 |
| frontend/.runtime/ | 实际端口、路径和生成配置，不公开、不打包 |
| .env | 部署设置，需要备份但不要公开 |

```bash
sudo bash install-docker.sh backup
sudo bash install-docker.sh stop   # 两端停止
sudo bash install-docker.sh start
sudo bash install-docker.sh down   # 两端移除容器，保留数据
```

后端备份不等于完整前端站点备份，.env、自定义静态资源和外层 TLS 配置应另外保存。Docker 不运行 systemd 证书续期 timer，仍需网页重新签发或宿主机续期流程；自动端口不替你更新安全组或外层反代。

## 安装速度与首次构建

保留 0.2.5 main 的构建输入指纹缓存。运行文件相同且已有标记镜像时可复用；密码、节点、.env 和 README 不参与镜像指纹，首次仍需依赖和核心下载，不提供预构建项目镜像。前端首次还需拉取 Nginx 镜像。

需要主动刷新基础镜像与依赖安全更新时（较慢）：

```bash
sudo env MIKU_DOCKER_REFRESH=1 bash install-docker.sh update
```

## 常见问题

| 情况 | 检查 |
| --- | --- |
| 根目录 404 | 使用 frontend info 的完整随机路径入口 |
| 登录 403 | 浏览器源与 MIKU_FRONTEND_ORIGIN 是否一致，别把路径填进 Origin |
| 登录后 API 502/504 | 实际后端地址、路径、Host/TLS、跨机防火墙；同步后重启前端 |
| 后端不是 8899 | 默认只是优先值，冲突自动避让，以 backend info 为准 |
| 端口改后旧入口失效 | 重新获取实际地址；后端改配置后执行 frontend restart |
| Nginx 无权读资源 | 使用新版 staging / 启动器整理公开资源权限，不要把后端私钥整体改为公开 |
| 资源清单校验失败 | 是否漏文件；有意修改后 refresh-manifest，不要关闭校验 |
| 网页主程序更新返回 409 | Docker 应用通过宿主部署脚本 update，不在运行容器里替换二进制 |
| 停前端后节点是否断？ | 前端 stop/down 不主动停止后端；顶层 stop/down 处理两端 |

## 构建与验证

源码构建需要 Go 1.26+。原生依赖与固定核心见 [CORE_ASSETS.json](CORE_ASSETS.json)。发布要求 Go race/vet、Python 维护/端口/前端回归、真实 Nginx 代理、两种页面 DOM、双架构源码构建、Docker 端口冲突/登录/持久化，以及所有附件和资源摘要验证通过。

Windows 本地静态检查不等于 Linux 容器、真实 VPN Gate 隧道或证书签发验收。完整细节见 [前后端分离指南](docs/FRONTEND.md)、[Docker 指南](docs/DOCKER.md)、[连接策略](docs/CONNECTION_POLICY.md)、[变更记录](CHANGELOG.md) 与 [验证记录](docs/VALIDATION.md)。

国旗许可证见 [FLAG_ICONS_LICENSE](FLAG_ICONS_LICENSE)，插画与字标见 [ARTWORK.md](ARTWORK.md)。项目不代表与角色权利人存在官方合作或背书。
