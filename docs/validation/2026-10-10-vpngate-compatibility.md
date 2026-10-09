# VPN Gate 兼容候选与双数据通道实验验收

> 2026-10-10（Asia/Shanghai），源码`478f9147def4a7b54eab80169307c915a8d131eb`。**通过的是官方样本的静态材料检查，以及自建强身份mTLS实验中的AEAD/CBC数据通道；没有连接公共VPN节点，也没有用旧共享客户端身份完成公共服务器握手。** 生产名称绑定、worker与网关接线仍未完成。

## 证据来源

- [机器可读记录](evidence/2026-10-10-vpngate-compatibility.json)，SHA-256：`b273aa1cad570a412cf0813eb74463a2dd030010c68c4bbe41d34078b490f32b`。
- [该源码CI](https://github.com/vioIetMiku/miku-gate/actions/runs/37999616208)，job114054217478：成功。
- VPS实际运行：Ubuntu24.04.5、Linux7.0.0-1013-oracle、OpenVPN2.6.19、Python3.12.3。测试二进制SHA-256 `393b6bca396dc5a195ba7eee2b6aa2d6f9fa5af703c7cfea40159243db15f9ad`。
- 官方配置调查、指纹例外与运行约束见 [VPNGATE_COMPATIBILITY.md](../VPNGATE_COMPATIBILITY.md)。仅原始日志/归属快照保留于Git忽略目录或VPS root私有目录，证书/私钥正文不发布。

## 实际官方样本检查

本次官方HTTPS响应含100条配置，全部使用AES-128-CBC/HMAC-SHA1；82个TCP、18个UDP。三个保存的样本均由本轮代码实际分类：

| 结果字段 | 三个样本的实际值 | 意义 |
| --- | --- | --- |
| strict_compatible | false | 不悄悄扩大默认AEAD政策 |
| legacy_candidate / legacy_data_channel | true / true | 属于明确选择后才能考虑的旧数据通道候选 |
| shared_client_identity | true | DER指纹精确匹配、对应私钥匹配；不是只检查CN |
| expected_peer_name_required | true | 不自动接受刚观察到的服务器名称 |
| server_identity_verified / network_tested | false / false | 不能据此宣称公共节点已可用 |

服务器根为强签名的ISRG Root X1，旧公开共享客户端证书则自签且不属于该服务器根链；二者按不同职责处理。样本哈希采用CRLF→LF规范化文本，官方响应整体哈希针对原始字节。最初只读检查器未统一换行，在执行前已另行构建修正版；旧文件没有覆盖，正式结果使用记录中的修正版哈希。

## 真实OpenVPN实验

两个独立用例串行运行，每个都有私有客户端/服务端namespace、真实官方OpenVPN程序和生成的强mTLS身份。没有宿主veth、公网端口、公共VPN连接或路由后备。

| 实际用例 | 耗时 | 丢弃计数（断线前→后） | 结果 |
| --- | --- | --- | --- |
| TestLinuxOpenVPNControlledTunnelAndFailClosed | 4.38s | 2→9 | PASS |
| TestLinuxOpenVPNLegacyControlledTunnelAndFailClosed | 4.30s | 2→8 | PASS |

第二项不仅设置了CBC参数，还断言OpenVPN日志实际出现`Data Channel: cipher 'AES-128-CBC'`。两项都继续验证：错误服务器名必须被TLS拒绝；真实隧道DNS UDP/TCP和业务TCP成功；私网/混合DNS拒绝；DNS超时不回退；终止隧道后旧/新连接与强行设置底层默认路由的绕行均受阻。

每项服务计数在断线前后均保持`DNSUDP=6, DNSTCP=1, Connections=5, Messages=5, BadSource=0`，说明失效后没有业务绕行请求。实验客户端/服务器证书都由测试自建，仍是强签名；这不替代旧公共客户端证书的原生OpenSSL或公共服务器验证。

## 资源与宿主

实际读回单核配额、384MiB内存、scope零swap、64tasks；两项合计8.691秒，cgroup峰值29,483,008 bytes（28.12MiB），所有内存/OOM/任务拒绝增量为0，scope无剩余子进程。

宿主前后哈希一致：`140db29dd81dcf59d99ccc47276f1a3b337daf3842605f93740098469489b8f9`。接口、IPv4/IPv6路由、规则、DNS、监听进程身份与挂载策略保持一致；已知测试根为空或不存在。仅新增的私有上传测试程序/日志留存，不修改系统服务、全局swap或`/run noexec`。

## 回归和证据链

| 证据 | 已验证结论 | 路径 / 边界 |
| --- | --- | --- |
| 官方响应统计+三份真实样本分类 | 严格模式确实不接受当前旧格式，显式候选例外能正确核对已观察到的共享身份 | 官方HTTPS→缓存→静态解析，不是公共节点连接 |
| 两项原生OpenVPN旅程PASS和计数对照 | AEAD与明确CBC模式在受控网络都保留TLS/DNS/断线保护 | 自建私有mTLS→隧道→故障注入 |
| Linux CI、API/浏览器检查 | 默认策略不变、恶意指令不放宽、API不输出材料、UI不冒充连通 | 单元/真实HTTP/合成缓存行的真实界面旅程 |
| 实际cgroup和宿主快照 | 此次小型受控实验在预算内完成，宿主网络不变 | 不是并发吞吐、长期运行或完整安全审计 |

本机Go测试/vet、Linux目标vet、48项HTTP/CLI smoke、9项前端测试、12项Python测试及Chrome旅程通过。本轮legacy fuzz配置5秒但仅执行4次，属于最小烟雾检查，**不能称作广泛fuzz覆盖或安全证明**。

下一步继续生产OpenVPN会话、固定最小权限worker、连接交接及可信服务器身份来源；之后补已有核心复用、域名/TLS/REALITY、质量信息和交互安装。dev.7仍开发中；dev.6预览与dev.2旧发行保持不可变。
