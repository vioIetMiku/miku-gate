"""Offline fixture checks; all fixed destinations and service operations mocked."""
import contextlib
import io
import json
import os
import hashlib
import socket
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import common as c
import maintain
import install

class MaintenanceTests(unittest.TestCase):
    def setUp(self):
        self.real_wait_ready=c.wait_ready
        self.directory=tempfile.TemporaryDirectory()
        self.root=Path(self.directory.name)
        self.patches=[]
        for name in ('DATA','LIB'):
            path=self.root/name;path.mkdir(mode=0o700);self.patches.append(patch.object(c,name,path))
        for name in ('UNIT','MENU','OPENRC'):self.patches.append(patch.object(c,name,self.root/name))
        self.patches += [patch.object(c,'systemd',return_value=True),patch.object(c,'service'),patch.object(c,'wait_ready'),patch.object(c,'info'),patch.object(c,'active',return_value=True)]
        for value in self.patches:value.start()
        c.atomic(c.DATA/'password',b'old-fixture-password\n');c.write_json(c.DATA/'settings.json',{'port':8899,'public_ip':'192.0.2.10','public_host':'old.example','tls_cert':'old-cert','tls_key':'old-key','certificate_domain':'old.example'})
        c.atomic(c.DATA/'basepath',b'fixture-path\n')
        c.write_json(c.DATA/'native.json',{'next_id':2,'inbounds':[]})

    def tearDown(self):
        for value in reversed(self.patches):value.stop()
        self.directory.cleanup()

    def test_password_without_old_login_preserves_nodes(self):
        before=(c.DATA/'native.json').read_bytes()
        with patch.object(maintain.getpass,'getpass',side_effect=['new-fixture-password','new-fixture-password']),contextlib.redirect_stdout(io.StringIO()):maintain.password()
        self.assertEqual((c.DATA/'password').read_text(),'new-fixture-password\n');self.assertEqual(before,(c.DATA/'native.json').read_bytes())

    def test_failed_password_restart_restores_previous(self):
        with patch.object(maintain.getpass,'getpass',side_effect=['new-fixture-password','new-fixture-password']),patch.object(c,'wait_ready',side_effect=RuntimeError('fixture service down')):
            with self.assertRaises(RuntimeError):maintain.password()
        self.assertEqual((c.DATA/'password').read_text(),'old-fixture-password\n')

    def test_domain_unbind_retains_credentials(self):
        before=(c.DATA/'password').read_bytes()
        with patch('builtins.input',return_value='none'),patch.object(maintain,'certificate_timer'),contextlib.redirect_stdout(io.StringIO()):maintain.domain()
        value=c.settings();self.assertEqual(value['public_host'],'192.0.2.10');self.assertNotIn('tls_cert',value);self.assertNotIn('certificate_domain',value);self.assertEqual(before,(c.DATA/'password').read_bytes())

    def test_program_directory_is_traversable_with_private_umask(self):
        c.LIB.rmdir();old=os.umask(0o077)
        try:c.program_dir()
        finally:os.umask(old)
        self.assertEqual(c.LIB.stat().st_mode&0o777,0o755)

    def test_legacy_private_directory_is_narrowed_and_symlinks_rejected(self):
        os.chmod(c.DATA,0o755);c.private_dir(c.DATA)
        self.assertEqual(c.DATA.stat().st_mode&0o777,0o700)
        alias=self.root/'alias';alias.symlink_to(c.DATA,target_is_directory=True)
        with self.assertRaises(RuntimeError):c.private_dir(alias)

    def test_maintenance_lock_does_not_create_data_and_survives_removal(self):
        import shutil
        shutil.rmtree(c.DATA)
        with c.maintenance_lock():
            self.assertFalse(c.DATA.exists())
            with self.assertRaises(BlockingIOError):
                with c.maintenance_lock():pass

    def test_ipv6_occupied_port_is_not_available(self):
        try:channel=socket.socket(socket.AF_INET6,socket.SOCK_STREAM)
        except OSError:self.skipTest('IPv6 not supported')
        with channel:
            channel.setsockopt(socket.IPPROTO_IPV6,socket.IPV6_V6ONLY,1)
            try:channel.bind(('::',0))
            except OSError:self.skipTest('IPv6 not available')
            self.assertFalse(c.port_available(channel.getsockname()[1]))

    def test_reserved_panel_and_native_ports_are_skipped(self):
        c.write_json(c.DATA/'native.json',{'inbounds':[{'port':29001}]})
        c.write_json(c.DATA/'state.json',{'tunnels':[{'port':29002}]})
        with patch.object(c,'port_available',return_value=True):
            self.assertNotEqual(c.choose_port(8899),8899)
            self.assertNotEqual(c.choose_port(29001),29001)
            self.assertNotEqual(c.choose_port(29002),29002)

    def test_purge_removes_only_miku_and_keeps_other_panel(self):
        other=self.root/'3x-ui';other.mkdir();(other/'data').write_text('keep')
        c.atomic(c.MENU,('exec python3 '+str(c.LIB/'maintain.py')).encode(),0o755)
        c.atomic(c.UNIT,('ExecStart='+str(c.LIB/'miku')).encode(),0o644)
        with patch.object(c,'run'),contextlib.redirect_stdout(io.StringIO()):maintain.purge()
        for path in (c.DATA,c.LIB,c.UNIT,c.MENU):self.assertFalse(path.exists())
        self.assertEqual((other/'data').read_text(),'keep')

    def test_purge_rejects_foreign_service_before_stopping_or_deleting(self):
        c.atomic(c.UNIT,b'ExecStart=/usr/local/bin/other-panel',0o644)
        with patch.object(c,'run'),self.assertRaises(RuntimeError):maintain.purge()
        self.assertTrue(c.DATA.exists());c.service.assert_not_called()

    def test_reinstall_validates_bundle_before_clearing_data(self):
        with patch.object(c,'verify_distribution',side_effect=RuntimeError('invalid bundle')),patch.object(c,'purge_current') as purge:
            with self.assertRaises(RuntimeError):install.install(self.root/'bad-bundle',reinstall=True)
            purge.assert_not_called()

    def test_update_repairs_old_directory_and_conflicted_port(self):
        bundle=self.root/'bundle';(bundle/'bin').mkdir(parents=True);(bundle/'scripts').mkdir()
        architecture=c.arch();binary='bin/miku-linux-'+architecture
        files={binary:b'new fixture binary','bin/miku-export-linux-'+architecture:b'fixture export helper','scripts/common.py':b'# fixture common','scripts/maintain.py':b'# fixture maintenance','scripts/quick.py':b'# fixture quick','scripts/install.py':b'# fixture install','scripts/miku.sh':b'#!/bin/sh\n','CORE_ASSETS.json':b'{}','LICENSE':b'fixture license','FLAG_ICONS_LICENSE':b'fixture flags license'}
        for name,raw in files.items():c.atomic(bundle/name,raw,0o755 if name.startswith('bin/') or name.endswith('.sh') else 0o644)
        c.atomic(c.LIB/'miku',b'old fixture binary',0o755);os.chmod(c.LIB,0o755)
        c.write_json(c.DATA/'install.json',{'product':'miku-fanout','version':'0.2.0-dev.34','binary_sha256':hashlib.sha256(b'old fixture binary').hexdigest()})
        os.chmod(c.DATA,0o755)
        meta={'version':'0.2.0-dev.35','files':{binary:hashlib.sha256(files[binary]).hexdigest()}}
        with patch.object(c,'verify_distribution',return_value=(bundle,meta)),patch.object(c,'old_maintenance',return_value=(None,None)),patch.object(c,'dependencies'),patch.object(c,'ensure_cores'),patch.object(c,'run'),patch.object(c,'choose_port',return_value=32001),patch.object(c,'banner'),contextlib.redirect_stdout(io.StringIO()):
            install.install(bundle,update=True)
        self.assertEqual(c.settings()['port'],32001)
        self.assertEqual(c.DATA.stat().st_mode&0o777,0o700)
        self.assertEqual((c.DATA/'password').read_bytes(),b'old-fixture-password\n')
        self.assertEqual((c.LIB/'miku').read_bytes(),files[binary])

    def test_existing_certificate_is_reused_even_if_port_80_is_busy(self):
        with patch.object(maintain,'certificate_sources',return_value=[(Path('/fixture/cert'),Path('/fixture/key'))]),patch.object(maintain,'checked_certificate',return_value=(b'cert',b'key')),patch.object(c,'port_available',return_value=False),patch.object(c,'packages') as packages:
            pair=maintain.certificate_pair('node.example.com');self.assertEqual(pair[:2],(b'cert',b'key'));self.assertEqual(pair[2:],('/fixture/cert','/fixture/key'));packages.assert_not_called()

    def test_core_downloads_can_progress_in_parallel(self):
        import threading
        barrier=threading.Barrier(2);seen=[]
        def download(core,architecture):
            seen.append(core);barrier.wait(timeout=2)
        with patch.object(c,'arch',return_value='amd64'),patch.object(c,'core_download',side_effect=download),contextlib.redirect_stdout(io.StringIO()):
            c.ensure_cores()
        self.assertEqual(set(seen),{'xray','sing-box'})

    def test_backup_contains_quick_command_for_version_rollback(self):
        import tarfile
        c.atomic(c.LIB/'quick.py',b'# fixture quick command',0o644)
        target=c.snapshot('fixture')
        with tarfile.open(target) as archive:self.assertIn('lib/quick.py',archive.getnames())

    def test_http_domain_option_prints_full_new_login_url(self):
        out=io.StringIO()
        with patch('builtins.input',return_value='new.example.com'),patch.object(maintain,'certificate_pair',return_value=None),patch.object(maintain,'certificate_timer'),contextlib.redirect_stdout(out):maintain.domain()
        self.assertEqual(c.settings()['public_host'],'new.example.com')
        self.assertIn('新的管理登录地址：http://new.example.com:8899/fixture-path/',out.getvalue())

    def test_certificate_validation_rejects_wrong_domain_and_public_key_permissions(self):
        cert,key=self.root/'fixture.pem',self.root/'fixture.key'
        c.run(['openssl','req','-x509','-newkey','rsa:2048','-nodes','-days','2','-subj','/CN=node.example.com','-addext','subjectAltName=DNS:node.example.com','-keyout',key,'-out',cert],timeout=30)
        os.chmod(key,0o600)
        self.assertEqual(maintain.checked_certificate(cert,key,'node.example.com')[0],cert.read_bytes())
        with self.assertRaises(RuntimeError):maintain.checked_certificate(cert,key,'other.example.com')
        os.chmod(key,0o644)
        with self.assertRaises(RuntimeError):maintain.checked_certificate(cert,key,'node.example.com')

    def test_bound_certificate_keeps_external_renewal_source(self):
        with patch('builtins.input',return_value='new.example.com'),patch.object(maintain,'certificate_pair',return_value=(b'cert',b'key','/fixture/3x-ui/cert','/fixture/3x-ui/key')),patch.object(maintain,'certificate_timer'),contextlib.redirect_stdout(io.StringIO()):maintain.domain()
        entry=json.loads((c.DATA/'domains.json').read_text())[0]
        self.assertEqual(entry['source_cert'],'/fixture/3x-ui/cert')
        self.assertEqual(entry['cert_file'],str(c.DATA/'certificates/new.example.com/certificate.pem'))
        self.assertEqual(c.settings()['tls_cert'],entry['cert_file'])

    def test_readiness_rejects_another_panels_html(self):
        import http.client
        with patch.object(c,'settings',return_value={'port':32001,'public_ip':'192.0.2.10'}),patch.object(http.client,'HTTPConnection') as connect,patch.object(c.time,'sleep'):
            response=connect.return_value.getresponse.return_value
            response.status=200;response.read.return_value=b'<html>3x-ui</html>';response.getheader.return_value=None
            with self.assertRaises(RuntimeError):self.real_wait_ready(seconds=.001)
            response.getheader.return_value='1'
            self.real_wait_ready(seconds=.1)

    def test_split_package_with_more_than_120_members_is_accepted_after_manifest_check(self):
        import tarfile
        package=self.root/'split.tar.gz'
        with tarfile.open(package,'w:gz') as archive:
            for i in range(150):
                item=tarfile.TarInfo('miku-gate/frontend/public/fixture-'+str(i)+'.txt')
                item.size=1;item.mode=0o644
                archive.addfile(item,io.BytesIO(b'x'))
        with patch.object(c,'verify_distribution',side_effect=lambda directory:(directory,{})) as verify,patch.object(install,'install') as upgrade:
            maintain.update(str(package))
        self.assertEqual(verify.call_count,1)
        self.assertTrue(upgrade.call_args.kwargs['update'])

if __name__=='__main__':unittest.main()
