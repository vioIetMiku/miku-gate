import importlib.util
import contextlib
import gzip
import hashlib
import tarfile
from types import SimpleNamespace
import io
import json
from pathlib import Path
import tempfile
import sys
import unittest
from unittest import mock
spec=importlib.util.spec_from_file_location('maintain',Path(__file__).with_name('miku-maintain.py'))
maintenance=importlib.util.module_from_spec(spec);spec.loader.exec_module(maintenance)
class ConfidentialBackupSafety(unittest.TestCase):
    def test_archive_member_paths_and_duplicates_fail_before_creation(self):
        manifest={'instance':'a'*22,'schema':6}
        for files in ([('../unsafe',b'secret',0,0,0o600)],[('data/one',b'a',0,0,0o600),('data/one',b'b',0,0,0o600)]):
            with self.assertRaises(maintenance.BackupError):maintenance.create_archive(Path('/new/backup.tar.gz'),files,manifest)
    @unittest.skipUnless(sys.platform=="linux", "actual archive/owner permissions are verified on Linux installation CI")
    def test_archive_verifies_private_snapshot_without_extraction(self):
        # Only archive logic runs cross-platform. Linux permission checking is
        # exercised independently in the disposable installed-system lab.
        with tempfile.TemporaryDirectory() as root:
            destination=Path(root).resolve(strict=True)/'snapshot.tar.gz'
            real_lstat=Path.lstat
            def metadata(path):
                info=real_lstat(path)
                if path==destination.parent:return type('Info',(),{'st_mode':0o40700,'st_uid':maintenance.os.geteuid(),'st_ino':info.st_ino,'st_dev':info.st_dev})()
                return info
            if not hasattr(maintenance.os,'geteuid'):
                uid=mock.patch.object(maintenance.os,'geteuid',return_value=0,create=True)
            else:uid=mock.patch.object(maintenance.os,'geteuid',return_value=0)
            with uid,mock.patch.object(Path,'lstat',metadata):
                files=[('data/miku-gate.db',b'fixture-ciphertext',1000,1000,0o600),('data/miku-gate.db.key',b'private-vault-key',1000,1000,0o600)]
                result=maintenance.create_archive(destination,files,{'instance':'a'*22,'schema':6})
                self.assertFalse(result['restore_verified']);self.assertEqual(result['member_count'],3)
                with self.assertRaises(maintenance.BackupError):maintenance.create_archive(destination,files,{'instance':'a'*22,'schema':6})
    def test_known_fixed_destinations_exclude_other_services(self):
        self.assertEqual(len(maintenance.FILES),6)
        self.assertNotIn('nginx',str(maintenance.FILES))
class InstalledBackupFixtureContract(unittest.TestCase):
    def test_real_api_creation_status_and_unprivileged_loopback_port(self):
        spec=importlib.util.spec_from_file_location('backup_verify',Path(__file__).with_name('verify-installed-backup.py'))
        verifier=importlib.util.module_from_spec(spec);spec.loader.exec_module(verifier)
        view={'configured':True,'has_password':True}
        request=mock.Mock(side_effect=[({'exit':{'id':'fixture-id'}},None),(None,None),(view,None)])
        id,secret,before=verifier.create_private_fixture(request)
        self.assertEqual(id,'fixture-id');self.assertEqual(before,view);self.assertTrue(secret.startswith('backup-fixture-'))
        self.assertEqual(request.call_args_list[0].kwargs['expected'],201)
        body=request.call_args_list[1].args[2]
        self.assertGreaterEqual(body['port'],1024);self.assertEqual(body['host'],'127.0.0.1');self.assertTrue(body['allow_loopback'])


class PairedMaintenanceSafety(unittest.TestCase):
    def state(self):
        helpers={name:b'fixture-'+name.encode() for name in ('miku-install.py','miku-maintain.py')}
        state={'instance':'a'*22,'users':{'miku-gate':[1000,1000]},'vpn':False,'cores':[],'core_sources':[],'origin':'https://192.0.2.10:8443','ip':'192.0.2.10','domain':'','port':8443,'certificate_summary':{'bootstrap':True},'binary_sha256':hashlib.sha256(b'old-program').hexdigest(),'maintenance_sha256':{name:hashlib.sha256(raw).hexdigest() for name,raw in helpers.items()}}
        return state,helpers
    def archive(self,mutate=None):
        state,helpers=self.state();targets=maintenance.destinations(state)
        contents={'installation/install-state.json':json.dumps(state).encode(),'binary/miku-gate':b'old-program','units/miku-gate.service':b'known-unit','data/miku-gate.db':b'fixture-db','data/miku-gate.db.key':b'paired-key','data/management/certificate.pem':b'certificate','data/management/private.key':b'private'}
        contents.update({'binary/'+name:raw for name,raw in helpers.items()})
        records=[{'name':name,'bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest(),'uid':targets[name][1],'gid':targets[name][2],'mode':targets[name][3]} for name,raw in contents.items()]
        manifest={'format':1,'schema':6,'instance':state['instance'],'binary_sha256':state['binary_sha256'],'encrypted_record_count':1,'files':records}
        contents['BACKUP.json']=json.dumps(manifest).encode()
        if mutate:mutate(contents)
        output=io.BytesIO()
        with tarfile.open(fileobj=output,mode='w:gz') as archive:
            for name,raw in contents.items():
                item=tarfile.TarInfo(name);item.size=len(raw)
                item.uid,item.gid,item.mode=targets[name][1:] if name in targets else (0,0,0o600)
                archive.addfile(item,io.BytesIO(raw))
        return state,output.getvalue()
    def test_snapshot_requires_same_instance_paired_material_and_fixed_paths(self):
        state,raw=self.archive()
        with mock.patch.object(maintenance,'check_identity'),mock.patch.object(maintenance.install,'unit_text',return_value={'miku-gate.service':'known-unit'}):
            resources,old,_=maintenance.decode_snapshot(raw,state)
            self.assertEqual(old['instance'],state['instance']);self.assertIn('data/miku-gate.db.key',[r[0] for r in resources])
            for change in (lambda c:c.pop('data/miku-gate.db.key'),lambda c:c.update({'../../elsewhere':b'payload'}),lambda c:c.update({'binary/miku-gate':b'changed'}),lambda c:c.update({'units/miku-gate.service':b'ExecStart=/bin/sh'})):
                _,bad=self.archive(change)
                with self.assertRaises(maintenance.BackupError):maintenance.decode_snapshot(bad,state)
            other=json.loads(json.dumps(state));other['instance']='b'*22
            with self.assertRaises(maintenance.BackupError):maintenance.decode_snapshot(raw,other)
    def test_link_archive_member_rejected_without_extraction(self):
        output=io.BytesIO()
        with tarfile.open(fileobj=output,mode='w:gz') as archive:
            item=tarfile.TarInfo('data/miku-gate.db');item.type=tarfile.SYMTYPE;item.linkname='/etc/shadow';archive.addfile(item)
        with self.assertRaises(maintenance.BackupError):maintenance.decode_snapshot(output.getvalue(),self.state()[0])
    def test_restore_keeps_writer_lease_and_removes_only_known_stale_sidecars(self):
        state,_=self.state();uid,gid=state['users']['miku-gate']
        resources=[('data/.serve.lock',b'lease',uid,gid,0o600),('data/miku-gate.db',b'db',uid,gid,0o600),('data/miku-gate.db.key',b'key',uid,gid,0o600)]
        with mock.patch.object(maintenance,'replace_owned') as replace,mock.patch.object(maintenance,'remove_owned') as remove:
            maintenance.restore_resources(resources,state)
            self.assertFalse(any(call.args[0].name=='.serve.lock' for call in replace.call_args_list))
            self.assertEqual({call.args[0].name for call in remove.call_args_list},{'miku-gate.db-wal','miku-gate.db-shm'})
    def test_failed_upgrade_restores_original_pair_and_reports_failure(self):
        state,_=self.state();original=[('data/miku-gate.db',b'old',1000,1000,0o600)]
        args=SimpleNamespace(command='upgrade',confirm_stop=True,output='/private/pre-upgrade.tar.gz',leave_stopped=False,confirm_backup=False,confirm_recover=False,confirm_upgrade=True,distribution='/release',archive=None,archive_sha256=None)
        events=[]
        with contextlib.ExitStack() as stack:
            for name in ('stop_services','set_startup_block','install_startup_guard','remove_owned','write_journal','protected_data_check'):
                stack.enter_context(mock.patch.object(maintenance,name,side_effect=lambda *a,n=name:events.append(n)))
            stack.enter_context(mock.patch.object(maintenance,'data_lease',return_value=contextlib.nullcontext()))
            stack.enter_context(mock.patch.object(maintenance,'check_output',return_value=Path(args.output)))
            stack.enter_context(mock.patch.object(maintenance,'service_active',return_value=True))
            stack.enter_context(mock.patch.object(maintenance,'snapshot',return_value=(original,{'file':args.output,'archive_sha256':'c'*64})))
            stack.enter_context(mock.patch.object(maintenance,'apply_release',return_value={**state,'binary_sha256':'d'*64}))
            stack.enter_context(mock.patch.object(maintenance,'validate_recovered',side_effect=[maintenance.BackupError('fixture failed'),None]))
            restored=stack.enter_context(mock.patch.object(maintenance,'restore_resources'))
            restarted=stack.enter_context(mock.patch.object(maintenance,'restart_services'))
            stack.enter_context(mock.patch.object(maintenance.install,'distribution_binary',return_value=(Path('/release/bin/miku-gate'),b'new',{'binary_sha256':'d'*64})))
            stack.enter_context(mock.patch.object(maintenance.install,'platform_gate',return_value='amd64'))
            stack.enter_context(mock.patch.object(maintenance.install,'trusted_tool',return_value='/usr/bin/systemctl'))
            stack.enter_context(mock.patch.object(maintenance.install,'run',return_value=b''))
            with self.assertRaisesRegex(maintenance.BackupError,'PAIRED_ROLLBACK_VERIFIED'):maintenance.run_change(args,state)
            restored.assert_called_once_with(original,state);restarted.assert_called_once_with(state,['miku-gate.service'])
            self.assertLess(events.index('write_journal'),events.index('set_startup_block'))
    def test_unconfirmed_rollback_keeps_recovery_journal_and_block(self):
        state,_=self.state()
        with mock.patch.object(maintenance,'safe_path',return_value=(json.dumps({'format':1,'operation':'upgrade','state':state,'active':['miku-gate.service'],'backup':'/private/one','archive_sha256':'c'*64}).encode(),None)),mock.patch.object(maintenance,'check_identity'),mock.patch.object(maintenance,'read_snapshot',return_value=([],state,{})),mock.patch.object(maintenance,'set_startup_block') as block,mock.patch.object(maintenance,'stop_services'),mock.patch.object(maintenance,'data_lease',return_value=contextlib.nullcontext()),mock.patch.object(maintenance,'remove_owned') as remove,mock.patch.object(maintenance,'restore_resources'),mock.patch.object(maintenance,'validate_recovered'),mock.patch.object(maintenance,'restart_services',side_effect=maintenance.BackupError('restart failure')),mock.patch.object(maintenance.install,'trusted_tool',return_value='/usr/bin/systemctl'),mock.patch.object(maintenance.install,'run',return_value=b''):
            with self.assertRaisesRegex(maintenance.BackupError,'RECOVERY_RESTART_UNCONFIRMED'):maintenance.recover_transaction()
            self.assertGreaterEqual(block.call_count,2)
            self.assertFalse(any(call.args[0]==maintenance.JOURNAL for call in remove.call_args_list))


class CertificateRefreshSafety(unittest.TestCase):
    def test_no_renewal_or_restart_without_domain_and_authorization(self):
        state={'domain':'','certificate_summary':{'bootstrap':True}}
        with mock.patch.object(maintenance.install,'run') as run:
            with self.assertRaises(maintenance.BackupError):maintenance.certificate_candidate(state,True)
            run.assert_not_called()
        state={'domain':'entry.example','certificate_summary':{'bootstrap':False}}
        with mock.patch.object(maintenance.install,'run') as run:
            with self.assertRaises(maintenance.BackupError):maintenance.certificate_candidate(state,True)
            run.assert_not_called()
    def test_occupied_80_never_stops_existing_service(self):
        state={'domain':'entry.example','certificate_summary':{'bootstrap':False,'not_after':'2026-01-01T00:00:00Z'},'certificate_automation':{'enabled':True,'renew':True}}
        with mock.patch.object(maintenance.install,'source_certificate',return_value=None),mock.patch.object(maintenance.install,'port_available',return_value=False),mock.patch.object(maintenance.install,'run') as run:
            with self.assertRaises(maintenance.BackupError):maintenance.certificate_candidate(state,True)
            run.assert_not_called()
    def test_unchanged_certificate_has_no_write_backup_stop_or_restart(self):
        state={'instance':'a'*22,'certificate_summary':{'leaf_sha256':'b'*64},'certificate_automation':{'enabled':True,'renew':False}}
        args=SimpleNamespace(archive=None,archive_sha256=None,distribution=None,confirm_backup=False,confirm_upgrade=False,confirm_rollback=False,confirm_recover=False,leave_stopped=False,scheduled=True,output=None,confirm_stop=False,confirm_certificate_refresh=False,renew=False)
        with mock.patch.object(maintenance,'certificate_candidate',return_value=(b'cert',b'key',{'leaf_sha256':'b'*64})),mock.patch.object(maintenance,'stop_services') as stop,mock.patch.object(maintenance,'snapshot') as snap,contextlib.redirect_stdout(io.StringIO()):
            maintenance.run_certificate_refresh(args,state);stop.assert_not_called();snap.assert_not_called()
    def test_scheduled_requires_prior_install_consent(self):
        state={'certificate_automation':{'enabled':False}}
        args=SimpleNamespace(archive=None,archive_sha256=None,distribution=None,confirm_backup=False,confirm_upgrade=False,confirm_rollback=False,confirm_recover=False,leave_stopped=False,scheduled=True,output=None,confirm_stop=False,confirm_certificate_refresh=False,renew=False)
        with mock.patch.object(maintenance,'certificate_candidate') as candidate:
            with self.assertRaises(maintenance.BackupError):maintenance.run_certificate_refresh(args,state)
            candidate.assert_not_called()

class CertificateUnitSnapshotRestoration(unittest.TestCase):
    def test_timer_restore_does_not_mutate_canonical_certificate_units(self):
        state,_=PairedMaintenanceSafety().state();state['certificate_automation']={'enabled':True,'renew':False}
        resources=[('units/'+name,text.encode(),0,0,0o644) for name,text in maintenance.install.certificate_units().items()]
        with mock.patch.object(maintenance,'replace_owned') as replace,mock.patch.object(maintenance,'remove_owned'):
            maintenance.restore_resources(resources,state)
            self.assertEqual({call.args[0].name:call.args[1] for call in replace.call_args_list},{name:text.encode() for name,text in maintenance.install.certificate_units().items()})
class DataPreservingServiceRemoval(unittest.TestCase):
    def test_unconfirmed_removal_does_not_touch_units_or_data(self):
        args=__import__('types').SimpleNamespace(confirm_uninstall_services=False,confirm_stop=False,output=None)
        with mock.patch.object(maintenance,'check_output') as check,mock.patch.object(maintenance,'stop_services') as stop,mock.patch.object(maintenance,'remove_owned') as remove:
            with self.assertRaises(maintenance.BackupError):maintenance.uninstall_services(args,{'services_uninstalled':False})
            check.assert_not_called();stop.assert_not_called();remove.assert_not_called()
    def test_registered_units_never_include_other_panel_services(self):
        state,_=PairedMaintenanceSafety().state()
        fixed={name:__import__('pathlib').PurePosixPath(str(path).replace('\\','/')) for name,path in maintenance.FILES.items()}
        with mock.patch.object(maintenance,'FILES',fixed),mock.patch.object(maintenance,'DATA',__import__('pathlib').PurePosixPath('/var/lib/miku-gate/data')):units=maintenance.registered_units(state)
        self.assertEqual(set(units),{'miku-gate.service'})
        state['certificate_automation']={'enabled':True,'renew':False}
        with mock.patch.object(maintenance,'FILES',fixed),mock.patch.object(maintenance,'DATA',__import__('pathlib').PurePosixPath('/var/lib/miku-gate/data')):
            self.assertEqual(set(maintenance.registered_units(state)),{'miku-gate.service','miku-gate-certificate.service','miku-gate-certificate.timer'})
        self.assertNotIn('x-ui.service',units)
if __name__=='__main__':unittest.main()
