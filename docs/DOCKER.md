# Docker 安装

此目录新增 Docker 部署方式，原来的 systemd / OpenRC 安装流程继续保留。Docker 不需要在宿主机安装 Go、OpenVPN 或 systemd 服务；运行依赖位于镜像中。

## 环境要求

- Linux amd64 / arm64 VPS，安装 [Docker Engine](https://docs.docker.com/engine/install/) 和 [Docker Compose v2 插件](https://docs.docker.com/compose/install/linux/)，构建使用 BuildKit。
- 使用 rootful Docker，当前账号能够运行 `docker info`；通常在 VPS 上使用 root 或 `sudo bash install-docker.sh`。
- 宿主机提供字符设备 `/dev/net/tun`。缺少时先在服务商面板开启 TUN；支持的 VPS 上可尝试 `sudo modprobe tun`。
- 可以访问 Docker 镜像仓库及 `CORE_ASSETS.json` 中固定版本的官方 Xray / sing-box 下载源；源码构建还需要 Go 模块下载源。镜像构建时校验核心的大小和 SHA256，失败会停止构建。
- 在防火墙和 VPS 安全组放行管理端口，以及之后实际创建的入口 / 代理端口。

Compose 使用 `network_mode: host` 和 `privileged: true`：程序需要创建 netns、挂载命名空间、调用 setns、配置 TUN、iptables 与 IPv4 转发。容器共享宿主机网络，不需要配置 `ports`。这些权限适用于自己管理的 VPS；rootless Docker、普通受限容器及 Docker Desktop 不作为支持环境。相关行为见 [Docker host 网络说明](https://docs.docker.com/engine/network/drivers/host/) 和 [Compose 服务配置](https://docs.docker.com/reference/compose-file/services/)。

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

默认命令为 `up`：校验 Compose 配置、构建镜像、后台启动，等待健康检查通过后打印管理地址和口令。首次默认管理端口为 **8899**；被其他服务占用时会明确报错，请在 `.env` 中指定其他端口后重新运行。

此 Docker 补充为本地改进，尚未发布远程 Release；当前应使用随包文件，不能假定远程 Latest 已包含 `--docker`。

附件的完整安装包包含 amd64 / arm64 预编译主程序，Dockerfile 按目标架构验证 `BUILD.json` 的 SHA256 后选取，不需要在 VPS 编译 Go。纯 Git 源码目录缺少 `bin/` 时，安装脚本自动选择镜像内源码构建；完整源码 ZIP 或 Git 目录也可显式执行以下命令（仅含预编译文件的 VPS tar.gz 不支持源码构建）：

```bash
MIKU_BINARY_SOURCE=source bash install-docker.sh up
```

手动使用 Compose 时，源码目录需要设置 `MIKU_BINARY_SOURCE=source`；完整安装包直接执行：

```bash
docker compose up -d --build --wait --wait-timeout 180
docker compose exec miku miku info
```

镜像自带 tini 接收停止信号并回收子进程，服务直接运行；Compose 允许 90 秒清理 VPN / netns。默认 `unless-stopped` 重启策略使 Docker 守护进程重启后恢复服务，手动停止的容器继续保持停止状态。

## 配置

`.env.example` 可复制成 `.env`，已有 `.env` 请直接编辑，不要覆盖。

| 变量 | 默认行为 |
| --- | --- |
| `WEB_PORT` | 留空：首次 8899，后续沿用已保存的网页设置；填写 1024–65535：每次启动以此端口为准 |
| `MIKU_PUBLIC_IP` | 留空自动探测；指定时须为 VPS 公网 IPv4，用于节点分享地址 |
| `MIKU_MAX_SLOTS` | `0`，不设置软件隧道数量上限；正整数设置上限 |
| `MIKU_BINARY_SOURCE` | 完整包默认 `prebuilt`；可设 `source` 在镜像内编译 |

修改 `.env` 后执行 `bash install-docker.sh start`，由 Compose 按配置重建容器。`restart` 只重启已有容器，不会读取变更后的环境变量。

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
bash install-docker.sh stop
bash install-docker.sh start
bash install-docker.sh restart
```

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

```bash
bash install-docker.sh backup
```

备份先停止服务，归档数据和两个 Certbot 目录，完成或失败后恢复备份前的运行状态；备份前已停止则继续停止。生成的 `docker-backups/miku-docker-时间-进程号.tar.gz` 权限为 `600`，包含私钥和口令，请另存到安全位置。

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

`update` 根据当前目录的程序重新构建镜像并重建容器，不会自动下载新版本。镜像更新保留数据目录里已有的 Xray / sing-box，避免覆盖已配置的核心。不要通过网页的主程序更新功能管理 Docker 镜像版本；请使用此更新流程。

## 验证范围

正式发布流程包含 Docker 离线生命周期测试、Linux amd64 镜像构建、Compose 健康启动、独立 netns 创建 / 删除、备份、容器重建后的凭据与节点配置保留、停止后清理及源码模式构建。主程序仍从源码生成 amd64 / arm64 两种架构，并对两种架构的预编译选择进行摘要回归验证。

Docker 实机启动验收在 Linux amd64 CI 运行，arm64 镜像运行尚未独立验收；未在 CI 连接公共 VPN Gate 隧道或实际签发公网证书，真实节点、外部防火墙与证书签发需在部署 VPS 验收。
