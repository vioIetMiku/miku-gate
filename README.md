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

## 先选一种安装方式，不要把下面的命令全部执行

**原来的原生一键安装完整保留，Docker 是新增选项，不是替代原生安装。**

| 你想怎么用 | 选择哪一节 | 是否要 Docker | 安装后的管理命令 |
| --- | --- | --- | --- |
| 直接装在 VPS 上，继续用原来的面板和 miku 菜单 | [方式一：原生一键安装](#install-native) | 不需要 | `miku`、`miku info`、`miku quick` |
| 在一台 VPS 上用 Docker，同时运行独立前端和后端 | [方式二：Docker 同机部署](#install-docker) | 需要 | 在部署目录运行 `bash install-docker.sh ...` |
| 后端在 VPS A，前端放到服务器 B | [方式三：跨服务器部署](#install-remote) | 两台都需要 | A 管后端；B 运行 `python3 -B scripts/frontend.py ...` |
| 不想在线运行脚本，自己下载并上传安装文件 | [方式四：手动安装](#install-offline) | 取决于选择的模式 | 仍分别使用原生或 Docker 的管理命令 |

**原生一键安装，还是这一条，不加任何参数：**

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/mikugate.sh)
```

**想用 Docker 才加 `--docker`：**

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/mikugate.sh) --docker
```

这两条命令下载的是**已发布的 Latest 正式安装器**，不是直接运行 main 的全部源码。本文的 Docker 默认前后端分离功能从 v0.2.6 开始；如果 Release 页面还没发布 v0.2.6，就不能把新文档当作旧安装包已经具有该功能。

## 所有安装方式先做这一步：进入正确的终端

**在哪里做：**通过 SSH 登录你的 Linux VPS。以下安装命令是在 **VPS 的 Bash 终端**执行，不是在本地 Windows PowerShell 执行。

- 已经用 root 登录：直接继续。
- 用的是具有 sudo 权限的普通用户：先运行下面命令，后续示例按 root 终端编写。

```bash
sudo -i
```

**这一步的作用：**进入 root Shell，后端才能创建网络命名空间、TUN 和防火墙规则。不要只在 `bash <(curl ...)` 前随意加 sudo，然后认为整段命令都已经处于 root 环境。

**后端 VPS 要求：**Linux amd64 / arm64、可用 TUN、允许 netns / iptables；原生安装还需要 systemd 或 OpenRC。只装前端的服务器 B 不需要 TUN，可跳过下面的 TUN 检查。后端 VPS 检查：

```bash
test -c /dev/net/tun && echo "TUN 已存在" || echo "未找到 TUN，请先向 VPS 服务商开启"
```

如果提示没有 TUN，先在 VPS 服务商面板开启，不能靠换成 Docker 绕过宿主机限制。

检查下载工具：

```bash
curl --version
```

只有提示没有 curl 时才安装。Debian / Ubuntu 可以执行下面这组；其他系统用自己的包管理器安装 Bash、curl 和 CA 证书：

```bash
apt-get update
apt-get install -y curl ca-certificates bash
```

**不要在同一 VPS 同时启动原生 Miku 后端和 Docker Miku 后端，也不要把原生 `/var/lib/miku` 直接挂给 Docker 当作迁移。** 安装前先选好一种，已有实例需要切换方式时先备份并停止旧服务。

<a id="install-native"></a>

## 方式一：原生一键安装（原来的方式，推荐新手）

这条路线**不需要 Docker、Compose、Node.js，也不需要提前安装 Go**。程序安装到 VPS，由 systemd / OpenRC 管理；网页由后端内嵌提供。

### 第 1 步：运行原生安装命令

**在哪里做：**上面准备好的 VPS root Bash，当前目录不限。

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/mikugate.sh)
```

**作用：**下载最新正式包、验证 SHA256 和文件清单，准备系统依赖和核心，然后安装、启动原生服务。这个命令不带 `--docker`，不会部署 Docker 容器。

### 第 2 步：按提示选择使用方式

安装完成后，交互菜单提供：

- 输入 **1**：使用网页面板管理。适合先登录页面，再添加出口、配置域名和查看节点。
- 输入 **2** 或直接回车：进入终端一键配置。随后询问节点域名；没有域名直接回车，使用 REALITY。

这两个选项只是**同一个原生服务的操作方式**，不是两种不同安装，不需要再次安装。

如果一开始就确定只想使用面板，可以用下面这条**代替第 1 步**，不要把两条都跑一遍：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/mikugate.sh) --panel
```

如果想不填写节点域名、立即开始后台一键配置，也可以用这条**代替第 1 步**：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/mikugate.sh) --quick --no-domain --background
```

`--quick` 表示安装后启动一键配置；`--no-domain` 使用 REALITY；`--background` 表示输出当前可用结果后返回终端，后台继续准备节点。

### 第 3 步：查看管理地址和登录口令

**在哪里做：**同一台 VPS root Bash，任意目录。

```bash
miku info
```

**作用：**重新打印实际管理地址和访问口令。浏览器打开输出的**完整地址，包括端口与随机路径**，例如 `http://VPS_IP:实际端口/随机路径/`。不要把例子里的文字照抄，不要只访问 VPS IP 或根目录。

打开网页之前，到服务商安全组及宿主防火墙放行 **info 显示的实际管理端口**。建立节点后，节点入口 / 代理端口还需要分别放行；管理端口开放不等于节点端口已开放。

### 第 4 步：创建节点，拿到订阅

可在网页中选择国家、数量和入口模板创建出口，也可以在 VPS 运行：

```bash
miku quick
```

**作用：**询问节点域名并启动后台配置，默认目标为日本 5、韩国 3、美国 3 个**成功且入口就绪**的节点。域名留空使用 REALITY；填写域名时需要 DNS 指向 VPS，并满足证书签发条件。公共节点不足时会继续尝试，不保证马上凑齐。

查看当前可用订阅和节点链接：

```bash
miku sub
```

**作用：**输出订阅及当前可用节点，把对应客户端支持的订阅地址导入客户端。地址包含访问凭据，不要公开分享。

Ctrl+C 只退出终端等待，不会停止后台补齐；要停止补齐但保留已成功节点：

```bash
miku quick --stop
```

### 第 5 步：原生版日常管理

以下命令都在**原生安装所在 VPS**执行，任意目录即可；不要把它们当作 Docker 维护命令。

| 命令 | 做什么 |
| --- | --- |
| `miku` | 打开快捷菜单 |
| `miku info` | 查看管理地址和口令 |
| `miku status` | 查看服务是否运行 |
| `miku password` | 重置管理口令 |
| `miku domain` | 绑定、更换或解绑后端管理域名；不是设置独立前端 HTTPS |
| `miku port` | 修改后端管理端口；改后重新运行 info |
| `miku quick` / `miku sub` | 一键配置节点 / 查看订阅 |
| `miku logs` | 查看服务日志，安装或连接异常时先看这里 |
| `miku restart` | 重启服务，现有连接可能暂时中断 |
| `miku backup` | 生成停机一致性备份；完成后恢复原运行状态 |
| `miku uninstall` | 停止并取消自启，保留程序和数据 |

`miku purge` / `miku uninstall --purge` 是**删除本服务数据和备份**的破坏性操作，不是普通更新命令；只有明确要清空时才使用。

<a id="install-docker"></a>

## 方式二：Docker 同机部署（独立前端 + 后端）

Docker 是可选方式。已经选择上面的原生安装，就不用再执行这一节。

### 第 1 步：先装好 Docker、Compose 和 Python

**在哪里做：**要运行 Docker 后端的 Linux VPS。

按发行版阅读官方说明：

- [Docker Engine 安装总览](https://docs.docker.com/engine/install/)
- [Ubuntu 官方步骤](https://docs.docker.com/engine/install/ubuntu/) / [Debian 官方步骤](https://docs.docker.com/engine/install/debian/)
- [Compose 插件安装（docker compose）](https://docs.docker.com/compose/install/linux/)，不是旧版独立的 `docker-compose`。

官方仓库安装方案可一并安装 `docker-compose-plugin`。已经安装过且能正常运行，就不必重装。检查：

```bash
docker info
docker compose version
python3 --version
```

**看到什么才算准备好：**Docker Engine 可连接、Compose 命令正常、Python 版本至少 **3.10**。Miku 安装入口不会替你安装 Docker Engine。

后端使用 host 网络、privileged 和 TUN，能够影响宿主网络；要求 rootful Docker，不支持 rootless / Docker Desktop。前端则是非特权、只读 Nginx；两端权限不同。

### 第 2 步：只运行 Docker 安装命令

**在哪里做：**同一台 VPS root Bash，当前目录不限。

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/mikugate.sh) --docker
```

**作用：**验证正式安装包，将部署文件保存到 `/opt/miku-gate-docker`，构建或复用后端镜像，再启动后端与独立前端。v0.2.6 中默认就是分离部署，不需要另外再装一套前端。

首次可能要下载基础镜像、系统依赖和核心。后续文件输入未变化时可复用已标记镜像；不要把首次构建等待误认为脚本已经无响应。

### 第 3 步：进入部署目录，确认两端状态

**在哪里做：**安装后的 VPS。与原生 `miku` 不同，宿主 Docker 维护脚本需在项目目录运行。

```bash
cd /opt/miku-gate-docker
bash install-docker.sh status
bash install-docker.sh frontend info
bash install-docker.sh backend info
```

- `cd`：进入部署文件、.env 和持久化数据所在目录。
- `status`：查看前端与后端容器状态。
- `frontend info`：显示浏览器应打开的**独立前端完整地址**。
- `backend info`：显示后端地址和访问口令。前端登录用这个口令，不另建账号。

浏览器打开前端 info 的完整地址，并放行输出的实际前端端口。后端首次优先 8899、前端优先 8900，但冲突会自动换端口，**不要固定认为一定是这两个端口**。

### 第 4 步：在网页或终端创建节点

网页中的节点操作仍由后端执行。想用终端一键配置，在 `/opt/miku-gate-docker` 运行：

```bash
bash install-docker.sh quick --no-domain --background
bash install-docker.sh sub
```

第一条使用 REALITY 启动后台补齐；第二条查看当前订阅和可用节点。Docker 服务启停、更新、备份用 `install-docker.sh`，不要用宿主原生安装的 `miku update` 或 `miku backup`。

### 第 5 步：确认访问路径与权限

同机部署通常不必填写 `MIKU_BACKEND_URL`、`MIKU_BACKEND_BASE_PATH` 等参数；启动器会读取后端的实际端口、路径和 TLS 配置。只有跨服务器或特别指定后端时才手工填写。

前端网页、登录、API 和订阅在同一源下访问；后端仍负责认证与数据。根路径 `/` 返回 404 是随机路径保护，使用完整入口即可。

## Docker 两端关系与独立管理

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
bash install-docker.sh up
bash install-docker.sh frontend info
bash install-docker.sh backend info
```

登录口令由后端维护，不需要在前端注册账号；前端不存后台密码，也不挂载后端私钥。根路径 404 属于随机路径保护，使用 info 打印的完整入口。

### 独立启停与管理

```bash
bash install-docker.sh frontend start
bash install-docker.sh frontend restart # 不重启后端 / 节点
bash install-docker.sh frontend check   # 只校验候选 Nginx 配置及资源
bash install-docker.sh frontend info
bash install-docker.sh frontend status
bash install-docker.sh frontend logs
bash install-docker.sh frontend stop
bash install-docker.sh frontend down
bash install-docker.sh backend status
bash install-docker.sh backend logs
bash install-docker.sh backend stop
bash install-docker.sh backend start
```

顶层 `stop` / `down` 处理两端；只停后端请带 backend，只停前端请带 frontend。quick/sub/password 仍操作后端，顶层 logs 默认看后端。

## 自动端口与防火墙

本节描述 Docker 前后两端。原生版不需要独立前端端口，实际管理端口以 `miku info` 为准。

- 后端首次优先 8899，前端优先 8900；重启时优先保存的实际端口。
- WEB_PORT / MIKU_FRONTEND_PORT 是优先值，冲突会选择后续空闲端口，并避开已登记节点端口；IPv4 / IPv6 都检查。
- 合法范围 1024–65535；80、非整数、65536 等配置直接报错，不伪装为端口冲突。
- 后端保存到 `docker-data/settings.json`；前端保存到 `frontend/.runtime/state.json`，不用手改生成文件。
- 以 info 打印的实际地址为准。自动选端口**不修改**云安全组、防火墙和外层反代上游。
- 网页订阅走前端同源；终端 sub 使用后端发布地址，如需客户端可达，还要放行后端端口。节点入口 / 代理端口分别放行。

## 前端 HTTPS 与外层反向代理

**在哪里做：前端所在的机器。** 同机部署就是那台 VPS；跨服务器部署就是前端服务器 B。下面以已经安装 Nginx 并持有有效证书为例，不会自动替你购买域名或签发外层证书。

1. 先将前端域名的 DNS 记录指向前端服务器，准备匹配该域名的有效 HTTPS 证书。
2. 进入前端项目目录，用 `nano .env` 或 `vi .env` 编辑以下设置并保存；前端内部只监听 loopback：

```dotenv
MIKU_FRONTEND_LISTEN=127.0.0.1
MIKU_FRONTEND_PORT=
MIKU_FRONTEND_ORIGIN=https://panel.example.com
```

```bash
python3 -B scripts/frontend.py restart
python3 -B scripts/frontend.py info
```

3. 上面两条命令在**前端包或完整项目的根目录**执行，两种包都支持：分别重新生成前端配置、打印实际监听地址。记下实际端口。
4. 编辑已有外层 Nginx 的站点配置；使用 conf.d 的安装通常可新建 `/etc/nginx/conf.d/miku-frontend.conf`。已有同域名站点时合并修改，不要重复创建冲突的 server。配置示例如下，**将 ACTUAL_FRONTEND_PORT 替换为上一步输出的实际端口**，域名和证书路径也改成自己的：

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

5. 保存站点配置后先检查语法；检查通过才加载。以下重载命令适用于 systemd 管理的 Nginx：

```bash
nginx -t && systemctl reload nginx
```

如果 Nginx 由 OpenRC、Docker 或其他管理器运行，使用该管理器的重载方式，不要套用 systemctl。`nginx -t` 失败时先修正报错，不要强制覆盖或关闭校验。

6. 在浏览器打开前端公网 HTTPS 的**完整随机路径地址**。必须转发完整请求路径，不删除随机前缀。最终入口为 `https://panel.example.com/真实随机路径/`。Origin 只填源地址、不含随机路径；它决定浏览器来源校验和 Cookie Secure 标志，不要改成通配符。

网页“管理域名 / 证书”操作的是后端或节点，不是外层前端 TLS。前端 HTTPS 证书由外层反代维护。内部端口自动改变时，外层代理的上游也要手动同步。

<a id="install-remote"></a>

## 方式三：前端部署到另一台服务器

### 第 1 步：在后端 VPS A 安装后端

**在哪里做：后端 VPS A。** 按“方式四”的步骤下载完整包、上传并解压，进入包内 `miku-gate` 目录，再执行：

```bash
bash install.sh --docker --backend-only
cd /opt/miku-gate-docker
bash install-docker.sh backend info
```

**作用：**这组命令只部署后端，不启动前端；`backend info` 打印后端实际入口和登录口令。记录真实后端源地址和随机路径。例如 `https://api.example.com:9443/abc123/` 只是格式示例，端口和路径必须换成 info 的结果。跨机建议使用 HTTPS，证书应被前端信任；自己的 CA 要加入前端镜像信任库，不能关闭 TLS 校验。

### 第 2 步：在前端服务器 B 安装前端

**在哪里做：另一台前端服务器 B，不是 VPS A。** 按“方式二第 1 步”准备 Linux Docker/Compose 和 Python 3.10+；B 不需要 TUN、Go、后端程序或后端私钥。在 B 上上传前端专用包和 SHA256SUMS，进入这两个文件所在目录后运行：

```bash
grep '  miku-gate-frontend-v0.2.6.tar.gz$' SHA256SUMS | sha256sum -c -
tar --no-same-owner -xzf miku-gate-frontend-v0.2.6.tar.gz
cd miku-gate
cp -n .env.example .env
```

**上面四条命令依次是：**校验下载文件 → 解压 → 进入前端包目录 → 首次建立配置文件。已有 `.env` 不要覆盖。

用你熟悉的编辑器打开前端机 B 的 `.env`（例如 `nano .env` 或 `vi .env`），将下面示例中的域名、后端端口、随机路径换成真实值，再保存。后端源地址与随机路径必须分开填写：

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

**这两条命令的作用：**`start` 校验资源和 Nginx 配置后启动 B 上的前端容器，`info` 打印实际监听端口及完整访问地址。

接着按“前端 HTTPS 与外层反向代理”一节在 B 上配置公网 HTTPS，代理到 info 显示的实际前端端口。用户打开 `https://panel.example.com/abc123/`，输入后端生成的口令。服务器间只需要前端能访问后端管理接口，VPN 隧道和节点监听仍在后端 VPS。

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
bash install-docker.sh frontend restart
bash install-docker.sh frontend info
```

远程前端先更新本机 .env，再运行 `python3 -B scripts/frontend.py restart`。本版没有后台配置监听器，不会自动发现远端路径改变。设置页的后端端口与路径不是前端监听设置，不要混淆。

## 独立更新前端

直接编辑 `frontend/public/` 的 HTML/CSS/JS，无需重编译 Go。修改后：

```bash
python3 -B scripts/export-frontend.py --refresh-manifest
python3 -B scripts/export-frontend.py
python3 -B scripts/frontend.py restart
```

默认 exporter **只校验、不覆盖**；`--from-backend` 才会用 Go 内嵌兼容页重新导入，可能覆盖分离版提示，使用前另存改动。前端没有 Node.js 运行依赖，国旗资源本地提供。

启动器先校验资源与 Nginx 候选配置，通过后替换运行配置；失败保留旧配置，重建失败会恢复旧配置并尝试恢复旧实例。前端重启不会主动重启后端或已运行的节点。

<a id="install-offline"></a>

## 方式四：下载文件后手动安装

这是不想在线执行脚本时的替代方式，**不是原生 / Docker 安装之后还要再做一次**。

### 第 1 步：在本地下载，再上传到 VPS

从 [v0.2.6 Release](https://github.com/vioIetMiku/miku-gate/releases/tag/v0.2.6) 下载文件和 `SHA256SUMS`。该版本尚未发布时不要使用不存在的下载地址；可先使用你已经拿到的完整安装包，或等待正式发布。

| 文件 | 用途 |
| --- | --- |
| `miku` | 单个自解压安装文件，含两种架构程序 |
| `miku-gate-vps-v0.2.6.tar.gz` | VPS 完整安装包，含后端、独立前端、部署脚本 |
| `miku-gate-v0.2.6.zip` | 完整源码和两种架构的安装程序 |
| `miku-gate-frontend-v0.2.6.tar.gz` | 只有前端部署文件，给另一台前端服务器用 |
| `install-online.sh` | 正式在线安装器，可先下载审阅 |
| `SHA256SUMS` | 上述文件的校验值 |

通过 SCP / SFTP 上传到 VPS 的一个新目录。GitHub 自动生成的 “Source code” 压缩包**不等于**本项目上传的完整 ZIP，它不含已编译的 bin 文件。

### 第 2 步：校验你实际下载的文件

**在哪里做：**VPS 上，文件与 SHA256SUMS 放在同一目录。下面两组按你下载的文件二选一。

使用单文件 `miku`：

```bash
grep '  miku$' SHA256SUMS | sha256sum -c -
bash miku --verify
```

使用 VPS tar.gz：

```bash
grep '  miku-gate-vps-v0.2.6.tar.gz$' SHA256SUMS | sha256sum -c -
tar --no-same-owner -xzf miku-gate-vps-v0.2.6.tar.gz
cd miku-gate
```

**作用：**前一条检查下载文件完整性，后一条解压并进入包根目录。校验失败先重新下载，不要修改校验值绕过去。

### 第 3 步：选择一种模式安装

| 想要的模式 | 使用单文件 miku | 已进入 tar.gz / ZIP 的 miku-gate 目录 |
| --- | --- | --- |
| 原生安装 | `bash miku` | `bash install.sh` |
| Docker，默认两端分离 | `bash miku --docker` | `bash install.sh --docker` |
| Docker，仅后端 | `bash miku --docker --backend-only` | `bash install.sh --docker --backend-only` |

**这里只选一行执行。** 原生完成后用 `miku info`；Docker 完成后进入 `/opt/miku-gate-docker`，运行 `bash install-docker.sh frontend info`。仅后端模式用 `backend info`，没有自动启动的独立前端。

`--docker --split` 保留为本地完整包的兼容别名，默认 Docker 已分离；在线一键入口直接用 `--docker`，不必加 `--split`。想在以后给已有 Docker 后端补上前端，使用 `bash install-docker.sh frontend start` 即可。


## 升级：原生和 Docker 分开操作

### 原生安装升级

**在哪里做：原生 Miku 所在 VPS，先保持旧服务及数据完好。**

1. 在任意目录运行 `miku backup`，记录打印的备份文件，并复制到安全位置。
2. 按“方式四”下载、校验新完整包，解压到一个新目录，进入其 `miku-gate` 目录。
3. 在这个**新包目录**执行以下命令。它调用新版安装工具更新原来的固定安装位置，不会创建 Docker 服务，也不要求删除旧数据：

```bash
bash install.sh --update
miku status
miku info
```

三个命令依次为：保留配置更新原生服务 → 确认运行状态 → 获取当前登录地址。更新可能短暂中断节点连接。不要使用 `--reinstall` 或 `purge` 做普通升级。

旧版 `miku update` 对包内文件数量有限制，本次含独立静态资源，旧原生安装推荐上述**新包 install.sh --update**流程。

### 备份和保留哪些目录

原生数据位于 `/var/lib/miku`、程序位于 `/usr/local/lib/miku`；Docker 数据位于其项目目录中的持久化子目录。两者不能混用备份命令或直接交换数据目录。


### 从 Docker v0.2.5 升级

1. 先在旧项目执行 `bash install-docker.sh backup`，另存 .env、反代配置和自定义前端文件。
2. 下载并校验 v0.2.6 完整包，把新程序和部署文件更新到原项目目录；保留 docker-data、docker-certs、docker-backups 和 .env。
3. 运行 `bash install-docker.sh update`。新版默认启动独立前端，用 frontend info 获取实际入口；只后端部署请用 backend update。

不需要删除数据或 --reinstall。旧原生维护工具的 miku update 有包内文件数限制，**从旧原生版升级推荐解压新包后执行 `bash install.sh --update`**，不要用旧维护工具硬导入大包。新维护工具已提高有界清单上限，但仍做路径、权限和摘要验证。

| 目录 | 用途 |
| --- | --- |
| docker-data/ | 口令、节点、订阅、核心与证书，敏感 |
| docker-certs/ | Certbot 账户和私钥，敏感 |
| docker-backups/ | 停机一致性备份，权限 600，敏感 |
| frontend/public/ | 独立静态资源，可自行发布 |
| frontend/.runtime/ | 实际端口、路径和生成配置，不公开、不打包 |
| .env | 部署设置，需要备份但不要公开 |

```bash
bash install-docker.sh backup
bash install-docker.sh stop   # 两端停止
bash install-docker.sh start
bash install-docker.sh down   # 两端移除容器，保留数据
```

后端备份不等于完整前端站点备份，.env、自定义静态资源和外层 TLS 配置应另外保存。Docker 不运行 systemd 证书续期 timer，仍需网页重新签发或宿主机续期流程；自动端口不替你更新安全组或外层反代。

## Docker 安装速度与首次构建

保留 0.2.5 main 的构建输入指纹缓存。运行文件相同且已有标记镜像时可复用；密码、节点、.env 和 README 不参与镜像指纹，首次仍需依赖和核心下载，不提供预构建项目镜像。前端首次还需拉取 Nginx 镜像。

需要主动刷新基础镜像与依赖安全更新时（较慢）：

```bash
MIKU_DOCKER_REFRESH=1 bash install-docker.sh update
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

## v0.2.6 变化

- Docker 默认两个独立 Compose 项目：非特权 Nginx 静态前端 + Go 后端。浏览器通过前端同源访问登录、API 和订阅，不是开放任意跨域后台接口。
- 后端负责认证、VPN、入口、节点与数据；前端仅提供静态文件和代理，重启 / 停止前端不会重启后端或节点。
- 后端优先 8899、前端优先 8900，冲突自动避让，实际端口持久化；已健康运行的前端重复启动不会被自己的端口占用逼走。
- 原生和仅后端模式继续支持内嵌页面。保留镜像构建指纹缓存、显式基础镜像刷新、quick/sub 和 v0.2.4 连接恢复策略。
- 新增前端专用包，支持前端放另一台服务器；主程序和兼容助手从源码重建为 0.2.6，不复用输入包中的旧二进制。
