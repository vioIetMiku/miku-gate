# 已有3x-ui入口的持久复用适配（开发中）

此适配修改面板持久Xray模板并请求面板自身重建/重启核心，不直接写`bin/config.json`。当前官方API默认current（`panel/api/xray/`），旧版显式legacy（`panel/xray/`）；不遍历接口/禁用CSRF。仅本机127.0.0.1、明确基础路径/非特权端口，面板账号和所选Miku出口凭据从私有stdin传入，登录密码不保存。

当前命令协调器 `scripts/miku-panel.py` 与主线新版CLI配套，尚未纳入旧dev.12发行/网页持久应用流程。需要已安装主线Miku和健康已启动出口；先inspect返回匿名入口ID、模板/生成配置双摘要，再明确apply。新程序通过Miku数据库实例/逻辑出口核对认证目标，并执行本机SOCKS认证握手。网络请求只是本机面板，不向外部发送面板秘密。

```sh
# 私有答案JSON包含 username/password/two_factor_code（可选）；apply还需proxy的port/username/password。
# 先在root私有0600文件准备，不把密码直接写命令历史；文件必须由你确认来源。
sudo python3 -B scripts/miku-panel.py inspect --port=2053 --base=/your-panel-base/ --confirm-read < /root/panel-private-input.json
```

apply需要`--inbound=匿名ID --exit-id=实际逻辑出口ID --expected-template=摘要 --expected-rendered=摘要 --backup=全新root私有文件 --confirm-read --confirm-apply`；rollback需要同来源归档/明确SHA256与当前双摘要/全新备份/明确确认。没有任何自动修改用户VPS面板操作。

原inbounds、客户端身份、域名证书/REALITY和未选入口保持；只新增所选inboundTag的UDP阻断优先规则和TCP到本机认证SOCKS规则。先保存root600私有旧模板/生成配置快照，写入前再次核对双摘要，保存后重新读取持久模板与生成配置验证。失败尝试恢复旧持久模板并验证，不能验证时明确RECOVERY_REQUIRED，不假称成功。归档含代理密码/已有节点身份，不上传GitHub。

已增加模板字段保留/双摘要变化/重复绑定拒绝和固定本机接口/入口不变/UDP优先负例。本批尚无真实面板版本兼容、实际端到端业务或公网部署证据，不能称完整面板复用完成；面板保存会短暂停止已有连接，并发面板编辑应停止。持久客户端选择/UI、凭据安全存储、断电恢复与跨版本适配继续开发。依据官方固定源码73a028f的CSRF、持久Xray配置和server API分组，不猜旧路径。
