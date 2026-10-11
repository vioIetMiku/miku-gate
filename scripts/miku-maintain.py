#!/usr/bin/env python3
"""Owner-checked Miku backup, same-install upgrade, paired rollback and recovery.

Explicit confirmations, fixed destinations, offline verified release bundles,
paired DB/key snapshots and a durable root-only recovery journal. Never extract
an archive to its own paths or downgrade by swapping an old executable alone.
"""
import argparse
import contextlib
import hashlib
import gzip
import datetime
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tarfile
import time

spec=importlib.util.spec_from_file_location('miku_install',Path(__file__).with_name('miku-install.py'))
install=importlib.util.module_from_spec(spec);spec.loader.exec_module(install)

FILES={
 'installation/install-state.json':Path('/etc/miku-gate/install-state.json'),
 'binary/miku-gate':Path('/usr/local/lib/miku-gate/miku-gate'),
 'units/miku-gate.service':Path('/etc/systemd/system/miku-gate.service'),
 'units/miku-gate-vpn.service':Path('/etc/systemd/system/miku-gate-vpn.service'),
 'units/miku-gate-certificate.service':Path('/etc/systemd/system/miku-gate-certificate.service'),
 'units/miku-gate-certificate.timer':Path('/etc/systemd/system/miku-gate-certificate.timer'),
}
DATA=Path('/var/lib/miku-gate/data')
DATABASE=('miku-gate.db','miku-gate.db-wal','miku-gate.db-shm','miku-gate.db.key','.serve.lock')
CERT_DIRS=('management','bootstrap')
MAX_FILE=48<<20
MAX_TOTAL=96<<20
MAX_ARCHIVE=MAX_TOTAL+(1<<20)
JOURNAL=Path("/etc/miku-gate/maintenance-transaction.json")
STARTUP_BLOCK=Path("/etc/miku-gate/maintenance-block")
MAINTENANCE_LOCK=Path("/etc/miku-gate/.maintenance.lock")

class BackupError(RuntimeError):pass

def require(value,message):
    if not value:raise BackupError(message)

def safe_path(path,uid,mode=None,max_bytes=MAX_FILE):
    # Open via resolved non-symlink metadata plus O_NOFOLLOW and identity check.
    # The root-only manifest paths are fixed, never constructed from archive data.
    path=Path(path)
    require(path.is_absolute() and path.resolve(strict=True)==path,'资源路径含链接或不存在，停止')
    info=path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink==1 and info.st_uid==uid and info.st_size<=max_bytes,'资源文件归属/类型/大小异常，停止')
    require(not info.st_mode&0o022 and (mode is None or stat.S_IMODE(info.st_mode)==mode),'资源权限不符合备份要求')
    for parent in path.parents:
        current=parent.lstat()
        require(stat.S_ISDIR(current.st_mode) and current.st_uid in (0,uid) and not current.st_mode&0o022,'资源父路径归属/权限异常')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK|os.O_CLOEXEC)
    with os.fdopen(fd,'rb') as stream:
        require(os.path.samestat(info,os.fstat(stream.fileno())),'读取时资源被替换')
        raw=stream.read(max_bytes+1)
    require(len(raw)<=max_bytes,'资源超过有界读取限制')
    return raw,info

def check_identity(state):
    expected=install.USERS if state.get("vpn") else install.USERS[:1]
    require(isinstance(state.get("vpn"),bool) and state.get("cores") in ([],["xray"],["sing-box"],["xray","sing-box"]),"未知安装模式")
    require(set(state.get("users",{}))==set(expected),"安装账号登记不完整")
    for name in expected:
        account=install.pwd.getpwnam(name)
        require(state["users"][name]==[account.pw_uid,account.pw_gid] and account.pw_uid>0,"安装账号身份已变化")
    require(re.fullmatch(r"[A-Za-z0-9_-]{22}",state.get("instance","")) is not None,"安装实例无效")

def installed_state():
    raw,_=safe_path(FILES['installation/install-state.json'],0,0o600)
    state=json.loads(raw)
    check_identity(state)
    require(re.fullmatch(r'[A-Za-z0-9_-]{22}',state.get('instance','')) is not None,'安装身份无效')
    require(re.fullmatch(r'[a-f0-9]{64}',state.get('binary_sha256','')) is not None,'安装程序摘要无效')
    require(isinstance(state.get('vpn'),bool) and state.get('cores') in ([],['xray'],['sing-box'],['xray','sing-box']),'安装模式未知')
    users=state.get('users',{})
    expected=install.USERS if state['vpn'] else install.USERS[:1]
    require(set(users)==set(expected),'专用身份登记不完整')
    for name in expected:
        account=install.pwd.getpwnam(name)
        require(users[name]==[account.pw_uid,account.pw_gid] and account.pw_uid>0,'安装账号与登记身份不匹配')
    raw,_=safe_path(FILES['binary/miku-gate'],0,0o755)
    require(hashlib.sha256(raw).hexdigest()==state['binary_sha256'],'安装程序已漂移，不自动备份为可信状态')
    helpers=state.get('maintenance_sha256')
    if helpers is not None:
        require(isinstance(helpers,dict) and set(helpers)=={'miku-install.py','miku-maintain.py'},'维护协调器登记不完整')
        for name,expected_hash in helpers.items():
            raw,_=safe_path(FILES['binary/miku-gate'].with_name(name),0,0o644)
            require(hashlib.sha256(raw).hexdigest()==expected_hash,'维护协调器已漂移，不自动认定可信')
    require(DATA.resolve(strict=True)==DATA and DATA.stat().st_uid==users['miku-gate'][0] and stat.S_IMODE(DATA.stat().st_mode)==0o700,'数据目录归属异常')
    sources=state.get('core_sources',[])
    require(isinstance(sources,list) and {r.get('core') for r in sources}==set(state['cores']),'核心来源登记不完整')
    for source in sources:
        core=source['core'];raw,_=safe_path(FILES['binary/miku-gate'].with_name(core),0,0o755,128<<20)
        require(hashlib.sha256(raw).hexdigest()==source.get('binary_sha256'),'核心程序漂移，不能按旧来源回滚')
    return state

def service_active(unit):
    require(unit in ('miku-gate.service','miku-gate-vpn.service','miku-gate-certificate.service','miku-gate-certificate.timer'),'服务状态仅固定已登记名称')
    command=[install.trusted_tool('systemctl'),'show',unit,'--property=LoadState,ActiveState,MainPID']
    result=subprocess.run(command,stdout=subprocess.PIPE,stderr=subprocess.PIPE,cwd='/',env=install.ENV,timeout=15)
    require(len(result.stdout)<=4096 and len(result.stderr)<=4096,'服务状态输出异常，不能确认已停止')
    fields={line.split('=',1)[0]:line.split('=',1)[1] for line in result.stdout.decode().splitlines() if '=' in line}
    require(set(fields)=={'LoadState','ActiveState','MainPID'} and fields['MainPID'].isdecimal(),'服务状态格式未知，不能确认已停止')
    if result.returncode!=0:
        require(fields['LoadState']=='not-found' and fields['ActiveState']=='inactive' and fields['MainPID']=='0','systemctl无法确认确切单元已停止')
    return fields['ActiveState'] in ('active','activating','deactivating','reloading') or int(fields['MainPID'])>0

def stop_services(state):
    units=['miku-gate.service']+(['miku-gate-vpn.service'] if state['vpn'] else [])
    active=[u for u in units if service_active(u)]
    for unit in units:
        # Interrupted service-only removal may already have deleted an inactive
        # unit file. Do not make a missing unit prevent paired recovery.
        if FILES['units/'+unit].exists() or service_active(unit):
            install.run([install.trusted_tool('systemctl'),'stop',unit],timeout=35)
        require(not service_active(unit),'服务未确认停止，不能做配套快照')
    return active

def restart_services(state,active):
    order=(['miku-gate-vpn.service'] if state['vpn'] else [])+['miku-gate.service']
    for unit in order:
        if unit in active:install.run([install.trusted_tool('systemctl'),'start',unit],timeout=35)
    if 'miku-gate.service' in active:
        certificate=state['certificate_summary']
        install.verify_served_management(state,DATA/'management/certificate.pem',certificate)
        if state['vpn']:install.verify_vpn_executor(FILES['binary/miku-gate'],state['instance'],state['users'])

def inspect_database(state):
    uid=state["users"]["miku-gate"][0]
    # Inspect through the same ordinary identity, so sqlite cannot leave a
    # root-owned WAL/SHM file in the control account's private data directory.
    inspector=r"""import json,sqlite3,sys
connection=sqlite3.connect('file:'+sys.argv[1]+'?mode=ro',uri=True)
try:
 schema=connection.execute('PRAGMA user_version').fetchone()[0]
 encrypted=sum(connection.execute('SELECT count(*) FROM '+table).fetchone()[0] for table in ('upstreams','vpn_configs','managed_ingress'))+connection.execute("SELECT count(*) FROM metadata WHERE key LIKE 'panel-backup-v1:%'").fetchone()[0]
 identity=connection.execute("SELECT value FROM metadata WHERE key='instance_id'").fetchone()[0]
 valid=connection.execute('PRAGMA quick_check').fetchone()[0]=='ok'
 print(json.dumps({'schema':schema,'encrypted_count':encrypted,'instance':identity,'integrity_valid':valid}))
finally:connection.close()
"""
    gid=state['users']['miku-gate'][1]
    examined=json.loads(install.run([install.trusted_tool('setpriv'),'--reuid='+str(uid),'--regid='+str(gid),'--clear-groups','--no-new-privs',install.trusted_tool('python3'),'-B','-c',inspector,str(DATA/'miku-gate.db')]))
    require(examined['instance']==state['instance'] and examined['schema'] in (6,7) and examined['integrity_valid'],'数据库身份/schema/完整性不匹配')
    return examined

def collect(state):
    uid=state['users']['miku-gate'][0]
    expected={'miku-gate.db',*DATABASE[1:],*CERT_DIRS}
    require(all(p.name in expected for p in DATA.iterdir()),'数据目录有未登记资源，拒绝不完整备份')
    resources=[]
    for member,path in FILES.items():
        if member.endswith('miku-gate-vpn.service') and not state['vpn']:continue
        if '/miku-gate-certificate.' in member and not state.get('certificate_automation',{}).get('enabled'):continue
        raw,info=safe_path(path,0,0o755 if member=='binary/miku-gate' else 0o600 if member.endswith('install-state.json') else 0o644)
        resources.append((member,raw,info.st_uid,info.st_gid,stat.S_IMODE(info.st_mode)))
    for name in state.get('maintenance_sha256',{}):
        raw,info=safe_path(FILES['binary/miku-gate'].with_name(name),0,0o644)
        resources.append(('binary/'+name,raw,info.st_uid,info.st_gid,0o644))
    examined=inspect_database(state)
    schema,counts=examined['schema'],examined['encrypted_count']
    for name in DATABASE:
        p=DATA/name
        if not p.exists():continue
        raw,info=safe_path(p,uid,0o600)
        resources.append(('data/'+name,raw,info.st_uid,info.st_gid,0o600))
    require(any(r[0]=='data/miku-gate.db' for r in resources),'数据库缺失，不备份假空数据')
    require(counts==0 or any(r[0]=='data/miku-gate.db.key' for r in resources),'有加密素材但缺配套.key，拒绝假恢复备份')
    for directory in CERT_DIRS:
        p=DATA/directory
        if not p.exists():continue
        info=p.lstat()
        require(p.resolve(strict=True)==p and stat.S_ISDIR(info.st_mode) and info.st_uid==uid and stat.S_IMODE(info.st_mode)==0o700,'证书副本目录归属异常')
        entries=list(p.iterdir())
        require({f.name for f in entries}=={'certificate.pem','private.key'},'证书副本目录不完整或含未知资源')
        for f in entries:
            raw,info=safe_path(f,uid,0o600)
            resources.append(('data/'+directory+'/'+f.name,raw,info.st_uid,info.st_gid,0o600))
    require(sum(len(r[1]) for r in resources)<=MAX_TOTAL,'备份超过当前有界预算，停止而非生成不完整归档')
    return resources,schema,counts

def create_archive(destination,resources,manifest):
    names=[r[0] for r in resources]
    require(len(names)==len(set(names)) and len(names)<=24,'重复/过多备份资源')
    for name in names:
        require(re.fullmatch(r'[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)*',name) is not None and '..' not in name.split('/'),'归档成员路径无效')
    records=[{'name':n,'bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest(),'uid':uid,'gid':gid,'mode':mode} for n,raw,uid,gid,mode in resources]
    safe_manifest={**manifest,'files':records,'restore_verified':False}
    manifest_raw=json.dumps(safe_manifest,ensure_ascii=False,sort_keys=True).encode()
    destination=Path(destination)
    require(destination.is_absolute() and not destination.exists() and not destination.is_symlink(),'只允许全新绝对备份文件，不覆盖')
    parent=destination.parent
    require(parent.resolve(strict=True)==parent,'备份父目录含链接')
    info=parent.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid==os.geteuid() and stat.S_IMODE(info.st_mode)==0o700,'备份父目录必须当前账号拥有0700')
    # Hold the checked parent descriptor through exclusive creation, so a
    # privileged write is never redirected after pathname permission checks.
    root_fd=os.open('/',os.O_RDONLY|os.O_DIRECTORY|os.O_CLOEXEC)
    try:
        for component in destination.parent.parts[1:]:
            next_fd=os.open(component,os.O_RDONLY|os.O_DIRECTORY|os.O_CLOEXEC|os.O_NOFOLLOW,dir_fd=root_fd)
            os.close(root_fd);root_fd=next_fd
        parent_info=os.fstat(root_fd)
        require(os.path.samestat(info,parent_info),'备份父目录在检查后被替换')
        fd=os.open(destination.name,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW|os.O_CLOEXEC,0o600,dir_fd=root_fd)
    finally:os.close(root_fd)
    with os.fdopen(fd,'wb') as output:
        os.fchmod(output.fileno(),0o600)
        with tarfile.open(fileobj=output,mode='w:gz') as archive:
            for name,raw,uid,gid,mode in [*resources,('BACKUP.json',manifest_raw,0,0,0o600)]:
                item=tarfile.TarInfo(name);item.type=tarfile.REGTYPE;item.mode=mode;item.uid=uid;item.gid=gid;item.size=len(raw);item.mtime=int(time.time());archive.addfile(item,io.BytesIO(raw))
        output.flush();os.fsync(output.fileno());created_info=os.fstat(output.fileno())
    # Verify archive contents and every confidential member without extraction.
    expected={r[0]:r[1] for r in resources};expected['BACKUP.json']=manifest_raw
    with tarfile.open(destination,'r:gz') as archive:
        members=archive.getmembers()
        require(len(members)==len(expected) and {m.name for m in members}==set(expected),'归档成员验证失败')
        for item in members:
            require(item.isfile() and not item.issym() and item.size==len(expected[item.name]),'归档类型/大小异常')
            require(archive.extractfile(item).read(item.size+1)==expected[item.name],'归档内容校验失败')
    with os.fdopen(os.open(destination,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC),'rb') as checked:
        require(os.path.samestat(created_info,os.fstat(checked.fileno())),'归档读回期间被替换')
        archive_raw=checked.read(MAX_ARCHIVE+1)
    require(len(archive_raw)<=MAX_ARCHIVE,'归档压缩大小超限')
    return {'archive_sha256':hashlib.sha256(archive_raw).hexdigest(),'file':str(destination),'member_count':len(expected),'instance':manifest['instance'],'schema':manifest['schema'],'contains_private_keys':True,'restore_verified':False}

def run_backup(args,state):
    require(args.confirm_backup and args.confirm_stop and args.output,'备份需--confirm-stop --confirm-backup及新文件--output，明确停机；不暗中升级/恢复')
    # Validate destination before stopping known units. Do not create arbitrary
    # parent directories or remove existing files to force a backup through.
    out=Path(args.output);info=out.parent.lstat()
    require(out.is_absolute() and out.parent.resolve(strict=True)==out.parent and info.st_uid==0 and stat.S_IMODE(info.st_mode)==0o700 and not out.exists() and not out.is_symlink(),'先准备root0700备份目录及全新文件')
    active=[];failure=None;result=None
    try:
        units=['miku-gate.service']+(['miku-gate-vpn.service'] if state['vpn'] else [])
        active=[u for u in units if service_active(u)]
        stop_services(state)
        # A manually started control process would still hold the data lease.
        # Refuse rather than archiving a database concurrent with unknown work.
        import fcntl
        lock_path=DATA/'.serve.lock'
        safe_path(lock_path,state['users']['miku-gate'][0],0o600)
        lease=os.open(lock_path,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
        try:
            fcntl.flock(lease,fcntl.LOCK_EX|fcntl.LOCK_NB)
            resources,schema,count=collect(state)
            result=create_archive(out,resources,{'format':1,'instance':state['instance'],'schema':schema,'binary_sha256':state['binary_sha256'],'encrypted_record_count':count,'core_provenance':state['core_sources']})
        finally:os.close(lease)
    except BaseException as exc:failure=exc
    finally:
        if not args.leave_stopped:
            try:restart_services(state,active)
            except BaseException as exc:
                print('BACKUP_RESTART_UNCONFIRMED '+type(exc).__name__,file=sys.stderr)
                if failure is None:failure=exc
    if failure is not None:raise failure
    result['service_restart_verified']=not args.leave_stopped
    print('MIKU_BACKUP_RESULT '+json.dumps(result,ensure_ascii=False,sort_keys=True))


@contextlib.contextmanager
def parent_fd(path,uid=0):
    path=Path(path)
    require(path.is_absolute() and path.parent.resolve(strict=True)==path.parent,'固定目标父路径含链接')
    fd=os.open('/',os.O_RDONLY|os.O_DIRECTORY|os.O_CLOEXEC)
    try:
        for component in path.parent.parts[1:]:
            child=os.open(component,os.O_RDONLY|os.O_DIRECTORY|os.O_CLOEXEC|os.O_NOFOLLOW,dir_fd=fd)
            os.close(fd);fd=child
            info=os.fstat(fd)
            require(info.st_uid in (0,uid) and not info.st_mode&0o022,'固定目标父目录归属/权限不可信')
        yield fd
    finally:os.close(fd)

def replace_owned(path,raw,uid,gid,mode):
    require(len(raw)<=MAX_FILE,'固定目标替换超出预算')
    # Services are stopped and the data lease/global maintenance lock are held.
    # Stage and rename in the checked same-directory descriptor, not shell mv.
    with parent_fd(path,uid) as directory:
        try:
            previous=os.stat(path.name,dir_fd=directory,follow_symlinks=False)
            require(stat.S_ISREG(previous.st_mode) and previous.st_nlink==1 and previous.st_uid==uid,'固定目标不能覆盖未知资源')
        except FileNotFoundError:previous=None
        pending='.miku-maintain-'+path.name+'.pending'
        fd=os.open(pending,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW|os.O_CLOEXEC,mode,dir_fd=directory)
        try:
            with os.fdopen(fd,'wb') as output:
                os.fchmod(output.fileno(),mode);os.fchown(output.fileno(),uid,gid)
                output.write(raw);output.flush();os.fsync(output.fileno())
            current=os.stat(path.name,dir_fd=directory,follow_symlinks=False) if previous is not None else None
            require(previous is None or os.path.samestat(previous,current),'固定目标在写入期间被替换')
            os.replace(pending,path.name,src_dir_fd=directory,dst_dir_fd=directory)
            os.fsync(directory)
        finally:
            try:os.unlink(pending,dir_fd=directory)
            except FileNotFoundError:pass

def remove_owned(path,uid,mode):
    with parent_fd(path,uid) as directory:
        try:info=os.stat(path.name,dir_fd=directory,follow_symlinks=False)
        except FileNotFoundError:return
        require(stat.S_ISREG(info.st_mode) and info.st_nlink==1 and info.st_uid==uid and stat.S_IMODE(info.st_mode)==mode,'只删除确切已知普通文件，不删除链接/目录')
        os.unlink(path.name,dir_fd=directory);os.fsync(directory)

@contextlib.contextmanager
def maintenance_lock():
    import fcntl
    with parent_fd(MAINTENANCE_LOCK) as directory:
        fd=os.open(MAINTENANCE_LOCK.name,os.O_RDWR|os.O_CREAT|os.O_NOFOLLOW|os.O_CLOEXEC,0o600,dir_fd=directory)
    try:
        info=os.fstat(fd);require(stat.S_ISREG(info.st_mode) and info.st_nlink==1 and info.st_uid==0 and stat.S_IMODE(info.st_mode)==0o600,'维护锁归属异常')
        fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
        yield
    finally:os.close(fd)

@contextlib.contextmanager
def data_lease(state):
    import fcntl
    path=DATA/'.serve.lock';safe_path(path,state['users']['miku-gate'][0],0o600)
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
    try:
        fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
        yield
    finally:os.close(fd)

def destinations(state):
    uid,gid=state['users']['miku-gate']
    targets={name:(path,0,0,0o755 if name=='binary/miku-gate' else 0o600 if name.endswith('install-state.json') else 0o644) for name,path in FILES.items() if (state['vpn'] or not name.endswith('miku-gate-vpn.service')) and (state.get('certificate_automation',{}).get('enabled') or '/miku-gate-certificate.' not in name)}
    for name in ('miku-install.py','miku-maintain.py'):targets['binary/'+name]=(FILES['binary/miku-gate'].with_name(name),0,0,0o644)
    for name in DATABASE:targets['data/'+name]=(DATA/name,uid,gid,0o600)
    for directory in CERT_DIRS:
        for name in ('certificate.pem','private.key'):targets['data/'+directory+'/'+name]=(DATA/directory/name,uid,gid,0o600)
    return targets

def decode_snapshot(raw,state):
    # Iterative bounded reads, never extract/extractall and never use archive
    # names as filesystem destinations. Every name maps to a fixed allowlist.
    targets=destinations(state);members={};total=0
    with gzip.GzipFile(fileobj=io.BytesIO(raw),mode='rb') as zipped:
        unpacked=zipped.read(MAX_TOTAL+(2<<20)+1)
    require(len(unpacked)<=MAX_TOTAL+(2<<20),'备份压缩膨胀超限')
    with tarfile.open(fileobj=io.BytesIO(unpacked),mode='r:') as archive:
        for item in archive:
            require(len(members)<24 and item.name not in members,'备份成员重复/过多')
            require(item.name in targets or item.name=='BACKUP.json','备份含未登记路径')
            require(item.isfile() and not item.issym() and not item.islnk() and 0<=item.size<=(65536 if item.name=='BACKUP.json' else MAX_FILE),'备份成员类型/大小非法')
            total+=item.size;require(total<=MAX_TOTAL+65536,'备份解压总大小超限')
            content=archive.extractfile(item).read(item.size+1);require(len(content)==item.size,'备份成员截断')
            members[item.name]=(content,item)
    require('BACKUP.json' in members and 'installation/install-state.json' in members,'备份缺实例清单')
    manifest=json.loads(members.pop('BACKUP.json')[0]);old=json.loads(members['installation/install-state.json'][0])
    require(isinstance(manifest,dict) and manifest.get('format')==1 and manifest.get('schema') in (6,7) and manifest.get('instance')==state['instance'],'备份格式/schema/实例不匹配')
    require(isinstance(old,dict),'备份安装登记无效');check_identity(old)
    for name in ('instance','users','vpn','cores','origin','port','domain','ip','certificate_automation'):
        require(old.get(name)==state.get(name),'只允许同一安装的配套回滚，不迁移账号/地址/核心')
    required={'installation/install-state.json','binary/miku-gate','binary/miku-install.py','binary/miku-maintain.py','units/miku-gate.service','data/miku-gate.db','data/management/certificate.pem','data/management/private.key'}
    if state['vpn']:required.add('units/miku-gate-vpn.service')
    if state.get('certificate_automation',{}).get('enabled'):required.update('units/'+name for name in install.certificate_units())
    require(isinstance(manifest.get('encrypted_record_count'),int) and manifest['encrypted_record_count']>=0,'备份加密行登记无效')
    if manifest['encrypted_record_count']>0:required.add('data/miku-gate.db.key')
    require(required<=set(members),'备份缺配套程序/维护工具/数据库/key/证书')
    records=manifest.get('files');require(isinstance(records,list) and all(isinstance(r,dict) and isinstance(r.get('name'),str) for r in records) and len(records)==len(members),'备份成员清单数量错误')
    indexed={r['name']:r for r in records};require(len(indexed)==len(records) and set(indexed)==set(members),'备份清单重复或不完整')
    result=[]
    for name,(content,item) in members.items():
        _,uid,gid,mode=targets[name];record=indexed[name]
        require(record.get('bytes')==len(content) and record.get('sha256')==hashlib.sha256(content).hexdigest(),'备份成员长度/摘要不匹配')
        require((record.get('uid'),record.get('gid'),record.get('mode'))==(uid,gid,mode) and (item.uid,item.gid,item.mode)==(uid,gid,mode),'备份成员不允许改变固定身份/权限')
        result.append((name,content,uid,gid,mode))
    require(hashlib.sha256(members['binary/miku-gate'][0]).hexdigest()==old.get('binary_sha256')==manifest.get('binary_sha256'),'备份程序来源摘要不匹配')
    helpers=old.get('maintenance_sha256');require(isinstance(helpers,dict) and set(helpers)=={'miku-install.py','miku-maintain.py'},'备份维护协调器不完整；旧无协调器备份不自动升级恢复')
    for name,expected in helpers.items():require(hashlib.sha256(members['binary/'+name][0]).hexdigest()==expected,'备份协调器摘要不匹配')
    expected_units=install.unit_text(FILES['binary/miku-gate'],DATA,old['origin'],old['port'],DATA/'management/certificate.pem',DATA/'management/private.key',old['certificate_summary']['bootstrap'],old['vpn'],old['instance'],old['users'],{core:FILES['binary/miku-gate'].with_name(core) for core in old['cores']})
    if state.get('certificate_automation',{}).get('enabled'):expected_units.update(install.certificate_units())
    for name,unit in expected_units.items():
        legacy=unit.replace('ConditionPathExists=!/etc/miku-gate/maintenance-block\n','')
        require(members['units/'+name][0] in (unit.encode(),legacy.encode()),'备份服务不是已知固定安装模板，拒绝root命令注入')
    return result,old,manifest

def read_snapshot(path,expected,state):
    require(isinstance(expected,str) and re.fullmatch(r'[a-f0-9]{64}',expected) is not None,'回滚必须明确提供备份归档SHA256')
    raw,_=safe_path(Path(path),0,0o600,MAX_ARCHIVE)
    require(stat.S_IMODE(Path(path).parent.stat().st_mode)==0o700,'回滚归档须root私有0700目录')
    require(hashlib.sha256(raw).hexdigest()==expected,'回滚归档与明确摘要不符')
    return decode_snapshot(raw,state)

def snapshot(destination,state):
    resources,schema,count=collect(state)
    result=create_archive(destination,resources,{'format':1,'instance':state['instance'],'schema':schema,'binary_sha256':state['binary_sha256'],'encrypted_record_count':count,'core_provenance':state['core_sources']})
    return resources,result

def restore_resources(resources,state):
    targets=destinations(state);names={r[0] for r in resources}
    require(names<=set(targets),'恢复不能写未登记路径')
    # Do not replace the lease inode: this process owns it and stopped writers
    # must continue to see the same lock until the entire paired restore ends.
    for name,raw,uid,gid,mode in sorted(resources,key=lambda r:r[0]=='installation/install-state.json'):
        if name=='data/.serve.lock':continue
        path,expected_uid,expected_gid,expected_mode=targets[name]
        require((uid,gid,mode)==(expected_uid,expected_gid,expected_mode),'恢复不能信任归档提供的身份/权限')
        if name in ('units/miku-gate.service','units/miku-gate-vpn.service') and b'ConditionPathExists=!/etc/miku-gate/maintenance-block\n' not in raw:
            # Insert after Description to preserve the canonical unit template.
            lines=raw.splitlines(keepends=True);index=next(i for i,line in enumerate(lines) if line.startswith(b'Description='))+1
            lines.insert(index,b'ConditionPathExists=!/etc/miku-gate/maintenance-block\n');raw=b''.join(lines)
        replace_owned(path,raw,uid,gid,mode)
    for name in ('miku-gate.db-wal','miku-gate.db-shm','miku-gate.db.key'):
        if 'data/'+name not in names:remove_owned(DATA/name,state['users']['miku-gate'][0],0o600)


def write_journal(state,active,backup,operation):
    require(not JOURNAL.exists() and not JOURNAL.is_symlink(),'已有维护事务，先明确recover，不覆盖')
    data={'format':1,'operation':operation,'state':state,'active':active,'backup':backup['file'],'archive_sha256':backup['archive_sha256']}
    raw=json.dumps(data,sort_keys=True).encode();require(len(raw)<=65536,'维护日志过大')
    install.exclusive(JOURNAL,raw,0o600)
    with parent_fd(JOURNAL) as directory:os.fsync(directory)

def check_output(output):
    path=Path(output);info=path.parent.lstat()
    require(path.is_absolute() and path.parent.resolve(strict=True)==path.parent and info.st_uid==0 and stat.S_IMODE(info.st_mode)==0o700 and not path.exists() and not path.is_symlink(),'先准备root0700目录及全新配套备份文件')
    return path

def protected_data_check(state):
    uid,gid=state["users"]["miku-gate"]
    raw=install.run([install.trusted_tool("setpriv"),"--reuid="+str(uid),"--regid="+str(gid),"--clear-groups","--no-new-privs",str(FILES["binary/miku-gate"]),"data-check","--data="+str(DATA)])
    checked=json.loads(raw)
    require(checked.get("instance")==state["instance"] and checked.get("schema") in (6,7) and checked.get("integrity_valid") and checked.get("protected_records_verified"),"真实数据库/key加密记录配套检查未通过")
    return checked

def validate_recovered(state):
    current=installed_state();require(current['binary_sha256']==state['binary_sha256'],'配套恢复后程序登记不一致')
    protected_data_check(state)

def apply_release(binary,meta,state):
    raw=binary.read_bytes();require(hashlib.sha256(raw).hexdigest()==meta['binary_sha256'],'升级候选程序在校验后漂移')
    root=binary.parent.parent;helpers={}
    for name in ('miku-install.py','miku-maintain.py'):
        _,content=install.validate_binary(str(root/'scripts'/name),meta['script_sha256'][name]);helpers[name]=content
    new=json.loads(json.dumps(state))
    new.update(binary_sha256=meta['binary_sha256'],maintenance_sha256={name:hashlib.sha256(content).hexdigest() for name,content in helpers.items()},distribution={name:meta[name] for name in ('source','platform','version')})
    uid,gid=state['users']['miku-gate']
    # Execute the staged candidate only as the ordinary control account, after
    # the root-owned digest has been checked. Root0700 release dirs cannot be
    # traversed by the account, so stage in the registered root-owned lib first.
    candidate=FILES['binary/miku-gate'].with_name('.miku-version-candidate')
    require(not candidate.exists() and not candidate.is_symlink(),'版本检查临时程序已存在，拒绝覆盖')
    install.exclusive(candidate,raw,0o755)
    try:
        version=install.run([install.trusted_tool('setpriv'),'--reuid='+str(uid),'--regid='+str(gid),'--clear-groups','--no-new-privs',str(candidate),'version']).decode().strip()
    finally:remove_owned(candidate,0,0o755)
    require('v'+version==meta['version'],'候选程序版本与发行包登记不符')
    replace_owned(FILES['binary/miku-gate'],raw,0,0,0o755)
    for name,content in helpers.items():replace_owned(FILES['binary/miku-gate'].with_name(name),content,0,0,0o644)
    replace_owned(FILES['installation/install-state.json'],json.dumps(new,ensure_ascii=False,indent=2).encode(),0,0,0o600)
    return new

def set_startup_block():
    if STARTUP_BLOCK.exists():safe_path(STARTUP_BLOCK,0,0o600)
    else:install.exclusive(STARTUP_BLOCK,b"Miku paired maintenance in progress\n",0o600)

def install_startup_guard(state):
    units=install.unit_text(FILES["binary/miku-gate"],DATA,state["origin"],state["port"],DATA/"management/certificate.pem",DATA/"management/private.key",state["certificate_summary"]["bootstrap"],state["vpn"],state["instance"],state["users"],{core:FILES["binary/miku-gate"].with_name(core) for core in state["cores"]})
    for name,text in units.items():
        path=FILES["units/"+name];raw,_=safe_path(path,0,0o644)
        legacy=text.replace("ConditionPathExists=!/etc/miku-gate/maintenance-block\n","").encode()
        require(raw in (text.encode(),legacy),"既有Miku单元已被修改，拒绝自动改写")
        replace_owned(path,text.encode(),0,0,0o644)
    install.run([install.trusted_tool("systemctl"),"daemon-reload"])

def recover_transaction():
    raw,_=safe_path(JOURNAL,0,0o600);journal=json.loads(raw)
    require(journal.get('format')==1 and journal.get('operation') in ('upgrade','rollback','certificate-refresh','uninstall-services'),'恢复事务格式无效')
    state=journal['state'];check_identity(state)
    units=['miku-gate.service']+(['miku-gate-vpn.service'] if state['vpn'] else [])
    require(isinstance(journal.get('active'),list) and set(journal['active'])<=set(units),'恢复日志不允许操作未知单元')
    resources,old,_=read_snapshot(journal['backup'],journal['archive_sha256'],state)
    set_startup_block();stop_services(state)
    with data_lease(state):
        # Power loss before rename may leave only these exact staged filenames.
        for path,uid,_,mode in destinations(state).values():
            if not path.parent.exists():continue
            remove_owned(path.with_name('.miku-maintain-'+path.name+'.pending'),uid,mode)
        remove_owned(FILES['binary/miku-gate'].with_name('.miku-version-candidate'),0,0o755)
        restore_resources(resources,old);validate_recovered(old)
    try:
        remove_owned(STARTUP_BLOCK,0,0o600)
        install.run([install.trusted_tool('systemctl'),'daemon-reload'])
        if journal['operation']=='uninstall-services':
            enabled=state.get('recovery_enabled_units',[])
            require(isinstance(enabled,list) and set(enabled)<=set(registered_units(old)),'恢复只允许原已登记启用单元')
            for name in enabled:install.run([install.trusted_tool('systemctl'),'enable',name])
        restart_services(old,journal['active'])
        if journal['operation']=='uninstall-services' and 'miku-gate-certificate.timer' in state.get('recovery_enabled_units',[]):install.run([install.trusted_tool('systemctl'),'start','miku-gate-certificate.timer'])
        remove_owned(JOURNAL,0,0o600)
    except BaseException:
        set_startup_block();stop_services(old)
        raise BackupError('RECOVERY_RESTART_UNCONFIRMED：保留日志与启动保护，服务停止。') from None
    print('MIKU_RECOVERY_RESULT '+json.dumps({'paired_restore_verified':True,'instance':old['instance'],'service_restart_verified':True},sort_keys=True))

def run_change(args,state):
    require(args.confirm_stop and args.output and not args.leave_stopped and not args.confirm_backup and not args.confirm_recover,'升级/回滚需明确停机与全新安全备份，不能跳过恢复核验')
    out=check_output(args.output)
    target=None
    if args.command=='upgrade':
        require(args.confirm_upgrade and args.distribution and not args.archive and not args.archive_sha256,'升级需--distribution --confirm-upgrade，不混用回滚参数')
        binary,_,meta=install.distribution_binary(args.distribution,install.platform_gate())
        require(meta['binary_sha256']!=state['binary_sha256'],'同一程序无需重复升级')
    else:
        require(args.confirm_rollback and args.archive and args.archive_sha256 and not args.distribution,'回滚需--archive --archive-sha256 --confirm-rollback')
        target=read_snapshot(args.archive,args.archive_sha256,state)
    units=['miku-gate.service']+(['miku-gate-vpn.service'] if state['vpn'] else [])
    active=[unit for unit in units if service_active(unit)]
    require('miku-gate.service' in active,'先明确启动已知管理服务；不把未启动安装的程序替换当已验证升级')
    protected_data_check(state)
    original=None;new=state;backup=None;changed=False
    try:
        stop_services(state)
        with data_lease(state):
            original,backup=snapshot(out,state)
            write_journal(state,active,backup,args.command)
            changed=True
            set_startup_block();install_startup_guard(state)
            if args.command=='upgrade':new=apply_release(binary,meta,state)
            else:
                resources,new,_=target;restore_resources(resources,new)
            validate_recovered(new)
            install_startup_guard(new)
        remove_owned(STARTUP_BLOCK,0,0o600)
        install.run([install.trusted_tool('systemctl'),'daemon-reload'])
        restart_services(new,active)
        remove_owned(JOURNAL,0,0o600)
    except BaseException as failure:
        if changed:
            try:
                set_startup_block();stop_services(state)
                with data_lease(state):restore_resources(original,state);validate_recovered(state)
                remove_owned(STARTUP_BLOCK,0,0o600)
                install.run([install.trusted_tool('systemctl'),'daemon-reload'])
                restart_services(state,active)
                remove_owned(JOURNAL,0,0o600)
            except BaseException:
                try:set_startup_block();stop_services(state)
                except BaseException:pass
                raise BackupError('MIKU_RECOVERY_REQUIRED：保留配套备份与root私有事务日志；服务停止，明确运行recover。') from None
            raise BackupError('MIKU_CHANGE_FAILED_PAIRED_ROLLBACK_VERIFIED：已恢复更改前整套状态；失败类别='+type(failure).__name__) from None
        try:restart_services(state,active)
        except BaseException:raise BackupError('MIKU_CHANGE_STOPPED_RESTART_UNCONFIRMED：没有应用程序变更，请检查已知服务。') from None
        raise failure
    print('MIKU_CHANGE_RESULT '+json.dumps({'operation':args.command,'instance':new['instance'],'binary_sha256':new['binary_sha256'],'backup':backup['file'],'backup_sha256':backup['archive_sha256'],'paired_restore_verified':args.command=='rollback','service_restart_verified':True,'units_or_core_choices_changed':False,'live_gateway_connections_restored':False},sort_keys=True))


def certificate_candidate(state,renew=False):
    domain=state.get('domain')
    require(domain and not state['certificate_summary'].get('bootstrap'),'自动刷新仅已信任域名，不自动更换用户已信任的IP自签身份')
    if renew:require(state.get('certificate_automation',{}).get('renew'),'安装未授权自动Certbot续期；不读取或运行续期来源')
    found=install.source_certificate(FILES['binary/miku-gate'],domain)
    already_updated=found is not None and found[2]['leaf_sha256']!=state['certificate_summary']['leaf_sha256']
    if renew:
        require(state.get('certificate_automation',{}).get('renew'),'安装未授权自动Certbot续期；先配置授权')
        expiry=datetime.datetime.fromisoformat(state['certificate_summary']['not_after'].replace('Z','+00:00'))
        if not already_updated and expiry-datetime.datetime.now(datetime.timezone.utc)<=datetime.timedelta(days=30):
            # Standalone installations must never stop an existing webserver to
            # renew. For externally managed webroot/DNS certs, sync only by default.
            require(install.port_available(80),'80端口已有服务，拒绝自动standalone续期；原服务未停止')
            install.run([install.trusted_tool('certbot'),'renew','--cert-name='+domain,'--quiet'],timeout=240)
            found=install.source_certificate(FILES['binary/miku-gate'],domain)
    require(found is not None,'未发现当前系统信任/同域名/钥匙匹配的证书，保持原副本')
    certificate,key,summary=found
    # Original renewal symlinks were resolved inside the known provider bound;
    # pin both files through trusted ownership/perms and exact content checks.
    cert_raw,_=safe_path(certificate,0,max_bytes=128<<10)
    key_raw,_=safe_path(key,0,max_bytes=128<<10)
    require(not key.stat().st_mode&0o077,'源私钥必须root私有')
    repeated=json.loads(install.run([str(FILES['binary/miku-gate']),'management','inspect','--origin='+state['origin'],'--certificate='+str(certificate),'--key='+str(key)]))
    require(repeated['leaf_sha256']==summary['leaf_sha256'] and repeated['system_trusted'] and not repeated['bootstrap'],'源证书在发现后漂移')
    return cert_raw,key_raw,summary

def scheduled_output():
    directory=FILES['installation/install-state.json'].parent/'certificate-updates'
    if not directory.exists():install.make_dir(directory,0o700)
    info=directory.lstat()
    require(directory.resolve(strict=True)==directory and info.st_uid==0 and stat.S_IMODE(info.st_mode)==0o700,'证书事务快照目录不可信')
    snapshots=list(directory.iterdir())
    require(all(p.is_file() and not p.is_symlink() and re.fullmatch(r'before-certificate-[0-9]+\.tar\.gz',p.name) for p in snapshots) and len(snapshots)<8,'证书私有快照达到8份或有未知文件，请先明确归档/清理；不自动删除密钥备份')
    return directory/('before-certificate-'+str(time.time_ns())+'.tar.gz')

def run_certificate_refresh(args,state):
    require(not any((args.archive,args.archive_sha256,args.distribution,args.confirm_backup,args.confirm_upgrade,args.confirm_rollback,args.confirm_recover,args.leave_stopped)),'证书刷新不能混用升级/回滚参数')
    automation=state.get('certificate_automation',{})
    if args.scheduled:
        require(automation.get('enabled') and not args.output and not args.confirm_stop and not args.confirm_certificate_refresh and not args.renew,'定时任务仅使用安装时明确授权模式')
        renew=automation.get('renew',False)
    else:
        require(args.confirm_certificate_refresh and args.confirm_stop and args.output,'证书刷新需明确--confirm-certificate-refresh --confirm-stop --output')
        renew=args.renew
    cert_raw,key_raw,summary=certificate_candidate(state,renew)
    if summary['leaf_sha256']==state['certificate_summary']['leaf_sha256']:
        print('MIKU_CERTIFICATE_RESULT '+json.dumps({'changed':False,'instance':state['instance'],'leaf_sha256':summary['leaf_sha256'],'services_restarted':False},sort_keys=True));return
    out=check_output(scheduled_output() if args.scheduled else args.output)
    units=['miku-gate.service']+(['miku-gate-vpn.service'] if state['vpn'] else [])
    active=[unit for unit in units if service_active(unit)]
    require('miku-gate.service' in active,'证书刷新只在已运行管理服务上验证，不自动启动原停用安装')
    original=None;changed=False;backup=None;updated_count=0
    new=json.loads(json.dumps(state));new['certificate_summary']=summary
    try:
        stop_services(state)
        with data_lease(state):
            original,backup=snapshot(out,state)
            write_journal(state,active,backup,'certificate-refresh');changed=True
            set_startup_block();install_startup_guard(state)
            uid,gid=state['users']['miku-gate']
            replace_owned(DATA/'management/certificate.pem',cert_raw,uid,gid,0o600)
            replace_owned(DATA/'management/private.key',key_raw,uid,gid,0o600)
        # The real CLI must acquire the SAME control lease itself. Root global
        # maintenance lock and stopped known services still protect the window.
        report=json.loads(install.run([install.trusted_tool('setpriv'),'--reuid='+str(uid),'--regid='+str(gid),'--clear-groups','--no-new-privs',str(FILES['binary/miku-gate']),'management','refresh-ingress','--data='+str(DATA),'--expected-instance='+state['instance'],'--expected-leaf='+state['certificate_summary']['leaf_sha256'],'--origin='+state['origin'],'--certificate='+str(DATA/'management/certificate.pem'),'--key='+str(DATA/'management/private.key'),'--confirm']))
        require(report['instance']==state['instance'] and report['leaf_sha256']==summary['leaf_sha256'] and report['identities_preserved'] and not report['private_material_returned'],'真实TLS入口刷新结果不一致')
        updated_count=report['updated_ingress_count']
        with data_lease(state):
            replace_owned(FILES['installation/install-state.json'],json.dumps(new,ensure_ascii=False,indent=2).encode(),0,0,0o600)
            validate_recovered(new)
        remove_owned(STARTUP_BLOCK,0,0o600)
        restart_services(new,active)
        remove_owned(JOURNAL,0,0o600)
    except BaseException as failure:
        if changed:
            try:
                set_startup_block();stop_services(state)
                with data_lease(state):restore_resources(original,state);validate_recovered(state)
                remove_owned(STARTUP_BLOCK,0,0o600)
                install.run([install.trusted_tool('systemctl'),'daemon-reload']);restart_services(state,active)
                remove_owned(JOURNAL,0,0o600)
            except BaseException:
                try:set_startup_block();stop_services(state)
                except BaseException:pass
                raise BackupError('CERTIFICATE_RECOVERY_REQUIRED：保留配套快照与日志，明确recover。') from None
            raise BackupError('CERTIFICATE_REFRESH_FAILED_PAIRED_ROLLBACK_VERIFIED：恢复旧证书/DB/key；失败类别='+type(failure).__name__) from None
        try:restart_services(state,active)
        except BaseException:raise BackupError('CERTIFICATE_RESTART_UNCONFIRMED：未应用新证书。') from None
        raise failure
    print('MIKU_CERTIFICATE_RESULT '+json.dumps({'changed':True,'instance':state['instance'],'leaf_sha256':summary['leaf_sha256'],'not_after':summary['not_after'],'updated_ingress_count':updated_count,'identities_preserved':True,'service_restart_verified':True,'business_sessions_auto_restarted':False,'backup_sha256':backup['archive_sha256']},sort_keys=True))


def configure_certificate_automation(args,state):
    require(args.confirm_certificate_automation and not any((args.output,args.archive,args.archive_sha256,args.distribution,args.confirm_stop,args.confirm_backup,args.confirm_upgrade,args.confirm_rollback,args.confirm_recover,args.confirm_certificate_refresh,args.scheduled,args.leave_stopped)),'定时更新授权需明确--confirm-certificate-automation，不混用其他变更')
    require(state.get('domain') and not state['certificate_summary'].get('bootstrap'),'不自动更换IP引导证书信任身份')
    require(not(args.disable_certificate_automation and args.renew),'停用不能同时授权续期')
    known=install.certificate_units();enabled=not args.disable_certificate_automation
    for name,text in known.items():
        path=FILES['units/'+name]
        if path.exists() or path.is_symlink():
            raw,_=safe_path(path,0,0o644);require(raw==text.encode(),'证书单元已有未知内容，拒绝接管')
        elif not enabled:raise BackupError('没有已登记定时单元，不冒充停用成功')
    new=json.loads(json.dumps(state));new['certificate_automation']={'enabled':enabled,'renew':bool(enabled and args.renew)}
    if enabled:
        install.trusted_tool('python3')
        if args.renew:
            source=state.get('certificate_source',{})
            require(source.get('mode')=='domain-issue' or '/etc/letsencrypt/archive/' in source.get('cert',''),'仅已有此域名Certbot来源支持明确续期；其他工具只能同步')
            install.trusted_tool('certbot')
        for name,text in known.items():
            path=FILES['units/'+name]
            if not path.exists():install.exclusive(path,text.encode(),0o644)
        install.run([install.trusted_tool('systemd-analyze'),'verify',*[str(FILES['units/'+name]) for name in known]])
        # Save explicit consent before allowing the fixed scheduled command.
        replace_owned(FILES['installation/install-state.json'],json.dumps(new,ensure_ascii=False,indent=2).encode(),0,0,0o600)
        install.run([install.trusted_tool('systemctl'),'daemon-reload'])
        install.run([install.trusted_tool('systemctl'),'enable','--now','miku-gate-certificate.timer'])
    else:
        install.run([install.trusted_tool('systemctl'),'disable','--now','miku-gate-certificate.timer'])
        replace_owned(FILES['installation/install-state.json'],json.dumps(new,ensure_ascii=False,indent=2).encode(),0,0,0o600)
    print('MIKU_CERTIFICATE_AUTOMATION '+json.dumps({'instance':state['instance'],'enabled':enabled,'renew_authorized':new['certificate_automation']['renew'],'management_service_restarted':False},sort_keys=True))


def registered_units(state):
    units=install.unit_text(FILES['binary/miku-gate'],DATA,state['origin'],state['port'],DATA/'management/certificate.pem',DATA/'management/private.key',state['certificate_summary']['bootstrap'],state['vpn'],state['instance'],state['users'],{core:FILES['binary/miku-gate'].with_name(core) for core in state['cores']})
    if state.get('certificate_automation',{}).get('enabled'):units.update(install.certificate_units())
    return units

def check_registered_units(state):
    units=registered_units(state)
    for name,text in units.items():
        raw,_=safe_path(FILES['units/'+name],0,0o644)
        legacy=text.replace('ConditionPathExists=!/etc/miku-gate/maintenance-block\n','').encode()
        require(raw in (text.encode(),legacy),'已有单元内容变化，拒绝服务卸载接管')
        overrides=install.run([install.trusted_tool('systemctl'),'show',name,'--property=DropInPaths','--value']).strip()
        require(not overrides,'已有单元drop-in自定义，先明确检查，不自动卸载')
    return units

def uninstall_services(args,state):
    require(args.confirm_uninstall_services and args.confirm_stop and args.output and not state.get('services_uninstalled'),'服务卸载需--confirm-uninstall-services --confirm-stop --output新私有快照')
    require(not any((args.archive,args.archive_sha256,args.distribution,args.confirm_backup,args.confirm_upgrade,args.confirm_rollback,args.confirm_recover,args.confirm_certificate_refresh,args.confirm_certificate_automation,args.disable_certificate_automation,args.confirm_reinstall_services,args.scheduled,args.renew,args.leave_stopped)),'服务卸载不混用其他变更')
    out=check_output(args.output);units=check_registered_units(state)
    active=[name for name in ('miku-gate.service','miku-gate-vpn.service') if name in units and service_active(name)]
    enabled=[]
    for name in units:
        value=install.run([install.trusted_tool('systemctl'),'show',name,'--property=UnitFileState','--value']).decode().strip()
        require(value in ('enabled','disabled','static','indirect'),'服务启用状态未知，拒绝卸载')
        if value=='enabled':enabled.append(name)
    # Stop future certificate triggers before snapshot; do not remove any data.
    if 'miku-gate-certificate.timer' in units:install.run([install.trusted_tool('systemctl'),'stop','miku-gate-certificate.timer'])
    original=None;changed=False
    try:
        stop_services(state)
        with data_lease(state):
            original,backup=snapshot(out,state)
            journal_state=json.loads(json.dumps(state));journal_state['recovery_enabled_units']=enabled
            write_journal(journal_state,active,backup,'uninstall-services');changed=True
            set_startup_block()
            for name in units:install.run([install.trusted_tool('systemctl'),'disable','--now',name])
            for name in units:remove_owned(FILES['units/'+name],0,0o644)
            new=json.loads(json.dumps(state));new['services_uninstalled']=True;new['previous_enabled_units']=enabled
            replace_owned(FILES['installation/install-state.json'],json.dumps(new,ensure_ascii=False,indent=2).encode(),0,0,0o600)
        install.run([install.trusted_tool('systemctl'),'daemon-reload'])
        remove_owned(STARTUP_BLOCK,0,0o600);remove_owned(JOURNAL,0,0o600)
    except BaseException:
        if changed:
            try:
                with data_lease(state):restore_resources(original,state);validate_recovered(state)
                remove_owned(STARTUP_BLOCK,0,0o600);install.run([install.trusted_tool('systemctl'),'daemon-reload'])
                for name in enabled:install.run([install.trusted_tool('systemctl'),'enable',name])
                restart_services(state,active)
                if 'miku-gate-certificate.timer' in enabled:install.run([install.trusted_tool('systemctl'),'start','miku-gate-certificate.timer'])
                remove_owned(JOURNAL,0,0o600)
            except BaseException:raise BackupError('UNINSTALL_RECOVERY_REQUIRED：数据/程序与事务快照保留，明确recover。') from None
        else:
            restart_services(state,active)
            if 'miku-gate-certificate.timer' in enabled:install.run([install.trusted_tool('systemctl'),'start','miku-gate-certificate.timer'])
        raise BackupError('服务卸载失败；未删除数据/程序/账号，旧服务状态已尝试恢复。') from None
    print('MIKU_UNINSTALL_RESULT '+json.dumps({'services_removed':list(units),'data_keys_certificates_retained':True,'binaries_accounts_retained':True,'backup_sha256':backup['archive_sha256'],'firewall_or_routes_changed':False},sort_keys=True))

def reinstall_services(args,state):
    require(args.confirm_reinstall_services and state.get('services_uninstalled') and not any((args.output,args.confirm_stop,args.archive,args.archive_sha256,args.distribution,args.confirm_backup,args.confirm_upgrade,args.confirm_rollback,args.confirm_recover,args.confirm_certificate_refresh,args.confirm_certificate_automation,args.disable_certificate_automation,args.confirm_uninstall_services,args.scheduled,args.renew,args.leave_stopped)),'恢复注册需明确--confirm-reinstall-services，且已有保留数据的服务卸载登记')
    units=registered_units(state)
    # No replacement of a newly occupied service file by this action.
    for name,text in units.items():
        path=FILES['units/'+name]
        if path.exists() or path.is_symlink():
            raw,_=safe_path(path,0,0o644);require(raw==text.encode(),'单元名称被新服务占用，不覆盖')
    set_startup_block()
    for name,text in units.items():
        path=FILES['units/'+name]
        if not path.exists():install.exclusive(path,text.encode(),0o644)
    install.run([install.trusted_tool('systemd-analyze'),'verify',*[str(FILES['units/'+name]) for name in units]])
    new=json.loads(json.dumps(state));new['services_uninstalled']=False
    install.run([install.trusted_tool('systemctl'),'daemon-reload']);remove_owned(STARTUP_BLOCK,0,0o600)
    # Keep services_uninstalled=True through any registration error so a retry
    # is possible; no service is started/enabled by re-registration.
    replace_owned(FILES['installation/install-state.json'],json.dumps(new,ensure_ascii=False,indent=2).encode(),0,0,0o600)
    print('MIKU_REINSTALL_RESULT '+json.dumps({'units_registered':list(units),'data_identity_preserved':True,'services_started':False,'startup_enable_changed':False},sort_keys=True))

def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command',choices=('status','backup','upgrade','rollback','recover','certificate-refresh','certificate-automation','uninstall-services','reinstall-services'))
    parser.add_argument('--output');parser.add_argument('--confirm-stop',action='store_true');parser.add_argument('--confirm-backup',action='store_true');parser.add_argument('--leave-stopped',action='store_true')
    parser.add_argument('--distribution');parser.add_argument('--confirm-upgrade',action='store_true')
    parser.add_argument('--archive');parser.add_argument('--archive-sha256');parser.add_argument('--confirm-rollback',action='store_true');parser.add_argument('--confirm-recover',action='store_true')
    parser.add_argument('--confirm-certificate-refresh',action='store_true');parser.add_argument('--renew',action='store_true');parser.add_argument('--scheduled',action='store_true')
    parser.add_argument('--confirm-certificate-automation',action='store_true');parser.add_argument('--disable-certificate-automation',action='store_true')
    parser.add_argument('--confirm-uninstall-services',action='store_true');parser.add_argument('--confirm-reinstall-services',action='store_true')
    args=parser.parse_args(argv)
    require(sys.platform=='linux' and os.geteuid()==0,'维护仅已授权Linux root协调，控制服务仍普通账号')
    if args.command=='status':
        require(not any((args.output,args.confirm_stop,args.confirm_backup,args.leave_stopped,args.distribution,args.archive,args.archive_sha256,args.confirm_upgrade,args.confirm_rollback,args.confirm_recover,args.confirm_certificate_refresh,args.renew,args.scheduled,args.confirm_certificate_automation,args.disable_certificate_automation,args.confirm_uninstall_services,args.confirm_reinstall_services)),'status只读，无变更参数')
        pending=JOURNAL.exists()
        state=installed_state() if not pending else json.loads(safe_path(JOURNAL,0,0o600)[0])['state']
        print(json.dumps({'instance':state['instance'],'origin':state['origin'],'vpn':state['vpn'],'cores':state['cores'],'control_active':False if state.get('services_uninstalled') else service_active('miku-gate.service'),'backup_required_before_upgrade':True,'upgrade_restore_implemented':True,'recovery_required':pending,'certificate_automation':state.get('certificate_automation',{'enabled':False,'renew':False}),'services_uninstalled':state.get('services_uninstalled',False)},ensure_ascii=False));return
    with maintenance_lock():
        if args.command=='recover':
            require(args.confirm_recover and args.confirm_stop and not any((args.output,args.archive,args.archive_sha256,args.distribution,args.confirm_backup,args.confirm_upgrade,args.confirm_rollback,args.leave_stopped,args.confirm_certificate_refresh,args.renew,args.scheduled,args.confirm_certificate_automation,args.disable_certificate_automation,args.confirm_uninstall_services,args.confirm_reinstall_services)),'recover需明确--confirm-recover --confirm-stop且无外部目标')
            recover_transaction();return
        require(not JOURNAL.exists() and not JOURNAL.is_symlink() and (args.command=='reinstall-services' or not STARTUP_BLOCK.exists()) and not STARTUP_BLOCK.is_symlink(),'已有维护事务，先明确recover，不能叠加更改')
        state=installed_state()
        if args.command=='uninstall-services':
            uninstall_services(args,state);return
        if args.command=='reinstall-services':
            reinstall_services(args,state);return
        require(not args.confirm_uninstall_services and not args.confirm_reinstall_services,'卸载/恢复注册参数不能用于其他更改')
        require(not state.get('services_uninstalled'),'服务已卸载，先明确reinstall-services恢复注册；保留的数据/程序未删除')
        if args.command=='certificate-automation':
            configure_certificate_automation(args,state);return
        require(not args.confirm_certificate_automation and not args.disable_certificate_automation,'定时授权参数不能用于其他更改')
        if args.command=='certificate-refresh':
            run_certificate_refresh(args,state);return
        require(not args.confirm_certificate_refresh and not args.renew and not args.scheduled,'证书专属参数不能用于其他更改')
        if args.command=='backup':
            require(not any((args.archive,args.archive_sha256,args.distribution,args.confirm_upgrade,args.confirm_rollback,args.confirm_recover,args.confirm_certificate_refresh,args.renew,args.scheduled,args.confirm_certificate_automation,args.disable_certificate_automation,args.confirm_uninstall_services,args.confirm_reinstall_services)),'备份不混用升级/回滚参数')
            run_backup(args,state)
        else:run_change(args,state)

if __name__=='__main__':
    try:main()
    except (BackupError,install.InstallError,ValueError,KeyError,OSError,tarfile.TarError) as exc:
        print('MAINTENANCE_STOPPED '+type(exc).__name__+': '+str(exc),file=sys.stderr);sys.exit(1)
