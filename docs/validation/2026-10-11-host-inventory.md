# 2026-10-11：主机只读清单 VPS 验收

源提交 `42e9e66c848179cb0ed1eef9504f6ef78087557e`；Linux amd64 二进制 SHA256 `8bece96951e231bef120351eca58879c0aa3723a2e5088b80e7224b99fe6db28`。来自干净提交，构建信息 `vcs.modified=false`。

## 实际结果

在授权 Ubuntu VPS 分别以 UID65534 和 root 执行实际 `miku-gate inventory`，两个视图都正确报告：

- Ubuntu 24.04、TUN 存在、systemd 可用。
- OpenVPN、ip、nft、nsenter、setpriv、systemctl 六项工具可找到。
- `x-ui.service` 与 `nginx.service` 已加载且运行；x-ui 家族与 Nginx 有安装痕迹。
- 独立 xray.service、sing-box.service、caddy.service 和 3x-ui.service 在固定位置未发现；**这不能证明不存在面板内嵌 Xray、自定义或容器部署**，且 x-ui 服务名不能单独区分 x-ui/3x-ui 分支。
- 所有组件的版本为未知、`reuse_ready=false`；不把存在文件或服务当作入口已识别和可复用。

命令由一次性资源 scope 限单核/128MiB/零scope swap/32 tasks/20秒（本包装器只声明所配置限制，没有另读回资源峰值）。仅将根所有的只读副本暂存于专用可执行目录，没有安装服务。返回后只删除精确本次副本与空目录。

主机接口、路由、规则、防火墙、DNS、监听与 PID/starttime、挂载策略等前后快照一致；测试目录无残留。未读取配置正文、面板数据库或私钥，未执行核心程序查版本。

证据：[机器可读摘要](evidence/2026-10-11-host-inventory.json)。原始完整报告、主机快照留在远端 root 私有证据目录；发布摘要不含客户端凭据、明文 VPS IP、实际服务 PID 或原始配置。

## 自动测试

同提交 GitHub verify 工作流 `38065913983`（job `114253404226`）成功，包含 Linux Go race、前端/15项 Python/50项实际 HTTP-CLI smoke、Chromium 完整旅程。Linux 浏览器路径展开 x-ui 证据列表，验证普通用户视图；不是 mock API。Windows 本机完整 Go 测试、Windows/Linux vet、9项前端测试和 Chrome 桌面/390px旅程也通过。

Windows 截图说明 Linux 检测不可用，未把本机环境冒充 VPS；移动截图禁用过渡以等待最终布局。以上不是安装器/备份回滚/现有入口复用完成证据。后续需继续实现版本/端口、用户选择与受控配置应用。
