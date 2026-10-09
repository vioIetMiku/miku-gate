# Linux 隔离验证记录 · 2026-10-09

> 本记录仅证明列出的隔离实验，不证明 OpenVPN、业务 DNS、代理出口或用户 VPS 已验收。发布资产与后续开发测试按源码提交区分。

## 已发布快照的首轮实验

- 版本：v0.1.0-dev.2。
- 固定提交：6de64d12684e102b17779d59bbc5bf01f45aa85d。
- [运行记录 37889209018](https://github.com/vioIetMiku/miku-gate/actions/runs/37889209018)，job 113686066916。
- 环境：GitHub 临时 Ubuntu 24.04 amd64 runner，Go 1.26.0。不是用户提供的 VPS。
- 日志中的已安装包：iproute2 6.1.0-1ubuntu6.4、nftables 1.0.9-1ubuntu0.1、util-linux 2.39.3-9ubuntu6.6、iputils-ping 3:20240117-1ubuntu0.1。
- 工作流完成状态为 success；逐项日志是 PASS，不是 SKIP。测试二进制由该提交在 runner 上重新编译，不是直接运行下载的 Release 二进制。

| 实验 | 观测 | 时间 |
| --- | --- | --- |
| TestLinuxLabQuarantineAndHostIsolation | 隔离 veth 正向 IPv4/IPv6 通信通过；装入规则后双向失败；input/output 丢包计数大于零；其他接口存在时拒绝清理；宿主接口/路由/防火墙快照未变 | 4.15 秒 |
| TestLinuxLabRefusesReplacedNamespace | 替换同名 namespace 后 inode 不同，拒绝接管和清理替代对象 | 0.03 秒 |
| TestLinuxLabRollsBackFailedPreparation | 注入规则安装失败后回收本次创建的 namespace 和归属记录 | 0.01 秒 |

以上由真实内核、iproute2、nftables 和 ping 执行。基准测试 IPv4 与文档 IPv6 地址只用于两个隔离 namespace 之间；没有宿主 veth 端点、NAT 或外网目标。

GitHub 自动验证也已通过：[control-plane run](https://github.com/vioIetMiku/miku-gate/actions/runs/37886288231)，包含 Go race、前端校验、真实 HTTP/CLI smoke 与 Chromium 浏览器回归。

## 进入 dev.3 的补充实验

已于同日完成真实执行，7 项主测试及 6 个文件子用例均 PASS，无跳过。

- 测试提交：c214584979e018023a66692432b966af3e8d240b。
- [运行记录 37893021964](https://github.com/vioIetMiku/miku-gate/actions/runs/37893021964)，job 113697975377。
- 内核：Linux 6.17.0-1022-azure x86_64；Ubuntu 24.04.5 LTS；Go 1.26.0。
- 工具：iproute2 6.1.0、nftables 1.0.9、util-linux 2.39.3、systemd 255。

覆盖：

- 活跃进程：清理必须拒绝，不能擅自杀死沙箱中的进程。
- 重启与锁：排他/共享锁生效，重新打开助手可核验并保留原 namespace。
- 文件对象：FIFO、符号链接、目录、硬链接、不安全权限和坏 JSON 被及时拒绝，不影响旁侧文件。
- 资源限制：单核 CPU 配额、512MiB 内存硬上限、零 swap、64 个 task 上限；测试读取实际 cgroup 文件验证限制确实生效。
- 额外设置 GOMAXPROCS=1、GOMEMLIMIT=256MiB，记录 cgroup memory.peak 与 GNU time 数据。

实际读回的 cgroup 数据：

| 指标 | 实测值 | 含义 |
| --- | --- | --- |
| memory.max | 536870912 bytes | 512MiB 硬上限 |
| cpu.max | 100000 100000 | 单核 CPU 带宽配额 |
| memory.swap.max | 0 | 此 scope 不使用 swap |
| pids.max | 64 | task 上限 |
| GOMAXPROCS | 1 | Go 调度并发限制 |
| memory.peak | 10498048 bytes | 资源检查时读到的本次 scope 高水位 |
| GNU time 最大 RSS | 10760 KiB | time 报告的最大 resident set，口径不等同 cgroup 高水位 |
| wall time / 退出码 | 4.29 秒 / 0 | 只计内核实验程序，不包括构建、下载、安装 |

新增的活跃进程、重启/锁和异常文件检查分别有明确的 Observed/PASS 日志；FIFO 用例验证了非阻塞打开后安全拒绝错误文件类型。归属目录必须 root 所有，此修复针对损坏/错误对象的鲁棒性，不声称发现了远程可利用漏洞。

[dev.3 常规 CI](https://github.com/vioIetMiku/miku-gate/actions/runs/37892037777) 也通过：Go race、前端构建/测试、HTTP/CLI smoke、浏览器回归。

GOMEMLIMIT 是 GC 软目标；硬边界来自 cgroup。单核 CPU 带宽配额也不代表 runner 的物理 CPU 数变成 1。本实验没有运行管理控制台或 OpenVPN；这些数字不能用作 1 核 / 1GB VPS、完整网关或代理吞吐的性能承诺。

## 重现方法与后续

使用 [Linux 沙箱手册](../LINUX_SANDBOX.md) 的源码构建和显式实验命令，或在 GitHub 手动运行 Linux isolation lab (manual)。普通单元测试不会隐式创建 namespace。

下一阶段继续受限特权 IPC、受控 OpenVPN 配置/服务器、namespace 内 DNS 与拨号器；在真实隧道失效与泄漏实验通过前，不解除 start 闸门。
