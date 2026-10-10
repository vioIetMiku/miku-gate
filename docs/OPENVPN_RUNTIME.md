# 隔离 OpenVPN 会话与业务 worker

> dev.8开发：独立`vpn-executor`、会话/worker、加密节点快照和Web绑定已接线，完整目标Linux验证仍在进行。**不能把已有私有AEAD/CBC实验当成本实现已经通过，更不能宣称公共VPN节点已连通。** 正式执行结果在独立验收文档记录。

## 不修改宿主路由的路径

root执行器只在宿主打开一条已规范化、固定IP/端口的VPN传输连接，然后将这个已连接的socket描述符交给新`CLONE_NEWNET`监督进程。该网络空间初始没有宿主接口；只启用loopback与自己的TUN。

隔离空间内的受限SOCKS适配器只接受一个OpenVPN控制连接，只能使用传入的固定对端socket：TCP按字节转发，UDP保留SOCKS数据报边界并固定本地发送者。业务worker拿不到该传输socket，不能用它打开任意宿主连接。无需宿主veth、NAT、forwarding、默认路由或DNS修改。

三种身份独立：

| 进程 | UID/能力 | 可操作范围 |
| --- | --- | --- |
| 控制台 | 独立普通UID | API/存储/认证监听；经Unix socket请求已授权操作 |
| OpenVPN | 另一个普通UID，仅NET_ADMIN，no_new_privs | 在专属netns中建立TUN/地址，不拥有宿主网络或root文件权限 |
| 业务worker | 第三个普通UID，仅NET_RAW，no_new_privs | TUN绑定DNS/TCP，不能修改nft/路由，不读取VPN私钥 |
| root监督/执行器 | 已授权系统服务 | 固定端点连接、创建匿名netns、规范化配置、UID切换、生命周期/归属核验 |

监督进程会实际读回子进程UID/GID、有效能力掩码和NoNewPrivs，而不是仅相信启动参数。OpenVPN配置仅通过已封印memfd传递，归属和权限限制为相应进程；worker启动材料不含VPN私钥。

## 两层私有通信

普通控制台↔root执行器使用独立`/run/miku-gate-vpnipc`路径，复用已有IPC的路径/锁/双向SO_PEERCRED/归属恢复校验，但不复用sandbox协议或偷偷扩大sandbox操作。仅指定控制UID可请求，三个工作UID必须不同。

root监督↔worker使用继承的Unix sequenced-packet socket，消息上限2KiB，每次最多一个stream FD，原子CLOEXEC接收、拒绝普通文件/多FD/截断/未知字段和重复JSON键。FD只是一条受监督的业务stream，不把有宿主网络权限的传输socket交给调用方。

每次业务连接由worker自己执行TUN绑定DNS和目标策略，握手完成后把状态及流量经专用stream返回。至多64条worker流，失败不回到宿主解析或路由。停止、控制台租约断开、OpenVPN退出或监督进程死亡会撤销关联会话；root监督的直接子进程有parent-death信号。

## 授权与管理

管理员需选用三个不同的非root账号/UID，不能复用已有nginx/Xray账号来省略权限设计。快速安装器后续会创建专用账号并先征得确认；当前需显式运行：

```text
miku-gate vpn-executor serve --instance=实例ID --allow-uid=控制UID --tunnel-uid=OpenVPN_UID --worker-uid=业务_UID --apply
miku-gate vpn-executor info --instance=实例ID
```

以上是参数形式说明，需换成真实已确认的值；不提供默认root或公共凭据。root执行器仍只接受规范化配置、固定端点/名称、会话生命周期和TCP目标，不能提交程序、脚本、文件路径或任意环境变量。`_vpn-*`内部入口仅供带合法继承描述符的exec，不是普通用户启动隧道的后门。

## 保持的边界

- 预期服务器名称必须独立确认；规范化与共享客户端例外不能背书服务器身份。
- 内核输出只允许TUN和特定UID的loopback传输，IPv6与forward默认丢弃。业务IPv6/UDP不支持，不能隐式直连。
- 监督进程记录netns/接口ifindex/规则指纹。健康检查重新核对，状态变化或进程退出不报告健康。
- 匿名netns随所属进程/FD关闭回收，避免命名空间文件残留；仍需实测进程崩溃和所有在途流关闭。
- 首版不自动换对端重连；单次传输/会话失败后明确失败或停止，重试需建立新会话。生产自动恢复和热切换需后续单独验收。

新增Linux集成用例在私有服务端netns中启动同一生产监督/worker入口，分别覆盖TCP/UDP固定传输、实际非root能力、DNS/TCP和FD交接、正常停止与监督进程SIGKILL。它不接公共节点，不使用假的worker实现；测试服务端与证书仍是受控夹具。完成该验证后还必须验证完整Web/IPC/真实节点路径。

## 本轮接线与版本边界

- 控制服务使用`--vpn-executor`显式连接固定实例的root服务，普通代理模式不隐式启用特权功能。
- 新增`GET/PUT/DELETE /api/exits/{id}/vpn`，保存24小时内官方缓存的加密快照、明确预期服务器名称和兼容同意。保存不联网；节点库刷新不会替换已同意的快照。
- 数据库schema v4加入VPN快照，同实例数据密钥与记录身份绑定。备份/回滚必须包括数据库及`.key`。选择VPN需确认替换原代理；改回代理需`replace_vpn=true`，不允许无声改变出口模式。
- 实际VPN健康检查通过worker解析域名、验证业务HTTPS和公网出口IP，再核对namespace/ifindex/规则指纹。仅此路径成功时报告`namespace`保护；不把执行器可达当成已保护。
- 运行中所有上游、VPN、凭据、删除和sandbox变更都受互锁。会话死亡关闭监听；停止未得到执行器确认时保留明确错误，不宣称成功。
- 首次8cf9f54 VPS实验：旧AEAD/CBC用例及新UDP生产会话通过，新TCP会话在SOCKS握手失败；宿主/残留检查通过。已针对OpenVPN对数字IP使用SOCKS域名编码的行为修正，仅允许精确固定IP字符串，不开放任意域名。
- Linux的parent-death信号绑定创建线程，新增固定OS线程贯穿Start/Wait，避免Go运行时回收线程误杀健康子进程。测试夹具不借用不稳定创建线程的parent-death信号。

修正后需要新精确源码重跑TCP/UDP及完整Web/IPC链路；旧失败日志保留。当前dev.8尚未发布，不替换dev.6资产；公共VPN节点、加密入口、域名和安装目标也未因这些接线而视为完成。

最新复测：1f9c278的TCP/UDP独立会话、实际低权限/NoNewPrivs和正常/强制关闭已在VPS通过，见[独立会话验收](validation/2026-10-10-openvpn-session.md)。完整Web/VPN IPC/公共节点路径仍未被本用例覆盖，继续保留相关验收门槛。
