# 启用受限的本机隔离 IPC

> dev.4 的实验性功能。接通 IPC **不代表接通 VPN**：只能创建、检查、清理不联网的全阻断沙箱，不启动 OpenVPN、TUN、业务 DNS 或代理监听。真实跨UID与HTTP→内核实验已通过，结果及限制见 [IPC验收记录](validation/2026-10-09-executor-ipc.md)。
>
> 默认不启用。Web 服务仍必须由普通用户运行；不要把整个控制台升为 root。已发布的 dev.2 压缩包不包含此功能，须使用 dev.4 对应源码构建。

## 先决条件

- Linux；使用已有的独立普通用户，UID 和主组均非 0。不要把共享 nobody 账号作为真实控制服务账号。
- root 执行器需要 CAP_SYS_ADMIN、CAP_NET_ADMIN，以及可信系统目录中的 ip、nft、nsenter。
- 同一份源码构建的控制台与执行器，同一份已初始化数据库的 instance ID。
- 默认管理监听仍是回环地址。此步骤不创建用户、不安装软件、不注册 systemd 服务，也不开放公网端口。

## 初始化并取得固定实例身份

在普通用户拥有的项目或解压目录运行。已有管理员时不要重新初始化，也不要覆盖数据库：

```bash
./bin/miku-gate init --data .data
./bin/miku-gate instance --data .data
```

instance 只输出非秘密的稳定实例 ID；数据库不存在时会报错，不偷偷创建新实例。数据库版本迁移仍遵循原有规则，升级前应备份数据目录。

## 管理员显式授权一个 UID

在上述普通用户的终端读取它自己的 UID，随后仅以 sudo 启动独立执行器。进程保持前台，另开终端运行控制台：

```bash
MIKU_INSTANCE="$(./bin/miku-gate instance --data .data)"
MIKU_UID="$(id -u)"
sudo ./bin/miku-gate executor serve \
  --instance="$MIKU_INSTANCE" --allow-uid="$MIKU_UID" --apply
```

执行器只接受这个 UID。主组权限只是限制 socket 可达性，最终授权依据内核 SO_PEERCRED 的 UID；**同组的其他用户甚至 root 客户端也不能代替指定 UID 调用**。root 自身仍有系统级权限，本设计不防御恶意 root。

授权范围是这个实例的隔离操作，而不是某个浏览器会话。该 UID 下的任何本机程序都可在协议限制内调用助手；同 UID 的恶意进程不在隔离边界之外。控制台 HTTP 入口另外执行管理员会话、CSRF、Host/Origin 和显式确认校验。

## 普通用户连接执行器

第二个终端使用同一用户、同一数据目录：

```bash
./bin/miku-gate executor info --instance="$(./bin/miku-gate instance --data .data)"
GOMAXPROCS=1 GOMEMLIMIT=256MiB ./bin/miku-gate serve --data .data --sandbox-ipc
```

GOMEMLIMIT 是 GC 软目标，不是内存硬上限。1GB VPS 上仍需验证整体内存，不能用这个值宣称进程被硬限制。

界面流程：出口详情 → 预览隔离计划 → 检查执行器 → 查看实际隔离状态 → 创建/清理隔离沙箱 → 二次确认。查看计划不会执行命令；检查实际状态会调用已授权的 root 助手读取 namespace/规则。

- IPC 未启用、执行器断开或状态未知时，不显示可直接执行的变更按钮。
- 只有实际状态 absent 才提供创建动作。
- 清理不会杀进程、移动网卡或删除陌生对象；占用/归属冲突时返回错误。
- 返回的 quarantined 仅指隔离快照，界面仍显示 VPN / 代理未启用。
- 点击原有“启动出口”仍返回 409 executor_unavailable。

同一普通用户也可使用 sandbox inspect/prepare/cleanup 的 --ipc 参数；变更仍需 --apply。plan 是纯计算，不接受 --ipc。执行器运行期间持有该实例网络锁，直接的 root sandbox 操作会被拒绝；要么通过 --ipc 调用，要么先停止执行器后使用直接模式。

## 协议与权限边界

| 项目 | 实现 |
| --- | --- |
| 传输 | 固定派生路径的 Unix stream socket；不监听 TCP |
| 路径 | /run/miku-gate-ipc/实例哈希/control.sock；不接受自定义路径 |
| 父目录与 socket | root 所有；实例目录 0750、socket 0660，组为授权用户的主组 |
| 双向身份 | 服务端核对允许的 UID；客户端核对服务端 UID=0、root 所有路径与 socket inode |
| 请求范围 | capabilities、inspect、prepare、cleanup；实例在 daemon 启动时固定 |
| 禁止输入 | shell、命令名、文件路径、环境变量、OpenVPN 配置、另一实例 ID |
| 帧 | 4字节大端长度；单连接一次请求/响应；随机请求 ID 回显 |
| JSON | 限定字段、类型/操作组合、ASCII ID；拒绝重复键、超深层级与未知字段 |
| 大小 | 请求最多 4KiB，响应最多 32KiB |
| 并发 | 最多4条授权连接；最多1个在途内核操作，忙时拒绝而不无限排队 |
| 期限 | 帧读写3秒、内核操作20秒、客户端整次调用32秒；回滚使用原有独立10秒预算 |
| 错误 | 固定码和固定中文消息，不把原始系统命令输出发给 Web 客户端 |

客户端发送数据前先验证对端；仅把一个 socket 文件 chown 成 root 不够，非 root 建立的监听端点仍会被拒绝。任何未知能力（包括声称 VPN 可用）或不匹配的实例/请求 ID/计划都拒绝。

## 取消、重启与删除

- 连接断开或服务停止会取消在途操作；prepare 的已知资源仍按原有回滚规则处理。
- 超时或断开后不能假定操作未发生。**先重新检查实际状态，不自动重发变更。**
- 停止 daemon 只关闭其 IPC 端点，保留内核全阻断沙箱；下次启动重新核验后可检查/清理。
- socket 有独立 root 所有的锁和归属记录。只有记录与 inode 相符、持锁且确认没有监听进程的旧 socket 才能自动恢复；未知文件、活跃监听或替换对象均保留并拒绝接管。
- 崩溃发生在 socket 创建而归属记录未保存的窗口时，可能需要管理员人工核查。不能通配符删除运行目录。
- Web 启用 IPC 时，删除配置前必须成功读取 absent；有沙箱或执行器不可达均拒绝。控制台内的准备/清理/删除串行互锁。
- 上述互锁不协调同 UID 自行运行的另一 CLI/控制台进程；请勿并发手工操作相同出口。没有启用 IPC 时，仍需先用独立 CLI 清理，再删除配置。

## 测试范围

普通单元测试涵盖协议、假响应拒绝、取消/忙碌边界、API 授权与默认关闭策略，不会隐式创建 Linux namespace。

手动 Linux isolation lab 工作流另外执行：

1. 真实 UID 65534 客户端与独立 root daemon，创建/检查/清理实际 namespace。
2. 同组不同 UID、root 客户端和畸形请求拒绝，连接数上限及释放恢复。
3. socket 崩溃残留恢复和无归属 socket 拒绝。
4. 非 root 伪服务器即使文件被 chown 成 root，也在发送请求前被客户端拒绝。
5. **真实普通用户 HTTP 控制面 → 本机 IPC → root → 内核**完整流程，含 CSRF、确认、删除互锁及 VPN 启动仍被阻止。

CI 的 65534 只是临时测试 UID，不创建真实账号或登录令牌；环境是临时 Ubuntu runner，不是用户 VPS。真实测试的结果和提交 SHA 必须单独记录，不能用本机交叉编译替代。

目标VPS后续也已实际完成跨UID及HTTP链路复核，来源456e863；"/run" noexec问题通过分离测试可执行目录解决，未更改挂载策略。见 [VPS验收记录](validation/2026-10-09-vps-lab.md) 和 [操作配方](VPS_LAB.md)。这不改变本协议sandbox-only的范围。
