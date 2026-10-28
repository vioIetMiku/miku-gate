# Miku Gate v0.2.6 前后端分离使用说明

v0.2.6 Docker 部署默认采用两个独立 Compose 项目。前端以非特权 Nginx 容器提供 `frontend/public/`；Go 后端负责认证、API、VPN 与节点。浏览器只访问前端，登录、API、订阅在相同源下转发，前端不保存登录口令，也不挂载后端数据、私钥或 TUN。

后端保留内嵌页面作为原生安装和仅后端部署的兼容入口。分离部署显示的页面来自独立文件，前端维护和发布不会覆盖为 Go 内嵌页面。

## 1. 在同一台 VPS 安装

要求 Linux amd64 / arm64、rootful Docker Engine、Compose v2、Python 3.10+ 和 TUN。完整说明见 [DOCKER.md](DOCKER.md)。

```bash
# 在新安装包的 miku-gate 目录运行：
sudo bash install.sh --docker

# 后续在固定部署目录操作：
cd /opt/miku-gate-docker
sudo bash install-docker.sh info
sudo bash install-docker.sh frontend info
```

`--docker --split` 仍可用，与 `--docker` 相同。若不复制到固定目录，也可在长期保留的解压目录运行 `sudo bash install-docker.sh up`，后续一直在该目录管理。

打开输出的**独立前端完整地址**，包括随机路径，输入后端生成的访问口令。根路径通常返回 404，这是预期行为。

已有 Docker 后端并更新了本包程序文件时，可以只启动前端：

```bash
sudo bash install-docker.sh frontend start
```

## 2. 自动端口如何工作

| 情况 | 后端 | 前端 |
| --- | --- | --- |
| 首次安装、端口留空 | 优先 8899 | 优先 8900 |
| 已有安装、端口留空 | 优先已保存的实际端口 | 优先上次成功启动的实际端口 |
| 显式设置端口 | 优先 `WEB_PORT` | 优先 `MIKU_FRONTEND_PORT` |
| 优先端口占用或已预留 | 逐个检查后续端口，必要时回到 1024 | 同样自动查找，并跳过后端端口 |
| 重复执行启动命令 | 保留已有后端容器 | 健康且配置相同的前端保持运行，不因自己的监听而换端口 |

检测包括 IPv4、IPv6 的 TCP 占用，并跳过已登记的节点入口 / 隧道端口。有效范围为 1024–65535。非法输入（如 80、65536、非整数）直接报错，配置文件无效也不会伪装为“端口冲突”。

后端实际端口写入 `docker-data/settings.json`；前端实际端口与访问地址写入 `frontend/.runtime/state.json`。**不用手动修改这些生成文件。** `frontend info` 读取上次成功启动的结果，不按新编辑的 `.env` 猜测地址；状态是否运行请看 `frontend status`。

例如 8899 和 8900 都被占用时，后端可选 8901，前端可选 8902。最终以输出为准。放行实际前端端口及实际节点端口，命令行输出的订阅使用后端地址时也要放行后端端口。

端口检测与真正监听之间仍有很短的竞争窗口。若另一个进程恰好抢占了新选端口，查看日志后重新执行 `start`，启动器会重新检查。端口选择不修改云安全组、宿主机防火墙或外层反向代理。

## 3. 独立管理

```bash
sudo bash install-docker.sh frontend start     # 读取后端真实配置，自动选端口并启动
sudo bash install-docker.sh frontend restart   # 重新生成配置并重建前端，后端不重启
sudo bash install-docker.sh frontend check     # 校验资源与 Nginx 候选配置，不替换运行配置
sudo bash install-docker.sh frontend info      # 实际访问地址与监听端口
sudo bash install-docker.sh frontend status
sudo bash install-docker.sh frontend logs
sudo bash install-docker.sh frontend stop
sudo bash install-docker.sh frontend down

sudo bash install-docker.sh backend status    # 只管理后端的统一入口
sudo bash install-docker.sh quick --no-domain --background
sudo bash install-docker.sh sub
```

前端停止、重启和移除不会停止运行中的节点。顶层 `stop` / `down` 现在处理两端；只停止后端请加 `backend`。`logs` 默认看后端日志。

启动器先校验资源，再用独立候选文件执行 Nginx 语法检查，通过后才替换运行配置。检查失败保留现有配置；重建失败会恢复旧配置，有健康旧实例时尝试恢复旧实例，并返回错误。可用端口耗尽、文件权限异常、无效后端或无效模板均会明确报错。

## 4. 配置变量

在实际部署目录复制 `.env.example` 为 `.env`，已有 `.env` 直接编辑。前端变量修改后执行 `frontend restart`；同时修改后端变量时使用顶层 `start`，由 Compose 重建后端并同步前端。

| 变量 | 含义 |
| --- | --- |
| `MIKU_FRONTEND_PORT` | 前端 HTTP 优先端口；留空记住实际端口，首次尝试 8900；范围 1024–65535 |
| `MIKU_FRONTEND_LISTEN` | 前端 IPv4 监听地址，默认 `0.0.0.0`；同机外层反代可用 `127.0.0.1` |
| `MIKU_FRONTEND_ORIGIN` | 浏览器公网源，如 `https://panel.example.com`，不含随机路径；同机默认 `http://公网IP:实际前端端口` |
| `MIKU_BACKEND_URL` | 后端连接源，如 `https://api.example.com:9443`；同机留空自动读取实际端口、TLS 配置 |
| `MIKU_BACKEND_BASE_PATH` | 显式后端必填，填写真实随机路径，例如 `/abc123`；无前缀填写 `/` |
| `MIKU_BACKEND_HOST` | 可选后端 Host（可含端口），并用于 TLS 证书域名校验 |

前端只接受配置的浏览器 Origin，不接受任意来源。通过另一 IP / 域名登录返回 403 时，确认访问源与 `MIKU_FRONTEND_ORIGIN` 一致后重新生成配置。Origin 不能包含 `/abc123` 等路径。

## 5. 配置 HTTPS

前端容器提供 HTTP。由已有 Nginx / Caddy 终止 HTTPS，并将**完整请求路径**转发给前端，不删除随机路径：

```dotenv
MIKU_FRONTEND_PORT=
MIKU_FRONTEND_LISTEN=127.0.0.1
MIKU_FRONTEND_ORIGIN=https://panel.example.com
```

```bash
sudo bash install-docker.sh frontend restart
sudo bash install-docker.sh frontend info
```

外层 Nginx 示例，`ACTUAL_FRONTEND_PORT` 必须替换成上述输出的实际监听端口；证书路径使用自己已有的证书：

```nginx
server {
    listen 443 ssl;
    server_name panel.example.com;
    ssl_certificate /你的证书路径/fullchain.pem;
    ssl_certificate_key /你的证书路径/privkey.pem;
    location / {
        proxy_pass http://127.0.0.1:ACTUAL_FRONTEND_PORT;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

固定的 `MIKU_FRONTEND_ORIGIN` 表示公网地址，**不会随内部端口改写**。内部端口自动避让后，需要将外层代理的上游同步为实际端口。前端 Cookie 是否带 Secure 根据公网 Origin 决定。

前端连接 HTTPS 后端时验证证书与域名，不跳过校验。同机后端已配置管理域名时，自动以该域名作为 Host / TLS 名，实际连接仍走本机。自己的 CA 需要加入前端镜像信任库；不要用无效证书来替代。

## 6. 后端端口、路径或证书变化

通过顶层 `start` / `restart` / `update` 操作后端时，脚本会在后端就绪后重新同步前端。若在网页、手动 `docker compose`、仅后端命令中修改了后端参数，或 Docker 自动重启后后端因占用换了端口，请执行：

```bash
sudo bash install-docker.sh frontend restart
sudo bash install-docker.sh frontend info
```

这会重新读取后端实际端口、随机路径和 TLS 配置。前端没有后台配置监听器。远程后端还要先更新前端机 `.env` 中的地址与路径。

设置页中的“后端监听端口 / 地址 / 路径”属于后端，不能用来设置前端端口。管理域名和证书页面管理的是后端与节点，前端 HTTPS 由外层代理维护。网页内替换主程序的更新接口在分离入口返回 409，Docker 更新使用部署脚本。

网页复制的订阅基于当前前端源；终端 `sub` 使用后端配置的发布地址。两者都需保留真实路径及订阅口令。节点链接使用的入口端口与管理端口是不同配置，不会为了管理端口避让而随意修改。

## 7. 部署到另一台服务器

前端机需要 Linux、Docker / Compose v2 和 Python 3.10+，不需要后端二进制、TUN 或 Go。保持相对目录结构复制：

```text
compose.frontend.yaml
scripts/frontend.py
scripts/common.py
frontend/nginx.conf.template
frontend/ASSETS.json
frontend/public/（完整目录）
```

在共同根目录创建 `.env`（不要复制后端口令、数据目录和 `.runtime`）：

```dotenv
MIKU_FRONTEND_PORT=
MIKU_FRONTEND_LISTEN=127.0.0.1
MIKU_FRONTEND_ORIGIN=https://panel.example.com
MIKU_BACKEND_URL=https://api.example.com:9443
MIKU_BACKEND_BASE_PATH=/abc123
MIKU_BACKEND_HOST=api.example.com:9443
```

`/abc123` 替换为后端真实路径。`9443` 也只是示例，使用后端 `info` 的实际端口。启动并查看地址：

```bash
python3 -B scripts/frontend.py start
python3 -B scripts/frontend.py info
```

按第 5 节配置 HTTPS 外层代理。前后端跨服务器使用 HTTPS，并让后端证书可被前端信任。前端用户通过反代共享后端基于连接来源的登录失败冷却计数。

## 8. 前端开发与检查

`frontend/public/` 可独立编辑和发布，修改后：

```bash
python3 -B scripts/export-frontend.py --refresh-manifest
python3 -B scripts/export-frontend.py
sudo bash install-docker.sh frontend restart
```

默认命令与发布脚本只验证资源，不覆盖文件。`--from-backend` 才会用旧版 Go 内嵌页面重新导入，可能覆盖本版独立前端的 Docker 提示，使用前自行保留改动。

```bash
cd scripts
python3 -B -m unittest -v test_ports test_docker test_frontend test_frontend_lifecycle
# 有真实 Nginx 可执行文件时，启用代理集成检查：
MIKU_NGINX=/绝对路径/nginx python3 -B -m unittest -v test_frontend
```

本包的实际验证范围记录在 [VALIDATION.md](VALIDATION.md)。

## v0.2.6 发布文件与升级补充

跨服务器的前端机可直接下载 `miku-gate-frontend-v0.2.6.tar.gz`，它只含公开前端、代理配置和管理脚本，不含后端程序、密码、证书私钥和 TUN 配置。先按 SHA256SUMS 校验，再解压并按照第 7 节配置。

完整 VPS 包与完整源码 ZIP 仍提供双架构后端；本次发布会从源码重建，不复用输入 ZIP 的 v0.2.4 二进制。旧原生维护工具存在包内文件数量上限，升级用新包 `sudo bash install.sh --update`；Docker 用旧脚本先备份，再更新原项目目录，保留 .env 和数据，之后运行新脚本 update。

已有后端运行时前端可独立启动，不要求公开后端口令文件。前端 HTTPS 由外层代理维护；节点证书与后端管理证书不自动变成前端证书。
