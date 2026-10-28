# Docker 安装（v0.2.6）

当前默认独立前端 + 后端，端口冲突自动避让。完整同机 / 跨服务器 / HTTPS 操作以 [README](../README.md) 和 [前后端分离指南](FRONTEND.md) 为准。

后端默认优先 8899，前端默认优先 8900，保存并输出真实端口，不再要求用户手改冲突端口。公开静态资源与后端数据完全分开；前端非特权，后端仍需 host 网络、privileged 和 TUN。


Docker 默认启动独立 Nginx 前端与 Go 后端，前端和后端端口冲突均会自动避让。浏览器访问最后打印的独立前端地址。详细说明见 [前后端分离](FRONTEND.md)。

此目录新增 Docker 部署方式，原来的 systemd / OpenRC 安装流程继续保留。Docker 不需要在宿主机安装 Go、OpenVPN 或 systemd 服务；运行依赖位于镜像中。

## 环境要求

- Linux amd64 / arm64 VPS，安装 [Docker Engine](https://docs.docker.com/engine/install/) 和 [Docker Compose v2 插件](https://docs.docker.com/compose/install/linux/)，构建使用 BuildKit。
- 宿主机 Python **3.10+**，用于安装、自动选端口和生成前端配置。以下管理命令在 root 终端运行，普通用户请加 `sudo`。
- 使用 rootful Docker，当前账号能够运行 `docker info`；通常在 VPS 上使用 root 或 `sudo bash install-docker.sh`。
- 宿主机提供字符设备 `/dev/net/tun`。缺少时先在服务商面板开启 TUN；支持的 VPS 上可尝试 `sudo modprobe tun`。
- 可以访问 Docker 镜像仓库及 `CORE_ASSETS.json` 中固定版本的官方 Xray / sing-box 下载源；源码构建还需要 Go 模块下载源。镜像构建时校验核心的大小和 SHA256，失败会停止构建。
- 在防火墙和 VPS 安全组放行管理端口，以及之后实际创建的入口 / 代理端口。

后端 Compose 使用 `network_mode: host` 和 `privileged: true`：程序需要创建 netns、挂载命名空间、调用 setns、配置 TUN、iptables 与 IPv4 转发。容器共享宿主机网络，不需要配置 `ports`。这些权限适用于自己管理的 VPS；rootless Docker、普通受限容器及 Docker Desktop 不作为支持环境。前端也使用 host 网络，但以非特权用户运行，移除所有 capabilities，只读挂载静态文件和生成配置，不挂载后端数据与 TUN。相关行为见 [Docker host 网络说明](https://docs.docker.com/engine/network/drivers/host/) 和 [Compose 服务配置](https://docs.docker.com/reference/compose-file/services/)。

## 新安装

完整安装包支持统一入口，会把所需文件复制到固定目录，适用于 ZIP、VPS tar.gz，也适用于重新构建后的自解压安装器：

```bash
sudo bash install.sh --docker
cd /opt/miku-gate-docker
```

默认固定目录为 `/opt/miku-gate-docker`。需要先设置参数时，首次可在命令前传入 `WEB_PORT=9900` / `MIKU_PUBLIC_IP=你的公网IPv4`；后续在固定目录编辑 `.env`。环境变量仅影响该次容器创建，长期设置请写入 `.env`。自定义部署目录可执行 `sudo MIKU_DOCKER_INSTALL_DIR=/opt/my-miku bash install.sh --docker`。再次运行统一入口会更新程序文件，保留目标目录的 `.env` 和数据目录。

独立入口使用当前目录作为部署目录：

将完整安装 ZIP 或 VPS tar.gz 解压到长期保留的目录，进入其中的 `miku-gate`。不要在临时目录中部署，后续配置和持久化数据均在该目录下。

```bash
cd /root/miku-gate
# 可选：需要自定义端口 / 公网 IP 时，先复制并编辑配置。
cp -n .env.example .env
# 编辑 .env 后安装；也可直接采用默认值。
bash install-docker.sh
```

默认命令为 `up`：校验配置、构建镜像、启动后端并等待健康，再按后端实际参数启动前端，打印完整地址和口令。后端首次优先 **8899**，前端首次优先 **8900**；优先端口占用时自动选择可用端口并保存，后续留空优先沿用实际端口。显式端口也是优先值，冲突仍自动避让。请按最终输出放行防火墙及安全组。

仅后端部署使用 `sudo bash install.sh --docker --backend-only`，或当前目录运行 `bash install-docker.sh backend up`。后端仍提供兼容页面；这不会停止已存在的前端。

此 Docker 补充为本地改进，尚未发布远程 Release；当前应使用随包文件，不能假定远程 Latest 已包含 `--docker`。

附件的完整安装包包含 amd64 / arm64 预编译主程序，Dockerfile 按目标架构验证 `BUILD.json` 的 SHA256 后选取，不需要在 VPS 编译 Go。纯 Git 源码目录缺少 `bin/` 时，安装脚本自动选择镜像内源码构建；完整源码 ZIP 或 Git 目录也可显式执行以下命令（仅含预编译文件的 VPS tar.gz 不支持源码构建）：

```bash
MIKU_BINARY_SOURCE=source bash install-docker.sh up
```

手动使用下面的 Compose 命令只管理后端，前端要额外运行启动器。源码目录需设置 `MIKU_BINARY_SOURCE=source`；完整安装包可执行：

```bash
docker compose up -d --build --wait --wait-timeout 180
docker compose exec miku miku info
bash install-docker.sh frontend start
```

镜像自带 tini 接收停止信号并回收子进程，服务直接运行；Compose 允许 90 秒清理 VPN / netns。默认 `unless-stopped` 重启策略使 Docker 守护进程重启后恢复服务，手动停止的容器继续保持停止状态。

## 配置

`.env.example` 可复制成 `.env`，已有 `.env` 请直接编辑，不要覆盖。

| 变量 | 默认行为 |
| --- | --- |
| `WEB_PORT` | 留空：优先已保存端口，首次尝试 8899；填写 1024–65535：优先尝试该端口；冲突自动避让并保存结果 |
| `MIKU_FRONTEND_PORT` | 留空：优先已保存的实际端口，首次尝试 8900；填写值也是优先值，冲突自动避让 |
| `MIKU_PUBLIC_IP` | 留空自动探测；指定时须为 VPS 公网 IPv4，用于节点分享地址 |
| `MIKU_MAX_SLOTS` | `0`，不设置软件隧道数量上限；正整数设置上限 |
| `MIKU_BINARY_SOURCE` | 完整包默认 `prebuilt`；可设 `source` 在镜像内编译 |

修改 `.env` 后执行 `bash install-docker.sh start`，由 Compose 按配置重建容器。`restart` 重启已有后端容器，再重新同步前端；不会给后端读取变更后的环境变量。前端独立变量可执行 `frontend restart` 应用。更多变量和 HTTPS 见 [FRONTEND.md](FRONTEND.md)。

默认使用容器内自建 Xray / sing-box。宿主机上 3x-ui / S-UI / xray-cf-lite 的文件不挂入容器，不会自动接管宿主机面板。原生安装与 Docker 模式应分开部署：默认工作目录相同会生成相同的 netns 名，不要在同一 VPS 同时运行两种模式，也不要直接挂载原生 `/var/lib/miku` 来做迁移。

## 首次使用与常用命令

打开安装结束打印的管理地址，使用生成的口令登录。随后可在网页管理域名、证书、入口和出口。终端一键配置继续使用原来的后台 API：

```bash
bash install-docker.sh info
bash install-docker.sh quick --no-domain --background
bash install-docker.sh quick --domain node.example.com
bash install-docker.sh sub
bash install-docker.sh password
bash install-docker.sh status
bash install-docker.sh logs
bash install-docker.sh frontend logs
bash install-docker.sh frontend info
bash install-docker.sh frontend restart
bash install-docker.sh stop
bash install-docker.sh start
bash install-docker.sh restart
```

`start` / `restart` / `update` 会在后端就绪后同步前端；`stop` / `down` 默认处理两端。加 `backend` 只操作后端，例如 `bash install-docker.sh backend stop`。前端单独停止不会影响运行中的节点。

容器内 CLI 支持 `info`、`quick`、`sub`、`password`、`status`。进程启停、更新、日志与备份由宿主机脚本管理；原生安装的 `miku update / backup / domain` 维护命令不在容器内使用。管理域名和证书改用网页设置；签发证书仍需要公网 TCP 80。

容器不运行 systemd 的证书续期 timer，Docker 模式暂不提供后台自动续期。证书到期前在网页重新签发 / 导入证书，或通过宿主机现有续期流程更新挂载的证书并在网页应用。节点域名证书也应在网页检查有效期。

## 数据、备份和恢复

下列目录相对于 `compose.yaml`，删除 / 重建容器不会删除这些目录：

| 宿主机目录 | 容器路径 | 内容 |
| --- | --- | --- |
| `docker-data/` | `/var/lib/miku` | 口令、随机管理路径、节点、订阅、核心、入口与管理证书等 |
| `docker-certs/etc/` | `/etc/letsencrypt` | Certbot 账号、签发记录、证书与续期配置 |
| `docker-certs/lib/` | `/var/lib/letsencrypt` | Certbot 工作数据 |
| `docker-backups/` | `/backups` | 停机一致性备份 |
| `frontend/.runtime/` | 前端 `/etc/miku` | 实际前端端口、访问地址和生成配置 |

```bash
bash install-docker.sh backup
```

备份先停止服务，归档数据和两个 Certbot 目录，完成或失败后恢复备份前的运行状态；备份前已停止则继续停止。生成的 `docker-backups/miku-docker-时间-进程号.tar.gz` 权限为 `600`，包含私钥和口令，请另存到安全位置。

备份不包含 `.env` 和自定义前端文件，请另外保留。前端 `.runtime` 可以由启动器重新生成，无需跨服务器复制。

恢复时在相同版本的完整安装包目录中停止服务，先将现有数据移到其他位置，再恢复三个归档目录。以下命令需要 root，`/安全位置/备份.tar.gz` 必须是自己生成且可信的备份：

```bash
bash install-docker.sh stop
restore_dir=$(mktemp -d)
tar --no-same-owner -xzf /安全位置/备份.tar.gz -C "$restore_dir"
# 确认旧数据已经另行移走，下面三个目标目录为空或尚不存在。
mkdir -p docker-data docker-certs/etc docker-certs/lib
cp -a "$restore_dir/data/." docker-data/
cp -a "$restore_dir/letsencrypt-etc/." docker-certs/etc/
cp -a "$restore_dir/letsencrypt-lib/." docker-certs/lib/
bash install-docker.sh start
```

保留 `.env`，或根据恢复的端口和 VPS 地址调整配置。跨 VPS 恢复后要更新发布 IP / 域名，并检查安全组与 DNS。

## 更新与移除

```bash
bash install-docker.sh backup
# 下载并校验新安装包，将程序、Docker 配置与脚本更新到当前目录，
# 保留 .env、docker-data、docker-certs、docker-backups。
bash install-docker.sh update

# 停止并移除容器，保留上述数据目录：
bash install-docker.sh down
```

`update` 根据当前目录的程序重新构建后端并同步前端，不会自动下载新版本。新安装包可在其解压目录再次运行 `sudo bash install.sh --docker`，更新固定部署目录中的程序文件并保留配置和数据。镜像更新保留数据目录里已有的 Xray / sing-box，避免覆盖已配置的核心。不要通过网页的主程序更新功能管理 Docker 镜像版本；请使用此更新流程。

## 常见问题

| 现象 | 处理 |
| --- | --- |
| 根路径返回 404 | 使用 `info` 输出的完整随机路径 |
| 前端显示 502 | 检查 `backend status` / `logs`；后端端口、路径、TLS 改过后执行 `frontend restart` |
| 登录 POST 返回 403 | 检查实际浏览器源与 `MIKU_FRONTEND_ORIGIN`，不要带路径；更新后执行 `frontend restart` |
| 自动换端口后公网访问不了 | 用 `frontend info` 获取实际端口，同步防火墙、安全组及外层反代上游 |
| 查看前端端口与改 `.env` 后预期不同 | `frontend info` 显示上次成功运行记录，需 `frontend restart` 才应用新值 |
| 资源校验失败 | 核对自己的前端修改后刷新 `ASSETS.json`，再重启前端；不要用校验刷新掩盖未知改动 |
| 缺少 Docker / Compose / TUN | 按环境要求安装、启用，修复后重新运行；换端口不能解决这些依赖问题 |

## 验证范围

本次实际检查与局限见 [VALIDATION.md](VALIDATION.md)。Docker Engine / TUN 容器验证由发布工作流执行；本地脚本回归和模拟后端代理检查不能证明真实 VPN Gate 节点可用。
