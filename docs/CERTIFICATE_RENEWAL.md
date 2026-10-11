# 域名证书续期与同身份入口更新（dev.12）

> 此功能在dev.12开发源码，已发布dev.11包不包含。仅域名、受系统信任的证书；不会自动更换无域名用户已导入的IP引导证书。换证时短暂停止Miku服务和连接，更新完成后管理服务恢复；业务出口/入口活动会话目前不自动重新启动。

## 首次安装时授权

域名证书选择后询问是否每12小时检查并更新已验证副本，默认yes但必须经过安装确认。如果证书来自Certbot，还单独询问是否授权仅此域名renew：自行复用的Certbot证书默认不授权，仅同步原工具已经续期的结果；本安装器申请的证书可明确授权续期。acme.sh等工具保持它们的既有续期流程，Miku只读取核对过的副本。拒绝启动服务时定时器也不会启动。

固定systemd服务只运行本机已登记维护脚本，无远程root脚本、无任意浏览器路径。定时器每天00/12时段加0–30分钟随机延迟、Persistent=yes；先检查真实证书变化，未变化不备份、不重启。

## 已有安装开启或关闭

先升级到包含此功能的固定校验包，再执行：

```sh
sudo python3 -B /usr/local/lib/miku-gate/miku-maintain.py certificate-automation \
  --confirm-certificate-automation
```

默认只同步已有证书工具的续期结果。如需Miku调用此域名Certbot，再明确添加 `--renew`。它会使用root已有的renewal配置和hook，此授权不应授予不可信配置。当前主动续期为保守standalone方案：证书临近30天、80端口空闲才尝试；已有webroot/DNS来源可自行续期后同步，不停止Nginx/Caddy或已有面板来抢端口。如果原工具已换好证书，即便80端口占用也能安全同步副本。

```sh
sudo python3 -B /usr/local/lib/miku-gate/miku-maintain.py certificate-automation \
  --confirm-certificate-automation --disable-certificate-automation
```

单元内容不匹配时拒绝接管；关闭只停定时器，不停止管理服务、不删除原ACME证书。

## 手动检查和应用

```sh
sudo install -d -m 700 /var/backups/miku-gate-private
sudo python3 -B /usr/local/lib/miku-gate/miku-maintain.py certificate-refresh \
  --output=/var/backups/miku-gate-private/pre-certificate-001.tar.gz \
  --confirm-stop --confirm-certificate-refresh
```

来源固定为后端发现的Let’s Encrypt/acme.sh根受保护路径；验证域名SAN、系统信任链、有效期、公私钥、文件/父路径权限与摘要，不允许HTTP提供私钥或任意路径。手动增加 `--renew` 也必须已有对应授权，不隐式安装Certbot。

1. 新证书真正改变时先记录原活动服务、明确停机，保存整套DB/key/证书/程序/固定单元一致快照，写持久事务日志和启动保护。
2. 受保护管理目录里写入新副本；用专用普通账号真实 `management refresh-ingress` 命令更新加密入口，自己持有原数据租约。只更新同域名、同旧叶指纹的TLS入口；不同证书绑定和REALITY不动。
3. 保留UUID/Trojan密码/代理凭据/端口/出口绑定，不旋转客户端身份。密文重封装在单个事务；私钥不进入API响应、事件或日志。
4. 检查全部加密材料配套，保存新证书元数据，再验证真实管理HTTPS叶指纹/Host和VPN能力。失败恢复完整旧快照；不能验证恢复时保留日志/启动保护，明确recover。
5. 自动快照root700目录、root600归档，最多8份；满额提示人工明确归档/清理，不默默删除敏感备份或无限占磁盘。

## 当前验证边界

快速回归覆盖授权拒绝、IP身份拒绝、80冲突不动原服务、证书不变无停机、定时单元固定命令。新Linux普通UID实际CLI用例覆盖TLS入口加密保存、证书轮换、UUID/代理绑定/客户端导入保持、换证后真实HTTPS；仅对子进程注入私有CA，不放宽生产根信任。源e20a777的certificate-lab38116179316/job114401409204已实际通过普通UID CLI换证、持久身份与真实HTTPS/客户端导入；它不是公网ACME验证。相同提交verify38116177520失败于非rootPython夹具意外访问不可读/root来源，已修正授权先拒绝/不可读来源跳过，失败记录保留。

公共域名真实ACME签发/续期、root定时协调器完整故障演练、换证期间业务会话无损恢复，以及任意其他域名入口的自动资产管理仍需继续交付。公开Release说明本版边界，不能借旧dev.11成功结果假称dev.12已完成。
