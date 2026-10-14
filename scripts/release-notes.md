## Miku Gate v0.2.2 正式版

基于 dev36 更新，项目与发布包继续统一为 **Miku Gate / miku-gate**。

### 更新内容

- 修复管理域名切换时同端口监听可能关闭的问题，加入旧页面地址交接、轮询暂停和随机路径保护。
- 兼容反向代理 HTTPS 来源，正确设置 Secure Cookie 与 Origin 校验。
- 证书来源探测缓存、手动刷新、Certbot 无邮箱签发、域名末尾点规范化和旧 API 参数兼容。
- 管理监听器临时 Accept 错误恢复、协议探测与空闲客户端处理。
- 协议标签缩小，完整配置提示保留。

### 附件

- `miku-gate-vps-v0.2.2.tar.gz`：amd64 / arm64 通用 VPS 安装包。
- `miku-gate-v0.2.2.zip`：完整源码与双架构安装程序。
- `install-online.sh`：VPS 一键安装脚本。
- `SHA256SUMS`：附件校验清单。

升级前建议执行 `miku backup`，然后运行 `miku update /root/miku-gate-vps-v0.2.2.tar.gz`。发布流程要求测试、竞态检查、双架构构建及上传后 SHA256 回验全部通过，才转为正式 Release。
