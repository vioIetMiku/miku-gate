#!/usr/bin/env python3
"""Explicit, owner-checked installed Miku backup. No wildcard deletes/restores.

Backup requires known install state and preserves paired encrypted DB/key,
management certificates, core provenance, exact units and current Miku binary.
This command does NOT implement upgrade/restore by swapping binary alone.
"""
import argparse
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import stat
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
}
DATA=Path('/var/lib/miku-gate/data')
DATABASE=('miku-gate.db','miku-gate.db-wal','miku-gate.db-shm','miku-gate.db.key','.serve.lock')
CERT_DIRS=('management','bootstrap')
MAX_FILE=48<<20
MAX_TOTAL=96<<20

class BackupError(RuntimeError):pass

def require(value,message):
    if not value:raise BackupError(message)

def safe_path(path,uid,mode=None):
    # Open via resolved non-symlink metadata plus O_NOFOLLOW and identity check.
    # The root-only manifest paths are fixed, never constructed from archive data.
    path=Path(path)
    require(path.is_absolute() and path.resolve(strict=True)==path,'资源路径含链接或不存在，停止')
    info=path.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink==1 and info.st_uid==uid and info.st_size<=MAX_FILE,'资源文件归属/类型/大小异常，停止')
    require(not info.st_mode&0o022 and (mode is None or stat.S_IMODE(info.st_mode)==mode),'资源权限不符合备份要求')
    for parent in path.parents:
        current=parent.lstat()
        require(stat.S_ISDIR(current.st_mode) and current.st_uid in (0,uid) and not current.st_mode&0o022,'资源父路径归属/权限异常')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK|os.O_CLOEXEC)
    with os.fdopen(fd,'rb') as stream:
        require(os.path.samestat(info,os.fstat(stream.fileno())),'读取时资源被替换')
        raw=stream.read(MAX_FILE+1)
    require(len(raw)<=MAX_FILE,'资源超过有界读取限制')
    return raw,info

def installed_state():
    raw,_=safe_path(FILES['installation/install-state.json'],0,0o600)
    state=json.loads(raw)
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
    return state

def service_active(unit):
    output=install.run([install.trusted_tool('systemctl'),'show',unit,'--property=ActiveState,MainPID','--value'])
    fields=output.decode().splitlines()
    require(len(fields)==2,'系统服务状态格式未知')
    values=set(fields)
    return 'active' in values or 'activating' in values or any(v.isdecimal() and int(v)>0 for v in values)

def stop_services(state):
    units=['miku-gate.service']+(['miku-gate-vpn.service'] if state['vpn'] else [])
    active=[u for u in units if service_active(u)]
    for unit in units:
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

def collect(state):
    uid=state['users']['miku-gate'][0]
    expected={'miku-gate.db',*DATABASE[1:],*CERT_DIRS}
    require(all(p.name in expected for p in DATA.iterdir()),'数据目录有未登记资源，拒绝不完整备份')
    resources=[]
    for member,path in FILES.items():
        if member.endswith('miku-gate-vpn.service') and not state['vpn']:continue
        raw,info=safe_path(path,0,0o755 if member=='binary/miku-gate' else 0o600 if member.endswith('install-state.json') else 0o644)
        resources.append((member,raw,info.st_uid,info.st_gid,stat.S_IMODE(info.st_mode)))
    for name in state.get('maintenance_sha256',{}):
        raw,info=safe_path(FILES['binary/miku-gate'].with_name(name),0,0o644)
        resources.append(('binary/'+name,raw,info.st_uid,info.st_gid,0o644))
    # Inspect through the same ordinary identity, so sqlite cannot leave a
    # root-owned WAL/SHM file in the control account's private data directory.
    inspector=r"""import json,sqlite3,sys
connection=sqlite3.connect('file:'+sys.argv[1]+'?mode=ro',uri=True)
try:
 schema=connection.execute('PRAGMA user_version').fetchone()[0]
 encrypted=sum(connection.execute('SELECT count(*) FROM '+table).fetchone()[0] for table in ('upstreams','vpn_configs','managed_ingress'))
 identity=connection.execute("SELECT value FROM metadata WHERE key='instance_id'").fetchone()[0]
 valid=connection.execute('PRAGMA quick_check').fetchone()[0]=='ok'
 print(json.dumps({'schema':schema,'encrypted_count':encrypted,'instance':identity,'integrity_valid':valid}))
finally:connection.close()
"""
    gid=state['users']['miku-gate'][1]
    examined=json.loads(install.run([install.trusted_tool('setpriv'),'--reuid='+str(uid),'--regid='+str(gid),'--clear-groups','--no-new-privs',install.trusted_tool('python3'),'-B','-c',inspector,str(DATA/'miku-gate.db')]))
    schema,counts=examined['schema'],examined['encrypted_count']
    require(examined['instance']==state['instance'] and schema==6 and examined['integrity_valid'],'备份数据库身份/schema/完整性不匹配')
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
        output.flush();os.fsync(output.fileno())
    # Verify archive contents and every confidential member without extraction.
    expected={r[0]:r[1] for r in resources};expected['BACKUP.json']=manifest_raw
    with tarfile.open(destination,'r:gz') as archive:
        members=archive.getmembers()
        require(len(members)==len(expected) and {m.name for m in members}==set(expected),'归档成员验证失败')
        for item in members:
            require(item.isfile() and not item.issym() and item.size==len(expected[item.name]),'归档类型/大小异常')
            require(archive.extractfile(item).read(item.size+1)==expected[item.name],'归档内容校验失败')
    return {'file':str(destination),'member_count':len(expected),'instance':manifest['instance'],'schema':manifest['schema'],'contains_private_keys':True,'restore_verified':False}

def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('command',choices=('status','backup'))
    parser.add_argument('--output');parser.add_argument('--confirm-stop',action='store_true');parser.add_argument('--confirm-backup',action='store_true');parser.add_argument('--leave-stopped',action='store_true')
    args=parser.parse_args(argv)
    require(sys.platform=='linux' and os.geteuid()==0,'维护仅已授权Linux root协调，控制服务仍普通账号')
    state=installed_state()
    if args.command=='status':
        require(not args.output and not args.confirm_stop and not args.confirm_backup and not args.leave_stopped,'status只读，无变更参数')
        print(json.dumps({'instance':state['instance'],'origin':state['origin'],'vpn':state['vpn'],'cores':state['cores'],'control_active':service_active('miku-gate.service'),'backup_required_before_upgrade':True,'upgrade_restore_implemented':False},ensure_ascii=False));return
    require(args.confirm_backup and args.confirm_stop and args.output,'备份需--confirm-stop --confirm-backup及新文件--output，明确停机；不升级/恢复')
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

if __name__=='__main__':
    try:main()
    except (BackupError,install.InstallError,ValueError,KeyError,OSError) as exc:
        print('MAINTENANCE_STOPPED '+type(exc).__name__+': '+str(exc),file=sys.stderr);sys.exit(1)
