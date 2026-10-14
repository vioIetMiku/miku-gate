## Miku Gate v0.2.1 正式版

基于 dev35 更新，项目与发布包继续统一为 **Miku Gate / miku-gate**。保留 VPN Gate 介绍、byJoey/fanout 致谢、MIT 许可证及原作者署名。

### 更新内容

- 源码、测试和内嵌素材迁入 `src/`。
- 实际入口协议栈显示（传输、安全层和 VISION）。
- 证书复用及有效性检查、Webroot 签发、域名完整登录地址和外部续期来源。
- IPv4 / IPv6 端口检查，避让已有面板与入口；就绪检查核对本服务响应标识。
- 完整卸载菜单，保留停止服务但不删数据的原模式。

### 附件

- `miku-gate-vps-v0.2.1.tar.gz`：amd64 / arm64 通用 VPS 安装包。
- `miku-gate-v0.2.1.zip`：完整源码与双架构安装程序。
- `install-online.sh`：VPS 一键脚本，默认下载最新正式版。
- `SHA256SUMS`：附件校验清单。

本版包内根目录为 `miku-gate/`，手动安装使用 `sudo bash miku-gate/install.sh`。现有 `miku` 命令、服务名、`/var/lib/miku` 和 `/usr/local/lib/miku` 路径不变。

### 升级

```bash
miku backup
miku update /root/miku-gate-vps-v0.2.1.tar.gz
```

请先按 SHA256SUMS 校验下载包，或使用 [README 一键安装](https://github.com/vioIetMiku/miku-gate#vps-一键安装)。`miku uninstall` 保留数据；`miku uninstall --purge` 会删除本服务数据和备份，执行前务必将备份保存到服务目录之外。

发布流程要求 Go 竞态检查、vet、安装器回归、双架构源码重建及上传后回验全部通过，才转为正式 Release。真实 VPS 上的 VPN Gate 连通、证书签发和客户端兼容仍需部署者验收。
