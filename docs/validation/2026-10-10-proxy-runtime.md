# dev.6 正式 CLI 代理路径与发行验收

> 2026-10-10（Asia/Shanghai）。正式CLI的代理上游路径已在用户授权VPS实际执行，dev.6预览压缩包已发布。**仍未完成生产OpenVPN、公共VPN Gate、Xray/sing-box、域名/TLS/REALITY和安装向导；本记录不能替代这些目标。**

## 可复核版本

- 运行源码：`2949ed7dba1216da8114c00ad1da8fcc6d291762`；运行时来自`998f2f2`，2949ed7增加验收脚本而未改生产运行逻辑。
- Linux amd64二进制SHA-256：`74e45891ea36dbb411eb5cfc3333bd8d1604b50976ce3c499e122a21ec0d57c8`。
- [dev.6 Release](https://github.com/vioIetMiku/miku-gate/releases/tag/v0.1.0-dev.6)：Linux amd64/arm64 tar.gz、Windows amd64 zip、SHA256SUMS.txt。新标签指向上述源码，旧dev.2未替换。
- [机器可读证据](evidence/2026-10-10-proxy-runtime.json)：完整断言摘要、实际配额、宿主快照哈希、资产哈希；文件SHA-256 `8d6ca962981191e3164cd9ca711b48464aff98d959df0864a04d36e187421b6c`。
- 执行方法和网络范围见 [VPS_PROXY_LAB.md](../VPS_PROXY_LAB.md)，测试源文件为`scripts/vps-proxy-live.py`，不是注入假的拨号器的API单元测试。

## 真实执行路径

授权测试机仍为Ubuntu24.04.5、Linux7.0.0-1013-oracle、约952MiB RAM。root协调器只负责哈希/路径检查、资源scope、普通用户启动及宿主快照；正式CLI使用UID65534运行，没有给它root权限或改为绕过TLS校验。

临时上游仅监听127.0.0.1、使用随机认证，只放行三个固定数字地址的443端口。CLI和测试客户端分别完成：

| 观察证据 | 验证结论 | 数据路径 |
| --- | --- | --- |
| 启动返回真实监听、healthy/DNS验证；工厂未替换 | 正式驱动可以处理用户保存的代理配置 | CLI→加密数据库→默认工厂→SOCKS上游 |
| Cloudflare真实DoH与系统CA验证通过 | 业务域名不依赖宿主DNS后备 | Miku→受限上游→数字DoH端点→TLS |
| HTTP CONNECT访问1.1.1.1的trace取得公网IPv4；SOCKS域名入口访问dns.google取得DNS答案 | 两种认证入口确有实际业务传输 | 认证入口→上游→真实业务TLS |
| 运行中改上游、重置凭据、删除均409 | 运行配置互锁生效 | API→运行时预约/状态 |
| 关闭上游后旧连接断开、新连接502、探测降级；停止后端口拒绝连接 | 失效不切换宿主直连 | 默认驱动→已终止上游，失败返回 |
| 改为HTTP CONNECT上游后同一逻辑出口与入口凭据继续工作 | 上游类型切换不重建逻辑入口 | 同一出口→HTTP上游 |
| 杀死控制进程后端口关闭；重启active_count=0、key哈希不变；再显式启动可传输 | 进程崩溃不会留下假活动状态，密钥/配置可恢复 | OS关闭句柄→恢复存储→显式启动 |

本机中继使用的仍是VPS公网出口，不是第二台远端出口或住宅IP。trace只证明返回了实际公网IPv4，其值没有写入发布记录，只有SHA-256。临时上游没有监听公网，也没有读取既有3x-ui的用户凭据或改其配置。

## 资源与宿主影响

- 实际读回：`cpu.max=100000 100000`、`memory.max=402653184`、`memory.swap.max=0`、`pids.max=64`。
- 验收流程5.645秒；包含前后检查的协调器墙钟6.105秒。
- scope内存峰值171401216 bytes（163.46MiB），内存压力/OOM/任务拒绝增量全为0；这包含测试、服务进程与缓存记账，不是常驻网关或并发容量承诺。
- 前后宿主快照SHA-256相同：`e860e3fd65e71ddb2d83956bf99bfc4f3c87020cac22ab44bde75d9cb7d80c5b`。覆盖接口、路由、nftables、DNS、原有11个监听进程身份及挂载选项。
- 没有新namespace；scope结束前没有剩余子进程。临时可执行副本和测试数据仅按本次已知路径/归属回收，可执行根保持为空。旧版本和本次root私有上传产物/日志保留。
- 未安装新依赖、未重启已有服务、未改宿主swap或`/run noexec`。

## 发行包校验

三平台均在干净源码上交叉构建，Go构建信息必须包含正确vcs.revision、vcs.modified=false、目标架构及CGO=0。归档写入后逐项重新读取，校验文件内容和权限；Windows zip实际解压后48项CLI/HTTP smoke通过。Linux amd64包内二进制与VPS测试完全相同；Linux arm64没有真机执行证据，只能称作已交叉构建和归档校验。

| 资产 | 字节数 | SHA-256 |
| --- | --- | --- |
| linux-amd64.tar.gz | 5757656 | `7d876f813c10ced538dafd76a3e0e74f5f937075e3e73766358fa3b7c78e7d41` |
| linux-arm64.tar.gz | 5348188 | `c2043ca8c21f07fdf3a666647b9fd302d073b76077257ecb372589f8693ab6ef` |
| windows-amd64.zip | 5758340 | `1babff757deb2309e8817d62088ec1fe0d48d6f66e6c5a460ad32dcb7ba4304f` |

准确全名带`miku-gate-v0.1.0-dev.6-`前缀。GitHub上传后再次核验每个资产的大小/服务器端SHA-256和不可变标签。源码CI检查成功后才创建此发布。

## 未外推的结论

HTTPS代理上游通过的是私有TLS集成回归，本次VPS验证的是SOCKS与HTTP上游到真实TLS目标。没有把HTTPS上游、更多发行版、多用户压力、长稳恢复、公共VPN或住宅信誉列为已验收。完整剩余需求继续按 [交付矩阵](../DELIVERY_PLAN.md) 追踪；升级/回滚需连同schema v3数据库及.key备份，不使用旧程序直接打开新数据库。
