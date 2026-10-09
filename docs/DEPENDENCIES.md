# 依赖、来源与分发边界

## 项目实现来源

Miku Gate 的当前控制面、数据模型、接口、代理前端和测试为本工作区独立编写。参考项目只用于理解需求和架构取舍，没有拷贝其 Go/Python/Worker 源码。

- fanout：参考多出口与 namespace 架构。
- AimiliVPN：参考候选检测、类型信息与恢复流程。
- Free-Residential-IP-Proxy-Controller：参考控制端与 Agent 分离思路。

参考项目的许可证和未来可能引入的代码需逐项审查；当前不声明已完成发行许可审查，项目分发许可证尚未选定。

## 锁定依赖

准确版本以 `go.mod` / `go.sum` 和 `web/package-lock.json` 为准。

| 用途 | 依赖 |
| --- | --- |
| SQLite | `modernc.org/sqlite`，纯 Go 驱动，避免本机 C 编译器依赖 |
| 密码哈希 | `golang.org/x/crypto/argon2` |
| Web UI | React、React DOM |
| 图标 | `@phosphor-icons/react` |
| 前端构建 | TypeScript、Vite、React 插件 |
| 前端单元测试 | Vitest |
| 浏览器回归 | Playwright Test |
| 格式校验 | Prettier |

当前并不捆绑 OpenVPN 或 Xray 二进制；那些运行时属于后续执行器阶段。引入时需另外处理许可证、版本固定、校验与升级策略。

## 本工作区工具安装记录

- 由于系统 PATH 未提供 Go，下载了项目内便携 Go 工具链，位于 `.tools/go`。
- 下载元数据来自 Go 官方下载接口，压缩包已和官方元数据中的 SHA-256 核对后解压；下载文件名和预期哈希保留在 `.tools/go-release.json`。
- 系统 PATH 未更改，工具链和缓存不会纳入 Git。
- 官方 Go 模块代理连接超时后，使用操作者已批准的 goproxy.cn 下载依赖，保留 `sum.golang.org` 校验，没有禁用 TLS 或校验数据库。
- npm 安装缓存位于项目 `.cache/npm`，没有全局安装前端依赖。
- 没有额外安装 Codex skill、插件、系统 VPN 服务、WSL 或浏览器扩展。

## 本机敏感文件

- `.data`：正式本机开发账号与配置，首次 init 后创建。
- `.cache/e2e*`：浏览器测试的临时身份及数据库，不能用作真实访问凭据。
- `.cache/smoke`：真实 HTTP/CLI 回归产生的隔离数据库；随机密码只由测试进程短暂读取。
- 以上均为忽略项，不应提交到 Git、上传日志平台或作为普通截图附件分享。

`.gitignore` 不是权限边界，不能保护取得宿主文件访问权限的人。外部分发前还需审查源码/二进制依赖许可证并提供必要 notices。
