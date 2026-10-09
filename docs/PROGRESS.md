# 开发进度与验证记录

更新：2026-10-10。当前代码版本：0.1.0-dev.7（开发中）；已发布快照：v0.1.0-dev.6（dev.2保留）。**当前支持本机认证代理上游；完整OpenVPN/Xray/sing-box/域名安装目标仍未完成。**

## dev.7：真实VPN Gate格式兼容与节点检查

官方当前100条配置均为AES-128-CBC/HMAC-SHA1；3个样本使用强ISRG服务器CA和精确固定的公开共享客户端身份，旧严格解析器无法支持。已新增显式兼容选项、预期服务器名称必需、共享身份指纹例外和只读节点配置检查；不放宽默认AEAD、TLS服务器验证或脚本/压缩限制。详见 [兼容政策](VPNGATE_COMPATIBILITY.md)。新增CBC/TLS/DNS/断线真实实验尚待本轮执行结果，公共VPN连接和生产worker仍未完成，dev.6资产不替换。

## 完整目标与真实代理运行时开发

完整终态见 [DELIVERY_PLAN.md](DELIVERY_PLAN.md)，不以私有实验或此阶段组件替代整个项目。dev.6已加入加密的代理上游配置、SOCKS5/HTTP/HTTPS驱动、经同一上游的DoH与数字IP拨号，以及监听停止/凭据边界回归。真实私有TCP/TLS测试通过；运行时、API与界面已接线。本机完整Go回归、48项真实HTTP/CLI smoke、9项前端单元及Chrome表单/失败/移动旅程通过，协议/运行时各有独立正反对照。正式CLI默认工厂现已在目标VPS通过真实TLS/DoH/代理传输、上游断开、停止和崩溃重启测试；dev.6三平台压缩包已发布，见[验收记录](validation/2026-10-10-proxy-runtime.md)。生产OpenVPN start仍被阻止。详见 [UPSTREAM_PROXY.md](UPSTREAM_PROXY.md)。

## 目标 VPS 受控验收已完成

- 实际环境：Ubuntu 24.04.5、约952MiB内存、2个可见vCPU；三组实验均读回单核/384MiB/零scope swap/64 tasks限制，宿主原有swap配置未改。
- 精确测试来源：`456e863f112be8ce9cadb016ef93389e8ab4b136`。netexec 7项主测试+6个文件子用例、IPC 4项主测试、受控OpenVPN完整旅程全部PASS，无SKIP；[独立验收与失败记录](validation/2026-10-09-vps-lab.md)及[机器可读证据](validation/evidence/2026-10-09-vps-lab.json)已保存。
- 首次c09d49b IPC因`/run` noexec失败；修正将只读测试程序副本与运行时数据分开，不重挂载、不放宽安全策略、不改生产启动路径。旧产物和失败日志保留。
- 实测cgroup峰值：netexec 13.34MiB、IPC 158.33MiB、OpenVPN 26.34MiB。无OOM/配额拒绝；不是网关常驻内存、吞吐或长稳容量承诺。
- OpenVPN实际mTLS、DNS UDP/TCP及业务TCP通过；断线后已有/新连接与底层绕行阻断，丢弃计数2→7，受控服务计数不变。
- 最终宿主网络/DNS/挂载策略及原有11个监听进程身份一致；无实验进程、scope、namespace残留。仅新增OpenVPN及其缺失依赖，系统服务inactive/disabled；私有产物约72.22MiB和空测试根保留。
- 新增9项离线守护测试和Linux labexec测试；[同提交CI](https://github.com/vioIetMiku/miku-gate/actions/runs/37952685864)含Go race、前端6项、37项HTTP/CLI、浏览器旅程均通过。操作见 [VPS_LAB.md](VPS_LAB.md)。已发布dev.2资产未替换。

## 本轮受控 OpenVPN 开发

- 已加入运行配置规范化、TLS材料校验、sealed memfd子进程和独立worker的接口绑定DNS/TCP构件，未接入现有Web/IPC启动路径。
- 增加受控OpenVPN实验：错误证书名称负向对照、推送路由/DNS过滤、TCP/DNS正向通信、私网与混合DNS拒绝、超时与断线阻断。
- 本机单元测试和Linux交叉编译通过。真实OpenVPN整条旅程在提交04292ec实际通过，含mTLS名称负向对照、隧道DNS/TCP、失效后绕行阻断及宿主快照；[独立验收记录](validation/2026-10-09-controlled-openvpn.md)含失败修正和计数证据。
- 生产普通用户Web控制台仍不提供可用VPN代理。当时未访问VPS；后续目标VPS实测见上节，已发布dev.2资产未替换。成功源码的本机有界fuzz：配置700481次、DNS1070925次均无崩溃；常规远端CI含Go race与浏览器回归也通过。

## 先前 IPC 开发

- 已实现限定操作、固定实例、指定非 root UID 的 Unix socket IPC；默认关闭、双向 peer credential 与 root 路径校验。
- 已实现 executor serve/info、instance 查询、sandbox --ipc，以及显式接入的 Web 状态/确认操作/审计/删除互锁。
- 4项真实IPC实验已通过：跨UID权限、伪服务器拒绝、重启/socket归属、真实普通用户HTTP→IPC→root→内核；原有7项内核主测试和6个文件子用例也通过。
- 本机 Go 测试/vet、37项真实 HTTP/CLI smoke、前端6项单元测试、TypeScript/Vite构建、Chrome完整旅程已通过。IPC解码器5秒有界fuzz执行637,737次无崩溃。远端Go race、浏览器与内核实验全部通过；测试源码为 c0c0995，详见 [IPC验收记录](validation/2026-10-09-executor-ipc.md)。
- 这仍不是 OpenVPN 或代理出口；已发布 dev.2 资产未替换。

## 先前交付

| 模块 | 当前状态 | 实际范围 |
| --- | --- | --- |
| 上一版备份 | 已推送 | GitHub 私有仓库 [vioIetMiku/miku-gate](https://github.com/vioIetMiku/miku-gate)，v0.1.0-dev.1 保留指向 b8e68b3 |
| 发布与开发 | dev.2 已发布，main 持续开发 | [Release](https://github.com/vioIetMiku/miku-gate/releases/tag/v0.1.0-dev.2) 提供三个平台压缩包和校验文件；[Issue #1](https://github.com/vioIetMiku/miku-gate/issues/1) 跟踪后续，已发布资产不被替换 |
| 稳定实例身份 | 已实现并回归 | SQLite schema v2；从 v1 迁移保留出口 ID/端口，重启保持实例 ID，损坏或丢失的 v2 身份拒绝自动替换 |
| 网络计划 | 已实现并回归 | 认证后只读 API、CLI、出口详情预览共用计划；不执行命令，不创建文件，不改变出口状态 |
| Linux 隔离助手 | dev.2 真实 Linux 隔离实验通过 | 独立 root CLI；仅不联网的 namespace + 内部 inet 全阻断规则；临时 Ubuntu runner 验证，不是实际 VPS 或 VPN 验收 |
| 资源归属与回滚 | 已实现并单元故障注入 | 归属记录、锁、namespace 身份核验、显式执行、幂等、预算、未知资源拒绝接管、独立超时回滚 |
| nft 规则核验 | 已实现并单元/fuzz 测试 | 结构化检查表/链/规则，拒绝错误 owner、额外规则、dormant 标志、重复 JSON 键和畸形输出 |
| 浏览器回归 | 本机 Chrome 已实际通过 | 登录、创建、计划预览、规则展开、重置凭据、编辑、删除、阻止假启动、诊断、主题、移动布局、退出与会话撤销 |
| Linux 实验设施 | dev.2 三项内核实验已实际通过 | 两个与宿主断开的 namespace，IPv4/IPv6 正反对照、丢包计数、归属替换、失败回滚及宿主快照对比 |
| 特权 CI | dev.2 与 dev.3 实验均通过 | 临时 Ubuntu runner；dev.3 的7项主测试和6个文件子用例通过，实际读回单核 CPU/512MiB/零 swap/64 tasks 限制 |

原有账号与会话、出口配置与独立凭据、官方节点缓存、受限解析、审计日志和未接线的 HTTP/SOCKS5 TCP 协议核心继续保留。

## 已实际完成的验证

dev.2 发布快照与 dev.3 补充代码分别有独立 CI 记录。dev.3 测试提交为 c214584979e018023a66692432b966af3e8d240b；后续仅文档记录提交不替代该测试来源。

| 验证 | 结果与边界 |
| --- | --- |
| go test -count=1 ./... | Windows 本机通过；覆盖计划、状态机、存储迁移、认证、API、协议核心等。Linux 专有实机测试不在此结果中 |
| go vet ./... | Windows 目标通过；Linux amd64 目标也通过 |
| nft JSON 校验器 fuzz | 配置 5 秒，287,578 次执行，无崩溃；不是完整安全证明 |
| npm --prefix web test | 6 项前端单元测试通过 |
| TypeScript / Vite 构建 | 通过 |
| node scripts/smoke.mjs | 30 项真实 HTTP/CLI 检查通过；隔离数据库、真实服务，无 VPN 或代理监听 |
| Playwright + 本机 Chrome | 1 条完整旅程通过；不是 mock API。新增网络计划、凭据重置和删除检查，桌面/390px 布局与滚动弹窗关闭按钮均有断言 |
| Windows amd64 二进制 | 构建通过 |
| Linux amd64 / arm64 二进制 | 交叉编译通过，不等于 Linux 运行验证 |
| Linux 内核实验测试二进制 | dev.2 已在 Ubuntu runner 实际执行三项并通过；[日志与版本边界](validation/2026-10-09-linux-quarantine.md) |
| GitHub control-plane 工作流 | dev.2、dev.3 各自通过 Go race、前端、30 项 smoke 与 Chromium 浏览器旅程 |
| dev.3 低资源内核实验 | 7项主测试及6个异常文件子用例通过；核验实际 cgroup 配额、进程不误杀、重启/锁、FIFO/符号链接/权限等边界 |

首轮浏览器回归发现原有 getByLabel 精确定位无法命中包含下拉选项的标签；依据实际无障碍树改用 combobox 的可访问名称后通过。截图检查还发现长弹窗关闭按钮会滚出视口，已改为吸顶标题栏，并在桌面和移动宽度重新验证。

可复核截图位于本机 artifacts 目录，包括登录、隔离计划、计划移动布局、概览、深色概览与移动概览。图中的出口是隔离测试配置，不是真实 VPN 连接。测试账号、数据库、trace、构建产物和截图均保持 Git 忽略；不上传账号密码或 SSH 凭据。

## 仍未验证或未实现

- 目标VPS三组受控实验已完成；仍未测公共节点、生产代理入口、并发吞吐、长时间负载或自动重连，不能外推为完整网关容量。
- dev.4 已有隔离IPC，dev.5 已在受控实验建立过OpenVPN/TUN及隧道DNS/TCP；这些构件尚未接到生产运行时协调、现有IPC或真实代理入口。
- 尚无真实代理监听、实际出口 IP、隧道断线保护、恢复切换、住宅类型实测、热备、Xray 或多机 Agent。
- 尚未完成完整安全审计、依赖漏洞审计、发行许可证审查或生产安装/升级验证。
- 浏览器自动化不等于手机真机、读屏器或完整可访问性验收。

未配置代理上游或请求生产OpenVPN时start仍返回409；已配置代理可真实监听，active_count来自运行时。executor_available/protection_verified仍只针对未接通的VPN执行器，代理模式另用proxy_runtime_available与proxy_only说明。独立 CLI 的 quarantine_verified 也不能作为真实 VPN 防泄漏的证明。

## VPS 访问边界

已按用户授权登录并完成本轮受限实验。SSH连接严格匹配首次固定的ED25519指纹，后续不接受不同公钥；该方式只能提供连续性，不冒充独立带外核验。密码仅用于进程内认证，不写文件/日志/Git/Issue。

只在私有实验网络测试，不连接公共VPN，不改宿主网络、现有服务或全局swap。首次失败与修正后均检查归属及残留，最终确认11个原有监听进程身份和宿主网络不变、noexec保留；复现材料与构建文件留在root私有产物目录。

## 下一步

1. 实现生产最小权限分离与受限隧道生命周期；测试专用启动助手不能直接用作网关守护进程。
2. 实现worker连接交接及可核验状态协调；先覆盖失败/取消/崩溃关闭顺序，禁止宿主解析或默认路由后备。
3. 本机认证HTTP/SOCKS代理上游已接线；后续完成Xray/sing-box加密入口、既有工具复用、域名/TLS/REALITY与VPS安装向导。
4. 通过隧道断线、DNS、IPv6、崩溃恢复与资源冲突测试后，才接通现有代理核心并解除启动闸门。

使用说明见 [LINUX_SANDBOX.md](LINUX_SANDBOX.md)，完整网络契约见 [NETWORK_EXECUTOR.md](NETWORK_EXECUTOR.md)。升级前备份旧数据目录；schema v3不能由旧dev.5及以前版本直接降级打开，备份需包括配套.key密钥。
