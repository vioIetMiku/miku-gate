"""Image cache build keys; no Docker daemon, privileged actions or network."""
import hashlib
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('miku_build_key',ROOT/'docker/build-key.py')
b=importlib.util.module_from_spec(spec);spec.loader.exec_module(b)

class BuildKeyTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name)
        for name in b.RUNTIME:
            p=self.root/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(b'fixture:'+name.encode())
        (self.root/'src').mkdir();(self.root/'src/main.go').write_text('package main')
        (self.root/'go.mod').write_text('module fixture');(self.root/'go.sum').write_text('')
        (self.root/'VERSION').write_text('0.2.5')
        (self.root/'bin').mkdir()
        files={}
        for arch in ('amd64','arm64'):
            name='bin/miku-linux-'+arch;raw=b'fixture executable '+arch.encode();(self.root/name).write_bytes(raw)
            files[name]=hashlib.sha256(raw).hexdigest()
        (self.root/'BUILD.json').write_text(json.dumps({'product':'miku-fanout','format':1,'files':files}))

    def test_same_inputs_have_same_key(self):
        self.assertEqual(b.fingerprint(self.root,'amd64','prebuilt'),b.fingerprint(self.root,'amd64','prebuilt'))

    def test_mutable_credentials_docs_and_backups_do_not_invalidate_image(self):
        before=b.fingerprint(self.root,'amd64','source')
        for name in ('.env','README.md','docker-data/password','docker-backups/key.tar.gz'):
            p=self.root/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(b'private change')
        self.assertEqual(before,b.fingerprint(self.root,'amd64','source'))

    def test_runtime_changes_invalidate(self):
        before=b.fingerprint(self.root,'amd64','prebuilt')
        (self.root/'scripts/common.py').write_bytes(b'updated runtime')
        self.assertNotEqual(before,b.fingerprint(self.root,'amd64','prebuilt'))

    def test_source_changes_invalidate_only_source_mode(self):
        source=b.fingerprint(self.root,'amd64','source');binary=b.fingerprint(self.root,'amd64','prebuilt')
        (self.root/'src/main.go').write_text('package main // updated')
        self.assertNotEqual(source,b.fingerprint(self.root,'amd64','source'))
        self.assertEqual(binary,b.fingerprint(self.root,'amd64','prebuilt'))

    def test_prebuilt_digest_mismatch_is_rejected(self):
        (self.root/'bin/miku-linux-amd64').write_bytes(b'corrupt')
        with self.assertRaises(ValueError):b.fingerprint(self.root,'amd64','prebuilt')

    def test_architecture_and_mode_are_part_of_key(self):
        self.assertNotEqual(b.fingerprint(self.root,'amd64','prebuilt'),b.fingerprint(self.root,'arm64','prebuilt'))
        self.assertNotEqual(b.fingerprint(self.root,'amd64','source'),b.fingerprint(self.root,'amd64','prebuilt'))
        with self.assertRaises(ValueError):b.fingerprint(self.root,'386','prebuilt')

if __name__=='__main__':unittest.main()
