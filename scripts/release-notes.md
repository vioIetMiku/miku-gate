## Miku Gate v0.2.0 正式版

基于 dev34，将 VPN Gate OpenVPN 出口、自建 Xray / sing-box、已有面板适配、认证代理与多格式订阅整合到 Miku 单页控制台。包含白天 / 夜间主题、移动布局、展示模式、同国家重连、IP 信息和稳定性提示。

首先感谢 VPN Gate 学术实验及志愿者提供公共节点，也特别感谢 [byJoey/fanout](https://github.com/byJoey/fanout) 的开源基础。原 MIT 许可证与署名完整保留。

### 下载

- **miku-gate-vps-v0.2.0.tar.gz**：Linux VPS 通用安装包，包含 amd64 / arm64 的主程序与兼容导出程序。
- **miku-gate-v0.2.0.zip**：完整源码及两种架构安装二进制。
- **install-online.sh**：VPS 一键安装脚本，默认选择最新正式版；支持私有仓库 GITHUB_TOKEN。
- **SHA256SUMS**：上述三个文件的完整性校验。

包内目录保留 `miku-fanout/` 以兼容现有安装流程。所有二进制从对应提交的源码重新构建，内置版本为 `0.2.0`，不复用 dev34 的预编译文件。

### 安装与更新

请阅读 [README 的 VPS 一键安装](https://github.com/vioIetMiku/miku-gate#vps-一键安装)，尤其是私有仓库的令牌安装方式。手动安装：

```bash
grep '  miku-gate-vps-v0.2.0.tar.gz$' SHA256SUMS | sha256sum -c -
sudo tar --no-same-owner -xzf miku-gate-vps-v0.2.0.tar.gz
sudo bash miku-fanout/install.sh
```

已有安装先运行 `miku backup`，再使用新包的安装器或 `miku update /绝对路径/安装包.tar.gz`。安装要求 root、TUN、netns、iptables，以及 systemd / OpenRC；VPS 不需要安装 Go。

### 注意

旧架构的任务、远程 Agent、多账号及高级记录不会完整迁移，升级前必须备份。VPN Gate 节点由志愿者运营，不保证在线、住宅属性、固定 IP 或严格隐私；实际 VPS 的网络、面板、证书和客户端连接仍需要部署者验收。

发布流程要求离线 Go 测试 / vet、安装器回归、两种架构源码构建、压缩包与版本验证及上传后 SHA256 校验全部通过，才从草稿转为正式 Release。
