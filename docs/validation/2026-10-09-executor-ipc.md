# 本机特权隔离 IPC 验收 · 2026-10-09

> 范围仅为 dev.4 的 sandbox-only IPC。以下不是 OpenVPN 隧道、业务 DNS、代理出站或实际 VPS 验收。协议/API 单元测试与真实内核测试分别列出，不混用证据。

## 版本与证据

- 执行过测试的源码提交：c0c0995c6e5f53d713350542cb5f6c4c44431a3c。
- [Linux 实验运行 37905099991](https://github.com/vioIetMiku/miku-gate/actions/runs/37905099991)，job 113736562864。
- [常规 CI 37904519522](https://github.com/vioIetMiku/miku-gate/actions/runs/37904519522)，job 113734647358。
- Linux 与常规工作流均 success；下表逐项为 PASS，没有跳过这些实验。
- 环境：临时 Ubuntu 24.04.5 LTS、Linux 6.17.0-1022-azure x86_64、Go 1.26.0；不是用户 VPS。
- 已发布 dev.2 Release 的标签和压缩包没有改动，不能拿旧包重现本轮新增 IPC。

## 真实跨权限实验

| 测试 | 真实操作与观察 | 结果 |
| --- | --- | --- |
| TestLinuxIPCPeerCredentialsLifecycle | 普通 UID 65534 通过 Unix socket 创建/读取/清理真实 namespace；同组 UID 65533 与 root 客户端被拒；畸形命令及超出4条的空闲连接被拒；释放后恢复；daemon重启保留并恢复隔离 | PASS，0.10秒 |
| TestLinuxIPCStaleSocketOwnership | 有归属记录且已失去监听的旧 socket 在锁下恢复；无归属的 socket 保留并拒绝接管 | PASS，约0.00秒 |
| TestLinuxIPCClientRejectsNonRootPeer | 非 root 监听者的 socket 文件被改为 root 所有后，客户端仍依据 SO_PEERCRED 拒绝，并未发送协议请求正文 | PASS，0.01秒 |
| TestLinuxIPCHTTPControlPlane | 普通用户进程运行真实 HTTP 控制路由和 SQLite，通过真实 IPC 调用独立 root daemon，再由内核建立隔离；测试 CSRF、确认、创建核验、拒绝假VPN启动、拒绝提前删除、清理后删除 | PASS，0.18秒 |

最后一项没有注入模拟执行器，不是只用假 API 验证页面。测试 HTTP 身份为隔离数据库中的测试会话；不接触现有账号或服务器口令。

原有的7项 netexec 主测试及6个文件类型子用例也在同一次运行通过，继续覆盖 IPv4/IPv6 正反对照、丢包计数、宿主网络未变、替换 namespace、活跃进程、回滚、锁和异常文件。

## 资源边界与测量口径

两组实验在各自临时 systemd scope 中运行，配置相同的单核 CPU 带宽配额、512MiB MemoryMax、零 swap、64 tasks；GOMAXPROCS=1、GOMEMLIMIT=256MiB。

- netexec 实验读取 cgroup 文件核实 memory.max=536870912、cpu.max=100000 100000、pids.max=64、memory.swap.max=0；该组 memory.peak=10661888 bytes。
- IPC 工作流施加同样的 scope 属性；本组未另外添加逐项读取 cgroup 文件的测试，不将前一 scope 的 memory.peak 移用过来。
- IPC 实验 GNU time 输出：总耗时0.29秒，最大 RSS 150056 KiB，退出码0。
- netexec 实验 GNU time 输出：总耗时4.22秒，最大 RSS 10600 KiB，退出码0。

最大 RSS 和 cgroup memory.peak 是不同口径。IPC 组包括测试进程、真实 HTTP/SQLite 和临时子进程，但没有 OpenVPN 或实际代理流量。它不能证明1核1GB VPS的完整网关容量、持续负载或吞吐。

## 本机与常规回归

- Windows 本机 go test -count=1 ./...、go vet、Linux目标交叉编译通过。
- 37项真实 HTTP/CLI smoke 通过，包括默认关闭IPC、显式确认、实例ID一致、缺失数据库不自动初始化。
- 6项前端单元测试、TypeScript/Vite构建、格式检查通过。
- 本机 Chrome 完整旅程通过，新增“检查执行器”默认关闭行为；未把 Windows 不可用的root IPC伪造为可用。
- IPC JSON 解码器配置5秒有界fuzz，637737次执行无崩溃；不是全面安全证明。
- 远端常规 CI 通过 Go race、前端测试/构建、HTTP/CLI smoke 和 Chromium 完整旅程。

## 仍然不具备的能力

- 没有任意命令执行接口、外部路径参数或可上传的 OpenVPN 配置。
- 没有真实 VPN 隧道、TUN、业务 DNS 或代理监听。start仍为409，active_count仍为0。
- 没有生产安装器、daemon服务注册、跨机Agent或多用户权限体系。
- 不抵御恶意root；授权UID中的任何进程都可在协议限制内调用。HTTP端的会话/CSRF不取代OS用户隔离。
- 控制台的删除互锁只协调本进程；管理员不能并发运行另一个CLI修改同一出口并假设跨进程事务。

下一阶段：规范化不可信 OpenVPN 配置、受控测试服务器和隧道内 DNS/拨号器；先证明失败时流量阻断，再接通现有代理核心。
