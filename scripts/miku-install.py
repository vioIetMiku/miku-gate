#!/usr/bin/env python3
'''Miku Gate explicit VPS installation coordinator (not an implicit updater).

Reads a local verified Miku binary; detects dependencies/services before asking
for changes. Supported apply target: Debian/Ubuntu with systemd, amd64/arm64.
Unknown existing Miku resources are never overwritten or automatically removed.
'''
import argparse
import hashlib
import io
import ipaddress
import json
import os
from pathlib import Path
import platform
try:
    import pwd
except ImportError:
    pwd=None  # Tests may inspect pure planning on Windows; apply is Linux-only.
import re
import shutil
import socket
import ssl
import stat
import subprocess
import sys
import tarfile
import urllib.parse
import urllib.request
import zipfile

ENV={"PATH":"/usr/sbin:/usr/bin:/sbin:/bin","LANG":"C.UTF-8","LC_ALL":"C.UTF-8","GOMAXPROCS":"1","GOMEMLIMIT":"160MiB","GOTELEMETRY":"off"}
USERS=("miku-gate","miku-openvpn","miku-worker")
LIB=Path('/usr/local/lib/miku-gate')
ETC=Path('/etc/miku-gate')
VAR=Path('/var/lib/miku-gate')
UNITS=Path('/etc/systemd/system')
CORE_ASSETS={
 ('xray','amd64'):('XTLS/Xray-core','v26.3.27','Xray-linux-64.zip','23cd9af937744d97776ee35ecad4972cf4b2109d1e0fe6be9930467608f7c8ae',21136402,'xray'),
 ('xray','arm64'):('XTLS/Xray-core','v26.3.27','Xray-linux-arm64-v8a.zip','4d30283ae614e3057f730f67cd088a42be6fdf91f8639d82cb69e48cde80413c',19716427,'xray'),
 ('sing-box','amd64'):('SagerNet/sing-box','v1.14.3','sing-box-1.14.3-linux-amd64.tar.gz','e2bdf179c15a3652955dc44867e33ca0965c9c9b91b34267dcc5a5d639a5feee',31678266,'sing-box-1.14.3-linux-amd64/sing-box'),
 ('sing-box','arm64'):('SagerNet/sing-box','v1.14.3','sing-box-1.14.3-linux-arm64.tar.gz','29dae24d76bea3bc62d07dd3e60cfc12003de7debbd6b768df9be9c68360174e',29110944,'sing-box-1.14.3-linux-arm64/sing-box'),
}
class InstallError(RuntimeError):pass

def require(condition,message):
    if not condition:raise InstallError(message)

def digest(raw):return hashlib.sha256(raw).hexdigest()

def trusted_tool(name):
    path=shutil.which(name,path=ENV['PATH']);require(path is not None,'缺少工具：'+name)
    resolved=Path(path).resolve(strict=True)
    for p in (resolved,*resolved.parents):
        info=p.stat();require(info.st_uid==0 and not info.st_mode&0o022,'工具路径归属/权限不可信：'+name)
    require(resolved.is_file(),'工具不是普通文件：'+name)
    return str(resolved)

def run(args,input_bytes=None,timeout=30,quiet=True):
    result=subprocess.run(args,input=input_bytes,stdout=subprocess.PIPE,stderr=subprocess.PIPE,cwd='/',env=ENV,timeout=timeout)
    require(len(result.stdout)<=1<<20 and len(result.stderr)<=1<<20,'命令输出过大，未回显')
    require(result.returncode==0,'命令失败：'+Path(args[0]).name+'（原始输出可能含秘密，不回显）')
    return result.stdout

class Prompt:
    def __init__(self,answers=None):self.answers=answers or {}
    def ask(self,key,message,default=''):
        if key in self.answers:
            value=str(self.answers[key]);print(message+' [已提供本地答案]')
        else:
            value=input(message+(' ['+default+']' if default else '')+'：').strip() or default
        require(len(value)<=300 and not any(ord(c)<32 for c in value),'输入包含控制字符或过长')
        return value
    def yes(self,key,message,default=False):
        answer=self.ask(key,message+' (yes/no)','yes' if default else 'no').lower()
        require(answer in ('yes','no','y','n'),'请明确输入yes/no')
        return answer in ('yes','y')

def dependencies(vpn):
    tools={'systemctl':'systemd','systemd-analyze':'systemd','getent':'libc-bin','useradd':'passwd','setpriv':'util-linux','ip':'iproute2'}
    if vpn:tools.update({'openvpn':'openvpn','nft':'nftables','nsenter':'util-linux'})
    if not Path('/etc/ssl/certs/ca-certificates.crt').is_file():
        tools['system-ca-bundle']='ca-certificates'
    absent=[{'tool':tool,'package':package} for tool,package in tools.items() if tool=='system-ca-bundle' or shutil.which(tool,path=ENV['PATH']) is None]
    return absent

def install_missing(absent,prompt):
    if not absent:return
    print('确实缺少的依赖：'+json.dumps(absent,ensure_ascii=False))
    require(prompt.yes('install_dependencies','是否只安装以上缺少的软件包（不升级全系统）？'),'用户拒绝依赖安装；没有创建项目账号/配置/服务')
    packages=sorted({x['package'] for x in absent});apt=trusted_tool('apt-get')
    run([apt,'update'],timeout=180)
    run([apt,'install','--no-install-recommends','-y',*packages],timeout=240)

def port_available(port,host='0.0.0.0'):
    try:
        with socket.socket(socket.AF_INET,socket.SOCK_STREAM) as s:s.bind((host,port))
        return True
    except OSError:return False

def public_ip_candidate():
    try:
        values=json.loads(run([trusted_tool('ip'),'-j','-4','address','show']))
        for item in values:
            for a in item.get('addr_info',[]):
                ip=ipaddress.ip_address(a.get('local',''))
                if ip.version==4 and ip.is_global:return str(ip)
    except (InstallError,ValueError):pass
    return ''

def hostname(value):
    require(not any(c in value for c in '/\\:@ *'),'域名不能是URL/路径/端口')
    try:value=value.lower().encode('idna').decode('ascii')
    except UnicodeError:raise InstallError('域名编码无效') from None
    require(3<=len(value)<=253 and '.' in value and all(re.fullmatch(r'[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?',label) for label in value.split('.')),'域名不规范')
    try:ipaddress.ip_address(value)
    except ValueError:return value
    raise InstallError('域名不能填数字IP')

def initial_paths():
    return [LIB,ETC,VAR,UNITS/'miku-gate.service',UNITS/'miku-gate-vpn.service']

def refusal_preflight():
    for p in initial_paths():require(not p.exists() and not p.is_symlink(),'已存在Miku资源，拒绝首次安装覆盖：'+str(p)+'；升级/恢复另行确认')
    for user in USERS:
        try:pwd.getpwnam(user)
        except KeyError:continue
        raise InstallError('账号名已存在，不自动复用或修改：'+user)

def validate_binary(path,expected):
    require(re.fullmatch(r'[a-f0-9]{64}',expected) is not None,'必须提供来自可信发行校验值的--sha256')
    p=Path(path);info=p.lstat()
    require(p.is_absolute() and p.resolve(strict=True)==p and stat.S_ISREG(info.st_mode) and info.st_nlink==1 and info.st_uid==0 and not info.st_mode&0o022 and info.st_size<=48<<20,'明确校验的安装程序必须root所有、无组/其他写、非链接的绝对路径')
    for parent in p.parents:
        meta=parent.lstat()
        require(stat.S_ISDIR(meta.st_mode) and meta.st_uid==0 and (not meta.st_mode&0o022 or bool(meta.st_mode&stat.S_ISVTX)),'安装程序目录必须root所有且不可替换（只允许root sticky临时根）')
    raw=p.read_bytes();require(digest(raw)==expected,'本地程序与明确提供校验值不匹配')
    return p.resolve(strict=True),raw

class HTTPSRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self,req,fp,code,msg,headers,newurl):
        u=urllib.parse.urlparse(newurl)
        require(u.scheme=='https' and u.hostname in ('github.com','release-assets.githubusercontent.com','objects.githubusercontent.com'),'核心下载跳转来源不受支持')
        return super().redirect_request(req,fp,code,msg,headers,newurl)

def core_bytes(core,arch):
    repo,tag,name,sha,size,member=CORE_ASSETS[(core,arch)]
    url='https://github.com/'+repo+'/releases/download/'+tag+'/'+name
    opener=urllib.request.build_opener(HTTPSRedirect(),urllib.request.HTTPSHandler(context=ssl.create_default_context()))
    with opener.open(urllib.request.Request(url,headers={'User-Agent':'MikuGate-explicit-installer'}),timeout=90) as response:
        raw=response.read(size+1)
    require(len(raw)==size and digest(raw)==sha,'官方核心压缩包SHA256/大小不匹配，不执行')
    if name.endswith('.zip'):
        with zipfile.ZipFile(io.BytesIO(raw)) as z:
            item=z.getinfo(member);require(item.file_size<=128<<20,'核心解压大小超限');binary=z.read(item)
    else:
        with tarfile.open(fileobj=io.BytesIO(raw),mode='r:gz') as t:
            item=t.getmember(member);require(item.isfile() and item.size<=128<<20,'核心归档成员不安全');binary=t.extractfile(item).read()
    require(binary[:4]==b'\x7fELF','核心不是Linux ELF')
    return binary,{'core':core,'version':tag,'archive_sha256':sha,'binary_sha256':digest(binary)}

def exclusive(path,raw,mode=0o600,uid=0,gid=0):
    fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,mode)
    with os.fdopen(fd,'wb') as stream:
        os.fchmod(stream.fileno(),mode);os.fchown(stream.fileno(),uid,gid);stream.write(raw);stream.flush();os.fsync(stream.fileno())

def make_dir(path,mode=0o700,uid=0,gid=0):
    path.mkdir(mode=mode);os.chmod(path,mode);os.chown(path,uid,gid)

def source_certificate(binary,domain):
    candidates=[(Path('/etc/letsencrypt/live')/domain/'fullchain.pem',Path('/etc/letsencrypt/live')/domain/'privkey.pem',Path('/etc/letsencrypt/archive')/domain),
                (Path('/root/.acme.sh')/domain/'fullchain.cer',Path('/root/.acme.sh')/domain/(domain+'.key'),Path('/root/.acme.sh')/domain),
                (Path('/root/.acme.sh')/(domain+'_ecc')/'fullchain.cer',Path('/root/.acme.sh')/(domain+'_ecc')/(domain+'.key'),Path('/root/.acme.sh')/(domain+'_ecc'))]
    for cert,key,bound in candidates:
        if not cert.exists() or not key.exists():continue
        c,k=cert.resolve(strict=True),key.resolve(strict=True)
        if not c.is_relative_to(bound) or not k.is_relative_to(bound):continue
        try:
            summary=json.loads(run([str(binary),'management','inspect','--origin=https://'+domain+':8443','--certificate='+str(c),'--key='+str(k)]))
            if summary['system_trusted'] and not summary['bootstrap']:return c,k,summary
        except InstallError:continue
    return None

def certificate_plan(binary,prompt,domain,ip,plan_only=False):
    if not domain:
        require(prompt.yes('confirm_ip_certificate','无域名：生成只覆盖此IP的自签引导证书。浏览器不默认信任，必须核对指纹/导入证书后再输入密码，是否同意？'),'未同意IP证书信任方案，未安装')
        return {'mode':'ip-bootstrap','ip':ip}
    existing=source_certificate(binary,domain)
    if existing:
        require(prompt.yes('reuse_certificate','找到系统信任/域名/钥匙匹配通过的既有证书，是否复制受管副本（不改原文件）？',True),'用户拒绝证书复用，未安装')
        c,k,summary=existing;return {'mode':'domain-existing','domain':domain,'cert':str(c),'key':str(k),'summary':summary}
    print('没有可复用的已验证证书；不会改为自签域名或关闭校验。')
    require(prompt.yes('issue_certificate','是否通过Certbot standalone申请？要求域名解析到此VPS且80端口空闲/可从外网访问，不停止已有Nginx。'),'没有证书且未确认申请，未安装')
    require(port_available(80),'80端口已占用，不能停止原服务或抢端口；请用已有Webroot/DNS申请证书后重试')
    if shutil.which('certbot',path=ENV['PATH']) is None:
        if plan_only:
            print('PLAN_ONLY：缺少certbot，申请前会另行征求安装确认，本次不安装。')
        else:
            install_missing([{'tool':'certbot','package':'certbot'}],prompt)
    email=prompt.ask('certificate_email','ACME通知邮箱')
    require(re.fullmatch(r'[A-Za-z0-9_.+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}',email) is not None,'邮箱格式无效')
    try:addresses={a[4][0] for a in socket.getaddrinfo(domain,80,proto=socket.IPPROTO_TCP)}
    except OSError:raise InstallError('域名解析失败，未安装') from None
    require(ip in addresses,'域名解析不含已确认VPS地址；CDN代理/DNS条件请先处理')
    return {'mode':'domain-issue','domain':domain,'email':email}

def unit_text(binary,data,origin,port,cert,key,bootstrap,vpn,instance,uids,cores):
    args=[str(binary),'serve','--data='+str(data),'--listen=0.0.0.0:'+str(port),'--public-origin='+origin,'--tls-certificate='+str(cert),'--tls-key='+str(key),'--max-active=1']
    if bootstrap:args.append('--allow-bootstrap-certificate')
    if vpn:args.append('--vpn-executor')
    for core,path in cores.items():args.append(('--xray-core=' if core=='xray' else '--sing-box-core=')+str(path))
    require(all(re.fullmatch(r'[A-Za-z0-9_./:=[\]-]+',a) for a in args),'systemd参数包含未允许字符')
    control='''[Unit]\nDescription=Miku Gate HTTPS management and owned ingress\nAfter=network-online.target\nWants=network-online.target\n'''
    if vpn:control+='Requires=miku-gate-vpn.service\nAfter=miku-gate-vpn.service\n'
    control+='''\n[Service]\nType=simple\nUser=miku-gate\nGroup=miku-gate\nUMask=0077\nExecStart='''+' '.join(args)+'''\nRestart=on-failure\nRestartSec=5\nTimeoutStopSec=15\nNoNewPrivileges=yes\nCapabilityBoundingSet=\nAmbientCapabilities=\nProtectSystem=strict\nProtectHome=yes\nPrivateTmp=yes\nProtectKernelTunables=yes\nProtectKernelModules=yes\nProtectControlGroups=yes\nReadWritePaths='''+str(data)+'''\nCPUQuota=100%\nMemoryMax=384M\nMemorySwapMax=0\nTasksMax=128\n\n[Install]\nWantedBy=multi-user.target\n'''
    units={'miku-gate.service':control}
    if vpn:
        v=USERS[0];command=[str(binary),'vpn-executor','serve','--instance='+instance,'--allow-uid='+str(uids[v][0]),'--tunnel-uid='+str(uids[USERS[1]][0]),'--worker-uid='+str(uids[USERS[2]][0]),'--apply']
        units['miku-gate-vpn.service']='''[Unit]\nDescription=Miku Gate isolated VPN executor\nAfter=network-online.target\nWants=network-online.target\n\n[Service]\nType=simple\nUser=root\nUMask=0077\nExecStart='''+' '.join(command)+'''\nRestart=on-failure\nRestartSec=5\nTimeoutStopSec=15\nProtectSystem=strict\nProtectHome=yes\nPrivateTmp=yes\nProtectKernelTunables=yes\nProtectKernelModules=yes\nProtectControlGroups=yes\nReadWritePaths=/run/miku-gate-vpnipc\nCPUQuota=100%\nMemoryMax=256M\nMemorySwapMax=0\nTasksMax=64\n\n[Install]\nWantedBy=multi-user.target\n'''
    return units

def verify_served_management(plan,cert_path,certificate_summary):
    import http.client,time
    context=ssl.create_default_context()
    if certificate_summary.get('bootstrap'):
        context=ssl.create_default_context(cafile=str(cert_path))
    last_category='unavailable';deadline=time.monotonic()+15
    while time.monotonic()<deadline:
        try:
            with socket.create_connection(('127.0.0.1',plan['port']),timeout=3) as sock:
                with context.wrap_socket(sock,server_hostname=plan['domain'] or plan['ip']) as secured:
                    require(digest(secured.getpeercert(binary_form=True))==certificate_summary['leaf_sha256'],'实际管理TLS证书指纹不匹配')
                    authority=urllib.parse.urlparse(plan['origin']).netloc
                    secured.sendall(('GET /api/health HTTP/1.1\r\nHost: '+authority+'\r\nConnection: close\r\n\r\n').encode())
                    response=http.client.HTTPResponse(secured);response.begin();raw=response.read(16385)
                    require(response.status==200 and len(raw)<=16384,'实际HTTPS管理健康响应失败')
                    require(json.loads(raw).get('version'),'管理程序版本响应缺失')
                    return
        except (OSError,ssl.SSLError,ValueError,InstallError) as exc:
            last_category=type(exc).__name__;time.sleep(0.3)
    raise InstallError('管理HTTPS尚未验证可达（'+last_category+'），不标记安装完成')

def verify_vpn_executor(binary,instance,uids):
    uid,gid=uids[USERS[0]]
    raw=run([trusted_tool('setpriv'),'--reuid='+str(uid),'--regid='+str(gid),'--clear-groups','--no-new-privs',str(binary),'vpn-executor','info','--instance='+instance],timeout=8)
    caps=json.loads(raw)
    require(caps.get('instance')==instance and caps.get('mode')=='isolated-openvpn' and caps.get('version')==1,'授权控制身份读取的VPN能力响应不匹配')
    options=caps.get('options',{})
    require(options.get('tunnel_uid')==uids[USERS[1]][0] and options.get('worker_uid')==uids[USERS[2]][0],'VPN工作身份与安装计划不匹配')
    return caps

def apply(binary_raw,expected,plan,prompt):
    refusal_preflight()
    require(port_available(plan['port']),'管理端口已被占用，不抢占已有服务')
    core_downloads={core:core_bytes(core,plan['arch']) for core in plan['cores']}
    created=[];started=[];users_created=[]
    print('开始明确同意的安装：软件包/ACME证书若已安装或签发不会自动卸载/撤销；本安装器不改宿主路由、防火墙或现有面板。')
    try:
        # Exact first-install resources only; never replace preexisting paths.
        for p,mode in [(LIB,0o755),(ETC,0o700),(VAR,0o755)]:make_dir(p,mode);created.append(p)
        copied=LIB/'miku-gate';exclusive(copied,binary_raw,0o755);created.append(copied);require(digest(copied.read_bytes())==expected,'安装程序hash不匹配')
        for user in (USERS if plan['vpn'] else USERS[:1]):
            run([trusted_tool('useradd'),'--system','--no-create-home','--shell','/usr/sbin/nologin','--user-group',user]);users_created.append(user)
        uids={u:(pwd.getpwnam(u).pw_uid,pwd.getpwnam(u).pw_gid) for u in users_created}
        uid,gid=uids[USERS[0]];data=VAR/'data';make_dir(data,0o700,uid,gid);created.append(data)
        management_root=data/'management';make_dir(management_root,0o700,uid,gid);created.append(management_root)
        cert,key=management_root/'certificate.pem',management_root/'private.key';certificate=plan['certificate']
        if certificate['mode']=='domain-issue':
            run([trusted_tool('certbot'),'certonly','--standalone','--preferred-challenges','http','--non-interactive','--agree-tos','--email',certificate['email'],'--cert-name',certificate['domain'],'-d',certificate['domain']],timeout=240)
            discovered=source_certificate(copied,certificate['domain']);require(discovered is not None,'新签发证书未通过安全验证');c,k,summary=discovered;certificate={'mode':'domain-existing','domain':certificate['domain'],'cert':str(c),'key':str(k),'summary':summary}
        if certificate['mode']=='ip-bootstrap':
            bootstrap_dir=data/'bootstrap';summary=json.loads(run([trusted_tool('setpriv'),'--reuid='+str(uid),'--regid='+str(gid),'--clear-groups','--no-new-privs',str(copied),'management','bootstrap-ip','--ip='+plan['ip'],'--output='+str(bootstrap_dir),'--confirm']))
            # Transfer only public/private PEM within the same dedicated account
            # data directory; no external original certificate is modified.
            exclusive(cert,(bootstrap_dir/'certificate.pem').read_bytes(),0o600,uid,gid);exclusive(key,(bootstrap_dir/'private.key').read_bytes(),0o600,uid,gid)
            created.extend([bootstrap_dir/'certificate.pem',bootstrap_dir/'private.key',bootstrap_dir])
        else:
            c,k=Path(certificate['cert']),Path(certificate['key']);summary=certificate['summary']
            exclusive(cert,c.read_bytes(),0o600,uid,gid);exclusive(key,k.read_bytes(),0o600,uid,gid)
        created.extend([cert,key]);cores={};core_reports=[]
        for core in plan['cores']:
            raw,report=core_downloads[core];target=LIB/core;exclusive(target,raw,0o755);created.append(target);cores[core]=target;core_reports.append(report)
        init=run([trusted_tool('setpriv'),'--reuid='+str(uid),'--regid='+str(gid),'--clear-groups','--no-new-privs',str(copied),'init','--data='+str(data)])
        # Only caller's terminal receives the one-time password; never include it
        # in state/unit files, report JSON, command parameters or diagnostics.
        account_text=init.decode('utf-8');require('密码：' in account_text,'初始化没有一次性凭据')
        print('以下是一次性管理凭据；即使后续服务启动失败也请保存，安装还在进行。')
        print(account_text,flush=True)
        instance=run([str(copied),'instance','--data='+str(data)]).decode().strip();require(re.fullmatch(r'[A-Za-z0-9_-]{22}',instance) is not None,'实例身份无效')
        if plan['vpn']:
            p=Path('/run/miku-gate-vpnipc')
            if not p.exists():make_dir(p,0o755)
            info=p.lstat();require(stat.S_ISDIR(info.st_mode) and info.st_uid==0 and not info.st_mode&0o022 and not p.is_symlink(),'VPN IPC根路径不安全')
        units=unit_text(copied,data,plan['origin'],plan['port'],cert,key,certificate['mode']=='ip-bootstrap',plan['vpn'],instance,uids,cores)
        for name,text in units.items():p=UNITS/name;exclusive(p,text.encode(),0o644);created.append(p)
        state={k:v for k,v in plan.items() if k!='certificate'};state.update(binary_sha256=expected,instance=instance,core_sources=core_reports,certificate_summary=summary,certificate_source=certificate,users=uids)
        exclusive(ETC/'install-state.json',json.dumps(state,ensure_ascii=False,indent=2).encode());created.append(ETC/'install-state.json')
        run([trusted_tool('systemd-analyze'),'verify',*[str(UNITS/n) for n in units]])
        run([trusted_tool('systemctl'),'daemon-reload'])
        if prompt.yes('start_services','是否启用开机启动并立即启动这些明确Miku服务？',True):
            for name in (['miku-gate-vpn.service'] if plan['vpn'] else [])+['miku-gate.service']:
                started.append(name)
                run([trusted_tool('systemctl'),'enable','--now',name],timeout=35)
            run([trusted_tool('systemctl'),'is-active','--quiet','miku-gate.service'])
            verify_served_management(plan,cert,summary)
            if plan['vpn']:verify_vpn_executor(copied,instance,uids)
        print('管理地址：'+plan['origin'])
        print('证书指纹SHA256：'+summary['leaf_sha256'])
        if certificate['mode']=='ip-bootstrap':print('这是自签IP引导证书，浏览器不默认信任。通过已信任SSH通道/安装输出核对指纹或导入certificate.pem后再输入密码，不能忽略证书警告。')
        print('未改防火墙：若外网不可达，请在现有防火墙/云安全组明确开放TCP '+str(plan['port'])+'；未开放出口认证代理端口。')
        print('已检测的既有面板保持原配置。TLS申请续期、管理证书受管副本热更新、既有面板持久复用与升级/回滚仍需后续完善。')
        return state
    except BaseException:
        # Stop only exact units created by this attempt. Retain private state,
        # accounts and files for review; no blind recursive rollback/uninstall.
        for name in reversed(started):
            with __import__('contextlib').suppress(Exception):run([trusted_tool('systemctl'),'disable','--now',name])
        print('安装未完成。已创建资源保留供检查，不覆盖/删除未知文件，不撤销已安装系统软件包或ACME证书。',file=sys.stderr)
        print('受控保留目录：'+', '.join(str(p) for p in (LIB,ETC,VAR) if p.exists()),file=sys.stderr)
        raise

def platform_gate():
    require(sys.platform=='linux' and os.geteuid()==0,'安装协调器需已授权Linux root；控制服务不是root')
    require(pwd is not None,'系统账号模块不可用')
    arch={'x86_64':'amd64','aarch64':'arm64','arm64':'arm64'}.get(platform.machine())
    require(arch is not None,'仅支持amd64/arm64')
    release=Path('/etc/os-release').read_text()
    require(re.search(r'^ID=(?:"?)(debian|ubuntu)(?:"?)$',release,re.M) is not None,'apply当前仅支持Debian/Ubuntu')
    require(Path('/run/systemd/system').is_dir(),'需要正在运行的systemd')
    return arch

def tun_available():return Path('/dev/net/tun').exists()

def main(argv=None,prompt=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary',required=True);parser.add_argument('--sha256',required=True);parser.add_argument('--plan-only',action='store_true');parser.add_argument('--answers',help='明确提供的本机答案JSON；没有密码或程序命令字段')
    args=parser.parse_args(argv)
    arch=platform_gate()
    binary,raw=validate_binary(args.binary,args.sha256)
    if prompt is None:
        answers={}
        if args.answers:
            p=Path(args.answers);info=p.lstat();require(stat.S_ISREG(info.st_mode) and not p.is_symlink() and info.st_size<=16384,'本地答案文件不安全/过大');answers=json.loads(p.read_text());require(isinstance(answers,dict) and all(isinstance(k,str) and isinstance(v,(str,int)) for k,v in answers.items()),'答案须JSON对象的短字符串/数字，不接受程序命令或秘密字段')
        prompt=Prompt(answers)
    refusal_preflight()
    vpn=prompt.yes('enable_vpn','是否启用隔离OpenVPN/VPN Gate出口（也可只用自备代理上游）？',True)
    if vpn:require(tun_available(),'宿主未提供TUN；不安装假VPN或改变宿主网络')
    absent=dependencies(vpn);print('检测缺少工具：'+json.dumps(absent,ensure_ascii=False))
    if not args.plan_only:install_missing(absent,prompt)
    inventory=json.loads(run([str(binary),'inventory']))
    print('已有服务检测（只读、不读取密码/数据库）：'+json.dumps([{'name':c['name'],'state':c['state']} for c in inventory['components']],ensure_ascii=False))
    ip=prompt.ask('vps_ip','确认VPS数字IPv4地址（NAT时请填外部地址）',public_ip_candidate());ip=str(ipaddress.IPv4Address(ip));require(not ipaddress.ip_address(ip).is_unspecified and not ipaddress.ip_address(ip).is_multicast,'VPS IP不能不指定/多播')
    domain=''
    if prompt.yes('use_domain','是否绑定自己的管理域名？域名HTTPS优先，否则用VPS IP明确证书信任方案。'):domain=hostname(prompt.ask('management_domain','管理域名（不填URL）'))
    port=int(prompt.ask('management_port','管理HTTPS端口（默认8443避免抢现有80/443）','8443'));require(1024<=port<=65535 and port_available(port),'管理端口不安全/已占用，不停止旧服务')
    existing=any(c['id'] in ('x-ui','xray','sing-box') and c['state']=='detected' for c in inventory['components'])
    reuse=False
    if existing:
        reuse=prompt.yes('reuse_existing','检测到已有节点工具，是否希望复用其入口（当前不改面板生成文件）？')
        if reuse:require(prompt.yes('confirm_deferred_reuse','面板持久协同尚未交付。是否先安装管理/出口，并保留复用意图稍后受控迁移，不擅自接管？'),'用户等待复用适配，未创建项目安装')
    choice=prompt.ask('managed_core','选择并行独立受管核心：xray / sing-box / both / none（none保留管理/出口）','xray')
    require(choice in ('xray','sing-box','both','none'),'核心选择无效')
    cores=['xray','sing-box'] if choice=='both' else [] if choice=='none' else [choice]
    certificate=certificate_plan(binary,prompt,domain,ip,args.plan_only)
    authority=domain or ip;origin='https://'+authority+(':'+str(port) if port!=443 else '')
    plan={'arch':arch,'vpn':vpn,'ip':ip,'domain':domain,'origin':origin,'port':port,'cores':cores,'reuse_requested':reuse,'certificate':certificate,'write_paths':[str(x) for x in initial_paths()],'host_routes_changed':False,'existing_service_configs_changed':False,'firewall_changed':False}
    print('明确安装计划：'+json.dumps({k:v for k,v in plan.items() if k!='certificate'},ensure_ascii=False,indent=2))
    print('账号：'+', '.join(USERS if vpn else USERS[:1])+'；管理384MiB/VPN256MiB上限，默认同时1出口（不是容量承诺）。')
    if args.plan_only:
        print('PLAN_ONLY：未安装依赖、未创建账号/文件/服务。证书读取与明确允许的DNS检查是只读。');return plan
    require(prompt.yes('confirm_install','是否应用以上独立安装计划？需下载所选固定官方核心，创建专用账号/TLS副本/systemd服务。'),'用户拒绝应用；未创建项目账号/文件/服务')
    return apply(raw,args.sha256,plan,prompt)

if __name__=='__main__':
    try:main()
    except (InstallError,ValueError,KeyError,OSError,subprocess.TimeoutExpired) as exc:
        print('INSTALL_STOPPED '+type(exc).__name__+': '+str(exc),file=sys.stderr);sys.exit(1)
