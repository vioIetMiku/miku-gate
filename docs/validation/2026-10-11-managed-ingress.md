# 2026-10-11：受管模板、真实目标与私有bundle验收

## 授权VPS的正式CLI结果

精确源码 `77242eb4fa05e3a1a4fae66e417a7d0bc848f144`，Linux二进制 SHA256 `7460e1585f5762ab3b2c8b0df8e0eb3c00955635ab3d3709fdb7bac6ea9232bf`，干净源构建。普通UID65534运行实际CLI，未安装或启动Xray/sing-box，没有用root运行控制服务。

- 明确仅两个公开域名：www.microsoft.com、www.cloudflare.com，各一次固定443标准TLS探测；实际系统根/SAN验证，TLS1.3、X25519-only、h2均通过。域名解析IP仅公布hash，完整输出留在root私有证据目录。
- 使用一个通过的目标分别生成Xray/sing-box REALITY模板bundle，生成时重新探测目标。连接/配置VPS地址为文档测试IP，未使用已存在面板入口。
- 实际新bundle目录0700、文件0600，随机入口与代理凭据不传stdout；client.json/URI存在，server/client/metadata间秘密隔离通过。
- 同目标重复生成拒绝覆盖原bundle，原server hash保持一致。没有启动核心，也没有把元数据标成core_validated/applied。
- 本次测试已按精确文件/归属清理，新stage为空，宿主接口/路由/防火墙/DNS/监听与进程身份/挂载快照前后一致。

资源由一次性scope配置单核128MiB/零swap/32tasks/每命令15秒，此包装器未另读回cgroup资源峰值，所以不宣称独立资源验收。仅bundle生成/目标TLS适配，不是公共REALITY入站业务连接或“入口→VPN”完整数据面证据。

[机器摘要](evidence/2026-10-11-managed-ingress-vps.json)。已公布摘要不含私钥、UUID、代理密码、完整原始配置或明文服务器IP。

## 真实双核心模板实验与保留的失败

`77242eb`手动实验 `38101599248`（job114358470470）：既有双入口路径通过，新建的两核心×三模板中5组合通过，Xray REALITY未到Miku认证前端，因此实验整体失败。通过的是Xray TLS/Vision、Trojan/TLS，以及sing-box TLS/Vision、REALITY/Vision、Trojan/TLS。

后续`8cbae90`、`f88fd04`、`b8753e7`、`27fad69`、`8c54ebf`分别补固定分类、完整目标证书链、诊断词汇/脱敏句和有界客户端等待，仍保留Xray REALITY失败，不把单元/核心check/五种通过外推为六种全通过。私有目标原3秒截止与官方REALITY冷启动记录探测5秒不一致，`a300500`将私有目标寿命改为15秒并启用夹具范围info级脱敏诊断；其精确运行结果需要实际回读。

没有修改产品证书/REALITY验证、替换成insecure或跳过失败子用例。所有实验仍隔离loopback，真实官方固定Xray26.3.27和sing-box1.14.3，真实生成server/client、认证Miku proxy.Server、受限私有业务Dial。不是用户面板应用或公共VPN业务旅程。

常规本机Go/vet、证书/模板/API单元、59项真实HTTP/CLI、Chrome选择/预览/移动布局通过。生产常驻启停、配置/凭据持久化、证书续期、安装/升级/回滚和管理域名/IP HTTPS仍未完成。

## 最终六组合实际通过

精确源码 `a300500565c80129cf33be1ffb54f490a8ee182a`：手动实验`38102651417`、job`114361602726`成功，同提交verify`38102648859`也成功。新增受管模板总13.95秒，六个子用例无SKIP：Xray TLS/Vision1.78、REALITY/Vision6.78、Trojan2.78；sing-box三个各0.87秒。既有入口双核心实验2.75秒继续通过。

Xray REALITY实际生成client/server的核心check、X25519密钥/shortID握手、未解析业务域名经Miku真实认证SOCKS、UDP不旁路及Miku停止不旧直连全部通过。修正的是私有目标3秒截止与核心冷启动5秒记录预检竞态，目标保留15秒；客户端有界8秒等待。完整证书链和脱敏诊断保留，产品安全设置没放宽。

新生成的6组合不安装到VPS，没有修改已存在面板；此实验使用私有CA/loopback/固定业务Dial，不是加密入口→公共VPN整链路证据。最终常驻/安装等仍待接线。先前失败运行均保留，用于解释修正来源，不删除失败日志冒充首轮全通过。
