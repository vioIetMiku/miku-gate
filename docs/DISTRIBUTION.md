# 使用新版发行包安装与维护

> 本说明对应dev.11起的新版包格式；当前dev.14已发布。dev.10已发布包不含此安装器；只有正式发布、带完整BUILD清单的新版压缩包可按下面无参数入口安装。同安装schema7配套升级/回滚、保留数据服务卸载/恢复注册已在干净Linux amd64实际通过；跨机灾备、完整用户VPS共存和未知未来schema迁移仍待完成。

## 在VPS安装

1. 从已认证的发行页取得匹配Linux amd64或arm64的压缩包和其SHA256；先核对整个归档。不要把包内BUILD摘要当成独立签名。
2. 在root拥有、不可由普通用户写入或替换的独立目录解压。不要从普通用户可写的Downloads目录直接sudo运行协调器。
3. 在包根目录运行：

```sh
sudo sh scripts/install-vps.sh --plan-only
sudo sh scripts/install-vps.sh
```

第一条只出计划、不安装包/账号/服务；缺python3时也不会暗中bootstrap。第二条保持逐项确认：仅缺失依赖、已有服务、VPS IP、域名或IP证书、管理端口、核心、最终应用。安装不会接管旧Miku目录，不停止原80/443服务、不自动改防火墙。

入口自动检查平台、二进制长度/SHA256以及三个安装维护脚本哈希。依赖缺失、未知包格式、缺维护脚本、摘要/架构不符都停止。源码/版本字段是来源记录；真正的外部信任来自已认证发行页的归档校验。无需下载任何新skill或插件。

IP引导证书不被浏览器默认信任，应通过已信任SSH安装输出核对指纹/导入信任，不能忽略TLS警告。域名证书来源与当前续期限制见[HTTPS安装说明](HTTPS_INSTALLATION.md)。

## 查看状态与私有备份

```sh
sudo python3 -B /usr/local/lib/miku-gate/miku-maintain.py status
sudo install -d -m 700 /var/backups/miku-gate-private
sudo python3 -B /usr/local/lib/miku-gate/miku-maintain.py backup \
  --output=/var/backups/miku-gate-private/first-snapshot.tar.gz \
  --confirm-stop --confirm-backup
```

备份明确停机，输出必须全新文件；默认重启之前活动的Miku服务并核对HTTPS/VPN身份。归档含数据库配套.key与TLS私钥，不上传GitHub。创建归档不等于已证明恢复；它不含全部核心二进制。同安装受控回滚需要明确归档摘要/停机/更改前快照，见[升级回滚说明](UPGRADE_ROLLBACK.md)和[备份边界](CONFIDENTIAL_BACKUP.md)。

## 构建固定源码发行包

在干净仓库先编译目标程序，再打包。例如Linux amd64：

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/miku-gate ./cmd/miku-gate
python3 -B scripts/package-dist.py --binary=bin/miku-gate \
  --version=v0.1.0-dev.14 --platform=linux-amd64 \
  --output-dir=.cache/release/dev14/assets
```

打包器读取Go嵌入的来源，要求vcs.revision等于当前HEAD、vcs.modified=false、目标平台匹配、CGO关闭。文档/协调器从Git确切提交取出；生成BUILD清单、固定权限压缩包与独立摘要JSON，对归档逐项读回，不执行任何归档程序、不下载latest、不覆盖既有输出。开发源码有未提交修改时拒绝。

Windows包是控制端/开发预览，不把Linux VPS安装服务声称为Windows能力。公开Release上传/标签动作另外明确执行，打包成功不代表已发布。
