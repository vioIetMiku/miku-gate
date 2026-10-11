# HTTPS管理与交互VPS安装（dev.11开发）

## 管理访问模式

默认仍回环HTTP。公开管理必须显式`--public-origin=https://域名或IP:端口`、数字IP监听和对应绝对路径`--tls-certificate`/`--tls-key`；TLS至少1.2。`serve`拒绝公网明文监听，直接HTTPS只接受精确Host与HTTPS Origin，不信任X-Forwarded-Proto/Host扩大权限；Secure/HttpOnly/SameSite会话与CSRF保留，退出Cookie也Secure。不是随意给Host白名单然后允许裸HTTP。

域名模式须证书的SAN、公私钥、有效期至少24小时、用途/算法与系统信任链通过。IP模式也可用已受信任的IP证书；没有公共CA证书时明确允许自签引导：`management bootstrap-ip`需数字IP/全新私有目录/confirm，证书IP SAN，随机密钥，90天，自签并不默认受浏览器信任。

使用自签IP必须通过已信任SSH安装输出核对叶证书SHA256或将公有certificate.pem导入客户端信任后再输入密码，不能让用户无脑忽略证书警告或宣称默认可信。TLS终端启动还要明确`--allow-bootstrap-certificate`，仅匹配IP的有效自签叶可接受，不给域名开无验证例外。

当前TLS证书载入在启动时，续期原子替换/重载与短期IP证书管理继续开发。已实现read-only`management inspect`，不返回私钥；不要打印或传浏览器PEM密钥。

## 当前快速安装入口

发行脚本将在下一确认可用版本一起打包，dev.10已发布资产不追加替换。当前源码中的`install-vps.sh`+`miku-install.py`使用**本地已校验二进制**：

- 提供`--binary=绝对路径`和`--sha256=可信发行校验值`；root所有、无组/其他写、独立文件及可信父路径才执行。不要pipe未经审核remote脚本。
- 先检查python3；缺少时明确询问，仅apt系统安装。`--plan-only`缺解释器不安装，而是停止。
- 协调器当前仅Debian/Ubuntu、systemd运行环境、amd64/arm64；有VPN需求还需要TUN。检查确实缺少的工具/用途/包，再询问是否安装，只安装缺少项，不全局upgrade。
- 明确确认VPS地址，询问是否域名、管理端口（默认8443避免抢80/443）；端口占用拒绝，不杀原服务。
- 只读显示x-ui/3x-ui/Xray/sing-box等痕迹，询问是否希望复用。若复用持久协同未适配，明确询问先装管理/出口、保留复用意图，**不擅自接管或只改面板生成文件**。
- 选择独立xray/sing-box/both/none，固定版本官方压缩包SHA256/大小验证，只提取精确ELF，不把检测的面板二进制当执行许可。
- 域名优先复用已验证常见ACME证书；没有时可明确同意Certbot standalone申请，先检查80空闲与域名解析，不停Nginx、不抢端口。冲突时请先使用Webroot/DNS自行申请后重试；集成已有Webroot/DNS流程待补。
- 无域名需要明确自签IP信任方案确认，提供指纹，管理使用VPS IP HTTPS，不强制SSH转发为唯一最终访问方式。
- 最终列出目录/服务/专用账号/资源预算并再次询问apply。`--plan-only`不装软件、不建账号、文件或service，明确允许的证书/DNS观察只是只读。

## 安装归属与服务

首次安装固定独立目录/usr/local/lib/miku-gate、/etc/miku-gate、/var/lib/miku-gate，任何既存Miku资源/账号/单元拒绝覆盖。专用账号miku-gate（控制）、miku-openvpn、miku-worker不同UID；控制和子核心普通用户、NoNewPrivileges、无能力，根VPN执行器明确授权三个身份。

管理unit启动直接HTTPS、默认同时1出口，CPUQuota100%、MemoryMax384MiB、swap0、Tasks128；VPN独立256MiB/swap0/Tasks64。资源预算是配置，不等于吞吐容量证明。已有面板/Nginx/宿主路由和防火墙不改；仅明确Miku units enable/start。外网访问还需用户在已有防火墙/云安全组明确开放管理TCP端口，不公开认证代理端口。

一次性管理密码只在终端显示；不会写install-state/unit文件。失败前就显示以防初始化已成功但服务失败后丢口令。CI使用仅0600的临时日志且不上传/回显该口令。IP/域名实际TLS证书指纹及Host健康响应通过才报告管理服务启动成功，不只看systemctl active。

失败只停用本次已创建的确切units，保留独立目录/账号供核对，不递归误删或撤销ACME证书/系统包。升级/备份/回滚/卸载尚未接线，不把未知已有路径当“可覆盖恢复”。账户/证书副本续期、根证书/入口资产助手仍需后续完成。

## 验证门槛

单元测试覆盖精确Origin/Host/TLS/转发头、Secure会话/退出、IP SAN/隐式信任拒绝/私钥权限/符号链接/独占、依赖拒绝、plan-only无写、端口80冲突不动原服务和unit身份/预算。手动installation-lab新增实际普通UID HTTPS CLI和可丢弃Ubuntu计划/应用/systemd/health旅程。新增测试/代码不等于已执行成功，精确CI或授权VPS执行证据另记录。

用户VPS本阶段先仅plan-only/独立临时HTTPS测试，未在真实VPS系统路径安装或修改现有服务。完整终态R01-R13与原功能范围保留。
