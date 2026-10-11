# 保留数据的服务卸载与恢复注册（dev.13开发）

本命令只卸载Miku登记的systemd服务：数据库/配套key、证书、程序、核心与专用账号保留，不卸载OpenVPN等共享系统包，不删除原面板，不修改防火墙/宿主网络。是明确的“服务卸载”，不是全盘擦除或账户清理。

```sh
sudo install -d -m 700 /var/backups/miku-gate-private
sudo python3 -B /usr/local/lib/miku-gate/miku-maintain.py uninstall-services \
  --output=/var/backups/miku-gate-private/pre-uninstall-001.tar.gz \
  --confirm-stop --confirm-uninstall-services
```

先核对root安装登记/程序/账号/数据与确切服务模板；有外部修改/drop-in拒绝接管。保留原active/enabled集合，停止未来证书触发，持有数据租约后保存新私有整套DB/key/证书/程序/units快照，写持久事务日志，再disable并仅删除已验证的确切unit普通文件。不递归删目录，不使用通配符；失败尝试恢复原注册/服务，无法确认时保留恢复日志。

状态命令报告services_uninstalled；卸载后原首次安装器仍拒绝覆盖保留数据。如需恢复注册：

```sh
sudo python3 -B /usr/local/lib/miku-gate/miku-maintain.py reinstall-services \
  --confirm-reinstall-services
```

核对同安装身份、名称未被其他服务占用、模板通过systemd-analyze verify后重新注册；不会自动启用开机或启动。管理员核对后再明确systemctl enable/start所需Miku单元。新同名单元有不同内容时拒绝覆盖。数据库/key/证书身份不变。

新增拒绝未确认/精确unit范围用例，不宣称已在用户VPS执行真实卸载/恢复旅程；目前从代码补齐运维入口，下一安装验收需覆盖该新操作。不提供删除数据/密钥命令，以免仓促破坏恢复资料。
