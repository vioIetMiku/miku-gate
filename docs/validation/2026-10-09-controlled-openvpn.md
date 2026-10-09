# 受控 OpenVPN、隧道 DNS/TCP 与断线阻断验收

> dev.5 开发实验。此记录证明下面的受控路径实际运行过，不代表公共 VPN 节点可用、用户 VPS 已部署、代理入口可用或产品已可生产发布。

## 版本与可复核来源

- 成功验收源码：04292ec461aa3a46ff1c8e71f322c03cba4c63cb。
- [OpenVPN运行37927092137](https://github.com/vioIetMiku/miku-gate/actions/runs/37927092137)，job 113808535720。
- [常规CI37927089304](https://github.com/vioIetMiku/miku-gate/actions/runs/37927089304)，job 113808519827。
- 环境：临时Ubuntu 24.04.5 LTS，Linux 6.17.0-1022-azure x86_64，Go 1.26.0。
- 实际程序：OpenVPN 2.6.19，OpenSSL 3.0.13，nftables 1.0.9，util-linux 2.39.3。
- 两个namespace、测试证书、DNS与TCP服务均由本次实验创建；未访问公共VPN或用户VPS。已发布dev.2标签和资产未替换。

## 正反对照及结果

TestLinuxOpenVPNControlledTunnelAndFailClosed 整条旅程 PASS（4.07秒），不是SKIP。它包含：

| 验证点 | 观测与断言 |
| --- | --- |
| 服务器身份负向对照 | 错误的预期服务器名不能完成初始化，日志必须含明确的VERIFY X509NAME ERROR，不能用泛化握手失败冒充通过 |
| 正常mTLS隧道 | 正确的客户端/服务器证书通过握手，独立namespace中建立真实TUN接口 |
| 路由与DNS推送边界 | 客户端没有应用服务端推送的默认/文档地址路由；业务DNS只使用指定的隧道resolver，宿主resolv.conf未改变 |
| TCP实际来源 | 字面IP、正常DNS域名和UDP截断后TCP解析均能访问受控TCP服务，服务观察到TUN网段源地址 |
| 目标策略 | localhost/元数据字面IP、私网DNS以及混入私网的DNS结果整体被拒绝；生产策略仍拒绝实验保留地址 |
| DNS失败 | 指定测试域超时后拨号失败，没有宿主解析/hosts/NSS后备；同一时刻的数字IP请求仍成功，证明不是整个拓扑失效 |
| 已建立连接失效 | 在连接已成功传输后终止OpenVPN，TUN失效；该连接再次传输必须失败 |
| 新连接失效 | 新的隧道worker因缺失/失效接口不能创建可用拨号器 |
| 底层路径绕行负向对照 | 明确给客户端namespace设置经底层veth的默认路由，再用测试专属原始socket尝试TCP/DNS；内核仍丢弃，服务端计数未增加 |
| 宿主与清理 | 宿主接口、IPv4/IPv6路由、nftables、DNS快照一致；临时进程退出后只回收自建且inode匹配的namespace |

程序的初始化日志仅用于等待启动阶段，正向服务来源、错误证书对照和失败后的计数器才构成此处的验证。实验没有对外发布代理监听。

## 计数器证据

| 指标 | 终止VPN前 | 终止VPN及绕行尝试后 |
| --- | --- | --- |
| 客户端默认丢弃计数 | 4 | 10 |
| 受控DNS UDP请求 | 6 | 6 |
| 受控DNS TCP请求 | 1 | 1 |
| 受控TCP连接 | 5 | 5 |
| 成功业务消息 | 5 | 5 |
| 非TUN来源请求 | 0 | 0 |

这组前后对照同时表明测试确有可工作的服务，失效后没有成功绕行请求，且内核记录了丢弃尝试。它不是对所有故障条件或恶意root的完整证明。

## 资源限制与测量口径

工作流通过临时systemd scope设置CPUQuota=100%、MemoryMax=512M、MemorySwapMax=0、TasksMax=64；Go侧GOMAXPROCS=1、GOMEMLIMIT=256MiB。该组未另外读取cgroup文件逐项核验，不能把先前隔离实验的配额读取结果移用于此组。

GNU time 的结果为wall time 4.07秒、最大RSS 14068 KiB、退出码0。最大RSS不是所有VPN/worker进程的内存总和，也不等同cgroup峰值；不能宣称完整网关仅需14MB或已通过1核1GB VPS容量测试。

## 失败与修正也保留记录

1. 初次新工作流的说明文本换行破坏了YAML标量，GitHub拒绝workflow_dispatch。提交790490b修复并用Prettier的YAML解析检查确认，不涉及运行代码。
2. [初次实际OpenVPN运行37924480129](https://github.com/vioIetMiku/miku-gate/actions/runs/37924480129)在负向证书断言处失败。mute 5隐藏了精确拒绝原因，只能看到通用certificate verify failed；不能算身份验证通过。
3. 提交04292ec固定mute 0，仍使用16KiB内存日志上限和verb 3；保留原来的精确身份断言，再次运行后完整旅程通过。没有放松证书、路由或断线断言。

没有覆盖早期失败日志或修改既有Release来隐藏失败。

## 常规回归与剩余工作

成功提交的常规CI也通过Go race、前端测试/构建、37项真实HTTP/CLI smoke、Chromium完整旅程。本机Go测试/vet、Linux双架构交叉编译与规范化/DNS单元测试通过。成功源码再次进行两组各配置5秒的本机有界fuzz：配置规范化700481次、DNS回答解析1070925次，均无崩溃；不等于完整安全审计。

剩余关键工作：生产权限分离、受限运行时协调器、配置来源/节点策略、worker连接交接、真实混合代理入口、自动重连/切换与更多故障注入。当前Web start仍返回409，active_count=0，sandbox-only IPC也没有接受任意VPN配置的新操作。

操作/设计见 [CONTROLLED_OPENVPN.md](../CONTROLLED_OPENVPN.md)。
