#!/usr/bin/env python3
"""Local prebuilt installer. Old Miku installations are paired-backed-up first."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import sys
import time
import common as c

UNIT=b'''[Unit]
Description=Miku Gate connection service
After=network-online.target
Wants=network-online.target
[Service]
Type=simple
ExecStart=/usr/local/lib/miku/miku -dir /var/lib/miku
Restart=on-failure
RestartSec=3
TimeoutStopSec=45
KillMode=mixed
UMask=0077
[Install]
WantedBy=multi-user.target
'''
OPENRC=b'''#!/sbin/openrc-run
name="miku"
description="Miku Gate connection service"
command="/usr/local/lib/miku/miku"
command_args="-dir /var/lib/miku"
command_background=true
pidfile="/run/miku.pid"
output_log="/var/log/miku.log"
error_log="/var/log/miku.log"
supervisor=supervise-daemon
respawn_delay=3
respawn_max=0
depend() { need net; after firewall; }
'''

def restore_new(backup):
    # Restore only the registered paths; never extract archive paths as root.
    with __import__('tarfile').open(backup,'r:gz') as archive:
        for item in archive:
            if item.isdir():continue
            c.require(item.isfile() and not item.issym() and not item.islnk() and item.size<=160<<20,'备份成员异常')
            parts=Path(item.name).parts;c.require('..' not in parts and not item.name.startswith('/'),'备份路径异常')
            if parts[0]=='data' and parts[-1]!='.maintenance.lock':target=c.DATA.joinpath(*parts[1:]);mode=0o600
            elif parts[0]=='lib' and len(parts)==2:target=c.LIB/parts[1];mode=0o755 if parts[1]=='miku' else 0o644
            elif item.name=='menu/miku':target=c.MENU;mode=0o755
            elif item.name=='service/miku':target=c.UNIT if c.systemd() else Path('/etc/init.d/miku');mode=0o644 if c.systemd() else 0o755
            else:continue
            if not target.parent.exists():target.parent.mkdir(mode=0o700,parents=True)
            c.atomic(target,archive.extractfile(item).read(),mode)

def restore_old(module,record):
    state=record['state']
    with module.maintenance_lock():
        module.stop_services(state)
        with module.data_lease(state):
            resources,_,_=module.read_snapshot(record['backup']['file'],record['backup']['archive_sha256'],state)
            module.restore_resources(resources,state)
        c.run(['systemctl','daemon-reload'])
        for unit in record['enabled']:c.run(['systemctl','enable',unit])
        module.restart_services(state,record['active'])
        if 'miku-gate-certificate.timer' in record['active']:c.run(['systemctl','start','miku-gate-certificate.timer'])

def migrate(module,state):
    c.require(c.systemd(),'旧版Miku迁移需要systemd')
    module.installed_state();units=module.check_registered_units(state)
    record={'state':state,'active':[],'enabled':[]}
    with module.maintenance_lock():
        for unit in units:
            if module.service_active(unit):record['active'].append(unit)
            if c.run(['systemctl','is-enabled','--quiet',unit],checked=False).returncode==0:record['enabled'].append(unit)
        for unit in ('miku-gate-certificate.timer','miku-gate-certificate.service'):
            if unit in units:c.run(['systemctl','stop',unit])
        module.stop_services(state)
        directory=c.private_dir(Path('/etc/miku-gate/fanout-switch-backups'))
        with module.data_lease(state):
            _,record['backup']=module.snapshot(directory/('before-switch-'+str(time.time_ns())+'.tar.gz'),state)
        # Durable recovery receipt is written BEFORE export or data conversion.
        c.write_json(c.DATA/'legacy-switch.json',record)
        uid,gid=state['users']['miku-gate']
        command=['setpriv','--reuid='+str(uid),'--regid='+str(gid),'--clear-groups','--no-new-privs',c.LIB/'miku-export','--data='+str(module.DATA),'--expected-instance='+state['instance']]
        packet=json.loads(c.run(command,timeout=60).stdout)
        c.require(packet.get('instance')==state['instance'],'旧数据实例不一致')
        c.import_legacy(packet)
        value={'port':state['port'],'listen_addr':'','public_ip':state['ip'],'public_host':state.get('domain') or state['ip'],'residential_only':False}
        if state['origin'].startswith('https://'):
            c.private_dir(c.DATA/'management')
            for source,name in ((module.DATA/'management/certificate.pem','certificate.pem'),(module.DATA/'management/private.key','private.key')):
                raw,_=module.safe_path(source,uid,0o600);c.atomic(c.DATA/'management'/name,raw)
            value.update(tls_cert=str(c.DATA/'management/certificate.pem'),tls_key=str(c.DATA/'management/private.key'))
        c.write_json(c.DATA/'settings.json',value)
        for unit in units:
            if unit in ('miku-gate.service','miku-gate-vpn.service','miku-gate-certificate.timer'):c.run(['systemctl','disable',unit])
    print('旧版配套备份：'+record['backup']['file']+'\n恢复旧版：miku rollback')
    return record

def _install(directory,update=False,reinstall=False):
    c.root();directory,meta=c.verify_distribution(directory);architecture=c.arch()
    c.require(c.systemd() or shutil.which('rc-service',path=c.ENV['PATH']),'需要systemd或OpenRC')
    old_module,old=(None,None) if reinstall else c.old_maintenance()
    previous=c.installed() if (c.DATA/'install.json').exists() else None
    if previous:
        c.require(hashlib.sha256(c.trusted(c.LIB/'miku',0o755)).hexdigest()==previous['binary_sha256'],'已安装程序摘要变化')
    else:
        c.require(not c.UNIT.exists() and not c.OPENRC.exists(),'已有同名服务，无法自动接管')
        c.require(old is not None or not c.MENU.exists(),'已有其他miku快捷命令')
        c.require(not c.LIB.exists(),'已有未登记程序目录，停止覆盖')
        if c.DATA.exists():c.require(set(p.name for p in c.DATA.iterdir())<={'.maintenance.lock'},'已有未登记数据，请先完成 miku rollback 或恢复')
    c.require(not update or previous is not None,'update需要已有此版安装')
    c.dependencies();c.program_dir();c.private_dir(c.DATA)
    # Put the read-only export tool in a traversable root-owned directory.
    c.atomic(c.LIB/'miku-export',c.trusted(directory/'bin'/('miku-export-linux-'+architecture)),0o755)
    c.atomic(c.LIB/'CORE_ASSETS.json',c.trusted(directory/'CORE_ASSETS.json'),0o600)
    try:c.ensure_cores(old)
    except BaseException:
        if not previous:
            # No old services have been stopped yet. Allow a clean retry of a
            # failed download, deleting only these newly created fixed files.
            for name in ('miku-export','CORE_ASSETS.json'):
                path=c.LIB/name
                if path.exists():c.trusted(path);path.unlink()
            if not any(c.LIB.iterdir()):c.LIB.rmdir()
            for name in ('xray','sing-box'):
                path=c.DATA/'bin'/name
                if path.exists():c.trusted(path);path.unlink()
            if (c.DATA/'bin').exists() and not any((c.DATA/'bin').iterdir()):(c.DATA/'bin').rmdir()
        raise
    backup=None;record=None;was_active=c.active() if previous else False;installed_files=False
    try:
        if previous:
            if was_active:c.service('stop')
            backup=c.snapshot('before-update')
        elif old:
            print('检测到旧版Miku，先备份并迁移可用入口、落地与凭据。',flush=True)
            record=migrate(old_module,old)
        else:
            preferred=os.environ.get('WEB_PORT')
            value={'port':c.choose_port(int(preferred) if preferred else None),'listen_addr':'','public_ip':c.public_ip(),'residential_only':False}
            c.write_json(c.DATA/'settings.json',value)
            c.atomic(c.DATA/'panel_mode',b'native\n')
        value=c.settings();old_port=value['port']
        value['port']=c.choose_port(old_port)
        if value['port']!=old_port:
            c.write_json(c.DATA/'settings.json',value)
            print('原管理端口不可用或与现有面板预留端口重合，已改用空闲端口 '+str(value['port'])+'。')
        for source,name,mode in [('bin/miku-linux-'+architecture,'miku',0o755),('scripts/common.py','common.py',0o644),('scripts/maintain.py','maintain.py',0o644),('scripts/quick.py','quick.py',0o644),('scripts/install.py','install.py',0o644),('CORE_ASSETS.json','CORE_ASSETS.json',0o600),('LICENSE','LICENSE',0o644),('FLAG_ICONS_LICENSE','FLAG_ICONS_LICENSE',0o644)]:
            c.atomic(c.LIB/name,c.trusted(directory/source),mode)
        c.atomic(c.MENU,c.trusted(directory/'scripts/miku.sh'),0o755)
        installed_files=True
        for name in ('basepath','password'):
            if not (c.DATA/name).exists():c.atomic(c.DATA/name,secrets.token_hex(16 if name=='password' else 8).encode()+b'\n')
        state={'product':'miku-fanout','version':meta['version'],'binary_sha256':meta['files']['bin/miku-linux-'+architecture],'source':meta.get('source',''),'legacy':record or (previous or {}).get('legacy')}
        c.write_json(c.DATA/'install.json',state)
        if c.systemd():
            c.atomic(c.UNIT,UNIT,0o644);c.run(['systemctl','daemon-reload']);c.run(['systemctl','enable','miku.service'])
        else:c.atomic(c.OPENRC,OPENRC,0o755);c.run(['rc-update','add','miku','default'])
        if previous and previous.get('version')=='0.2.0-dev.24':
            value=c.settings();value['residential_only']=False;c.write_json(c.DATA/'settings.json',value)
        c.service('start');c.wait_ready()
    except BaseException:
        if installed_files:c.service('stop') if c.active() else None
        if backup:
            restore_new(backup)
            if c.systemd():c.run(['systemctl','daemon-reload'])
            if was_active:c.service('start')
            print('更新失败，已恢复更新前的程序与配置。',file=sys.stderr)
        elif old:
            receipt=record or (json.loads(c.trusted(c.DATA/'legacy-switch.json',0o600)) if (c.DATA/'legacy-switch.json').exists() else None)
            if receipt:restore_old(old_module,receipt);print('切换失败，旧版配套数据和服务已恢复。',file=sys.stderr)
        raise
    print('\n安装完成：Miku Gate '+meta['version']);c.info()
    print('面板增删节点、切换出口、修改域名；终端一键搭建：miku quick；查看全部订阅：miku sub。')

def main():
    parser=argparse.ArgumentParser(description='Miku Gate VPS安装');parser.add_argument('--directory',type=Path,default=Path(__file__).resolve().parents[1]);parser.add_argument('--update',action='store_true');parser.add_argument('--reinstall',action='store_true',help='卸载此 Miku Gate 并清除数据，然后重新安装')
    mode=parser.add_mutually_exclusive_group();mode.add_argument('--quick',action='store_true',help='安装后直接配置默认节点');mode.add_argument('--panel',action='store_true',help='安装后使用网页面板')
    domain=parser.add_mutually_exclusive_group();domain.add_argument('--domain');domain.add_argument('--no-domain',action='store_true');parser.add_argument('--background',action='store_true');args=parser.parse_args()
    c.require(not (args.update and args.reinstall),'--update 与 --reinstall 不能同时使用')
    if install(args.directory,args.update,args.reinstall) is False:return
    use_quick=args.quick or args.domain is not None or args.no_domain
    if not args.panel and not use_quick and not args.update and os.isatty(0):
        print('\n1 使用面板管理（增删节点、切换、域名管理）\n2 终端一键搭建，直接输出订阅（默认）')
        answer=input('使用方式 [2]：').strip() or '2';c.require(answer in ('1','2'),'请选择 1 或 2');use_quick=answer=='2'
    if use_quick:
        sys.path.insert(0,str(c.LIB))
        from quick import quick
        quick('' if args.no_domain else args.domain,args.background)

def install(directory,update=False,reinstall=False):
    c.root()
    # Validate the incoming bundle before offering to remove anything.
    directory,_=c.verify_distribution(directory);c.banner()
    with c.maintenance_lock():
        existing=any(p.exists() or p.is_symlink() for p in (c.DATA,c.LIB,c.MENU,c.UNIT,c.OPENRC))
        if existing and not update and not reinstall and os.isatty(0):
            print('检测到已有 Miku Gate 或安装残留。\n1 保留配置和节点，更新安装（默认）\n2 清除本服务数据后重新安装\n0 取消')
            choice=input('选择 [1]：').strip() or '1'
            c.require(choice in ('0','1','2'),'请选择 0、1 或 2')
            if choice=='0':return False
            reinstall=choice=='2'
        if reinstall:c.purge_current()
        elif existing and not (c.DATA/'install.json').exists() and c.DATA.exists() and any(p.name!='.maintenance.lock' for p in c.DATA.iterdir()):
            raise RuntimeError('发现未登记的旧数据；确认不再需要后，运行 bash install.sh --reinstall 清除并重新安装')
        _install(directory,update,reinstall)
    return True

if __name__=='__main__':
    try:main()
    except (Exception,KeyboardInterrupt) as exc:print('安装未完成：'+str(exc),file=sys.stderr);sys.exit(1)
