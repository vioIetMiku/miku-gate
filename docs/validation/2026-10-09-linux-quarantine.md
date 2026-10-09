# Linux 隔离验证记录 · 2026-10-09

> 本记录仅证明列出的隔离实验，不证明 OpenVPN、业务 DNS、代理出口或用户 VPS 已验收。发布资产与后续开发测试按源码提交区分。

## 已发布快照的首轮实验

- 版本：v0.1.0-dev.2。
- 固定提交：6de64d12684e102b17779d59bbc5bf01f45aa85d。
- [运行记录 37889209018](https://github.com/vioIetMiku/miku-gate/actions/runs/37889209018)，job 113686066916。
- 环境：GitHub 临时 Ubuntu 24.04 amd64 runner，Go 1.26.0。不是用户提供的 VPS。
- 日志中的已安装包：iproute2 6.1.0-1ubuntu6.4、nftables 1.0.9-1ubuntu0.1、util-linux 2.39.3-9ubuntu6.6、iputils-ping 3:20240117-1ubuntu0.1。
- 工作流完成状态为 success；逐项日志是 PASS，不是 SKIP。

| 实验 | 观测 | 时间 |
| --- | --- | --- |
| TestLinuxLabQuarantineAndHostIsolation | 隔离 veth 正向 IPv4/IPv6 通信通过；装入规则后双向失败；input/output 丢包计数大于零；其他接口存在时拒绝清理；宿主接口/路由/防火墙快照未变 | 4.15 秒 |
| TestLinuxLabRefusesReplacedNamespace | 替换同名 namespace 后 inode 不同，拒绝接管和清理替代对象 | 0.03 秒 |
| TestLinuxLabRollsBackFailedPreparation | 注入规则安装失败后回收本次创建的 namespace 和归属记录 | 0.01 秒 |

以上由真实内核、iproute2、nftables 和 ping 执行。基准测试 IPv4 与文档 IPv6 地址只用于两个隔离 namespace 之间；没有宿主 veth 端点、NAT 或外网目标。

GitHub 自动验证也已通过：[control-plane run](https://github.com/vioIetMiku/miku-gate/actions/runs/37886288231)，包含 Go race、前端校验、真实 HTTP/CLI smoke 与 Chromium 浏览器回归。

## 进入 dev.3 的补充实验

已加入，等待该提交的远端执行结果：

- 活跃进程：清理必须拒绝，不能擅自杀死沙箱中的进程。
- 重启与锁：排他/共享锁生效，重新打开助手可核验并保留原 namespace。
- 文件对象：FIFO、符号链接、目录、硬链接、不安全权限和坏 JSON 被及时拒绝，不影响旁侧文件。
- 资源限制：单核 CPU 配额、512MiB 内存硬上限、零 swap、64 个 task 上限；测试读取实际 cgroup 文件验证限制确实生效。
- 额外设置 GOMAXPROCS=1、GOMEMLIMIT=256MiB，记录 cgroup memory.peak 与 GNU time 数据。

GOMEMLIMIT 是 GC 软目标；硬边界来自 cgroup。单核 CPU 带宽配额也不代表 runner 的物理 CPU 数变成 1。即使补充实验通过，也不能据此宣布 1 核 / 1GB VPS 的性能或 OpenVPN 容量。

## 重现方法与后续

使用 [Linux 沙箱手册](../LINUX_SANDBOX.md) 的源码构建和显式实验命令，或在 GitHub 手动运行 Linux isolation lab (manual)。普通单元测试不会隐式创建 namespace。

下一阶段继续受限特权 IPC、受控 OpenVPN 配置/服务器、namespace 内 DNS 与拨号器；在真实隧道失效与泄漏实验通过前，不解除 start 闸门。
