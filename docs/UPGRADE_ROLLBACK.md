# 升级、同安装配套回滚与中断恢复

> dev.11开发功能。当前只支持已登记的同机、同账号/实例、同管理地址、同核心选择与schema6；不迁移3x-ui，不做跨主机恢复，不支持用旧dev.10程序单独降级。Linux amd64可丢弃安装实际升级/配套回滚已通过，见[独立验收](validation/2026-10-11-dev11-installed-upgrade.md)。不代表用户VPS永久部署或断电恢复完成。会停止当前连接，不承诺升级后自动恢复业务出口会话。

## 使用新版发行包升级

先核对已认证发行归档SHA256，再在root拥有且普通用户不可替换的目录解压。旧安装和新包均须含本版维护工具与 `data-check` 命令。以下发行包路径是示例，替换为你实际解压的绝对目录：

```sh
sudo install -d -m 700 /var/backups/miku-gate-private
sudo python3 -B /usr/local/lib/miku-gate/miku-maintain.py upgrade \
  --distribution=/root/releases/miku-gate-v0.1.0-dev.11-linux-amd64 \
  --output=/var/backups/miku-gate-private/pre-upgrade-001.tar.gz \
  --confirm-stop --confirm-upgrade
```

程序摘要未变化时拒绝重复升级。确认后验证新包平台/程序/脚本，检查旧安装身份、核心来源与全部受保护数据可解密；仅停止Miku已登记单元，记录原先活动集合，持有维护锁和原数据租约，生成新私有配套备份及明确归档摘要。备份失败时不应用程序更改。

候选程序以普通控制UID验证版本，不作为root运行。安装程序和维护脚本按固定目的地、同目录FD写入/同步/原子替换，最后更新安装登记。已有核心不自动下载/替换，未知systemd单元修改拒绝。检查真实加密数据、重启原活动服务并验证管理TLS指纹/Host健康以及VPN身份。只发出start命令不算完成。

发生异常会停服务，恢复整个更改前的程序、维护工具、DB/WAL/SHM、配套.key、证书副本、安装登记和固定服务模板。失败后若原服务恢复验证通过，仍以失败退出并明确 `MIKU_CHANGE_FAILED_PAIRED_ROLLBACK_VERIFIED`，不假称升级成功。

## 回滚到一个同安装快照

先取得备份命令输出的 `archive_sha256`，或通过可信本地通道核对归档摘要。输出的新快照用来保留回滚前状态，也不能覆盖已有文件。下面的 `<snapshot_sha256>` 必须替换为实际64位十六进制摘要：

```sh
sudo python3 -B /usr/local/lib/miku-gate/miku-maintain.py rollback \
  --archive=/var/backups/miku-gate-private/pre-upgrade-001.tar.gz \
  --archive-sha256=<snapshot_sha256> \
  --output=/var/backups/miku-gate-private/pre-rollback-001.tar.gz \
  --confirm-stop --confirm-rollback
```

需要root600归档/root700父目录。先完整校验再停服务：成员名、类型、数量、大小、摘要、固定UID/GID/权限、实例、管理地址、核心选择、完整DB/key与维护程序；压缩/解压均有上限。只读取归档普通成员并映射预设路径，不调用extractall，不信任归档路径/权限/root服务命令。备份中固定单元只接受已知安装模板。数据库租约inode不替换；只清理目标快照未携带的确切WAL/SHM/key，不通配删目录。

回滚恢复的是整个快照时点，后续新增配置、事件和会话会回到旧时点；当前连接会停止。先备份再确认，不将它当配置撤销按钮。快照声明 `restore_verified=false` 仍保留其创建时事实；一次实际回滚结果单独给出验证，不篡改旧归档。

## 断电/进程中断后的恢复

维护事务在root600 `maintenance-transaction.json`记录原状态、原活动单元与精确快照路径/摘要；新单元带 `ConditionPathExists=!/etc/miku-gate/maintenance-block`。处于实际替换阶段时有保护文件，重启不能运行部分更新状态。遗留事务会拒绝另一次升级/备份。

```sh
sudo python3 -B /usr/local/lib/miku-gate/miku-maintain.py status
sudo python3 -B /usr/local/lib/miku-gate/miku-maintain.py recover \
  --confirm-stop --confirm-recover
```

`recover`只使用现有root私有日志，不接收任意恢复目标。校验快照后恢复整套旧状态，再验证原服务；失败保留日志/备份和启动保护并停止Miku服务。若安装协调器本身已损坏，从核对过的新版发行包运行同名维护脚本，不能删除日志/数据库key强行继续。不会改原面板、宿主路由或防火墙。

## 数据配套只读检查

维护协调器自动以专用普通UID调用 `miku-gate data-check --data=/var/lib/miku-gate/data`。它不迁移schema、不创建数据库/key、不启动网络；检查schema6、SQLite完整性和实例，对每条上游、VPN、受管入口材料执行现有加密认证与配置解码，输出只有数量/布尔值/实例。key错配、密文损坏、记录串实例和未知schema都会失败。Linux不允许直接以root调用，避免制造root所有WAL/SHM。

## 证据与仍未完成项

- Python回归覆盖未知成员/链接/缺key/错实例/摘要漂移/任意root命令拒绝、租约inode保留、失败回滚和中断恢复失败保留保护。
- Go用例覆盖真实加密上游与错key拒绝、无记录不创建key、未知schema不迁移、不创建缺失数据目录。
- 现有可丢弃安装任务新增同源码不同构建程序升级，修改一条真实加密配置后回滚原快照，再通过真实HTTPS检查原数据/程序/钥匙与日志清理。源177aa6e两模式已实际通过；同源码不同构建，不代表未知schema迁移通过。
- 暂不支持未来schema迁移、跨账号/地址/核心变更、重新建机灾备、无维护工具旧安装自动接管、未运行安装升级。证书续期、保留数据卸载和面板持久协同继续开发。
