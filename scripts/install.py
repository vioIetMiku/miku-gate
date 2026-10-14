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
Description=Miku - fanout VPN Gate gateway
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
description="Miku - fanout VPN Gate gateway"
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

def _install(directory,update=False):
    c.root();directory,meta=c.verify_distribution(directory);architecture=c.arch()
    c.require(c.systemd() or shutil.which('rc-service',path=c.ENV['PATH']),'需要systemd或OpenRC')
    old_module,old=c.old_maintenance()
    previous=c.installed() if (c.DATA/'install.json').exists() else None
    if previous:
        c.require(hashlib.sha256(c.trusted(c.LIB/'miku',0o755)).hexdigest()==previous['binary_sha256'],'已安装程序摘要变化')
    else:
        c.require(not c.UNIT.exists() and not Path('/etc/init.d/miku').exists(),'已有同名服务，无法自动接管')
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
            value={'port':c.choose_port(int(os.environ.get('WEB_PORT','8899'))),'listen_addr':'','public_ip':c.public_ip(),'residential_only':False}
            c.write_json(c.DATA/'settings.json',value)
        for source,name,mode in [('bin/miku-linux-'+architecture,'miku',0o755),('scripts/common.py','common.py',0o644),('scripts/maintain.py','maintain.py',0o644),('scripts/install.py','install.py',0o644),('CORE_ASSETS.json','CORE_ASSETS.json',0o600),('LICENSE','LICENSE',0o644)]:
            c.atomic(c.LIB/name,c.trusted(directory/source),mode)
        c.atomic(c.MENU,c.trusted(directory/'scripts/miku.sh'),0o755)
        installed_files=True
        for name in ('basepath','password'):
            if not (c.DATA/name).exists():c.atomic(c.DATA/name,secrets.token_hex(16 if name=='password' else 8).encode()+b'\n')
        state={'product':'miku-fanout','version':meta['version'],'binary_sha256':meta['files']['bin/miku-linux-'+architecture],'source':meta.get('source',''),'legacy':record or (previous or {}).get('legacy')}
        c.write_json(c.DATA/'install.json',state)
        if c.systemd():
            c.atomic(c.UNIT,UNIT,0o644);c.run(['systemctl','daemon-reload']);c.run(['systemctl','enable','miku.service'])
        else:c.atomic(Path('/etc/init.d/miku'),OPENRC,0o755);c.run(['rc-update','add','miku','default'])
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
    print('\n安装完成：Miku '+meta['version']);c.info()
    if not previous and not old and os.isatty(0):
        if input('\n有自己的域名吗？[y/N]（以后也可 miku domain）：').strip().lower() in ('y','yes'):
            os.execv(sys.executable,[sys.executable,'-B',str(c.LIB/'maintain.py'),'domain'])

def main():
    parser=argparse.ArgumentParser(description='Miku fanout版VPS安装');parser.add_argument('--directory',type=Path,default=Path(__file__).resolve().parents[1]);parser.add_argument('--update',action='store_true');args=parser.parse_args()
    install(args.directory,args.update)

def install(directory,update=False):
    c.root()
    with c.maintenance_lock():_install(directory,update)

if __name__=='__main__':
    try:main()
    except (Exception,KeyboardInterrupt) as exc:print('安装未完成：'+str(exc),file=sys.stderr);sys.exit(1)
