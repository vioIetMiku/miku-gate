#!/usr/bin/env python3
"""Disposable CI verifier: encrypted installed data survives checked backup.

No initialization password/config/key is printed, uploaded or sent in arguments
other than the explicit local private initialization-log path.
"""
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import socket
import ssl
import stat
import subprocess
import tarfile

class VerifyError(RuntimeError):pass

def require(value,message):
    if not value:raise VerifyError(message)

def create_private_fixture(request):
    created,_=request('POST','/api/exits',{'name':'private backup consistency fixture','country':'JP'},expected=201)
    id=created['exit']['id'];secret='backup-fixture-'+os.urandom(12).hex()
    request('PUT','/api/exits/'+id+'/upstream',{'kind':'socks5','host':'127.0.0.1','port':19090,'username':'backup-fixture','password':secret,'allow_loopback':True,'dns':'cloudflare'},expected=204)
    before,_=request('GET','/api/exits/'+id+'/upstream')
    require(before['configured'] and before['has_password'] and 'password' not in before,'saved upstream metadata leaked private data')
    return id,secret,before

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--private-init-log',required=True)
    parser.add_argument('--maintenance-script',required=True)
    parser.add_argument('--output',required=True)
    parser.add_argument('--upgrade-distribution')
    parser.add_argument('--verify-service-removal',action='store_true')
    args=parser.parse_args()
    require(os.geteuid()==0,'disposable installed verification needs root coordinator')
    state=json.loads(Path('/etc/miku-gate/install-state.json').read_text())
    log=Path(args.private_init_log);info=log.lstat()
    require(stat.S_ISREG(info.st_mode) and info.st_nlink==1 and stat.S_IMODE(info.st_mode)==0o600 and info.st_size<=1<<20,'private initialization log bound/permissions')
    match=re.search(r'密码：([^\r\n]+)',log.read_text(encoding='utf-8'))
    require(match is not None,'initialization credential absent')
    password=match[1];cookie='';csrf=''
    cp=Path('/var/lib/miku-gate/data/management/certificate.pem')
    context=ssl.create_default_context(cafile=str(cp)) if state['certificate_summary']['bootstrap'] else ssl.create_default_context()
    hostname=state['domain'] or state['ip'];authority=hostname+':'+str(state['port'])
    def request(method,path,body=None,expected=200):
        with socket.create_connection(('127.0.0.1',state['port']),timeout=4) as raw:
            with context.wrap_socket(raw,server_hostname=hostname) as secure:
                require(hashlib.sha256(secure.getpeercert(binary_form=True)).hexdigest()==state['certificate_summary']['leaf_sha256'],'installed certificate identity drift')
                content=json.dumps(body).encode() if body is not None else b''
                headers='Host: '+authority+'\r\nOrigin: '+state['origin']+'\r\nContent-Length: '+str(len(content))+'\r\nConnection: close\r\n'
                if body is not None:headers+='Content-Type: application/json\r\n'
                if cookie:headers+='Cookie: '+cookie+'\r\n'
                if csrf:headers+='X-CSRF-Token: '+csrf+'\r\n'
                secure.sendall((method+' '+path+' HTTP/1.1\r\n'+headers+'\r\n').encode()+content)
                response=http.client.HTTPResponse(secure);response.begin();payload=response.read(65537)
                require(response.status==expected and len(payload)<=65536,'bounded installed HTTPS API failed: '+method+' '+path.rsplit('/',1)[-1]+' status='+str(response.status)+' expected='+str(expected))
                return json.loads(payload) if payload else None,response.getheader('Set-Cookie')
    session,session_cookie=request('POST','/api/auth/login',{'username':'admin','password':password})
    cookie=session_cookie.split(';',1)[0];csrf=session['csrf']
    id,secret,before=create_private_fixture(request)
    key=Path('/var/lib/miku-gate/data/miku-gate.db.key')
    require(key.is_file(),'paired credential key absent')
    original_key_sha=hashlib.sha256(key.read_bytes()).hexdigest()
    output=Path(args.output);output.parent.mkdir(mode=0o700,exist_ok=False)
    os.chmod(output.parent,0o700)
    response=subprocess.run(['/usr/bin/python3','-B',args.maintenance_script,'backup','--output='+str(output),'--confirm-backup','--confirm-stop'],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=90)
    require(response.returncode==0,'maintenance backup/restart failed; private diagnostic retained only locally')
    require(len(response.stdout)<16384 and len(response.stderr)<16384,'maintenance output exceeded budget')
    require(stat.S_IMODE(output.stat().st_mode)==0o600,'confidential archive permissions')
    with tarfile.open(output,'r:gz') as archive:
        members={m.name:m for m in archive.getmembers()}
        require('data/miku-gate.db' in members and 'data/miku-gate.db.key' in members and 'BACKUP.json' in members,'paired database/key missing from archive')
        manifest=json.loads(archive.extractfile('BACKUP.json').read())
        require(manifest['instance']==state['instance'] and manifest['schema'] in (6,7) and manifest['encrypted_record_count']>0 and not manifest['restore_verified'],'backup identity/state overstated')
        require(hashlib.sha256(archive.extractfile('data/miku-gate.db.key').read()).hexdigest()==original_key_sha,'archive mismatched vault key')
        database=archive.extractfile('data/miku-gate.db').read()
        require(secret.encode() not in database,'upstream recoverable secret plaintext in DB')
    require(hashlib.sha256(key.read_bytes()).hexdigest()==original_key_sha,'backup replaced live vault identity')
    after,_=request('GET','/api/exits/'+id+'/upstream')
    require(after==before,'encrypted upstream not recoverable after ordinary service restart')
    rollback_verified=False;upgrade_verified=False
    if args.upgrade_distribution:
        def change(command,arguments):
            result=subprocess.run(['/usr/bin/python3','-B',args.maintenance_script,command,*arguments],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=90)
            require(result.returncode==0,'paired '+command+' failed; private diagnostics not returned')
            require(len(result.stdout)<16384 and len(result.stderr)<16384,'paired maintenance output exceeded budget')
        original_binary=state['binary_sha256']
        change('upgrade',['--distribution='+args.upgrade_distribution,'--output='+str(output.parent/'pre-upgrade.tar.gz'),'--confirm-stop','--confirm-upgrade'])
        upgraded=json.loads(Path('/etc/miku-gate/install-state.json').read_text())
        require(upgraded['binary_sha256']!=original_binary and upgraded['instance']==state['instance'],'real candidate program not upgraded or instance drift')
        after,_=request('GET','/api/exits/'+id+'/upstream');require(after==before,'encrypted fixture not readable after upgrade')
        upgrade_verified=True
        request('PUT','/api/exits/'+id+'/upstream',{'kind':'socks5','host':'127.0.0.1','port':19090,'username':'changed-after-snapshot','password':'changed-'+os.urandom(12).hex(),'allow_loopback':True,'dns':'cloudflare'},expected=204)
        changed,_=request('GET','/api/exits/'+id+'/upstream');require(changed!=before,'rollback fixture did not change persistent data')
        archive_sha=hashlib.sha256(output.read_bytes()).hexdigest()
        change('rollback',['--archive='+str(output),'--archive-sha256='+archive_sha,'--output='+str(output.parent/'pre-rollback.tar.gz'),'--confirm-stop','--confirm-rollback'])
        restored=json.loads(Path('/etc/miku-gate/install-state.json').read_text())
        require(restored['binary_sha256']==original_binary and restored['instance']==state['instance'],'paired rollback did not restore original program/state')
        require(hashlib.sha256(key.read_bytes()).hexdigest()==original_key_sha,'paired rollback restored wrong credential key')
        after,_=request('GET','/api/exits/'+id+'/upstream');require(after==before,'paired rollback did not restore original encrypted data')
        require(not Path('/etc/miku-gate/maintenance-transaction.json').exists() and not Path('/etc/miku-gate/maintenance-block').exists(),'successful paired rollback left an unresolved transaction')
        rollback_verified=True
    service_removal_verified=False
    if args.verify_service_removal:
        require(args.upgrade_distribution,'service removal proof follows the same installed upgrade fixture')
        data=Path('/var/lib/miku-gate/data');units=Path('/etc/systemd/system')
        unit_names=['miku-gate.service']+(['miku-gate-vpn.service'] if state['vpn'] else [])
        def systemctl(*arguments):
            result=subprocess.run(['/usr/bin/systemctl',*arguments],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=35)
            require(result.returncode==0 and len(result.stdout)<16384 and len(result.stderr)<16384,'bounded exact-unit systemctl check failed')
            return result.stdout.decode().strip()
        def manifest():
            paths=[Path('/etc/miku-gate/install-state.json'),Path('/usr/local/lib/miku-gate/miku-gate'),key,data/'miku-gate.db',cp,data/'management/private.key',*[(units/name) for name in unit_names]]
            return {str(path):{'sha256':hashlib.sha256(path.read_bytes()).hexdigest(),'uid':path.stat().st_uid,'gid':path.stat().st_gid,'mode':stat.S_IMODE(path.stat().st_mode)} for path in paths}
        initial_users=state['users'];before_files=manifest()
        failed=subprocess.run(['/usr/bin/python3','-B',args.maintenance_script,'uninstall-services','--output='+str(output.parent/'unconfirmed.tar.gz')],stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=15)
        require(failed.returncode!=0 and not (output.parent/'unconfirmed.tar.gz').exists(),'unconfirmed service removal did not refuse before snapshot')
        systemctl('is-active','--quiet','miku-gate.service')
        change('uninstall-services',['--output='+str(output.parent/'pre-uninstall.tar.gz'),'--confirm-stop','--confirm-uninstall-services'])
        retained=json.loads(Path('/etc/miku-gate/install-state.json').read_text())
        require(retained['services_uninstalled'] and retained['users']==initial_users and retained['instance']==state['instance'],'data preserving removal changed identities')
        for name in unit_names:
            require(not (units/name).exists(),'service-only removal left an unverified unit file')
        for name in ['/usr/local/lib/miku-gate/miku-gate',str(key),str(cp),str(data/'management/private.key')]:
            require(hashlib.sha256(Path(name).read_bytes()).hexdigest()==before_files[name]['sha256'],'service removal changed retained binary/key/certificate')
        try:
            with socket.create_connection(('127.0.0.1',state['port']),timeout=1):
                raise VerifyError('management listener remained after confirmed service removal')
        except OSError:pass
        accounts={name:[int(x) for x in subprocess.check_output(['/usr/bin/id','-u',name],timeout=5).split()] for name in initial_users}
        require(all(accounts[name]==[ids[0]] for name,ids in initial_users.items()),'service removal deleted/reassigned dedicated account')
        require(not Path('/etc/miku-gate/maintenance-transaction.json').exists() and not Path('/etc/miku-gate/maintenance-block').exists(),'successful removal left unresolved paired transaction')
        change('reinstall-services',['--confirm-reinstall-services'])
        registered=json.loads(Path('/etc/miku-gate/install-state.json').read_text())
        require(not registered['services_uninstalled'] and registered['users']==initial_users and registered['instance']==state['instance'],'explicit registration did not preserve installation identity')
        for name in unit_names:
            require((units/name).exists() and hashlib.sha256((units/name).read_bytes()).hexdigest()==before_files[str(units/name)]['sha256'],'registered unit template changed')
            values=systemctl('show',name,'--property=ActiveState,MainPID,UnitFileState','--value').splitlines()
            require('active' not in values and 'activating' not in values and 'enabled' not in values and not any(value.isdecimal() and int(value)>0 for value in values),'re-registration secretly enabled/started unit')
        # Start explicitly, in executor/control order; the re-register action did
        # not do this. Authenticate with the original private session/key/cert.
        if state['vpn']:systemctl('start','miku-gate-vpn.service')
        systemctl('start','miku-gate.service')
        import time
        deadline=time.monotonic()+15;last=None
        while time.monotonic()<deadline:
            try:
                after,_=request('GET','/api/exits/'+id+'/upstream');last=after;break
            except (OSError,VerifyError):time.sleep(0.2)
        require(last==before,'original encrypted config/session not readable after service-only uninstall and explicit registration/start')
        require(hashlib.sha256(key.read_bytes()).hexdigest()==original_key_sha,'service removal/re-registration replaced database vault key')
        service_removal_verified=True
    request('DELETE','/api/exits/'+id,expected=204)
    print('INSTALLED_BACKUP_VERIFIED '+json.dumps({'paired_encrypted_db_key_verified':True,'private_archive_mode':'0600','ordinary_service_https_restart_verified':True,'data_identity_retained':True,'raw_private_material_returned':False,'restore_verified':rollback_verified,'upgrade_verified':upgrade_verified,'service_removal_and_explicit_registration_verified':service_removal_verified,'vpn_mode':state['vpn']},sort_keys=True))

if __name__=='__main__':
    try:main()
    except (VerifyError,OSError,ValueError,KeyError,subprocess.TimeoutExpired) as exc:
        detail=str(exc) if isinstance(exc,VerifyError) else 'external error; no raw diagnostics'
        print('INSTALLED_BACKUP_FAILED '+type(exc).__name__+': '+detail,file=__import__('sys').stderr);__import__('sys').exit(1)
