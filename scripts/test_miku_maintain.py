import importlib.util
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
    def test_unknown_data_paths_and_restore_commands_are_not_implemented(self):
        self.assertEqual(len(maintenance.FILES),4)
        self.assertNotIn('restore',maintenance.main.__doc__ or '')
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

if __name__=='__main__':unittest.main()
