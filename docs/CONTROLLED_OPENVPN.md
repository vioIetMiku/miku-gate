# 受控 OpenVPN 最小闭环

> dev.5 的开发实验，不是可直接对外使用的网关。新代码尚未接入 Web 的 start、隔离 IPC 或真实入口监听。对应Linux实测已在提交04292ec完成，见 [验收记录](validation/2026-10-09-controlled-openvpn.md)。后续目标VPS三组受控复核已在提交456e863通过，见 [VPS独立记录](validation/2026-10-09-vps-lab.md)；两者都不代表生产代理或公共节点已验收。
>
> 最初在临时Linux环境验证，随后在用户授权VPS复核；始终使用自己生成的证书和私有OpenVPN服务，不连接公共志愿者节点，不改宿主默认路由、防火墙、DNS或现有服务。

## 本轮构件

| 构件 | 功能与边界 |
| --- | --- |
| 运行配置规范化 | 从不可信文本提取有限字段、重建固定配置；不直接执行原文件 |
| TLS材料校验 | 限制PEM块类型、根CA、有效期、密钥强度、签名算法、客户端证书角色与密钥匹配 |
| 进程实验助手 | 在已经确认的非宿主namespace中启动官方OpenVPN，配置仅放在封印的memfd，不写入普通日志/文件 |
| 隧道worker拨号器 | 独立exec进程继承namespace，校验namespace与接口ifindex，DNS/TCP socket均SO_BINDTODEVICE |
| 显式DNS | 使用golang.org/x/net/dns/dnsmessage，不读宿主hosts/NSS/search配置；唯一指定resolver，A/CNAME检查及受限UDP→TCP回退 |
| 受控服务与故障实验 | 两个无宿主连接的namespace，验证TLS、DNS/TCP正向通信，再中止VPN并检查已有/新连接及底层网络绕行尝试 |

所有OpenVPN进程和服务端夹具当前仅由测试代码驱动，没有新增可对用户提供公网代理的命令。现有隔离IPC仍只接受capabilities/inspect/prepare/cleanup。

## 不可信配置如何处理

生产入口 Normalize 只接受规范形式的公网IPv4字面地址作为唯一remote；域名、私网/保留地址、多remote、冲突协议或异常端口会被拒绝。实验中的基准测试地址仅由未导出的测试路径精确允许，不放宽默认公网策略。

允许UDP4或TCP4客户端配置，但本轮真隧道首先验收UDP传输。规范化输出固定使用TLS1.2最低版本、服务器证书角色、AEAD数据加密、禁止压缩、禁止DCO以固定测试路径，以及script-security 1。CA/客户端身份只使用通过校验的内嵌PEM。

以下输入被拒绝而不是“清洗后冒险运行”：脚本钩子、插件、include/config、外部CA/密钥/认证文件、management接口、任意环境变量、重复指令、混淆引号、压缩和旧CBC/SHA1配置。交互式auth-user-pass、tls-auth/tls-crypt配置以及不符合本轮证书格式的公共节点暂不支持。

源文件中的redirect-gateway、route-delay等不会照搬。输出使用route-nopull、route-noexec，并过滤推送route、redirect-gateway、dhcp-option、block-outside-dns和IPv6接口配置。OpenVPN仍可在专属namespace设置TUN IPv4地址，业务默认路由由受控实验单独设置，不作用于宿主。

**节点附带的CA并不证明节点运营者可信。** 证书解析/加密握手与住宅属性、匿名性、节点信誉是不同问题。自签根的来源仍需外部可信策略，不能把格式校验称作信任背书。

## DNS 与 TCP 的边界

- 必须显式给出父进程记录的宿主/目标namespace身份、固定隧道接口和数字DNS端点，不能推断默认环境。
- worker通过独立exec进入目标namespace，而不是临时切换Go线程；所有Go线程继承同一网络空间。
- 每次拨号前及socket创建时校验namespace、ifindex、接口UP状态。接口消失/被同名替换或落入宿主时立即拒绝。
- DNS使用随机事务ID、完整问题匹配、有界响应和别名链；只访问指定resolver，收到匹配的截断UDP响应才向同一resolver发TCP查询。
- 不使用系统解析器、hosts、NSS、search域或再次解析主机名。只把校验后的IP字面地址交给TCP拨号。
- 业务仅支持TCP4。全部候选A地址先做公网策略检查；混入私网、元数据、IPv6、过多地址等时整体拒绝。
- DNS超时、接口不可用和TCP失败不会切换宿主路径。每次业务拨号总预算5秒，DNS单次交换2秒，最多5次有界别名查询。

用于实验的benchmark地址被精确测试策略放行；生产NewWorkerDialer仍拒绝它。这不是访问任意私网的后门。

## 实验拓扑及失败观测

客户端和服务器分别置于独立namespace。veth的两端都在实验空间中，不接入宿主。客户端先原子安装默认丢弃策略，再创建底层链路，仅允许指定VPN端点UDP端口和隧道接口流量；业务IPv6与转发保持阻断。

实验步骤：

1. 生成短期临时测试CA、服务器和客户端身份，配置通过memfd传给OpenVPN；不上传私钥或证书正文。
2. 使用错误的预期服务器名建立负向对照，必须看到TLS名称拒绝且不能初始化隧道。
3. 用正确身份建连，检查恶意/无关推送路由未应用，然后在客户端namespace设置实验默认隧道路由。
4. 指定DNS通过隧道处理UDP A查询及截断后的TCP回退；TCP服务记录源地址，必须来自TUN网段。
5. 混合/私网DNS答案被拒绝；DNS超时时不能走宿主解析，并用仍可通的字面IP作为正向对照。
6. 保持一条已建立TCP连接，终止OpenVPN使TUN失效；已有连接和新拨号必须失败。
7. 刻意在客户端namespace改设一条通往服务端的底层默认路由，再用测试专属的未绑定socket尝试TCP/DNS绕行；必须被内核阻断且服务计数不能增加。
8. 对比宿主接口、IPv4/IPv6路由、nftables和resolv.conf快照，确认未改变；清理只作用于持有已验证inode的自建实验空间。

该实验的端点、DNS、TCP服务都受本地测试控制，不用外网IP查询网站“证明防泄漏”。现阶段不覆盖IPv6隧道业务、UDP代理、双出口切换、长时间负载、自动重连或发行版兼容性矩阵。

## 显式运行

仅在授权、可恢复的Linux测试VM或临时runner执行，需要OpenVPN 2.6、iproute2、nftables、util-linux、TUN、root网络能力和time。先以普通用户编译：

```bash
mkdir -p .cache
go test -c -o .cache/openvpn-linux.test ./internal/tunnel
sudo systemd-run --quiet --scope --collect \
  --property=CPUQuota=100% --property=MemoryMax=512M \
  --property=MemorySwapMax=0 --property=TasksMax=64 \
  /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LANG=C LC_ALL=C \
    MIKU_GATE_OPENVPN_TEST=1 MIKU_GATE_LAB_EXEC_ROOT=/usr/local/lib/miku-gate-lab-exec GOMAXPROCS=1 GOMEMLIMIT=256MiB \
    /usr/bin/time -v "$PWD/.cache/openvpn-linux.test" \
      -test.v -test.parallel 1 -test.run '^TestLinuxOpenVPN' -test.timeout 3m
```

`/run`为noexec时保留挂载策略，把测试程序放入上述受校验的固定可执行根；具体权限与清理规则见 [VPS_LAB.md](VPS_LAB.md)。普通go test不运行此root实验。GitHub手动工作流Controlled OpenVPN lab (manual)执行同样步骤，只保存环境和断言结果，证书、配置memfd和测试数据库不作为产物上传。OpenVPN握手进程此时仍在受控测试中以root运行；生产接线前还要完成最小权限、目录/进程隔离和生命周期协调，不能直接拿此测试启动器部署公共节点。

## 后续接线条件

先取得本轮实测证据，再实现受限运行时协调、节点选取、可核验的健康状态、worker通信/连接交接及代理入口的关闭顺序。没有这些条件，不能只把API的409改成200。现有dev.2 Release保持原样，不包含本轮构件。

### 官方参数依据

[OpenVPN 2.6 手册](https://openvpn.net/community-docs/community-articles/openvpn-2-6-manual.html)：route-nopull过滤推送路由/DHCP选项，route-noexec禁止自动添加/删除路由，pull-filter按前缀处理推送选项。实现中的严格限制仍需实际测试，文档描述不是安全证明。
