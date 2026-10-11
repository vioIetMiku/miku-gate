# 开发进度与验证记录

## 官方面板首次具体兼容修正

4b30f7f的官方专项38125723601/job114429573198已真实登录/CSRF、持久API保存及生成路由验证通过，但失败于持久模板字节比对：官方SaveXraySetting会把内部api统计规则置顶。修正候选生成保留唯一仅api的统计首规则，MikuUDP/TCP仍位于所有业务规则前；不忽略任意配置差异、不跳过其他节点保留核验。首次失败保留，修正后只继续这条新的官方专项。

## dev.15主线：真实面板兼容与HTTPS-only接入

固定官方v3.9.0面板专项4b30f7f已调度一次，仅匿名回环空间，不访问用户VPS/公开VPN。并行补面板HTTPS-only接入：系统可信证书/明确SAN，身份域名不用于DNS，未信任时不发登录请求；UI/快照来源绑定协调。旧dev.14不修改，官方实装结果待精确读回。

## 下一缺口：真实官方3x-ui持久复用

新增固定官方v3.9.0（归档摘要核对）的隔离实装专项，非夹具面板：普通UID本机API/CSRF→两个TLS/Vision节点→生产持久路由→真实加密客户端/Miku认证代理→面板进程重启仍保留→停止代理无直连→旧模板节点回滚。只编译/vet这条新路径，执行结果待读取；不修改用户VPS或重放公共VPN。

## 最新交付：dev.14已发布，schema7安装运维实际通过

固定源205f674，verify38123495516和installation-lab38123497800两模式success。新schema7真实安装/双核心/配套升级回滚、服务卸载保留数据/key/证书/账号、恢复注册不start/enable、明确启动后原HTTPS会话/加密配置可读均读回安全断言。三平台包及校验文件已上传逐项核对。自动恢复/同国家候选/持久暂停界面与受控实际监听原凭据恢复用例通过；公共VPN实际自动换线未用旧结果代替。详见[本版证据](validation/2026-10-11-dev14-release.md)。

完整目标仍开放：官方面板/独立旧核心持久兼容、公网客户端真实业务、root实际ACME续期、installed系统数据面长稳及公共自动换线、跨机恢复等继续推进，不反复运行已经通过的公开实验。

## 自动恢复检查的同步修正

2ed2c48的verify38122865196失败于新回环恢复测试把starting预约误判为已开放监听（factory调用计数早于Probe完成），不是实际失败路径打开代理。改为等待观察到真实backoff后再判断ProxyListening，并使用完整SOCKS帧读取；旧失败保留。新安装schema7/卸载恢复专项同批推进，未完成前不发布成功声明。

## 新安装schema7运维专项

现有installation-lab新增本版实际安装/配套升级回滚后服务卸载→保留数据/账号/key→恢复注册但不偷偷enable/start→明确启动→原HTTPS会话和加密配置仍可读取旅程。只核对固定Miku单元，新卸载恢复用例不是旧公开VPN重放。源码接线并待执行读取，不把新增脚本说成已经通过。

## dev.14主线：自动恢复与同国家换线

明确授权策略/控制重启后恢复、最多3个同国家候选/真实TLS指纹、指数退避、健康不查询、探测失败立即撤销转发、人工停止持久取消，入口凭据/端口不变；受管核心可显式恢复并重检REALITY素材。状态/设置网页接入，新增受控恢复检查；公共VPN自动换线与installed长稳待专项，不重复旧实测。详见[AUTO_RECOVERY](AUTO_RECOVERY.md)。

## 最新交付：dev.13面板网页与保留数据运维已发布

源376aef8的verify38120443665全部success；Linux两架构/Windows三包与SHA256SUMS已上传逐项核对。面板本机网页CSRF登录/脱敏入口选择/所选路由预览/确认应用/加密快照回滚已交付，schema7和面板key配套检查，新增保留数据服务卸载/注册恢复。新路径受控API和手机浏览器通过；原VPS/公开VPN未重复测试。详见[本版证据](validation/2026-10-11-dev13-release.md)。

真实官方面板/公网完整客户端业务、独立旧Xray/sing-box持久协同、root实际ACME、systemd长稳/自动换线与真实卸载恢复注册仍未完成。整个目标不关闭。

## 主线dev.13：保留数据的服务卸载入口

新增uninstall-services/reinstall-services，精确已登记Miku单位/原服务状态/整套私有快照/未知模板或drop-in拒绝，不删除DB/key/证书/程序/账号、不卸载共享工具或面板。恢复注册不自动start/enable。拒绝确认与unit范围快速检查，真实Linux系统卸载恢复旅程仍待执行，不称整个运维验收完成。详见[保留数据服务卸载](UNINSTALL.md)。

## 主线dev.13：面板复用网页与schema7私有快照

现已从本机命令推进到网页真实API：明确CSRF登录/脱敏入口、所选路由预览、健康Miku出口绑定、双摘要/候选摘要确认、加密旧配置与未完成事务、应用后生成规则和其他节点保留回读、失败恢复及网页回滚。面板账号密码不存，当前只本机HTTP接口，旧Release不改。schema7与旧配套DB/key迁移/检查协调，避免panel-only秘密被旧程序忽略。

新增受控回环面板HTTP/CSRF、真实管理员API应用/失败回滚和加密快照用例；界面构建与手机新旅程只检查这一新增路径。实际官方面板版本/公网客户端到VPN业务、独立旧Xray/sing-box服务的持久协同和自动恢复仍待交付；不是整个项目完成。

## 主线新增面板持久模板API适配

3x-ui本机明确API模式/CSRF、持久模板双摘要/定向TCP与UDP保护/配套备份/失败恢复已加入；Miku实例与逻辑出口绑定，不改config.json生成物。仍缺真实面板版本与业务、网页向导验收，完整目标不关闭。详见[PANEL_REUSE](PANEL_REUSE.md)。

## 最新交付：dev.12已发布，证书更新与IP情报

固定发行源c7535d8，verify38117134364全部success；三平台包与校验文件已上传逐项读回，旧Release不改。证书普通UID真实CLI换证证据源e20a777已成功；节点/实测出口/域名归属、ASN和DNSBL辅助API/界面与同意/未知边界已交付。新浏览器旅程和手机布局通过，第三方响应为明确UI夹具。详见[本版证据](validation/2026-10-11-dev12-release.md)。

未缩小完整目标：面板持久复用、公网部署/ACME、root定时协调器完整实测、systemd长期VPN/自动换线与保留数据卸载继续待完成。下批优先持久面板接口而非反复公开VPN测试。

## Linux静态检查的具体修正

fef1eba自动verify38116932082在Go vet发现新增Linux域名换证测试重启context缺少显式cancel路径；实际换证流程已有成功证据，仍不将此次检查说成成功。补充显式defer cancel，不重新调度公共VPN/安装实验。原失败保留；新源自动检查结果待读取。

## 新情报弹窗浏览器旅程

真实dev.12 CLI/登录会话下仅运行新增情报弹窗用例，核对勾选前不请求、勾选后的固定请求、pending/住宅未知文案、手机无横向溢出和无JS错误，约2.8秒通过。第三方结果是明确UI夹具，不冒充公网数据链路。证书timer归档恢复保持原模板的负例通过；不重跑旧VPN/VPS实验。

## dev.12本批：换证实际CLI通过与归属/ASN/风险界面

源e20a777的certificate-lab38116179316成功（job114401409204），真实普通UID替换证书后保留加密入口身份/代理绑定/客户端导入并通过新HTTPS；无公网ACME。verify38116177520失败于非rootPython夹具读取/root来源，现修正先拒绝未授权续期、来源不可读安全跳过，不重跑旧公开VPN/安装。

新增节点库和实际健康公网出口的归属/ASN/黑名单辅助弹窗，明确同意后访问固定HTTPS源，显示来源时间/缓存/未知；域名只解析公共IPv4，住宅始终未知，未列出不叫纯净。后端对应回归和10项前端契约/构建通过。完整目标仍开放，面板持久复用、公网部署/ACME和自动恢复等继续开发。详见[信息查询说明](IP_INTELLIGENCE.md)。

## dev.12开发：证书续期、配套TLS更新

接上中断前证书功能：固定来源域名副本/明确Certbot授权，定时或手动配套快照更新，保留客户端身份，失败恢复旧DB/key/证书。不改已发布dev.11。增加只执行新增换证路径的certificate-lab，不重跑公开VPN/整套安装实验。普通UID真实域名换证用例的结果待读取，公共ACME和业务会话自动恢复尚未交付。详见[证书更新说明](CERTIFICATE_RENEWAL.md)。

## 最新交付：dev.11已发布，安装/配套升级与回滚实际通过

源177aa6e的installation-lab38114561022两模式success，真实加密配置先保存/备份/升级后更改，再从归档恢复原程序、DB/key与配置，并通过HTTPS/VPN身份；verify38114558358success。Linux amd64/arm64、Windows amd64和SHA256SUMS四资产dev.11已上传读回。详见[独立证据](validation/2026-10-11-dev11-installed-upgrade.md)。

本次是同schema/同源码不同构建替换验收，不证明跨schema迁移、真实断电恢复、用户VPS永久安装、公网可达与长期业务VPN。原目标仍开放。标签与文档-only不重复跑整套verify；后续优先证书续期、出口情报与面板持久协同。

## 本批：配套升级/回滚与中断恢复

增加同安装/schema6的release升级、备份归档回滚、失败自动恢复和durable恢复日志；启动保护阻止替换中途重启，固定路径/身份/摘要/普通账号程序，原配套DB/key/证书/登记/单元一致恢复。新只读data-check逐条真实解密加密记录；不是仅看文件/healthy。跨机灾备/未知schema迁移/旧无维护工具安装不冒充已交付。

Python本批新增负例与故障回滚检查；新增Go配套key检查与CLI检查已通过。现有installation-lab接入配置变更后的实际升级/回滚，执行结果待精确读取。不重复公开VPN/VPS实验。详见[升级回滚](UPGRADE_ROLLBACK.md)。

## 本批：发行包直接安装与固定维护入口

解压新版包可直接交互安装，自动核对BUILD清单/平台/程序与三脚本；安装后固定路径维护工具一并登记摘要、纳入私有备份。精确clean-HEAD二进制离线打包器已加入，旧Release不修改。单批只做必要Python回归，不再重复公开VPN实验。

18247c6已有安装任务38112539466已结束：两模式真实HTTPS安装和官方双核心运行通过，verify任务38112537151通过。备份夹具201状态码判断失败，且准备上游端口9不符合非特权限制；已修正并保留失败记录，不宣称备份实际通过。完整目标仍保留；升级/恢复、续期、持久面板协同继续待实现。

更新：2026-10-11。主线 `0.1.0-dev.15` 开发中；已发布 dev.2、dev.6、dev.8、dev.10、dev.11、dev.12、dev.13、dev.14。**整个网关目标仍未完成：安装、管理 HTTPS、已有核心复用与加密入口等继续开发。**

## 当前最新状态

- dev.11直接IP/域名HTTPS实际CLI与无VPN/有VPN首次安装矩阵已在1ba9f7e通过；用户VPS仅plan-only和回环临时IP HTTPS登录/指纹/退出通过，永久资源/宿主快照不变，没有应用永久安装。域名CA为测试子进程私有根，不冒充ACME/DNS已验；systemd内VPN数据面/受管核心下载启动/续期回滚仍待验。见[精确结果与失败修正](validation/2026-10-11-https-install.md)。


- dev.11开始直接域名/IP HTTPS管理与显式交互安装：TLS/精确Host-Origin/Secure会话、明确IP自签指纹方案；依赖/域名/已有服务/核心/最终apply逐项确认，专用账号和systemd有界units。不覆盖未知资源，不停现有Nginx/面板。新实际安装/HTTPS实验待回读，见[流程与边界](HTTPS_INSTALLATION.md)。


- dev.10新Release已发布，源c642476，三平台压缩包＋校验值远端验证，Linux amd64与同文件两核心公共实验一致。最终23.740秒、191.34MiB峰值、全宿主快照一致，Windows压缩包实际程序63项smoke通过，arm64未真机验证。首轮Fail2ban集合差异保留为失败，未过滤集合重测通过；见[发行同文件验收](validation/2026-10-11-dev10-release.md)。安装/管理HTTPS/面板持久复用/生产升级目标仍未完成。


- dev.8 的 `02f37db` 已在授权 VPS 成功完成真实官方节点、服务器验证/证书固定、隧道 DNS/出口 IP、认证入口 HTTPS、probe/stop；5.939秒，176MiB scope峰值，宿主快照不变，无残留。见 [公共验收](validation/2026-10-10-public-vpngate.md)。
- 同提交 verify、受控 OpenVPN、完整 CLI/IPC VPN 工作流均通过；错误服务器名称/指纹拒绝及断线/崩溃不直连有实际断言。
- dev.8 Release 已发布三平台包和校验值；Linux amd64 与 VPS 测试二进制一致，Windows 解压 smoke 48项通过，arm64 未在真机运行。旧 Release 不变。
- dev.9 开始实现安装向导：只读既有面板/核心/工具检测，CLI、认证 API 和系统页已接线。只展示证据，不假称已可复用；[操作与边界](HOST_INVENTORY.md)。

- `42e9e66` 新检测在 VPS 普通 UID/root 两视图实测通过：发现 x-ui 和 Nginx，工具清单正确，主机快照不变；GitHub CI `38065913983` 通过。见[主机清单实测](validation/2026-10-11-host-inventory.md)。仍未识别端口/可复用入口，不自动接管。

- dev.9 新增已有核心配置检测（明确同意、固定路径、脱敏摘要）和本机私有定向路由候选。保留原客户端和其他入口，选定 TCP 送认证 SOCKS、UDP显式阻断；不改运行配置、未做面板持久化。双核心 TLS/Vision实际实验已在`e1e97ee`通过，选定/未选入口、UDP正反对照和停止不直连均有断言。补充安装归属明确授权只读方式与实际TCP监听相关性，VPS复测另记精确结果。见[已有入口](EXISTING_INGRESS.md)。

- `65fca7e`最终双核心与verify均通过；VPS入口摘要root成功、普通账号权限拒绝，发现3条没有客户端的TLS配置，不误标可复用，原配置hash/宿主快照不变。明确并行新建需求。
- dev.9补受管入口模板/客户端导入、后端证书自动查找与验证、REALITY十候选/自填/固定443安全TLS检测和私有bundle命令；UI只预览和安全素材摘要，不宣称已部署。实际六组合新实验在`a300500`全部通过；见[受管入口](MANAGED_INGRESS.md)。

- `77242eb`已在授权VPS通过两个真实REALITY候选TLS1.3/X25519/h2探测、两核心私有bundle生成/不覆盖/秘密隔离，主机快照不变；未启动核心。六模板私有实验最终`a300500`全部通过，Xray REALITY修正私有目标过早关闭（不改产品安全验证）；[完整实测与失败](validation/2026-10-11-managed-ingress.md)。

- 最终`a300500`同提交verify与核心实验均通过：六模板13.95秒、既有双核心2.75秒，无SKIP，实际core check/加密握手/认证出口/UDP阻断/停止无直连全覆盖。修正私有REALITY预检竞态，失败历史保留；常驻/安装/完整公共链路仍未完成。

- dev.10新增schema6的入口加密保存/出口绑定、客户端材料明确显示、CLI显式核心路径、普通UID真实核心check/stdin/监听FD归属、启停与出口租约互锁；UI已接保存/启停/状态。新native核心实验需独立实际回读；[操作/边界](MANAGED_RUNTIME.md)。

- 受管运行器两核心真实stdin/UID/NoNewPrivs/PID监听/租约停止已在5ee75c4通过。a837ecf授权VPS完整公共Xray REALITY→隔离VPN→HTTPS/IP一致性/重启身份/互锁/退出通过；sing-box测试客户端启动失败，整体验收false继续定位，主机快照一致。见[实际证据/边界](validation/2026-10-11-managed-runtime.md)。

- 最终授权VPS两核心加密入口→实际公共VPN→业务HTTPS/IP一致性全部通过：产品a837ecf/脚本f82502d，23.299秒，205.79MiB峰值，资源事件0/无残留/宿主不变。sing-box修复的是root测试协调器stdin管道归属，不改产品验证；失败历史保留。入口回环测试并非已开放公网安装验收，[成功证据](validation/evidence/2026-10-11-encrypted-public-success.json)。

- 同域名管理证书受管副本→普通服务UID的TLS入口自动复用已接并在ea2adae实际CLI/API保存/客户端输出通过（私有CA只作用于测试子进程，不代表公开ACME）。安装器两核心下载/运行在18247c6通过；配套备份实验在准备数据夹具失败，修正后的归档/重启结果尚未读取，恢复升级尚未实现。

## 历史阶段记录（保留当时限制，不代表当前状态）

下面记录各提交当时的实现/失败状态。“尚未接线/待公共验证”等必须结合上面的最新状态与精确提交阅读，不作为当前功能回退声明。

## 本轮完整链路与自动身份绑定

8d3b8df的完整普通用户CLI/HTTP→root VPN IPC→隔离会话已在临时Ubuntu CI实际通过（运行38038087926，1.68秒）；包含真实业务TLS、互锁、控制进程SIGKILL、根执行器退出、重启及凭据轮换。端点与CA为私有夹具，不是公共VPN证据；VPS两次命令传输中断，实际状态另外核对，不混淆两种环境。

官方公开TCP节点的标准TLS实测发现：SAN覆盖列表的vpn数字.opengw.net，但CN是opengw.net且节点可能共享证书。已接默认自动TLS身份检查、限频短缓存和精确叶证书指纹绑定，见[身份流程](VPN_SERVER_IDENTITY.md)。后续正式CLI最多两公共节点验收仍待执行结果，已发布dev.6资产不变。

## dev.8：独立OpenVPN执行器与Web接线

新增无宿主路由变更的固定传输FD→匿名namespace方案；OpenVPN与worker分用非rootUID、单一网络能力与no_new_privs。正式root服务使用独立Unix路径，保留原sandbox-only协议边界。已接schema v5加密VPN快照与服务器指纹、API、界面确认选择和gateway工厂；需要明确`--vpn-executor`，保存不联网，活动状态来自实际TLS/DNS/出口核验。

8cf9f54的VPS首测通过UDP生产会话但TCP的SOCKS编码适配失败；宿主/残留核验通过。已修正数字IP域名编码及Linux父线程生命周期，修正后1f9c278已在目标VPS通过TCP/UDP生产会话及旧AEAD/CBC用例，10.190秒、scope峰值35.00MiB，宿主不变；见[会话验收记录](validation/2026-10-10-openvpn-session.md)。完整Web/IPC/公共节点验收尚未完成，不能用首测UDP结果代替；详见 [OPENVPN_RUNTIME.md](OPENVPN_RUNTIME.md)。旧dev.6 Release不替换。

## dev.7：真实VPN Gate格式兼容与节点检查

官方当前100条配置均为AES-128-CBC/HMAC-SHA1；3个样本使用强ISRG服务器CA和精确固定的公开共享客户端身份，旧严格解析器无法支持。已新增显式兼容选项、预期服务器名称必需、共享身份指纹例外和只读节点配置检查；不放宽默认AEAD、TLS服务器验证或脚本/压缩限制。详见 [兼容政策](VPNGATE_COMPATIBILITY.md)。三个官方缓存样本的静态识别，以及AEAD/CBC两条原生OpenVPN/TLS/DNS/断线旅程已在目标VPS通过；单核/384MiB，8.691秒、cgroup峰值28.12MiB，宿主不变。详见[实测证据](validation/2026-10-10-vpngate-compatibility.md)。旧共享身份仍未用于公共服务器握手，公共VPN连接和生产worker未完成，dev.6资产不替换。

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

使用说明见 [LINUX_SANDBOX.md](LINUX_SANDBOX.md)，完整网络契约见 [NETWORK_EXECUTOR.md](NETWORK_EXECUTOR.md)。升级前备份旧数据目录；schema v5不能由旧schema4及以前程序直接降级打开，备份需包括配套.key密钥。
