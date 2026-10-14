#!/usr/bin/env python3
"""Small root menu for Miku fanout; password changes need no old login."""
import getpass
import hashlib
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import ssl
import sys
import tarfile
import tempfile
import common as c

def restart():c.service('restart');c.wait_ready()

def password():
    new=getpass.getpass('新口令（留空随机生成）：').strip()
    c.require(not new or 8<=len(new)<=128,'口令需8至128个字符')
    if new:c.require(getpass.getpass('再输入一次：').strip()==new,'两次口令不一致')
    new=new or secrets.token_urlsafe(24)
    c.require(not any(char in new for char in '\r\n\0'),'口令不能包含换行')
    with c.maintenance_lock():
        previous=c.trusted(c.DATA/'password',0o600,1024);c.atomic(c.DATA/'password',(new+'\n').encode())
        try:restart()
        except BaseException:c.atomic(c.DATA/'password',previous);c.service('restart');raise
    print('口令已修改，原登录会话已失效。');c.info()

def port():
    value=c.settings();requested=int(input('新的管理端口（当前 '+str(value['port'])+'）：').strip())
    if requested==value['port']:return c.info()
    c.require(1024<=requested<=65535 and c.port_available(requested),'端口不合法或已被占用')
    with c.maintenance_lock():
        old=c.trusted(c.DATA/'settings.json',0o600);value['port']=requested;c.write_json(c.DATA/'settings.json',value)
        try:restart()
        except BaseException:c.atomic(c.DATA/'settings.json',old);c.service('restart');raise
    c.info()

def certificate_pair(domain,issue=True):
    root=Path('/etc/letsencrypt/live')/domain
    if not (root/'fullchain.pem').exists() or not (root/'privkey.pem').exists():
        c.require(issue,'未找到原签发证书')
        c.require(c.port_available(80),'申请证书需要80端口；已有服务占用时请先提供已签发证书')
        import socket
        addresses={v[4][0] for v in socket.getaddrinfo(domain,80,socket.AF_INET,socket.SOCK_STREAM)}
        c.require(c.settings().get('public_ip') in addresses,'域名A记录尚未指向VPS公网IP，请解析后再运行 miku domain')
        if not shutil.which('certbot',path=c.ENV['PATH']):c.packages(['certbot'])
        email=input('证书联系邮箱：').strip();c.require(re.fullmatch(r'[^\s@]+@[^\s@]+\.[^\s@]+',email),'邮箱格式无效')
        c.run(['certbot','certonly','--standalone','--preferred-challenges','http','--non-interactive','--agree-tos','--email',email,'--cert-name',domain,'-d',domain],timeout=240)
    cert=(root/'fullchain.pem').resolve(strict=True);key=(root/'privkey.pem').resolve(strict=True)
    expected=Path('/etc/letsencrypt/archive')/domain
    c.require(cert.parent==expected and key.parent==expected,'证书来源路径异常')
    cert_raw=c.trusted(cert,max_bytes=128<<10);key_raw=c.trusted(key,max_bytes=128<<10)
    context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);context.load_cert_chain(cert,key)
    c.run(['openssl','x509','-in',cert,'-noout','-checkhost',domain]);c.run(['openssl','x509','-in',cert,'-noout','-checkend','86400'])
    return cert_raw,key_raw

def refresh_native_tls(old_host,new_host,old_cert,new_cert,new_key):
    path=c.DATA/'native.json'
    if not path.exists():return 0
    value=json.loads(c.trusted(path,0o600,4<<20));count=0
    for inbound in value.get('inbounds',[]):
        tls=inbound.get('tls',{})
        if inbound.get('security')=='tls' and (tls.get('cert_file')==old_cert or tls.get('self_signed') or tls.get('server_name')==old_host):
            tls.update(server_name=new_host,cert_file=new_cert,key_file=new_key,self_signed=False);tls.pop('cert_sha256',None);count+=1
    if count:c.write_json(path,value)
    return count

def certificate_timer(enable=True):
    if not c.systemd():return
    service=b'''[Unit]\nDescription=Refresh Miku domain certificate\n[Service]\nType=oneshot\nExecStart=/usr/bin/env python3 -B /usr/local/lib/miku/maintain.py certificate-refresh\nUMask=0077\n'''
    timer=b'''[Unit]\nDescription=Daily Miku certificate renewal\n[Timer]\nOnCalendar=daily\nRandomizedDelaySec=1h\nPersistent=true\n[Install]\nWantedBy=timers.target\n'''
    for name,raw in (('miku-certificate.service',service),('miku-certificate.timer',timer)):
        target=Path('/etc/systemd/system')/name
        if target.exists():c.require(c.trusted(target,0o644)==raw,'已有同名证书服务被修改')
        if enable:c.atomic(target,raw,0o644)
    if enable:c.run(['systemctl','daemon-reload']);c.run(['systemctl','enable','--now','miku-certificate.timer'])
    elif Path('/etc/systemd/system/miku-certificate.timer').exists():c.run(['systemctl','disable','--now','miku-certificate.timer'])

def domain():
    value=c.settings();old_host=value.get('public_host') or value.get('public_ip','')
    print('当前地址：'+c.origin(value)+'\n域名可随时绑定、替换或解绑。')
    host=input('域名（留空取消，输入 none 解绑回HTTP+IP）：').strip().lower().rstrip('.')
    if not host:return
    c.require(host=='none' or c.valid_domain(host),'请只填域名，不带https://或端口')
    pair=certificate_pair(host) if host!='none' else None
    with c.maintenance_lock():
        backup=c.snapshot('before-domain');was_active=c.active()
        try:
            if was_active:c.service('stop')
            if host=='none':value['public_host']=value.get('public_ip','');value.pop('tls_cert',None);value.pop('tls_key',None);value.pop('certificate_domain',None)
            else:
                c.private_dir(c.DATA/'management');cert=c.DATA/'management/certificate.pem';key=c.DATA/'management/private.key'
                c.atomic(cert,pair[0]);c.atomic(key,pair[1]);refresh_native_tls(old_host,host,value.get('tls_cert',''),str(cert),str(key))
                value.update(public_host=host,tls_cert=str(cert),tls_key=str(key),certificate_domain=host)
            c.write_json(c.DATA/'settings.json',value);c.service('start');c.wait_ready();certificate_timer(host!='none')
        except BaseException:
            if c.active():c.service('stop')
            from install import restore_new
            restore_new(backup)
            if was_active:c.service('start')
            raise
    print('域名设置已生效；请在网页重新复制节点/订阅。');c.info()
    if not c.systemd() and host!='none':print('OpenRC：定期运行 miku certificate-refresh 更新证书。')

def certificate_refresh():
    value=c.settings();entries=[]
    path=c.DATA/'domains.json'
    if path.exists():
        entries=json.loads(c.trusted(path,0o600,1<<20));c.require(isinstance(entries,list),'域名登记异常')
    changed=[]
    for entry in entries:
        domain=entry.get('domain','');c.require(c.valid_domain(domain),'已登记域名无效')
        cert=c.DATA/'certificates'/domain/'certificate.pem';key=c.DATA/'certificates'/domain/'private.key'
        c.require(entry.get('cert_file')==str(cert) and entry.get('key_file')==str(key),'域名证书目标路径异常')
        source_cert=entry.get('source_cert');source_key=entry.get('source_key')
        if not source_cert or not source_key:continue
        if str(source_cert).startswith('/etc/letsencrypt/live/') and shutil.which('certbot',path=c.ENV['PATH']):c.run(['certbot','renew','--cert-name',domain,'--quiet'],timeout=240)
        source_cert=Path(source_cert).resolve(strict=True);source_key=Path(source_key).resolve(strict=True)
        cert_raw=c.trusted(source_cert,max_bytes=256<<10);key_raw=c.trusted(source_key,max_bytes=256<<10)
        context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER);context.load_cert_chain(source_cert,source_key)
        c.run(['openssl','x509','-in',source_cert,'-noout','-checkhost',domain]);c.run(['openssl','x509','-in',source_cert,'-noout','-checkend','3600'])
        if cert_raw!=c.trusted(cert,0o600) or key_raw!=c.trusted(key,0o600):changed.append((cert,key,cert_raw,key_raw))
    host=value.get('certificate_domain')
    if host and value.get('tls_cert') and not any(str(cert)==value['tls_cert'] for cert,_,_,_ in changed):
        expected=Path('/etc/letsencrypt/live')/host
        if expected.exists():
            if shutil.which('certbot',path=c.ENV['PATH']):c.run(['certbot','renew','--cert-name',host,'--quiet'],timeout=240)
            pair=certificate_pair(host,issue=False)
            if pair[0]!=c.trusted(Path(value['tls_cert']),0o600):changed.append((Path(value['tls_cert']),Path(value['tls_key']),pair[0],pair[1]))
    if not changed:return
    with c.maintenance_lock():
        backup=c.snapshot('before-certificate');was_active=c.active()
        try:
            if was_active:c.service('stop')
            for cert,key,cert_raw,key_raw in changed:c.atomic(cert,cert_raw);c.atomic(key,key_raw)
            if was_active:c.service('start');c.wait_ready()
        except BaseException:
            if c.active():c.service('stop')
            from install import restore_new
            restore_new(backup)
            if was_active:c.service('start')
            raise
    print('已刷新 '+str(len(changed))+' 组域名证书，入口凭据和端口保留。')

def certificate_prepare():
    if not shutil.which('certbot',path=c.ENV['PATH']):c.packages(['certbot'])


def repair():
    with c.maintenance_lock():c.dependencies();c.ensure_cores();restart()
    print('依赖和服务已修复。');c.info()

def backup():
    with c.maintenance_lock():
        was_active=c.active()
        try:
            if was_active:c.service('stop')
            target=c.snapshot('manual')
        finally:
            if was_active:c.service('start')
    print('备份：'+str(target))

def rollback():
    c.require(c.systemd(),'旧版恢复需要systemd')
    record=c.installed().get('legacy');c.require(record,'此安装没有旧版Miku配套备份')
    module,_=c.old_maintenance();c.require(module is not None,'旧维护程序缺失')
    with c.maintenance_lock():
        c.snapshot('before-legacy-rollback');certificate_timer(False)
        if c.active():c.service('stop')
        c.run(['systemctl','disable','miku.service'])
        from install import restore_old
        restore_old(module,record)
    print('已恢复切换前的旧版和 miku 菜单；新版数据保留在 /var/lib/miku。')

def update(filename):
    path=Path(filename).resolve(strict=True);c.trusted(path,max_bytes=160<<20)
    with tempfile.TemporaryDirectory(prefix='miku-update-') as temporary:
        root=Path(temporary);total=0;members=set();directory=None
        with tarfile.open(path,'r:gz') as archive:
            for item in archive:
                parts=Path(item.name).parts;c.require(parts and not item.name.startswith('/') and '..' not in parts and all(re.fullmatch(r'[A-Za-z0-9_.-]+',x) for x in parts),'安装包路径无效')
                if directory is None:directory=parts[0]
                c.require(parts[0]==directory and item.name not in members,'安装包重复或多根目录');members.add(item.name)
                c.require(len(members)<=120 and (item.isdir() or item.isfile()) and not item.issym() and not item.islnk() and 0<=item.size<=160<<20,'安装包类型/大小异常')
                total+=item.size;c.require(total<=320<<20,'安装包解压超限');target=root.joinpath(*parts)
                if item.isdir():target.mkdir(mode=0o700,parents=True,exist_ok=True)
                else:target.parent.mkdir(mode=0o700,parents=True,exist_ok=True);c.atomic(target,archive.extractfile(item).read(),0o600)
        c.require(directory,'安装包为空');directory,_=c.verify_distribution(root/directory)
        from install import install
        install(directory,update=True)

def logs():
    if c.systemd():os.execvp('journalctl',['journalctl','-u','miku.service','-n','100','--no-pager'])
    else:os.execvp('tail',['tail','-n','100','/var/log/miku.log'])

def uninstall():
    with c.maintenance_lock():
        target=c.snapshot('before-uninstall');certificate_timer(False)
        if c.active():c.service('stop')
        if c.systemd():c.run(['systemctl','disable','miku.service'])
        else:c.run(['rc-update','del','miku','default'])
    print('已停止并取消开机启动；数据、程序和备份保留。重新启动：miku start\n备份：'+str(target))

def menu():
    print('\nMiku '+c.installed()['version']+' · 基于 byJoey/fanout\n1 管理地址 / 查看口令\n2 修改口令\n3 域名：绑定 / 更换 / 解绑\n4 修复依赖（含OpenVPN）\n5 启动\n6 停止\n7 重启\n8 日志\n9 修改管理端口\n10 备份\n11 恢复切换前旧版\n12 停止并取消自启（保留数据）\n0 退出')
    choice=input('选择：').strip()
    commands={'1':c.info,'2':password,'3':domain,'4':repair,'5':lambda:(c.service('start'),c.wait_ready(),c.info()),'6':lambda:c.service('stop'),'7':restart,'8':logs,'9':port,'10':backup,'11':rollback,'12':uninstall}
    if choice in commands:commands[choice]()

def main():
    c.root();args=sys.argv[1:];command=args[0] if args else 'menu'
    commands={'menu':menu,'info':c.info,'status':lambda:print('运行中' if c.active() else '已停止'),'password':password,'passwd':password,'domain':domain,'port':port,'repair':repair,'backup':backup,'rollback':rollback,'logs':logs,'certificate-refresh':certificate_refresh,'certificate-prepare':certificate_prepare,'certificate-automation':certificate_timer,'start':lambda:(c.service('start'),c.wait_ready(),c.info()),'stop':lambda:c.service('stop'),'restart':restart,'uninstall':uninstall}
    if command=='update':c.require(len(args)==2,'用法：miku update /绝对路径/新版安装包.tar.gz');return update(args[1])
    c.require(command in commands and len(args)<=1,'可用命令：miku [info|password|domain|repair|start|stop|restart|logs|port|backup|rollback|update 安装包|uninstall]');commands[command]()

if __name__=='__main__':
    try:main()
    except (Exception,KeyboardInterrupt) as exc:print('操作未完成：'+str(exc),file=sys.stderr);sys.exit(1)
