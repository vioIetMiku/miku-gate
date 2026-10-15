## Miku Gate v0.2.3 正式版

基于 dev41，包含 dev37—dev41 的更新，继续使用 **Miku Gate / miku-gate**。

### 更新重点

- 独立自解压安装文件 `miku`，无需 Go：`sudo bash miku`。支持选择终端一键搭建或网页面板。
- `miku quick` 后台持续准备日本 5、韩国 3、美国 3 个就绪节点，最多 33 个并发候选，失败滚动替换并刷新来源；`miku sub` 输出五种订阅及可用节点。
- 终端静默等待，Ctrl+C 输出已有结果并保留后台补齐；来源、核心、证书并行准备，管理页面启动不再等待慢来源。
- 一键节点域名更换或切回 REALITY 时复用端口与凭据；未就绪候选不进入订阅或重启恢复。
- 新版欢迎区、实时统计、IP 详情、白天/夜间主题与内置国旗；桌面双列、620px 以下单列，任务展开状态保留，默认近期实测延迟排序。

### 附件

- **miku**：自解压安装文件，含 amd64 / arm64 程序，`bash miku --verify` 只校验。
- **miku-gate-vps-v0.2.3.tar.gz**：通用 VPS 包。
- **miku-gate-v0.2.3.zip**：完整源码及双架构程序。
- **install-online.sh**：在线一键安装脚本。
- **SHA256SUMS**：上述四个附件校验。

```bash
# 已有安装：先备份，校验新包后更新
miku backup
miku update /root/miku-gate-vps-v0.2.3.tar.gz
# 之后按需启动一键配置
miku quick
```

`miku` 命令、服务名与数据目录保持兼容。节点域名与管理域名分开；不带域名使用 REALITY。33 是候选并发上限，不是成功节点数量。公共节点不足时后台持续等待，服务进程重启后需重新运行 quick 继续补齐。

本次发布要求 Go 竞态/vet、安装器与终端回归、页面 DOM 交互、双架构源码构建、自解压校验及上传后 SHA256 回验全部通过。实际 VPS 的 OpenVPN、证书签发与客户端连通需部署者验收。

感谢 VPN Gate 志愿者及 [byJoey/fanout](https://github.com/byJoey/fanout)；MIT 署名与新增国旗许可证均保留。
