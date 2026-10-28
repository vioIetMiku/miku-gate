"""Fixed paths and install/maintenance helpers for the Miku fanout edition."""
import base64
import contextlib
import hashlib
import ipaddress
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import shutil
import socket
import ssl
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request
import zipfile

DATA=Path('/var/lib/miku')
LIB=Path('/usr/local/lib/miku')
UNIT=Path('/etc/systemd/system/miku.service')
MENU=Path('/usr/local/bin/miku')
OPENRC=Path('/etc/init.d/miku')
ENV={**os.environ,'PATH':'/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/sbin:/usr/local/bin','GOTELEMETRY':'off','DEBIAN_FRONTEND':'noninteractive'}
SERVICE='miku'

def require(value,message):
    if not value:raise RuntimeError(message)

def run(args,timeout=60,input_bytes=None,checked=True):
    result=subprocess.run([str(a) for a in args],input=input_bytes,stdout=subprocess.PIPE,stderr=subprocess.PIPE,env=ENV,timeout=timeout)
    require(len(result.stdout)<=8<<20 and len(result.stderr)<=2<<20,'命令输出超限')
    if checked:require(result.returncode==0,'命令失败：'+Path(str(args[0])).name+'；请查看 miku logs')
    return result

def root():
    require(os.geteuid()==0,'请用 sudo 运行')
    require(os.uname().sysname=='Linux','当前安装面向Linux VPS')
    os.umask(0o077)

def trusted(path,mode=None,max_bytes=160<<20):
    path=Path(path);info=path.lstat()
    require(path.resolve(strict=True)==path and stat.S_ISREG(info.st_mode) and info.st_uid==0 and not info.st_mode&0o022 and info.st_nlink==1 and info.st_size<=max_bytes,'文件归属/路径异常：'+str(path))
    if mode is not None:require(stat.S_IMODE(info.st_mode)==mode,'文件权限异常：'+str(path))
    return path.read_bytes()

def private_dir(path):
    path=Path(path)
    if not path.exists():path.mkdir(mode=0o700,parents=True)
    info=path.lstat();require(path.resolve(strict=True)==path and stat.S_ISDIR(info.st_mode) and info.st_uid==0 and not info.st_mode&0o022,'私有目录异常：'+str(path)+'；可使用安装器的清除重装选项')
    # Older releases created root-owned 755 data directories. Narrowing their
    # permissions is safe and allows an ordinary in-place upgrade.
    if stat.S_IMODE(info.st_mode)!=0o700:os.chmod(path,0o700)
    return path

def program_dir():
    if not LIB.exists():LIB.mkdir(mode=0o755,parents=True);os.chmod(LIB,0o755)
    info=LIB.lstat();require(LIB.resolve(strict=True)==LIB and info.st_uid==0 and stat.S_ISDIR(info.st_mode) and stat.S_IMODE(info.st_mode)==0o755,'程序目录异常')
    return LIB

def atomic(path,raw,mode=0o600):
    path=Path(path)
    if path.exists():trusted(path)
    fd,temp=tempfile.mkstemp(prefix='.miku-',dir=path.parent)
    try:
        with os.fdopen(fd,'wb') as stream:os.fchmod(stream.fileno(),mode);stream.write(raw);stream.flush();os.fsync(stream.fileno())
        os.replace(temp,path)
    finally:
        if os.path.exists(temp):os.unlink(temp)

def write_json(path,value):atomic(path,(json.dumps(value,ensure_ascii=False,indent=2)+'\n').encode())
def settings():return json.loads(trusted(DATA/'settings.json',0o600,64<<10))
def installed():return json.loads(trusted(DATA/'install.json',0o600,64<<10))
def systemd():return Path('/run/systemd/system').is_dir() and shutil.which('systemctl',path=ENV['PATH']) is not None
def service(action):
    if systemd():return run(['systemctl',action,'miku.service'],timeout=60)
    return run(['rc-service','miku',action],timeout=60)
def active():
    return run(['systemctl','is-active','--quiet','miku.service'] if systemd() else ['rc-service','miku','status'],checked=False).returncode==0

def package_manager():
    for name in ('apt-get','dnf','yum','apk','pacman','zypper'):
        if shutil.which(name,path=ENV['PATH']):return name
    raise RuntimeError('未找到支持的包管理器')

def packages(names):
    if not names:return
    manager=package_manager()
    print('自动安装缺少依赖：'+', '.join(names),flush=True)
    if manager=='apt-get':run(['apt-get','update','-qq'],timeout=240);command=['apt-get','install','--no-install-recommends','-y',*names]
    elif manager in ('dnf','yum'):command=[manager,'install','-y',*names]
    elif manager=='apk':command=['apk','add','--no-cache',*names]
    elif manager=='pacman':command=['pacman','-Sy','--noconfirm','--needed',*names]
    else:command=['zypper','--non-interactive','install','-y',*names]
    run(command,timeout=300)

def dependencies():
    manager=package_manager();required={'openvpn':'openvpn','curl':'curl','openssl':'openssl','iptables':'iptables','ip':'iproute2' if manager in ('apt-get','apk','pacman') else 'iproute','tar':'tar','ping':'iputils-ping' if manager=='apt-get' else 'iputils'}
    missing=sorted({package for command,package in required.items() if not shutil.which(command,path=ENV['PATH'])})
    if not ssl.get_default_verify_paths().cafile:missing.append('ca-certificates')
    packages(sorted(set(missing)))
    if not Path('/dev/net/tun').exists() and shutil.which('modprobe',path=ENV['PATH']):run(['modprobe','tun'],checked=False)
    require(Path('/dev/net/tun').exists(),'VPS未提供TUN；请在服务商面板启用TUN后重试')

def arch():
    result={'x86_64':'amd64','aarch64':'arm64','arm64':'arm64'}.get(os.uname().machine)
    require(result is not None,'支持 amd64 / arm64 VPS');return result

def verify_distribution(directory):
    directory=Path(directory).resolve(strict=True)
    meta=json.loads(trusted(directory/'BUILD.json',max_bytes=256<<10))
    require(meta.get('product')=='miku-fanout' and meta.get('format')==1,'不是此Miku版安装包')
    files=meta.get('files',{});require(isinstance(files,dict) and 5<=len(files)<=4096,'安装包清单无效')
    for name,expected in files.items():
        require(re.fullmatch(r'[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*',name) and '..' not in name.split('/'),'安装包路径无效')
        raw=trusted(directory/name);require(hashlib.sha256(raw).hexdigest()==expected,'安装包校验失败：'+name)
    return directory,meta

def core_download(core,architecture):
    assets=json.loads(trusted(LIB/'CORE_ASSETS.json',0o600,16384));record=assets[core+'-'+architecture]
    address='https://github.com/'+record['repo']+'/releases/download/'+record['version']+'/'+record['name']
    with urllib.request.urlopen(urllib.request.Request(address,headers={'User-Agent':'MikuFanoutInstaller'}),timeout=90) as response:
        require(response.geturl().startswith('https://'),'核心下载必须HTTPS');raw=response.read(record['bytes']+1)
    require(len(raw)==record['bytes'] and hashlib.sha256(raw).hexdigest()==record['sha256'],'官方核心大小/SHA256校验失败')
    if record['name'].endswith('.zip'):
        with zipfile.ZipFile(io.BytesIO(raw)) as archive:
            item=archive.getinfo(record['member']);require(item.file_size<=128<<20,'核心解压超限');binary=archive.read(item)
    else:
        with tarfile.open(fileobj=io.BytesIO(raw),mode='r:gz') as archive:
            item=archive.getmember(record['member']);require(item.isfile() and item.size<=128<<20,'核心成员无效');binary=archive.extractfile(item).read()
    require(binary.startswith(b'\x7fELF'),'核心不是Linux ELF');atomic(DATA/'bin'/core,binary,0o755)
    return hashlib.sha256(binary).hexdigest()

def ensure_cores(old=None):
    private_dir(DATA/'bin');architecture=arch()
    def prepare(core):
        path=DATA/'bin'/core
        if path.exists():trusted(path,0o755);return
        previous=Path('/usr/local/lib/miku-gate')/core
        records={r['core']:r for r in (old or {}).get('core_sources',[])}
        if core in records and previous.exists():
            raw=trusted(previous,0o755);require(hashlib.sha256(raw).hexdigest()==records[core]['binary_sha256'],'旧核心程序已变化');atomic(path,raw,0o755)
        else:print('准备官方 '+core,flush=True);core_download(core,architecture)
    from concurrent.futures import ThreadPoolExecutor
    with ThreadPoolExecutor(max_workers=2) as workers:
        list(workers.map(prepare,('xray','sing-box')))

def port_available(port):
    if not 1<=port<=65535:return False
    for family,host in ((socket.AF_INET,'0.0.0.0'),(socket.AF_INET6,'::')):
        try:
            with socket.socket(family,socket.SOCK_STREAM) as channel:
                if family==socket.AF_INET6:channel.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,1)
                channel.bind((host,port))
        except OSError as exc:
            import errno
            if family==socket.AF_INET6 and exc.errno in (errno.EAFNOSUPPORT,errno.EPROTONOSUPPORT,errno.EADDRNOTAVAIL):continue
            return False
    return True

def reserved_ports(data=None,include_common=True):
    data=Path(data) if data is not None else DATA
    ports={8899} if include_common else set()  # Native installer compatibility.
    binary=Path('/usr/local/x-ui/x-ui')
    if binary.is_file() and os.access(binary,os.X_OK):
        try:
            result=run([binary,'setting','-show'],timeout=8,checked=False)
            clean=re.sub(r'\x1b\[[0-9;]*m','',result.stdout.decode(errors='replace'))
            ports.update(int(p) for p in re.findall(r'(?m)^port:[^\S\r\n]*(\d+)',clean))
        except (OSError,RuntimeError,subprocess.TimeoutExpired):pass
    for path,field in ((data/'native.json','inbounds'),(data/'xray.json','inbounds'),(data/'state.json','tunnels'),(Path('/usr/local/x-ui/bin/config.json'),'inbounds')):
        if not path.exists():continue
        try:
            value=json.loads(trusted(path,max_bytes=8<<20))
            ports.update(int(i['port']) for i in (value.get(field) or []) if isinstance(i,dict) and str(i.get('port','')).isdigit())
        except (ValueError,KeyError,RuntimeError):pass
    return ports

def select_port(preferred,reserved=(),owned=None):
    """Prefer the saved/requested port, then scan and wrap without duplicates.

    owned is accepted only after the caller verifies a running service's actual
    health/configuration; it must never be inferred from a saved file alone.
    """
    require(isinstance(preferred,int) and 1024<=preferred<=65535,'端口需1024至65535')
    reserved=set(reserved)
    for offset in range(65536-1024):
        port=1024+(preferred-1024+offset)%(65536-1024)
        if port not in reserved and (port==owned or port_available(port)):
            return port
    raise RuntimeError('未找到可用端口（1024–65535），请检查监听服务和预留入口')

def choose_port(requested=None):
    import secrets
    reserved=reserved_ports()
    if requested is not None:
        require(1024<=requested<=65535,'管理端口需1024至65535')
        if requested not in reserved and port_available(requested):return requested
    start=secrets.randbelow(40000)+10000
    for offset in range(50000):
        port=10000+(start-10000+offset)%50000
        if port not in reserved and port_available(port):return port
    raise RuntimeError('未找到空闲管理端口')

def login_url(value=None):
    return origin(value)+'/'+trusted(DATA/'basepath',0o600,1024).decode().strip().strip('/')+'/'

def banner(animate=True):
    lines=[r' __  __ _ _                ____       _       ',r'|  \/  (_) | ___   _      / ___| __ _| |_ ___ ',r'| |\/| | | |/ / | | |____| |  _ / _` | __/ _ \\',r'| |  | | |   <| |_| |____| |_| | (_| | ||  __/',r'|_|  |_|_|_|\_\\__,_|     \____|\__,_|\__\___|']
    tty=sys.stdout.isatty() and os.environ.get('TERM','dumb')!='dumb' and not os.environ.get('NO_COLOR')
    for line in lines:
        print(('\033[96m' if tty else '')+line+('\033[0m' if tty else ''),flush=True)
        if tty and animate:time.sleep(.045)
    print('                    Miku Gate  /  你的连接，你的节奏\n')

def purge_current():
    """Remove only fixed Miku-owned destinations; callers hold the outer lock."""
    destinations=[DATA,LIB,MENU,UNIT,OPENRC,UNIT.with_name('miku-certificate.service'),UNIT.with_name('miku-certificate.timer')]
    for path in destinations:
        if not path.exists() and not path.is_symlink():continue
        info=path.lstat()
        require(not path.is_symlink() and path.resolve()==path and info.st_uid==0,'拒绝清除归属或路径异常的文件：'+str(path))
        if path.is_dir():
            require(not os.path.ismount(path),'不能清除挂载点：'+str(path))
            for child in path.rglob('*'):
                require(child.lstat().st_uid==0 and not os.path.ismount(child),'目录中包含非本服务文件或挂载点：'+str(child))
        elif path in (UNIT,OPENRC):require(str(LIB/'miku') in trusted(path).decode(errors='replace'),'同名服务不属于 Miku Gate')
        elif path==MENU:require(str(LIB/'maintain.py') in trusted(path).decode(errors='replace'),'同名快捷命令不属于 Miku Gate')
        elif path.name.startswith('miku-certificate.'):require(str(LIB/'maintain.py') in trusted(path).decode(errors='replace') or b'Unit=miku-certificate.service' in trusted(path) or b'Description=Daily Miku certificate renewal' in trusted(path),'同名定时服务不属于 Miku Gate')
    if UNIT.exists() or OPENRC.exists():
        if active():service('stop')
        if systemd():run(['systemctl','disable','miku.service'],checked=False)
        else:run(['rc-update','del','miku','default'],checked=False)
    if systemd() and UNIT.with_name('miku-certificate.timer').exists():run(['systemctl','disable','--now','miku-certificate.timer'],checked=False)
    if systemd() and UNIT.with_name('miku-certificate.service').exists():run(['systemctl','stop','miku-certificate.service'],checked=False)
    for path in destinations:
        if path.is_dir():shutil.rmtree(path)
        elif path.exists():path.unlink()
    if systemd():run(['systemctl','daemon-reload'])
    print('Miku Gate 已卸载，配置、节点、凭据和本服务备份已清除，可重新运行安装命令。')

def valid_domain(domain):
    return len(domain)<=253 and '.' in domain and all(re.fullmatch(r'[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?',label) for label in domain.split('.'))

def origin(value=None):
    value=value or settings();host=value.get('public_host') or value.get('public_ip') or 'VPS_IP'
    if ':' in host and not host.startswith('['):host='['+host+']'
    return ('https' if value.get('tls_cert') and value.get('tls_key') else 'http')+'://'+host+':'+str(value['port'])

def info():
    value=settings();path=trusted(DATA/'basepath',0o600,1024).decode().strip();password=trusted(DATA/'password',0o600,1024).decode().strip()
    print('管理地址：'+origin(value)+'/'+path.strip('/')+'/\n访问口令：'+password+'\n快捷菜单：miku\n一键节点：miku quick\n全部订阅：miku sub\n修改口令：miku password\n域名设置：miku domain\n卸载并清除数据：miku purge')

def public_ip():
    for url in ('https://api.ipify.org','https://ipv4.icanhazip.com'):
        try:
            with urllib.request.urlopen(url,timeout=5) as response:value=response.read(64).decode().strip()
            address=ipaddress.IPv4Address(value)
            if address.is_global:return str(address)
        except (OSError,ValueError):pass
    value=input('请输入VPS公网IPv4：').strip() if os.isatty(0) else ''
    address=ipaddress.IPv4Address(value);require(address.is_global,'需要有效公网IPv4');return str(address)

def wait_ready(seconds=75):
    value=settings();path=trusted(DATA/'basepath',0o600,1024).decode().strip().strip('/')
    context=ssl.create_default_context()
    if value.get('tls_cert'):context.load_verify_locations(cafile=value['tls_cert'])
    host=value.get('public_host') or value.get('public_ip') or '127.0.0.1'
    # Connect locally, retaining the published hostname for TLS verification.
    import http.client
    class LocalTLS(http.client.HTTPSConnection):
        def connect(self):
            self.sock=socket.create_connection(('127.0.0.1',self.port),timeout=3)
            self.sock=self._context.wrap_socket(self.sock,server_hostname=self.host)
    deadline=time.monotonic()+seconds
    while time.monotonic()<deadline:
        connection=None
        try:
            connection=LocalTLS(host,value['port'],context=context,timeout=3) if value.get('tls_cert') else http.client.HTTPConnection('127.0.0.1',value['port'],timeout=3)
            connection.request('GET','/'+path+'/',headers={'Host':host+':'+str(value['port'])})
            response=connection.getresponse();raw=response.read(1<<20)
            if response.status==200 and response.getheader('X-Miku-Gate')=='1' and b'<html' in raw.lower():return
        except (OSError,ssl.SSLError,http.client.HTTPException):pass
        finally:
            if connection:connection.close()
        time.sleep(1)
    raise RuntimeError('服务未就绪，请用 miku logs 查看日志；已有配置和备份保留')

@contextlib.contextmanager
def maintenance_lock():
    import fcntl
    # Outside DATA so even a purge/reinstall holds the same lock throughout.
    parent=DATA.parent;parent.mkdir(mode=0o755,parents=True,exist_ok=True)
    info=parent.lstat();require(parent.resolve()==parent and info.st_uid==0 and stat.S_ISDIR(info.st_mode) and not info.st_mode&0o022,'维护锁父目录异常')
    fd=os.open(parent/('.'+DATA.name+'.maintenance.lock'),os.O_RDWR|os.O_CREAT|os.O_CLOEXEC|os.O_NOFOLLOW,0o600)
    try:
        info=os.fstat(fd);require(info.st_uid==0 and stat.S_ISREG(info.st_mode) and info.st_nlink==1,'维护锁异常')
        fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB);yield
    finally:os.close(fd)

def snapshot(label):
    private_dir(DATA/'backups');path=DATA/'backups'/(label+'-'+str(time.time_ns())+'.tar.gz')
    with tarfile.open(path,'w:gz') as archive:
        for child in DATA.iterdir():
            if child.name in ('backups','bin','.maintenance.lock') or child.is_symlink():continue
            archive.add(child,arcname='data/'+child.name,recursive=True)
        for name in ('miku','maintain.py','common.py','quick.py','install.py','CORE_ASSETS.json','FLAG_ICONS_LICENSE'):
            file=LIB/name
            if file.exists():archive.add(file,arcname='lib/'+name)
        if MENU.exists():archive.add(MENU,arcname='menu/miku')
        file=UNIT if systemd() else Path('/etc/init.d/miku')
        if file.exists():archive.add(file,arcname='service/miku')
    os.chmod(path,0o600)
    return path

def old_maintenance():
    path=Path('/etc/miku-gate/install-state.json')
    if not path.exists():return None,None
    old=json.loads(trusted(path,0o600,64<<10));hashes=old.get('maintenance_sha256',{})
    require(set(hashes)=={'miku-install.py','miku-maintain.py'},'旧安装缺少登记的维护工具')
    for name,digest in hashes.items():require(hashlib.sha256(trusted(Path('/usr/local/lib/miku-gate')/name,0o644)).hexdigest()==digest,'旧安装维护工具已变化')
    spec=importlib.util.spec_from_file_location('legacy_miku_maintenance','/usr/local/lib/miku-gate/miku-maintain.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    return module,old

def import_legacy(packet):
    lands=packet.get('landings',[]);by_exit={v['exit']['id']:v for v in lands};tunnels=[]
    for slot,v in enumerate(lands):
        node=v['node'];exit=v['exit'];tunnels.append({'slot':slot,'port':exit['port'],'hostname':node['hostname'],'country_code':exit['country'],'country':node.get('country_name',exit['country']),'config':v['config'],'socks_user':exit['username'],'socks_pass':v['password']})
    if tunnels:write_json(DATA/'state.json',{'tunnels':tunnels})
    inbounds=[];private_dir(DATA/'certs')
    def tag(host):return re.sub(r'[^A-Za-z0-9_-]','-',host)
    for index,node in enumerate(packet.get('nodes',[]),1):
        material=node['material'];request=material['request'];identity=material['identity'];template=request['template'];protocol='trojan' if template=='trojan-tls' else 'vless';security='reality' if template=='vless-reality-vision' else 'tls'
        clients=[];bound=''
        for route in node.get('routes',[]):
            landing=by_exit.get(route['exit_id']);host=tag(landing['node']['hostname']) if landing else 'legacy-unconfigured-'+tag(route['exit_id'])
            rid=route['identity'];clients.append({'email':route['name'],'id':rid['uuid'],'password':rid['password'],'flow':'xtls-rprx-vision' if protocol=='vless' else '', 'enable':True,'bound_to':host})
            if not bound:bound=host
        if not clients:clients=[{'email':node['name'],'id':identity['uuid'],'password':identity['password'],'flow':'xtls-rprx-vision' if protocol=='vless' else '', 'enable':not material.get('legacy_disabled',False)}]
        if node.get('legacy_exit_id'):
            landing=by_exit.get(node['legacy_exit_id']);bound=tag(landing['node']['hostname']) if landing else 'legacy-unconfigured-'+tag(node['legacy_exit_id'])
        ib={'id':index,'port':request['port'],'protocol':protocol,'network':'tcp','path':'','host':'','security':security,'remark':node['name'],'enable':True,'clients':clients,'bound_to':bound}
        if security=='reality':
            private=identity['reality_private_key'];pub=run([DATA/'bin'/'xray','x25519','-i',private]).stdout.decode();match=re.search(r'(?i)(?:password\s*\(publickey\)|public\s*key):\s*(\S+)',pub);require(match,'无法保留原REALITY密钥')
            ib['reality']={'dest':request['reality_hostname']+':443','server_names':[request['server_name']],'private_key':private,'public_key':match.group(1),'short_ids':[identity['short_id']],'fingerprint':'chrome'}
        else:
            cert=DATA/'certs'/('legacy-'+str(index)+'.pem');key=DATA/'certs'/('legacy-'+str(index)+'.key');atomic(cert,base64.b64decode(material['certificate_pem']));atomic(key,base64.b64decode(material['private_key_pem']));ib['tls']={'server_name':request['server_name'],'cert_file':str(cert),'key_file':str(key),'self_signed':False}
        inbounds.append(ib)
    if inbounds:
        write_json(DATA/'native.json',{'next_id':len(inbounds)+1,'inbounds':inbounds});core=packet['nodes'][0]['material']['request']['core'];atomic(DATA/'panel_mode',('sing-box' if core=='sing-box' else 'native').encode())
    print('已导入入口 '+str(len(inbounds))+' 个、已配置VPN落地 '+str(len(tunnels))+' 个；未配置/特殊上游记录 '+str(packet.get('skipped',0))+' 个保留在旧版备份。')
