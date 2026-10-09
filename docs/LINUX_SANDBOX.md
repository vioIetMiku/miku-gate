# 使用 Linux 隔离沙箱

> **实验性功能，不是 VPN 网关。** 本轮仅实现无外部连接的全阻断 namespace：没有 TUN、OpenVPN、业务 DNS 或代理监听。Linux 实机验收结果以 [PROGRESS.md](PROGRESS.md) 为准；交叉编译和模拟故障测试不能代替内核验证。
>
> Web 控制台只能预览计划，不能调用特权操作。不要把控制服务改成 root，也不要为了测试修改宿主默认路由或公开管理端口。

## 先看计划，不执行变更

进入“出口详情 → 预览隔离计划”，可查看实例 ID、出口 ID、namespace 名称、规则表和拟安装规则。这是纯计算结果，不是实时运行状态；刷新计划不会创建文件、记录审计事件或修改出口状态。

命令行也可预览。下面的固定 ID **只用于独立示例沙箱**，不是控制台中的实际出口：

```bash
./bin/miku-gate sandbox plan --instance=example_instance_01 --exit=example_exit_01
```

实际出口应使用详情页中持久化的两个 ID。名称由版本、实例 ID 和出口 ID 的 SHA-256 派生，不把用户输入直接拼成路径或 shell 命令。

## 在授权的 Linux 测试机执行

### 前置条件

- Linux；建议首先使用可恢复的 Debian/Ubuntu 测试 VM，不在承载重要服务的机器上直接验收实验代码。
- 普通用户构建可信源码，特权沙箱命令单独通过 sudo 运行。
- root 且具备 CAP_SYS_ADMIN、CAP_NET_ADMIN；容器内的 root 不一定有这些能力。
- 系统安装 iproute2 的 ip、nftables 的 nft、util-linux 的 nsenter。仅此沙箱不要求 OpenVPN 或 /dev/net/tun。
- 助手从固定系统目录寻找二进制，检查解析后的路径和目录归属，拒绝非 root 所有、可被组/其他用户写入的程序；不信任自定义 PATH。

助手不会安装包、调整服务或自动设置 sudo 权限。变更操作没有 --apply 会立即拒绝。

| 操作 | 是否变更 | 权限与平台 |
| --- | --- | --- |
| plan | 否；纯计划，不读特权状态 | 任意支持的控制台平台，普通用户 |
| inspect | 否；读取实际 namespace 和规则 | Linux，独立 root 助手，共享锁 |
| prepare --apply | 创建归属记录、独立 namespace 和该空间内的规则 | Linux，独立 root 助手，排他锁 |
| cleanup --apply | 仅回收经过归属核验的本出口资源 | Linux，独立 root 助手，排他锁 |

在项目根目录、完成构建后，可对上述示例 ID 执行：

```bash
sudo ./bin/miku-gate sandbox prepare --instance=example_instance_01 --exit=example_exit_01 --apply
sudo ./bin/miku-gate sandbox inspect --instance=example_instance_01 --exit=example_exit_01
sudo ./bin/miku-gate sandbox cleanup --instance=example_instance_01 --exit=example_exit_01 --apply
```

每个实例最多保留 32 个沙箱记录。这是实验资源预算，不代表代理吞吐或并发容量。控制台最多 100 条出口配置与该预算是两回事。

### 会修改和不会修改什么

**会修改：**

- 在系统 namespace 注册目录中创建本出口专属的 mg- 前缀对象。
- 在 /run/miku-gate/netexec 下创建 root 专属实例目录、锁和 JSON 归属记录。
- 只在新 namespace 内创建本实例的 inet 规则表，input / forward / output 三条链均 policy drop，并使用 counter drop 记录阻断。

**不会修改：**

- 宿主网卡、默认路由、全局 nftables/iptables、IP forwarding 或 DNS 文件。
- 现有 VPN、Docker 或 systemd 服务。
- 代理监听、OpenVPN 进程、TUN 或到宿主的 veth 链路。

初始 namespace 只有回环接口，没有底层出口，且不启动业务进程。规则事务失败时不会出现“先开放外网、再补阻断”的窗口。当前规则连回环流量也丢弃，不应把它作为可运行应用的完整网络环境。

## 理解返回状态

| 状态 | 含义 | 下一步 |
| --- | --- | --- |
| absent | namespace 不存在 | 可 prepare；若刚回滚，阅读命令错误原因 |
| quarantined | 已读取实际 namespace 身份、仅回环接口和预期全阻断规则 | 只代表本次隔离快照，不代表 VPN 已连通 |
| unowned | 同名 namespace 没有本实例归属记录 | 不接管、不删除，管理员先调查来源 |
| ownership-conflict | 记录与 namespace 的设备号/inode 不一致 | 不按名称猜测或强制清理 |
| incomplete | 创建中断，归属身份尚未可靠持久化 | 保留现场，管理员核验；不自动收编 |
| stale-record | 记录存在，但 namespace 已不存在 | 显式 cleanup 仅清理本出口记录 |
| drifted | 规则或接口与计划不同 | 不自动覆盖；检查变更原因 |
| cleanup-required | 回滚/清理失败或发现忙碌资源 | 保留归属记录，先处理具体原因 |
| unverified | 无法安全读取或验证 | 不报告保护已生效 |

quarantine_verified 只可能来自内核状态检查；tunnel_ready 和 proxy_listening 在本版本始终为 false。控制台的 protection_verified / executor_available 仍为 false，不能用沙箱状态绕过 VPN 启动闸门。

## 清理与中断的安全边界

- 归属记录先于资源创建写入，再原子保存 namespace 的设备号/inode；记录文件为 root 所有的单链接普通文件，目录 0700、文件 0600。
- 执行 nft / ip 查询时通过已打开、验证为 NSFS 的文件描述符进入 namespace；不会在 Go 多线程进程中直接切换网络空间。
- nft JSON 按结构匹配，不靠搜索 drop 文本。缺链、额外规则、错误协议族、错误 owner、dormant 标志、重复 JSON 键等都不能获得已验证状态。
- 清理前复核 inode、接口、规则归属和进程。存在其他网卡、外来规则对象或进程时拒绝，不杀进程、不迁移网卡、不 flush 宿主规则。
- 取消请求不跳过回滚；回滚使用独立的 10 秒预算。每个外部命令另有 5 秒期限和 256 KiB 输出上限。
- 若崩溃发生在 namespace 创建之后、inode 持久化之前，助手选择保留不确定资源，绝不因为名称像 Miku Gate 就删除。
- 这是对意外失败和资源冲突的保护，不声称能防御同时修改内核/运行目录的恶意 root。管理员持有的额外 namespace fd 也可能延长内核对象生命周期。

**先清理手动沙箱，再删除控制台出口配置。** 两者尚未通过特权 IPC 接通，删除数据库配置不会自动触发 root 清理。

## 运行真实内核实验

普通 go test 会跳过以下实验。必须显式启用；不要把 skipped 当作内核测试通过。

测试依赖还包括 iputils 的 ping。测试构建不需要前端，也不访问 VPN Gate 或互联网目标：

```bash
mkdir -p .cache
go test -c -o .cache/netexec-linux.test ./internal/netexec
sudo env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin MIKU_GATE_NETNS_TEST=1 \
  "$PWD/.cache/netexec-linux.test" -test.v -test.run '^TestLinuxLab' -test.timeout 3m
```

实验创建两个本实例 namespace，veth 两端都放在隔离空间内，**没有一端接入宿主**。测试使用基准测试 IPv4 地址段和文档 IPv6 地址段；先证明两端能通信，再安装目标阻断规则，验证双向 IPv4/IPv6 丢包及 nft 计数器。同时检查：

- prepare 幂等；添加其他接口时 cleanup 拒绝。
- 宿主接口、路由和规则快照未被测试修改（忽略动态计数等易变字段）。
- 同名 namespace 被替换后，inode 冲突阻止接管和清理。
- 注入规则安装失败后，仅回滚本次已证明归属的资源。

测试仅非递归清理自己确认的记录/目录；异常记录会保留并报错，不通配符扫删 namespace。实验根目录 /run/miku-gate-netexec-tests 可能保留为空目录。

另提供手动触发的 GitHub Actions 工作流 Linux isolation lab (manual)，只在临时 Ubuntu runner 中安装实验依赖。它不自动连接 VPS，也不在普通 PR 测试中隐式执行特权实验。dev.2 的首轮实际结果见 [验证记录](validation/2026-10-09-linux-quarantine.md)。

从 dev.3 开始补充活跃进程、重启/锁、异常文件对象检查；CI 使用 systemd 临时 scope 约束 CPU、内存和 tasks，并由测试读回 cgroup v2 文件验证有效限制。它不安装常驻服务，scope 在测试结束后收集。

在具备 systemd/cgroup v2 的独立 Linux 测试机，可显式执行同样的低资源实验（需 time 程序）。不支持这些限制时应报错，不把无配额运行当成低资源通过：

```bash
sudo systemd-run --quiet --scope --collect \
  --property=CPUQuota=100% --property=MemoryMax=512M \
  --property=MemorySwapMax=0 --property=TasksMax=64 \
  /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LANG=C LC_ALL=C \
    MIKU_GATE_NETNS_TEST=1 MIKU_GATE_LAB_BUDGET=1 \
    GOMAXPROCS=1 GOMEMLIMIT=256MiB \
    /usr/bin/time -v "$PWD/.cache/netexec-linux.test" \
      -test.v -test.parallel 1 -test.run '^TestLinuxLab' -test.timeout 3m
```

GOMEMLIMIT 是 GC 软目标，不是硬上限；硬上限由 cgroup 提供。输出包括实际配额和 memory.peak。该实验不运行控制台或 OpenVPN，所以其内存观测不代表完整网关的容量。

这些实验仍**不覆盖** OpenVPN 握手、隧道 DNS、断线业务流量、真实出口 IP 或代理协议与隧道的集成。完成隔离验收后才进入这些阶段。

## 数据库升级与远程验证

dev.2 将 SQLite user_version 升级到 2，增加持久化实例 ID，不更改已有出口 ID、端口、密码摘要或策略。升级前停止旧服务并备份完整数据目录；旧 dev.1 程序会拒绝打开 v2 数据库。回退应恢复对应旧备份，不手改版本号。不要在同一宿主克隆一份数据库后并行运行同身份实例。

远程连接必须验证 SSH 主机公钥指纹。未知指纹时先与 VPS 提供商控制台或 Netcatty 的已信任记录核对，不能关闭主机校验。核对只需要公钥指纹，不需要再次发送密码、私钥或 GitHub token。
