# 当前 API：控制面开发原型

版本：`0.1.0-dev.1`。默认基址为 `http://127.0.0.1:3939/api`。所有数据面启动操作目前明确失败，不提供真实代理地址或活动隧道。

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
| GET | `/health` | 本机访问，无会话要求 | 200，版本与 `control-plane-only` 状态 |
| POST | `/auth/login` | 本机访问，登录限速 | 200，会话信息及 Cookie |
| GET | `/auth/session` | 已登录 | 200，会话信息 |
| POST | `/auth/logout` | 已登录 + CSRF | 204，撤销会话和 Cookie |
| GET | `/overview` | 已登录 | 200，配置统计与未接入执行器的状态 |
| GET | `/exits` | 已登录 | 200，出口配置数组，不含密码 |
| POST | `/exits` | 已登录 + CSRF | 201，`exit`、一次性 `password` 与说明 |
| PATCH | `/exits/{id}` | 已登录 + CSRF | 204，更新名称、国家与策略 |
| DELETE | `/exits/{id}` | 已登录 + CSRF | 204，删除出口配置，保留事件 |
| POST | `/exits/{id}/start` | 已登录 + CSRF | 当前固定返回 409，保存 blocked 状态和原因 |
| POST | `/exits/{id}/stop` | 已登录 + CSRF | 204，保存 stopped 状态 |
| POST | `/exits/{id}/credentials` | 已登录 + CSRF | 200，一次性新 `password` |
| GET | `/nodes` | 已登录 | 200，缓存候选节点，不含 OpenVPN 配置正文 |
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
- `policy`：`any`、`residential_preferred`、`residential_strict`。当前仅保存，不执行网络分类策略。
- `port`：0 表示在 20000–29999 范围内选择未被本数据库占用的端口；显式值允许 1024–65535。
- 创建最多允许 100 条出口配置；数据库端口唯一，不代表操作系统已经检查该端口的实际可用性。
- 监听地址固定为 `127.0.0.1`，状态初始为 `stopped`。
- 创建时的一次性密码需要由调用方妥善保存。后续读取不可恢复原密码，只能重置。
- PATCH 提交名称、国家和策略，不允许提交非零端口；更新不改变原端口和凭据。

出口字段包括：`id`、`name`、`country`、`policy`、`port`、`listen`、`username`、`status`、`reason`、`created_at`。当前合法状态仅有 `stopped` 与 `blocked`。

## 节点与统计的含义

- `advertised_ping` 和 `advertised_speed` 是上游列表的数据，不是本机测速。
- `type` 固定为 `unknown`，`status` 固定为 `untested`。
- 获取配置会验证已知安全格式，但并未完成服务器证书链验证、运行时安全验证或建连测试。
- 每次显式同步最多处理 5000 条记录、保留 2000 条候选，总响应限制 8 MiB。
- 一个进程内同时只运行一个同步，至少间隔一分钟，HTTP 超时 25 秒；官方 HTTPS 端点固定，禁止重定向。
- 同步失败或结果为空时保留原缓存，不把过期缓存说成当前可用。
- 概览的 `active_count` 为 0，`executor_available` 和 `protection_verified` 为 false。这是未实现能力的明确表示，不是模拟数据。

## 错误格式

```json
{
  "code": "executor_unavailable",
  "message": "启动已阻止：网络执行器尚未接入，未建立隧道、未监听代理端口。"
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
