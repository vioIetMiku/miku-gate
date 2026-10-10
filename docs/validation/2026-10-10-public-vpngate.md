# 2026-10-10：正式 CLI 与公共 VPN Gate 实测

## 结论与范围

源提交 `02f37dbdc080f00defcfeab90e2c6f98ad753cd2` 已在用户授权 VPS 完成一次真实公共 VPN Gate 连接，并发布为 dev.8。不是私有 CA/假 VPN 夹具，也没有替换 gateway 工厂。这里只证明单节点、短时链路；不证明全库可用、吞吐容量、长期稳定、住宅/纯净度或公网加密入口已完成。

路径：普通 UID 控制服务 → 真实认证 HTTP API → root VPN IPC → 独立低权限 OpenVPN/worker → 官方节点 → HTTPS 业务目标。代理只监听 127.0.0.1，不对外开放测试代理。

## 实际断言

1. 刷新官方 HTTPS 列表，最多选择两个符合官方命名的节点；首个成功后不继续连接。
2. 针对列表端点完成系统信任根/SAN 验证，得到 CN 与精确叶证书 SHA256；加密保存绑定快照。没有关闭服务器验证、降低全局 TLS 策略或采用无校验重试。
3. 正式启动返回 OpenVPN、内核/隧道 DNS 已验证以及公网出口 IPv4；出口 IP 与所选节点 IP 的哈希相同。
4. 使用该出口的真实认证 HTTP CONNECT 入口访问 example.com 的 HTTPS，验证 TLS 和业务响应。
5. probe、stop、停止后连接拒绝、删除测试出口成功。退出后无 scope 子进程。
6. root 包装器比较主机接口、全部路由/规则、nft、DNS、监听端口及 PID/starttime、挂载/sysctl 等快照，前后完全一致。按已确认实例/文件归属清理，没有通配递归删除。

机器证据：[JSON](evidence/2026-10-10-public-vpngate.json)。原始日志保存在忽略的本机证据目录，完整主机快照留在远端 root 私有目录，不发布凭据、数据库、原始配置或明文 IP。

## 资源和来源

- 实验时间：5.939 秒；包装器含清理：6.539 秒。
- 实际读回配额：单核、384MiB、scope swap=0、64 tasks。
- cgroup 峰值 184537088 bytes（176 MiB）；资源事件增量为零。不是稳定运行内存或容量承诺。
- Linux amd64 二进制 SHA256：`1e7d6577465568b8b4a872c37ad2790e8d08c4ad9dcbd604d3b735813ff86be4`；与 dev.8 包内二进制相同。
- 官方节点可能共享证书，指纹并不证明各节点运营者唯一。公开共享客户端证书在本次握手可用；不能推广到所有未来节点。

## 同提交 CI 的补充证据

- verify：38043299919，成功。
- Controlled OpenVPN lab：38064359162，job 114248884408，成功。实际日志包括 AEAD/CBC、错误名称与错误指纹拒绝、隧道 DNS UDP/TCP、业务 TCP、断线/底层绕行阻断；独立会话 TCP/UDP 的 UID/能力/no_new_privs 与强制退出也通过。
- Full VPN runtime lab：38064364668，job 114248900526，1.61 秒，通过实际 CLI/HTTP/IPC/worker、错 UID 拒绝、修改互锁、正常停止、控制服务 SIGKILL、root daemon 退出/重启与凭据轮换。

CI 使用私有网络和测试 CA，不把其结果混作公共节点证据。VPS 公共实验没有人为杀死志愿服务器；故障场景由受控 CI 补充验证。

## 发行与下一步

dev.8 三平台资产与 SHA256SUMS 已发布，远端文件大小与 SHA256 回读匹配。Windows 解压后的实际程序通过 48 项 HTTP/CLI smoke；arm64 仅交叉构建，未在真机验证。旧 dev.2/dev.6 资产不变。

后续仍需交互安装、域名/IP HTTPS、已有核心复用/迁移、Xray/sing-box 加密入口与证书资产管理；本次公共成功不代表整个目标完成。
