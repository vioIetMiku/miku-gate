# 2026-10-11：dev.11发行包安装与配套升级/回滚验收

## 精确来源

发行与安装源 `177aa6e8688ec664d69f7995bd6095e36e1abadf`。installation-lab `38114561022`最终success，Linux amd64 Ubuntu可丢弃runner：有VPN `114396773179`、无VPN `114396773245`都success。同源verify `38114558358`success，包括前端检查、Python负例、Go race/vet、HTTP/CLI与Chromium既有旅程。

[机器摘要](evidence/2026-10-11-dev11-installed-upgrade-release.json)含精确源、作业/断言与四个发行资产摘要，不含初始化密码/私钥/归档内容。真实初始化日志root私有，未下载/上传/回显；测试归档也不作为artifact。

## 实际执行而非仅命令下发

1. 新版BUILD清单入口运行真实plan-only与apply。创建专用账号、IP SAN证书、副本/安装登记、固定systemd服务；健康检查验证TLS叶摘要/Host。账号预算与普通UID来自安装结果。
2. 两个固定官方核心实际下载/校验、以安装普通账号运行native生命周期；不是另取示例核心，也没有因此控制旧面板。
3. 用真实HTTPS/安全会话登录，创建逻辑出口并保存真实加密上游材料。明确停机后创建root600配套DB/key/证书/程序/协调器/安装单元归档，逐项读回摘要，重启原活动服务后原材料仍可读取。
4. 升级到同源码/同版本、不同strip构建的程序（实际程序SHA256变化），保存完整更改前快照；普通UID版本/数据检查、真实HTTPS/VPN能力验收通过。
5. 升级后改变这条加密上游的用户名/密码，确认元数据确实不同；以原私有归档明确SHA256回滚，恢复原程序摘要、同实例、同配套key和原加密上游数据，再次通过真实HTTPS检查。不是只重启旧程序而数据仍未恢复。
6. 最终无事务日志/维护启动保护遗留；真实fixture安全删除。两job均发出restore_verified=true、upgrade_verified=true、paired_encrypted_db_key_verified=true。

## 发布

`v0.1.0-dev.11`预览发布，标签固定同源；Linux amd64/arm64和Windows amd64三平台压缩包与SHA256SUMS上传后逐项API读回大小/摘要。已有dev.10与其他旧资产不替换。

Linux amd64压缩包SHA256：`39651c6e92f44eea973ce24fb20712f34ae323df26154351b603d175dadc1563`；约11.7MB。发行包带界面、BUILD清单、安装/维护脚本和相关文档；无需Go/Node。核心另行固定版本下载。包完整性不等于所有平台原生安装已执行：arm64与Windows本批只构建/打包，不套用amd64的系统安装证据。

## 未被本次证明的范围

这是同实例、schema6、同源码不同构建的真实替换/配套回滚，不是跨版本schema迁移证明。故障回滚、事务中断保护有Python逻辑负例；真实kill/断电中断恢复尚未专项执行。归档本身保留其创建时restore_verified=false，实际恢复证据独立记录。

未证明用户VPS永久安装/已有服务整机快照、互联网外部浏览器/客户端可达、公共域名ACME续期、systemd内实际业务VPN长期会话、面板持久同步、地理/ASN/信誉及卸载。产品目标和Issue继续开放，不把一个成功矩阵当完整项目完成。

## 避免重复检查

正常verify只在main推送与PR执行，文档-only更新不重跑，发行标签不再重复跑同源整套浏览器/Go检查。本版标签触发的重复任务若仍活跃，在核对同源main成功后可取消；不是取消首次必需安装/验证。后续功能按批次必要检查，不重放已经通过的公开VPN/VPS实验。
