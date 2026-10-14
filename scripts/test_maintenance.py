"""Offline fixture checks; all fixed destinations and service operations mocked."""
import contextlib
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import common as c
import maintain

class MaintenanceTests(unittest.TestCase):
    def setUp(self):
        self.directory=tempfile.TemporaryDirectory(dir='/workspace/scratch/88d5335da7f4/.tooling/tmp' if Path('/workspace/scratch/88d5335da7f4/.tooling/tmp').exists() else None)
        self.root=Path(self.directory.name)
        self.patches=[]
        for name in ('DATA','LIB'):
            path=self.root/name;path.mkdir(mode=0o700);self.patches.append(patch.object(c,name,path))
        for name in ('UNIT','MENU'):self.patches.append(patch.object(c,name,self.root/name))
        self.patches += [patch.object(c,'systemd',return_value=True),patch.object(c,'service'),patch.object(c,'wait_ready'),patch.object(c,'info'),patch.object(c,'active',return_value=True)]
        for value in self.patches:value.start()
        c.atomic(c.DATA/'password',b'old-fixture-password\n');c.write_json(c.DATA/'settings.json',{'port':8899,'public_ip':'192.0.2.10','public_host':'old.example','tls_cert':'old-cert','tls_key':'old-key','certificate_domain':'old.example'})
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

if __name__=='__main__':unittest.main()
