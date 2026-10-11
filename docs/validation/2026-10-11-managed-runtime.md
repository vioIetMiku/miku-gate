# 2026-10-11：受管核心运行与公共完整路径验收进展

## 普通用户原生运行器实际通过

源提交`5ee75c4f404cabef36a02a0c33f5344f514952c9`，实验`38103913654`（job114365293877）、verify`38103911183`成功。既有双核心、六模板继续通过；新增`TestLinuxManagedNativeRuntime`0.45秒（Xray0.22、sing-box0.24），实际生产运行器check、stdin内联证书、非root/no_new_privs、PID的socket归属、TLS握手、修改互锁与出口租约撤销核心/监听。不是用户面板复用或公共全路径。

后续`533f9d7`修复状态查询与进程发布并发锁，增加并发快照回归；最新`a837ecf`verify`38106637125`成功，真实核心实验`38106812839`已启动，最终结果需实际回读。常规CI包含Linux race、浏览器、63项HTTP/CLI和20项Python。Windows race因当前CGO关闭未运行，不把非race单元结果冒充race证据。

## 公共链路：Xray已通过，sing-box仍待验证

源提交`a837ecf13bcf43748e270559a577a42680637f20`，二进制SHA256`dd62f22dac8b3430d51f77e190fa5c43ea5952feb3edc54db5f7c870e7d1b302`。授权VPS全新独立数据/普通UID控制和核心，根VPN执行器及不同UID的OpenVPN/worker。控制/API/gateway/VPN工厂均实际生产实现，没有替换。

Xray VLESS+REALITY+Vision完整路径实际成功：

1. 官方缓存最多2个候选，服务器TLS系统根/SAN/CN与精确证书指纹验证后绑定VPN快照；未关闭验证。
2. 从API加密保存入口、显示仅客户端材料，重启真实控制服务后客户端URI保持不变，独立根VPN执行器不替换。
3. 启动实际隔离VPN、证明内核/DNS/公网IPv4，再直接本机认证VPN代理验证业务HTTPS基线。
4. 显式信任独立固定官方Xray程序，用真实原生运行器启动保存入口，核验PID监听。
5. 生成Xray客户端仅改本机SOCKS监听端口/认证，不改VLESS/REALITY身份或TLS验证；真实客户端→REALITY入口→Miku认证SOCKS→隔离VPN→example.com HTTPS成功。
6. 同加密业务路径访问公网IP检测，IP与VPN运行时proof及官方所选节点的hash一致。
7. 运行中入口修改409拒绝；停止逻辑出口撤销核心/监听，runtime回stopped。客户端与测试进程均关闭。

sing-box该次保存、重启身份核对、隔离VPN基线、原生服务端核心启动通过，但生成的测试客户端在监听准备阶段退出，没有进入业务握手。整体验收明确false，不用Xray成功掩盖它；继续定位。总23.388秒、scope峰值200093696 bytes，单核/384MiB/零scope swap/64tasks实际读回，无资源事件/遗留子进程；主机快照完全一致，原面板与服务不变。入口/客户端都只监听loopback，不是已开放公网端口的远端客户端验收。

[原始结果机器摘要与失败历史](evidence/2026-10-11-encrypted-public-attempts.json)。明文IP、UUID、代理密码、私钥和配置不发布。

## 保留的失败与推论边界

- 首次包装器CRLF在shell第一行拒绝，没有Python/测试执行；修正只改LF。
- e839b87首轮两个身份检查不通过，未启动VPN/加密入口；增传输端点静态摘要，最多20条本地缓存读取优先TCP，网络仍最多2节点，不扫描端口。
- afb5984至56d270b用Microsoft作REALITY目标，直接VPN业务基线通过但加密业务TLS重置；固定分类表明REALITY拒绝连接，不宣称全路径成功。各次主机不变并精确清理。
- 换此前同VPS验证通过的Cloudflare目标，Xray完整路径通过。标准TLS1.3/X25519/h2适配并不是REALITY所有核心与目标组合必然可用的证明，不能把十候选标成绝对可用。

目前没有完整两核心公共验收、生产安装器、管理HTTPS、专用核心账号/预算配置、系统升级回滚。保存运行功能推进不代表完整目标完成。下一步是修复sing-box测试客户端，并复测两核心；之后发布新的运行里程碑而非覆盖旧Release。
