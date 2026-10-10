# 当前 API：控制面与代理上游运行时

当前开发版本：`0.1.0-dev.8`，接口保持与 dev.2 兼容。默认基址为 `http://127.0.0.1:3939/api`。已配置的HTTP/SOCKS/HTTPS代理上游可实际启动；OpenVPN驱动通过显式独立执行器接入，完整实测进行中。运行中的配置/凭据/删除有互锁，不能假定全部旧的写操作仍无条件成功。

## 认证与请求规则

1. 管理员在本机 CLI 初始化，没有远程注册或公开脚本分发凭据接口。
2. `POST /auth/login` 提交 JSON 用户名和密码，成功后获得 HttpOnly、SameSite=Strict 的 `miku_session` Cookie。
3. 登录响应和 `GET /auth/session` 返回当前会话的 `csrf`。所有登录后的写请求必须同时携带 Cookie 与 `X-CSRF-Token`。
4. JSON 请求限制为 8 KiB，拒绝未知字段、多 JSON 值及错误输入。
5. 请求 Host 必须是 localhost 或回环 IP；存在 Origin 时须同源。当前版本不信任转发头，不面向公网反代部署。
6. 响应不缓存；不允许嵌入 iframe。

登录响应字段：`username`、`csrf`、`expires_at`。原始会话 token 只通过 Cookie 返回，数据库保存它的摘要。会话最多保留 32 条，过期会话无效。

## 路由

| 方法 | 路径 | 权限 | 成功响应 |
| --- | --- | --- | --- |
| GET | `/health` | 本机访问，无会话要求 | 200，版本与 `proxy-upstream-runtime`（或显式control-plane-only）状态 |
| POST | `/auth/login` | 本机访问，登录限速 | 200，会话信息及 Cookie |
| GET | `/auth/session` | 已登录 | 200，会话信息 |
| POST | `/auth/logout` | 已登录 + CSRF | 204，撤销会话和 Cookie |
| GET | `/overview` | 已登录 | 200，配置统计、实际监听数与最近健康数 |
| GET | `/exits` | 已登录 | 200，出口配置数组，不含密码 |
| POST | `/exits` | 已登录 + CSRF | 201，`exit`、一次性 `password` 与说明 |
| PATCH | `/exits/{id}` | 已登录 + CSRF | 204，更新名称、国家与策略 |
| DELETE | `/exits/{id}` | 已登录 + CSRF | 204，删除出口配置，保留事件 |
| GET | `/executor` | 已登录 | 200，是否显式配置、是否可达及 sandbox-only 能力 |
| GET | `/exits/{id}/sandbox` | 已登录 + 显式配置 IPC | 200，真实隔离快照；不代表 VPN 在线 |
| POST | `/exits/{id}/sandbox/prepare` | 已登录 + CSRF + confirm:true + IPC | 200，限定为创建全阻断沙箱 |
| POST | `/exits/{id}/sandbox/cleanup` | 已登录 + CSRF + confirm:true + IPC | 200，仅回收可证明归属且未占用的沙箱 |
| GET | `/exits/{id}/network-plan` | 已登录 | 200，只读隔离计划；不创建沙箱或代理 |
| POST | `/exits/{id}/start` | 已登录 + CSRF | 200，实际运行快照；验证失败不开放监听 |
| POST | `/exits/{id}/stop` | 已登录 + CSRF | 204，实际关闭入口和在途连接后保存stopped |
| POST | `/exits/{id}/credentials` | 已登录 + CSRF | 200，一次性新 `password` |
| GET | `/exits/{id}/upstream` | 已登录 | 200，配置视图、has_password，不包含密码 |
| PUT | `/exits/{id}/upstream` | 已登录 + CSRF + 出口停止 | 204，加密保存上游，不隐式连接 |
| DELETE | `/exits/{id}/upstream` | 已登录 + CSRF + 出口停止 | 204，仅移除上游，保留出口和入口凭据 |
| GET | `/exits/{id}/runtime` | 已登录 | 200，实际监听/健康/最近探测，不用配置冒充存活 |
| POST | `/exits/{id}/probe` | 已登录 + CSRF + 运行中 | 200，经当前上游的DNS/TLS复测；失败返回明确错误 |
| GET | `/vpn-executor` | 已登录 | 200，独立VPN执行器配置/可达状态；不代表隧道在线 |
| GET | `/exits/{id}/vpn` | 已登录 | 200，加密快照元数据，不返回配置/密钥正文 |
| PUT | `/exits/{id}/vpn` | 已登录 + CSRF + confirm:true + 停止 | 204，保存官方缓存快照和明确名称，替换原上游 |
| DELETE | `/exits/{id}/vpn` | 已登录 + CSRF + 停止 | 204，移除绑定，保留入口凭据 |
| GET | `/nodes` | 已登录 | 200，缓存候选节点，不含 OpenVPN 配置正文 |
| POST | `/nodes/{id}/identity` | 已登录 + CSRF | 200，系统CA与官方域名TLS验证后的公开身份；限频，不建立VPN |
| GET | `/nodes/{id}/profile` | 已登录，官方来源缓存 | 200，仅静态格式/材料检查，不返回配置正文、不联网 |
| POST | `/nodes/refresh` | 已登录 + CSRF | 200，`count`、`skipped`、`tested:false` |
| GET | `/events` | 已登录 | 200，最近 100 条事件 |
| GET | `/diagnostics` | 已登录 | 200，只读平台/工具检查及执行器不可用状态 |

## 创建与编辑出口

创建请求示例：

```json
{
  "name": "日本开发环境",
  "country": "JP",
  "policy": "any",
  "port": 0
}
```

- `name`：去除两端空白后 1–48 字符，不允许内部控制字符。
- `country`：两位字母代码，标准化为大写。
- `policy`：`any`、`residential_preferred`、`residential_strict`。地区/类型不是实测；缺少住宅证据时residential_strict拒绝启动。
- `port`：0 表示在 20000–29999 范围内选择未被本数据库占用的端口；显式值允许 1024–65535。
- 创建最多允许 100 条出口配置；数据库端口唯一，不代表操作系统已经检查该端口的实际可用性。
- 监听地址固定为 `127.0.0.1`，状态初始为 `stopped`。
- 创建时的一次性密码需要由调用方妥善保存。后续读取不可恢复原密码，只能重置。
- PATCH 提交名称、国家和策略，不允许提交非零端口；更新不改变原端口和凭据。

出口字段包括：`id`、`name`、`country`、`policy`、`port`、`listen`、`username`、`status`、`reason`、`created_at`。配置只持久化stopped/blocked；读取时可叠加真实运行时的starting/running/degraded/stopping。`upstream_kind`可表示已配置的代理类型。

## 预览出口的隔离计划

GET /exits/{id}/network-plan 返回稳定的 instance_id、exit_id、namespace、nft_table、owner、nft_ruleset、changes 和 warnings。stage 为 quarantine-only；host_routes_changed、host_firewall_changed、tunnel_enabled、proxy_enabled 均为 false。

- 必须登录；不存在的出口返回 404。与所有读取接口一样，Host/Origin 检查仍适用。
- 该接口不执行 OS 命令、不创建文件、不写审计、不改变出口状态；响应不是运行时快照。
- CLI 的 sandbox plan 使用同样两个 ID 时得到相同资源名和规则。
- dev.4 提供默认关闭的独立 sandbox/prepare 与 sandbox/cleanup HTTP 路由，需显式启用 IPC、会话、CSRF 和 {"confirm":true}。纯 network-plan 仍不执行命令。Web 不接收任意命令、root 路径或脚本。
- 删除配置不自动清理沙箱。启用 IPC 时先读取 absent 才可删除；不可达、未清理等状态返回错误。未启用时仍需手工回收。详见 [IPC 手册](EXECUTOR_IPC.md)。

诊断新增 sandbox_plan_available 和 sandbox_helper_supported；后者只说明当前平台是否为 Linux，不说明 root 能力、依赖可信性、沙箱存在或隔离测试通过。实际沙箱状态由独立执行器核验，可通过直接 inspect 或显式启用的 IPC 查询。GET /executor 的 reachable 不等于隔离已验证；vpn_available 和 proxy_available 保持 false。

## 节点与统计的含义

- `advertised_ping` 和 `advertised_speed` 是上游列表的数据，不是本机测速。
- `type` 固定为 `unknown`，`status` 固定为 `untested`。
- 获取配置会验证已知安全格式，但并未完成服务器证书链验证、运行时安全验证或建连测试。
- 每次显式同步最多处理 5000 条记录、保留 2000 条候选，总响应限制 8 MiB。
- 一个进程内同时只运行一个同步，至少间隔一分钟，HTTP 超时 25 秒；官方 HTTPS 端点固定，禁止重定向。
- 同步失败或结果为空时保留原缓存，不把过期缓存说成当前可用。
- 概览的active_count统计真实代理监听，healthy_count是其中最近探测健康的数量；executor_available/protection_verified仍为false，因为生产VPN执行器未接线。代理上游能力由proxy_runtime_available单独标明。

## 错误格式

```json
{
  "code": "executor_unavailable",
  "message": "尚未配置代理上游；生产OpenVPN驱动仍未接通"
}
```

| HTTP 状态 | 常见含义 |
| --- | --- |
| 400 | 字段、端口、国家、JSON 或请求格式错误 |
| 401 | 登录失败、未登录、会话撤销或过期 |
| 403 | CSRF、Host、Origin 或跨站请求校验失败 |
| 404 | 接口或出口配置不存在 |
| 409 | 端口冲突、同步已运行、执行器不可用等 |
| 415 | Content-Type 不受支持 |
| 429 | 登录限速、写操作繁忙、刷新间隔未到 |
| 500 | 内部持久化或处理失败，不返回数据库细节 |
| 502 | 官方节点源访问/解析失败，保留缓存 |
| 503 | 未构建前端资源 |

此 API 尚处于开发阶段；未来启用数据面时需要增加作业模型、能力协商和运行时状态，不能仅把 start 的 409 改成 200。

## 隔离 IPC 错误与并发

IPC 未启用返回 503 sandbox_ipc_disabled；无法验证/连接返回 503 sandbox_ipc_unavailable；缺少确认返回400。忙碌、归属冲突、未清理和资源预算通常返回409，无法核验/操作失败返回502，执行器报告取消/超时返回504。错误不包含原始子进程输出。

所有变更无自动重试；连接中断后先 GET sandbox 查询实际状态。控制台内的 sandbox prepare/cleanup/delete 共用互锁；不会协调同 UID 另行运行的独立 CLI。正常 start 仍返回409 executor_unavailable。

## 代理上游与运行快照

PUT上游字段：`kind`（socks5/http/https）、`host`、`port`、可选`username`/`password`、`reuse_password`、HTTPS可选`tls_name`、`dns`（cloudflare/google）、`allow_loopback`、`allow_plaintext`。只允许已校验公网地址，或显式选择的127.0.0.1非特权端口；远程明文协议需明确确认风险。

`password`只写不读。留空并设`reuse_password=true`时，只能对相同协议/地址/端口/TLS身份/用户名复用旧凭据。清空用户名和密码且不复用表示取消上游认证。更换目标而复用旧密码返回409 credential_target_changed。

运行快照包括`phase`、`listen`、`proxy_listening`、`healthy`、`proof`、`started_at`、`checked_at`、`check_ms`与`reason`。proof的`dns_verified`是经指定上游的实际探测；代理模式`protection_mode=proxy_only`、`kernel_verified=false`，不表示VPN/住宅/公网出口IP已验证。active_count统计实际监听入口，healthy_count统计最近探测成功的运行时。

常见新错误：409 runtime_busy（运行中修改或操作冲突）、409 runtime_capacity、409 port_unavailable、409 policy_unverified、409 executor_unavailable（未配置驱动/生产OpenVPN未接线）、502 upstream_unavailable、503 credential_store_unavailable、504 runtime_timeout。

启动是有界的同步操作；在已提交为运行状态的边界附近断开HTTP连接，调用方不能仅凭未收到响应推断未启动，应查询runtime。重复启动运行中的出口不重复建立会话。停止会等待已接受连接退出；超时后同样先查状态。重启不会从数据库恢复虚假的running值，也不自动启动已保存的出口。

代理模式与独立的quarantine-only沙箱不是同一条数据路径。若该出口尚有独立沙箱，需先清理再启动代理上游。运行中也不能创建/清理沙箱，避免误把隔离状态当作代理的内核防泄漏证明。

节点profile响应说明见 [VPNGATE_COMPATIBILITY.md](VPNGATE_COMPATIBILITY.md)。strict/legacy-candidate不表示节点在线，network_tested与server_identity_verified固定false；点击查看不授权连接或兼容模式。

VPN选择请求字段：`node_id`、`peer_name`、`allow_legacy`、`confirm`。预期服务器名称由用户从可信来源确认，不能自动信任连接错误给出的名字。选中节点须来自固定官方HTTPS缓存、时间不超过24小时，并通过所选规范化政策。保存快照不会自动启动，缓存刷新不自动改已有快照。

`--vpn-executor`未启用时，保存的VPN节点启动返回409 `vpn_executor_disabled`。实际根服务不可达/会话失败返回502，绝不转成代理直连。普通代理PUT若替换VPN必须加`replace_vpn:true`。schema v4包含新的加密记录，缺少配套.key时拒绝恢复。

概览新增`vpn_runtime_configured`与`vpn_verified_count`；只有确有活动会话且全都通过当前namespace/TUN/规则/TLS/DNS核验时`protection_verified`才为true。混合代理模式不能被标成全局内核保护。实际UI和部署链路验收仍按独立记录追踪。

自动身份保存使用use_verified_identity:true，浏览器不提供peer_fingerprint或证书私钥。先POST身份检查，再在5分钟内保存对应快照；过期或节点/配置变化时返回409 identity_check_required。schema v5保存精确指纹，见[服务器身份流程](VPN_SERVER_IDENTITY.md)。
