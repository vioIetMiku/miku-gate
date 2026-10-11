# 2026-10-11：直接HTTPS与交互首次安装验收

## 实际CI安装矩阵

最新精确源码`1ba9f7e193b899f60b18d9a2126c5385a312f682`，installation-lab `38110960541`成功：无VPNjob114386252259、有VPNjob114386252347都实际创建专用账号/证书副本/systemd单元并启动服务。verify `38110958270`成功，包含Go race、66项前新增前63项HTTP/CLI、30项Python与Chromium旧旅程。后续管理访问摘要UI改动须另按精确提交回归。

每矩阵都有实际普通UID CLI程序测试：IP SAN引导证书按明确本地信任核验HTTPS、精确Host/Origin、Secure Cookie与退出撤销；域名management.test通过SAN+私有CA（仅子进程注入），不用bootstrap或insecure，不改变宿主根信任。这不是公共域名DNS控制权或ACME签发成功的证据。

plan-only真实命令后断言固定项目目录/单元仍不存在，再以明确本地答案apply；选择IP、独立39443端口、managed_core=none。安装器核对HTTPS叶指纹和Host健康响应，不只看systemctl active；实际服务回读User=miku-gate、NoNewPrivileges=yes、MemoryMax=402653184、CPUQuotaPerSecUSec=1s。

有VPN矩阵安装缺少发行包，生成不同非rootOpenVPN/worker账号和根执行器单元；能力由授权普通控制UID读回isolated-openvpn/instance/工作UID。只证明安装后的IPC可达和授权身份正确，没有在systemd沙箱里另建立VPN数据会话或安装Xray/sing-box路径，不能拿此前手工root实验替代这些门槛。

[矩阵机器摘要](evidence/2026-10-11-https-install-ci.json)。初始化一次性口令日志保持0600、未上传/回显，下载的是有界断言结果，不是私有安装日志。CI未独立比较整个宿主网络快照，不宣称这部分已验收；可丢弃runner并不替代用户已有面板主机。

## 已授权VPS只读计划和临时HTTPS

精确源`90c77fe55a9682ea2a80ff6bc85fbfeb2f3d2074`，程序SHA256 `6f637142ce0897e6efb10e7ae837f562ec89e5605f6cd3e62933b7447fe3a792`，干净构建。实际miku-install plan-only在已有x-ui/Nginx主机完成检测与计划，不安装任何包/账号/systemd/永久目录。随后独立临时数据和普通UID65534CLI，用覆盖VPS IP的自签证书只回环监听，客户端显式导入此证书、不跳过TLS，核对叶指纹。

真实登录、Secure/HttpOnly/SameSite Cookie、错误Host与HTTP Origin拒绝、logout和旧session401通过。宿主接口/路由/rules/nft/DNS/监听PID-starttime/挂载等快照相同，固定永久Miku目录/账号/unit存在性前后相同，临时进程/目录精确清理。没有将自签身份标为系统CA或浏览器默认信任，也没有让管理密码走公网明文。

[用户VPS机器摘要](evidence/2026-10-11-https-plan-vps.json)。这是已有服务主机的只读/临时HTTPS证据，不证明用户VPS已永久安装、公开防火墙或外部设备可达。当前VPS没有被这个测试应用永久安装。

## 失败修正保留

首轮4da3b38矩阵：无VPN应用成功，有VPN安装/管理也启动但验收脚本用root查询仅授权普通UID的执行器，被IPC拒绝，整个分项失败。90c77fe改为授权控制UID查询，且安装器自身也纳入instance/工作UID能力核验，未扩大执行器allowUID；重跑两矩阵通过。原失败日志保留，不把服务active当全部完成。

## 剩余明确门槛

公共域名ACME申请及续期、真实受管核心安装后启停、systemd沙箱内VPN数据面、永久安装对已有服务的完整快照、管理公网/远端浏览器、证书副本原子更新、备份升级回滚卸载、已有面板持久协同和出口情报仍待逐项验收。脚本已可规划并在干净runner实际应用不是整个安装/运维终态完成。
