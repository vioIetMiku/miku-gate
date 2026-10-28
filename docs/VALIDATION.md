# v0.2.6 验证状态

发布状态以 CI 的实际成功记录为准。输入包的历史测试声明不能代替本次发布证据。

本次门槛：Go race/vet 与双架构源码构建；Python 维护、端口、前端配置与生命周期；指定真实 Nginx 的代理集成；内嵌与独立前端 DOM；Docker 健康启动、凭据保留、8899/8900 同时占用的自动避让、前端登录/API/Origin拒绝、前端停止后后端存活；完整包/源码/前端包/自解压/资源清单校验。

当前工作环境为 Windows，没有本机 Linux Docker / Nginx。真实容器与代理步骤需在 CI 执行；未声称真实 VPN Gate 或公网签证书已经验收。

---

以下是输入包自带的历史记录：

# 本包验证记录

版本基线：v0.2.4。记录日期：2026-10-28。本次修改集中在 Docker 部署、独立前端、自动端口与文档；随包 amd64 / arm64 后端和旧版导出助手的二进制保持原基线，未声称发布新的远程正式版本。

## 已执行

- 83 项 Python 回归：安装与维护、Docker 启停及备份、真实 IPv4 / IPv6 端口占用、预留端口、端口持久化、实际端口健康检查环境、前端重复启动、后端变更同步、Nginx 候选配置验证、失败回滚及资源校验。
- 其中 5 项使用本地编译的真实 Nginx 1.28.0 与模拟 HTTP 后端，检查登录 / 会话、API、订阅查询参数、随机路径、Origin 拒绝、内部路径隔离及容器二进制更新接口拦截。
- Node.js / jsdom 交互检查：现有节点、任务、订阅、主题、复制和 IP 查询流程；新增分离设置页检查，确认保存后端端口 / 路径时不跳转到猜测地址，并提示同步前端。
- Go 1.26.0：主后端 `go test ./src` 与 `go vet ./...`；兼容导出助手 `go test ./...`。
- 所有 Shell 脚本及工作流 Shell 块的语法检查、所有 Python 源码的 AST 检查、Compose 与工作流 YAML 解析检查。
- 前端资源 SHA256、完整项目 BUILD 清单、最终 ZIP 完整性与解包后的安装清单校验；两种架构的预编译文件摘要与输入包保持一致。

可复现的核心检查：

```bash
python3 -B scripts/export-frontend.py
MIKU_NGINX=/绝对路径/nginx python3 -B -m unittest discover -s scripts -p 'test*.py'
MIKU_JSDOM_MODULE=/绝对路径/jsdom node scripts/test-ui.cjs frontend/public/index.html
go test ./src
go vet ./...
```

`jsdom` 检查需要 jsdom 26.1.0；没有 `MIKU_NGINX` 时，5 项真实代理测试会被跳过。Python 检查包含 root 文件归属 / 权限断言，建议在隔离环境以 root 运行，不针对生产数据执行测试。

## 尚未在本环境执行

当前交付环境没有 Docker Engine 和可用 TUN，未实际构建 / 运行 Docker 镜像，也未创建真实 VPN Gate 隧道。真实 Nginx 与模拟后端测试通过不能替代容器、netns、路由、证书签发或公网节点验证。

发布工作流已增加默认端口 8899 / 8900 同时被占用时的 Docker 启动检查，并从实际保存结果读取前端地址；这些工作流步骤需在具备 Docker / TUN 的运行器执行，本地没有运行远程工作流。

外层反向代理、云安全组与宿主机防火墙需要按实际选中的端口配置；自定义固定公网 Origin 不会自动改写。后端在网页或 Docker 自动重启中改变连接参数后，需要运行 `frontend restart` 同步；本版不含后台配置监听器。
