Miku Gate v0.2.7

- 统一原生 / Docker 终端菜单，Docker 宿主机支持 `miku`。
- 菜单支持选国家创建节点、查看出口 IP、切换已有出口 IP、查看后台任务与订阅。
- Docker 安装自动准备 Debian / Ubuntu 环境，以简洁进度代替构建刷屏；失败保留私有日志。
- 安装结束显示实际前端入口与登录口令，`miku info` 可随时查看。
- 缓存软件包与已验证核心归档、下载重试，并继续复用未变化的本地镜像。
- 保留原生安装、独立前端、端口自动避让、现有数据和凭据。
- 一键配置固定为 VLESS + TCP + REALITY + Vision，复用已完成的日本 5 / 韩国 3 / 美国 3 个节点，仅输出该批节点链接。
- 新建单个入口不再询问出口，输出单条节点链接并引导去面板新建出口。
- sing-box 提供 VLESS / VMess 的 WebSocket + REALITY，以及 Trojan + TCP + REALITY；Xray 隐藏这些专属选项。
- 菜单支持保留数据卸载及彻底卸载；修复安装进度漂移、重复标题和部署方式识别。

安装（Linux root）：
```bash
bash <(curl -fsSL https://raw.githubusercontent.com/vioIetMiku/miku-gate/main/mikugate.sh)
```
Docker：在同一命令末尾添加 `--docker`。
安装完成后运行 `miku` 打开菜单，`miku info` 查看地址和口令。
