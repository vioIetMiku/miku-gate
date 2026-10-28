## Miku Gate v0.2.5 — Docker 部署支持

保留 v0.2.4 的连接稳定性与全部 Go 应用功能，新增 Linux Docker / Compose 部署。

### 安装

```bash
# Linux VPS root Bash；先准备 Docker Engine、Compose v2 和 TUN
bash <(curl -fsSL https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/mikugate.sh) --docker
```

Docker 项目保存在 `/opt/miku-gate-docker`，本机构建镜像后后台运行。也可下载校验完整 VPS 包后 `sudo bash install.sh --docker`；Git 源码可通过 `sudo bash install-docker.sh up` 源码构建。未发布 GHCR / Docker Hub 预制项目镜像。

### 关键限制

- 需要 rootful Linux Docker、`network_mode: host`、`privileged: true`、`/dev/net/tun`；容器可影响宿主网络，不适用于 rootless 或 Desktop。
- 不得与同机原生 Miku 服务混跑，不直接复用原生数据目录。已有原生安装可继续使用原流程升级。
- 凭据、节点、核心与证书保存在项目目录的数据卷中；`backup` 停机一致性备份，`down` 保留数据。
- 升级需下载新文件后 `bash install-docker.sh update`；原生 `miku update` 不用于容器。
- Docker 暂无后台自动续证，需网页重新签发或宿主机续期后在网页应用。

附件仍为独立 `miku`、`miku-gate-vps-v0.2.5.tar.gz`、`miku-gate-v0.2.5.zip`、`install-online.sh` 和 `SHA256SUMS`。完整流程见 [Docker 指南](https://github.com/vioIetMiku/miku-gate/blob/v0.2.5/docs/DOCKER.md)。

发布前验证原生测试、Docker 离线回归、Go 竞态/vet、DOM、双架构源码构建、镜像启动与重建保留数据、备份恢复，以及附件 SHA256 / BUILD 清单与自解压校验。真实 VPN Gate 连接与证书签发仍需部署者验收。
