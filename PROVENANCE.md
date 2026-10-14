# 源码来源

本版直接基于 [byJoey/fanout](https://github.com/byJoey/fanout)，原始提交：

`d7ce5224caf3469b19876982abdf8d1be16c4a8b`

原项目的 MIT LICENSE 和作者署名完整保留，原说明保存在 README.fanout.md。
Miku 对安装、认证 HTTP 代理、sing-box、S-UI 接入、按客户端选出口和域名维护作了修改。
界面沿用 fanout 单页流程；原项目链接和署名保留在源码说明与 MIT 许可证中。

S-UI 接入使用其公开本机 API 协议，适配代码独立编写；未打包 S-UI 程序。
安装时下载的 Xray 和 sing-box 来自各自官方发布，版本及 SHA256 固定在 CORE_ASSETS.json。
这些第三方程序适用其自身许可证。

compat/miku-export 包含此会话前一版 Miku 的数据导出代码，用于停机后按原普通账号读取配套数据库和密钥。
它不在公网提供服务。旧版快照先于兼容导出生成；恢复时使用原版维护工具核验快照和固定目标。

VPN Gate 滚动池协议来自 GeorgeXie2333/vpngate-list-mirror 的公开文档；本版自行实现读取、固定提交和 SHA256 校验，不打包或执行该项目的脚本。IP 信息按 IP-API、ipwhois.io 和 ipapi.is 官方接口文档实现。Miku 插画见 ARTWORK.md。

dev26 按 Ping0 官方 https://ping0.cc/ip/api 文档实现免费 `/geo` 出口定位和 ASN 查询。该接口通过每条 OpenVPN 隧道调用，不使用付费接口、不抓取网页内部接口。IP 属性来自其他已注明的来源。

dev27 将持续更新的开源镜像与近期TCP记录置于官方API之前，新增读取 root latest.json 固定提交CSV及SHA256校验，并采用项目文档公开的 Fastly/Gcore CDN入口。调研中的旧 Aimili 页面链接失效，main镜像元数据过期且字节哈希不一致，未作为默认来源；SSTP/VLESS有效库不作为OpenVPN配置导入。

dev28 独立实现严格国家重试、出口占用与入口对账、公开CSV确认添加及来源优先顺序、已有分享链接导入和订阅转换。Clash/Mihomo字段参考其官方配置文档（https://wiki.metacubex.one/en/config/proxies/）；sing-box字段参考官方outbound、TLS及V2Ray Transport文档（https://sing-box.sagernet.org/configuration/）。保留TLS/REALITY参数；不支持的传输返回错误，不执行第三方转换脚本或外传节点凭据。


dev29 修复停用入口显示、按客户端的出口关联和复制，增加 Miku 卡片展示、国家统计、双类型住宅规则与本机链接核验。类型字段依照 ipapi.is 官方 https://ipapi.is/developers.html 文档读取，住宅标签按用户指定的 ISP+ISP / BUS+ISP 规则推断；不代表来源对住宅属性的认证。


dev30 删除圈选的统计与筛选区域。IP 归属分类思路参考 OpenMili/aimili-vpngate 的 vpn_utils.py（https://github.com/OpenMili/aimili-vpngate/blob/main/vpn_utils.py）：移动 / 机房标记优先，普通运营商规则与代理标记分开，必要时第二来源复核。Go 检测、缓存、限流和浏览器显示代码独立实现，未复制或打包该项目代码。IP-API字段及限流参照 https://ip-api.com/docs/api:json，ipapi.is匿名 / 带Key短响应与字段参照 https://ipapi.is/developers.html。分类依据与置信程度明示。总延迟由当前浏览器HTTP RTT和VPS实测当前VPN Gate服务端ICMP RTT相加，不再使用节点列表的参考Ping；浏览器限制和代理路径开销见README。


dev31 独立实现 Miku 深蓝青绿界面、AS / 归属商 / 网络类型语义配色、全宽度两列卡片和按出口保留的详情展开状态。参考 frontend-design（https://github.com/anthropics/skills/blob/main/skills/frontend-design/SKILL.md）与 UI/UX Pro Max（https://github.com/nextlevelbuilder/ui-ux-pro-max-skill/blob/main/.claude/skills/ui-ux-pro-max/SKILL.md）的公开设计原则，未安装或打包其代码。原插画、网络查询来源及许可证保持原有说明。

dev32 独立实现 ICMP→配置 TCP→隧道 TCP 参考值的延迟兜底、有上限的并发健康检测，以及同国家候选预检、原 ASN / 地区偏好持久化和实际出口国家校验。OpenVPN `remote` 的地址、端口与协议字段依照官方手册 https://build.openvpn.net/man/openvpn-2.6/openvpn.8.html；TCP 建连使用 Go `net.Dialer` 的上下文和超时 https://pkg.go.dev/net#Dialer。隧道连接时间依照 curl 官方 `time_connect` / `time_namelookup` 字段 https://curl.se/docs/manpage.html，两者之差仍包含出口到检测站的开销，界面明确标为参考值。健康检测使用 https://www.ipify.org/ 与 Ping0 已公开免费 `/geo`。候选归属批量查询依照 IP-API 官方 https://ip-api.com/docs/api:batch，实现更小的批次、缓存及额度退避，不发送节点凭据或分享链接。

dev33 独立实现每出口十分钟滑动窗口的自动断线重连计数、状态持久化及温和稳定性提示，第 4 次触发、过期自动消失，手动操作与重连内部候选尝试不计数。延迟刷新统一为 30 秒限频，复用健康检查时间及浏览器同标签页的有效样本，不新增第三方服务或依赖。提示在管理列表及展示卡片内展示，延续原有两列布局和语义配色。

dev34 独立实现白天 / 夜间主题、浏览器偏好保存与跨标签页同步。登录页和工作台共用主题初始化与切换控件；白天模式覆盖卡片、弹窗、表单、类型标签和稳定性提示。使用原生 CSS、JavaScript、内联 SVG 图标及现有 Miku 插画，无新增第三方依赖。

## v0.2.0 正式发布整理

应用功能基于用户提供的 miku-gate-dev34.zip；保留原始 fanout 基线与 MIT 署名。正式发布只调整文档、版本元数据、在线引导安装器和构建发布流程，不重新设计用户已完成的应用优化。

新增 install-online.sh 从 GitHub 正式 Release 下载，经外部 SHA256 清单及内部 BUILD.json 校验后安全解包并调用现有安装器。支持私有仓库 Contents 读取令牌；跨主机重定向剥离 Authorization，实际安装子进程不继承令牌。应用及兼容导出程序均从源码重建 amd64 / arm64 版本。
