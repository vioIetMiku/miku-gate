# 新建受管入口：配置生成与安全素材（dev.9开发）

## 当前用户流程

控制台 → 系统与诊断 → 新建受管入口：选择Xray或sing-box、模板、VPS IP或域名、端口。TLS填写自己的证书域名并明确同意自动查找；REALITY选十个候选之一或自填域名，明确同意单目标443 TLS探测。点击预览只检查参数、说明计划，不生成密钥、不启动/开放端口、不保存绑定。

模板包括VLESS+TCP+TLS+Vision、VLESS+TCP+REALITY+Vision、Trojan+TCP+TLS。客户端连接地址可以是VPS IP，但TLS仍要求证书覆盖的真实域名SNI。管理界面HTTPS是另一个交付项，不因为生成入口TLS而自动公开管理控制台。

## 本机生成真正的私有配置

`miku-gate ingress templates`列出三种模板，`reality-candidates`列出十个待检测目标。`certificate --domain=自己的域名 --confirm-read`输出脱敏证书摘要；`reality-check --domain=目标域名 --confirm-network`输出当前VPS端点TLS检测结果。

`ingress managed --bundle-dir=新目录绝对路径 --confirm-create`从stdin严格JSON接收`request`和`proxy`（认证材料不放参数）：

- request：core、template、address、listen、port、server_name，以及REALITY时的reality_hostname。
- proxy：port、username、password；地址固定127.0.0.1。不要在shell历史中直接粘贴真实密码，使用受保护文件stdin或安装助手内存传递。
- TLS还需`--confirm-read`，自动发现证书；REALITY还需`--confirm-network`，生成时重新检查目标，不信任浏览器“检测通过”字段。

目录父级必须当前账号所有0700，bundle必须全新；dirfd逐级拒绝符号链接，新目录0700、各文件0600，独占创建/同步，不覆盖已有文件。输出stdout只有安全计划/摘要，不含服务端私钥、Miku代理密码、URI或客户端秘密。

bundle包含server.json、client.json、client-uri.txt和BUNDLE.json；TLS另有复制并核验的certificate.pem/private.key，服务端配置自动引用它们。入口UUID/Trojan密码、REALITY X25519私钥和8字节shortID均随机生成。客户端只含所需入口凭据和REALITY公钥，不包含服务器私钥或Miku代理密码。

**此阶段只生成，不部署常驻服务。** 需要后续核心check、专用账号、配置持久保存、端口/防火墙确认与systemd启停。不能把创建目录当作入口已连接。生成后的客户端URI/config有使用权限，仍是敏感材料。

## 证书自动发现和验证

只允许域名绑定的常见Let's Encrypt live/archive、root acme.sh RSA/ECC路径，拒绝任意文件API；标准live符号链接只能落在同一域名archive中，再用dirfd安全读取实际文件。路径/文件root所有、无组/其他写；私钥无组/其他读，普通独立文件、128KiB上限。

必须通过SAN主机名、有效期至少24小时、公私钥匹配、服务器用途、系统根信任链、最低公钥强度与非SHA1等检查。无证书/权限不足/验证失败是未知或不可用，不伪造成功。不申请新证书、不改变宿主根信任、不会显示私钥。无域名证书时可选REALITY或等待安装器证书签发流程，不能自动改成allowInsecure。

当前bundle是证书快照，后续必须接续期原子替换/重载；不能宣称复制一次就已完成自动续期。正常root私有证书无法由普通控制台读取，安装助手后续需授权读取并传回摘要/受管副本，不把私钥复制到前端。

## REALITY目标检测的边界

十个候选只是便捷清单，可用性不保证，生成时从VPS再测。IDNA规范化、仅域名（不接受URL/IP/用户指定端口），解析最多16个IPv4，任何私网/保留/元数据地址会拒绝；只拨一个已验证数字IP的443端点，系统根/SAN验证TLS1.3、X25519-only、h2，5秒预算，无insecure或端口轮询。

这是主机网络的入口握手引导探测，不是VPN业务出口DNS/地理/纯净度检测。目标证书可用、h2握手等不证明长期稳定、运营者归属或不可区分性；结果有效10分钟，不允许浏览器伪造可信记录。REALITY服务端目标固化为测得IP和SNI，后续IP变化需重新验证/更新，不能无声随DNS切私网。

## 防止出口失败后直连

生成核心只有认证Miku SOCKS出口和阻断路径，不提供freedom/direct后备。选定TCP路由Miku、UDP显式drop/blackhole；其余流量最终阻断。生成客户端不禁用TLS验证。业务DNS域名保持交给Miku前端，不为业务添加宿主解析后备。

## 测试与未完成项

单元/API测试覆盖参数、证书域名/信任/有效期/钥匙匹配、秘密不序列化、客户端导入、REALITY可信状态/过期、固定目标、明确读取/联网确认、缓存限频、bundle独占权限。真实核心手动实验新增两核心×三模板的服务端/客户端check和握手，UDP阻断、Miku停止不直连；精确实验结果单独记录，不用新增测试冒充已运行。

仍需：生产常驻启停、凭据/配置加密保存、已有面板持久复用、核心切换/升级/回滚、证书申请/续期、REALITY重新验证策略、完整加密入口→公共VPN整链路、管理域名/IP HTTPS、最终交互快速安装。完整交付清单没有缩减。
