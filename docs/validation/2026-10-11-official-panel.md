# 2026-10-11：真实官方3x-ui v3.9.0持久复用

源362daa7b19001a09edce154ba14b488cd6dfb350，official-panel-lab38126941080/job114433154872最终success，2.18秒；同源verify38126926056success。固定官方Linux amd64归档摘要d7cbe0bf6358ee0d2117c24fd2efb483502e411d38e2ea59bd0bf5e7a3e39390。不是自写HTTP面板夹具。

## 实际链路

普通UID65534/NoNewPrivileges、匿名只lo网络、独立私有panel DB/bin/log，不执行外部安装脚本，不访问用户VPS/宿主服务。官方panel CLI初始化本机管理后真实登录/CSRF，官方API创建两个TLS/Vision节点，客户端UUID/其他节点保持。

Miku生产paneladapter读取持久模板和生成配置，限定选定inboundTag的UDP block/TCP socks，保存到实际面板持久API且回读。真实原生Xray客户端用私有CA验证TLS（不insecure）→面板Xray→Miku实际认证SOCKS→受控回环目标。面板进程停止/重新启动且DB保留后，路由/原客户端仍保留并实际业务成功。停止Miku代理后业务失败、不direct；再面板持久API恢复原模板，原客户端/未选节点/生成配置一致。

安全断言只布尔/版本，无账号/密钥/配置日志。完整[机器摘要](evidence/2026-10-11-official-panel.json)。公共VPN未连接，这不是最终公网入口到实际VPN的证明。

## 首次兼容失败保留

4b30f7f专项38125723601/job114429573198实际登录/CSRF/生成路由后，持久模板字节不符失败。官方SaveXraySetting会将内部api stats规则置顶。候选精确保留原唯一api field路由首位，MikuUDP/TCP仍位于其他业务规则前，混合/重复api拒绝；没有忽略任意模板差异或放宽TLS/其他节点保留比较。新源成功另记录，旧失败不改。

## 当前范围

已证明官方v3.9.0、Linux amd64、隔离本机HTTP面板与TLS/Vision入口的持久复用/重启/回滚。新HTTPS-only本机接入另有受控信任边界用例：系统CA/明确SAN，数字回环拨号，未受信任时不发凭据，没有insecure生产开关。

其他面板版本、用户VPS已有服务环境/公网实际VPN链路、独立Xray/sing-box旧服务持久协同、root公网ACME/自动换线长稳等仍未全部完成。完整目标/Issue保留开放。
